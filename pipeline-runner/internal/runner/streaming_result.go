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
