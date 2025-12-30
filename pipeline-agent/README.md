# Pipeline Agent

분산 파이프라인 실행 에이전트

## 개요

`pipeline-agent`는 Control Plane의 명령을 받아 데이터 파이프라인을 실행하는 에이전트입니다. 여러 대의 서버에 배포되어 파이프라인 클러스터를 구성합니다.

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Pipeline Agent 아키텍처                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                          ┌─────────────────────┐                            │
│                          │    Control Plane    │                            │
│                          └──────────┬──────────┘                            │
│                                     │                                        │
│                    ┌────────────────┼────────────────┐                      │
│                    │                │                │                      │
│                    ▼                ▼                ▼                      │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                           Agent Core                                 │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────────┐  │   │
│  │  │  Heartbeat  │  │   Command   │  │     Communication Mode      │  │   │
│  │  │   Manager   │  │   Handler   │  │  Redis ←→ REST Fallback    │  │   │
│  │  └─────────────┘  └─────────────┘  └─────────────────────────────┘  │   │
│  └──────────────────────────────┬──────────────────────────────────────┘   │
│                                 │                                           │
│                                 ▼                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Pipeline Instances                              │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│  │  │ Pipeline A  │  │ Pipeline B  │  │ Pipeline C  │                  │   │
│  │  │  [Running]  │  │  [Paused]   │  │  [Running]  │                  │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 디렉토리 구조

```
pipeline-agent/
├── cmd/
│   └── agent/
│       └── main.go              # CLI 진입점
├── internal/
│   └── agent/
│       └── agent.go             # Agent 코어 로직
├── api/
│   └── handlers.go              # REST API 핸들러
└── go.mod
```

## 핵심 기능

### 1. 통신 모드 관리

Agent는 세 가지 통신 모드를 지원하며, Redis 상태에 따라 자동 전환됩니다.

```go
type CommunicationMode int

const (
    ModeRedis  CommunicationMode = iota  // Redis Pub/Sub (기본)
    ModeREST                              // REST API 폴백
    ModeHybrid                            // Redis + REST 동시
)
```

```
┌─────────────────────────────────────────────────────────────────┐
│                    통신 모드 상태 전이                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   시작 ──▶ [ModeRedis] ──Redis 실패──▶ [ModeHybrid]             │
│                 ▲                            │                   │
│                 │                            │ Redis 계속 실패   │
│                 │                            ▼                   │
│            Redis 복구              ◀── [ModeREST]               │
│             (안정화)                                             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 2. 하트비트 관리

Agent 상태를 주기적으로 Control Plane에 보고합니다.

```go
type AgentHeartbeat struct {
    AgentID       string              `json:"agent_id"`
    Timestamp     time.Time           `json:"timestamp"`
    CPUUsage      float64             `json:"cpu_usage"`
    MemoryUsage   float64             `json:"memory_usage"`
    Pipelines     []string            `json:"pipelines"`
    PipelineStats []PipelineStatShort `json:"pipeline_stats"`
}
```

- **기본 간격**: 10초
- **전송 방식**: Redis 우선, 실패 시 REST 폴백
- **타임아웃**: 30초 (Control Plane에서 오프라인 판정)

### 3. 명령 처리

Control Plane으로부터 명령을 수신하여 처리합니다.

| 명령 | 설명 | 응답 |
|-----|------|-----|
| `start_pipeline` | 파이프라인 시작 | 성공/실패 |
| `stop_pipeline` | 파이프라인 중지 | 성공/실패 |
| `pause_pipeline` | 파이프라인 일시중지 | 성공/실패 |
| `resume_pipeline` | 파이프라인 재개 | 성공/실패 |
| `update_config` | 설정 업데이트 | 성공/실패 |
| `shutdown` | 에이전트 종료 | - |

### 4. 파이프라인 인스턴스 관리

```go
type PipelineInstance struct {
    ID        string
    Config    *config.PipelineConfig
    Runner    *pipeline.Runner
    Status    types.PipelineStatus
    StartTime time.Time
    StopTime  time.Time
}
```

## 설정

### 환경 변수

| 변수 | 설명 | 기본값 |
|-----|------|-------|
| `AGENT_ID` | 에이전트 고유 ID | 자동 생성 (UUID) |
| `CONTROL_PLANE_URL` | Control Plane URL | `http://localhost:8080` |
| `REDIS_HOST` | Redis 호스트 | `localhost` |
| `REDIS_PORT` | Redis 포트 | `6379` |
| `REDIS_PASSWORD` | Redis 비밀번호 | - |
| `HEARTBEAT_INTERVAL` | 하트비트 간격 | `10s` |
| `COMMAND_POLL_INTERVAL` | REST 폴링 간격 | `5s` |
| `ENABLE_REST_FALLBACK` | REST 폴백 활성화 | `true` |
| `CONFIG_DIR` | 파이프라인 설정 디렉토리 | `/etc/conduix/configs` |

