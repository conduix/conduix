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
// Job/Deployment 기반 실행을 지원
type PipelineRun struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	PipelineID     string     `gorm:"size:36;not null;index" json:"pipeline_id"`
	WorkflowID     string     `gorm:"size:36;index" json:"workflow_id,omitempty"` // 워크플로우 FK (optional)
	ClusterID      string     `gorm:"size:36;index" json:"cluster_id,omitempty"`  // 실행 클러스터
	Status         string     `gorm:"size:50;not null" json:"status"`             // pending, running, completed, failed, canceled
	AgentID        string     `gorm:"size:36" json:"agent_id,omitempty"`          // 실행 관리한 Agent
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	ProcessedCount int64      `gorm:"default:0" json:"processed_count"`
	ErrorCount     int64      `gorm:"default:0" json:"error_count"`
	ErrorMessage   string     `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`

	// K8s 리소스 정보 (Job/Deployment 기반 실행)
	ExecutionMode   string `gorm:"size:50" json:"execution_mode,omitempty"`     // batch, streaming, hybrid
	K8sResourceType string `gorm:"size:50" json:"k8s_resource_type,omitempty"`  // job, cronjob, deployment
	K8sResourceName string `gorm:"size:255" json:"k8s_resource_name,omitempty"` // K8s 리소스 이름
	K8sNamespace    string `gorm:"size:255" json:"k8s_namespace,omitempty"`     // K8s 네임스페이스
	RunnerImage     string `gorm:"size:500" json:"runner_image,omitempty"`      // Pipeline Runner 이미지

	// Relations
	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline"`
	Cluster  *Cluster `gorm:"foreignKey:ClusterID" json:"cluster,omitempty"`
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

	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline"`
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

// Cluster 클러스터 모델
// 여러 Kubernetes 클러스터를 관리하기 위한 모델
type Cluster struct {
	ID           string `gorm:"primaryKey;size:36" json:"id"`
	Name         string `gorm:"size:255;not null;uniqueIndex" json:"name"`
	Description  string `gorm:"type:text" json:"description,omitempty"`
	APIServerURL string `gorm:"size:500" json:"api_server_url,omitempty"` // 정보성 (직접 연결 안 함)
	Region       string `gorm:"size:100" json:"region,omitempty"`
	Status       string `gorm:"size:50;default:active" json:"status"` // active, inactive

	// 워크플로우가 cluster_id 미지정 시 실행 대상이 되는 기본 클러스터. 전체에서 최대 1개.
	IsDefault bool `gorm:"default:false;index" json:"is_default"`

	// Agent 배포 설정
	DesiredAgents int    `gorm:"default:1" json:"desired_agents"`         // 원하는 Agent 수
	AgentConfig   string `gorm:"type:text" json:"agent_config,omitempty"` // JSON: nodeSelector, tolerations, affinity 등

	CreatedBy string         `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 테이블 이름
func (Cluster) TableName() string {
	return "clusters"
}

// Agent 에이전트 모델
// Agent는 클러스터 매니저 역할을 담당 (파이프라인 실행 X, Job/Deployment 관리 O)
type Agent struct {
	ID            string     `gorm:"primaryKey;size:36" json:"id"`
	Hostname      string     `gorm:"size:255;not null" json:"hostname"`
	IPAddress     string     `gorm:"size:45" json:"ip_address,omitempty"`
	Status        string     `gorm:"size:50;default:unknown" json:"status"` // active, inactive, error
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	RegisteredAt  time.Time  `json:"registered_at"`
	Version       string     `gorm:"size:50" json:"version,omitempty"`
	Labels        string     `gorm:"type:text" json:"labels,omitempty"` // JSON array
	ClusterID     string     `gorm:"size:36;index" json:"cluster_id,omitempty"`

	// 리소스 모니터링 (클러스터 매니저 역할)
	Metrics      string `gorm:"type:text" json:"metrics,omitempty"`      // JSON - CPU, Memory, Pod count 등
	Capabilities string `gorm:"type:text" json:"capabilities,omitempty"` // JSON - 지원 기능 목록
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

	User User `gorm:"foreignKey:UserID" json:"user"`
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

	Pipeline Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline"`
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

	Request ProvisioningRequest `gorm:"foreignKey:RequestID" json:"request"`
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
	User User `gorm:"foreignKey:UserID" json:"user"`
}

// TableName 테이블 이름
func (ProjectOwner) TableName() string {
	return "project_owners"
}

