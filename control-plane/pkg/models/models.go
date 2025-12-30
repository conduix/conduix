package models

import (
	"time"

	"gorm.io/gorm"
)

// Pipeline 파이프라인 모델
type Pipeline struct {
	ID          string `gorm:"primaryKey;size:36" json:"id"`
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	ConfigYAML  string `gorm:"type:text;not null" json:"config_yaml"`

	// 데이터 유형 참조 (삭제 전략 상속)
	DataTypeID string `gorm:"size:36;index" json:"data_type_id,omitempty"`

	CreatedBy string         `gorm:"size:255" json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	DataType *DataType `gorm:"foreignKey:DataTypeID" json:"data_type,omitempty"`
}

// TableName 테이블 이름
func (Pipeline) TableName() string {
	return "pipelines"
}

// PipelineRun 파이프라인 실행 기록
type PipelineRun struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	PipelineID     string     `gorm:"size:36;not null;index" json:"pipeline_id"`
	Status         string     `gorm:"size:50;not null" json:"status"`
	AgentID        string     `gorm:"size:36" json:"agent_id,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	ProcessedCount int64      `gorm:"default:0" json:"processed_count"`
	ErrorCount     int64      `gorm:"default:0" json:"error_count"`
	ErrorMessage   string     `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`

	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline,omitempty"`
}

// TableName 테이블 이름
func (PipelineRun) TableName() string {
	return "pipeline_runs"
}

// Schedule 스케줄 모델
type Schedule struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	PipelineID     string     `gorm:"size:36;not null;index" json:"pipeline_id"`
	CronExpression string     `gorm:"size:100;not null" json:"cron_expression"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline,omitempty"`
}

// TableName 테이블 이름
func (Schedule) TableName() string {
	return "schedules"
}

// User 사용자 모델
type User struct {
	ID         string     `gorm:"primaryKey;size:36" json:"id"`
	Email      string     `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Name       string     `gorm:"size:255" json:"name,omitempty"`
	Provider   string     `gorm:"size:50" json:"provider,omitempty"`
	ProviderID string     `gorm:"size:255" json:"provider_id,omitempty"`
	Role       string     `gorm:"size:50;default:viewer" json:"role"`
	AvatarURL  string     `gorm:"size:500" json:"avatar_url,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastLogin  *time.Time `json:"last_login,omitempty"`
}

// TableName 테이블 이름
func (User) TableName() string {
	return "users"
}

