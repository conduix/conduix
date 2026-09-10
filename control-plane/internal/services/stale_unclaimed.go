package services

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// batch 실행의 자동 복구 구멍을 메운다.
//
// 배경: detectStaleExecutions 는 agent heartbeat 의 running_execs 를 근거로 orphan 을
// 판정하는데, batch 는 agent 가 K8s Job 을 위임 생성만 하고 끝나 heartbeat 에 등록되지
// 않는다. 그래서 batch 를 감지 대상에서 아예 제외했다 — 안 하면 정상 실행 중인 Job 을
// 2분 뒤 죽인다.
//
// 그 제외가 만든 구멍: 실행 명령은 Redis pub/sub(at-most-once) 로 발행되므로 받을 agent 가
// 없으면 조용히 사라진다. realtime 은 위 감지기가 2분 뒤 error 로 되돌리지만 batch 는
// 아무도 손대지 않아 영구히 running 으로 남는다. 사용자는 "실행 시작됨" 응답을 받고도
// 진행도 실패도 보지 못한다.
//
// 여기서는 heartbeat 가 아니라 **위임 접수 흔적**(agent_id/delegated_to)을 근거로 판정한다.
// 위임에 성공한 agent 는 claim 을 보고하므로, 유예 시간이 지났는데도 접수 흔적이 없는
// 실행은 아무도 받지 않은 것이다. 정상 실행 중인 Job 은 접수 기록이 있어 오판되지 않는다.

// unclaimedGrace 는 발행 후 접수 보고를 기다리는 시간이다.
//
// agent 가 명령을 받아 K8s Job 을 만들고 claim 을 보고하기까지의 시간을 넉넉히 덮어야
// 한다 — 짧으면 느린 클러스터에서 정상 실행을 죽인다. staleGrace(2분)와 같은 값을 쓰지
// 않고 별도로 두는 이유는, 이 판정이 heartbeat 가 아니라 접수 보고 한 번에 의존해
// 실패 비용이 다르기 때문이다.
const unclaimedGrace = 3 * time.Minute

// detectUnclaimedExecutions 는 아무 agent 도 접수하지 않은 실행을 실패로 확정한다.
//
// 대상은 위임 실행(batch)이다. realtime 은 heartbeat 기반 감지가 이미 담당한다 —
// 두 감지기가 같은 실행을 보면 중복 전이가 된다.
func (s *SchedulerService) detectUnclaimedExecutions() {
	cutoff := time.Now().Add(-unclaimedGrace)

	var execs []models.WorkflowExecution
	err := s.db.Where(
		"status = ? AND started_at < ? AND (agent_id IS NULL OR agent_id = '') AND workflow_id IN (?)",
		string(types.PipelineGroupStatusRunning), cutoff,
		s.db.Model(&models.Workflow{}).Select("id").
			Where("type = ?", string(types.WorkflowTypeBatch)),
	).Find(&execs).Error
	if err != nil {
		slog.Error("unclaimed-check: query failed", "error", err)
		return
	}

	if len(execs) == 0 {
		return
	}

	now := time.Now()
	for i := range execs {
		exec := &execs[i]

		exec.Status = string(types.PipelineGroupStatusError)
		exec.CompletedAt = &now
		exec.ErrorMessage = unclaimedErrorMessage(exec.ClusterID)
		if err := s.db.Save(exec).Error; err != nil {
			slog.Error("unclaimed-check: failed to mark execution failed",
				"execution_id", exec.ID, "workflow_id", exec.WorkflowID, "error", err)
			continue
		}

		// sub-execution 이면 부모 취합을 진행한다 — 안 그러면 부모가 영구 미완료로 갇힌다
		// (heartbeat 감지기와 같은 정합성 규칙).
		if exec.ParentExecutionID != "" {
			s.advanceParentOnStaleSub(exec.ParentExecutionID)
			slog.Warn("marked unclaimed sub-execution as failed",
				"execution_id", exec.ID, "parent_execution_id", exec.ParentExecutionID)
			continue
		}

		// 워크플로우를 running 에서 풀어준다. 안 풀면 재실행이 영구히 막힌다
		// (WORKFLOW_RUNNING 으로 409).
		if err := s.db.Model(&models.Workflow{}).
			Where("id = ? AND status = ?", exec.WorkflowID, string(types.PipelineGroupStatusRunning)).
			Update("status", string(types.PipelineGroupStatusError)).Error; err != nil {
			slog.Error("unclaimed-check: failed to reset workflow status",
				"workflow_id", exec.WorkflowID, "execution_id", exec.ID, "error", err)
		}

		slog.Warn("marked unclaimed execution as failed — no agent accepted it",
			"execution_id", exec.ID, "workflow_id", exec.WorkflowID, "cluster_id", exec.ClusterID)
	}
}

// unclaimedErrorMessage 는 사용자가 다음에 무엇을 확인해야 하는지까지 담는다.
// "orphaned" 같은 내부 용어만 남기면 사용자가 조치를 알 수 없다.
func unclaimedErrorMessage(clusterID string) string {
	return fmt.Sprintf(
		"실행 명령을 접수한 agent 가 없습니다(%s 이상 대기). 클러스터 %s 의 agent 가 실행 중인지 확인하세요.",
		unclaimedGrace, clusterID)
}
