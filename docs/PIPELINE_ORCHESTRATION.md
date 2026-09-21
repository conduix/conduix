# Pipeline Orchestration Guide

[한국어](PIPELINE_ORCHESTRATION.ko.md)

There are three ways to chain pipelines together. **They all sound like "dependency," but they do different things.**

> Why this document exists: `depends_on` was set correctly, yet two pipelines
> still started 0.1 seconds apart. The cause was `execution_mode` — it was left
> at its default. The table below prevents that mistake.

---

## In 30 seconds: which one do you want

| What you want | Use | Required setting |
|---|---|---|
| **B starts only after A finishes** (they share a table, etc.) | `depends_on` | `execution_mode: dag` ⚠️ |
| Just run them **one at a time** | `priority` | `execution_mode: sequential` |
| B **consumes A's output records** | pipeline link | link API + Kafka |
| Unrelated, run them all at once | (nothing) | `execution_mode: parallel` (default) |

**The most common mistake**: setting `depends_on` but leaving `execution_mode` alone.
The default is `parallel`, which **silently ignores** `depends_on` — no error, no log line.

---

## 1. `depends_on` — execution order (DAG)

"B starts only after A completes."

### When

- Two pipelines write to the **same table** (collect → enrich)
- B needs A's results to **already be in the database**
- Reversing the order corrupts the data

### Configuration

```yaml
execution_mode: dag        # ← without this, depends_on is ignored

pipelines:
  - id: restroom-api-collect        # collect
    priority: 0
    input: { type: rest_api, ... }

  - id: restroom-csv-enrich         # enrich
    priority: 1
    depends_on: [restroom-api-collect]
    input: { type: rest_api, ... }
```

### How it works

`GroupExecutor.runDAG()` builds a dependency graph and starts a pipeline only once
**all** of its dependencies have completed. Pipelines with no dependency between
them still run in parallel.

### How to verify

`runDAG` emits no dedicated log line. Judge by the **gap between pipeline start times**.

```bash
kubectl logs -n conduix <job-pod> | grep "creating input source"
```

```
07:59:14.858  restroom-api-collect     ← first
08:02:00.447  restroom-csv-enrich      ← 2m46s later = dependency honored ✅
```

A gap **under 0.1 seconds** means it is running in `parallel` — check `execution_mode`.

---

## 2. `priority` — simple sequential order

"Lowest number first, one at a time."

### When

- You just want them to run in order
- Spelling out every dependency is overkill
- You want to avoid concurrent execution to save resources

### Configuration

```yaml
execution_mode: sequential   # ← without this, priority is ignored

pipelines:
  - id: first
    priority: 0
  - id: second
    priority: 1
```

### How it differs from `depends_on`

| | `sequential` + `priority` | `dag` + `depends_on` |
|---|---|---|
| Ordering basis | Number order | Declared dependencies |
| Parallelism | None (always one) | Independent ones run together |
| If an earlier one fails | Depends on config | Dependents never start |
| Best for | Lining things up | Real data dependencies |

If you have five pipelines and only two of them are order-sensitive, prefer `dag` —
the other three have no reason to wait.

---

## 3. pipeline link — data flow (parent → child)

"Records produced by A become the input of B."

### When

- Collect a board index (A) → collect **each post** (B)
- Every **output record** of the parent feeds the child
- Hierarchical data (Board → Post → Comment)

### The fundamental difference from `depends_on`

`depends_on` only sets **order**. No data is handed over — B has to read the
database again.

A pipeline link streams **records over a Kafka topic**. B consumes them as A
produces them, so it does not wait for A to finish.

```
depends_on :  [A done] ──→ [B starts]        ordering
link       :  [A] ──Kafka──→ [B]             record stream
```

### Configuration

Links are created through a **separate API**, not in the pipeline config.

```bash
POST /api/v1/pipeline-links
{
  "workflow_id": "...",
  "parent_pipeline_id": "board-collect",
  "child_pipeline_id": "post-collect"
}
```

Once a link exists, the child's input is **automatically replaced with a Kafka
source** at execution time (`kafka_link_input_<parent>`). You do not declare an
input on the child.

Kafka is required — locally, `docker-compose --profile with-kafka up -d`.

---

## The three `execution_mode` values

This applies to the **whole workflow**. It cannot be set per pipeline.

| Value | Behavior | `priority` | `depends_on` |
|---|---|---|---|
| `parallel` (default) | All at once | ignored | **ignored** ⚠️ |
| `sequential` | One at a time, by number | **used** | ignored |
| `dag` | By dependency graph | sorting only | **used** |

Setting it:

```bash
# API
curl -X PUT .../api/v1/workflows/<id> -d '{"execution_mode":"dag"}'

# Check in the database
SELECT execution_mode FROM workflows WHERE id='<id>';
```

---

## Worked example: public restroom collect + enrich

Two distributions of the same dataset write to the same `restrooms` table.

- **API (15155058)** — the collector. Name, address, coordinates
- **CSV (15012892)** — the enricher. Accessible/child toilet counts, diaper-changing
  station location

Their management numbers overlap by 99.99%, so this is **enrichment, not
replacement**. If the order flips, the API collection overwrites the CSV values, or
records that exist only in the CSV get inserted without a name (8 rows actually did).

```yaml
execution_mode: dag

pipelines:
  - id: restroom-api-collect
    priority: 0
    # rest_api → geocode → restrooms

  - id: restroom-csv-enrich
    priority: 1
    depends_on: [restroom-api-collect]
    # CSV (cp949) → remap → restrooms (UPDATE)
```

Result: 107,164 records (53,582 collected + 53,582 enriched), 0 failures, 7m43s.

---

## Troubleshooting

### I set `depends_on` but they run at the same time

Check `execution_mode`. If it is `parallel` (the default), `depends_on` is ignored.

```sql
SELECT execution_mode FROM workflows WHERE id='<id>';   -- must be dag
```

This failure **raises no error**. Logs do not distinguish it either — the only
signal is the gap between start times.

### I set `priority` but the order is random

Outside `sequential`, `priority` is used only for sorting; it does not gate
execution. To actually enforce order, use `sequential` or `dag` + `depends_on`.

### The child pipeline receives no data

`depends_on` does not hand over data. To receive the parent's records you need a
pipeline link, and Kafka must be running.

### What happens with a dependency cycle

`runDAG` detects it and aborts:

```
circular dependency detected
```

Do not let pipelines point at each other (A → B → A). Listing a pipeline in its own
`depends_on` is the same thing.

---

## Related documents

- [ARCHITECTURE.md](ARCHITECTURE.md) — execution delegation (agent → K8s Job / resident pod)
- [EXECUTION_TOPOLOGY_INTENT.md](EXECUTION_TOPOLOGY_INTENT.md) — execution topology intent
- [design-v2.md](design-v2.md) — the Input/Stage/Output model
