package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// completed 는 최종 상태다. batch fire-and-forget 위임은 (a)위임 실패 error 보고와
// (b)Job 완주 completed 콜백이 서로 다른 경로로 같은 execution row 를 쓴다. 도착 순서와
// 무관하게 완주 결과가 error 로 덮이지 않아야 한다 — 이 가드가 그걸 보장한다.
func TestUpdateExecutionStatus_TerminalGuard(t *testing.T) {
	h, db := reconcileTestHandler(t)

	seed := func(id, status string) {
		require.NoError(t, db.Create(&models.WorkflowExecution{
			ID: id, WorkflowID: "wf", Status: status,
		}).Error)
	}
	statusOf := func(id string) string {
		var e models.WorkflowExecution
		require.NoError(t, db.First(&e, "id = ?", id).Error)
		return e.Status
	}

	completed := string(types.WorkflowStatusCompleted)

	t.Run("completed 는 이후 error 로 덮이지 않는다", func(t *testing.T) {
		seed("e1", completed)
		h.updateExecutionStatus("e1", "error", map[string]any{"status": "error"})
		require.Equal(t, completed, statusOf("e1"), "완주한 실행이 error 로 뒤집혔다")
	})

	t.Run("completed 는 이후 timeout 으로도 덮이지 않는다", func(t *testing.T) {
		seed("e2", completed)
		h.updateExecutionStatus("e2", "failed", map[string]any{"status": "failed"})
		require.Equal(t, completed, statusOf("e2"))
	})

	t.Run("running → completed 전이는 허용된다", func(t *testing.T) {
		seed("e3", string(types.WorkflowStatusRunning))
		h.updateExecutionStatus("e3", completed, map[string]any{"status": completed})
		require.Equal(t, completed, statusOf("e3"))
	})

	t.Run("running → error 전이는 허용된다(아직 완주 아님)", func(t *testing.T) {
		seed("e4", string(types.WorkflowStatusRunning))
		h.updateExecutionStatus("e4", "error", map[string]any{"status": "error"})
		require.Equal(t, "error", statusOf("e4"))
	})

	t.Run("completed → completed(재도착)는 무해하게 유지", func(t *testing.T) {
		seed("e5", completed)
		h.updateExecutionStatus("e5", completed, map[string]any{"status": completed, "total_records": 42})
		require.Equal(t, completed, statusOf("e5"))
		var e models.WorkflowExecution
		require.NoError(t, db.First(&e, "id = ?", "e5").Error)
		require.EqualValues(t, 42, e.TotalRecords, "completed 재도착 시 레코드 수 갱신은 허용")
	})
}
