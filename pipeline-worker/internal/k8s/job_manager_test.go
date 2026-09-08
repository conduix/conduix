package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/conduix/conduix/shared/types"
)

func newTestJobManager() (*JobManager, *fake.Clientset) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	jm := NewJobManager(client, "http://localhost:8080", "conduix/runner:latest", nil, nil)
	return jm, fakeClient
}

func TestCreateBatchJob(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	spec := &JobSpec{
		ExecutionID:     "exec-001",
		WorkflowID:      "wf-001",
		PipelinesConfig: `[{"id":"p1","name":"test"}]`,
		JobConfig:       types.DefaultJobConfig(),
	}

	job, err := jm.CreateBatchJob(ctx, spec)
	if err != nil {
		t.Fatalf("CreateBatchJob failed: %v", err)
	}

	if job == nil {
		t.Fatal("job should not be nil")
	}

	// Job이 실제 생성되었는지 확인
	jobs, err := fakeClient.BatchV1().Jobs("conduix").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List jobs failed: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs.Items))
	}

	createdJob := jobs.Items[0]

	// 레이블 확인
	if createdJob.Labels["app.kubernetes.io/managed-by"] != "conduix-worker" {
		t.Errorf("expected managed-by label, got %v", createdJob.Labels)
	}
	if createdJob.Labels["conduix.io/workflow-id"] != "wf-001" {
		t.Errorf("expected workflow-id label wf-001, got %s", createdJob.Labels["conduix.io/workflow-id"])
	}

	// 컨테이너 환경변수 확인
	container := createdJob.Spec.Template.Spec.Containers[0]
	envMap := envToMap(container.Env)
	if envMap["EXECUTION_MODE"] != "batch" {
		t.Errorf("expected EXECUTION_MODE=batch, got %s", envMap["EXECUTION_MODE"])
	}
	if envMap["EXECUTION_ID"] != "exec-001" {
		t.Errorf("expected EXECUTION_ID=exec-001, got %s", envMap["EXECUTION_ID"])
	}
	if envMap["WORKFLOW_ID"] != "wf-001" {
		t.Errorf("expected WORKFLOW_ID=wf-001, got %s", envMap["WORKFLOW_ID"])
	}
	if container.Image != "conduix/runner:latest" {
		t.Errorf("expected image conduix/runner:latest, got %s", container.Image)
	}
	// 파티션 미지정 → ASSIGNED_PARTITIONS env 없음(전체 실행).
	if _, ok := envMap["ASSIGNED_PARTITIONS"]; ok {
		t.Errorf("파티션 미지정인데 ASSIGNED_PARTITIONS env 존재: %v", envMap["ASSIGNED_PARTITIONS"])
	}
}

// 파티션 분산: AssignedPartitions 지정 시 Job env 로 콤마 결합돼 전달된다.
// 같은 execution 의 재위임(claim 조기 만료로 발생)은 동일 이름 Job Create 가 AlreadyExists
// 로 실패한다. 이때 새로 만들지 않고 이미 도는 Job 을 채택(adopt)해 반환해야 한다 — 아니면
// 완주 중인 Job 이 "생성 실패" error 로 보고되어 status 가 뒤집힌다.
func TestCreateBatchJob_AdoptOnAlreadyExists(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	spec := &JobSpec{
		ExecutionID:     "exec-dup",
		WorkflowID:      "wf-dup",
		PipelinesConfig: `[{"id":"p1","name":"test"}]`,
		JobConfig:       types.DefaultJobConfig(),
	}

	first, err := jm.CreateBatchJob(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateBatchJob failed: %v", err)
	}

	// 재위임: 같은 spec → 같은 Job 이름 → AlreadyExists → adopt.
	second, err := jm.CreateBatchJob(ctx, spec)
	if err != nil {
		t.Fatalf("재위임은 adopt 로 성공해야 하는데 error: %v", err)
	}
	if second.Name != first.Name {
		t.Fatalf("adopt 된 Job 이름이 다르다: first=%s second=%s", first.Name, second.Name)
	}

	// Job 은 여전히 하나뿐이어야 한다(중복 생성 없음).
	jobs, _ := fakeClient.BatchV1().Jobs("conduix").List(ctx, metav1.ListOptions{})
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job after re-delegation, got %d", len(jobs.Items))
	}
}

