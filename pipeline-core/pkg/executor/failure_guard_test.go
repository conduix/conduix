package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/shared/types"
)

func rec() source.Record { return source.Record{Data: map[string]any{"x": 1}} }

// FailurePolicy가 없으면 서킷은 절대 열리지 않는다(가드는 no-op에 가깝게 동작).
func TestFailureGuard_NoPolicy_NeverTrips(t *testing.T) {
	g, err := newFailureGuard(nil, "wf", "p", "pipe")
	if err != nil {
		t.Fatalf("newFailureGuard: %v", err)
	}
	for range 100 {
		if g.recordFailure(context.Background(), rec(), errors.New("boom"), "es") {
			t.Fatal("circuit should never trip without a policy")
		}
	}
	if g.isTripped() {
		t.Fatal("should not be tripped")
	}
}

// 연속 실패 임계 초과 시 서킷이 열린다.
func TestFailureGuard_ConsecutiveThreshold(t *testing.T) {
	fp := &types.FailurePolicy{
		CircuitBreaker: &types.CircuitBreakerPolicy{Enabled: true, MaxConsecutiveFailures: 3},
	}
	g, _ := newFailureGuard(fp, "wf", "p", "pipe")

	if g.recordFailure(context.Background(), rec(), errors.New("e"), "es") {
		t.Fatal("should not trip on 1st failure")
	}
	if g.recordFailure(context.Background(), rec(), errors.New("e"), "es") {
		t.Fatal("should not trip on 2nd failure")
	}
	if !g.recordFailure(context.Background(), rec(), errors.New("e"), "es") {
		t.Fatal("should trip on 3rd consecutive failure")
	}
	if !g.isTripped() {
		t.Fatal("guard should report tripped")
	}
	if g.trippedErr() == nil {
		t.Fatal("trippedErr should be non-nil")
	}
}

// 성공이 연속 카운트를 리셋한다.
func TestFailureGuard_SuccessResetsConsecutive(t *testing.T) {
	fp := &types.FailurePolicy{
		CircuitBreaker: &types.CircuitBreakerPolicy{Enabled: true, MaxConsecutiveFailures: 3},
	}
	g, _ := newFailureGuard(fp, "wf", "p", "pipe")

	g.recordFailure(context.Background(), rec(), errors.New("e"), "es")
	g.recordFailure(context.Background(), rec(), errors.New("e"), "es")
	g.recordSuccess() // 리셋
	// 다시 2번 실패 — 리셋됐으므로 아직 임계(3) 미달
	g.recordFailure(context.Background(), rec(), errors.New("e"), "es")
	if g.recordFailure(context.Background(), rec(), errors.New("e"), "es") {
		t.Fatal("consecutive should have been reset by success; 2 < 3 must not trip")
	}
}

// 누적 실패 임계 초과 시 서킷이 열린다(성공이 사이에 껴도).
func TestFailureGuard_TotalThreshold(t *testing.T) {
	fp := &types.FailurePolicy{
		CircuitBreaker: &types.CircuitBreakerPolicy{Enabled: true, MaxTotalFailures: 3},
	}
	g, _ := newFailureGuard(fp, "wf", "p", "pipe")

	g.recordFailure(context.Background(), rec(), errors.New("e"), "es")
	g.recordSuccess()
	g.recordFailure(context.Background(), rec(), errors.New("e"), "es")
	g.recordSuccess()
	if !g.recordFailure(context.Background(), rec(), errors.New("e"), "es") {
		t.Fatal("should trip on 3rd total failure regardless of successes")
	}
}
