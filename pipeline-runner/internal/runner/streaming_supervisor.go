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

	"github.com/conduix/conduix/shared/types"
)

// 상주 파드의 감시 루프.
//
// 두 가지를 겸한다:
//  1. 실행 고루틴의 생존 점검 — 멈춘 실행을 찾아 정리한다(체크포인트에서 재개된다)
//  2. 중앙 보고 — 실행별 상태·통계를 control-plane 에 올린다
//
// 왜 보고를 겸하는가: 중앙이 파드에 일일이 물어보는 pull 방식은 파드가 느리거나 응답이
// 늦을 때 "죽었다" 로 오판하기 쉽다. 파드가 스스로 올리면 중앙은 받은 것만 보면 되고,
// 모니터링·체크포인트 관리가 한 곳으로 모인다.
//
// 판정 기조: 체크포인트가 정확하므로 재시작 비용이 작다. 이상이 의심되면 살려두기보다
// 정리하고 체크포인트에서 재개하는 쪽이 낫다 — 멈춘 채 살아 있는 실행은 아무도 모르게
// 데이터만 밀리게 한다.

const (
	// superviseInterval 은 감시·보고 주기다.
	// 너무 짧으면 control-plane 에 부하가 되고, 너무 길면 멈춘 실행을 늦게 찾는다.
	superviseInterval = 30 * time.Second

	// superviseReportTimeout 은 중앙 보고 1회 상한이다.
	// 보고가 느려도 감시 루프가 막히면 안 된다.
	superviseReportTimeout = 10 * time.Second
)

// PodStatusReport 는 상주 파드가 주기적으로 올리는 상태다.
type PodStatusReport struct {
	AgentID    string               `json:"agent_id,omitempty"`
	PodName    string               `json:"pod_name,omitempty"`
	ReportedAt time.Time            `json:"reported_at"`
	Executions []PodExecutionStatus `json:"executions"`
}

// PodExecutionStatus 는 실행 하나의 상태다.
// 중앙이 이것만 보고 모니터링·체크포인트·이상 판정을 할 수 있어야 한다.
type PodExecutionStatus struct {
	ExecutionID string    `json:"execution_id"`
	WorkflowID  string    `json:"workflow_id"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	// LastBeatAt 은 이 실행 고루틴의 마지막 생존 신호다.
	// 처리량과 별개다 — 느린 소스로 레코드가 0건이어도 살아 있으면 갱신된다.
	LastBeatAt time.Time `json:"last_beat_at"`
	// Alive 는 생존 판정 결과다. false 면 파드가 그 실행을 정리했다.
	Alive bool `json:"alive"`

	TotalRecords  int64  `json:"total_records"`
	FailedRecords int64  `json:"failed_records"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

// superviseExecutions 는 파드가 살아 있는 동안 감시·보고를 반복한다.
func (r *Runner) superviseExecutions(ctx context.Context, reg *streamingRegistry) {
	ticker := time.NewTicker(superviseInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report := r.checkAndCollect(reg)
			if len(report.Executions) == 0 {
				continue // 빈 파드는 보고할 것이 없다
			}
			if err := r.sendPodStatus(ctx, report); err != nil {
				// 보고 실패는 실행에 영향을 주지 않는다. 중앙은 다음 주기에 받는다.
				slog.Debug("pod status report failed", "error", err)
			}
		}
	}
}

// checkAndCollect 는 생존을 점검하면서 보고할 상태를 모은다.
//
// 점검과 수집을 한 번에 하는 이유: 따로 돌면 "점검 시점에는 살아 있었는데 보고에는
// 죽은 것으로 나오는" 불일치가 생긴다.
func (r *Runner) checkAndCollect(reg *streamingRegistry) PodStatusReport {
	report := PodStatusReport{
		AgentID:    r.cfg.AgentID,
		ReportedAt: time.Now(),
	}

	for _, id := range reg.IDs() {
		e := reg.get(id)
		if e == nil {
			continue
		}

		st := PodExecutionStatus{
			ExecutionID: e.executionID,
			WorkflowID:  e.workflowID,
			StartedAt:   e.startedAt,
			LastBeatAt:  e.lastBeat(),
			Alive:       true,
		}

		if e.exec != nil {
			if execution := e.exec.Execution(); execution != nil {
				st.Status = string(execution.Status)
				st.TotalRecords = execution.TotalRecords
				st.FailedRecords = execution.FailedRecords
				st.ErrorMessage = execution.ErrorMessage
			}
		}

		// 생존 판정: 신호가 끊긴 실행은 정리한다.
		// 시작 직후(신호 없음)는 유예한다 — 아직 첫 beat 전일 수 있다.
		beat := e.lastBeat()
		stale := !beat.IsZero() && time.Since(beat) > livenessTimeout
		startupGrace := time.Since(e.startedAt) < livenessTimeout
		if stale && !startupGrace {
			st.Alive = false
			st.ErrorMessage = fmt.Sprintf(
				"execution goroutine stopped responding for %s — restarting from checkpoint",
				time.Since(beat).Round(time.Second))

			slog.Error("execution liveness lost, removing from pod",
				"execution_id", e.executionID, "workflow_id", e.workflowID,
				"last_beat", beat, "since", time.Since(beat).String())

			// 정리하면 control-plane 의 reconcile 백스톱이 체크포인트에서 재개한다.
			if err := reg.Stop(e.executionID); err != nil {
				slog.Warn("failed to remove stalled execution", "execution_id", e.executionID, "error", err)
			}
		}

		report.Executions = append(report.Executions, st)
	}
	return report
}

// sendPodStatus 는 파드 상태를 control-plane 에 올린다.
func (r *Runner) sendPodStatus(ctx context.Context, report PodStatusReport) error {
	if r.cfg.ControlPlaneURL == "" {
		return nil
	}

	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal pod status: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, superviseReportTimeout)
	defer cancel()

	url := r.cfg.ControlPlaneURL + "/api/v1/internal/streaming/pod-status"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("pod-status returned %d: %s", resp.StatusCode, string(msg))
	}
	return nil
}

// 컴파일 시 타입 확인용(보고 구조가 모니터링 타입과 어긋나지 않게).
var _ = types.ExecutionMonitoringInfo{}