// JobConfig 에 timeout 이 없으면 env DEFAULT_JOB_TIMEOUT_SECONDS 를, 그것도 없으면 1시간을 쓴다.
func TestEnvDefaultTimeoutSeconds(t *testing.T) {
	t.Setenv("DEFAULT_JOB_TIMEOUT_SECONDS", "")
	if got := envDefaultTimeoutSeconds(); got != defaultJobTimeoutSeconds {
		t.Errorf("빈 env: want %d, got %d", defaultJobTimeoutSeconds, got)
	}

	t.Setenv("DEFAULT_JOB_TIMEOUT_SECONDS", "7200")
	if got := envDefaultTimeoutSeconds(); got != 7200 {
		t.Errorf("env=7200: want 7200, got %d", got)
	}

	// 잘못된 값(음수/비숫자)은 무시하고 기본값 폴백.
	t.Setenv("DEFAULT_JOB_TIMEOUT_SECONDS", "-5")
	if got := envDefaultTimeoutSeconds(); got != defaultJobTimeoutSeconds {
		t.Errorf("음수 env: want %d(fallback), got %d", defaultJobTimeoutSeconds, got)
	}
	t.Setenv("DEFAULT_JOB_TIMEOUT_SECONDS", "abc")
	if got := envDefaultTimeoutSeconds(); got != defaultJobTimeoutSeconds {
		t.Errorf("비숫자 env: want %d(fallback), got %d", defaultJobTimeoutSeconds, got)
	}
}

func TestCreateBatchJob_AssignedPartitions(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	spec := &JobSpec{
		ExecutionID:        "exec-sub-1",
		WorkflowID:         "wf-001",
		PipelinesConfig:    `[{"id":"p1","name":"test"}]`,
		JobConfig:          types.DefaultJobConfig(),
		AssignedPartitions: []string{"p-a", "p-b"},
	}
	if _, err := jm.CreateBatchJob(ctx, spec); err != nil {
		t.Fatalf("CreateBatchJob failed: %v", err)
	}

	jobs, _ := fakeClient.BatchV1().Jobs("conduix").List(ctx, metav1.ListOptions{})
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs.Items))
	}
	envMap := envToMap(jobs.Items[0].Spec.Template.Spec.Containers[0].Env)
	if envMap["ASSIGNED_PARTITIONS"] != "p-a,p-b" {
		t.Errorf("ASSIGNED_PARTITIONS = %q, want \"p-a,p-b\"", envMap["ASSIGNED_PARTITIONS"])
	}
}

func TestCreateBatchJobWithCustomImage(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	cfg := types.DefaultJobConfig()
	cfg.Image = "myregistry/custom-runner:v1.0"

	spec := &JobSpec{
		ExecutionID:     "exec-002",
		WorkflowID:      "wf-002",
		PipelinesConfig: `[]`,
		JobConfig:       cfg,
	}

	job, err := jm.CreateBatchJob(ctx, spec)
	if err != nil {
		t.Fatalf("CreateBatchJob failed: %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "myregistry/custom-runner:v1.0" {
		t.Errorf("expected custom image, got %s", container.Image)
	}
}

func TestCreateBatchJobNoImage(t *testing.T) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	jm := NewJobManager(client, "http://localhost:8080", "", nil, nil) // 이미지 없음

	spec := &JobSpec{
		ExecutionID:     "exec-003",
		WorkflowID:      "wf-003",
		PipelinesConfig: `[]`,
		JobConfig:       types.JobConfig{}, // 이미지 미지정
	}

	_, err := jm.CreateBatchJob(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error when no image specified")
	}
}

func TestDeleteJob(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	// Job 생성
	spec := &JobSpec{
		ExecutionID:     "exec-del",
		WorkflowID:      "wf-del",
		PipelinesConfig: `[]`,
		JobConfig:       types.DefaultJobConfig(),
	}
	job, err := jm.CreateBatchJob(ctx, spec)
	if err != nil {
		t.Fatalf("CreateBatchJob failed: %v", err)
	}

	// 삭제
	err = jm.DeleteJob(ctx, "", job.Name)
	if err != nil {
		t.Fatalf("DeleteJob failed: %v", err)
	}

	// 삭제 확인
	jobs, _ := fakeClient.BatchV1().Jobs("conduix").List(ctx, metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", len(jobs.Items))
	}
}

