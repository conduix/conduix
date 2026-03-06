package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ResourceMonitor 클러스터 리소스 모니터링
type ResourceMonitor struct {
	clientset kubernetes.Interface
	namespace string
	interval  time.Duration
	metrics   *ClusterMetrics
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
}

// ClusterMetrics 클러스터 리소스 메트릭
type ClusterMetrics struct {
	// 노드 리소스
	NodeCount      int   `json:"node_count"`
	CPUCapacity    int64 `json:"cpu_capacity_millicores"`    // 전체 CPU (millicores)
	MemoryCapacity int64 `json:"memory_capacity_bytes"`      // 전체 메모리 (bytes)
	CPUAllocatable int64 `json:"cpu_allocatable_millicores"` // 할당 가능 CPU
	MemAllocatable int64 `json:"mem_allocatable_bytes"`      // 할당 가능 메모리

	// Pod 현황
	TotalPods   int `json:"total_pods"`
	RunningPods int `json:"running_pods"`
	PendingPods int `json:"pending_pods"`
	FailedPods  int `json:"failed_pods"`

	// Conduix 파이프라인 리소스
	RunnerJobs        int `json:"runner_jobs"`
	RunnerDeployments int `json:"runner_deployments"`
	RunnerPods        int `json:"runner_pods"`

	// 타임스탬프
	CollectedAt time.Time `json:"collected_at"`
}

// NewResourceMonitor ResourceMonitor 생성
func NewResourceMonitor(clientset kubernetes.Interface, namespace string, interval time.Duration) *ResourceMonitor {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ResourceMonitor{
		clientset: clientset,
		namespace: namespace,
		interval:  interval,
		metrics:   &ClusterMetrics{},
	}
}

// Start 모니터링 시작
func (m *ResourceMonitor) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// 즉시 1회 수집
	m.collect()

	go m.loop()
}

// Stop 모니터링 중지
func (m *ResourceMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// GetMetrics 현재 메트릭 반환
func (m *ResourceMonitor) GetMetrics() *ClusterMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 복사본 반환
	copy := *m.metrics
	return &copy
}

// loop 주기적 수집 루프
func (m *ResourceMonitor) loop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.collect()
		}
	}
}

// collect 리소스 정보 수집
func (m *ResourceMonitor) collect() {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	metrics := &ClusterMetrics{
		CollectedAt: time.Now(),
	}

	// 노드 리소스 수집
	m.collectNodeMetrics(ctx, metrics)

	// Pod 현황 수집
	m.collectPodMetrics(ctx, metrics)

	// Conduix Runner 리소스 수집
	m.collectRunnerMetrics(ctx, metrics)

	m.mu.Lock()
	m.metrics = metrics
	m.mu.Unlock()
}

// collectNodeMetrics 노드 리소스 수집
func (m *ResourceMonitor) collectNodeMetrics(ctx context.Context, metrics *ClusterMetrics) {
	nodes, err := m.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[ResourceMonitor] Failed to list nodes: %v\n", err)
		return
	}

	metrics.NodeCount = len(nodes.Items)
	for _, node := range nodes.Items {
		if cpu, ok := node.Status.Capacity[corev1.ResourceCPU]; ok {
			metrics.CPUCapacity += cpu.MilliValue()
		}
		if mem, ok := node.Status.Capacity[corev1.ResourceMemory]; ok {
			metrics.MemoryCapacity += mem.Value()
		}
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			metrics.CPUAllocatable += cpu.MilliValue()
		}
		if mem, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			metrics.MemAllocatable += mem.Value()
		}
	}
}

// collectPodMetrics Pod 현황 수집
func (m *ResourceMonitor) collectPodMetrics(ctx context.Context, metrics *ClusterMetrics) {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[ResourceMonitor] Failed to list pods: %v\n", err)
		return
	}

	metrics.TotalPods = len(pods.Items)
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			metrics.RunningPods++
		case corev1.PodPending:
			metrics.PendingPods++
		case corev1.PodFailed:
			metrics.FailedPods++
		}
	}
}

// collectRunnerMetrics Conduix Runner 관련 리소스 수집
func (m *ResourceMonitor) collectRunnerMetrics(ctx context.Context, metrics *ClusterMetrics) {
	labelSelector := "app.kubernetes.io/managed-by=conduix-agent"

	// Jobs
	jobs, err := m.clientset.BatchV1().Jobs(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		metrics.RunnerJobs = len(jobs.Items)
	}

	// Deployments
	deployments, err := m.clientset.AppsV1().Deployments(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		metrics.RunnerDeployments = len(deployments.Items)
	}

	// Runner Pods
	runnerPods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=conduix-runner",
	})
	if err == nil {
		metrics.RunnerPods = len(runnerPods.Items)
	}
}
