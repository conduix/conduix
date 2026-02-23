package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// KubernetesJobService Kubernetes Job 관리 서비스
type KubernetesJobService struct {
	clientset      *kubernetes.Clientset
	namespace      string
	agentImage     string
	callbackURL    string
	redisAddr      string
	redisPassword  string
	mu             sync.RWMutex
	activeWatchers map[string]context.CancelFunc // jobName -> cancel func
}

// KubernetesJobServiceConfig 서비스 설정
type KubernetesJobServiceConfig struct {
	Namespace     string // 기본: "conduix"
	AgentImage    string // 기본: 환경변수 AGENT_IMAGE
	CallbackURL   string // Control Plane 내부 API URL
	RedisAddr     string
	RedisPassword string
}

// NewKubernetesJobService Kubernetes Job 서비스 생성
// In-cluster 또는 kubeconfig 기반으로 클라이언트 초기화
func NewKubernetesJobService(cfg *KubernetesJobServiceConfig) (*KubernetesJobService, error) {
	var config *rest.Config
	var err error

	// In-cluster 설정 시도
	config, err = rest.InClusterConfig()
	if err != nil {
		// 로컬 개발 환경: kubeconfig 사용
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "conduix"
	}

	agentImage := cfg.AgentImage
	if agentImage == "" {
		agentImage = os.Getenv("AGENT_IMAGE")
		if agentImage == "" {
			agentImage = "conduix/pipeline-agent:latest"
		}
	}

	return &KubernetesJobService{
		clientset:      clientset,
		namespace:      namespace,
		agentImage:     agentImage,
		callbackURL:    cfg.CallbackURL,
		redisAddr:      cfg.RedisAddr,
		redisPassword:  cfg.RedisPassword,
		activeWatchers: make(map[string]context.CancelFunc),
	}, nil
}

// CreateBatchJob Batch 워크플로우용 Kubernetes Job 생성
// 반환: Job 이름
func (s *KubernetesJobService) CreateBatchJob(
	ctx context.Context,
	workflow *models.Workflow,
	execution *models.WorkflowExecution,
	jobConfig *types.JobConfig,
) (string, error) {
	// Job 이름 생성: workflow-{workflowID 앞 8자}-{executionID 앞 8자}
	jobName := fmt.Sprintf("batch-%s-%s",
		truncateID(workflow.ID, 8),
		truncateID(execution.ID, 8))

	// 기본 설정 적용
	if jobConfig == nil {
		defaultConfig := types.DefaultJobConfig()
		jobConfig = &defaultConfig
	}

	namespace := jobConfig.Namespace
	if namespace == "" {
		namespace = s.namespace
	}

	image := jobConfig.Image
	if image == "" {
		image = s.agentImage
	}

	// 환경변수 구성
	envVars := []corev1.EnvVar{
		{Name: "EXECUTION_MODE", Value: "batch"},
		{Name: "WORKFLOW_ID", Value: workflow.ID},
		{Name: "EXECUTION_ID", Value: execution.ID},
		{Name: "PIPELINES_CONFIG", Value: workflow.PipelinesConfig},
		{Name: "CALLBACK_URL", Value: s.callbackURL},
	}

	// Redis 설정 (체크포인트용)
	if s.redisAddr != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "REDIS_ADDR", Value: s.redisAddr})
	}
	if s.redisPassword != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "conduix-secrets"},
					Key:                  "redis-password",
					Optional:             boolPtr(true),
				},
			},
		})
	}

	// 리소스 요청/제한 설정
	resources := corev1.ResourceRequirements{}
	if jobConfig.CPU != "" || jobConfig.Memory != "" {
		resources.Requests = corev1.ResourceList{}
		if jobConfig.CPU != "" {
			resources.Requests[corev1.ResourceCPU] = resource.MustParse(jobConfig.CPU)
		}
		if jobConfig.Memory != "" {
			resources.Requests[corev1.ResourceMemory] = resource.MustParse(jobConfig.Memory)
		}
	}
	if jobConfig.CPULimit != "" || jobConfig.MemoryLimit != "" {
		resources.Limits = corev1.ResourceList{}
		if jobConfig.CPULimit != "" {
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(jobConfig.CPULimit)
		}
		if jobConfig.MemoryLimit != "" {
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(jobConfig.MemoryLimit)
		}
	}

	// ImagePullPolicy 설정
	imagePullPolicy := corev1.PullIfNotPresent
	if jobConfig.ImagePullPolicy != "" {
		switch jobConfig.ImagePullPolicy {
		case "Always":
			imagePullPolicy = corev1.PullAlways
		case "Never":
			imagePullPolicy = corev1.PullNever
		}
	}

	// Job 스펙 생성
	ttlSeconds := jobConfig.TTLAfterFinished
	if ttlSeconds == 0 {
		ttlSeconds = 300 // 기본 5분
	}

	backoffLimit := jobConfig.BackoffLimit
	if backoffLimit == 0 {
		backoffLimit = 3
	}

	activeDeadlineSeconds := jobConfig.TimeoutSeconds
	if activeDeadlineSeconds == 0 {
		activeDeadlineSeconds = 3600 // 기본 1시간
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":          "conduix-batch",
				"workflow-id":  workflow.ID,
				"execution-id": execution.ID,
				"managed-by":   "conduix-control-plane",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlSeconds,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "conduix-batch",
						"workflow-id":  workflow.ID,
						"execution-id": execution.ID,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "batch-runner",
							Image:           image,
							ImagePullPolicy: imagePullPolicy,
							Env:             envVars,
							Resources:       resources,
						},
					},
				},
			},
		},
	}

	// NodeSelector 설정
	if len(jobConfig.NodeSelector) > 0 {
		job.Spec.Template.Spec.NodeSelector = jobConfig.NodeSelector
	}

	// ServiceAccount 설정
	if jobConfig.ServiceAccount != "" {
		job.Spec.Template.Spec.ServiceAccountName = jobConfig.ServiceAccount
	}

	// Job 생성
	createdJob, err := s.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create kubernetes job: %w", err)
	}

	fmt.Printf("[KubernetesJobService] Created batch job: %s/%s\n", namespace, createdJob.Name)
	return createdJob.Name, nil
}

