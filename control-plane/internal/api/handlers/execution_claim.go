package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// 왜 이 엔드포인트가 필요한가:
//
// batch 실행은 agent 가 K8s Job 을 위임 생성하고 끝낸다(fire-and-forget). Job 은 agent
// heartbeat 의 running_execs 에 등록되지 않으므로, heartbeat 기반 stale 감지는 batch 에
// 카테고리 오류다 — 정상 실행 중인 Job 을 2분 뒤 죽여버린다. 그래서 스케줄러는 batch 를
// stale 감지에서 아예 제외했다.
//
// 그 결과 구멍이 생겼다: 실행 명령이 Redis pub/sub 로 발행됐는데 받을 agent 가 없으면
// (at-most-once) 명령이 조용히 사라지는데, batch 는 자동 복구가 없어 워크플로우가 영구히
// running 으로 남는다. 사용자는 "실행 시작됨" 을 보고도 아무 진행이 없고 실패조차 표시되지
// 않는다(실측 확인된 최악의 형태 — 에러도 안 보임).
//
// 위임을 접수한 agent 가 그 사실을 즉시 알리면, "유예 시간이 지났는데 접수 흔적이 없는
// batch" 만 골라 안전하게 stale 판정할 수 있다. 정상 실행 중인 Job 은 접수 기록이 있어
// 오판되지 않는다.

// ClaimExecutionRequest 는 agent 가 실행 위임을 접수했음을 알리는 요청이다.
type ClaimExecutionRequest struct {
	AgentID string `json:"agent_id" binding:"required"`
	// DelegatedTo 는 위임 대상 리소스명(K8s Job 또는 Deployment)이다.
	// 실행이 어디로 갔는지 추적할 수 있어야 문제를 K8s 쪽에서 이어서 볼 수 있다.
	DelegatedTo string `json:"delegated_to,omitempty"`
}

// ClaimExecution POST /api/v1/internal/workflows/:id/executions/:executionId/claim
//
// agent 가 실행을 접수(위임 생성 성공)한 직후 호출한다. agent_id 를 채워
// "아무도 받지 않은 실행" 과 구분되게 한다.
func (h *WorkflowHandler) ClaimExecution(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	executionID := c.Param("executionId")

	var req ClaimExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":    false,
			"error":      gin.H{"code": "VALIDATION_ERROR", "message": err.Error()},
			"request_id": requestID,
		})
		return
	}

	updates := map[string]any{"agent_id": req.AgentID}
	if req.DelegatedTo != "" {
		updates["delegated_to"] = req.DelegatedTo
	}

	// 이미 결과가 보고돼 종료된 실행은 건드리지 않는다 — 늦게 도착한 접수 알림이
	// 완료된 실행의 상태를 되돌리면 안 된다.
	res := h.db.Model(&models.WorkflowExecution{}).
		Where("id = ? AND status = ?", executionID, string(types.PipelineGroupStatusRunning)).
		Updates(updates)
	if res.Error != nil {
		h.logger.Error("failed to record execution claim",
			"execution_id", executionID, "agent_id", req.AgentID, "error", res.Error)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":    false,
			"error":      gin.H{"code": "DB_ERROR", "message": "failed to record claim"},
			"request_id": requestID,
		})
		return
	}

	h.logger.Info("execution claim recorded",
		"execution_id", executionID, "agent_id", req.AgentID,
		"delegated_to", req.DelegatedTo, "rows", res.RowsAffected)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"request_id": requestID,
	})
}