### 설정 파일 (agent.yaml)

```yaml
agent:
  id: "agent-001"  # 생략 시 자동 생성
  labels:
    - "region=ap-northeast-2"
    - "zone=a"

control_plane:
  url: "http://control-plane:8080"

redis:
  host: "redis"
  port: 6379
  password: ""

communication:
  heartbeat_interval: 10s
  command_poll_interval: 5s
  enable_rest_fallback: true

logging:
  level: info
  format: json
```

## REST API

Agent는 로컬 상태 조회를 위한 REST API를 제공합니다.

### 엔드포인트

| Method | Path | 설명 |
|--------|------|------|
| GET | `/health` | 헬스체크 |
| GET | `/status` | 에이전트 상태 |
| GET | `/pipelines` | 파이프라인 목록 |
| GET | `/pipelines/:id` | 파이프라인 상세 |
| GET | `/metrics` | Prometheus 메트릭 |

### 응답 예시

```bash
# 에이전트 상태
curl http://localhost:8081/status
```

```json
{
  "agent_id": "agent-001",
  "hostname": "worker-1",
  "status": "online",
  "communication_mode": "redis",
  "redis_healthy": true,
  "pipelines_running": 3,
  "uptime": "24h12m30s"
}
```

```bash
# 파이프라인 목록
curl http://localhost:8081/pipelines
```

```json
{
  "pipelines": [
    {
      "id": "pipeline-001",
      "name": "log-processor",
      "status": "running",
      "started_at": "2025-01-01T00:00:00Z",
      "processed_count": 1234567,
      "error_count": 12
    }
  ]
}
```

## 빌드 및 실행

### 빌드

```bash
# 단독 빌드
cd pipeline-agent
go build -o agent ./cmd/agent

# 전체 프로젝트에서 빌드
cd conduix
make build-agent
```

### 실행

```bash
# 기본 실행
./agent

# 환경 변수로 설정
CONTROL_PLANE_URL=http://control-plane:8080 \
REDIS_HOST=redis \
./agent

# 설정 파일 사용
./agent --config agent.yaml
```

### Docker 실행

```bash
docker run -d \
  --name pipeline-agent \
  -e CONTROL_PLANE_URL=http://control-plane:8080 \
  -e REDIS_HOST=redis \
  conduix/agent:latest
```

## 장애 복구

### Redis 장애 시 동작

1. Redis 연결 실패 감지
2. `ModeHybrid`로 전환 (Redis 재연결 시도 + REST 병행)
3. Redis 계속 실패 시 `ModeREST`로 전환
4. REST 폴링으로 명령 수신 (5초 간격)
5. 하트비트는 REST API로 전송
6. Redis 복구 시 자동으로 `ModeRedis`로 복귀

### Control Plane 장애 시 동작

1. 기존 실행 중인 파이프라인 계속 실행
2. 로컬 체크포인트 저장 유지
3. 하트비트 전송 실패 로깅
4. Control Plane 복구 시 자동 재연결

### 파이프라인 장애 시 동작

1. Actor Supervisor가 장애 감지
2. 재시작 정책에 따라 Actor 재시작
3. 체크포인트에서 상태 복구
4. max_restarts 초과 시 파이프라인 일시중지
5. Control Plane에 상태 보고

## 메트릭

### Prometheus 메트릭

```
# 에이전트 메트릭
agent_uptime_seconds
agent_pipelines_total{status="running|paused|stopped"}
agent_heartbeat_success_total
agent_heartbeat_failed_total

# 통신 메트릭
agent_redis_connected
agent_communication_mode{mode="redis|rest|hybrid"}
agent_command_received_total{type="start|stop|pause|resume"}

# 파이프라인 메트릭
pipeline_events_processed_total{pipeline_id="..."}
pipeline_events_failed_total{pipeline_id="..."}
pipeline_processing_duration_seconds{pipeline_id="..."}
```

## 의존성

```go
require (
    github.com/conduix/shared v0.0.0
    github.com/conduix/pipeline-core v0.0.0
    github.com/gin-gonic/gin v1.9.1
    github.com/redis/go-redis/v9 v9.3.0
    github.com/google/uuid v1.5.0
)
```

## 관련 문서

- [Control Plane](../control-plane/README.md)
- [장애 처리](../README.md#장애-처리-fault-tolerance)
