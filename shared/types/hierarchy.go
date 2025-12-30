package types

import "time"

// Workflow 워크플로우 - 워크플로우 단위로만 실행 제어됨
// 워크플로우 내 모든 파이프라인은 함께 시작/중지됨 (개별 제어 없음)
// 프로젝트 내에 realtime, batch 워크플로우 존재
type Workflow struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"` // 상위 Project ID
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Type          WorkflowType   `json:"type"`               // realtime 또는 batch (고정)
	ExecutionMode ExecutionMode  `json:"execution_mode"`     // parallel, sequential, dag
	Status        WorkflowStatus `json:"status"`             // 워크플로우 전체 상태
	Enabled       bool           `json:"enabled"`            // 워크플로우 활성화 여부 (수집 on/off)
	Schedule      *ScheduleConfig     `json:"schedule,omitempty"` // 배치용 스케줄
	Pipelines     []WorkflowPipeline  `json:"pipelines"`          // Pipelines in workflow (Config only, no individual control)
	FailurePolicy *FailurePolicy      `json:"failure_policy,omitempty"`
	Metadata      map[string]any      `json:"metadata,omitempty"`
	Tags          []string            `json:"tags,omitempty"`
	CreatedBy     string              `json:"created_by"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// WorkflowType 워크플로우 유형 (데이터 제공자당 2개만 존재)
type WorkflowType string

const (
	WorkflowTypeRealtime WorkflowType = "realtime" // 실시간 스트리밍 (Kafka, CDC, WebSocket 등)
	WorkflowTypeBatch    WorkflowType = "batch"    // 배치 처리 (SQL, REST API polling 등)
)

// ExecutionMode 실행 모드
type ExecutionMode string

const (
	ExecutionModeParallel   ExecutionMode = "parallel"   // 병렬 실행
	ExecutionModeSequential ExecutionMode = "sequential" // 순차 실행
	ExecutionModeDAG        ExecutionMode = "dag"        // DAG 기반 의존성 실행
)

// WorkflowStatus 워크플로우 상태
type WorkflowStatus string

const (
	WorkflowStatusIdle      WorkflowStatus = "idle"
	WorkflowStatusRunning   WorkflowStatus = "running"
	WorkflowStatusPaused    WorkflowStatus = "paused"
	WorkflowStatusStopped   WorkflowStatus = "stopped"
	WorkflowStatusError     WorkflowStatus = "error"
	WorkflowStatusCompleted WorkflowStatus = "completed" // 배치용
)

// ExpansionMode 자식 파이프라인 확장 모드
type ExpansionMode string

const (
	ExpansionModeNone          ExpansionMode = "none"            // 확장 없음, 단일 실행
	ExpansionModeForEachRecord ExpansionMode = "for_each_record" // 부모 출력 레코드마다 실행
)

// ParameterBinding 부모 파이프라인 출력을 자식 파이프라인 입력으로 매핑
type ParameterBinding struct {
	ParentField string `json:"parent_field"` // 부모 출력 필드 (예: "id", "board_id")
	ChildParam  string `json:"child_param"`  // 자식 파라미터 이름 (예: "board_id")
}

// WorkflowPipeline 워크플로우 내 파이프라인 정의
// 개별 파이프라인은 실행 제어 불가 - 워크플로우 단위로만 제어됨
type WorkflowPipeline struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Priority    int            `json:"priority"`             // 실행 우선순위 (낮을수록 먼저)
	DependsOn   []string       `json:"depends_on,omitempty"` // 의존하는 파이프라인 ID들
	Source      WorkflowSource `json:"source"`
	Stages      []Stage        `json:"stages,omitempty"` // 파이프라인 단계 (추상화 단위)
	Sinks       []WorkflowSink `json:"sinks"`
	Weight      int            `json:"weight,omitempty"` // 로드밸런싱 가중치

	// 계층형 파이프라인 필드
	ParentPipelineID  *string            `json:"parent_pipeline_id,omitempty"`  // 부모 파이프라인 ID
	TargetDataTypeID  *string            `json:"target_data_type_id,omitempty"` // 확장용 DataType ID (부모 출력 조회)
	ExpansionMode     ExpansionMode      `json:"expansion_mode,omitempty"`      // 자식 파이프라인 확장 모드
	ParameterBindings []ParameterBinding `json:"parameter_bindings,omitempty"`  // 부모→자식 파라미터 매핑
}

// WorkflowSource 워크플로우 내 소스 설정
type WorkflowSource struct {
	Type       string            `json:"type"`                 // kafka, cdc, rest_api, sql, file, sql_event
	Name       string            `json:"name"`                 // 소스 식별자
	Config     map[string]any    `json:"config"`               // 소스별 설정
	Partitions []PartitionConfig `json:"partitions,omitempty"` // 파티션 설정 (병렬 처리용)
}

// PartitionConfig 파티션 설정
type PartitionConfig struct {
	ID      string         `json:"id"`
	Name    string         `json:"name,omitempty"`
	Config  map[string]any `json:"config,omitempty"` // 파티션별 추가 설정
	Enabled bool           `json:"enabled"`
}

// Stage 파이프라인 단계 (추상화 단위)
// Stage는 input → output 인터페이스를 가지며, 구현에 따라 역할이 결정됨
// (filter, remap, aggregate, elasticsearch, kafka, trigger 등)
type Stage struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"` // filter, remap, sample, aggregate, elasticsearch, kafka, trigger, etc.
	Config map[string]any `json:"config"`
}

// WorkflowSink 워크플로우 내 싱크 설정
type WorkflowSink struct {
	Type      string         `json:"type"` // elasticsearch, kafka, sql, mongodb, s3, rest_api
	Name      string         `json:"name"`
	Config    map[string]any `json:"config"`
	Condition string         `json:"condition,omitempty"` // 조건부 라우팅
}

