package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/conduix/conduix/shared/types"
)

// managedByLabel은 worker가 생성한 Job임을 표시하는 라벨 값이다.
// 생성(CreateBatchJob)과 조회(ListJobs)가 반드시 동일 값을 써야 하므로 한 곳에서 관리한다.
const (
	labelManagedByKey = "app.kubernetes.io/managed-by"
	managedByValue    = "conduix-worker"
)

// streamingHealthPort 는 streaming pod 의 health/command REST 포트다.
// pipeline-batch-job config 기본값(HEALTH_PORT=8082)과 일치해야 한다 — probe·명령 전송 대상 포트.
const streamingHealthPort = 8082

// fetchRunnerContainerName 은 바이너리 주입 initContainer 이름이다.
// rolling(UpdateStreamingDeployment)에서 이 이름으로 initContainer 를 찾아 fetch URL 을 교체한다.
const fetchRunnerContainerName = "fetch-runner"

// JobManager K8s Job/CronJob 관리자
type JobManager struct {
	client          *Client
	controlPlaneURL string
	runnerImage     string
	// runnerEnvFrom: batch Job/streaming pod 에 envFrom 으로 주입할 Secret/ConfigMap 이름 목록.
	// 파이프라인 config 의 ${VAR}(예: DB 비밀번호, API 키)를 실행 파드에서 해소하려면 그 값이
	// 파드 env 로 존재해야 한다 — Job 은 CP/agent 와 별개 파드라 자동 상속되지 않으므로 명시 주입.
	// 비면(기본) envFrom 없음 → 평문 config 만 동작(기존 동작 보존).
	runnerEnvFrom []corev1.EnvFromSource
}

// NewJobManager JobManager 생성.
// envFromSecrets/envFromConfigMaps: 실행 파드에 envFrom 으로 붙일 Secret/ConfigMap 이름들(빈 슬라이스 허용).
func NewJobManager(client *Client, controlPlaneURL, runnerImage string, envFromSecrets, envFromConfigMaps []string) *JobManager {
	return &JobManager{
		client:          client,
		controlPlaneURL: controlPlaneURL,
		runnerImage:     runnerImage,
		runnerEnvFrom:   buildEnvFromSources(envFromSecrets, envFromConfigMaps),
	}
}

// buildEnvFromSources Secret/ConfigMap 이름 목록을 EnvFromSource 로 변환한다.
// optional=true: 목록에 있으나 클러스터에 없는 소스 때문에 파드가 기동 실패하지 않도록.
func buildEnvFromSources(secrets, configMaps []string) []corev1.EnvFromSource {
	sources := make([]corev1.EnvFromSource, 0, len(secrets)+len(configMaps))
	optional := true
	for _, name := range secrets {
		if name == "" {
			continue
		}
		sources = append(sources, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Optional:             &optional,
			},
		})
	}
	for _, name := range configMaps {
		if name == "" {
			continue
		}
		sources = append(sources, corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Optional:             &optional,
			},
		})
	}
	return sources
}

