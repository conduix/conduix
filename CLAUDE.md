# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Conduix is an Actor Model-based scalable data pipeline platform that combines Bento's verified connectors with Apache Flink-style Actor Model. The project consists of five main modules:

- **control-plane**: Operations backend (Go 1.24 + Gin + GORM + MySQL)
- **pipeline-core**: Pipeline execution engine (Go 1.21 + Actor Model + Bento)
- **pipeline-agent**: Pipeline execution agent (Go 1.21 + Gin)
- **web-ui**: Frontend (React 18 + TypeScript + Vite + Ant Design)
- **shared**: Shared types and utilities

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
cd pipeline-agent && go test -v ./...
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
cd pipeline-agent && make run-local
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
pipeline-agent/         # Depends on: shared, pipeline-core
control-plane/          # Depends on: shared, pipeline-core
```

### Key Packages

**pipeline-core/pkg/**
- `actor/`: Actor system with Supervisor pattern (one_for_one, one_for_all strategies)
- `source/`: Data sources (Kafka, HTTP, file, CDC, Kubernetes logs)
- `sink/`: Data sinks (Elasticsearch, S3, Kafka)
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

### Pipeline Configuration Types

**Flat structure** (Bento compatible):
```yaml
version: "1.0"
sources:
  kafka_input:
    type: kafka
transforms:
  parse:
    type: remap
sinks:
  elasticsearch:
    type: elasticsearch
```

**Hierarchical structure** (Actor model):
```yaml
version: "1.0"
type: actor
pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
  children:
    - name: "SourceSupervisor"
      type: supervisor
```

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
