package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/shared/types"
)

// streaming 실행이 스스로 끝났는지 확인하는 주기.
// realtime 은 원래 무한 실행이므로 이 폴링은 "비정상 종료 감지" 용도다 —
// 1초면 좀비 상태가 길게 남지 않고, 부하도 무시할 수준이다.
const streamingWatchInterval = time.Second

// 종료 보고에 쓰는 타임아웃. 파드가 곧 사라질 수 있으므로 길게 잡지 않는다.
const streamingReportTimeout = 10 * time.Second

// watchStreamingCompletion 은 streaming 실행이 스스로 종료됐는지 감시한다.
//
// 왜 필요한가: streaming 은 ctx 취소(SIGTERM/stop 명령)로만 끝나는 것으로 가정돼 있었지만,
// GroupExecutor 는 스스로 끝나는 경로를 갖고 있다 — 대표적으로 DDL 방어
// (group_executor.go 의 schema_changed)와 소스 오류다.
//
// 그 경우 기존 코드는 아무것도 하지 않고 ctx.Done() 을 계속 기다렸다. 결과:
//   - control-plane 의 workflow_executions.status 가 영구히 "running"
//   - 그 status 를 근거로 agent 의 sweepAbandonedDeployments 가 live 로 판정 → 파드가 영구 잔존
//   - web-ui 는 running 으로 보이는데 모니터링 데이터는 비어 있음(정지한 파이프라인은
//     statsCollectors 에서 제거되므로) → 사용자에게 성공도 실패도 안 보임
//
// 실측: restrooms realtime 파이프라인이 무관한 테이블 DDL 로 정지한 뒤 9분간
// 1/1 Running 좀비로 남았고 UI 에는 아무 표시도 없었다.
//
// 종료가 감지되면 결과를 control-plane 에 보고하고 cancel 로 종료 경로를 태운다.
// 파드가 실제로 죽어야 K8s 가 Deployment 를 재시작하거나 agent 가 회수할 수 있다.
func (r *Runner) watchStreamingCompletion(ctx context.Context, groupExec *executor.GroupExecutor, startTime time.Time, cancel context.CancelFunc) {
	ticker := time.NewTicker(streamingWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // 정상 종료(stop/SIGTERM) — 이 경로는 runStreaming 이 처리한다
		case <-ticker.C:
			exec := groupExec.Execution()
			if exec == nil {
				continue
			}
			if !streamingEnded(exec.Status) {
				continue
			}

			slog.Warn("streaming execution ended on its own — reporting and shutting down",
				"workflow_id", r.cfg.WorkflowID, "execution_id", r.cfg.ExecutionID,
				"status", exec.Status, "error", exec.ErrorMessage)

			if err := r.sendStreamingResult(startTime, exec); err != nil {
				// 보고 실패해도 종료는 진행한다 — 살아있는 좀비보다 죽은 파드가 낫다.
				// (파드가 사라지면 agent 의 회수 경로가 불일치를 잡을 수 있다.)
				slog.Error("failed to report streaming result",
					"workflow_id", r.cfg.WorkflowID, "execution_id", r.cfg.ExecutionID, "error", err)
			}

			r.healthServer.SetStatus("error")
			cancel()
			return
		}
	}
}

// streamingEnded 는 이 status 가 "더 이상 데이터를 흘리지 않는 상태"인지 판정한다.
//
// running/paused 만 계속 사는 상태다. paused 는 resume 을 기다리는 정상 상태이므로
// 종료로 보지 않는다 — 여기서 종료 처리하면 일시정지가 파드 종료가 된다.
func streamingEnded(status types.WorkflowStatus) bool {
	switch status {
	case types.PipelineGroupStatusRunning, types.PipelineGroupStatusPaused:
		return false
	case "":
		return false // 아직 시작 상태가 기록되지 않음
	default:
		// completed/error/stopped 및 앞으로 추가될 종료 상태.
		// 화이트리스트(계속 사는 상태)로 판정해, 새 종료 상태가 생겨도
		// 좀비로 남지 않는다 — 블랙리스트면 추가를 잊는 순간 이 버그가 재발한다.
		return true
	}
}

// sendStreamingResult 는 streaming 실행의 종료를 control-plane 에 보고한다.
//
// batch 의 sendBatchResult 와 엔드포인트가 다르다. batch 는 Job 결과 콜백
// (JobExecutionResult, CallbackURL)을 쓰지만, streaming 은 Job 이 아니라 Deployment 이고
// CallbackURL 이 주입되지 않는다. agent 의 in-process 경로가 쓰는
// POST /workflows/:id/executions/:executionId/result 를 재사용한다 — 그 핸들러는 이미
// error 매핑과 error_message 저장을 갖추고 있다.
func (r *Runner) sendStreamingResult(startTime time.Time, exec *types.PipelineGroupExecution) error {
	if r.cfg.ControlPlaneURL == "" {
		return fmt.Errorf("control plane URL not configured — cannot report streaming result")
	}

	completedAt := time.Now()
	result := &types.GroupExecutionResult{
		ExecutionID:     r.cfg.ExecutionID,
		WorkflowID:      r.cfg.WorkflowID,
		AgentID:         r.cfg.AgentID,
		Status:          exec.Status,
		PipelineResults: exec.PipelineResults,
		TotalRecords:    exec.TotalRecords,
		FailedRecords:   exec.FailedRecords,
		StartedAt:       startTime,
		CompletedAt:     &completedAt,
		ErrorMessage:    streamingErrorMessage(exec),
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming result: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/workflows/%s/executions/%s/result",
		r.cfg.ControlPlaneURL, r.cfg.WorkflowID, r.cfg.ExecutionID)

	// 종료 중이라 넘겨받은 ctx 는 이미 취소됐을 수 있으므로 별도 컨텍스트를 쓴다.
	sendCtx, cancel := context.WithTimeout(context.Background(), streamingReportTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send streaming result: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("result endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	slog.Info("streaming result reported",
		"workflow_id", r.cfg.WorkflowID, "execution_id", r.cfg.ExecutionID, "status", result.Status)
	return nil
}

// streamingErrorMessage 는 공용 사유 판정에 streaming 전용 폴백을 덧붙인다.
//
// 사유 추출 정책 자체는 executor.ExecutionErrorMessage 한 곳에 둔다 — 모니터링 화면과
// 실행 이력이 같은 사유를 보여야 하고, 여기서 따로 구현하면 둘이 어긋난다.
//
// 폴백이 필요한 이유: 실행 이력에는 "실패" 만 남고 이유가 비면 사용자가 원인을 찾을
// 단서가 없다. 모니터링 응답은 status 를 별도 필드로 이미 보여주므로 폴백이 불필요하지만,
// 종료 보고는 error_message 가 화면에 뜨는 유일한 사유다.
func streamingErrorMessage(exec *types.PipelineGroupExecution) string {
	if msg := executor.ExecutionErrorMessage(exec); msg != "" {
		return msg
	}
	if exec.Status != types.PipelineGroupStatusCompleted {
		return fmt.Sprintf("streaming execution ended with status %s", exec.Status)
	}
	return ""
}
