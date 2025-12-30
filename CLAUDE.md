# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Conduix is an Actor Model-based scalable data pipeline platform that combines Bento's verified connectors with Apache Flink-style Actor Model. The project consists of four main modules:

- **control-plane**: Operations backend (Go + Gin + GORM + MySQL)
- **pipeline-core**: Pipeline execution engine (Go + Actor Model + Bento)
- **pipeline-agent**: Pipeline execution agent (Go + Gin)
- **web-ui**: Frontend (React 18 + TypeScript + Vite + Ant Design)
- **shared**: Shared types and utilities

## Common Commands

### Development Setup
```bash
make deps              # Install all dependencies
make infra-up          # Start MySQL and Redis (docker-compose)
make dev               # Start development environment
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
make test              # Run all tests
make test-coverage     # Run tests with coverage report

# Module-specific tests
cd control-plane && make test
cd pipeline-agent && make test
cd web-ui && make test
```

### Linting and Code Quality
```bash
make lint              # Run linters on all modules
make fmt               # Format code
make vet               # Run go vet
make check             # Run vet + lint + test

# Module-specific
cd control-plane && make lint
cd web-ui && make lint
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
cd web-ui && make dev
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
```

## Architecture

### Communication Flow
```
Control Plane (API Server)
        ↓
Redis Pub/Sub + REST API (fallback)
        ↓
Pipeline Agent Cluster → Pipeline Core (Actor System + Bento)
```

### Go Module Structure
All Go modules use local replace directives:
- `github.com/conduix/conduix/shared`
- `github.com/conduix/conduix/pipeline-core`

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

### Key Packages

**pipeline-core/pkg/**
- `actor/`: Actor system with Supervisor pattern
- `source/`: Data sources (Kafka, HTTP, file)
- `sink/`: Data sinks (Elasticsearch, S3, Kafka)
- `stream/`: Stream processing
- `config/`: YAML pipeline config parsing

**control-plane/internal/**
- `api/handlers/`: REST API handlers
- `api/middleware/`: Auth middleware
- `services/`: Business logic (scheduler, Redis)

**web-ui/src/**
- `pages/`: React pages
- `services/api.ts`: API client
- `store/`: Zustand state management
- `i18n/`: Internationalization (en, ko)

## Environment Variables

Key variables (see `.env.example`):
```
DB_HOST, DB_PORT (3307), DB_USER, DB_PASSWORD, DB_NAME
REDIS_ADDR, REDIS_PASSWORD
JWT_SECRET
GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET
GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
```

## User Roles

Defined in `config/users.yaml`:
- **admin**: Full access
- **operator**: Pipeline and agent management
- **viewer**: Read-only (default)

## Go Version

- Go 1.21+ required
- Control Plane uses Go 1.24.0
