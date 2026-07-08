package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/services/assignment"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// agentHeartbeatFreshness 는 배정 후보로 인정할 heartbeat 최대 나이다.
// heartbeat 주기 10초·TTL 30초라 최근 신호만 신뢰(죽은 지 얼마 안 된 agent 를 선호 지정해
// claim 지연을 낭비하지 않도록). 이보다 오래된 agent 는 배정 후보에서 제외한다.
const agentHeartbeatFreshness = 20 * time.Second

// applyAgentAssignments 는 배정 전략에 따라 각 cmd 의 PreferredAgentID 를 채운다.
// broadcast(기본) 전략은 heartbeat 조회 없이 즉시 반환 → 현행 동작 불변(상위호환).
func (h *WorkflowHandler) applyAgentAssignments(cmds []*types.WorkflowExecutionCommand, clusterID string) {
	if h.assignStrategy == nil || h.assignStrategy.Name() == "broadcast" {
		return
	}

	live := h.liveAgentsForCluster(clusterID)
	if len(live) == 0 {
		return // 후보 없음 → 선호 지정 없이 broadcast 경쟁(안전 폴백)
	}

	subs := make([]assignment.Sub, len(cmds))
	for i, c := range cmds {
		subs[i] = assignment.Sub{ExecutionID: c.ExecutionID, AssignedPartitions: c.AssignedPartitions}
	}
	byID := make(map[string]string, len(subs))
	for _, a := range h.assignStrategy.Assign(subs, live) {
		byID[a.ExecutionID] = a.PreferredAgentID
	}
	for _, c := range cmds {
		c.PreferredAgentID = byID[c.ExecutionID]
	}
	h.logger.Info("applied agent assignments",
		"strategy", h.assignStrategy.Name(), "cluster_id", clusterID, "live_agents", len(live), "subs", len(subs))
}

// liveAgentsForCluster 는 해당 cluster 의 신선한 agent 를 배정 후보로 반환한다.
func (h *WorkflowHandler) liveAgentsForCluster(clusterID string) []assignment.Agent {
	heartbeats, err := h.redisService.GetAllAgentHeartbeats()
	if err != nil {
		h.logger.Warn("assignment: failed to get heartbeats, falling back to broadcast", "error", err)
		return nil
	}
	now := time.Now()
	var out []assignment.Agent
	for _, hb := range heartbeats {
		if clusterID != "" && hb.ClusterID != clusterID {
			continue
		}
		if now.Sub(hb.Timestamp) > agentHeartbeatFreshness {
			continue // stale heartbeat — 죽었을 수 있으므로 후보 제외
		}
		out = append(out, assignment.Agent{AgentID: hb.AgentID, RunningExecs: len(hb.RunningExecs)})
	}
	return out
}

// planPartitionGroups 는 partitioned source 를 sub-execution 으로 나눌 파티션 그룹을 계산한다.
// 현재는 파티션 1개당 sub-execution 1개(그룹 크기 1) — 가장 단순·정확한 fan-out.
// enabled 파티션이 2개 미만이면 nil(분산 불필요, 단일 실행).
// 여러 파이프라인이 파티션을 가지면 가장 많은 파이프라인 기준(대표 분산 소스).
func planPartitionGroups(pipelines []types.GroupedPipeline) [][]string {
	var ids []string
	for i := range pipelines {
		in := pipelines[i].GetInput()
		var enabled []string
		for _, p := range in.Partitions {
			if p.Enabled {
				enabled = append(enabled, p.ID)
			}
		}
		if len(enabled) > len(ids) {
			ids = enabled
		}
	}
	if len(ids) < 2 {
		return nil // 분산 불필요
	}
	groups := make([][]string, len(ids))
	for i, id := range ids {
		groups[i] = []string{id}
	}
	return groups
}

