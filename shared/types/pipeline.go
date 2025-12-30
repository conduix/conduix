package types

import "time"

// PipelineType 파이프라인 타입
type PipelineType string

const (
	PipelineTypeFlat   PipelineType = "flat"   // Vector 호환 flat 구조
	PipelineTypeActor  PipelineType = "actor"  // 계층적 Actor 구조 (분산 처리용)
	PipelineTypeStream PipelineType = "stream" // 스트림 처리 (로컬 최적화)
)

// PipelineStatus 파이프라인 상태
type PipelineStatus string

const (
	PipelineStatusPending   PipelineStatus = "pending"
	PipelineStatusRunning   PipelineStatus = "running"
	PipelineStatusPaused    PipelineStatus = "paused"
	PipelineStatusCompleted PipelineStatus = "completed"
	PipelineStatusFailed    PipelineStatus = "failed"
	PipelineStatusStopped   PipelineStatus = "stopped"
)

// Pipeline 파이프라인 정의
type Pipeline struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Type        PipelineType `json:"type"`
	ConfigYAML  string       `json:"config_yaml"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// PipelineRun 파이프라인 실행 기록
type PipelineRun struct {
	ID             string         `json:"id"`
	PipelineID     string         `json:"pipeline_id"`
	Status         PipelineStatus `json:"status"`
	AgentID        string         `json:"agent_id,omitempty"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	ProcessedCount int64          `json:"processed_count"`
	ErrorCount     int64          `json:"error_count"`
	ErrorMessage   string         `json:"error_message,omitempty"`
}

// PipelineMetrics 파이프라인 메트릭
type PipelineMetrics struct {
	PipelineID       string    `json:"pipeline_id"`
	EventsIn         int64     `json:"events_in"`
	EventsOut        int64     `json:"events_out"`
	BytesIn          int64     `json:"bytes_in"`
	BytesOut         int64     `json:"bytes_out"`
	ErrorsTotal      int64     `json:"errors_total"`
	ProcessingTimeMs int64     `json:"processing_time_ms"`
	LastUpdated      time.Time `json:"last_updated"`
}

// Schedule 스케줄 정의
type Schedule struct {
	ID             string     `json:"id"`
	PipelineID     string     `json:"pipeline_id"`
	CronExpression string     `json:"cron_expression"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