// WatchJobCompletion Job 완료를 감시하고 콜백 호출 (폴백용)
// Job Pod에서 콜백을 보내지 못하는 경우를 대비
func (s *KubernetesJobService) WatchJobCompletion(
	ctx context.Context,
	jobName string,
	namespace string,
	onComplete func(result *types.JobExecutionResult),
) {
	if namespace == "" {
		namespace = s.namespace
	}

	// 중복 감시 방지
	s.mu.Lock()
	if cancel, exists := s.activeWatchers[jobName]; exists {
		cancel()
	}
	watchCtx, cancel := context.WithCancel(ctx)
	s.activeWatchers[jobName] = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.activeWatchers, jobName)
		s.mu.Unlock()
	}()

	// Watch 설정
	watcher, err := s.clientset.BatchV1().Jobs(namespace).Watch(watchCtx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", jobName),
	})
	if err != nil {
		fmt.Printf("[KubernetesJobService] Failed to watch job %s: %v\n", jobName, err)
		return
	}
	defer watcher.Stop()

	for {
		select {
		case <-watchCtx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}

			if event.Type == watch.Modified || event.Type == watch.Deleted {
				job, ok := event.Object.(*batchv1.Job)
				if !ok {
					continue
				}

				// Job 완료 확인
				if isJobComplete(job) {
					result := s.buildJobResult(watchCtx, job, namespace)
					if onComplete != nil {
						onComplete(result)
					}
					return
				}
			}
		}
	}
}

// CancelJobWatch Job 감시 취소
func (s *KubernetesJobService) CancelJobWatch(jobName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, exists := s.activeWatchers[jobName]; exists {
		cancel()
		delete(s.activeWatchers, jobName)
	}
}

// GetJobStatus Job 상태 조회
func (s *KubernetesJobService) GetJobStatus(ctx context.Context, jobName, namespace string) (*batchv1.JobStatus, error) {
	if namespace == "" {
		namespace = s.namespace
	}

	job, err := s.clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	return &job.Status, nil
}

// GetPodLogs Pod 로그 조회
func (s *KubernetesJobService) GetPodLogs(ctx context.Context, jobName, namespace string, tailLines int64) (string, error) {
	if namespace == "" {
		namespace = s.namespace
	}

	// Job의 Pod 찾기
	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}

	// 가장 최근 Pod의 로그 가져오기
	pod := pods.Items[len(pods.Items)-1]

	logOptions := &corev1.PodLogOptions{}
	if tailLines > 0 {
		logOptions.TailLines = &tailLines
	}

	req := s.clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, logOptions)
	logs, err := req.DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %w", err)
	}

	return string(logs), nil
}

// DeleteJob Job 삭제
func (s *KubernetesJobService) DeleteJob(ctx context.Context, jobName, namespace string) error {
	if namespace == "" {
		namespace = s.namespace
	}

	propagationPolicy := metav1.DeletePropagationBackground
	err := s.clientset.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	fmt.Printf("[KubernetesJobService] Deleted job: %s/%s\n", namespace, jobName)
	return nil
}

// buildJobResult Job 결과 생성
func (s *KubernetesJobService) buildJobResult(ctx context.Context, job *batchv1.Job, namespace string) *types.JobExecutionResult {
	result := &types.JobExecutionResult{
		JobName:     job.Name,
		StartedAt:   job.CreationTimestamp.Time,
		CompletedAt: time.Now(),
	}

	// Labels에서 ID 추출
	if labels := job.Labels; labels != nil {
		result.WorkflowID = labels["workflow-id"]
		result.ExecutionID = labels["execution-id"]
	}

	// 상태 판단
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			result.Status = types.JobStatusCompleted
		} else if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			result.Status = types.JobStatusFailed
			result.ErrorMessage = condition.Message
		}
	}

	// Pod 이름 가져오기
	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err == nil && len(pods.Items) > 0 {
		result.PodName = pods.Items[len(pods.Items)-1].Name
	}

	// Duration 계산
	if result.CompletedAt.After(result.StartedAt) {
		result.DurationMs = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
	}

	return result
}

// ParseJobConfig JSON 문자열에서 JobConfig 파싱
func ParseJobConfig(configJSON string) (*types.JobConfig, error) {
	if configJSON == "" {
		return nil, nil
	}

	var config types.JobConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("failed to parse job config: %w", err)
	}

	return &config, nil
}

// isJobComplete Job 완료 여부 확인
func isJobComplete(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) &&
			condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// truncateID ID를 지정된 길이로 잘라서 반환
func truncateID(id string, length int) string {
	if len(id) <= length {
		return id
	}
	return id[:length]
}

// boolPtr bool 포인터 반환 헬퍼
func boolPtr(b bool) *bool {
	return &b
}
