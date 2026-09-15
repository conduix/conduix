package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// 상주 realtime 파드가 주기적으로 올리는 상태를 받는다.
//
// 왜 push 인가: 중앙이 파드에 일일이 물어보는 pull 방식은 파드가 느리거나 응답이 늦을 때
// "죽었다" 로 오판하기 쉽다. 파드가 스스로 올리면 중앙은 받은 것만 보면 되고,
// 모니터링·체크포인트 관리가 한 곳으로 모인다.
//
// realtime 은 종료 콜백이 영구히 발생하지 않으므로, 이 경로가 실행 중 상태를 DB 에
// 반영하는 유일한 수단이다.

// PodExecutionStatus 는 실행 하나의 상태다(pipeline-runner 와 같은 계약).
type PodExecutionStatus struct {
	ExecutionID string    `json:"execution_id"`
	WorkflowID  string    `json:"workflow_id"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	// LastBeatAt 은 실행 고루틴의 마지막 생존 신호다. 처리량과 별개다 —
	// 느린 소스로 레코드가 0건이어도 살아 있으면 갱신된다.
	LastBeatAt time.Time `json:"last_beat_at"`
	// Alive 가 false 면 파드가 그 실행을 정리했다(재개는 reconcile 이 담당).
	Alive bool `json:"alive"`

	TotalRecords  int64  `json:"total_records"`
	FailedRecords int64  `json:"failed_records"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

// PodStatusRequest 는 파드가 올리는 보고 본문이다.
type PodStatusRequest struct {
	AgentID    string               `json:"agent_id,omitempty"`
	PodName    string               `json:"pod_name,omitempty"`
	ReportedAt time.Time            `json:"reported_at"`
	Executions []PodExecutionStatus `json:"executions"`
}

// ReceiveStreamingPodStatus POST /api/v1/internal/streaming/pod-status
//
// 인증 불필요(클러스터 내부 통신) — job-result·claim 과 같은 신뢰 모델이다.
func (h *WorkflowHandler) ReceiveStreamingPodStatus(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req PodStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":    false,
			"error":      gin.H{"code": "VALIDATION_ERROR", "message": err.Error()},
			"request_id": requestID,
		})
		return
	}

	now := time.Now()
	for _, e := range req.Executions {
		if e.ExecutionID == "" {
			continue
		}

		// 살아 있는 실행: 진행 상황만 갱신한다. status 는 건드리지 않는다 —
		// 종료 판정은 결과 보고(ReceiveExecutionResult)가 한다.
		updates := map[string]any{
			"total_records":  e.TotalRecords,
			"failed_records": e.FailedRecords,
		}
		if req.AgentID != "" {
			updates["agent_id"] = req.AgentID
		}

		if !e.Alive {
			// 파드가 생존 신호 끊김으로 정리한 실행이다. 실패로 확정해야 reconcile 이
			// 체크포인트에서 재개할 수 있다 — running 인 채 두면 아무도 손대지 않는다.
			updates["status"] = string(types.PipelineGroupStatusError)
			updates["completed_at"] = now
			if e.ErrorMessage != "" {
				updates["error_message"] = e.ErrorMessage
			}
			h.logger.Warn("streaming execution reported as not alive by pod",
				"execution_id", e.ExecutionID, "workflow_id", e.WorkflowID,
				"last_beat", e.LastBeatAt, "error", e.ErrorMessage)
		}

		// 이미 종료된 실행은 건드리지 않는다 — 늦게 도착한 보고가 완료 상태를 되돌리면 안 된다.
		if err := h.db.Model(&models.WorkflowExecution{}).
			Where("id = ? AND status = ?", e.ExecutionID, string(types.PipelineGroupStatusRunning)).
			Updates(updates).Error; err != nil {
			h.logger.Error("failed to apply pod status",
				"execution_id", e.ExecutionID, "error", err)
		}

		// 정리된 실행은 워크플로우도 풀어준다. 안 그러면 status=running 에 갇혀
		// 재실행이 409 로 막힌다.
		if !e.Alive && e.WorkflowID != "" {
			h.db.Model(&models.Workflow{}).
				Where("id = ? AND status = ?", e.WorkflowID, string(types.PipelineGroupStatusRunning)).
				Update("status", string(types.PipelineGroupStatusError))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"received":   len(req.Executions),
		"request_id": requestID,
	})
}
