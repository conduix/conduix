# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Conduix is a scalable data pipeline platform that combines Bento's verified connectors with parallel processing. The project consists of five main modules:

- **control-plane**: Operations backend (Go + Gin + GORM + MySQL)
- **pipeline-core**: Pipeline execution engine (Go + Bento connectors + parallel processing)
- **pipeline-worker**: Pipeline execution agent (Go + Gin)
- **web-ui**: Frontend (React 18 + TypeScript + Vite + MUI)
- **shared**: Shared types and utilities

### Pipeline Architecture
```
Input → [공통 Stage] → [Output별 PreStages] → Output
         (병렬 처리)    (Output 전용 변환)     (bulk/individual)
```

- **Input**: 데이터 소스 (Kafka, REST API, SQL 등)
- **Stage**: 공통 데이터 변환/처리 (filter, remap 등)
- **PreStages**: Output별 전용 변환 (각 Output마다 다른 변환 적용 가능)
- **Output**: 데이터 출력 대상 (Elasticsearch, S3, Kafka 등)

### Prerequisites

- Go 1.27+
- Node.js 20.19+ (web-ui build requires it)
- Docker & Docker Compose
- MySQL 8.0
- Redis 7.0

## Common Commands

### Development Setup
```bash
make deps              # Install all dependencies
make infra-up          # Start MySQL and Redis (docker-compose)
make dev               # Start infrastructure + show service commands
```

### Building
```bash
make build             # Build everything (Go + Web UI)
make build-go          # Build Go binaries only
make build-web         # Build Web UI only
make build-linux       # Build Linux binaries
```

### Testing
```bash
# All tests
make test              # Run all tests
make test-coverage     # Run tests with coverage report
make test-race         # Run tests with race detector

# Module-specific tests
cd control-plane && go test -v ./...
cd pipeline-worker && go test -v ./...
cd pipeline-core && go test -v ./...

# Single package test
cd control-plane && go test -v ./internal/api/handlers/...

# Single test function
cd control-plane && go test -v -run TestFunctionName ./internal/api/handlers/...

# Web UI tests
cd web-ui && npm run test
cd web-ui && npm run test -- --watch  # Watch mode
```

### Linting and Code Quality
```bash
make lint              # Run linters on all modules
make fmt               # Format code (gofumpt for Go, prettier for web)
make vet               # Run go vet
make check             # Run vet + lint + test
```

### Running Services
```bash
# Run each service in separate terminals
make run-control-plane  # API server on :8080
make run-agent          # Agent on :8081
make run-web            # Web UI dev server on :3000

# Or from module directories
cd control-plane && make run-local
cd pipeline-worker && make run-local
cd web-ui && npm run dev
```

### Docker
```bash
make docker-build      # Build all Docker images
make up                # Run with docker-compose
make down              # Stop docker-compose
docker-compose --profile with-kafka up -d  # Include Kafka
```

### Database
```bash
make migrate           # Run database migrations
cd control-plane && make run-migrate  # Run with migration flag
```

## Architecture

### Communication Flow
```
Control Plane (API Server :8080)
        ↓
Redis Pub/Sub + REST API (fallback)
        ↓
Pipeline Agent (:8081) → Pipeline Core (Actor System + Bento)
```

### Go Module Structure
All Go modules use local replace directives in go.mod:
```
shared/                 # Base module (no dependencies)
pipeline-core/          # Depends on: shared
pipeline-worker/         # Depends on: shared, pipeline-core
control-plane/          # Depends on: shared, pipeline-core
```

### Key Packages

