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

// AgentCommand 에이전트로 전송되는 명령
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
	ID             string         `json:"id"`
	WorkflowID     string         `json:"workflow_id"`
	ExecutionID    string         `json:"execution_id"`
	TriggeredBy    string         `json:"triggered_by"` // "user", "schedule", "event"
	UserID         string         `json:"user_id,omitempty"`
	WorkflowConfig *Workflow      `json:"workflow_config,omitempty"`
	Timestamp      time.Time      `json:"timestamp"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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