// JobSpec 배치 Job 생성용 파라미터
type JobSpec struct {
	ExecutionID        string
	WorkflowID         string
	AgentID            string // 이 Job 을 위임 생성한 agent(노드) — batch-job 이 결과 콜백에 담아 분산 현황 모니터링에 사용
	PipelinesConfig    string // JSON
	JobConfig          types.JobConfig
	AssignedPartitions []string // 파티션 분산: 이 Job 이 처리할 파티션 ID 부분집합(비면 전체)
	RunnerVersionID    string   // 있으면 native stage compile-in 바이너리를 CP 에서 initContainer 로 주입해 실행(레지스트리 push 없이). 비면 이미지 실행(현행).
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
		"app.kubernetes.io/name":      "conduix-worker",
		"app.kubernetes.io/component": "batch-job",
		labelManagedByKey:             managedByValue,
		"conduix.io/workflow-id":      sanitizeLabel(spec.WorkflowID),
		"conduix.io/execution-id":     sanitizeLabel(spec.ExecutionID),
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
	// 위임 agent 식별: batch-job 이 결과 콜백에 담아 "어느 노드가 이 Job 을 만들었는지" 노출.
	if spec.AgentID != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENT_ID", Value: spec.AgentID})
	}
	// 파티션 분산: 배정된 파티션이 있으면 Job 에 전달(batch-job 이 WithAssignedPartitions 로 사용).
	if len(spec.AssignedPartitions) > 0 {
		envVars = append(envVars, corev1.EnvVar{Name: "ASSIGNED_PARTITIONS", Value: strings.Join(spec.AssignedPartitions, ",")})
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
							EnvFrom:         m.runnerEnvFrom,
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

	// native stage compile-in 바이너리 주입(batch/streaming 공통 정책 — 한 곳에서 관리).
	m.injectRunnerBinary(&job.Spec.Template.Spec, spec.RunnerVersionID, image, pullPolicy)

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
		"app.kubernetes.io/name":      "conduix-worker",
		"app.kubernetes.io/component": "cron-job",
		labelManagedByKey:             managedByValue,
		"conduix.io/workflow-id":      sanitizeLabel(spec.WorkflowID),
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
									EnvFrom:   m.runnerEnvFrom,
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

// fetchRunnerCommand 는 RunnerVersion 바이너리를 CP 에서 받아 압축해제·설치하는 initContainer 명령이다.
// URL 은 versionID 로만 달라지므로 rolling 시 이 명령만 교체하면 새 바이너리로 재기동된다.
func (m *JobManager) fetchRunnerCommand(runnerVersionID, binMount string) []string {
	binURL := fmt.Sprintf("%s/api/v1/internal/runner/versions/%s/binary", m.controlPlaneURL, runnerVersionID)
	return []string{
		"sh", "-c",
		fmt.Sprintf("set -e; wget -q -O- %q | gunzip > %s/pipeline-batch-job && chmod +x %s/pipeline-batch-job",
			binURL, binMount, binMount),
	}
}

// injectRunnerBinary native stage compile-in 바이너리 주입(batch Job·streaming Deployment 공통).
// RunnerVersionID 가 있으면 CP 에서 바이너리를 받아 initContainer(fetch-runner)로 emptyDir 에 놓고,
// main container 는 그 바이너리를 실행한다. base 이미지(alpine)에 wget/gunzip/sh 내장 → 레지스트리 push 불필요.
// RunnerVersionID 가 비면 no-op(이미지 실행).
func (m *JobManager) injectRunnerBinary(ps *corev1.PodSpec, runnerVersionID, image string, pullPolicy corev1.PullPolicy) {
	if runnerVersionID == "" {
		return
	}
	const binMount = "/runner"
	ps.Volumes = append(ps.Volumes, corev1.Volume{
		Name:         "runner-bin",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	ps.InitContainers = append(ps.InitContainers, corev1.Container{
		Name:            fetchRunnerContainerName,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command:         m.fetchRunnerCommand(runnerVersionID, binMount),
		VolumeMounts:    []corev1.VolumeMount{{Name: "runner-bin", MountPath: binMount}},
	})
	ps.Containers[0].Command = []string{binMount + "/pipeline-batch-job"}
	ps.Containers[0].Args = nil // base 이미지 ENTRYPOINT/CMD 잔여 인자 제거
	ps.Containers[0].VolumeMounts = append(ps.Containers[0].VolumeMounts,
		corev1.VolumeMount{Name: "runner-bin", MountPath: binMount})
}

// StreamingSpec realtime streaming Deployment 생성용 파라미터.
// JobSpec 과 필드가 겹치지만 realtime 은 무한 실행이라 Job 이 아닌 Deployment 로 띄운다.
type StreamingSpec struct {
	ExecutionID        string
	WorkflowID         string
	AgentID            string
	PipelinesConfig    string // JSON
	JobConfig          types.JobConfig
	AssignedPartitions []string
	RunnerVersionID    string
}

// streamingDeploymentName realtime Deployment 이름. execution 단위(파티션 sub-execution 포함)로 고유.
func (m *JobManager) streamingDeploymentName(workflowID, executionID string) string {
	return "conduix-rt-" + sanitizeName(executionID)
}

// CreateStreamingDeployment realtime 파이프라인용 K8s Deployment 생성(무한 실행, RestartPolicy=Always).
// batch(CreateBatchJob)와 달리 완료 개념이 없어 Job 이 아닌 Deployment(replicas=1)로 상주시킨다.
// initContainer 바이너리 주입·env 는 batch 와 동일 정책(injectRunnerBinary 재사용).
func (m *JobManager) CreateStreamingDeployment(ctx context.Context, spec *StreamingSpec) (*appsv1.Deployment, error) {
	name := m.streamingDeploymentName(spec.WorkflowID, spec.ExecutionID)
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

	labels := map[string]string{
		"app.kubernetes.io/name":      "conduix-worker",
		"app.kubernetes.io/component": "streaming-runner",
		labelManagedByKey:             managedByValue,
		"conduix.io/workflow-id":      sanitizeLabel(spec.WorkflowID),
		"conduix.io/execution-id":     sanitizeLabel(spec.ExecutionID),
	}
	selector := map[string]string{"conduix.io/execution-id": sanitizeLabel(spec.ExecutionID)}

	callbackURL := fmt.Sprintf("%s/api/v1/internal/job-result", m.controlPlaneURL)
	envVars := []corev1.EnvVar{
		{Name: "EXECUTION_MODE", Value: "streaming"},
		{Name: "EXECUTION_ID", Value: spec.ExecutionID},
		{Name: "WORKFLOW_ID", Value: spec.WorkflowID},
		{Name: "PIPELINES_CONFIG", Value: spec.PipelinesConfig},
		{Name: "CONTROL_PLANE_URL", Value: m.controlPlaneURL},
		{Name: "CALLBACK_URL", Value: callbackURL},
	}
	if spec.AgentID != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENT_ID", Value: spec.AgentID})
	}
	if len(spec.AssignedPartitions) > 0 {
		envVars = append(envVars, corev1.EnvVar{Name: "ASSIGNED_PARTITIONS", Value: strings.Join(spec.AssignedPartitions, ",")})
	}

	pullPolicy := corev1.PullIfNotPresent
	switch cfg.ImagePullPolicy {
	case "Always":
		pullPolicy = corev1.PullAlways
	case "Never":
		pullPolicy = corev1.PullNever
	}

	replicas := int32(1)
	// checkpoint flush 여유를 위해 graceful 종료 시간을 넉넉히(runStreaming 이 SIGTERM 후 flush).
	gracePeriod := int64(60)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			// native 변경 rolling(S8) 시 구/신 pod 가 동시에 살면 같은 파티션을 이중 소비한다
			// (pod 내부에 source-level claim 이 없음 — Q4). Recreate 로 구 pod 를 완전히 종료
			// (checkpoint flush)한 뒤 신 pod 를 띄워 겹침을 제거한다. 짧은 공백은 있으나 신 pod 가
			// checkpoint offset 부터 재개하므로 무손실. Kafka 는 consumer group 이 겹침을 막지만
			// 비-Kafka 파티션 소스까지 안전하게 하려면 Recreate 가 정답.
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: &gracePeriod,
					Containers: []corev1.Container{
						{
							Name:            "streaming-runner",
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Env:             envVars,
							EnvFrom:         m.runnerEnvFrom,
							Resources:       buildResourceRequirements(cfg),
							// health/command REST(:8082). agent 가 이 포트로 stop/pause/resume 를 보낸다(W4).
							Ports: []corev1.ContainerPort{{Name: "health", ContainerPort: streamingHealthPort}},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(streamingHealthPort)},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       15,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt(streamingHealthPort)},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		},
	}

	if len(cfg.NodeSelector) > 0 {
		dep.Spec.Template.Spec.NodeSelector = cfg.NodeSelector
	}
	if cfg.ServiceAccount != "" {
		dep.Spec.Template.Spec.ServiceAccountName = cfg.ServiceAccount
	}

	m.injectRunnerBinary(&dep.Spec.Template.Spec, spec.RunnerVersionID, image, pullPolicy)

	created, err := m.client.Clientset().AppsV1().Deployments(namespace).Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create streaming deployment %s: %w", name, err)
	}
	return created, nil
}

