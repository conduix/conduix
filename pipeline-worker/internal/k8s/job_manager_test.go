package k8s

import (
	"context"
	"fmt"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/conduix/conduix/shared/types"
)

func newTestJobManager() (*JobManager, *fake.Clientset) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	jm := NewJobManager(client, "http://localhost:8080", "conduix/runner:latest")
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
	jm := NewJobManager(client, "http://localhost:8080", "") // 이미지 없음

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

// envToMap 환경변수 슬라이스를 맵으로 변환 (테스트 헬퍼)
func envToMap(envs []corev1.EnvVar) map[string]string {
	m := make(map[string]string)
	for _, e := range envs {
		m[e.Name] = e.Value
	}
	return m
}