**pipeline-core/pkg/**
- `source/`: Data inputs (Kafka, REST API, SQL, CDC, file, Kubernetes logs)
- `sink/`: Data outputs (Elasticsearch, S3, Kafka, SQL, MongoDB, REST API)
- `executor/`: Pipeline executor with parallel Stage processing and PreStages support
- `stream/`: Stream processing with Bento integration
- `config/`: YAML pipeline config parsing
- `checkpoint/`: State checkpoint management

**control-plane/internal/**
- `api/handlers/`: REST API handlers (pipeline, workflow, agent, auth)
- `api/middleware/`: Auth middleware (JWT, OAuth2)
- `services/`: Business logic (scheduler with cron, Redis pub/sub)

**web-ui/src/**
- `pages/`: React pages (Dashboard, PipelineDetail, WorkflowDetail)
- `services/api.ts`: API client with axios
- `store/`: Zustand state management
- `i18n/`: Internationalization (en, ko)

### Pipeline Configuration

**Workflow Pipeline structure**:
```yaml
pipelines:
  - id: "my-pipeline"
    name: "My Pipeline"
    input:                     # 데이터 소스 (source도 호환)
      type: kafka
      config:
        brokers: ["localhost:9092"]
        topics: ["events"]
    stages:                    # 공통 데이터 변환/처리 (병렬)
      - type: filter
        config: { condition: ".status == 'active'" }
      - type: remap
        config: { mappings: { "name": ".full_name" } }
    outputs:                   # 데이터 출력 (bulk/individual)
      - name: "elasticsearch"
        type: elasticsearch
        pre_stages:            # Output별 전용 변환
          - type: remap
            config: { mappings: { "@timestamp": ".created_at" } }
        config: { addresses: ["http://es:9200"], index: "events" }
      - name: "s3-backup"
        type: s3
        config: { bucket: "backup", path: "events/" }
    batch:                     # 배치 처리 설정
      enabled: true
      output_mode: bulk        # bulk 또는 individual
      size: 100
      workers: 20
```

**하위 호환성**: `source` 필드도 `input`과 동일하게 동작합니다.

**Input Types** (데이터 소스): kafka, rest_api, sql, sql_incremental(증분 폴링, 구 sql_event), cdc, file, k8s_logs

**Stage Types** (데이터 변환): filter, remap, drop, merge, split, encrypt, dedupe, default, cast, timestamp, throttle, validate, contract, route, delete

**Output Types** (데이터 출력): sql, elasticsearch, kafka, mongodb, s3, rest_api, file

**PreStages**: Output의 `pre_stages` 필드에 Stage 배열을 지정하여 Output별로 다른 변환을 적용할 수 있습니다.

## Environment Variables

Key variables (see `.env.example`):
```
DB_HOST, DB_PORT (3306/3307), DB_USER, DB_PASSWORD, DB_NAME
REDIS_ADDR, REDIS_PASSWORD
JWT_SECRET
CONTROL_PLANE_URL (for agent)
```

OAuth2 providers: GITHUB, GOOGLE, NAVER, KAKAO, GITLAB (each needs _CLIENT_ID and _CLIENT_SECRET)

## User Roles

Defined in `config/users.yaml`:
- **admin**: Full access
- **operator**: Pipeline and agent management
- **viewer**: Read-only (default)

## Pipeline Design Concepts

### Stage Abstraction

Stage is the core unit following `input → output` interface:
- **FilterStage**: Filter records by condition
- **RemapStage**: Transform/rename fields (Bloblang)
- **AggregateStage**: Aggregate over windows
- **EnrichStage**: Add external data (lookup join)
- **TriggerStage**: Trigger child pipelines

### DataType Dependency Patterns

1. **Different DataTypes (Hierarchical)**: Board → Post → Comment (parent-child collection)
2. **Same DataType (Fan-out)**: Single source to multiple processing paths via Kafka

### Router Stage Modes

For in-pipeline branching without Kafka:
- `fan_out`: Copy to all routes
- `condition`: First matching route
- `filter`: All matching routes

## Fault Tolerance

### Redis Resilience (ResilientClient)
- Auto-reconnect with exponential backoff
- Circuit breaker pattern
- Local cache fallback
- Auto-resubscribe for Pub/Sub

### Kafka Recovery (Actor Supervisor)
- Checkpoint-based offset recovery
- Supervision strategies: `one_for_one`, `one_for_all`
- Backoff on restart failure (1s → 2s → 4s → 8s → 16s)

### Communication Mode Fallback
```
ModeRedis (default) → ModeHybrid → ModeREST (fallback)
```
Agent automatically switches modes when Redis becomes unavailable.

## Local Kubernetes (Colima) Setup

### Prerequisites
- Colima with Kubernetes enabled
- ArgoCD installed in `argocd` namespace

### Start Colima
```bash
colima start --arch x86_64 --kubernetes
```

### Deploy Order (ArgoCD Applications)
```bash
# 1. Operators (infrastructure)
kubectl apply -f deploy/argocd/application-mysql-operator.yaml
kubectl apply -f deploy/argocd/application-kafka-strimzi.yaml

# 2. Clusters (databases/messaging)
kubectl apply -f deploy/argocd/application-mysql-cluster.yaml
kubectl apply -f deploy/argocd/kafka-cluster.yaml

# 3. Applications
kubectl apply -f deploy/argocd/application-sonarqube.yaml
kubectl apply -f deploy/argocd/application-kafka-ui.yaml
kubectl apply -f deploy/argocd/application-local-kafka.yaml
```

### Service Access URLs (Fixed NodePorts)

| Service | URL | Credentials |
|---------|-----|-------------|
| **Conduix Web UI** | http://localhost:30000 | - |
| **Conduix API** | http://localhost:30080 | - |
| **ArgoCD** | https://localhost:30443 | admin / `kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" \| base64 -d` |
| **SonarQube** | http://localhost:30900 | admin / admin |
| **Kafka UI** | http://localhost:30808 | - |

### Resource Management
Local Colima has limited resources. Keep replicas minimal:
- Control Plane: 1 replica
- Agent: 1 replica
- Web UI: 1 replica

### Troubleshooting
```bash
# Check all pods
kubectl get pods -A | grep -v Running

# Check ArgoCD sync status
kubectl get applications -n argocd

# Check resource usage
kubectl top nodes

# Scale down if CPU/memory issues
kubectl scale deployment <name> -n conduix --replicas=1
```
