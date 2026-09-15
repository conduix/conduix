package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 상주 streaming pod 에서 실행 단위로 도는 보조 루프들.
//
// 기존 구현은 "파드 하나 = 실행 하나" 전제라 groupExec 를 직접 받았다. 상주 파드에서는
// 실행이 중간에 사라질 수 있으므로(다른 실행의 stop 과 무관하게) 레지스트리를 통해
// 매번 조회한다 — 사라진 실행을 붙들고 있으면 이미 끝난 것을 계속 보고하게 된다.

// runStatsReporter 는 실행 하나의 시간 버킷 통계를 control-plane 에 보낸다.
//
// realtime 은 종료 콜백이 영구히 발생하지 않으므로 이 경로가 없으면 시간당 수집량·에러량이
// 어디에도 남지 않는다. 리포터를 실행별로 두는 이유: statsReporter 의 lastSent 가
// pipelineID 키라, 여러 워크플로우를 한 리포터에 실으면 같은 pipelineID 가 충돌한다.
func (r *Runner) runStatsReporter(ctx context.Context, reg *streamingRegistry, executionID, workflowID string) {
	e := reg.get(executionID)
	if e == nil || e.exec == nil {
		return
	}
	// statsReporter.run 은 ctx 종료까지 블록한다. 실행이 먼저 멈추면 그 ctx(execCtx)가
	// 취소되므로 여기서도 함께 끝난다.
	newStatsReporter(r.cfg.ControlPlaneURL, workflowID).run(ctx, e.exec)
}

// watchExecutionCompletion 은 실행이 스스로 끝났는지 감시한다.
//
// realtime 은 원래 무한 실행이지만 스스로 끝나는 경로가 있다(DDL 방어의 schema_changed,
// 소스 오류). 감시가 없으면 실행은 죽었는데 파드는 살아 control-plane 의 status 가
// running 에 머물고, 화면에는 성공도 실패도 안 보인다(실측).
//
// 상주 파드에서 달라지는 점: 예전에는 종료를 감지하면 프로세스를 죽였지만(cancel),
// 이제는 그 실행만 레지스트리에서 걷어낸다 — 다른 실행과 파드는 계속 살아야 한다.
func (r *Runner) watchExecutionCompletion(ctx context.Context, reg *streamingRegistry, executionID, workflowID string, startTime time.Time) {
	ticker := time.NewTicker(streamingWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e := reg.get(executionID)
			if e == nil {
				return // 이미 stop 으로 걷어냈다
			}
			if e.exec == nil {
				continue // 아직 시작 중
			}
			execution := e.exec.Execution()
			if execution == nil || !streamingEnded(execution.Status) {
				continue
			}

			slog.Warn("streaming execution ended on its own — reporting and removing from pod",
				"execution_id", executionID, "workflow_id", workflowID,
				"status", execution.Status, "error", execution.ErrorMessage)

			if err := r.sendStreamingResultFor(executionID, workflowID, startTime, execution); err != nil {
				// 보고 실패해도 정리는 진행한다. 남겨두면 끝난 실행이 파드 자리를 차지한다.
				slog.Error("failed to report streaming result",
					"execution_id", executionID, "error", err)
			}

			if err := reg.Stop(executionID); err != nil {
				slog.Warn("failed to remove ended execution", "execution_id", executionID, "error", err)
			}
			return
		}
	}
}

// sendStreamingResultFor 는 실행 하나의 종료를 control-plane 에 보고한다.
// 기존 sendStreamingResult 는 r.cfg 의 단일 실행 id 를 쓰므로, 상주 파드용으로 분리한다.
func (r *Runner) sendStreamingResultFor(executionID, workflowID string, startTime time.Time, execution *types.PipelineGroupExecution) error {
	saveExecID, saveWfID := r.cfg.ExecutionID, r.cfg.WorkflowID
	r.cfg.ExecutionID, r.cfg.WorkflowID = executionID, workflowID
	defer func() { r.cfg.ExecutionID, r.cfg.WorkflowID = saveExecID, saveWfID }()
	return r.sendStreamingResult(startTime, execution)
}
