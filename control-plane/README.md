# Control Plane

파이프라인 관리 및 운영 백엔드 API 서버

## 개요

`control-plane`은 Conduix 시스템의 중앙 관리 서버입니다. 파이프라인 설정, 에이전트 관리, 스케줄링, 모니터링 등 운영에 필요한 모든 기능을 REST API로 제공합니다.

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Control Plane 아키텍처                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                          API Layer (Gin)                             │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │    │
│  │  │  Pipeline │  │   Agent   │  │  Schedule │  │   Auth    │        │    │
│  │  │  Handler  │  │  Handler  │  │  Handler  │  │  Handler  │        │    │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┼──────────────────────────────────────┐    │
│  │                        Middleware                                    │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │    │
│  │  │    JWT    │  │   CORS    │  │   Rate    │  │  Logging  │        │    │
│  │  │   Auth    │  │           │  │  Limiter  │  │           │        │    │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │    │
│  └──────────────────────────────┼──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┼──────────────────────────────────────┐    │
│  │                        Services                                      │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │    │
│  │  │  Redis    │  │ Scheduler │  │   Auth    │  │  Metrics  │        │    │
│  │  │  Service  │  │  Service  │  │  Service  │  │  Service  │        │    │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │    │
│  └──────────────────────────────┼──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┴──────────────────────────────────────┐    │
│  │                        Data Layer                                    │    │
│  │  ┌─────────────────────┐        ┌─────────────────────┐             │    │
│  │  │   MySQL (GORM)      │        │   Redis             │             │    │
│  │  │   • Pipelines       │        │   • Commands        │             │    │
│  │  │   • Runs            │        │   • Heartbeats      │             │    │
│  │  │   • Schedules       │        │   • Metrics         │             │    │
│  │  │   • Users           │        │   • Checkpoints     │             │    │
│  │  └─────────────────────┘        └─────────────────────┘             │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 디렉토리 구조

```
control-plane/
├── cmd/
│   └── server/
│       └── main.go              # 서버 진입점
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── pipeline.go      # 파이프라인 CRUD, 제어
│   │   │   └── auth.go          # OAuth2/OIDC 인증
│   │   ├── middleware/
│   │   │   └── auth.go          # JWT, CORS, Role 미들웨어
│   │   └── routes.go            # 라우터 설정
│   └── services/
│       └── redis_service.go     # Redis 통신 서비스
├── pkg/
│   ├── database/
│   │   └── database.go          # MySQL 연결 및 마이그레이션
│   └── models/
│       └── models.go            # GORM 모델 정의
├── migrations/                   # DB 마이그레이션 파일
└── go.mod
```

## 핵심 기능

### 1. 파이프라인 관리

파이프라인의 전체 생명주기를 관리합니다.

```go
// Pipeline 모델
type Pipeline struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"not null"`
    Description string
    ConfigYAML  string    `gorm:"type:text"`
    CreatedBy   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

| 기능 | 설명 |
|-----|------|
| 생성 | YAML 설정으로 파이프라인 정의 |
| 조회 | 목록, 상세, 상태 조회 |
| 수정 | 설정 업데이트 |
| 삭제 | 파이프라인 삭제 |
| 시작/중지/일시중지 | 에이전트에 명령 전송 |

### 2. 에이전트 관리

분산된 에이전트들을 모니터링하고 관리합니다.

```go
// Agent 모델
type Agent struct {
    ID            string `gorm:"primaryKey"`
    Hostname      string
    IPAddress     string
    Status        string // online, offline, unknown
    LastHeartbeat *time.Time
    RegisteredAt  time.Time
}
```

- 하트비트 모니터링 (타임아웃: 30초)
- 자동 상태 업데이트
- 파이프라인 할당

### 3. 스케줄링

Cron 기반 파이프라인 스케줄링을 지원합니다.

```go
// Schedule 모델
type Schedule struct {
    ID             string `gorm:"primaryKey"`
    PipelineID     string
    CronExpression string
    Enabled        bool
    LastRunAt      *time.Time
    NextRunAt      *time.Time
}
```

```yaml
# Cron 표현식 예시
"0 * * * *"      # 매 시간
"0 0 * * *"      # 매일 자정
"0 0 * * 0"      # 매주 일요일
"0 0 1 * *"      # 매월 1일
```

### 4. 인증/인가

OAuth2 및 OIDC 기반 SSO를 지원합니다.

```go
// User 모델
type User struct {
    ID         string `gorm:"primaryKey"`
    Email      string `gorm:"unique"`
    Name       string
    Provider   string // oauth2, oidc
    ProviderID string
    Role       string // admin, operator, viewer
    CreatedAt  time.Time
}
```

