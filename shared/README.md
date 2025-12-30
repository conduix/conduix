# Shared

공유 타입, 상수 및 유틸리티 라이브러리

## 개요

`shared`는 Conduix 시스템의 모든 컴포넌트에서 공통으로 사용하는 타입 정의, 상수, 유틸리티를 제공합니다.

## 디렉토리 구조

```
shared/
├── types/
│   ├── pipeline.go      # 파이프라인 관련 타입
│   ├── agent.go         # 에이전트 관련 타입
│   ├── user.go          # 사용자/인증 관련 타입
│   ├── checkpoint.go    # 체크포인트 관련 타입
│   ├── actor.go         # Actor 시스템 관련 타입
│   └── api.go           # API 응답 타입
├── constants/
│   └── constants.go     # 공통 상수
├── redis/
│   └── resilient_client.go  # Redis 장애 복구 클라이언트
└── go.mod
```

## 타입 정의

### Pipeline 타입

```go
// types/pipeline.go

// PipelineStatus 파이프라인 상태
type PipelineStatus string

const (
    PipelineStatusPending   PipelineStatus = "pending"
    PipelineStatusRunning   PipelineStatus = "running"
    PipelineStatusPaused    PipelineStatus = "paused"
    PipelineStatusStopped   PipelineStatus = "stopped"
    PipelineStatusFailed    PipelineStatus = "failed"
    PipelineStatusCompleted PipelineStatus = "completed"
)

// Pipeline 파이프라인 정의
type Pipeline struct {
    ID          string         `json:"id"`
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    ConfigYAML  string         `json:"config_yaml"`
    Status      PipelineStatus `json:"status"`
    CreatedBy   string         `json:"created_by,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
}

// PipelineRun 파이프라인 실행 기록
type PipelineRun struct {
    ID             string         `json:"id"`
    PipelineID     string         `json:"pipeline_id"`
    AgentID        string         `json:"agent_id,omitempty"`
    Status         PipelineStatus `json:"status"`
    StartedAt      *time.Time     `json:"started_at,omitempty"`
    EndedAt        *time.Time     `json:"ended_at,omitempty"`
    ProcessedCount int64          `json:"processed_count"`
    ErrorCount     int64          `json:"error_count"`
    ErrorMessage   string         `json:"error_message,omitempty"`
}

// PipelineMetrics 파이프라인 메트릭
type PipelineMetrics struct {
    PipelineID       string    `json:"pipeline_id"`
    EventsProcessed  int64     `json:"events_processed"`
    EventsPerSecond  float64   `json:"events_per_second"`
    BytesProcessed   int64     `json:"bytes_processed"`
    ErrorsCount      int64     `json:"errors_count"`
    AverageLatencyMs float64   `json:"average_latency_ms"`
    LastUpdated      time.Time `json:"last_updated"`
}

// CreatePipelineRequest 파이프라인 생성 요청
type CreatePipelineRequest struct {
    Name        string `json:"name" binding:"required"`
    Description string `json:"description"`
    ConfigYAML  string `json:"config_yaml" binding:"required"`
}

// UpdatePipelineRequest 파이프라인 수정 요청
type UpdatePipelineRequest struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    ConfigYAML  string `json:"config_yaml"`
}
```

### Agent 타입

```go
// types/agent.go

// AgentStatus 에이전트 상태
type AgentStatus string

const (
    AgentStatusOnline  AgentStatus = "online"
    AgentStatusOffline AgentStatus = "offline"
    AgentStatusUnknown AgentStatus = "unknown"
)

// Agent 에이전트 정의
type Agent struct {
    ID            string      `json:"id"`
    Hostname      string      `json:"hostname"`
    IPAddress     string      `json:"ip_address,omitempty"`
    Status        AgentStatus `json:"status"`
    LastHeartbeat *time.Time  `json:"last_heartbeat,omitempty"`
    RegisteredAt  time.Time   `json:"registered_at"`
    Version       string      `json:"version,omitempty"`
    Labels        []string    `json:"labels,omitempty"`
}

// AgentHeartbeat 에이전트 하트비트
type AgentHeartbeat struct {
    AgentID       string              `json:"agent_id"`
    Timestamp     time.Time           `json:"timestamp"`
    CPUUsage      float64             `json:"cpu_usage"`
    MemoryUsage   float64             `json:"memory_usage"`
    DiskUsage     float64             `json:"disk_usage"`
    Pipelines     []string            `json:"pipelines"`
    PipelineStats []PipelineStatShort `json:"pipeline_stats,omitempty"`
}

// PipelineStatShort 간략한 파이프라인 통계
type PipelineStatShort struct {
    PipelineID     string         `json:"pipeline_id"`
    Status         PipelineStatus `json:"status"`
    ProcessedCount int64          `json:"processed_count"`
    ErrorCount     int64          `json:"error_count"`
}