func TestDeleteJobNotFound(t *testing.T) {
	jm, _ := newTestJobManager()

	// 존재하지 않는 Job 삭제 - 에러 없어야 함
	err := jm.DeleteJob(context.Background(), "", "nonexistent-job")
	if err != nil {
		t.Fatalf("DeleteJob should not error for not found: %v", err)
	}
}

func TestGetJobStatus(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	// Job 직접 생성 (상태 포함)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-status",
			Namespace: "conduix",
		},
		Status: batchv1.JobStatus{
			Active:    1,
			Succeeded: 0,
			Failed:    0,
		},
	}
	_, err := fakeClient.BatchV1().Jobs("conduix").Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create job failed: %v", err)
	}

	status, err := jm.GetJobStatus(ctx, "", "test-job-status")
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}

	if status.Status != "running" {
		t.Errorf("expected status running, got %s", status.Status)
	}
	if status.Active != 1 {
		t.Errorf("expected 1 active, got %d", status.Active)
	}
}

func TestListJobs(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	// 2개 Job 생성
	for i := range 2 {
		spec := &JobSpec{
			ExecutionID:     fmt.Sprintf("exec-%d", i),
			WorkflowID:      fmt.Sprintf("wf-%d", i),
			PipelinesConfig: `[]`,
			JobConfig:       types.DefaultJobConfig(),
		}
		_, err := jm.CreateBatchJob(ctx, spec)
		if err != nil {
			t.Fatalf("CreateBatchJob %d failed: %v", i, err)
		}
	}

	jobs, err := jm.ListJobs(ctx, "")
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestCreateCronJob(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	spec := &CronJobSpec{
		WorkflowID:      "wf-cron-001",
		CronExpression:  "0 * * * *",
		PipelinesConfig: `[{"id":"p1"}]`,
		JobConfig:       types.DefaultJobConfig(),
	}

	cronJob, err := jm.CreateCronJob(ctx, spec)
	if err != nil {
		t.Fatalf("CreateCronJob failed: %v", err)
	}

	if cronJob.Spec.Schedule != "0 * * * *" {
		t.Errorf("expected schedule '0 * * * *', got %s", cronJob.Spec.Schedule)
	}

	// CronJob이 생성되었는지 확인
	cronJobs, _ := fakeClient.BatchV1().CronJobs("conduix").List(ctx, metav1.ListOptions{})
	if len(cronJobs.Items) != 1 {
		t.Fatalf("expected 1 cronjob, got %d", len(cronJobs.Items))
	}
}

func TestDeleteCronJob(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	spec := &CronJobSpec{
		WorkflowID:      "wf-cron-del",
		CronExpression:  "0 2 * * *",
		PipelinesConfig: `[]`,
		JobConfig:       types.DefaultJobConfig(),
	}

	cronJob, err := jm.CreateCronJob(ctx, spec)
	if err != nil {
		t.Fatalf("CreateCronJob failed: %v", err)
	}

	err = jm.DeleteCronJob(ctx, "", cronJob.Name)
	if err != nil {
		t.Fatalf("DeleteCronJob failed: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-workflow-001", "my-workflow-001"},
		{"MY_WORKFLOW", "my-workflow"},
		{"test@#$name", "testname"},
		{"-leading-trailing-", "leading-trailing"},
		{"a550e8400-e29b-41d4-a716", "a550e8400-e29b-41d4-a716"},
	}

	for _, tc := range tests {
		result := sanitizeName(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeName(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestSanitizeLabel(t *testing.T) {
	// 63자 초과 테스트
	long := "a" + string(make([]byte, 100))
	result := sanitizeLabel(long)
	if len(result) > 63 {
		t.Errorf("sanitizeLabel should truncate to 63 chars, got %d", len(result))
	}
}

// realtime 재위임(claim 조기 만료)은 동일 이름 Deployment Create 가 AlreadyExists 로 실패한다.
// batch 와 동일하게 이미 도는 Deployment 를 채택(adopt)해야 execution 이 error 로 뒤집히지 않는다.
func TestCreateStreamingDeployment_AdoptOnAlreadyExists(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()
	spec := &StreamingSpec{
		ExecutionID:     "rt-dup",
		WorkflowID:      "wf-rt-dup",
		PipelinesConfig: `[{"id":"p1"}]`,
		JobConfig:       types.DefaultJobConfig(),
		RunnerVersionID: "rv-1",
	}
	first, err := jm.CreateStreamingDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateStreamingDeployment failed: %v", err)
	}
	second, err := jm.CreateStreamingDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("재위임은 adopt 로 성공해야 하는데 error: %v", err)
	}
	if second.Name != first.Name {
		t.Fatalf("adopt 된 Deployment 이름이 다르다: first=%s second=%s", first.Name, second.Name)
	}
	deps, _ := fakeClient.AppsV1().Deployments("conduix").List(ctx, metav1.ListOptions{})
	if len(deps.Items) != 1 {
		t.Fatalf("expected 1 deployment after re-delegation, got %d", len(deps.Items))
	}
}

func TestCreateStreamingDeployment(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	spec := &StreamingSpec{
		ExecutionID:        "rt-exec-1",
		WorkflowID:         "wf-rt",
		PipelinesConfig:    `[{"id":"p1"}]`,
		JobConfig:          types.DefaultJobConfig(),
		AssignedPartitions: []string{"0", "1"},
		RunnerVersionID:    "rv-1",
	}

	dep, err := jm.CreateStreamingDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateStreamingDeployment failed: %v", err)
	}
	if dep.Name != "conduix-rt-rt-exec-1" {
		t.Errorf("deployment name = %q, want conduix-rt-rt-exec-1", dep.Name)
	}
	if *dep.Spec.Replicas != 1 {
		t.Errorf("replicas = %d, want 1", *dep.Spec.Replicas)
	}

	ps := dep.Spec.Template.Spec
	if ps.RestartPolicy != corev1.RestartPolicyAlways {
		t.Errorf("restart policy = %s, want Always", ps.RestartPolicy)
	}
	c := ps.Containers[0]
	envMap := envToMap(c.Env)
	if envMap["EXECUTION_MODE"] != "streaming" {
		t.Errorf("EXECUTION_MODE = %q, want streaming", envMap["EXECUTION_MODE"])
	}
	if envMap["ASSIGNED_PARTITIONS"] != "0,1" {
		t.Errorf("ASSIGNED_PARTITIONS = %q, want 0,1", envMap["ASSIGNED_PARTITIONS"])
	}
	// native stage 바이너리 주입: RunnerVersionID 지정 시 initContainer 가 붙는다.
	if len(ps.InitContainers) == 0 {
		t.Error("expected fetch-runner initContainer when RunnerVersionID set")
	}
	if c.LivenessProbe == nil || c.ReadinessProbe == nil {
		t.Error("streaming container must have liveness/readiness probes")
	}
	if len(c.Ports) == 0 || c.Ports[0].ContainerPort != streamingHealthPort {
		t.Errorf("expected health container port %d", streamingHealthPort)
	}

	// execution-id 셀렉터로 조회 가능해야 한다(명령 전송 시 pod 발견 경로).
	if dep.Spec.Selector.MatchLabels["conduix.io/execution-id"] != "rt-exec-1" {
		t.Errorf("selector execution-id = %q, want rt-exec-1", dep.Spec.Selector.MatchLabels["conduix.io/execution-id"])
	}

	deps, _ := fakeClient.AppsV1().Deployments("conduix").List(ctx, metav1.ListOptions{})
	if len(deps.Items) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deps.Items))
	}
}