// Agent 에이전트 모델
type Agent struct {
	ID            string     `gorm:"primaryKey;size:36" json:"id"`
	Hostname      string     `gorm:"size:255;not null" json:"hostname"`
	IPAddress     string     `gorm:"size:45" json:"ip_address,omitempty"`
	Status        string     `gorm:"size:50;default:unknown" json:"status"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	RegisteredAt  time.Time  `json:"registered_at"`
	Version       string     `gorm:"size:50" json:"version,omitempty"`
	Labels        string     `gorm:"type:text" json:"labels,omitempty"` // JSON array
}

// TableName 테이블 이름
func (Agent) TableName() string {
	return "agents"
}

// Session 세션 모델
type Session struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`
	UserID    string    `gorm:"size:36;not null;index" json:"user_id"`
	Token     string    `gorm:"size:500;not null;uniqueIndex" json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName 테이블 이름
func (Session) TableName() string {
	return "sessions"
}

// AuditLog 감사 로그
type AuditLog struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	UserID     string    `gorm:"size:36;index" json:"user_id"`
	Action     string    `gorm:"size:100;not null" json:"action"`
	Resource   string    `gorm:"size:100" json:"resource"`
	ResourceID string    `gorm:"size:36" json:"resource_id"`
	Details    string    `gorm:"type:text" json:"details,omitempty"`
	IPAddress  string    `gorm:"size:45" json:"ip_address,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName 테이블 이름
func (AuditLog) TableName() string {
	return "audit_logs"
}

// ProvisioningRequest 사전작업 요청
type ProvisioningRequest struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`
	PipelineID  string    `gorm:"size:36;not null;index" json:"pipeline_id"`
	SinkType    string    `gorm:"size:50;not null" json:"sink_type"`
	SinkName    string    `gorm:"size:255;not null" json:"sink_name"`
	Type        string    `gorm:"size:50;not null" json:"type"`      // table_creation, topic_creation, external 등
	Config      string    `gorm:"type:text" json:"config,omitempty"` // JSON
	ExternalURL string    `gorm:"size:500" json:"external_url,omitempty"`
	CallbackURL string    `gorm:"size:500" json:"callback_url,omitempty"`
	Status      string    `gorm:"size:50;default:pending" json:"status"`
	RequestedBy string    `gorm:"size:36" json:"requested_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline,omitempty"`
}

// TableName 테이블 이름
func (ProvisioningRequest) TableName() string {
	return "provisioning_requests"
}

// ProvisioningResult 사전작업 결과
type ProvisioningResult struct {
	ID         string `gorm:"primaryKey;size:36" json:"id"`
	RequestID  string `gorm:"size:36;not null;index" json:"request_id"`
	PipelineID string `gorm:"size:36;not null;index" json:"pipeline_id"`
	SinkType   string `gorm:"size:50" json:"sink_type"`
	Status     string `gorm:"size:50;not null" json:"status"`

	// 결과 필드들 - 저장소 타입에 따라 선택적 사용
	ResultTableName   string `gorm:"column:table_name;size:255" json:"table_name,omitempty"`
	ResultTopicName   string `gorm:"column:topic_name;size:255" json:"topic_name,omitempty"`
	ResultIndexName   string `gorm:"column:index_name;size:255" json:"index_name,omitempty"`
	ResultBucketName  string `gorm:"column:bucket_name;size:255" json:"bucket_name,omitempty"`
	ResultFilePath    string `gorm:"column:file_path;size:500" json:"file_path,omitempty"`
	ResultAPIEndpoint string `gorm:"column:api_endpoint;size:500" json:"api_endpoint,omitempty"`
	ResultAPIKey      string `gorm:"column:api_key;size:255" json:"api_key,omitempty"`
	ResultMetadata    string `gorm:"column:metadata;type:text" json:"metadata,omitempty"` // JSON

	Message     string     `gorm:"type:text" json:"message,omitempty"`
	ErrorDetail string     `gorm:"type:text" json:"error_detail,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CompletedBy string     `gorm:"size:255" json:"completed_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`

	Request ProvisioningRequest `gorm:"foreignKey:RequestID" json:"request,omitempty"`
}

// TableName 테이블 이름
func (ProvisioningResult) TableName() string {
	return "provisioning_results"
}

// Project 프로젝트 모델
// 프로젝트 > 워크플로우 > 파이프라인 계층구조의 최상위
type Project struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Name        string         `gorm:"size:255;not null;uniqueIndex" json:"name"`  // 프로젝트명 (표시용, unique)
	Alias       string         `gorm:"size:255;not null;uniqueIndex" json:"alias"` // URL 경로용 별칭 (unique)
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Status      string         `gorm:"size:50;default:active" json:"status"` // active, inactive, archived
	OwnerID     string         `gorm:"size:36;index" json:"owner_id,omitempty"`
	Metadata    string         `gorm:"type:text" json:"metadata,omitempty"` // JSON
	Tags        string         `gorm:"type:text" json:"tags,omitempty"`     // JSON array
	CreatedBy   string         `gorm:"size:36" json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations (외래키 제약조건 비활성화 - 애플리케이션 레벨에서 관리)
	Owner     *User          `gorm:"foreignKey:OwnerID;constraint:false" json:"owner,omitempty"`
	Owners    []ProjectOwner `gorm:"foreignKey:ProjectID" json:"owners,omitempty"`
	Workflows []Workflow     `gorm:"foreignKey:ProjectID;constraint:false" json:"workflows,omitempty"`
	DataTypes []DataType     `gorm:"foreignKey:ProjectID;constraint:false" json:"data_types,omitempty"`
}

// TableName 테이블 이름
func (Project) TableName() string {
	return "projects"
}