// AgentCommand 에이전트 명령
type AgentCommand struct {
    ID         string      `json:"id"`
    Type       CommandType `json:"type"`
    PipelineID string      `json:"pipeline_id,omitempty"`
    Payload    any         `json:"payload,omitempty"`
    Timestamp  time.Time   `json:"timestamp"`
}

// CommandType 명령 타입
type CommandType string

const (
    CommandStartPipeline  CommandType = "start_pipeline"
    CommandStopPipeline   CommandType = "stop_pipeline"
    CommandPausePipeline  CommandType = "pause_pipeline"
    CommandResumePipeline CommandType = "resume_pipeline"
    CommandUpdateConfig   CommandType = "update_config"
    CommandShutdown       CommandType = "shutdown"
)

// AgentCommandResponse 명령 응답
type AgentCommandResponse struct {
    CommandID string `json:"command_id"`
    Success   bool   `json:"success"`
    Message   string `json:"message,omitempty"`
    Error     string `json:"error,omitempty"`
}
```

### User 타입

```go
// types/user.go

// UserRole 사용자 역할
type UserRole string

const (
    RoleAdmin    UserRole = "admin"
    RoleOperator UserRole = "operator"
    RoleViewer   UserRole = "viewer"
)

// User 사용자 정의
type User struct {
    ID         string    `json:"id"`
    Email      string    `json:"email"`
    Name       string    `json:"name"`
    Provider   string    `json:"provider,omitempty"`
    ProviderID string    `json:"provider_id,omitempty"`
    Role       UserRole  `json:"role"`
    CreatedAt  time.Time `json:"created_at"`
}

// AuthToken 인증 토큰
type AuthToken struct {
    AccessToken  string    `json:"access_token"`
    TokenType    string    `json:"token_type"`
    ExpiresAt    time.Time `json:"expires_at"`
    RefreshToken string    `json:"refresh_token,omitempty"`
}

// AuthSession 인증 세션
type AuthSession struct {
    UserID    string    `json:"user_id"`
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
    CreatedAt time.Time `json:"created_at"`
}
```

### Checkpoint 타입

```go
// types/checkpoint.go

// Checkpoint 체크포인트 데이터
type Checkpoint struct {
    PipelineID     string                 `json:"pipeline_id"`
    Timestamp      time.Time              `json:"timestamp"`
    KafkaOffsets   map[string]KafkaOffset `json:"kafka_offsets,omitempty"`
    ProcessedCount int64                  `json:"processed_count"`
    State          map[string]any         `json:"state,omitempty"`
}

// KafkaOffset Kafka 오프셋 정보
type KafkaOffset struct {
    Topic     string `json:"topic"`
    Partition int32  `json:"partition"`
    Offset    int64  `json:"offset"`
}
```

### Actor 타입

```go
// types/actor.go

// ActorType Actor 유형
type ActorType string

const (
    ActorTypeSource     ActorType = "source"
    ActorTypeTransform  ActorType = "transform"
    ActorTypeSink       ActorType = "sink"
    ActorTypeRouter     ActorType = "router"
    ActorTypeSupervisor ActorType = "supervisor"
)

// SupervisionStrategy Supervision 전략
type SupervisionStrategy string

const (
    StrategyOneForOne  SupervisionStrategy = "one_for_one"
    StrategyOneForAll  SupervisionStrategy = "one_for_all"
    StrategyRestForOne SupervisionStrategy = "rest_for_one"
)

// MailboxConfig Mailbox 설정
type MailboxConfig struct {
    Capacity         int    `json:"capacity"`
    OverflowStrategy string `json:"overflow_strategy"` // backpressure, drop_oldest, drop_newest
}

// DispatcherConfig Dispatcher 설정
type DispatcherConfig struct {
    Type        string `json:"type"` // fork-join, thread-pool
    Parallelism int    `json:"parallelism"`
}

// SupervisionConfig Supervision 설정
type SupervisionConfig struct {
    Strategy      SupervisionStrategy `json:"strategy"`
    MaxRestarts   int                 `json:"max_restarts"`
    WithinSeconds int                 `json:"within_seconds"`
}
```

### API 타입

```go
// types/api.go

// APIResponse API 응답 래퍼
type APIResponse[T any] struct {
    Success bool   `json:"success"`
    Data    T      `json:"data,omitempty"`
    Error   string `json:"error,omitempty"`
    Message string `json:"message,omitempty"`
}

