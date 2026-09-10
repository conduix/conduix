package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// 실행 시작의 핵심 로직을 HTTP 와 분리한다.
//
// 왜 분리하는가: runner 재빌드가 필요해 실행이 막힐 때, 예전에는 409 를 던지고 사용자가
// 빌드 화면으로 이동해 직접 빌드한 뒤 실행을 다시 누르게 했다. 이제 서버가 빌드를 걸고
// 완료 후 실행을 이어서 시작하는데, 그 시점에는 원래의 HTTP 요청이 이미 끝나 gin.Context 가
// 없다. 같은 실행 로직을 두 번 구현하면 클러스터 확정·파티션 배정·스냅샷 정책이 두 곳으로
// 갈려 한쪽만 고쳐지는 버그가 난다.

// startWorkflowOutcome 은 실행 시작 결과다. 호출자가 HTTP 응답 또는 로그로 변환한다.
type startWorkflowOutcome struct {
	ExecutionID string
	Status      string
	StartedAt   time.Time
	// SubExecutions 는 파티션 분산으로 나눠 발행한 sub-execution 수다(1 이면 단일 실행).
	SubExecutions int
}

// startWorkflowBlocked 는 실행을 시작하지 못한 사유를 담는다.
// 자동 해소가 가능한 사유(빌드 필요)와 그렇지 않은 사유(클러스터 없음)를 호출자가 구분해야 한다.
type startWorkflowBlocked struct {
	Reason string
	// BuildRequired 가 non-nil 이면 빌드로 해소 가능한 차단이다.
	BuildRequired *services.BuildRequiredError
}

func (b *startWorkflowBlocked) Error() string {
	if b.BuildRequired != nil {
		return b.BuildRequired.Error()
	}
	return b.Reason
}

// 차단 사유 식별자. 문자열 비교로 분기하던 것을 상수로 고정한다 —
// 오타가 나면 조용히 500 으로 떨어져 원인 파악이 어려워진다.
const (
	blockedWorkflowRunning = "WORKFLOW_RUNNING"
	blockedBuildRequired   = "BUILD_REQUIRED"
)

// startWorkflowCore 는 실행 기록을 만들고 실행 명령을 발행한다.
//
// triggeredBy 는 "user"(사용자 요청) 또는 "auto_build"(빌드 완료 후 이어서 시작)다.
// 이 값이 실행 이력에 남아, 자동으로 시작된 실행을 사용자가 구분할 수 있다.
func (h *WorkflowHandler) startWorkflowCore(workflowID, userID, triggeredBy string) (*startWorkflowOutcome, error) {
	var workflow models.Workflow
	var execution *models.WorkflowExecution
	resolvedClusterID := ""
	resolvedRunnerVersionID := ""
	var buildRequiredErr *services.BuildRequiredError

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&workflow, "id = ?", workflowID).Error; err != nil {
			return err
		}

		if workflow.Status == string(types.PipelineGroupStatusRunning) {
			return fmt.Errorf("%s", blockedWorkflowRunning)
		}

		versionID, _, _, rerr := h.runnerResolver.ResolveRunnerVersion(&workflow)
		if rerr != nil {
			var bre *services.BuildRequiredError
			if errors.As(rerr, &bre) {
				buildRequiredErr = bre
				return fmt.Errorf("%s", blockedBuildRequired)
			}
			return rerr
		}
		resolvedRunnerVersionID = versionID

		cid, cerr := h.resolveExecutionCluster(tx, workflow.ClusterID)
		if cerr != nil {
			return cerr
		}
		resolvedClusterID = cid

		// 발행 전 agent 가용성 확인. Redis pub/sub 는 구독자가 없어도 에러를 내지 않으므로,
		// 이 검사가 없으면 명령이 조용히 사라지고 워크플로우는 영구히 running 으로 남는다
		// (batch 는 stale 감지 대상이 아니라 자동 복구조차 없다).
		//
		// 판정은 Redis heartbeat 로 한다 — DB agents 테이블에는 죽은 pod 레코드가 누적되어
		// (실측 66행) 살아있는 agent 를 죽었다고 오판한다.
		if aerr := services.EnsureLiveAgent(h.redisService, resolvedClusterID); aerr != nil {
			return aerr
		}

		// 비정상 종료로 running 에 남은 이전 실행 정리.
		now := time.Now()
		tx.Model(&models.WorkflowExecution{}).
			Where("workflow_id = ? AND status = ?", workflowID, string(types.PipelineGroupStatusRunning)).
			Updates(map[string]any{
				"status":        string(types.PipelineGroupStatusStopped),
				"completed_at":  now,
				"error_message": "Terminated: new execution started",
			})

		execution = &models.WorkflowExecution{
			ID:                uuid.New().String(),
			WorkflowID:        workflowID,
			ClusterID:         resolvedClusterID,
			Status:            string(types.PipelineGroupStatusRunning),
			StartedAt:         time.Now(),
			PipelinesSnapshot: workflow.PipelinesConfig,
			RunnerVersionID:   resolvedRunnerVersionID,
			TriggeredBy:       triggeredBy,
			TriggeredByID:     userID,
			CreatedAt:         time.Now(),
		}

		if err := tx.Create(execution).Error; err != nil {
			return err
		}

		workflow.Status = string(types.PipelineGroupStatusRunning)
		workflow.LastRunAt = &execution.StartedAt
		return tx.Save(&workflow).Error
	})

	if err != nil {
		switch err.Error() {
		case blockedWorkflowRunning:
			return nil, &startWorkflowBlocked{Reason: blockedWorkflowRunning}
		case blockedBuildRequired:
			return nil, &startWorkflowBlocked{Reason: blockedBuildRequired, BuildRequired: buildRequiredErr}
		}
		return nil, err
	}

	h.logger.Info("Workflow status updated to running",
		"workflow_id", workflowID, "triggered_by", triggeredBy)

	var pipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		if err := json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines); err != nil {
			h.logger.Error("Failed to parse pipelines config", "workflow_id", workflowID, "error", err)
		}
	}

	workflowConfig := &types.Workflow{
		ID:            workflow.ID,
		ProjectID:     workflow.ProjectID,
		Name:          workflow.Name,
		Type:          types.PipelineGroupType(workflow.Type),
		ExecutionMode: types.ExecutionMode(workflow.ExecutionMode),
		Pipelines:     pipelines,
	}

	outcome := &startWorkflowOutcome{
		ExecutionID:   execution.ID,
		Status:        execution.Status,
		StartedAt:     execution.StartedAt,
		SubExecutions: 1,
	}

	// 파티션 분산: partitioned source 가 있으면 sub-execution 으로 나눠 발행한다.
	partitionGroups := planPartitionGroups(pipelines)
	if len(partitionGroups) > 1 {
		h.publishSubExecutionsCore(workflowID, userID, execution, workflow.JobConfig,
			workflowConfig, partitionGroups, resolvedRunnerVersionID)
		outcome.SubExecutions = len(partitionGroups)
		return outcome, nil
	}

	cmd := &types.WorkflowExecutionCommand{
		ID:              uuid.New().String(),
		WorkflowID:      workflowID,
		ExecutionID:     execution.ID,
		TargetClusterID: execution.ClusterID,
		TriggeredBy:     triggeredBy,
		UserID:          userID,
		JobConfig:       workflow.JobConfig,
		WorkflowConfig:  workflowConfig,
		RunnerVersionID: resolvedRunnerVersionID,
		Timestamp:       time.Now(),
	}

	if err := h.redisService.PublishWorkflowExecution(cmd); err != nil {
		h.logger.Error("Failed to publish workflow execution",
			"execution_id", execution.ID, "cluster_id", execution.ClusterID, "error", err)
	}

	return outcome, nil
}