| 역할 | 권한 |
|-----|------|
| `admin` | 모든 권한 |
| `operator` | 파이프라인 CRUD, 실행 제어 |
| `viewer` | 조회만 가능 |

### 5. Redis 서비스

에이전트 통신 및 실시간 데이터 관리를 담당합니다.

```go
// RedisService 주요 기능
type RedisService struct {
    // 에이전트 명령 전송
    SendCommandToAgent(agentID string, cmdType CommandType, ...) error

    // 하트비트 조회
    GetAgentHeartbeat(agentID string) (*AgentHeartbeat, error)

    // 체크포인트 관리
    SetPipelineCheckpoint(pipelineID string, checkpoint *Checkpoint) error
    GetPipelineCheckpoint(pipelineID string) (*Checkpoint, error)

    // 메트릭 관리
    SetPipelineMetrics(pipelineID string, metrics *Metrics) error
    GetPipelineMetrics(pipelineID string) (*Metrics, error)
}
```

## REST API

### 인증

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/v1/auth/providers` | SSO 제공자 목록 |
| GET | `/api/v1/auth/login/:provider` | SSO 로그인 시작 |
| GET | `/api/v1/auth/callback/:provider` | SSO 콜백 |
| POST | `/api/v1/auth/logout` | 로그아웃 |
| GET | `/api/v1/auth/me` | 현재 사용자 정보 |

### 파이프라인

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/v1/pipelines` | 파이프라인 목록 |
| POST | `/api/v1/pipelines` | 파이프라인 생성 |
| GET | `/api/v1/pipelines/:id` | 파이프라인 상세 |
| PUT | `/api/v1/pipelines/:id` | 파이프라인 수정 |
| DELETE | `/api/v1/pipelines/:id` | 파이프라인 삭제 |
| POST | `/api/v1/pipelines/:id/start` | 파이프라인 시작 |
| POST | `/api/v1/pipelines/:id/stop` | 파이프라인 중지 |
| POST | `/api/v1/pipelines/:id/pause` | 파이프라인 일시중지 |
| POST | `/api/v1/pipelines/:id/resume` | 파이프라인 재개 |
| GET | `/api/v1/pipelines/:id/status` | 파이프라인 상태 |
| GET | `/api/v1/pipelines/:id/history` | 실행 히스토리 |
| GET | `/api/v1/pipelines/:id/metrics` | 파이프라인 메트릭 |

### 에이전트

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/v1/agents` | 에이전트 목록 |
| GET | `/api/v1/agents/:id` | 에이전트 상세 |
| GET | `/api/v1/agents/:id/status` | 에이전트 상태 |
| POST | `/api/v1/agents/:id/heartbeat` | 하트비트 수신 (REST 폴백) |
| GET | `/api/v1/agents/:id/commands` | 대기 명령 조회 (REST 폴백) |

### 스케줄

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/v1/schedules` | 스케줄 목록 |
| POST | `/api/v1/schedules` | 스케줄 생성 |
| GET | `/api/v1/schedules/:id` | 스케줄 상세 |
| PUT | `/api/v1/schedules/:id` | 스케줄 수정 |
| DELETE | `/api/v1/schedules/:id` | 스케줄 삭제 |
| POST | `/api/v1/schedules/:id/enable` | 스케줄 활성화 |
| POST | `/api/v1/schedules/:id/disable` | 스케줄 비활성화 |

### 헬스체크

| Method | Path | 설명 |
|--------|------|------|
| GET | `/health` | 헬스체크 |
| GET | `/ready` | 준비 상태 |
| GET | `/metrics` | Prometheus 메트릭 |

## API 응답 형식

### 성공 응답

```json
{
  "success": true,
  "data": { ... },
  "message": "optional message"
}
```

### 에러 응답

```json
{
  "success": false,
  "error": "error message",
  "code": "ERROR_CODE"
}
```

### 페이징 응답

```json
{
  "success": true,
  "data": {
    "items": [ ... ],
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

## 설정

### 환경 변수

| 변수 | 설명 | 기본값 |
|-----|------|-------|
| `PORT` | 서버 포트 | `8080` |
| `DB_HOST` | MySQL 호스트 | `localhost` |
| `DB_PORT` | MySQL 포트 | `3306` |
| `DB_USER` | MySQL 사용자 | `root` |
| `DB_PASSWORD` | MySQL 비밀번호 | - |
| `DB_NAME` | 데이터베이스 이름 | `conduix` |
| `REDIS_HOST` | Redis 호스트 | `localhost` |
| `REDIS_PORT` | Redis 포트 | `6379` |
| `REDIS_PASSWORD` | Redis 비밀번호 | - |
| `JWT_SECRET` | JWT 서명 키 | - (필수) |
| `OAUTH2_CLIENT_ID` | OAuth2 클라이언트 ID | - |
| `OAUTH2_CLIENT_SECRET` | OAuth2 클라이언트 시크릿 | - |
| `OIDC_ISSUER_URL` | OIDC Issuer URL | - |

### 설정 파일 (config.yaml)

```yaml
server:
  port: 8080
  mode: release  # debug, release