// Workflow 워크플로우 모델
type Workflow struct {
	ID               string         `gorm:"primaryKey;size:36" json:"id"`
	ProjectID        string         `gorm:"size:36;not null;index" json:"project_id"` // 프로젝트 FK (필수)
	ClusterID        string         `gorm:"size:36;index" json:"cluster_id,omitempty"`
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
	JobConfig        string         `gorm:"type:text" json:"job_config,omitempty"`     // JSON - Kubernetes Job 설정 (Type=batch일 때)
	LastRunAt        *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt        *time.Time     `json:"next_run_at,omitempty"`
	CreatedBy        string         `gorm:"size:36" json:"created_by"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Project    *Project            `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Cluster    *Cluster            `gorm:"foreignKey:ClusterID" json:"cluster,omitempty"`
	Executions []WorkflowExecution `gorm:"foreignKey:WorkflowID" json:"executions,omitempty"`
}

// TableName 테이블 이름
func (Workflow) TableName() string {
	return "workflows"
}

// WorkflowExecution 워크플로우 실행 이력
type WorkflowExecution struct {
	ID                string     `gorm:"primaryKey;size:36" json:"id"`
	WorkflowID        string     `gorm:"size:36;not null;index" json:"workflow_id"`
	ClusterID         string     `gorm:"size:36;index" json:"cluster_id,omitempty"` // 실행 시점 클러스터
	AgentID           string     `gorm:"size:36;index" json:"agent_id,omitempty"`   // 실행한 Agent
	Status            string     `gorm:"size:50;not null" json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	DurationMs        int64      `json:"duration_ms,omitempty"`
	PipelinesSnapshot string     `gorm:"type:longtext" json:"pipelines_snapshot,omitempty"` // JSON - 실행 시점 파이프라인 설정 스냅샷
	PipelineResults   string     `gorm:"type:longtext" json:"pipeline_results,omitempty"`   // JSON array
	TotalRecords      int64      `gorm:"default:0" json:"total_records"`
	FailedRecords     int64      `gorm:"default:0" json:"failed_records"`
	ErrorMessage      string     `gorm:"type:text" json:"error_message,omitempty"`
	TriggeredBy       string     `gorm:"size:50" json:"triggered_by"` // user, schedule, event, api
	TriggeredByID     string     `gorm:"size:36" json:"triggered_by_id,omitempty"`
	Metadata          string     `gorm:"type:text" json:"metadata,omitempty"` // JSON
	CreatedAt         time.Time  `json:"created_at"`

	// 파티션 분산 실행(partition-distributed-execution).
	// 단일 실행이면 셋 다 기본값(부모 없음) → 현행과 동일.
	ParentExecutionID      string `gorm:"size:36;index" json:"parent_execution_id,omitempty"`  // sub-execution 이면 부모 execution id
	TotalSubExecutions     int    `gorm:"default:0" json:"total_sub_executions,omitempty"`     // 부모: 예상 sub-execution 수
	CompletedSubExecutions int    `gorm:"default:0" json:"completed_sub_executions,omitempty"` // 부모: 완료된 sub-execution 수

	// Relations
	Workflow Workflow `gorm:"foreignKey:WorkflowID" json:"workflow"`
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

	User User `gorm:"foreignKey:UserID" json:"user"`
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

	// JSON Schema for validation (JSON Schema draft-07)
	// Source에서 읽은 데이터가 이 스키마를 만족하는지 검증
	JSONSchema string `gorm:"type:text" json:"json_schema,omitempty"`

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
	DataType DataType `gorm:"foreignKey:DataTypeID" json:"data_type"`
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

// InputCheckpoint 입력 체크포인트 모델
// Realtime 파이프라인 입력의 오프셋을 추적하여 재시작 시 연속성 보장
type InputCheckpoint struct {
	ID           string    `gorm:"primaryKey;size:36" json:"id"`
	WorkflowID   string    `gorm:"size:36;not null;index:idx_checkpoint_workflow" json:"workflow_id"`
	PipelineID   string    `gorm:"size:36;not null;index:idx_checkpoint_pipeline" json:"pipeline_id"`
	PipelineName string    `gorm:"size:255" json:"pipeline_name"`
	InputType    string    `gorm:"size:50;not null;column:source_type" json:"input_type"`                        // kubernetes, kafka, cdc, sql_event (DB 컬럼명은 하위호환성 유지)
	PartitionKey string    `gorm:"size:255;not null;index:idx_checkpoint_partition,unique" json:"partition_key"` // ns/pod/container, topic/partition
	OffsetValue  string    `gorm:"size:255;not null" json:"offset_value"`                                        // timestamp, offset number
	OffsetType   string    `gorm:"size:50;not null" json:"offset_type"`                                          // timestamp, numeric
	RecordCount  int64     `gorm:"default:0" json:"record_count"`                                                // 누적 처리 레코드 수
	Metadata     string    `gorm:"type:text" json:"metadata,omitempty"`                                          // JSON - 추가 정보
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Unique constraint: workflow_id + pipeline_id + partition_key
	// index:idx_checkpoint_partition,unique 에 포함
}

// TableName 테이블 이름 (하위호환성 유지)
func (InputCheckpoint) TableName() string {
	return "source_checkpoints"
}

// SourceCheckpoint는 InputCheckpoint의 별칭 (하위 호환성)
// Deprecated: InputCheckpoint를 사용하세요
type SourceCheckpoint = InputCheckpoint

// PipelineLink 파이프라인 간 연결 모델
// 부모 파이프라인의 출력을 자식 파이프라인의 입력으로 연결 (Kafka 기반)
type PipelineLink struct {
	ID               string    `gorm:"primaryKey;size:36" json:"id"`
	WorkflowID       string    `gorm:"size:36;not null;index" json:"workflow_id"`
	ParentPipelineID string    `gorm:"size:36;not null;index:idx_pipeline_link,unique" json:"parent_pipeline_id"` // 부모 파이프라인
	ChildPipelineID  string    `gorm:"size:36;not null;index:idx_pipeline_link,unique" json:"child_pipeline_id"`  // 자식 파이프라인
	KafkaTopic       string    `gorm:"size:255;not null" json:"kafka_topic"`                                      // 자동 생성된 Kafka Topic 이름
	Status           string    `gorm:"size:50;default:active" json:"status"`                                      // active, inactive, error
	Metadata         string    `gorm:"type:text" json:"metadata,omitempty"`                                       // JSON - 추가 설정
	CreatedBy        string    `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Unique constraint: parent_pipeline_id + child_pipeline_id
	// index:idx_pipeline_link,unique 에 포함
	// Note: kafka_brokers 필드 제거 - 환경변수나 설정에서 동적으로 가져옴
}