// PaginatedResponse 페이징 응답
type PaginatedResponse[T any] struct {
    Items      []T   `json:"items"`
    Total      int64 `json:"total"`
    Page       int   `json:"page"`
    PageSize   int   `json:"page_size"`
    TotalPages int   `json:"total_pages"`
}
```

## 상수 정의

```go
// constants/constants.go

// Redis 키 접두사
const (
    RedisKeyPipelineCheckpoint = "pipeline:%s:checkpoint"
    RedisKeyPipelineMetrics    = "pipeline:%s:metrics"
    RedisKeyPipelineState      = "pipeline:%s:state"
    RedisKeyAgentHeartbeat     = "agent:%s:heartbeat"
    RedisKeyAgentPipelines     = "agent:%s:pipelines"
    RedisChannelAgentCommands  = "agent:commands:%s"
)

// 기본값
const (
    DefaultHeartbeatInterval = 10 * time.Second
    DefaultHeartbeatTimeout  = 30 * time.Second
    DefaultCheckpointInterval = 10 * time.Second
    DefaultCommandPollInterval = 5 * time.Second
)

// 에러 코드
const (
    ErrCodeUnauthorized     = "UNAUTHORIZED"
    ErrCodeForbidden        = "FORBIDDEN"
    ErrCodeNotFound         = "NOT_FOUND"
    ErrCodeInvalidRequest   = "INVALID_REQUEST"
    ErrCodeInternalError    = "INTERNAL_ERROR"
    ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)
```

## Redis Resilient Client

장애 복구 기능이 내장된 Redis 클라이언트입니다.

### 주요 기능

| 기능 | 설명 |
|-----|------|
| Auto-Reconnect | Exponential Backoff로 자동 재연결 |
| Circuit Breaker | 연속 실패 시 요청 차단 |
| Local Cache | 읽기 작업 폴백 |
| Auto-Resubscribe | Pub/Sub 자동 재구독 |
| Metrics | 성공/실패율, 지연시간 측정 |

### 사용 예시

```go
import "github.com/conduix/shared/redis"

// 클라이언트 생성
config := redis.DefaultConfig("localhost:6379")
config.Password = "secret"
config.OnStateChange = func(old, new redis.ConnectionState) {
    log.Printf("Redis state: %s -> %s", old, new)
}

client, err := redis.NewResilientClient(config)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// 값 저장
err = client.Set(ctx, "key", "value", time.Hour)

// 값 조회 (Redis 실패 시 로컬 캐시에서 조회)
value, err := client.Get(ctx, "key")

// Pub/Sub 구독 (자동 재구독)
client.Subscribe(ctx, "channel", func(msg string) {
    log.Printf("Received: %s", msg)
})

// 메시지 발행
client.Publish(ctx, "channel", "message")

// 메트릭 조회
metrics := client.GetMetrics()
log.Printf("Success rate: %.2f%%",
    float64(metrics.SuccessfulRequests)/float64(metrics.TotalRequests)*100)
```

### 설정 옵션

```go
type Config struct {
    Addr     string
    Password string
    DB       int

    // 재연결 설정
    MaxRetries        int           // 최대 재시도 (0 = 무한)
    InitialBackoff    time.Duration // 초기 백오프
    MaxBackoff        time.Duration // 최대 백오프
    BackoffMultiplier float64       // 백오프 배수

    // Circuit Breaker 설정
    FailureThreshold int           // Open 임계값
    SuccessThreshold int           // Close 임계값
    OpenTimeout      time.Duration // Open 유지 시간

    // 로컬 캐시 설정
    EnableLocalCache  bool
    LocalCacheTTL     time.Duration
    LocalCacheMaxSize int

    // 콜백
    OnStateChange func(old, new ConnectionState)
    OnError       func(err error)
}
```

## 사용 방법

### 다른 모듈에서 import

```go
import (
    "github.com/conduix/shared/types"
    "github.com/conduix/shared/constants"
    "github.com/conduix/shared/redis"
)

// 타입 사용
pipeline := types.Pipeline{
    ID:     "pipeline-001",
    Name:   "my-pipeline",
    Status: types.PipelineStatusRunning,
}

// 상수 사용
key := fmt.Sprintf(constants.RedisKeyPipelineCheckpoint, pipeline.ID)

// Redis 클라이언트 사용
client, _ := redis.NewResilientClient(redis.DefaultConfig("localhost:6379"))
```

### go.mod에서 참조

```go
// pipeline-core/go.mod
require (
    github.com/conduix/shared v0.0.0
)

replace github.com/conduix/shared => ../shared
```

## 의존성

```go
require (
    github.com/redis/go-redis/v9 v9.3.0
)
```

## 관련 문서

- [Pipeline Core](../pipeline-core/README.md)
- [Pipeline Agent](../pipeline-agent/README.md)
- [Control Plane](../control-plane/README.md)
