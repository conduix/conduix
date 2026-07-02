package executor

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 실패하는 파이프라인(잘못된 소스 타입)은 FailureActionRetry + MaxRetries만큼 재시도해야 한다.
// RetryDelay를 측정해 재시도 횟수를 간접 검증한다.
func TestRunPipelineWithRetry_RetriesOnFailure(t *testing.T) {
	group := &types.PipelineGroup{
		FailurePolicy: &types.FailurePolicy{
			Action:     types.FailureActionRetry,
			MaxRetries: 2,      // 최초 1 + 재시도 2 = 총 3회 시도
			RetryDelay: "20ms", // 재시도 간 대기
		},
	}
	e := NewGroupExecutor(group)

	badPipeline := types.GroupedPipeline{
		ID:    "p1",
		Name:  "always-fails",
		Input: types.WorkflowInput{Type: "nonexistent_source_type", Config: map[string]any{}},
	}

	start := time.Now()
	_, err := e.runPipelineWithRetry(context.Background(), badPipeline)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from always-failing pipeline")
	}
	// 재시도 2회 → 최소 2 * 20ms = 40ms 대기가 있어야 한다.
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected retries to add delay (>=40ms), elapsed=%v — retries likely not happening", elapsed)
	}
}

// retry 정책이 아니면 1회만 시도(딜레이 없음).
func TestRunPipelineWithRetry_NoRetryWhenPolicyAbsent(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{}) // FailurePolicy 없음

	badPipeline := types.GroupedPipeline{
		ID:    "p1",
		Name:  "fails-once",
		Input: types.WorkflowInput{Type: "nonexistent_source_type", Config: map[string]any{}},
	}

	start := time.Now()
	_, err := e.runPipelineWithRetry(context.Background(), badPipeline)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	// 재시도 없음 → 거의 즉시(딜레이 없어야 함)
	if elapsed > 30*time.Millisecond {
		t.Errorf("expected single attempt (no retry delay), elapsed=%v", elapsed)
	}
}

// ctx 취소 시 재시도 대기 중 즉시 중단해야 한다.
func TestRunPipelineWithRetry_ContextCancelStopsRetry(t *testing.T) {
	group := &types.PipelineGroup{
		FailurePolicy: &types.FailurePolicy{
			Action:     types.FailureActionRetry,
			MaxRetries: 10,
			RetryDelay: "500ms",
		},
	}
	e := NewGroupExecutor(group)

	badPipeline := types.GroupedPipeline{
		ID:    "p1",
		Name:  "fails",
		Input: types.WorkflowInput{Type: "nonexistent_source_type", Config: map[string]any{}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := e.runPipelineWithRetry(ctx, badPipeline)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	// 10회 * 500ms = 5s가 아니라, 취소로 훨씬 빨리 끝나야 한다.
	if elapsed > 1*time.Second {
		t.Errorf("context cancel should stop retries quickly, elapsed=%v", elapsed)
	}
}
