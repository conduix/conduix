package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/conduix/conduix/shared/types"
)

// JobManager K8s Job/CronJob 관리자
type JobManager struct {
	client          *Client
	controlPlaneURL string
	runnerImage     string
}

// NewJobManager JobManager 생성
func NewJobManager(client *Client, controlPlaneURL, runnerImage string) *JobManager {
	return &JobManager{
		client:          client,
		controlPlaneURL: controlPlaneURL,
		runnerImage:     runnerImage,
	}
}

// JobSpec 배치 Job 생성용 파라미터
type JobSpec struct {
	ExecutionID     string
	WorkflowID      string
	PipelinesConfig string // JSON
	JobConfig       types.JobConfig
}

// CreateBatchJob 배치 파이프라인용 K8s Job 생성
func (m *JobManager) CreateBatchJob(ctx context.Context, spec *JobSpec) (*batchv1.Job, error) {
	jobName := m.generateJobName(spec.WorkflowID, spec.ExecutionID)
	cfg := spec.JobConfig

	image := m.runnerImage
	if cfg.Image != "" {
		image = cfg.Image
	}
	if image == "" {
		return nil, fmt.Errorf("runner image is required")
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	backoffLimit := cfg.BackoffLimit
	if backoffLimit == 0 {
		backoffLimit = 3
	}

	ttl := cfg.TTLAfterFinished
	if ttl == 0 {
		ttl = 300
	}

	timeoutSeconds := cfg.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 3600
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "conduix-runner",
		"app.kubernetes.io/component":  "batch-job",
		"app.kubernetes.io/managed-by": "conduix-agent",
		"conduix.io/workflow-id":       sanitizeLabel(spec.WorkflowID),
		"conduix.io/execution-id":      sanitizeLabel(spec.ExecutionID),
	}

	callbackURL := fmt.Sprintf("%s/api/v1/internal/job-result", m.controlPlaneURL)

	envVars := []corev1.EnvVar{
		{Name: "EXECUTION_MODE", Value: "batch"},
		{Name: "EXECUTION_ID", Value: spec.ExecutionID},
		{Name: "WORKFLOW_ID", Value: spec.WorkflowID},
		{Name: "PIPELINES_CONFIG", Value: spec.PipelinesConfig},
		{Name: "CONTROL_PLANE_URL", Value: m.controlPlaneURL},
		{Name: "CALLBACK_URL", Value: callbackURL},
		{Name: "TIMEOUT_SECONDS", Value: fmt.Sprintf("%d", timeoutSeconds)},
	}

	// 리소스 설정
	resources := buildResourceRequirements(cfg)

	// ImagePullPolicy 설정
	pullPolicy := corev1.PullIfNotPresent
	switch cfg.ImagePullPolicy {
	case "Always":
		pullPolicy = corev1.PullAlways
	case "Never":
		pullPolicy = corev1.PullNever
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "pipeline-batch-job",
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Env:             envVars,
							Resources:       resources,
						},
					},
				},
			},
		},
	}

	// NodeSelector 설정
	if len(cfg.NodeSelector) > 0 {
		job.Spec.Template.Spec.NodeSelector = cfg.NodeSelector
	}

	// ServiceAccount 설정
	if cfg.ServiceAccount != "" {
		job.Spec.Template.Spec.ServiceAccountName = cfg.ServiceAccount
	}

	created, err := m.client.Clientset().BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job %s: %w", jobName, err)
	}

	return created, nil
}

// CronJobSpec CronJob 생성용 파라미터
type CronJobSpec struct {
	WorkflowID      string
	CronExpression  string
	PipelinesConfig string // JSON
	JobConfig       types.JobConfig
	Suspend         bool
}

// CreateCronJob 스케줄 파이프라인용 K8s CronJob 생성
func (m *JobManager) CreateCronJob(ctx context.Context, spec *CronJobSpec) (*batchv1.CronJob, error) {
	cronJobName := fmt.Sprintf("conduix-cron-%s", sanitizeName(spec.WorkflowID))
	cfg := spec.JobConfig

	image := m.runnerImage
	if cfg.Image != "" {
		image = cfg.Image
	}
	if image == "" {
		return nil, fmt.Errorf("runner image is required")
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	backoffLimit := cfg.BackoffLimit
	if backoffLimit == 0 {
		backoffLimit = 3
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "conduix-runner",
		"app.kubernetes.io/component":  "cron-job",
		"app.kubernetes.io/managed-by": "conduix-agent",
		"conduix.io/workflow-id":       sanitizeLabel(spec.WorkflowID),
	}

	callbackURL := fmt.Sprintf("%s/api/v1/internal/job-result", m.controlPlaneURL)

	envVars := []corev1.EnvVar{
		{Name: "EXECUTION_MODE", Value: "batch"},
		{Name: "WORKFLOW_ID", Value: spec.WorkflowID},
		{Name: "PIPELINES_CONFIG", Value: spec.PipelinesConfig},
		{Name: "CONTROL_PLANE_URL", Value: m.controlPlaneURL},
		{Name: "CALLBACK_URL", Value: callbackURL},
	}

	resources := buildResourceRequirements(cfg)

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: spec.CronExpression,
			Suspend:  &spec.Suspend,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: &backoffLimit,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labels,
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:      "pipeline-batch-job",
									Image:     image,
									Env:       envVars,
									Resources: resources,
								},
							},
						},
					},
				},
			},
		},
	}

	if len(cfg.NodeSelector) > 0 {
		cronJob.Spec.JobTemplate.Spec.Template.Spec.NodeSelector = cfg.NodeSelector
	}
	if cfg.ServiceAccount != "" {
		cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName = cfg.ServiceAccount
	}

	created, err := m.client.Clientset().BatchV1().CronJobs(namespace).Create(ctx, cronJob, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create cronjob %s: %w", cronJobName, err)
	}

	return created, nil
}

