package agent

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// 종료 시 자기 claim 을 해제해야 다음 agent 가 즉시 인계받는다.
// 안 하면 TTL(30s) 동안 죽은 agent 의 claim 이 남아 reconcile 을 돈 새 agent 들이
// 전부 skip 하고, heartbeat 에서 실행이 빠져 stale 감지기가 orphan 으로 확정한다.
func TestReleaseClaimsOnShutdown_ReleasesOwnClaim(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-own"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}

	a.releaseClaimsOnShutdown([]*RunningExecution{{ExecutionID: exec}})

	if _, err := mr.Get(executionClaimKey(exec)); err == nil {
		t.Fatal("claim should be released so the next agent can take over immediately")
	}
}

// 다른 agent 의 claim 은 절대 지우지 않는다 — 지우면 같은 소스를 두 agent 가 이중 소비한다.
func TestReleaseClaimsOnShutdown_KeepsOtherAgentClaim(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-other"

	key := executionClaimKey(exec)
	if err := mr.Set(key, "agent-B"); err != nil {
		t.Fatalf("seed other agent claim: %v", err)
	}

	a.releaseClaimsOnShutdown([]*RunningExecution{{ExecutionID: exec}})

	owner, err := mr.Get(key)
	if err != nil {
		t.Fatalf("other agent's claim was deleted: %v", err)
	}
	if owner != "agent-B" {
		t.Fatalf("owner changed: got %q, want agent-B", owner)
	}
}

// 위임 실행(streaming pod)은 GroupExecutor 가 nil 이지만 claim 은 이 agent 가 쥔다.
// pod 은 agent 와 독립적으로 계속 도므로, 해제하지 않으면 실물은 살아있는데 DB 만
// error 가 되는 불일치가 생긴다.
func TestReleaseClaimsOnShutdown_CoversDelegatedExecution(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-delegated"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}

	delegated := &RunningExecution{
		ExecutionID:         exec,
		WorkflowID:          "wf-1",
		StreamingDeployment: "conduix-rt-exec-delegated",
		StreamingNamespace:  "conduix",
		// GroupExecutor 는 nil — pod 안에서 돌기 때문.
	}
	a.releaseClaimsOnShutdown([]*RunningExecution{delegated})

	if _, err := mr.Get(executionClaimKey(exec)); err == nil {
		t.Fatal("delegated execution's claim should also be released")
	}
}

// Stop() 전체 경로에서 claim 이 해제되는지 — a.cancel() 로 a.ctx 가 끝난 뒤에도
// 별도 ctx 로 해제가 동작해야 한다.
func TestStop_ReleasesClaims(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-stop"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}
	a.cancel = func() {} // Stop() 이 호출하는 cancel — 테스트 ctx 를 죽이지 않는다.
	a.runningExecs = map[string]*RunningExecution{
		exec: {ExecutionID: exec, WorkflowID: "wf-1"},
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := mr.Get(executionClaimKey(exec)); err == nil {
		t.Fatal("Stop() should release claims for handover")
	}
	if a.Status != types.AgentStatusOffline {
		t.Fatalf("status: got %q, want offline", a.Status)
	}
}
