package k8s

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/conduix/conduix/shared/types"
)

func newTestDeploymentManager() (*DeploymentManager, *fake.Clientset) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	dm := NewDeploymentManager(client, "http://localhost:8080", "conduix/runner:latest")
	return dm, fakeClient
}

func TestCreateDeployment(t *testing.T) {
	dm, fakeClient := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-stream-001",
		PipelinesConfig: `[{"id":"p1","name":"streaming-test"}]`,
		Replicas:        2,
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	if deploy == nil {
		t.Fatal("deployment should not be nil")
	}

	// 레이블 확인
	if deploy.Labels["app.kubernetes.io/component"] != "streaming" {
		t.Errorf("expected component=streaming, got %s", deploy.Labels["app.kubernetes.io/component"])
	}

	// Replicas 확인
	if *deploy.Spec.Replicas != 2 {
		t.Errorf("expected 2 replicas, got %d", *deploy.Spec.Replicas)
	}

	// K8s에서 확인
	deployments, _ := fakeClient.AppsV1().Deployments("conduix").List(ctx, metav1.ListOptions{})
	if len(deployments.Items) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deployments.Items))
	}

	// 환경변수 확인
	container := deploy.Spec.Template.Spec.Containers[0]
	envMap := envToMap(container.Env)
	if envMap["EXECUTION_MODE"] != "streaming" {
		t.Errorf("expected EXECUTION_MODE=streaming, got %s", envMap["EXECUTION_MODE"])
	}
}

func TestCreateDeploymentWithResources(t *testing.T) {
	dm, _ := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-res",
		PipelinesConfig: `[]`,
		Replicas:        1,
		Resources: &types.ResourceRequirements{
			Requests: types.ResourceList{CPU: "500m", Memory: "512Mi"},
			Limits:   types.ResourceList{CPU: "1000m", Memory: "1Gi"},
		},
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	resources := deploy.Spec.Template.Spec.Containers[0].Resources
	if resources.Requests.Cpu().String() != "500m" {
		t.Errorf("expected CPU request 500m, got %s", resources.Requests.Cpu().String())
	}
	if resources.Limits.Memory().String() != "1Gi" {
		t.Errorf("expected memory limit 1Gi, got %s", resources.Limits.Memory().String())
	}
}

func TestCreateDeploymentDefaultReplicas(t *testing.T) {
	dm, _ := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-default-rep",
		PipelinesConfig: `[]`,
		Replicas:        0, // 기본값 사용
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	if *deploy.Spec.Replicas != 1 {
		t.Errorf("expected default 1 replica, got %d", *deploy.Spec.Replicas)
	}
}

func TestDeleteDeployment(t *testing.T) {
	dm, fakeClient := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-del-deploy",
		PipelinesConfig: `[]`,
		Replicas:        1,
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	err = dm.DeleteDeployment(ctx, "", deploy.Name)
	if err != nil {
		t.Fatalf("DeleteDeployment failed: %v", err)
	}

	deployments, _ := fakeClient.AppsV1().Deployments("conduix").List(ctx, metav1.ListOptions{})
	if len(deployments.Items) != 0 {
		t.Errorf("expected 0 deployments after delete, got %d", len(deployments.Items))
	}
}

func TestDeleteDeploymentNotFound(t *testing.T) {
	dm, _ := newTestDeploymentManager()

	err := dm.DeleteDeployment(context.Background(), "", "nonexistent")
	if err != nil {
		t.Fatalf("DeleteDeployment should not error for not found: %v", err)
	}
}

func TestScaleDeployment(t *testing.T) {
	dm, fakeClient := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-scale",
		PipelinesConfig: `[]`,
		Replicas:        1,
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	err = dm.ScaleDeployment(ctx, "", deploy.Name, 3)
	if err != nil {
		t.Fatalf("ScaleDeployment failed: %v", err)
	}

	// 스케일 확인
	updated, _ := fakeClient.AppsV1().Deployments("conduix").Get(ctx, deploy.Name, metav1.GetOptions{})
	if *updated.Spec.Replicas != 3 {
		t.Errorf("expected 3 replicas after scale, got %d", *updated.Spec.Replicas)
	}
}

func TestUpdateDeployment(t *testing.T) {
	dm, _ := newTestDeploymentManager()
	ctx := context.Background()

	spec := &DeploymentSpec{
		WorkflowID:      "wf-update",
		PipelinesConfig: `[{"id":"p1"}]`,
		Replicas:        1,
	}

	deploy, err := dm.CreateDeployment(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	// 이미지와 replicas 업데이트
	updated, err := dm.UpdateDeployment(ctx, "", deploy.Name, &DeploymentSpec{
		Replicas: 3,
		Image:    "conduix/runner:v2.0",
	})
	if err != nil {
		t.Fatalf("UpdateDeployment failed: %v", err)
	}

	if *updated.Spec.Replicas != 3 {
		t.Errorf("expected 3 replicas, got %d", *updated.Spec.Replicas)
	}

	if updated.Spec.Template.Spec.Containers[0].Image != "conduix/runner:v2.0" {
		t.Errorf("expected updated image, got %s", updated.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestListDeployments(t *testing.T) {
	dm, _ := newTestDeploymentManager()
	ctx := context.Background()

	// 2개 Deployment 생성
	for _, wfID := range []string{"wf-list-1", "wf-list-2"} {
		spec := &DeploymentSpec{
			WorkflowID:      wfID,
			PipelinesConfig: `[]`,
			Replicas:        1,
		}
		_, err := dm.CreateDeployment(ctx, spec)
		if err != nil {
			t.Fatalf("CreateDeployment %s failed: %v", wfID, err)
		}
	}

	deployments, err := dm.ListDeployments(ctx, "")
	if err != nil {
		t.Fatalf("ListDeployments failed: %v", err)
	}

	if len(deployments) != 2 {
		t.Errorf("expected 2 deployments, got %d", len(deployments))
	}
}

func TestCreateDeploymentNoImage(t *testing.T) {
	fakeClient := fake.NewClientset()
	client := NewClientWithInterface(fakeClient, "conduix")
	dm := NewDeploymentManager(client, "http://localhost:8080", "") // 이미지 없음

	spec := &DeploymentSpec{
		WorkflowID:      "wf-no-image",
		PipelinesConfig: `[]`,
		Replicas:        1,
	}

	_, err := dm.CreateDeployment(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error when no image specified")
	}
}
