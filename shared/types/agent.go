package types

import "time"

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
	AgentID       string                 `json:"agent_id"`
	ClusterID     string                 `json:"cluster_id,omitempty"` // 에이전트가 속한 클러스터
	Hostname      string                 `json:"hostname"`
	Timestamp     time.Time              `json:"timestamp"`
	CPUUsage      float64                `json:"cpu_usage"`
	MemoryUsage   float64                `json:"memory_usage"`
	DiskUsage     float64                `json:"disk_usage"`
	Pipelines     []string               `json:"pipelines"`
	PipelineStats []PipelineStatShort    `json:"pipeline_stats,omitempty"`
	RunningExecs  []RunningExecutionInfo `json:"running_execs,omitempty"` // 실행 중인 워크플로우
}

// RunningExecutionInfo 실행 중인 워크플로우 정보
type RunningExecutionInfo struct {
	ExecutionID string    `json:"execution_id"`
	WorkflowID  string    `json:"workflow_id"`
	StartedAt   time.Time `json:"started_at"`
}

// PipelineStatShort 간략한 파이프라인 통계
type PipelineStatShort struct {
	PipelineID     string         `json:"pipeline_id"`
	Status         PipelineStatus `json:"status"`
	ProcessedCount int64          `json:"processed_count"`
	ErrorCount     int64          `json:"error_count"`
}

// AgentCommand 에이전트로 전송되는 명령
type AgentCommand struct {
	ID          string      `json:"id"`
	Type        CommandType `json:"type"`
	PipelineID  string      `json:"pipeline_id,omitempty"`
	WorkflowID  string      `json:"workflow_id,omitempty"`  // 워크플로우 단위 명령 대상
	ExecutionID string      `json:"execution_id,omitempty"` // 특정 실행 대상 (stop/pause/resume)
	Payload     any         `json:"payload,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
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
	// 워크플로우 실행 명령
	CommandStartWorkflow  CommandType = "start_workflow"
	CommandStopWorkflow   CommandType = "stop_workflow"
	CommandPauseWorkflow  CommandType = "pause_workflow"
	CommandResumeWorkflow CommandType = "resume_workflow"
)

// AgentCommandResponse 명령 응답
type AgentCommandResponse struct {
	CommandID string `json:"command_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WorkflowExecutionCommand 워크플로우 실행 명령
// Control Plane에서 Agent로 전송되어 워크플로우 실행을 트리거
type WorkflowExecutionCommand struct {
	ID              string         `json:"id"`
	WorkflowID      string         `json:"workflow_id"`
	ExecutionID     string         `json:"execution_id"`
	TargetClusterID string         `json:"target_cluster_id,omitempty"` // 대상 클러스터 ID
	TriggeredBy     string         `json:"triggered_by"`                // "user", "schedule", "event"
	UserID          string         `json:"user_id,omitempty"`
	WorkflowConfig  *Workflow      `json:"workflow_config,omitempty"`
	JobConfig       string         `json:"job_config,omitempty"` // batch 위임 시 worker가 K8s Job 리소스 스펙으로 사용(선택)
	Timestamp       time.Time      `json:"timestamp"`
	Metadata        map[string]any `json:"metadata,omitempty"`

	// 파티션 분산 실행(partition-distributed-execution): partitioned source 를 여러 agent/Job 에
	// 나눠 실행할 때 쓴다. 둘 다 비면 현행(단일 실행)과 100% 동일 동작(상위호환).
	ParentExecutionID  string   `json:"parent_execution_id,omitempty"` // sub-execution 이면 부모 execution id
	AssignedPartitions []string `json:"assigned_partitions,omitempty"` // 이 sub-execution 이 처리할 파티션 ID 부분집합
}

// WorkflowExecutionResult 워크플로우 실행 결과
// Agent에서 Control Plane으로 전송
type WorkflowExecutionResult struct {
	ExecutionID     string                    `json:"execution_id"`
	WorkflowID      string                    `json:"workflow_id"`
	Status          WorkflowStatus            `json:"status"`
	PipelineResults []PipelineExecutionResult `json:"pipeline_results,omitempty"`
	TotalRecords    int64                     `json:"total_records"`
	FailedRecords   int64                     `json:"failed_records"`
	StartedAt       time.Time                 `json:"started_at"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
	ErrorMessage    string                    `json:"error_message,omitempty"`
}

// Backward compatibility type aliases
type GroupExecutionCommand = WorkflowExecutionCommand
type GroupExecutionResult = WorkflowExecutionResult

// ClusterAgentConfig 클러스터별 Agent 배포 설정
type ClusterAgentConfig struct {
	// Kubernetes 배포 설정
	NodeSelector map[string]string `json:"node_selector,omitempty"` // 노드 선택 레이블
	Tolerations  []Toleration      `json:"tolerations,omitempty"`   // 테인트 허용
	Affinity     *Affinity         `json:"affinity,omitempty"`      // 어피니티 규칙

	// 리소스 설정
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Agent 설정
	Labels map[string]string `json:"labels,omitempty"` // Agent Pod에 추가할 레이블
	Env    map[string]string `json:"env,omitempty"`    // 추가 환경변수
}

// Toleration Kubernetes Toleration
type Toleration struct {
	Key               string `json:"key,omitempty"`
	Operator          string `json:"operator,omitempty"` // Exists, Equal
	Value             string `json:"value,omitempty"`
	Effect            string `json:"effect,omitempty"` // NoSchedule, PreferNoSchedule, NoExecute
	TolerationSeconds *int64 `json:"toleration_seconds,omitempty"`
}

// Affinity Kubernetes Affinity (간소화)
type Affinity struct {
	NodeAffinity    *NodeAffinity    `json:"node_affinity,omitempty"`
	PodAntiAffinity *PodAntiAffinity `json:"pod_anti_affinity,omitempty"`
}

// NodeAffinity 노드 어피니티
type NodeAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  *NodeSelector          `json:"required,omitempty"`
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedNodeSelector `json:"preferred,omitempty"`
}

// NodeSelector 노드 선택기
type NodeSelector struct {
	NodeSelectorTerms []NodeSelectorTerm `json:"terms,omitempty"`
}

// NodeSelectorTerm 노드 선택 조건
type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement `json:"match_expressions,omitempty"`
}

// NodeSelectorRequirement 노드 선택 요구사항
type NodeSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"` // In, NotIn, Exists, DoesNotExist
	Values   []string `json:"values,omitempty"`
}

// WeightedNodeSelector 가중치 노드 선택기
type WeightedNodeSelector struct {
	Weight     int32            `json:"weight"`
	Preference NodeSelectorTerm `json:"preference"`
}

// PodAntiAffinity Pod 안티 어피니티
type PodAntiAffinity struct {
	// 같은 노드에 배포되지 않도록 분산
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedPodAffinityTerm `json:"preferred,omitempty"`
}

// WeightedPodAffinityTerm 가중치 Pod 어피니티 조건
type WeightedPodAffinityTerm struct {
	Weight        int32  `json:"weight"`
	TopologyKey   string `json:"topology_key"` // kubernetes.io/hostname
	LabelSelector string `json:"label_selector,omitempty"`
}

// ResourceRequirements 리소스 요구사항
type ResourceRequirements struct {
	Requests ResourceList `json:"requests,omitempty"`
	Limits   ResourceList `json:"limits,omitempty"`
}

// ResourceList 리소스 목록
type ResourceList struct {
	CPU    string `json:"cpu,omitempty"`    // "500m"
	Memory string `json:"memory,omitempty"` // "512Mi"
}
