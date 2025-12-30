// Package types 파이프라인 통계 타입 정의
package types

import "time"

// PipelineStatistics 파이프라인 실행 통계
// 수집량, 처리량, Stage별 처리량, 에러 등을 추적
type PipelineStatistics struct {
	PipelineID       string           `json:"pipeline_id"`
	PipelineName     string           `json:"pipeline_name"`
	RecordsCollected int64            `json:"records_collected"`  // 수집량 - 소스에서 읽어온 레코드 수
	RecordsProcessed int64            `json:"records_processed"`  // 처리량 - 싱크로 전송된 레코드 수
	PerStageCounts   map[string]int64 `json:"per_stage_counts"`   // Stage별 처리량 - Stage별 통과 레코드 수
	CollectionErrors int64            `json:"collection_errors"`  // 수집에러 - 소스 읽기 중 에러
	ProcessingErrors int64            `json:"processing_errors"`  // 처리에러 - Stage/Sink 에러
	StartedAt        time.Time        `json:"started_at"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
	DurationMs       int64            `json:"duration_ms,omitempty"`
}

// StageStatistics 개별 Stage 통계
type StageStatistics struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	InputCount  int64  `json:"input_count"`  // Stage에 들어온 레코드 수
	OutputCount int64  `json:"output_count"` // Stage에서 나간 레코드 수
	ErrorCount  int64  `json:"error_count"`  // Stage 처리 중 에러 수
}

// WorkflowStatistics 워크플로우 집계 통계
// 워크플로우 내 모든 파이프라인의 통계를 집계
type WorkflowStatistics struct {
	WorkflowID            string               `json:"workflow_id"`
	WorkflowName          string               `json:"workflow_name"`
	WorkflowType          WorkflowType         `json:"workflow_type"` // batch or realtime
	TotalRecordsCollected int64                `json:"total_records_collected"`
	TotalRecordsProcessed int64                `json:"total_records_processed"`
	TotalCollectionErrors int64                `json:"total_collection_errors"`
	TotalProcessingErrors int64                `json:"total_processing_errors"`
	PipelineStats         []PipelineStatistics `json:"pipeline_stats,omitempty"`
	Period                StatsPeriod          `json:"period"`
	StartTime             time.Time            `json:"start_time"`
	EndTime               time.Time            `json:"end_time"`
}

// StatsPeriod 통계 집계 기간 유형
type StatsPeriod string

const (
	StatsPeriodExecution StatsPeriod = "execution" // 배치: 실행 단위
	StatsPeriodHourly    StatsPeriod = "hourly"    // 실시간: 시간 단위
	StatsPeriodDaily     StatsPeriod = "daily"     // 일 단위 요약
)

// HourlyStatsBucket 실시간 파이프라인 시간별 통계 버킷
type HourlyStatsBucket struct {
	ID               string           `json:"id"`
	PipelineID       string           `json:"pipeline_id"`
	WorkflowID       string           `json:"workflow_id"`
	BucketHour       time.Time        `json:"bucket_hour"` // 시간 경계 (truncated to hour)
	RecordsCollected int64            `json:"records_collected"`
	RecordsProcessed int64            `json:"records_processed"`
	PerStageCounts   map[string]int64 `json:"per_stage_counts"`
	CollectionErrors int64            `json:"collection_errors"`
	ProcessingErrors int64            `json:"processing_errors"`
	SampleCount      int              `json:"sample_count"` // 버킷에 포함된 샘플 수
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// StatsQuery 통계 조회 쿼리 파라미터
type StatsQuery struct {
	From     *time.Time `json:"from,omitempty"`
	To       *time.Time `json:"to,omitempty"`
	Type     string     `json:"type,omitempty"` // batch or realtime
	Limit    int        `json:"limit,omitempty"`
	Page     int        `json:"page,omitempty"`
	PageSize int        `json:"page_size,omitempty"`
}
