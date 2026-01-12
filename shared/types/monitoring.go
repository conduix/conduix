package types

import "time"

// ExecutionMonitoringInfo 워크플로우 실행 모니터링 정보
type ExecutionMonitoringInfo struct {
	ExecutionID string                   `json:"execution_id"`
	WorkflowID  string                   `json:"workflow_id"`
	Status      string                   `json:"status"`
	Pipelines   []PipelineMonitoringInfo `json:"pipelines"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// PipelineMonitoringInfo 파이프라인 모니터링 정보
type PipelineMonitoringInfo struct {
	PipelineID   string             `json:"pipeline_id"`
	PipelineName string             `json:"pipeline_name"`
	Status       string             `json:"status"`
	Checkpoints  []CheckpointInfo   `json:"checkpoints"`
	Stages       []StageMonitorInfo `json:"stages"`
	Statistics   *MonitoringStats   `json:"statistics,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// CheckpointInfo 체크포인트 정보
type CheckpointInfo struct {
	PartitionKey string `json:"partition_key"`
	OffsetValue  string `json:"offset_value"`
	OffsetType   string `json:"offset_type"` // timestamp, numeric, string
	RecordCount  int64  `json:"record_count"`
}

// StageMonitorInfo Stage 모니터링 정보
type StageMonitorInfo struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	InputCount  int64        `json:"input_count"`
	OutputCount int64        `json:"output_count"`
	ErrorCount  int64        `json:"error_count"`
	Samples     []DataSample `json:"samples,omitempty"`
}

// DataSample 데이터 샘플
type DataSample struct {
	Data      map[string]any `json:"data"`
	Timestamp int64          `json:"timestamp"` // Unix milliseconds
}

// MonitoringStats 모니터링 통계
type MonitoringStats struct {
	RecordsCollected int64 `json:"records_collected"`
	RecordsProcessed int64 `json:"records_processed"`
	CollectionErrors int64 `json:"collection_errors"`
	ProcessingErrors int64 `json:"processing_errors"`
}
