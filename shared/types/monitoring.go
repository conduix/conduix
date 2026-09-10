package types

import "time"

// ExecutionMonitoringInfo 워크플로우 실행 모니터링 정보
type ExecutionMonitoringInfo struct {
	ExecutionID string `json:"execution_id"`
	WorkflowID  string `json:"workflow_id"`
	Status      string `json:"status"`
	// AgentID 는 이 실행을 위임 생성한 agent(노드)다. realtime(streaming) 은 종료 콜백이
	// 없어 결과 보고로 agent_id 를 남길 기회가 없으므로, 상시 흐르는 모니터링 경로에 실어
	// 보낸다. in-process 실행은 GroupExecutor 가 채우지 않으므로 비며, 이때는 agent 가
	// 자기 ID 로 채운다.
	AgentID string `json:"agent_id,omitempty"`
	// ErrorMessage 는 실행이 실패·정지한 사유다. 스스로 끝난 파이프라인은
	// statsCollectors 에서 제거되어 Pipelines 에 나타나지 않으므로, 사유가 여기 없으면
	// 화면에 "No monitoring data available" 만 남고 실패 이유를 알 방법이 없다(실측).
	ErrorMessage string                   `json:"error_message,omitempty"`
	Pipelines    []PipelineMonitoringInfo `json:"pipelines"`
	UpdatedAt    time.Time                `json:"updated_at"`
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