// ScheduleConfig 스케줄 설정
type ScheduleConfig struct {
	Type       ScheduleType `json:"type"` // cron, interval, event
	Cron       string       `json:"cron,omitempty"`
	Interval   string       `json:"interval,omitempty"` // 5m, 1h, 1d
	Timezone   string       `json:"timezone,omitempty"` // Asia/Seoul
	StartTime  *time.Time   `json:"start_time,omitempty"`
	EndTime    *time.Time   `json:"end_time,omitempty"`
	MaxRetries int          `json:"max_retries,omitempty"`
	Enabled    bool         `json:"enabled"`
}

// ScheduleType 스케줄 유형
type ScheduleType string

const (
	ScheduleTypeCron     ScheduleType = "cron"
	ScheduleTypeInterval ScheduleType = "interval"
	ScheduleTypeManual   ScheduleType = "manual"
	ScheduleTypeEvent    ScheduleType = "event" // 이벤트 기반 트리거
)

// FailurePolicy 실패 정책
type FailurePolicy struct {
	Action          FailureAction `json:"action"` // stop_all, continue, retry
	MaxRetries      int           `json:"max_retries"`
	RetryDelay      string        `json:"retry_delay"` // 1m, 5m
	NotifyOnFailure bool          `json:"notify_on_failure"`
	NotifyChannels  []string      `json:"notify_channels,omitempty"` // email, slack, webhook
}

// FailureAction 실패 시 행동
type FailureAction string

const (
	FailureActionStopAll  FailureAction = "stop_all" // 전체 중지
	FailureActionContinue FailureAction = "continue" // 다른 파이프라인 계속
	FailureActionRetry    FailureAction = "retry"    // 재시도
	FailureActionSkip     FailureAction = "skip"     // 건너뛰기
)

// WorkflowExecution 워크플로우 실행 이력
type WorkflowExecution struct {
	ID              string                    `json:"id"`
	WorkflowID      string                    `json:"workflow_id"`
	Status          WorkflowStatus            `json:"status"`
	StartedAt       time.Time                 `json:"started_at"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
	Duration        *time.Duration            `json:"duration,omitempty"`
	PipelineResults []PipelineExecutionResult `json:"pipeline_results"`
	TotalRecords    int64                     `json:"total_records"`
	FailedRecords   int64                     `json:"failed_records"`
	ErrorMessage    string                    `json:"error_message,omitempty"`
	TriggeredBy     string                    `json:"triggered_by"` // user, schedule, event
	Metadata        map[string]any            `json:"metadata,omitempty"`
}

// PipelineExecutionResult 개별 파이프라인 실행 결과
type PipelineExecutionResult struct {
	PipelineID       string              `json:"pipeline_id"`
	PipelineName     string              `json:"pipeline_name"`
	Status           string              `json:"status"`
	StartedAt        time.Time           `json:"started_at"`
	CompletedAt      time.Time           `json:"completed_at,omitempty"`
	RecordsRead      int64               `json:"records_read"`      // 수집량 (backward compat)
	RecordsWritten   int64               `json:"records_written"`   // 처리량 (backward compat)
	RecordsProcessed int64               `json:"records_processed"` // 처리량
	RecordsFailed    int64               `json:"records_failed"`    // 실패량
	ErrorCount       int64               `json:"error_count"`       // 총 에러 (backward compat)
	ErrorMessage     string              `json:"error_message,omitempty"`
	Offset           int64               `json:"offset,omitempty"`      // 실시간용 오프셋
	Checkpoint       map[string]any      `json:"checkpoint,omitempty"` // 체크포인트 (오프셋 포함 가능)
	Statistics       *PipelineStatistics `json:"statistics,omitempty"` // 상세 통계
}

// Permission 권한 설정
type Permission struct {
	ID           string             `json:"id"`
	ResourceType ResourceType       `json:"resource_type"` // project, workflow, pipeline
	ResourceID   string             `json:"resource_id"`
	UserID       string             `json:"user_id,omitempty"`
	RoleID       string             `json:"role_id,omitempty"`
	Actions      []PermissionAction `json:"actions"`
	CreatedAt    time.Time          `json:"created_at"`
}

// ResourceType 리소스 유형
type ResourceType string

const (
	ResourceTypeProject  ResourceType = "project"
	ResourceTypeWorkflow ResourceType = "workflow"
	ResourceTypePipeline ResourceType = "pipeline"
)

// PermissionAction 권한 액션
type PermissionAction string

const (
	PermissionActionRead    PermissionAction = "read"
	PermissionActionWrite   PermissionAction = "write"
	PermissionActionExecute PermissionAction = "execute"
	PermissionActionDelete  PermissionAction = "delete"
	PermissionActionAdmin   PermissionAction = "admin"
)

// Backward compatibility type aliases
type PipelineGroup = Workflow
type PipelineGroupType = WorkflowType
type PipelineGroupStatus = WorkflowStatus
type PipelineGroupExecution = WorkflowExecution
type GroupedPipeline = WorkflowPipeline
type GroupedSource = WorkflowSource
type GroupedSink = WorkflowSink

// Backward compatibility constants
const (
	PipelineGroupTypeRealtime = WorkflowTypeRealtime
	PipelineGroupTypeBatch    = WorkflowTypeBatch

	PipelineGroupStatusIdle      = WorkflowStatusIdle
	PipelineGroupStatusRunning   = WorkflowStatusRunning
	PipelineGroupStatusPaused    = WorkflowStatusPaused
	PipelineGroupStatusStopped   = WorkflowStatusStopped
	PipelineGroupStatusError     = WorkflowStatusError
	PipelineGroupStatusCompleted = WorkflowStatusCompleted
)