// publishSubExecutions 는 부모 execution 아래에 파티션 그룹별 sub-execution 을 만들어 발행한다.
// 부모는 이미 생성돼 있고(execution), 이를 "부모"로 재활용해 상태/취합의 앵커로 쓴다.
// 각 sub-execution 은 고유 ExecutionID + AssignedPartitions 로 발행 → 여러 worker 가 나눠 claim/실행.
func (h *WorkflowHandler) publishSubExecutions(
	c *gin.Context,
	workflowID, userID string,
	parent *models.WorkflowExecution,
	jobConfig string,
	workflowConfig *types.Workflow,
	groups [][]string,
) {
	// 부모 execution: 직접 실행하지 않고 sub-execution 결과를 취합하는 앵커.
	h.db.Model(&models.WorkflowExecution{}).Where("id = ?", parent.ID).
		Update("total_sub_executions", len(groups))

	// sub-execution 레코드 먼저 생성(부하 분산 배정은 sub 목록 전체를 보고 계산해야 하므로 발행과 분리).
	cmds := make([]*types.WorkflowExecutionCommand, 0, len(groups))
	subIDs := make([]string, 0, len(groups))
	for _, partitionIDs := range groups {
		sub := &models.WorkflowExecution{
			ID:                uuid.New().String(),
			WorkflowID:        workflowID,
			ClusterID:         parent.ClusterID,
			ParentExecutionID: parent.ID,
			Status:            string(types.PipelineGroupStatusRunning),
			StartedAt:         time.Now(),
			PipelinesSnapshot: parent.PipelinesSnapshot,
			TriggeredBy:       "user",
			TriggeredByID:     userID,
			CreatedAt:         time.Now(),
		}
		if err := h.db.Create(sub).Error; err != nil {
			h.logger.Error("Failed to create sub-execution", "workflow_id", workflowID, "error", err)
			continue
		}
		subIDs = append(subIDs, sub.ID)
		cmds = append(cmds, &types.WorkflowExecutionCommand{
			ID:                 uuid.New().String(),
			WorkflowID:         workflowID,
			ExecutionID:        sub.ID,
			ParentExecutionID:  parent.ID,
			AssignedPartitions: partitionIDs,
			TargetClusterID:    parent.ClusterID,
			TriggeredBy:        "user",
			UserID:             userID,
			JobConfig:          jobConfig,
			WorkflowConfig:     workflowConfig,
			Timestamp:          time.Now(),
		})
	}

	// 부하 분산 배정: 전략이 broadcast 가 아니면 살아있는 agent 를 조회해 선호 agent 를 지정한다.
	// broadcast(기본)는 조회 없이 PreferredAgentID 를 비운 채 발행 → 현행과 100% 동일.
	h.applyAgentAssignments(cmds, parent.ClusterID)

	for _, cmd := range cmds {
		if err := h.redisService.PublishWorkflowExecution(cmd); err != nil {
			h.logger.Error("Failed to publish sub-execution", "execution_id", cmd.ExecutionID, "cluster_id", parent.ClusterID, "error", err)
		}
	}

	h.logger.Info("published partition sub-executions",
		"workflow_id", workflowID, "parent_execution_id", parent.ID, "sub_count", len(subIDs))

	c.JSON(202, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"execution_id":      parent.ID,
			"workflow_id":       workflowID,
			"status":            parent.Status,
			"distributed":       true,
			"sub_execution_ids": subIDs,
		},
	})
}

// aggregateSubExecutionResult 는 sub-execution 결과를 부모 execution 에 누적한다.
// 모든 sub-execution 이 끝나면 부모를 완료 처리하고 워크플로우 상태를 전이한다.
// 하나라도 error/stopped 면 부모도 그 상태로(부분 실패 표시).
func (h *WorkflowHandler) aggregateSubExecutionResult(parentID string, result *types.GroupExecutionResult) {
	// 원자적 누적: 동시에 여러 sub-execution 결과가 올 수 있으므로 SQL 증분 업데이트.
	h.db.Model(&models.WorkflowExecution{}).Where("id = ?", parentID).
		Updates(map[string]any{
			"total_records":            gorm.Expr("total_records + ?", result.TotalRecords),
			"failed_records":           gorm.Expr("failed_records + ?", result.FailedRecords),
			"completed_sub_executions": gorm.Expr("completed_sub_executions + ?", 1),
		})

	var parent models.WorkflowExecution
	if err := h.db.First(&parent, "id = ?", parentID).Error; err != nil {
		h.logger.Error("aggregate: parent not found", "parent_execution_id", parentID, "error", err)
		return
	}

	// 부분 실패 기록: 하나라도 실패면 부모 error 로 표시(뒤에 완료 판정에서 확정).
	if result.Status == types.PipelineGroupStatusError || result.Status == types.PipelineGroupStatusStopped {
		h.db.Model(&models.WorkflowExecution{}).Where("id = ?", parentID).
			Update("error_message", "one or more sub-executions failed/stopped")
	}

	// 완료 판정은 == 로 한다(>= 아님). SQL 증분이 DB 에서 직렬화되므로 completed 를 total 로
	// 만드는 결과는 정확히 하나뿐 → 그 하나만 완료 처리(동시 결과에서 이중 완료 방지).
	if parent.CompletedSubExecutions != parent.TotalSubExecutions {
		return // 아직 진행 중이거나(<) 이미 다른 결과가 완료 처리함(>)
	}

	// 모든 sub-execution 완료 → 부모 완료 처리.
	now := time.Now()
	finalStatus := string(types.PipelineGroupStatusCompleted)
	if parent.ErrorMessage != "" {
		finalStatus = string(types.PipelineGroupStatusError)
	}
	h.db.Model(&models.WorkflowExecution{}).Where("id = ?", parentID).
		Updates(map[string]any{"status": finalStatus, "completed_at": now})

	wfStatus := "idle"
	if finalStatus == string(types.PipelineGroupStatusError) {
		wfStatus = "error"
	}
	h.db.Model(&models.Workflow{}).Where("id = ?", parent.WorkflowID).Update("status", wfStatus)

	h.logger.Info("partition sub-executions all completed",
		"parent_execution_id", parentID, "status", finalStatus, "total_records", parent.TotalRecords)
}

// subExecutionParent 는 executionID 의 부모 execution id 를 반환한다(sub-execution 이 아니면 "").
func (h *WorkflowHandler) subExecutionParent(executionID string) string {
	var exec models.WorkflowExecution
	if err := h.db.Select("parent_execution_id").First(&exec, "id = ?", executionID).Error; err != nil {
		return ""
	}
	return exec.ParentExecutionID
}

// updateWorkflowStatusUnlessSub 는 워크플로우 상태를 갱신하되, sub-execution 결과면 스킵한다
// (sub 하나 완료로 워크플로우를 전이하면 안 됨 — 취합기가 모든 sub 완료 후 확정).
func (h *WorkflowHandler) updateWorkflowStatusUnlessSub(workflowID, status string, isSub bool) error {
	if isSub {
		return nil
	}
	return h.db.Model(&models.Workflow{}).Where("id = ?", workflowID).Update("status", status).Error
}
