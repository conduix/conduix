package monitor

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewResourceMonitor(t *testing.T) {
	fakeClient := fake.NewClientset()
	monitor := NewResourceMonitor(fakeClient, "conduix", 10*time.Second)

	if monitor == nil {
		t.Fatal("monitor should not be nil")
	}
	if monitor.namespace != "conduix" {
		t.Errorf("expected namespace conduix, got %s", monitor.namespace)
	}
	if monitor.interval != 10*time.Second {
		t.Errorf("expected interval 10s, got %v", monitor.interval)
	}
}

func TestDefaultInterval(t *testing.T) {
	fakeClient := fake.NewClientset()
	monitor := NewResourceMonitor(fakeClient, "conduix", 0) // 0 => 기본 30초

	if monitor.interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", monitor.interval)
	}
}

func TestCollectNodeMetrics(t *testing.T) {
	fakeClient := fake.NewClientset()

	// 노드 추가
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3500m"),
				corev1.ResourceMemory: resource.MustParse("14Gi"),
			},
		},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	monitor := NewResourceMonitor(fakeClient, "conduix", 30*time.Second)
	metrics := &ClusterMetrics{}
	monitor.collectNodeMetrics(context.Background(), metrics)

	if metrics.NodeCount != 1 {
		t.Errorf("expected 1 node, got %d", metrics.NodeCount)
	}
	if metrics.CPUCapacity != 4000 { // 4 cores = 4000 millicores
		t.Errorf("expected CPU capacity 4000m, got %d", metrics.CPUCapacity)
	}
	if metrics.CPUAllocatable != 3500 {
		t.Errorf("expected CPU allocatable 3500m, got %d", metrics.CPUAllocatable)
	}
}

func TestCollectPodMetrics(t *testing.T) {
	fakeClient := fake.NewClientset()
	ctx := context.Background()

	// 다양한 상태의 Pod 추가
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "running-pod", Namespace: "conduix"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pending-pod", Namespace: "conduix"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Namespace: "conduix"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
	}

	for _, pod := range pods {
		_, err := fakeClient.CoreV1().Pods("conduix").Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create pod: %v", err)
		}
	}

	monitor := NewResourceMonitor(fakeClient, "conduix", 30*time.Second)
	metrics := &ClusterMetrics{}
	monitor.collectPodMetrics(ctx, metrics)

	if metrics.TotalPods != 3 {
		t.Errorf("expected 3 total pods, got %d", metrics.TotalPods)
	}
	if metrics.RunningPods != 1 {
		t.Errorf("expected 1 running pod, got %d", metrics.RunningPods)
	}
	if metrics.PendingPods != 1 {
		t.Errorf("expected 1 pending pod, got %d", metrics.PendingPods)
	}
	if metrics.FailedPods != 1 {
		t.Errorf("expected 1 failed pod, got %d", metrics.FailedPods)
	}
}

func TestGetMetrics(t *testing.T) {
	fakeClient := fake.NewClientset()
	monitor := NewResourceMonitor(fakeClient, "conduix", 30*time.Second)

	// 초기 메트릭은 비어있어야 함
	metrics := monitor.GetMetrics()
	if metrics == nil {
		t.Fatal("metrics should not be nil")
	}
	if metrics.NodeCount != 0 {
		t.Errorf("expected 0 nodes initially, got %d", metrics.NodeCount)
	}
}

func TestStartAndStop(t *testing.T) {
	fakeClient := fake.NewClientset()
	monitor := NewResourceMonitor(fakeClient, "conduix", 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	monitor.Start(ctx)

	// 잠시 대기하여 수집 확인
	time.Sleep(200 * time.Millisecond)

	metrics := monitor.GetMetrics()
	if metrics.CollectedAt.IsZero() {
		t.Error("expected CollectedAt to be set after start")
	}

	monitor.Stop()
}