database:
  host: localhost
  port: 3306
  user: vpuser
  password: vppassword
  name: conduix
  max_open_conns: 100
  max_idle_conns: 10

redis:
  host: localhost
  port: 6379
  password: ""
  db: 0

auth:
  jwt_secret: "your-secret-key"
  token_expiry: 24h

  oauth2:
    - provider: google
      client_id: "..."
      client_secret: "..."
      redirect_url: "http://localhost:8080/api/v1/auth/callback/google"

  oidc:
    - provider: keycloak
      issuer_url: "http://keycloak:8080/realms/myrealm"
      client_id: "..."
      client_secret: "..."

scheduler:
  enabled: true
  timezone: "Asia/Seoul"

logging:
  level: info
  format: json
```

## 데이터베이스 스키마

### 자동 마이그레이션

GORM의 AutoMigrate를 사용하여 스키마를 자동 관리합니다.

```go
func (db *DB) Migrate() error {
    return db.AutoMigrate(
        &models.Pipeline{},
        &models.PipelineRun{},
        &models.Schedule{},
        &models.User{},
        &models.Agent{},
        &models.Session{},
    )
}
```

### 주요 테이블

```sql
-- pipelines
CREATE TABLE pipelines (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    config_yaml TEXT NOT NULL,
    created_by VARCHAR(255),
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

-- pipeline_runs
CREATE TABLE pipeline_runs (
    id VARCHAR(36) PRIMARY KEY,
    pipeline_id VARCHAR(36) NOT NULL,
    agent_id VARCHAR(36),
    status VARCHAR(50) NOT NULL,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    processed_count BIGINT DEFAULT 0,
    error_count BIGINT DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMP
);

-- agents
CREATE TABLE agents (
    id VARCHAR(36) PRIMARY KEY,
    hostname VARCHAR(255) NOT NULL,
    ip_address VARCHAR(45),
    status VARCHAR(50) DEFAULT 'unknown',
    last_heartbeat TIMESTAMP,
    registered_at TIMESTAMP
);
```

## 빌드 및 실행

### 빌드

```bash
# 단독 빌드
cd control-plane
go build -o server ./cmd/server

# 전체 프로젝트에서 빌드
cd conduix
make build-control-plane
```

### 실행

```bash
# 기본 실행
./server

# 환경 변수로 설정
DB_HOST=localhost \
DB_USER=root \
REDIS_HOST=localhost \
JWT_SECRET=my-secret \
./server

# 마이그레이션 실행
./server --migrate

# 설정 파일 사용
./server --config config.yaml
```

### Docker 실행

```bash
docker run -d \
  --name control-plane \
  -p 8080:8080 \
  -e DB_HOST=mysql \
  -e REDIS_HOST=redis \
  -e JWT_SECRET=my-secret \
  conduix/control-plane:latest
```

## 의존성

```go
require (
    github.com/conduix/shared v0.0.0
    github.com/gin-gonic/gin v1.9.1
    github.com/redis/go-redis/v9 v9.3.0
    github.com/google/uuid v1.5.0
    gorm.io/gorm v1.25.5
    gorm.io/driver/mysql v1.5.2
    github.com/robfig/cron/v3 v3.0.1
    github.com/golang-jwt/jwt/v5 v5.2.0
    golang.org/x/oauth2 v0.15.0
    github.com/coreos/go-oidc/v3 v3.9.0
)
```

## 고가용성

### 수평 확장

Control Plane은 Stateless하게 설계되어 수평 확장이 가능합니다.

```yaml
# Kubernetes Deployment
replicas: 3

# Load Balancer 뒤에서 실행
# - 세션 상태: Redis에 저장
# - 스케줄러: Leader Election으로 단일 실행 보장
```

### Redis 장애 대응

```go
// RedisService는 장애 시 자동 대응
// - Circuit Breaker로 장애 격리
// - 명령 큐에 저장 후 복구 시 재전송
// - 202 Accepted 응답으로 클라이언트에 상태 전달
```

## 관련 문서

- [Pipeline Agent](../pipeline-agent/README.md)
- [Web UI](../web-ui/README.md)
- [장애 처리](../README.md#장애-처리-fault-tolerance)
