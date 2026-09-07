package types

import "time"

// RunnerBinaryName 은 위임 실행 pod(batch Job·streaming Deployment)이 실행하는
// runner 바이너리 파일명이다. 세 곳이 반드시 일치해야 하며, 하나만 달라지면
// pod 이 "no such file" 로 기동 실패한다:
//   - CP 빌더의 `go build -o` (control-plane/internal/builder/runner_builder.go)
//   - initContainer 가 gunzip 으로 저장하는 이름 (pipeline-worker/internal/k8s)
//   - main container 의 command (같은 파일)
//
// 모듈이 달라 상수를 공유할 곳이 shared 뿐이므로 여기 둔다.
const RunnerBinaryName = "pipeline-batch-job"

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
	NodeSelector    map[string]string `json:"node_selector,omitempty"`
	ServiceAccount  string            `json:"service_account,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`         // 기본: conduix
	Image           string            `json:"image,omitempty"`             // 기본: 현재 Agent 이미지
	ImagePullPolicy string            `json:"image_pull_policy,omitempty"` // Always, IfNotPresent, Never
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
		// RUNNER_IMAGE 는 :latest/:main 처럼 같은 태그에 새 이미지가 덮이는 가변 태그다.
		// IfNotPresent 면 노드 캐시의 옛 이미지가 재사용돼 방금 배포한 stage 수정이
		// 실행 pod 에 반영되지 않는다(라이브 모니터링 배포 때 실측).
		ImagePullPolicy: "Always",
	}
}

// JobExecutionResult Job 실행 결과 (콜백용)
// Job Pod에서 Control Plane으로 결과를 전송할 때 사용
type JobExecutionResult struct {
	ExecutionID string `json:"execution_id"`
	WorkflowID  string `json:"workflow_id"`
	AgentID     string `json:"agent_id,omitempty"` // 이 Job 을 위임 생성한 agent(노드) — 분산 현황 모니터링용
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
