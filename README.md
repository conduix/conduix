<p align="center">
  <img src="images/logo-with-title.png" alt="Conduix Logo" width="600">
</p>

**Conduix** = **Intelligent Conduit**

A platform service that connects data, controls flow, and orchestrates pipelines.

Actor Model-based Large-scale Data Pipeline Platform

[한국어](README.ko.md)

## Overview

Conduix is a scalable data pipeline system that combines [Bento](https://github.com/warpstreamlabs/bento) (MIT License) proven connectors with Apache Flink-style Actor Model.

**Hybrid Architecture**:
- **Actor System**: Flink-style control with Supervisor pattern, Mailbox, Backpressure
- **Bento Connectors**: Reuse proven connectors for Kafka, Elasticsearch, S3, etc.
- **Pure Go**: Single binary, no external dependencies

## Key Features

- **Actor Model-based Pipeline**: Automatic fault recovery with hierarchical Supervisor pattern
- **Flat/Hierarchical Structure Support**: Both simple flat structure and advanced hierarchical Actor structure
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
│  │  Agent (Actor System + Bento Connectors)          │      │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐           │      │
│  │  │ Source  │→ │Transform│→ │  Sink   │           │      │
│  │  │ (Kafka) │  │(Bloblang│  │  (ES)   │           │      │
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
│     ┌────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌──────┐    │
│     │ Source │───→│ Stage 1 │───→│ Stage 2 │───→│ Stage 3 │───→│ Sink │    │
│     └────────┘    └─────────┘    └─────────┘    └─────────┘    └──────┘    │
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

### Stage: The Abstraction Unit

**Stage** is the core abstraction unit following `input → output` interface. The implementation determines its role:

| Stage Type | Description | Example Use |
|------------|-------------|-------------|
| **FilterStage** | Filter records by condition | Remove invalid data |
| **RemapStage** | Transform/rename fields | JSON field mapping |
| **AggregateStage** | Aggregate over windows | Count, sum, average |
| **EnrichStage** | Add external data | Lookup table join |
| **ElasticsearchStage** | Write to Elasticsearch | Index documents |
| **KafkaStage** | Produce to Kafka | Cross-pipeline boundary |
| **TriggerStage** | Trigger other pipelines | Parent-child coordination |

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

### Router Stage (Optional In-Pipeline Branching)

For simple fan-out within a single pipeline (without Kafka):

```yaml
stages:
  - id: router-1
    type: router
    config:
      mode: fan_out          # fan_out | condition | filter
      routes:
        - name: es-path
          next: stage-es
        - name: agg-path
          next: stage-agg

  - id: stage-es
    type: elasticsearch
    config: {...}

  - id: stage-agg
    type: aggregate
    config: {...}
```

| Router Mode | Description | Use Case |
|-------------|-------------|----------|
| **fan_out** | Copy to all routes | Same data to ES + DB |
| **condition** | First matching route | Error/success branching |
| **filter** | All matching routes | Tag-based routing |

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
├── pipeline-core/     # Pipeline core (Actor system, Bento integration)
├── pipeline-agent/    # Pipeline execution agent
├── control-plane/     # Operations tool backend API
├── web-ui/            # Operations tool frontend
├── shared/            # Shared types/constants
└── deploy/            # Deployment (Docker, Helm, scripts)
```

## Documentation

- [Standalone Pipeline Execution Guide](docs/standalone-usage.md) - Run independently without operations tool
- [Fault Handling Scenarios](#fault-tolerance) - Redis/Kafka fault handling

## Quick Start

### Prerequisites

- Go 1.21+
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

### Flat Structure (Bento Compatible)

```yaml
version: "1.0"
name: "log-pipeline"

sources:
  kafka_input:
    type: kafka
    brokers: ["kafka:9092"]
    topics: ["logs"]

transforms:
  parse:
    type: remap
    inputs: ["kafka_input"]
    source: '. = parse_json!(.message)'

sinks:
  elasticsearch:
    type: elasticsearch
    inputs: ["parse"]
    endpoints: ["http://es:9200"]
```

### Hierarchical Actor Structure

```yaml
version: "1.0"
name: "analytics-pipeline"
type: actor

pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
    max_restarts: 3

  children:
    - name: "SourceSupervisor"
      type: supervisor
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            brokers: ["kafka:9092"]
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

Kafka is used in Source (data collection) and Sink (data transmission).

#### Response by Fault Type

| Scenario | Source Actor Behavior | Sink Actor Behavior | Data Guarantee |
|----------|----------------------|---------------------|----------------|
| **Temporary Broker Disconnection** | Auto-reconnect, restart Consumer | Buffer then resend | At-least-once |
| **Broker Down** | Supervisor restarts Actor | Store in local buffer | At-least-once |
| **Partition Rebalance** | Offset adjustment, checkpoint recovery | Complete in-progress batch then reconnect | Exactly-once (with checkpoint) |
| **Leader Change** | Auto-detect new Leader | Auto-switch to new Leader | At-least-once |
| **Topic Deletion/Permission Error** | Error logging, report to Supervisor | Error logging, stop retries | Manual intervention required |

#### Kafka Source Actor Recovery Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Kafka Source Actor Fault Recovery Flow                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────┐     │
│  │                    SourceSupervisor                                 │     │
│  │                                                                     │     │
│  │   Strategy: one_for_one (restart only failed Actor)                │     │
│  │   max_restarts: 5 (max 5 times within 5 minutes)                   │     │
│  │                                                                     │     │
│  └────────────────────────────────────────────────────────────────────┘     │
│         │                    │                    │                          │
│         ▼                    ▼                    ▼                          │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐                    │
│  │ KafkaSource │     │ KafkaSource │     │ KafkaSource │                    │
│  │ Partition-0 │     │ Partition-1 │     │ Partition-2 │                    │
│  └──────┬──────┘     └─────────────┘     └─────────────┘                    │
│         │                                                                    │
│         │ Broker Connection Failed                                           │
│         ▼                                                                    │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ 1. Actor Crash → Notify Supervisor                                  │    │
│  │ 2. Supervisor checks restart policy                                 │    │
│  │ 3. Query last offset from checkpoint                                │    │
│  │ 4. Create new Actor → restart from offset                           │    │
│  │ 5. Backoff on restart failure (1s → 2s → 4s → 8s → 16s)            │    │
│  │ 6. Escalate to Supervisor when max_restarts exceeded                │    │
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
│  Kafka ──▶ Source Actor ──▶ Transform ──▶ Sink                             │
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

#### Supervision Strategy

```yaml
# Pipeline configuration example
pipeline:
  name: "KafkaPipeline"
  supervision:
    strategy: one_for_one    # Restart only failed Actor
    max_restarts: 5          # Max restarts within window
    within_seconds: 300      # Restart count window

  children:
    - name: "SourceSupervisor"
      type: supervisor
      supervision:
        strategy: one_for_one
        max_restarts: 10     # Allow more retries for Source
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            brokers: ["kafka1:9092", "kafka2:9092", "kafka3:9092"]
            topics: ["events"]
            group_id: "pipeline-consumer"
            # Kafka Consumer settings
            auto_offset_reset: earliest
            enable_auto_commit: false  # Manual commit (checkpoint integration)
            session_timeout_ms: 30000
            heartbeat_interval_ms: 10000
            max_poll_interval_ms: 300000
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
2. Kafka failure occurs → Source Actor restart attempts
3. Source Actor reaches max_restarts → Escalate to Supervisor
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
| Actor Restart Count | > 3/5min | > 5/5min | Repeated failures |
| Checkpoint Failure | > 3 consecutive | > 10 consecutive | State save failure |

## License

Apache License 2.0