// DeleteJob Job 삭제
func (m *JobManager) DeleteJob(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	propagation := metav1.DeletePropagationBackground
	err := m.client.Clientset().BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete job %s: %w", name, err)
	}
	return nil
}

// DeleteCronJob CronJob 삭제
func (m *JobManager) DeleteCronJob(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	propagation := metav1.DeletePropagationBackground
	err := m.client.Clientset().BatchV1().CronJobs(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete cronjob %s: %w", name, err)
	}
	return nil
}

// JobStatus Job 상태 정보
type JobStatus struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Active    int32  `json:"active"`
	Succeeded int32  `json:"succeeded"`
	Failed    int32  `json:"failed"`
	Status    string `json:"status"` // pending, running, completed, failed
}

// GetJobStatus Job 상태 조회
func (m *JobManager) GetJobStatus(ctx context.Context, namespace, name string) (*JobStatus, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	job, err := m.client.Clientset().BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get job %s: %w", name, err)
	}

	status := &JobStatus{
		Name:      job.Name,
		Namespace: job.Namespace,
		Active:    job.Status.Active,
		Succeeded: job.Status.Succeeded,
		Failed:    job.Status.Failed,
	}

	switch {
	case job.Status.Succeeded > 0:
		status.Status = "completed"
	case job.Status.Failed > 0:
		status.Status = "failed"
	case job.Status.Active > 0:
		status.Status = "running"
	default:
		status.Status = "pending"
	}

	return status, nil
}

// ListJobs 레이블로 Job 목록 조회
func (m *JobManager) ListJobs(ctx context.Context, namespace string) ([]JobStatus, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	jobs, err := m.client.Clientset().BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=conduix-agent",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	var result []JobStatus
	for _, job := range jobs.Items {
		s := JobStatus{
			Name:      job.Name,
			Namespace: job.Namespace,
			Active:    job.Status.Active,
			Succeeded: job.Status.Succeeded,
			Failed:    job.Status.Failed,
		}
		switch {
		case job.Status.Succeeded > 0:
			s.Status = "completed"
		case job.Status.Failed > 0:
			s.Status = "failed"
		case job.Status.Active > 0:
			s.Status = "running"
		default:
			s.Status = "pending"
		}
		result = append(result, s)
	}

	return result, nil
}

// generateJobName Job 이름 생성 (K8s 이름 규칙 준수)
func (m *JobManager) generateJobName(workflowID, executionID string) string {
	wfShort := sanitizeName(workflowID)
	if len(wfShort) > 20 {
		wfShort = wfShort[:20]
	}
	execShort := sanitizeName(executionID)
	if len(execShort) > 8 {
		execShort = execShort[:8]
	}
	return fmt.Sprintf("conduix-job-%s-%s", wfShort, execShort)
}

// buildResourceRequirements JobConfig에서 K8s ResourceRequirements 생성
func buildResourceRequirements(cfg types.JobConfig) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}

	if cfg.CPU != "" || cfg.Memory != "" {
		resources.Requests = corev1.ResourceList{}
		if cfg.CPU != "" {
			resources.Requests[corev1.ResourceCPU] = resource.MustParse(cfg.CPU)
		}
		if cfg.Memory != "" {
			resources.Requests[corev1.ResourceMemory] = resource.MustParse(cfg.Memory)
		}
	}

	if cfg.CPULimit != "" || cfg.MemoryLimit != "" {
		resources.Limits = corev1.ResourceList{}
		if cfg.CPULimit != "" {
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(cfg.CPULimit)
		}
		if cfg.MemoryLimit != "" {
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(cfg.MemoryLimit)
		}
	}

	return resources
}

// sanitizeName K8s 이름 규칙에 맞게 정리
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	// 알파벳, 숫자, 하이픈만 허용
	var result []byte
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		}
	}
	// 하이픈으로 시작/끝나지 않도록
	return strings.Trim(string(result), "-")
}

// sanitizeLabel K8s 레이블 값 규칙에 맞게 정리 (최대 63자)
func sanitizeLabel(value string) string {
	s := sanitizeName(value)
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.Trim(s, "-")
}

// PipelinesConfigToJSON WorkflowPipeline 배열을 JSON 문자열로 변환
func PipelinesConfigToJSON(pipelines []types.WorkflowPipeline) (string, error) {
	data, err := json.Marshal(pipelines)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pipelines config: %w", err)
	}
	return string(data), nil
}
