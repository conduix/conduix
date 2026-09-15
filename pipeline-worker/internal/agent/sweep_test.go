package agent

import (
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-worker/internal/k8s"
)

// 상주 파드 모델에서의 건강 판정.
//
// 예전에는 "Deployment 1개 = 실행 1개" 라 Deployment 하나의 readiness 가 곧 그 실행의
// 성패였다. 파드가 하나로 합쳐지면서 판정 단위가 바뀌었다 — 파드가 못 뜨면 그 안의
// 실행이 전부 못 도는 것이므로, CP 가 running 으로 보는 실행 전체를 함께 확정한다.

// 유예 안에서는 아무것도 하지 않는다.
// 첫 기동은 이미지 pull + initContainer 바이너리 다운로드(32MB)로 느릴 수 있다.
func TestFailIfPodStuckUnhealthy_WithinGraceDoesNothing(t *testing.T) {
	a, _ := newTestAgent(t)
	a.runningExecs = map[string]*RunningExecution{
		"exec-young": {ExecutionID: "exec-young", StreamingDeployment: k8s.StreamingPodName},
	}
	live := map[string]struct{}{"exec-young": {}}

	// jobManager 가 없으면 조기 반환한다 — K8s 없이 로컬 정리가 일어나지 않아야 한다.
	a.failIfPodStuckUnhealthy(live)

	if _, ok := a.runningExecs["exec-young"]; !ok {
		t.Fatal("K8s 클라이언트가 없으면 실행을 정리하지 않아야 한다")
	}
}

// in-process 실행은 파드와 무관하므로 파드 상태로 판정하면 안 된다.
func TestFailIfPodStuckUnhealthy_IgnoresInProcessExecutions(t *testing.T) {
	a, _ := newTestAgent(t)
	a.runningExecs = map[string]*RunningExecution{
		// StreamingDeployment 가 비면 agent 프로세스 안에서 도는 실행이다.
		"in-process": {ExecutionID: "in-process"},
	}
	live := map[string]struct{}{"in-process": {}}

	a.failIfPodStuckUnhealthy(live)

	if _, ok := a.runningExecs["in-process"]; !ok {
		t.Fatal("in-process 실행은 파드 건강 판정의 대상이 아니다")
	}
}

// Healthy() 판정: ready 가 desired 이상이어야 건강하다.
// 이 판정이 틀리면 정상 파드의 실행을 죽이거나, 죽은 파드를 방치한다.
func TestStreamingExecutionHealthy(t *testing.T) {
	cases := []struct {
		name  string
		ready int32
		want  bool
	}{
		{"ready 가 desired 와 같으면 건강", 1, true},
		{"ready 가 0 이면 불건강", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := k8s.StreamingExecution{ReadyReplicas: tc.ready, DesiredReplicas: 1}
			if got := d.Healthy(); got != tc.want {
				t.Errorf("Healthy() = %v, want %v", got, tc.want)
			}
		})
	}
}

// desired 가 0 이면 건강으로 보지 않는다 — 스케일 다운된 파드를 정상으로 오판하면
// 실행이 안 도는데도 아무도 알아채지 못한다.
func TestStreamingExecutionHealthy_ZeroDesiredIsNotHealthy(t *testing.T) {
	d := k8s.StreamingExecution{ReadyReplicas: 0, DesiredReplicas: 0}
	if d.Healthy() {
		t.Error("desired=0 을 건강으로 판정했다")
	}
}

// 유예 상수가 기동 시간보다 충분히 길어야 한다.
// 짧으면 이미지 pull 중인 정상 파드를 죽인다.
func TestUnhealthyGraceIsLongEnough(t *testing.T) {
	if unhealthyGrace < 3*time.Minute {
		t.Errorf("unhealthyGrace = %s — 이미지 pull + 32MB 바이너리 다운로드에 부족하다", unhealthyGrace)
	}
}