// TableName 테이블 이름
func (PipelineLink) TableName() string {
	return "pipeline_links"
}

// Plugin 플러그인 모델
// 커스텀 Stage 플러그인 (script: Starlark, native: Go 네이티브 빌드)
type Plugin struct {
	ID              string         `gorm:"primaryKey;size:36" json:"id"`
	Name            string         `gorm:"size:255;not null;uniqueIndex" json:"name"`    // 플러그인 이름 (예: my-company-plugins)
	Type            string         `gorm:"size:20;not null;default:native" json:"type"`  // "script" | "native"
	Version         string         `gorm:"size:50;not null" json:"version"`              // 버전 (예: v1.0.0)
	Image           string         `gorm:"size:500" json:"image,omitempty"`              // 컨테이너 이미지 (V2 호환, V3에서는 optional)
	Description     string         `gorm:"type:text" json:"description,omitempty"`       // 플러그인 설명
	SourceCode      string         `gorm:"type:mediumtext" json:"source_code,omitempty"` // Go 소스 또는 Starlark 스크립트
	GoMod           string         `gorm:"type:text" json:"go_mod,omitempty"`            // native만: go.mod 내용
	SourceHash      string         `gorm:"size:64" json:"source_hash,omitempty"`         // 현재 소스의 SHA256
	DeployedHash    string         `gorm:"size:64" json:"deployed_hash,omitempty"`       // 최신 ready 이미지에 포함된 소스 해시
	RunnerVersionID string         `gorm:"size:36" json:"runner_version_id,omitempty"`   // 이 stage가 포함된 최신 ready 버전 ID
	SourceRepo      string         `gorm:"size:500" json:"source_repo,omitempty"`        // Git 저장소 URL (optional)
	LastTestPassed  bool           `gorm:"default:false" json:"last_test_passed"`        // 마지막 테스트 성공 여부
	LastTestAt      *time.Time     `json:"last_test_at,omitempty"`                       // 마지막 테스트 시각
	LastTestError   string         `gorm:"type:text" json:"last_test_error,omitempty"`   // 마지막 테스트 에러 메시지
	Status          string         `gorm:"size:50;default:active" json:"status"`         // active, inactive, deprecated
	CreatedBy       string         `gorm:"size:36" json:"created_by,omitempty"`          // 등록한 사용자
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Builds        []PluginBuild  `gorm:"foreignKey:PluginID" json:"builds,omitempty"`
	RunnerVersion *RunnerVersion `gorm:"foreignKey:RunnerVersionID" json:"runner_version,omitempty"`
}

// TableName 테이블 이름
func (Plugin) TableName() string {
	return "plugins"
}

