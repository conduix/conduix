package agent

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	redisclient "github.com/conduix/conduix/shared/redis"
)

func newTestAgent(t *testing.T) (*Agent, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rc, err := redisclient.NewResilientClient(redisclient.DefaultConfig(mr.Addr()))
	if err != nil {
		t.Fatalf("resilient client: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &Agent{ID: "agent-A", ctx: ctx, redisClient: rc, claimRenewEvery: 20 * time.Millisecond}, mr
}

// claim 획득 → 같은 에이전트는 갱신 성공, TTL 이 연장된다.
func TestClaim_AcquireAndRenew(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-1"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}
	if _, err := mr.Get(executionClaimKey(exec)); err != nil {
		t.Fatalf("claim key not set: %v", err)
	}

	// TTL 을 거의 만료 직전으로 앞당긴 뒤 renew → 다시 claimTTL 로 연장돼야 한다.
	mr.FastForward(claimTTL - time.Second)
	if !a.renewClaim(exec) {
		t.Fatal("expected renew to succeed (still owner)")
	}
	ttl := mr.TTL(executionClaimKey(exec))
	if ttl <= claimTTL-2*time.Second {
		t.Errorf("TTL not renewed: got %v, want ~%v", ttl, claimTTL)
	}
}

// 다른 에이전트가 claim 을 가져가면 renew 는 false(소유권 상실) 를 반환한다.
func TestClaim_TakenOverByOtherAgent(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-2"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}
	// 다른 에이전트가 강제로 소유권 표시(값 교체).
	if err := mr.Set(executionClaimKey(exec), "agent-B"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if a.renewClaim(exec) {
		t.Error("expected renew to fail (owned by agent-B)")
	}
}

// claim 이 만료(키 삭제)되고 아무도 안 가져갔으면 renew 가 재획득해 true.
func TestClaim_ExpiredThenReacquired(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-3"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}
	mr.Del(executionClaimKey(exec)) // 만료 시뮬레이션

	if !a.renewClaim(exec) {
		t.Error("expected renew to reacquire empty claim")
	}
	if owner, _ := mr.Get(executionClaimKey(exec)); owner != a.ID {
		t.Errorf("owner after reacquire = %q, want %q", owner, a.ID)
	}
}

// releaseClaim 은 우리 소유일 때만 삭제한다(다른 에이전트 claim 은 건드리지 않음).
func TestClaim_ReleaseOnlyOwn(t *testing.T) {
	a, mr := newTestAgent(t)

	// 우리 소유 → 삭제됨.
	_ = a.claimExecution("exec-own")
	a.releaseClaim("exec-own")
	if mr.Exists(executionClaimKey("exec-own")) {
		t.Error("own claim should be released")
	}

	// 남의 소유 → 유지.
	_ = mr.Set(executionClaimKey("exec-other"), "agent-B")
	a.releaseClaim("exec-other")
	if !mr.Exists(executionClaimKey("exec-other")) {
		t.Error("other agent's claim must not be deleted")
	}
}

// 소유권 상실 시 claimRenewalLoop 가 cancel() 을 호출해 실행 ctx 를 취소한다(CDC 중단 경로).
func TestClaimRenewalLoop_CancelsOnLoss(t *testing.T) {
	a, mr := newTestAgent(t)
	const exec = "exec-loop"

	if !a.claimExecution(exec) {
		t.Fatal("expected to acquire claim")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go a.claimRenewalLoop(ctx, cancel, exec)

	// 다른 에이전트가 인수 → 다음 tick 에서 renew 실패 → cancel.
	_ = mr.Set(executionClaimKey(exec), "agent-B")

	select {
	case <-ctx.Done():
		// 정상: 소유권 상실로 취소됨
	case <-time.After(2 * time.Second):
		t.Fatal("claimRenewalLoop did not cancel ctx after claim loss")
	}
}

// standalone(Redis nil) 은 claim 이 항상 true 라 취소되지 않는다.
func TestClaim_StandaloneAlwaysOwns(t *testing.T) {
	a := &Agent{ID: "solo", ctx: context.Background(), redisClient: nil}
	if !a.claimExecution("x") {
		t.Error("standalone claimExecution should be true")
	}
	if !a.renewClaim("x") {
		t.Error("standalone renewClaim should be true")
	}
}