func TestDeleteStreamingDeployment(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	dep, err := jm.CreateStreamingDeployment(ctx, &StreamingSpec{
		ExecutionID:     "rt-del",
		WorkflowID:      "wf-rt",
		PipelinesConfig: `[]`,
		JobConfig:       types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateStreamingDeployment failed: %v", err)
	}
	if err := jm.DeleteStreamingDeployment(ctx, "", dep.Name); err != nil {
		t.Fatalf("DeleteStreamingDeployment failed: %v", err)
	}
	// 없는 것 삭제는 NotFound 무시 → nil.
	if err := jm.DeleteStreamingDeployment(ctx, "", dep.Name); err != nil {
		t.Fatalf("DeleteStreamingDeployment on missing should be nil, got %v", err)
	}
}

func TestUpdateStreamingDeployment(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	dep, err := jm.CreateStreamingDeployment(ctx, &StreamingSpec{
		ExecutionID:     "rt-roll",
		WorkflowID:      "wf-rt",
		PipelinesConfig: `[]`,
		JobConfig:       types.DefaultJobConfig(),
		RunnerVersionID: "rv-old",
	})
	if err != nil {
		t.Fatalf("CreateStreamingDeployment failed: %v", err)
	}

	// Recreate 전략이어야 rolling 중 구/신 pod 겹침이 없다(Q4 이중소비 방지).
	if dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("strategy = %s, want Recreate", dep.Spec.Strategy.Type)
	}

	if err := jm.UpdateStreamingDeployment(ctx, "", dep.Name, "rv-new"); err != nil {
		t.Fatalf("UpdateStreamingDeployment failed: %v", err)
	}

	updated, _ := jm.client.Clientset().AppsV1().Deployments("conduix").Get(ctx, dep.Name, metav1.GetOptions{})
	var initCmd string
	for _, ic := range updated.Spec.Template.Spec.InitContainers {
		if ic.Name == fetchRunnerContainerName {
			initCmd = ic.Command[len(ic.Command)-1]
		}
	}
	if initCmd == "" {
		t.Fatal("fetch-runner initContainer not found after update")
	}
	// 새 versionID 로 fetch URL 이 교체돼야 한다.
	if !strings.Contains(initCmd, "rv-new") || strings.Contains(initCmd, "rv-old") {
		t.Errorf("init command should fetch rv-new, got: %s", initCmd)
	}

	// versionID 없이 rolling → 에러.
	if err := jm.UpdateStreamingDeployment(ctx, "", dep.Name, ""); err == nil {
		t.Error("expected error when runnerVersionID empty")
	}
}