// PluginBuild 플러그인 빌드 이력
type PluginBuild struct {
	ID         string     `gorm:"primaryKey;size:36" json:"id"`
	PluginID   string     `gorm:"size:36;index" json:"plugin_id"`
	Status     string     `gorm:"size:20;not null;default:pending" json:"status"` // pending, building, success, failed
	SourceCode string     `gorm:"type:mediumtext;not null" json:"source_code"`    // main.go 소스
	GoMod      string     `gorm:"type:text" json:"go_mod,omitempty"`              // go.mod 내용
	BuildLog   string     `gorm:"type:mediumtext" json:"build_log,omitempty"`     // 빌드 출력 로그
	Error      string     `gorm:"type:text" json:"error,omitempty"`               // 에러 메시지
	DurationMs int        `json:"duration_ms,omitempty"`                          // 빌드 소요 시간 (ms)
	Version    string     `gorm:"size:50" json:"version,omitempty"`               // 빌드 대상 버전
	Platform   string     `gorm:"size:20;default:linux/arm64" json:"platform"`    // GOOS/GOARCH
	CreatedBy  string     `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	// Relations
	Plugin *Plugin `gorm:"foreignKey:PluginID" json:"plugin,omitempty"`
}

// TableName 테이블 이름
func (PluginBuild) TableName() string {
	return "plugin_builds"
}

// RunnerVersion pipeline-batch-job 이미지 빌드 버전
// 모든 native stage를 포함하는 Runner 이미지의 빌드 기록
type RunnerVersion struct {
	ID           string     `gorm:"primaryKey;size:36" json:"id"`                   // "rv-42"
	BuildNumber  int        `gorm:"autoIncrement;uniqueIndex" json:"build_number"`  // 자동 증가
	Status       string     `gorm:"size:20;not null;default:pending" json:"status"` // pending → building → ready | failed
	ImageTag     string     `gorm:"size:500" json:"image_tag,omitempty"`            // "ghcr.io/.../pipeline-batch-job:rv-42"
	ImageDigest  string     `gorm:"size:100" json:"image_digest,omitempty"`         // sha256:... (이미지 무결성 검증)
	Binary       []byte     `gorm:"type:longblob" json:"-"`                         // gzip 압축된 pipeline-batch-job 바이너리. Job initContainer 가 받아 실행(레지스트리 push 없이). json:"-" 로 목록 응답 제외.
	BinarySize   int        `json:"binary_size,omitempty"`                          // 압축 전 바이너리 크기(참고용)
	SourceHash   string     `gorm:"size:64;not null" json:"source_hash"`            // 모든 native stage 소스의 결합 해시
	PluginIDs    string     `gorm:"type:text" json:"plugin_ids,omitempty"`          // JSON: 포함된 native stage ID 목록
	PluginHashes string     `gorm:"type:text" json:"plugin_hashes,omitempty"`       // JSON: plugin_id → source_hash 스냅샷
	RevisionSeq  int        `gorm:"default:0" json:"revision_seq"`                  // 빌드 시점 최신 revision seq (여기까지 포함)
	Trigger      string     `gorm:"size:20;default:manual" json:"trigger"`          // "manual" | "auto" | "rebuild"
	ParentID     string     `gorm:"size:36" json:"parent_id,omitempty"`             // 재빌드 시 원본 버전 ID
	BuildLog     string     `gorm:"type:mediumtext" json:"build_log,omitempty"`
	Error        string     `gorm:"type:text" json:"error,omitempty"`
	DurationMs   int        `json:"duration_ms,omitempty"`
	CreatedBy    string     `gorm:"size:36" json:"created_by,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// StageRevision stage 변경 히스토리 (글로벌 seq 순번)
// Plugin 추가/수정/삭제 시 자동으로 생성되며, 소스 스냅샷을 zstd 압축하여 저장
type StageRevision struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`
	Seq         int       `gorm:"autoIncrement;uniqueIndex" json:"seq"`    // 글로벌 순번
	PluginID    string    `gorm:"size:36;not null;index" json:"plugin_id"` // 변경된 plugin FK
	PluginName  string    `gorm:"size:255" json:"plugin_name"`             // 조회 편의
	Action      string    `gorm:"size:20;not null" json:"action"`          // "create" | "update" | "delete"
	SourceData  []byte    `gorm:"type:mediumblob" json:"-"`                // zstd 압축된 소스 스냅샷
	GoModData   []byte    `gorm:"type:blob" json:"-"`                      // zstd 압축된 go.mod (nullable)
	SourceHash  string    `gorm:"size:64" json:"source_hash"`              // 변경 시점 소스 해시
	DiffSummary string    `gorm:"size:500" json:"diff_summary,omitempty"`  // "+12 -3 lines" 등
	Message     string    `gorm:"size:500" json:"message,omitempty"`       // 사용자 메모 (optional)
	CreatedBy   string    `gorm:"size:36" json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName 테이블 이름
func (RunnerVersion) TableName() string {
	return "runner_versions"
}

// TableName 테이블 이름
func (StageRevision) TableName() string {
	return "stage_revisions"
}