// ProjectOwner 프로젝트 담당자 모델 (다대다 관계)
type ProjectOwner struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`
	ProjectID string    `gorm:"size:36;not null;index:idx_project_owner,unique" json:"project_id"`
	UserID    string    `gorm:"size:36;not null;index:idx_project_owner,unique" json:"user_id"`
	Role      string    `gorm:"size:50;default:owner" json:"role"` // owner, maintainer, viewer
	CreatedAt time.Time `json:"created_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName 테이블 이름
func (ProjectOwner) TableName() string {
	return "project_owners"
}

// Workflow 워크플로우 모델
type Workflow struct {
	ID               string         `gorm:"primaryKey;size:36" json:"id"`
	ProjectID        string         `gorm:"size:36;not null;index" json:"project_id"` // 프로젝트 FK (필수)
	Name             string         `gorm:"size:255;not null" json:"name"`
	Slug             string         `gorm:"size:255;index" json:"slug,omitempty"` // URL 경로명
	Description      string         `gorm:"type:text" json:"description,omitempty"`
	Type             string         `gorm:"size:50;not null" json:"type"`                   // realtime, batch, hybrid
	ExecutionMode    string         `gorm:"size:50;default:parallel" json:"execution_mode"` // parallel, sequential, dag
	Status           string         `gorm:"size:50;default:idle" json:"status"`             // idle, running, paused, stopped, error, completed
	ScheduleType     string         `gorm:"size:50" json:"schedule_type,omitempty"`         // cron, interval, manual, event
	ScheduleCron     string         `gorm:"size:100" json:"schedule_cron,omitempty"`
	ScheduleInterval string         `gorm:"size:50" json:"schedule_interval,omitempty"`
	ScheduleTimezone string         `gorm:"size:50" json:"schedule_timezone,omitempty"`
	ScheduleEnabled  bool           `gorm:"default:true" json:"schedule_enabled"`
	PipelinesConfig  string         `gorm:"type:longtext" json:"pipelines_config"`     // JSON - GroupedPipeline array
	FailurePolicy    string         `gorm:"type:text" json:"failure_policy,omitempty"` // JSON
	Metadata         string         `gorm:"type:text" json:"metadata,omitempty"`       // JSON
	Tags             string         `gorm:"type:text" json:"tags,omitempty"`           // JSON array
	LastRunAt        *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt        *time.Time     `json:"next_run_at,omitempty"`
	CreatedBy        string         `gorm:"size:36" json:"created_by"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Project    *Project            `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Executions []WorkflowExecution `gorm:"foreignKey:WorkflowID" json:"executions,omitempty"`
}

// TableName 테이블 이름
func (Workflow) TableName() string {
	return "workflows"
}

// WorkflowExecution 워크플로우 실행 이력
type WorkflowExecution struct {
	ID              string     `gorm:"primaryKey;size:36" json:"id"`
	WorkflowID      string     `gorm:"size:36;not null;index" json:"workflow_id"`
	Status          string     `gorm:"size:50;not null" json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	DurationMs      int64      `json:"duration_ms,omitempty"`
	PipelineResults string     `gorm:"type:longtext" json:"pipeline_results,omitempty"` // JSON array
	TotalRecords    int64      `gorm:"default:0" json:"total_records"`
	FailedRecords   int64      `gorm:"default:0" json:"failed_records"`
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"`
	TriggeredBy     string     `gorm:"size:50" json:"triggered_by"` // user, schedule, event, api
	TriggeredByID   string     `gorm:"size:36" json:"triggered_by_id,omitempty"`
	Metadata        string     `gorm:"type:text" json:"metadata,omitempty"` // JSON
	CreatedAt       time.Time  `json:"created_at"`

	// Relations
	Workflow Workflow `gorm:"foreignKey:WorkflowID" json:"workflow,omitempty"`
}

// TableName 테이블 이름
func (WorkflowExecution) TableName() string {
	return "workflow_executions"
}

