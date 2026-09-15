package runner

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 상주 파드는 여러 realtime 실행을 동시에 담아야 한다.
// 예전에는 실행마다 파드가 떠 realtime 10개면 파드 10개(5 CPU/5Gi)를 점유했다.
func TestRegistry_HoldsMultipleExecutions(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	for _, id := range []string{"ex-1", "ex-2", "ex-3"} {
		if err := reg.Start(ctx, newTestCommand(id, "wf-"+id)); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	if got := reg.Count(); got != 3 {
		t.Fatalf("count = %d, want 3 — 파드 하나가 여러 실행을 담아야 한다", got)
	}
}

// 실행 하나를 멈춰도 나머지는 살아 있어야 한다.
// 예전 구조에서는 stop 이 프로세스 전체를 죽여 무관한 실행까지 끊겼다.
func TestRegistry_StopIsolatesOneExecution(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	for _, id := range []string{"keep-1", "drop", "keep-2"} {
		if err := reg.Start(ctx, newTestCommand(id, "wf")); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	if err := reg.Stop("drop"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if got := reg.Count(); got != 2 {
		t.Fatalf("count = %d, want 2 — 다른 실행이 함께 죽었다", got)
	}
	for _, id := range []string{"keep-1", "keep-2"} {
		if reg.get(id) == nil {
			t.Errorf("%s 가 사라졌다 — stop 은 지정한 실행만 멈춰야 한다", id)
		}
	}
}

// 중복 배정은 오류로 구분되어야 한다. 명령이 at-least-once 로 재전송되면
// 같은 소스를 두 번 읽어 데이터가 중복 적재된다.
func TestRegistry_RejectsDuplicate(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	if err := reg.Start(ctx, newTestCommand("ex-1", "wf-1")); err != nil {
		t.Fatalf("first start: %v", err)
	}
	err := reg.Start(ctx, newTestCommand("ex-1", "wf-1"))
	if err == nil {
		t.Fatal("중복 배정이 통과했다 — 같은 소스를 이중 소비한다")
	}
	if err != ErrExecutionExists {
		t.Errorf("err = %v, want ErrExecutionExists (핸들러가 이 값으로 중복을 구분한다)", err)
	}
	if got := reg.Count(); got != 1 {
		t.Errorf("count = %d, want 1", got)
	}
}

// 실행을 멈추면 보조 고루틴(생존 루프)도 함께 끝나야 한다.
// 파드 ctx 를 쓰면 실행이 멈춰도 고루틴이 남아 누수가 된다.
func TestRegistry_StopEndsExecutionGoroutines(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	if err := reg.Start(ctx, newTestCommand("ex-1", "wf-1")); err != nil {
		t.Fatalf("start: %v", err)
	}
	e := reg.get("ex-1")
	if e == nil || e.done == nil {
		t.Fatal("done 채널이 없다 — 고루틴 정리를 확인할 수 없다")
	}
	done := e.done

	if err := reg.Stop("ex-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop 후에도 생존 루프가 살아 있다 — 고루틴 누수")
	}
}

// 파드 종료 시 모든 실행이 정리되어야 한다(체크포인트 flush 포함).
func TestRegistry_StopAllClearsEverything(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := reg.Start(ctx, newTestCommand(id, "wf")); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	reg.StopAll()

	if got := reg.Count(); got != 0 {
		t.Errorf("count = %d, want 0 — 파드 종료 시 전부 정리돼야 한다", got)
	}
}

// 파드 컨텍스트가 취소되면 실행들도 함께 끝난다.
// 바이너리 갱신으로 파드를 교체할 때 이 경로로 모든 실행이 체크포인트를 남기고 종료된다.
func TestRegistry_PodContextCancelStopsExecutions(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx, cancel := context.WithCancel(context.Background())

	if err := reg.Start(ctx, newTestCommand("ex-1", "wf-1")); err != nil {
		t.Fatalf("start: %v", err)
	}
	e := reg.get("ex-1")
	done := e.done

	cancel() // 파드 종료(SIGTERM 상당)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("파드 취소 후에도 실행 고루틴이 남았다")
	}
}

// 시작 직후에는 생존 신호가 찍혀야 한다. 없으면 감시 루프가 곧바로 죽은 것으로 본다.
func TestRegistry_TouchesLivenessOnStart(t *testing.T) {
	reg := newStreamingRegistry("", "")
	if err := reg.Start(context.Background(), newTestCommand("ex-1", "wf-1")); err != nil {
		t.Fatalf("start: %v", err)
	}
	e := reg.get("ex-1")
	if e.lastBeat().IsZero() {
		t.Fatal("시작 시 생존 신호가 없다 — 감시 루프가 즉시 오탐한다")
	}
}

// 없는 실행 제어는 오류여야 한다. 조용히 성공하면 사라진 실행을 계속 추적한다.
func TestRegistry_ControlsUnknownExecution(t *testing.T) {
	reg := newStreamingRegistry("", "")
	if err := reg.Stop("missing"); err == nil {
		t.Error("없는 실행 stop 이 성공했다")
	}
	if err := reg.Pause("missing"); err == nil {
		t.Error("없는 실행 pause 가 성공했다")
	}
	if err := reg.Resume("missing"); err == nil {
		t.Error("없는 실행 resume 이 성공했다")
	}
}

// 배정 요청 검증: 실행 id·워크플로우 설정이 없으면 받지 않는다.
func TestRegistry_ValidatesInput(t *testing.T) {
	reg := newStreamingRegistry("", "")
	ctx := context.Background()

	if err := reg.Start(ctx, nil); err == nil {
		t.Error("nil 명령이 통과했다")
	}
	if err := reg.Start(ctx, &types.WorkflowExecutionCommand{WorkflowID: "wf"}); err == nil {
		t.Error("execution_id 없는 명령이 통과했다")
	}
	if err := reg.Start(ctx, &types.WorkflowExecutionCommand{ExecutionID: "ex"}); err == nil {
		t.Error("workflow 설정 없는 명령이 통과했다")
	}
}

func newTestCommand(executionID, workflowID string) *types.WorkflowExecutionCommand {
	return &types.WorkflowExecutionCommand{
		ExecutionID: executionID,
		WorkflowID:  workflowID,
		WorkflowConfig: &types.Workflow{
			ID:   workflowID,
			Name: "test-" + executionID,
			Type: types.WorkflowTypeRealtime,
		},
	}
}