// startAfterBuild 는 자동 빌드 완료 후 실행을 시작한다(AutoBuilder.StartFn).
//
// 빌드 직후에도 여전히 빌드가 필요하다고 나오면 재시도하지 않는다 — 무한 빌드 루프를 만든다.
// 그 경우는 빌드가 이 워크플로우의 요구를 충족하지 못한 것이므로 실행 이력에 사유를 남긴다.
func (h *WorkflowHandler) startAfterBuild(workflowID, userID string) error {
	outcome, err := h.startWorkflowCore(workflowID, userID, triggeredByAutoBuild)
	if err != nil {
		var blocked *startWorkflowBlocked
		if errors.As(err, &blocked) && blocked.Reason == blockedBuildRequired {
			// 빌드했는데도 부족하다 = 재시도해도 같은 결과다. 루프를 만들지 않는다.
			h.recordAutoStartFailure(workflowID, userID,
				fmt.Sprintf("자동 빌드를 마쳤지만 여전히 실행할 수 없습니다: %s", blocked.Error()))
			return err
		}
		h.recordAutoStartFailure(workflowID, userID,
			fmt.Sprintf("자동 빌드 후 실행 시작에 실패했습니다: %s", err.Error()))
		return err
	}

	h.logger.Info("workflow started after auto-build",
		"workflow_id", workflowID, "execution_id", outcome.ExecutionID)
	return nil
}

// triggeredByAutoBuild 는 자동 빌드 후 시작된 실행의 트리거 값이다.
// 사용자가 실행 이력에서 "내가 누른 것" 과 구분할 수 있어야 한다.
const triggeredByAutoBuild = "auto_build"

// recordAutoStartFailure 는 자동 시작 실패를 실행 이력에 남긴다.
//
// 로그에만 남기면 사용자는 실행 버튼을 눌렀는데 아무 일도 안 일어난 것으로 보인다 —
// 그것이 이번에 고치려는 문제 자체다. 실패한 실행 레코드를 만들어 화면에 사유가 뜨게 한다.
func (h *WorkflowHandler) recordAutoStartFailure(workflowID, userID, message string) {
	now := time.Now()
	rec := &models.WorkflowExecution{
		ID:            uuid.New().String(),
		WorkflowID:    workflowID,
		Status:        string(types.PipelineGroupStatusError),
		StartedAt:     now,
		CompletedAt:   &now,
		ErrorMessage:  message,
		TriggeredBy:   triggeredByAutoBuild,
		TriggeredByID: userID,
		CreatedAt:     now,
	}
	if err := h.db.Create(rec).Error; err != nil {
		h.logger.Error("failed to record auto-start failure",
			"workflow_id", workflowID, "error", err)
		return
	}
	h.db.Model(&models.Workflow{}).Where("id = ?", workflowID).
		Update("status", string(types.PipelineGroupStatusError))
}
