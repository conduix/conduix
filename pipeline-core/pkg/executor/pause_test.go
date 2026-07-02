package executor

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// waitIfPaused는 일시정지 중 블록하고 Resume 시 해제되어야 한다.
func TestPauseResume_UnblocksWaiter(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})

	if err := e.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		e.waitIfPaused(context.Background())
		close(done)
	}()

	// paused 상태이므로 waiter는 블록되어야 한다.
	select {
	case <-done:
		t.Fatal("waitIfPaused returned while paused; should have blocked")
	case <-time.After(50 * time.Millisecond):
	}

	if err := e.Resume(); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitIfPaused did not return after Resume")
	}
}

// Stop은 일시정지 대기 중인 waiter도 깨워야 한다(paused 상태로 멈춘 실행 중지 보장).
func TestStop_UnblocksPausedWaiter(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})

	if err := e.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		e.waitIfPaused(context.Background())
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("waitIfPaused returned while paused")
	case <-time.After(50 * time.Millisecond):
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitIfPaused did not return after Stop")
	}
}

// Resume은 일시정지 상태가 아니면 에러를 반환해야 한다.
func TestResume_NotPausedReturnsError(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	if err := e.Resume(); err == nil {
		t.Fatal("expected error resuming a non-paused executor")
	}
}

// ctx가 취소되면 일시정지 대기 중이어도 waitIfPaused는 반환해야 한다
// (paused 상태에서 실행 타임아웃/취소가 무시되지 않도록).
func TestWaitIfPaused_ContextCancelUnblocks(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	if err := e.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.waitIfPaused(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("waitIfPaused returned before cancel while paused")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitIfPaused did not return after ctx cancel")
	}
}

// paused가 아닐 때 waitIfPaused는 즉시 반환해야 한다.
func TestWaitIfPaused_NotPausedReturnsImmediately(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	done := make(chan struct{})
	go func() {
		e.waitIfPaused(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitIfPaused blocked even though not paused")
	}
}