// ResourcePermission 리소스 권한 모델
type ResourcePermission struct {
	ID           string    `gorm:"primaryKey;size:36" json:"id"`
	ResourceType string    `gorm:"size:50;not null;index" json:"resource_type"` // project, workflow, pipeline
	ResourceID   string    `gorm:"size:36;not null;index" json:"resource_id"`
	UserID       string    `gorm:"size:36;index" json:"user_id,omitempty"`
	RoleID       string    `gorm:"size:36;index" json:"role_id,omitempty"`
	Actions      string    `gorm:"size:255;not null" json:"actions"` // read,write,execute,delete,admin
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName 테이블 이름
func (ResourcePermission) TableName() string {
	return "resource_permissions"
}

// PipelineExecutionStats 배치 파이프라인 실행 통계
// 배치 작업 실행 시 수집량, 처리량, 에러 등을 저장
type PipelineExecutionStats struct {
	ID               string     `gorm:"primaryKey;size:36" json:"id"`
	ExecutionID      string     `gorm:"size:36;not null;index" json:"execution_id"` // FK: workflow_executions
	PipelineID       string     `gorm:"size:36;not null;index" json:"pipeline_id"`
	PipelineName     string     `gorm:"size:255" json:"pipeline_name"`
	WorkflowID       string     `gorm:"size:36;not null;index" json:"workflow_id"`
	RecordsCollected int64      `gorm:"default:0" json:"records_collected"` // 수집량
	RecordsProcessed int64      `gorm:"default:0" json:"records_processed"` // 처리량
	PerStageCounts   string     `gorm:"type:json" json:"per_stage_counts"`  // Stage별 처리량 JSON
	CollectionErrors int64      `gorm:"default:0" json:"collection_errors"` // 수집에러
	ProcessingErrors int64      `gorm:"default:0" json:"processing_errors"` // 처리에러
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	DurationMs       int64      `gorm:"default:0" json:"duration_ms"`
	CreatedAt        time.Time  `json:"created_at"`
}

// TableName 테이블 이름
func (PipelineExecutionStats) TableName() string {
	return "pipeline_execution_stats"
}

// PipelineHourlyStats 실시간 파이프라인 시간별 통계
// 실시간 파이프라인의 시간 단위 통계 집계
type PipelineHourlyStats struct {
	ID               string    `gorm:"primaryKey;size:36" json:"id"`
	PipelineID       string    `gorm:"size:36;not null;index:idx_pipeline_hour,unique" json:"pipeline_id"`
	PipelineName     string    `gorm:"size:255" json:"pipeline_name"`
	WorkflowID       string    `gorm:"size:36;not null;index" json:"workflow_id"`
	BucketHour       time.Time `gorm:"not null;index:idx_pipeline_hour,unique;index" json:"bucket_hour"` // 시간 경계
	RecordsCollected int64     `gorm:"default:0" json:"records_collected"`
	RecordsProcessed int64     `gorm:"default:0" json:"records_processed"`
	PerStageCounts   string    `gorm:"type:json" json:"per_stage_counts"`
	CollectionErrors int64     `gorm:"default:0" json:"collection_errors"`
	ProcessingErrors int64     `gorm:"default:0" json:"processing_errors"`
	SampleCount      int       `gorm:"default:0" json:"sample_count"` // 버킷 내 샘플 수
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TableName 테이블 이름
func (PipelineHourlyStats) TableName() string {
	return "pipeline_hourly_stats"
}

// DataType 데이터 유형 모델
// 같은 유형의 데이터는 동일한 스키마, 삭제 전략, 저장소를 공유
// 프로젝트에 종속되며, 같은 프로젝트 내 여러 Workflow에서 공유 가능
type DataType struct {
	ID          string  `gorm:"primaryKey;size:36" json:"id"`
	ProjectID   string  `gorm:"size:36;not null;index:idx_datatype_project_name,unique" json:"project_id"` // 프로젝트 FK
	ParentID    *string `gorm:"size:36;index" json:"parent_id,omitempty"`                                  // 부모 데이터타입 FK (종속관계: 게시판-게시글)
	Name        string  `gorm:"size:100;not null;index:idx_datatype_project_name,unique" json:"name"`      // 데이터 유형 코드 (프로젝트 내 unique)
	DisplayName string  `gorm:"size:255;not null" json:"display_name"`                                     // 표시명 (예: 사용자 정보, 주문 내역)
	Description string  `gorm:"type:text" json:"description,omitempty"`
	Category    string  `gorm:"size:50" json:"category,omitempty"` // master, transaction, log, etc.

	// 삭제 전략 (JSON)
	DeleteStrategy string `gorm:"type:text" json:"delete_strategy,omitempty"`

	// ID 필드 설정 (JSON array - 복합키 지원, 예: ["board_id", "post_id"])
	// 종속관계 데이터의 경우 부모 ID + 자신 ID로 복합키 구성
	IDFields string `gorm:"type:text" json:"id_fields,omitempty"`

	// 스키마 정보 (JSON)
	Schema string `gorm:"type:text" json:"schema,omitempty"`

	// 저장소 설정 (JSON)
	Storage string `gorm:"type:text" json:"storage,omitempty"`

	CreatedBy string         `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Project  *Project          `gorm:"foreignKey:ProjectID;constraint:false" json:"project,omitempty"`
	Parent   *DataType         `gorm:"foreignKey:ParentID;constraint:false" json:"parent,omitempty"`
	Children []DataType        `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Preworks []DataTypePrework `gorm:"foreignKey:DataTypeID" json:"preworks,omitempty"`
}

// TableName 테이블 이름
func (DataType) TableName() string {
	return "data_types"
}

// DataTypePrework 데이터 유형별 사전작업 모델
type DataTypePrework struct {
	ID          string `gorm:"primaryKey;size:36" json:"id"`
	DataTypeID  string `gorm:"size:36;not null;index" json:"data_type_id"`
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Type        string `gorm:"size:50;not null" json:"type"`  // sql, http, elasticsearch, s3, script
	Phase       string `gorm:"size:50;not null" json:"phase"` // data_type, pipeline, manual
	Order       int    `gorm:"default:0" json:"order"`        // 실행 순서

	// 타입별 설정 (JSON)
	Config string `gorm:"type:text;not null" json:"config"`

	// 실행 상태
	Status     string     `gorm:"size:50;default:pending" json:"status"` // pending, running, completed, failed, skipped
	ExecutedAt *time.Time `json:"executed_at,omitempty"`
	ExecutedBy string     `gorm:"size:36" json:"executed_by,omitempty"`
	ErrorMsg   string     `gorm:"type:text" json:"error_msg,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Relations
	DataType DataType `gorm:"foreignKey:DataTypeID" json:"data_type,omitempty"`
}

// TableName 테이블 이름
func (DataTypePrework) TableName() string {
	return "data_type_preworks"
}

// DeleteStrategyPreset 삭제 전략 프리셋 모델
type DeleteStrategyPreset struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`
	Name        string    `gorm:"size:100;not null;uniqueIndex" json:"name"`
	DisplayName string    `gorm:"size:255" json:"display_name,omitempty"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	Strategy    string    `gorm:"type:text;not null" json:"strategy"` // JSON
	IsDefault   bool      `gorm:"default:false" json:"is_default"`
	IsSystem    bool      `gorm:"default:false" json:"is_system"` // 시스템 제공 프리셋
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 테이블 이름
func (DeleteStrategyPreset) TableName() string {
	return "delete_strategy_presets"
}

// Connection 외부 연결 정보 모델
// 사전작업에서 참조하는 외부 시스템 연결 정보
type Connection struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Name        string         `gorm:"size:100;not null;uniqueIndex" json:"name"`
	DisplayName string         `gorm:"size:255" json:"display_name,omitempty"`
	Type        string         `gorm:"size:50;not null" json:"type"`     // mysql, postgresql, elasticsearch, s3, http
	Config      string         `gorm:"type:text;not null" json:"config"` // JSON (encrypted sensitive fields)
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Status      string         `gorm:"size:50;default:active" json:"status"` // active, inactive, testing
	CreatedBy   string         `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 테이블 이름
func (Connection) TableName() string {
	return "connections"
}