func TestStreamingDeploymentExists(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	// 없으면 false, 에러 없음.
	exists, err := jm.StreamingDeploymentExists(ctx, "conduix", "wf-x", "exec-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected not-exists before create")
	}

	dep, err := jm.CreateStreamingDeployment(ctx, &StreamingSpec{
		ExecutionID: "exec-x", WorkflowID: "wf-x", PipelinesConfig: `[]`, JobConfig: types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateStreamingDeployment failed: %v", err)
	}

	exists, err = jm.StreamingDeploymentExists(ctx, "conduix", "wf-x", "exec-x")
	if err != nil || !exists {
		t.Fatalf("expected exists=true after create, got exists=%v err=%v", exists, err)
	}

	// 삭제 후 다시 false.
	if err := jm.DeleteStreamingDeployment(ctx, "", dep.Name); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	exists, _ = jm.StreamingDeploymentExists(ctx, "conduix", "wf-x", "exec-x")
	if exists {
		t.Error("expected not-exists after delete")
	}
}

func TestStreamingCommandURL(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	// running·IP 있는 pod 를 execution-id 라벨로 생성.
	_, err := fakeClient.CoreV1().Pods("conduix").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rt-pod-1",
			Namespace: "conduix",
			Labels:    map[string]string{"conduix.io/execution-id": "rt-exec-1"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.1.2.3"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pod failed: %v", err)
	}

	url, err := jm.StreamingCommandURL(ctx, "conduix", "rt-exec-1")
	if err != nil {
		t.Fatalf("StreamingCommandURL failed: %v", err)
	}
	want := fmt.Sprintf("http://10.1.2.3:%d/commands", streamingHealthPort)
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}

	// 매칭 pod 없으면 에러.
	if _, err := jm.StreamingCommandURL(ctx, "conduix", "no-such-exec"); err == nil {
		t.Error("expected error when no running pod matches execution-id")
	}
}

