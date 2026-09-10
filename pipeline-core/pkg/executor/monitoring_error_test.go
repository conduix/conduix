package executor

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// 실측: realtime 파이프라인이 DDL 방어로 정지했을 때 화면에 "모니터링 데이터 없음" 만
// 떴다. 정지한 파이프라인은 statsCollectors 에서 제거되어(runPipeline 의 defer)
// Pipelines 배열에 나타나지 않으므로, 사유는 그룹 레벨로만 전달할 수 있다.
func TestExecutionErrorMessage(t *testing.T) {
	t.Run("그룹 메시지를 우선한다", func(t *testing.T) {
		got := ExecutionErrorMessage(&types.PipelineGroupExecution{
			ErrorMessage: "group failure",
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "p1", ErrorMessage: "pipeline failure"},
			},
		})
		if got != "group failure" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("그룹 메시지가 없으면 파이프라인 사유를 쓴다 — DDL 정지 실측 경로", func(t *testing.T) {
		// DDL 정지는 사유를 파이프라인 result 에만 남긴다. 여기서 빈 문자열을 내면
		// 화면에 실패 사실도 이유도 안 보인다.
		got := ExecutionErrorMessage(&types.PipelineGroupExecution{
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "restrooms-cdc", ErrorMessage: "schema change (DDL) detected"},
			},
		})
		want := "pipeline restrooms-cdc: schema change (DDL) detected"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("사유가 없으면 빈 문자열 — 정상 실행에 에러를 붙이지 않는다", func(t *testing.T) {
		if got := ExecutionErrorMessage(&types.PipelineGroupExecution{}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("사유 있는 첫 파이프라인을 쓴다", func(t *testing.T) {
		got := ExecutionErrorMessage(&types.PipelineGroupExecution{
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "ok", ErrorMessage: ""},
				{PipelineName: "bad", ErrorMessage: "boom"},
			},
		})
		if got != "pipeline bad: boom" {
			t.Errorf("got %q", got)
		}
	})
}

// Stop() 은 status 를 무조건 stopped 로 덮어쓴다(group_executor.go 의 Stop).
// 종료 감시가 error 를 보고한 뒤 종료 경로가 Stop 을 호출하므로, 그 뒤에도 사유가
// 남아 있어야 파드가 살아있는 동안 화면이 이유를 보여줄 수 있다.
func TestGetMonitoringInfo_KeepsErrorMessageAfterStop(t *testing.T) {
	e := &GroupExecutor{
		execution: &types.PipelineGroupExecution{
			ID:         "ex-1",
			WorkflowID: "wf-1",
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "restrooms-cdc", ErrorMessage: "schema change (DDL) detected"},
			},
		},
		status: types.PipelineGroupStatusStopped,
	}

	info := e.GetMonitoringInfo()
	if info == nil {
		t.Fatal("monitoring info 가 nil")
	}
	if info.ErrorMessage == "" {
		t.Error("Stop 이후 사유가 사라졌다 — 화면에 이유가 안 보인다")
	}
	if len(info.Pipelines) != 0 {
		t.Errorf("정지한 파이프라인은 목록에서 빠진다(전제 확인): %d개", len(info.Pipelines))
	}
}

// 정상 실행에 에러 메시지가 붙으면 오해를 준다.
func TestGetMonitoringInfo_NoErrorMessageWhenHealthy(t *testing.T) {
	e := &GroupExecutor{
		execution: &types.PipelineGroupExecution{ID: "ex-1", WorkflowID: "wf-1"},
		status:    types.PipelineGroupStatusRunning,
	}

	info := e.GetMonitoringInfo()
	if info == nil {
		t.Fatal("monitoring info 가 nil")
	}
	if info.ErrorMessage != "" {
		t.Errorf("정상 실행에 사유가 붙었다: %q", info.ErrorMessage)
	}
}
