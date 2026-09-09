package agent

import (
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-worker/internal/k8s"
)

// failIfStuckUnhealthy 는 유예 안에서는 아무것도 하지 않는다.
// 첫 기동은 이미지 pull + initContainer 바이너리 다운로드로 느릴 수 있다.
func TestFailIfStuckUnhealthy_WithinGraceDoesNothing(t *testing.T) {
	a, _ := newTestAgent(t)
	a.runningExecs = map[string]*RunningExecution{
		"exec-young": {ExecutionID: "exec-young"},
	}

	a.failIfStuckUnhealthy(k8s.StreamingExecution{
		ExecutionID:     "exec-young",
		ReadyReplicas:   0,
		DesiredReplicas: 1,
		CreatedAt:       time.Now().Add(-time.Minute), // 유예(5m) 안
	})

	if _, ok := a.runningExecs["exec-young"]; !ok {
		t.Fatal("유예 안에서는 실행을 정리하지 않아야 한다")
	}
}

// 준비된 실행은 나이와 무관하게 건드리지 않는다.
func TestFailIfStuckUnhealthy_HealthyIsUntouched(t *testing.T) {
	a, _ := newTestAgent(t)
	a.runningExecs = map[string]*RunningExecution{
		"exec-ok": {ExecutionID: "exec-ok"},
	}

	a.failIfStuckUnhealthy(k8s.StreamingExecution{
		ExecutionID:     "exec-ok",
		ReadyReplicas:   1,
		DesiredReplicas: 1,
		CreatedAt:       time.Now().Add(-time.Hour),
	})

	if _, ok := a.runningExecs["exec-ok"]; !ok {
		t.Fatal("정상 실행을 정리해서는 안 된다")
	}
}

// 유예를 넘겨 계속 unhealthy 하면 로컬 추적과 claim 을 정리한다.
// (CP 보고·Deployment 삭제는 K8s/HTTP 의존이라 여기서는 로컬 정리만 검증한다.)
func TestFailIfStuckUnhealthy_StuckClearsLocalState(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-stuck"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}
	a.runningExecs = map[string]*RunningExecution{exec: {ExecutionID: exec}}

	a.failIfStuckUnhealthy(k8s.StreamingExecution{
		ExecutionID:     exec,
		ReadyReplicas:   0,
		DesiredReplicas: 1,
		CreatedAt:       time.Now().Add(-unhealthyGrace - time.Minute),
	})

	if _, ok := a.runningExecs[exec]; ok {
		t.Fatal("좀비 실행은 로컬 추적에서 제거돼야 한다")
	}
	if _, err := mr.Get(executionClaimKey(exec)); err == nil {
		t.Fatal("claim 이 해제돼야 다른 agent 가 재시도할 수 있다")
	}
}

// label 이 없으면 판정 근거가 없으므로 건드리지 않는다.
func TestFailIfStuckUnhealthy_NoExecutionIDIsIgnored(t *testing.T) {
	a, _ := newTestAgent(t)
	a.runningExecs = map[string]*RunningExecution{"other": {ExecutionID: "other"}}

	a.failIfStuckUnhealthy(k8s.StreamingExecution{
		ExecutionID:     "",
		ReadyReplicas:   0,
		DesiredReplicas: 1,
		CreatedAt:       time.Now().Add(-time.Hour),
	})

	if len(a.runningExecs) != 1 {
		t.Fatal("execution-id 없는 Deployment 때문에 다른 실행이 영향받아서는 안 된다")
	}
}