// batch Job pod 도 execution-id 라벨을 달기 때문에 같은 조회로 /monitoring 을 pull 할 수 있어야 한다.
// 이 경로가 없으면 batch 실행 중 라이브 모니터링이 항상 비었다.
func TestExecutionPodURL_MonitoringPath(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	_, err := fakeClient.CoreV1().Pods("conduix").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "batch-pod-1",
			Namespace: "conduix",
			Labels:    map[string]string{"conduix.io/execution-id": "batch-exec-1"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.4.5.6"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pod failed: %v", err)
	}

	url, err := jm.ExecutionPodURL(ctx, "conduix", "batch-exec-1", "/monitoring")
	if err != nil {
		t.Fatalf("ExecutionPodURL failed: %v", err)
	}
	want := fmt.Sprintf("http://10.4.5.6:%d/monitoring", streamingHealthPort)
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// namespace 를 비우면 JobManager 기본 namespace 로 조회해야 한다.
// batch 는 runningExecs 에 등록되지 않아 agent 가 namespace 를 모른 채 호출한다.
func TestExecutionPodURL_DefaultNamespace(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	_, err := fakeClient.CoreV1().Pods("conduix").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "batch-pod-2",
			Namespace: "conduix",
			Labels:    map[string]string{"conduix.io/execution-id": "batch-exec-2"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.7.8.9"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pod failed: %v", err)
	}

	url, err := jm.ExecutionPodURL(ctx, "", "batch-exec-2", "/monitoring")
	if err != nil {
		t.Fatalf("ExecutionPodURL with empty namespace failed: %v", err)
	}
	if want := fmt.Sprintf("http://10.7.8.9:%d/monitoring", streamingHealthPort); url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// Pending pod(IP 미배정)는 대상이 아니어야 한다 — 붙어도 연결이 실패한다.
func TestExecutionPodURL_SkipsPendingPod(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	_, err := fakeClient.CoreV1().Pods("conduix").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "conduix",
			Labels:    map[string]string{"conduix.io/execution-id": "exec-pending"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pod failed: %v", err)
	}

	if _, err := jm.ExecutionPodURL(ctx, "conduix", "exec-pending", "/monitoring"); err == nil {
		t.Error("expected error for pending pod without IP")
	}
}

// envToMap 환경변수 슬라이스를 맵으로 변환 (테스트 헬퍼)
func envToMap(envs []corev1.EnvVar) map[string]string {
	m := make(map[string]string)
	for _, e := range envs {
		m[e.Name] = e.Value
	}
	return m
}

// envFrom 주입: runnerEnvFromSecrets/ConfigMaps 로 지정한 소스가 batch Job 컨테이너에
// EnvFrom 으로 붙어야 파이프라인 config 의 ${VAR} 를 실행 파드에서 해소할 수 있다.
func TestCreateBatchJob_EnvFromInjection(t *testing.T) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	jm := NewJobManager(client, "http://cp:8080", "runner:latest",
		[]string{"pipeline-secrets", ""}, []string{"pipeline-config"}) // 빈 이름은 무시돼야 함

	job, err := jm.CreateBatchJob(context.Background(), &JobSpec{
		ExecutionID: "e1", WorkflowID: "w1",
		PipelinesConfig: `[{"id":"p1"}]`, JobConfig: types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateBatchJob: %v", err)
	}

	envFrom := job.Spec.Template.Spec.Containers[0].EnvFrom
	if len(envFrom) != 2 {
		t.Fatalf("expected 2 envFrom sources (empty name skipped), got %d", len(envFrom))
	}
	if envFrom[0].SecretRef == nil || envFrom[0].SecretRef.Name != "pipeline-secrets" {
		t.Errorf("expected secret ref pipeline-secrets, got %+v", envFrom[0])
	}
	if envFrom[0].SecretRef.Optional == nil || !*envFrom[0].SecretRef.Optional {
		t.Errorf("secret ref should be optional (missing source must not block pod start)")
	}
	if envFrom[1].ConfigMapRef == nil || envFrom[1].ConfigMapRef.Name != "pipeline-config" {
		t.Errorf("expected configmap ref pipeline-config, got %+v", envFrom[1])
	}
}

