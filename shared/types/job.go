package types

import "time"

// JobConfig Kubernetes Job 설정
// Batch 워크플로우 실행 시 Job Pod 리소스 및 동작 설정
type JobConfig struct {
	// Resource Requests
	CPU    string `json:"cpu,omitempty"`    // CPU 요청량 (예: "500m")
	Memory string `json:"memory,omitempty"` // Memory 요청량 (예: "512Mi")

	// Resource Limits
	CPULimit    string `json:"cpu_limit,omitempty"`    // CPU 제한 (예: "1000m")
	MemoryLimit string `json:"memory_limit,omitempty"` // Memory 제한 (예: "1Gi")

	// Job Behavior
	TimeoutSeconds   int64 `json:"timeout_seconds,omitempty"`    // 최대 실행 시간 (기본: 3600)
	BackoffLimit     int32 `json:"backoff_limit,omitempty"`      // 재시도 횟수 (기본: 3)
	TTLAfterFinished int32 `json:"ttl_after_finished,omitempty"` // 완료 후 삭제 대기(초, 기본: 300)

	// Pod Configuration
	NodeSelector   map[string]string `json:"node_selector,omitempty"`
	ServiceAccount string            `json:"service_account,omitempty"`
	Namespace      string            `json:"namespace,omitempty"` // 기본: conduix
	Image          string            `json:"image,omitempty"`     // 기본: 현재 Agent 이미지
	ImagePullPolicy string           `json:"image_pull_policy,omitempty"` // Always, IfNotPresent, Never
}

// DefaultJobConfig 기본 Job 설정 반환
func DefaultJobConfig() JobConfig {
	return JobConfig{
		CPU:              "500m",
		Memory:           "512Mi",
		CPULimit:         "1000m",
		MemoryLimit:      "1Gi",
		TimeoutSeconds:   3600,
		BackoffLimit:     3,
		TTLAfterFinished: 300,
		Namespace:        "conduix",
		ImagePullPolicy:  "IfNotPresent",
	}
}

// JobExecutionResult Job 실행 결과 (콜백용)
// Job Pod에서 Control Plane으로 결과를 전송할 때 사용
type JobExecutionResult struct {
	ExecutionID string `json:"execution_id"`
	WorkflowID  string `json:"workflow_id"`
	JobName     string `json:"job_name"`
	PodName     string `json:"pod_name,omitempty"`

	// 실행 결과
	Status          string                    `json:"status"` // completed, error
	PipelineResults []PipelineExecutionResult `json:"pipeline_results,omitempty"`
	TotalRecords    int64                     `json:"total_records"`
	FailedRecords   int64                     `json:"failed_records"`
	ErrorMessage    string                    `json:"error_message,omitempty"`

	// 타이밍
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMs  int64     `json:"duration_ms,omitempty"`
}

// JobStatus Job 상태 상수
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusTimeout   = "timeout"
)

// BatchExecutionCommand Batch 실행 명령 (Job Pod 환경변수로 전달)
type BatchExecutionCommand struct {
	ExecutionID     string `json:"execution_id"`
	WorkflowID      string `json:"workflow_id"`
	PipelinesConfig string `json:"pipelines_config"` // JSON - GroupedPipeline array
	CallbackURL     string `json:"callback_url"`
}
