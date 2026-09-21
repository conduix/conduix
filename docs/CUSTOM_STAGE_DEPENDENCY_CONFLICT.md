# Custom Stage Dependency Version Conflict

[한국어](CUSTOM_STAGE_DEPENDENCY_CONFLICT.ko.md)

> Status: **Problem statement — no solution chosen yet.**
> This document exists to inform a decision, not to announce one. The options below
> have not been picked.

Custom stages are compiled into a single binary. As a result, **when stages written by
different organizations at different times require different versions of the same
library, the build breaks.**

---

## One-line summary

| | |
|---|---|
| **What** | Multiple stages requiring different minor/patch versions of the same module |
| **Why it happens** | Because they were written at different **times**. No one has to be stubborn — the passage of time is enough |
| **What happens today** | One stage failing to compile **blocks the build for every stage** |
| **Can it be prevented?** | **No**, as long as compile-in is used. It follows from Go's module rules (MVS) |

---

## Why this is unavoidable

This is not caused by users making unreasonable demands.

```
2026  A company's developer writes a stage  →  codes against resty 0.8, the latest then
2027  B company's developer writes a stage  →  codes against resty 1.0, the latest then
```

Both simply used "the latest at the time." Neither the conduix developers nor either
company's developers know about each other, and no one can know what a future author
will need. **The conflict arises purely because the writing times differ.**

So the question is not "how often does this happen" but
**"it will happen eventually — what happens then?"**

---

## Technical basis

### One binary, one version

Every custom stage is imported into a single `package main` and linked into one binary.

```go
// runner_builder.go:642-651 (GenerateRegistryCustom)
buf.WriteString("package main\n\n")
for _, p := range plugins {
    fmt.Fprintf(&buf, "\t%s %q\n", alias, modPath)   // all plugins imported into one main
}
```

Go's MVS (Minimal Version Selection) picks **exactly one version per module** for a
build. The language offers no way to link two copies of the same package into one
process.

```go
// runner_builder.go:388
// The allowed modules must also be required by the main module so that go mod tidy
// resolves the plugin's (locally replaced) external dependencies to a single version.
```

### The registry enforces a single version too

```go
// models.go:645-646
ModulePath string `gorm:"primaryKey;size:255"`  // PK — one row per module
Version    string // "single per module — the physical basis for conflict prevention"
```

Because `module_path` is the primary key, `resty 0.8` and `resty 1.0` cannot both be
registered. POSTing an already-registered module returns 409.

```
module already registered: <path> (version <v>). use PUT to update.
```

### Exception: major versions do coexist

From v2 onward, Go puts the **major version in the module path**. Different paths mean
different modules, so they coexist.

```
github.com/go-resty/resty/v2   v2.16.5   ← can be registered as its own row
github.com/go-resty/resty/v3   v3.0.0    ← can be registered as its own row
```

**But v0 and v1 carry no version in the path.** 0.8 → 1.0 is the same path, so those
cannot coexist. "Different majors are fine" applies only from v2 up.

---

## What actually happens today

When an admin updates a module version (PUT):

| Step | Result |
|---|---|
| resty updated 0.8 → 1.0 | Registry reflects it |
| Next runner build | A company's stage, written against 0.8, **fails to compile** |
| Build handling | `go build` failure returns immediately — `status=failed` (`runner_builder.go:414`) |
| Existing executions | **Keep running** (the previous `ready` binary is retained) |
| Adding new stages, core changes | **All blocked** ← the core of the problem |

**One company's code holds every other company hostage.** Until A fixes its stage,
B and C cannot ship a new stage either.

The build today is **all-or-nothing**. There is no path that drops the failing stage and
keeps the rest. (A `skipped` status exists, but it means "identical hash, rebuild
skipped" — not stage exclusion. See `runner_builder.go:205-214`.)

### Users cannot break each other directly

```go
// routes.go:417-419
modules.POST   — RoleMiddleware(admin)
modules.PUT    — RoleMiddleware(admin)
modules.DELETE — RoleMiddleware(admin)
```

Registering and updating modules is **admin-only**. A regular user cannot bump a version
and break someone else's stage. But this only **funnels the conflict into an admin
decision** — it does not remove it. And today **the admin has no way to know what will
break** before pressing PUT.

---

## Options

### ① Mitigate — the conflict remains, but hurts less

1. **Record per-stage module usage** — on a successful build, store the versions actually used
2. **Impact analysis before update** — on PUT, show "3 stages will be affected" first
3. **Dry-run build** — build against the new version to detect breakage in advance
4. **Exclude failing stages from the build** — build the rest without the broken one ← **removes the hostage problem**

Item 4 is the key one. It does not remove conflicts, but **no one is blocked by another
company's code any more.**

- Upside: additive, reversible, no performance cost.
- Limit: **A's stage stays broken.** That company still has to fix its code.

### ② Process isolation — remove the conflict itself

One process per stage plus gRPC (go-plugin or similar). Each carries its own `go.mod`,
so versions are fully isolated.

- Upside: conflicts disappear at the root. One stage cannot affect another.
- Cost: **performance and deployment complexity.** But **we do not know the magnitude**
  (see below). Process/IPC management is added, and it is a redesign that is hard to undo.

⚠️ **There is no quantitative basis in this repository for judging this option.**
conduix previously used gRPC-based plugins (V3, HashiCorp go-plugin) and moved to
compile-in. Yet [ADR-0003](adr/0003-plugin-architecture-evolution.md) states plainly:

> "No quantitative basis (benchmarks etc.) was recorded for dropping gRPC. Only the
> direction stated in the commit message."

So evaluating ② requires **measuring first.** "We already abandoned it once" is not a
basis on its own — that decision was made without numbers too.

---

## What the decision needs

This document deliberately stops short of a recommendation. The following need answers first.

1. **How many independent parties supply stages?**
   One organization → ① suffices. Several companies publishing independently → ②'s
   isolation earns its cost.

2. **How long do stages live?**
   The longer they live, the more stages accumulate pinned to old versions. ① cannot
   revive those.

3. **How often are stages invoked — and what does IPC actually cost?**
   Per-record invocation makes inter-process overhead matter for throughput; rare
   invocation makes it negligible. **That cost has never been measured here**, so taking
   ② seriously requires a benchmark first.

---

## Related

- [PIPELINE_ORCHESTRATION.md](PIPELINE_ORCHESTRATION.md) — how pipelines are chained
- [ARCHITECTURE.md](ARCHITECTURE.md) — execution structure (compile-in binary delivery)
- [adr/](adr/) — design decision records
