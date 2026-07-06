<p align="center">
  <img src="images/logo-title-nobg.png" alt="Conduix Logo" width="600">
</p>

**Conduix** = **Intelligent Conduit**

A platform service that connects data, controls flow, and orchestrates pipelines.

Scalable Data Pipeline Platform with Parallel Processing

[한국어](README.ko.md)

## In 30 Seconds

**What it is:** A Kubernetes-native data pipeline platform that unifies *connectors + stream/batch transformation + orchestration* into one workflow — so you don't run Kafka Connect, Flink, and Airflow as three separate stacks.

**What you do with it:** Read from a source → transform → load into one or many sinks (each with its own per-output transforms), as either a **batch** job or a **realtime** stream, defined through a **GUI or YAML/API** (same model).

- **16 sources / 9 sinks** built in (Kafka, SQL, REST, MySQL CDC, files, S3, Elasticsearch, MongoDB, BigQuery, …).
- **21 built-in transform stages** + custom logic via **JavaScript (no build, edit→save→run)** or **native Go plugins** (compiled into the runner image).
- **One source → many sinks, each shaped differently** (per-output PreStages), plus routing (fan-out/condition/filter) and parent–child pipelines.
- **Ops built in:** circuit breaker, DLQ, retry+backoff, orphan-execution detection, Prometheus metrics, structured logging.

**When to pick it:** you want to consolidate connector + transform + schedule into one K8s-native platform, let operators build pipelines in a GUI while engineers version them as YAML, and iterate transforms fast.
**When not to:** heavy stateful exactly-once stream compute (→ Flink), general-purpose task orchestration (→ Airflow), or PostgreSQL CDC (not supported — route via Debezium/Kafka).

→ Full comparison & selection guide: **[docs/COMPARISON.md](docs/COMPARISON.md)** · Architecture: **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** · Design decisions: **[docs/adr/](docs/adr/)**

## Overview