// 소스 미지정(nil) 시 envFrom 이 비어야 한다 — 기존 동작 보존.
func TestCreateBatchJob_NoEnvFromByDefault(t *testing.T) {
	jm, _ := newTestJobManager()
	job, err := jm.CreateBatchJob(context.Background(), &JobSpec{
		ExecutionID: "e1", WorkflowID: "w1",
		PipelinesConfig: `[{"id":"p1"}]`, JobConfig: types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateBatchJob: %v", err)
	}
	if len(job.Spec.Template.Spec.Containers[0].EnvFrom) != 0 {
		t.Errorf("expected no envFrom by default")
	}
}

// 가변 태그(:latest/:main)를 쓰므로 기본값은 Always 여야 한다. IfNotPresent 가 기본이면
// 노드 캐시의 옛 이미지가 재사용돼 방금 배포한 수정이 실행 pod 에 반영되지 않는다.
func TestResolvePullPolicy(t *testing.T) {
	cases := map[string]corev1.PullPolicy{
		"":             corev1.PullAlways,
		"Always":       corev1.PullAlways,
		"IfNotPresent": corev1.PullIfNotPresent,
		"Never":        corev1.PullNever,
		"바보같은값":        corev1.PullAlways,
	}
	for in, want := range cases {
		if got := resolvePullPolicy(in); got != want {
			t.Errorf("resolvePullPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

// batch Job / streaming Deployment 양쪽 컨테이너에 실제로 반영돼야 한다.
func TestPullPolicyAppliedToPods(t *testing.T) {
	jm, _ := newTestJobManager()
	ctx := context.Background()

	job, err := jm.CreateBatchJob(ctx, &JobSpec{
		ExecutionID: "pp-1", WorkflowID: "w1",
		PipelinesConfig: `[{"id":"p1"}]`, JobConfig: types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateBatchJob: %v", err)
	}
	if got := job.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullAlways {
		t.Errorf("batch Job pullPolicy = %q, want Always", got)
	}

	dep, err := jm.CreateStreamingDeployment(ctx, &StreamingSpec{
		ExecutionID: "pp-2", WorkflowID: "w1",
		PipelinesConfig: `[{"id":"p1"}]`, JobConfig: types.DefaultJobConfig(),
	})
	if err != nil {
		t.Fatalf("CreateStreamingDeployment: %v", err)
	}
	if got := dep.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullAlways {
		t.Errorf("streaming Deployment pullPolicy = %q, want Always", got)
	}
}

// agent 재시작으로 runningExecs 가 비어도 K8s 에 남은 streaming Deployment 를 찾을 수 있어야 한다.
// 이게 없으면 stop 이 아무 일도 못 하고 pod 이 계속 돌아 "DB 는 stopped, 실제로는 running" 이 된다.
func TestFindStreamingDeployments(t *testing.T) {
	jm, fakeClient := newTestJobManager()
	ctx := context.Background()

	mk := func(name, wf, ex string) {
		_, err := fakeClient.AppsV1().Deployments("conduix").Create(ctx, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "conduix",
				Labels: map[string]string{
					"app.kubernetes.io/component": "streaming-runner",
					"conduix.io/workflow-id":      wf,
					"conduix.io/execution-id":     ex,
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	mk("conduix-rt-e1", "wf-a", "e1")
	mk("conduix-rt-e2", "wf-a", "e2")
	mk("conduix-rt-e9", "wf-b", "e9")

	// executionID 지정 → 그 실행만
	got, err := jm.FindStreamingDeployments(ctx, "conduix", "wf-a", "e1")
	if err != nil {
		t.Fatalf("by execution: %v", err)
	}
	if len(got) != 1 || got[0] != "conduix-rt-e1" {
		t.Errorf("by execution = %v, want [conduix-rt-e1]", got)
	}

	// executionID 없음 → workflow 의 모든 실행 (stop 재호출 시 고아 전부 정리)
	got, err = jm.FindStreamingDeployments(ctx, "conduix", "wf-a", "")
	if err != nil {
		t.Fatalf("by workflow: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("by workflow = %v, want 2 deployments", got)
	}

	// 다른 워크플로우는 섞이지 않아야 한다
	got, _ = jm.FindStreamingDeployments(ctx, "conduix", "wf-b", "")
	if len(got) != 1 || got[0] != "conduix-rt-e9" {
		t.Errorf("wf-b = %v, want [conduix-rt-e9]", got)
	}

	// 없는 워크플로우 → 빈 목록(에러 아님). stop 은 멱등해야 한다.
	got, err = jm.FindStreamingDeployments(ctx, "conduix", "wf-none", "")
	if err != nil || len(got) != 0 {
		t.Errorf("wf-none = %v, err=%v; want empty without error", got, err)
	}

	// 식별자가 둘 다 없으면 에러 — 전체 삭제 같은 사고를 막는다
	if _, err := jm.FindStreamingDeployments(ctx, "conduix", "", ""); err == nil {
		t.Error("expected error when both workflow and execution id are empty")
	}
}