// DeleteStreamingDeployment realtime Deployment 삭제(워크플로우 stop 시).
func (m *JobManager) DeleteStreamingDeployment(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = m.client.Namespace()
	}
	propagation := metav1.DeletePropagationBackground
	err := m.client.Clientset().AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete streaming deployment %s: %w", name, err)
	}
	return nil
}

// StreamingDeploymentExists 는 execution 의 streaming Deployment 가 실제로 존재하는지 K8s 에서 확인한다.
// reconcile 이 로컬 상태(runningExecs)가 아니라 K8s 실제 상태로 복구 여부를 판단하게 한다 —
// Deployment 가 외부 삭제/유실됐는데 agent 로컬엔 아직 "실행 중"으로 남아있는 경우를 잡는다.
func (m *JobManager) StreamingDeploymentExists(ctx context.Context, namespace, workflowID, executionID string) (bool, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}
	name := m.streamingDeploymentName(workflowID, executionID)
	_, err := m.client.Clientset().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get streaming deployment %s: %w", name, err)
	}
	return true, nil
}

// UpdateStreamingDeployment 는 실행 중 realtime Deployment 를 새 RunnerVersion 바이너리로 rolling 한다(S8).
// fetch-runner initContainer 의 URL(versionID)만 교체하면 pod template 이 바뀌어 K8s 가 재기동한다.
// Deployment 전략이 Recreate 이므로 구 pod 가 완전히 종료(checkpoint flush)된 뒤 신 pod 가 뜬다 — 겹침 없음(Q4).
// 신 pod 는 checkpoint offset 부터 재개하므로 무손실. versionID 가 비면(native 아님) rolling 대상 아님 → 에러.
func (m *JobManager) UpdateStreamingDeployment(ctx context.Context, namespace, name, runnerVersionID string) error {
	if runnerVersionID == "" {
		return fmt.Errorf("runner version id required for streaming rolling")
	}
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	deps := m.client.Clientset().AppsV1().Deployments(namespace)
	dep, err := deps.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get streaming deployment %s: %w", name, err)
	}

	const binMount = "/runner"
	found := false
	for i := range dep.Spec.Template.Spec.InitContainers {
		ic := &dep.Spec.Template.Spec.InitContainers[i]
		if ic.Name == fetchRunnerContainerName {
			ic.Command = m.fetchRunnerCommand(runnerVersionID, binMount)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("streaming deployment %s has no %s initContainer (not native-injected?)", name, fetchRunnerContainerName)
	}

	if _, err := deps.Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update streaming deployment %s: %w", name, err)
	}
	return nil
}

// StreamingCommandURL 은 execution-id 라벨로 streaming pod 를 찾아 command REST 엔드포인트 URL 을 만든다.
// pod IP 는 in-cluster 에서 직접 접근 가능하므로 service 없이 pod IP:health-port 로 명령을 보낸다(Q2).
// running·IP 배정된 pod 만 대상으로 한다. 없으면 에러(아직 스케줄 중이거나 rolling 교체 중일 수 있음).
func (m *JobManager) StreamingCommandURL(ctx context.Context, namespace, executionID string) (string, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}
	selector := "conduix.io/execution-id=" + sanitizeLabel(executionID)
	pods, err := m.client.Clientset().CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", fmt.Errorf("failed to list streaming pods (execution=%s): %w", executionID, err)
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			return fmt.Sprintf("http://%s:%d/commands", pod.Status.PodIP, streamingHealthPort), nil
		}
	}
	return "", fmt.Errorf("no running streaming pod with IP for execution=%s", executionID)
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
		LabelSelector: labelManagedByKey + "=" + managedByValue,
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