Conduix is a scalable data pipeline system that combines [Bento](https://github.com/warpstreamlabs/bento) (MIT License) proven connectors with parallel processing architecture.

**Hybrid Architecture**:
- **Parallel Processing**: Stage-level parallel processing, batch optimization
- **Bento Connectors**: Reuse proven connectors for Kafka, Elasticsearch, S3, etc.
- **Pure Go**: Single binary, no external dependencies

## Key Features

- **Parallel Batch Processing**: Stage always parallel, Output selectable bulk/individual mode
- **Input/Stage/Output Separation**: Clear separation of data ingestion (Input), transformation (Stage+PreStages), and storage (Output)
- **Bento Connector Integration**: Rich connectors including Kafka, ES, S3, HTTP, NATS, AMQP
- **High Availability**: Redis-based checkpoints, automatic fault handling
- **Operations Tools**: Web-based pipeline configuration, monitoring, scheduling
- **SSO Support**: OAuth2/OIDC-based login
- **Flexible Deployment**: Physical servers, Docker, Kubernetes (Helm) support

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Control Plane (Operations)               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │   Web UI    │  │  API Server │  │  Scheduler  │         │
│  │  (React)    │  │  (Go+Gin)   │  │  (Go+Cron)  │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
└─────────────────────────────────────────────────────────────┘
                              │
                    REST API + Redis Pub/Sub
                              │
┌─────────────────────────────────────────────────────────────┐
│                   Pipeline Agent Cluster                     │
│  ┌───────────────────────────────────────────────────┐      │
│  │  Agent (Parallel Processing + Bento Connectors)   │      │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐           │      │
│  │  │  Input  │→ │  Stage  │→ │ Output  │           │      │
│  │  │ (Kafka) │  │ (remap) │  │  (ES)   │           │      │
│  │  └─────────┘  └─────────┘  └─────────┘           │      │
│  └───────────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## Pipeline Design

### Core Concepts

Conduix uses a **Unix Pipe-inspired linear pipeline** design with **DataType-based DAG** for complex workflows.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Pipeline Design Philosophy                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  1. Single Pipeline = Unix Pipe (Linear Chain)                               │
│     ┌────────┐    ┌─────────┐    ┌─────────┐    ┌──────────┐    ┌────────┐│
│     │ Input  │───→│ Stage 1 │───→│ Stage 2 │───→│PreStages │───→│ Output ││
│     └────────┘    └─────────┘    └─────────┘    └──────────┘    └────────┘│
│                                                                               │
│  2. Multiple Pipelines = DataType Dependency DAG                             │
│     ┌──────────────┐                                                         │
│     │ Board Pipeline│ (DataType: Board)                                      │
│     └───────┬──────┘                                                         │
│             │ triggers                                                        │
│             ▼                                                                 │
│     ┌──────────────┐                                                         │
│     │ Post Pipeline │ (DataType: Post, Parent: Board)                        │
│     └───────┬──────┘                                                         │
│             │ triggers                                                        │
│             ▼                                                                 │
│     ┌──────────────┐                                                         │
│     │Comment Pipeline│ (DataType: Comment, Parent: Post)                     │
│     └──────────────┘                                                         │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Input, Stage, and Output Types

**Input** handles data ingestion, **Stage** handles common transformation/processing, **Output** handles data storage/delivery. Each Output can have its own **PreStages** for Output-specific transformations.

#### Input Types (Data Ingestion)

| Type | Description | Example Use |
|------|-------------|-------------|
| **kafka** | Kafka consumer | Event streaming |
| **rest_api** | REST API polling | External API |
| **partitioned_http** | Partitioned REST API | Parallel API collection |
| **sql** | SQL database | MySQL, PostgreSQL |
| **partitioned_sql** | Partitioned SQL | Parallel DB collection |
| **cdc** | Change Data Capture | MySQL/PostgreSQL replication |
| **mongodb_cdc** | MongoDB Change Stream | MongoDB replication |
| **file** | File source | CSV, JSON files |
| **k8s_logs** | Kubernetes logs | Container logs |
| **websocket** | WebSocket connection | Real-time data |
| **mqtt** | MQTT subscriber | IoT devices (wildcard support) |
| **sse** | Server-Sent Events | Event streams |
| **sqs** | AWS SQS | Message queue |
| **rabbitmq** | RabbitMQ (AMQP) | Message queue |
| **pubsub** | Google Cloud Pub/Sub | GCP messaging |
| **redis_stream** | Redis Stream | Redis messaging |

#### Stage Types (Transformation/Processing)

| Type | Description | Example Use |
|------|-------------|-------------|
| **filter** | Filter records by condition | Remove invalid data |
| **remap** | Transform/rename fields (Bloblang) | JSON field mapping |
| **drop** | Drop specific fields | Remove sensitive data |
| **merge** | Merge multiple fields | Combine name fields |
| **split** | Split field by regex | Parse log lines |
| **encrypt** | Encrypt/mask fields | PII protection (AES256, SHA, mask) |
| **dedupe** | Remove duplicates | Deduplication |
| **default** | Set default values | Fill nulls |
| **cast** | Type conversion | String to int |
| **timestamp** | Timestamp handling | Add/convert timestamps |
| **throttle** | Rate limiting | API rate limit |
| **sample** | Record sampling | Data sampling |
| **validate** | JSON Schema validation | Data quality |
| **contract** | Data Contract validation | Business rules |
| **route** | Event routing | CDC event routing |
| **aggregate** | Window-based aggregation | Time-window stats |
| **enrich** | Add external data | Lookup join |
| **sub_pipeline** | Trigger child pipeline | Pipeline chaining |
| **script** | Starlark scripting | Custom transformation logic |

#### Output Types (Storage/Delivery)

| Type | Description | Example Use |
|------|-------------|-------------|
| **sql** | SQL database | MySQL, PostgreSQL |
| **elasticsearch** | Elasticsearch | Log indexing |
| **kafka** | Kafka topic | Event streaming |
| **mongodb** | MongoDB | Document storage |
| **s3** | S3 storage | Data lake |
| **gcs** | Google Cloud Storage | GCP storage |
| **bigquery** | Google BigQuery | Data warehouse |
| **rest_api** | REST API | External API call |
| **file** | File output | Local/remote file |

#### PreStages (Output-specific Transformation)

Each Output can define `pre_stages` for Output-specific transformations. This allows different transformations before each destination.

```yaml
outputs:
  - name: "elasticsearch"
    type: elasticsearch
    pre_stages:              # ES-specific transformations
      - type: remap
        config:
          mappings:
            "@timestamp": ".created_at"
    config:
      endpoints: ["http://es:9200"]
  - name: "s3-backup"
    type: s3
    pre_stages:              # S3-specific transformations
      - type: drop
        config:
          fields: ["sensitive_data"]
    config:
      bucket: "backup"
```

### DataType Dependency Patterns

#### Pattern 1: Different DataTypes (Hierarchical Collection)

```
Use Case: Collect boards → then collect posts for each board

Pipeline A: Board Collection
  API(/boards) → Transform → Elasticsearch
  Target DataType: Board

Pipeline B: Post Collection
  API(/boards/{board_id}/posts) → Transform → Elasticsearch
  Target DataType: Post
  Parent DataType: Board  ← Different!

Execution: Pipeline A completes → Pipeline B starts (for each board)
```

#### Pattern 2: Same DataType, Different Processing (Fan-out)

```
Use Case: Same data needs different processing paths

Pipeline 1: Order Ingestion (Source)
  API → Kafka
  Target DataType: Order

Pipeline 2: Order Detail Storage (Consumer A)
  Kafka → Enrich → Elasticsearch
  Target DataType: Order  ← Same!
  Parent: Pipeline 1

Pipeline 3: Order Analytics (Consumer B)
  Kafka → Aggregate → Dashboard DB
  Target DataType: Order  ← Same!
  Parent: Pipeline 1

┌──────────────┐
│ Pipeline 1   │ API → Kafka
│ (Order)      │
└──────┬───────┘
       │ Kafka Topic
       ├─────────────────────┐
       ▼                     ▼
┌──────────────┐     ┌──────────────┐
│ Pipeline 2   │     │ Pipeline 3   │
│ (Order→ES)   │     │ (Order→Agg)  │
└──────────────┘     └──────────────┘
```

### Batch Processing

Batch processing optimizes data pipeline performance:
- **Stage**: Always parallel processing (concurrent workers)
- **Output**: Selectable bulk or individual mode

```yaml
pipelines:
  - id: batch-pipeline
    name: "Batch Processing Example"
    input:                   # Data source (source also supported)
      type: rest_api
      config:
        url: "https://api.example.com/users"
    batch:
      enabled: true
      output_mode: bulk      # bulk | individual
      size: 100              # Records per batch
      workers: 10            # Parallel workers for Stage
      flush_interval: "5s"   # Time-based flush
    stages:                  # Common transformations
      - name: "filter"
        type: filter
        config:
          condition: ".status == 'active'"
      - name: "transform"
        type: remap
        config:
          mappings:
            user_id: ".id"
            user_name: ".name"
    outputs:
      - name: "elasticsearch"
        type: elasticsearch
        pre_stages:          # ES-specific transformations
          - type: remap
            config:
              mappings:
                "@timestamp": ".created_at"
        config:
          endpoints: ["http://es:9200"]
          index: "users"
```

| Output Mode | Description | Use Case |
|-------------|-------------|----------|
| **bulk** | Batch delivery | SQL bulk INSERT, ES bulk API |
| **individual** | One-by-one delivery | APIs without bulk support |

### When to Use Each Approach

| Scenario | Recommended Approach |
|----------|---------------------|
| Different data types (Board→Post) | DataType dependency |
| Same data, different processing | Kafka boundary + separate pipelines |
| Simple in-process fan-out | Router Stage |
| Need independent scaling | Separate pipelines with Kafka |
| Need fault isolation | Separate pipelines with Kafka |
| Low latency required | Router Stage (no Kafka hop) |

## Project Structure

```
conduix/
├── pipeline-core/     # Pipeline core (Parallel processing, Bento integration)
├── pipeline-worker/    # Pipeline execution agent
├── control-plane/     # Operations tool backend API
├── web-ui/            # Operations tool frontend
├── shared/            # Shared types/constants
├── plugin-sdk/        # Plugin SDK for custom stages
└── deploy/            # Deployment (Docker, Helm, scripts)
```

## Documentation

- [Standalone Pipeline Execution Guide](docs/standalone-usage.md) - Run independently without operations tool
- [Fault Handling Scenarios](#fault-tolerance) - Redis/Kafka fault handling

## Quick Start

### Prerequisites

- Go 1.26+
- Node.js 18+
- Docker & Docker Compose
- MySQL 8.0
- Redis 7.0

### Development Environment

```bash
# Install dependencies
make deps

# Start infrastructure (MySQL, Redis)
make infra-up

# Build all services
make build

# Run in development mode
make dev
```

### Run with Docker Compose

```bash
docker-compose up -d
```

### Kubernetes Deployment

```bash
helm install conduix ./deploy/helm/conduix
```

## Pipeline Configuration Examples

### Workflow Pipeline Configuration

```yaml
# Workflow containing multiple pipelines
id: "wf-001"
name: "Log Processing Workflow"
type: "batch"
execution_mode: "dag"

pipelines:
  - id: "pipeline-1"
    name: "Log Ingestion"
    priority: 1
    input:                   # Data source (source also supported)
      type: rest_api
      name: "log-api"
      config:
        url: "https://api.example.com/logs"
        method: "GET"
    stages:                  # Common transformations
      - name: "parse"
        type: remap
        config:
          mappings:
            timestamp: ".created_at"
            message: ".log_message"
      - name: "filter"
        type: filter
        config:
          condition: ".level != 'debug'"
    outputs:
      - name: "elasticsearch"
        type: elasticsearch
        pre_stages:          # ES-specific transformations
          - type: remap
            config:
              mappings:
                "@timestamp": ".timestamp"
        config:
          endpoints: ["http://es:9200"]
          index: "logs"
      - name: "s3-backup"
        type: s3
        pre_stages:          # S3: remove sensitive fields
          - type: drop
            config:
              fields: ["user_ip", "session_id"]
        config:
          bucket: "logs-backup"
          region: "ap-northeast-2"
    batch:
      enabled: true
      output_mode: bulk
      size: 100
      workers: 10
```

## Fault Tolerance

Conduix has built-in resilience mechanisms for various fault scenarios.

### Fault Recovery Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           Fault Recovery Mechanisms                              │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                    ResilientClient (Redis Common)                         │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │   │
│  │  │Auto-Reconnect│  │Circuit Breaker│  │ Local Cache │  │Auto-Resubscribe│ │   │
│  │  │(Exp.Backoff) │  │ (Open/Close) │  │  (Fallback) │  │   (Pub/Sub)   │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                  Actor Supervisor (Kafka/Source Common)                   │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │   │
│  │  │  Checkpoint  │  │   Restart    │  │   Backoff    │  │    Offset    │  │   │
│  │  │   Recovery   │  │   Strategy   │  │   Strategy   │  │   Tracking   │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

### Redis Fault Scenarios

Redis is used for communication between Control Plane and Agent, checkpoint storage, and real-time metrics delivery.

#### Response by Fault Type

| Scenario | Agent Behavior | Control Plane Behavior | Recovery Method |
|----------|---------------|------------------------|-----------------|
| **Temporary Network Disconnection** (< 30s) | Auto-reconnect (Exponential Backoff), use local cache | Circuit Breaker activation, queue requests | Auto-normalize on reconnection |
| **Redis Server Down** | Switch to REST API fallback mode | Store in command queue, respond 202 Accepted | Resend pending commands after Redis restart |
| **Extended Redis Failure** (> 5min) | Receive commands via REST polling (5s interval) | Switch to DB-based state management | Continue operation without manual intervention |
| **Redis Recovery** | Hybrid → Redis mode return, Pub/Sub resubscribe | Batch resend pending commands | Automatic |

#### Agent Communication Mode Transition

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Agent Communication Mode State Diagram                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                    Redis Connection Success                                  │
│                         │                                                    │
│                         ▼                                                    │
│   ┌─────────────────────────────────────┐                                   │
│   │         ModeRedis (Default)         │◀──────────────────────┐           │
│   │  • Receive commands via Redis Pub/Sub│                       │           │
│   │  • Store heartbeat in Redis          │     Redis Connection  │           │
│   │  • Send real-time metrics            │     Recovery          │           │
│   └──────────────┬──────────────────────┘     (after stable)     │           │
│                  │                                               │           │
│                  │ Redis Connection Failure Detected             │           │
│                  ▼                                               │           │
│   ┌─────────────────────────────────────┐                       │           │
│   │          ModeHybrid                 │───────────────────────┘           │
│   │  • Try Redis + REST simultaneously  │                                   │
│   │  • Gradual recovery check           │                                   │
│   └──────────────┬──────────────────────┘                                   │
│                  │                                                           │
│                  │ Redis Continues to Fail                                   │
│                  ▼                                                           │
│   ┌─────────────────────────────────────┐                                   │
│   │          ModeREST (Fallback)        │                                   │
│   │  • Send heartbeat via REST API      │                                   │
│   │  • Receive commands via REST polling│                                   │
│   │  • Continue operation without limits│                                   │
│   └─────────────────────────────────────┘                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### ResilientClient Key Settings

```yaml
# Reconnection settings
max_retries: 0              # Infinite retries
initial_backoff: 100ms      # Initial wait time
max_backoff: 30s            # Maximum wait time
backoff_multiplier: 2.0     # Backoff increase multiplier

# Circuit Breaker settings
failure_threshold: 5        # Failures before Circuit Open
success_threshold: 2        # Successes before Circuit Close
open_timeout: 30s           # Circuit Open duration

# Local cache settings (read fallback)
enable_local_cache: true
local_cache_ttl: 5m
local_cache_max_size: 1000
```

---

### Kafka Fault Scenarios

Kafka is used in Input (data collection) and Output (data transmission).

#### Response by Fault Type

| Scenario | Input Behavior | Output Behavior | Data Guarantee |
|----------|-----------------|-----------------|----------------|
| **Temporary Broker Disconnection** | Auto-reconnect, restart Consumer | Buffer then resend | At-least-once |
| **Broker Down** | Restart with backoff | Store in local buffer | At-least-once |
| **Partition Rebalance** | Offset adjustment, checkpoint recovery | Complete in-progress batch then reconnect | Exactly-once (with checkpoint) |
| **Leader Change** | Auto-detect new Leader | Auto-switch to new Leader | At-least-once |
| **Topic Deletion/Permission Error** | Error logging | Error logging, stop retries | Manual intervention required |

#### Kafka Input Recovery Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Kafka Input Fault Recovery Flow                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────┐     │
│  │                    Pipeline Executor                                │     │
│  │                                                                     │     │
│  │   Restart policy: max 5 retries within 5 minutes                   │     │
│  │   Backoff: 1s → 2s → 4s → 8s → 16s                                 │     │
│  │                                                                     │     │
│  └────────────────────────────────────────────────────────────────────┘     │
│         │                    │                    │                          │
│         ▼                    ▼                    ▼                          │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐                    │
│  │ KafkaInput  │     │ KafkaInput  │     │ KafkaInput  │                    │
│  │ Partition-0 │     │ Partition-1 │     │ Partition-2 │                    │
│  └──────┬──────┘     └─────────────┘     └─────────────┘                    │
│         │                                                                    │
│         │ Broker Connection Failed                                           │
│         ▼                                                                    │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ 1. Input error detected                                             │    │
│  │ 2. Check restart policy                                             │    │
│  │ 3. Query last offset from checkpoint                                │    │
│  │ 4. Reconnect and restart from offset                                │    │
│  │ 5. Exponential backoff on repeated failure                          │    │
│  │ 6. Mark pipeline as error when max_restarts exceeded                │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Checkpoint-based Recovery (Exactly-Once)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Kafka Checkpoint Recovery Mechanism                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  [Normal Operation]                                                          │
│                                                                              │
│  Kafka ──▶ Input ──▶ Stage ──▶ Output                                      │
│    │              │                          │                               │
│    │              │ Periodic checkpoint (10s) │                               │
│    │              ▼                          ▼                               │
│    │       ┌─────────────────────────────────────┐                          │
│    │       │           Redis                     │                          │
│    │       │  pipeline:{id}:checkpoint           │                          │
│    │       │  {                                  │                          │
│    │       │    "kafka_offsets": {               │                          │
│    └──────▶│      "partition_0": 12345,          │                          │
│            │      "partition_1": 67890           │                          │
│            │    },                               │                          │
│            │    "processed_count": 1000000,      │                          │
│            │    "timestamp": "2025-01-01T12:00"  │                          │
│            │  }                                  │                          │
│            └─────────────────────────────────────┘                          │
│                                                                              │
│  ─────────────────────────────────────────────────────────────────────────  │
│                                                                              │
│  [Fault Recovery]                                                            │
│                                                                              │
│  1. Agent or Actor restart                                                   │
│  2. Query checkpoint from Redis                                              │
│  3. Seek Kafka Consumer to saved offset                                      │
│  4. Resume processing from that point                                        │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Kafka Consumer                                                      │    │
│  │  consumer.Seek(partition_0, 12345)  // Last checkpoint              │    │
│  │  consumer.Seek(partition_1, 67890)                                   │    │
│  │  // Resume processing after offset 12345, 67890                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Failure Policy Configuration

```yaml
# Workflow failure policy
failure_policy:
  action: retry            # stop_all | continue | retry | skip
  max_retries: 5           # Max retries
  retry_delay: "1m"        # Retry delay
  notify_on_failure: true
  notify_channels:
    - slack
    - email

# Pipeline-level Kafka settings
pipelines:
  - id: "kafka-pipeline"
    input:                   # Data source (source also supported)
      type: kafka
      config:
        brokers: ["kafka1:9092", "kafka2:9092", "kafka3:9092"]
        topics: ["events"]
        group_id: "pipeline-consumer"
        auto_offset_reset: earliest
        enable_auto_commit: false  # Manual commit (checkpoint integration)
        session_timeout_ms: 30000
        heartbeat_interval_ms: 10000
```

---

### Complete Fault Recovery Scenarios

#### Scenario 1: Agent Process Crash

```
1. Agent process terminates unexpectedly
2. Control Plane detects heartbeat timeout (30s)
3. Change Agent status to "offline"
4. Reassign pipeline to another available Agent
5. New Agent queries checkpoint from Redis
6. Restart pipeline from last checkpoint
7. Kafka offset recovery → No data loss
```

#### Scenario 2: Redis + Kafka Simultaneous Failure

```
1. Redis failure occurs → Agent switches to REST fallback mode
2. Kafka failure occurs → Source restart attempts
3. Source reaches max_restarts → Pipeline error state
4. Pipeline transitions to paused state
5. Preserve unsent data in local buffer
6. Redis recovery → Checkpoint query available
7. Kafka recovery → Restart from checkpoint offset
8. Process buffer data + Kafka data normally
```

#### Scenario 3: Network Partition (Split-Brain)

```
1. Control Plane ↔ Agent network separation
2. Agent continues pipeline execution independently
3. Store local checkpoint (file if Redis unavailable)
4. Sync state with Control Plane on network recovery
5. Leader Election check to prevent duplicate execution
```

---

### Monitoring and Alerts

#### Redis Metrics

```go
type Metrics struct {
    TotalRequests        int64   // Total requests
    SuccessfulRequests   int64   // Successful requests
    FailedRequests       int64   // Failed requests
    CacheHits            int64   // Local cache hits
    CacheMisses          int64   // Local cache misses
    ReconnectAttempts    int64   // Reconnection attempts
    CircuitBreakerTrips  int64   // Circuit Breaker trips
    AverageLatencyMs     float64 // Average latency
}
```

#### Recommended Alert Settings

| Metric | Warning Threshold | Critical Threshold | Description |
|--------|------------------|-------------------|-------------|
| Redis Connection Status | Disconnected > 30s | Disconnected > 5min | Redis disconnection |
| Circuit Breaker | Enter Open state | Open > 5min | Persistent Redis failure |
| Kafka Consumer Lag | > 10,000 | > 100,000 | Processing delay |
| Pipeline Restart Count | > 3/5min | > 5/5min | Repeated failures |
| Checkpoint Failure | > 3 consecutive | > 10 consecutive | State save failure |

## License

Apache License 2.0
