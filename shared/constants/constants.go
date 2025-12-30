package constants

import "time"

// 버전 정보
const (
	Version   = "1.0.0"
	APIPrefix = "/api/v1"
)

// 기본 설정값
const (
	DefaultPageSize    = 20
	MaxPageSize        = 100
	DefaultTimeout     = 30 * time.Second
	HeartbeatInterval  = 10 * time.Second
	HeartbeatTimeout   = 30 * time.Second
	CheckpointInterval = 10 * time.Second
)

// Redis 키 접두사
const (
	RedisKeyPipelineState      = "pipeline:%s:state"
	RedisKeyPipelineCheckpoint = "pipeline:%s:checkpoint"
	RedisKeyPipelineMetrics    = "pipeline:%s:metrics"
	RedisKeyAgentHeartbeat     = "agent:%s:heartbeat"
	RedisKeyAgentPipelines     = "agent:%s:pipelines"
	RedisKeySession            = "session:%s"
)

// Redis Pub/Sub 채널
const (
	RedisChanAgentCommands = "agent:commands:%s"
	RedisChanPipelineEvent = "pipeline:events"
	RedisChanSystemEvent   = "system:events"
)

// 에러 코드
const (
	ErrCodeInvalidRequest     = "INVALID_REQUEST"
	ErrCodeUnauthorized       = "UNAUTHORIZED"
	ErrCodeForbidden          = "FORBIDDEN"
	ErrCodeNotFound           = "NOT_FOUND"
	ErrCodeConflict           = "CONFLICT"
	ErrCodeInternalError      = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeValidationFailed   = "VALIDATION_FAILED"
)

// HTTP 헤더
const (
	HeaderRequestID     = "X-Request-ID"
	HeaderAuthorization = "Authorization"
	HeaderContentType   = "Content-Type"
)

// Actor 시스템 설정
const (
	DefaultMailboxCapacity       = 10000
	DefaultDispatcherParallelism = 8
	DefaultMaxRestarts           = 3
	DefaultRestartWindow         = 60 * time.Second
)

// 파이프라인 설정
const (
	DefaultBufferMaxEvents = 5000
	DefaultBufferTimeout   = 10 * time.Second
)
