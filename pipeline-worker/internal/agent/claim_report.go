package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// 위임 접수를 control-plane 에 알린다.
//
// 왜 필요한가: batch/streaming 실행은 agent 가 K8s 리소스를 위임 생성하고 끝난다
// (fire-and-forget). Job/Deployment 는 agent heartbeat 의 running_execs 에 등록되지
// 않으므로 control-plane 은 "이 실행을 누가 받았는지" 를 실행이 끝날 때까지 알 수 없었다.
//
// 그래서 control-plane 은 batch 를 stale 감지에서 제외해야 했고(정상 실행 중인 Job 을
// 죽이지 않기 위해), 그 결과 실행 명령이 유실되면(Redis pub/sub 는 구독자가 없어도
// 에러를 내지 않는다) 워크플로우가 영구히 running 으로 남는 구멍이 생겼다.
//
// 접수 사실을 즉시 알리면 control-plane 이 "유예 시간이 지났는데 접수 흔적이 없는 실행" 만
// 골라 안전하게 실패 확정할 수 있다.

// claimReportTimeout 은 접수 보고에 쓰는 상한이다.
// 보고 실패가 실행을 막아서는 안 되므로 짧게 잡는다 — 실행은 이미 시작됐다.
const claimReportTimeout = 5 * time.Second

// reportExecutionClaim 은 위임 접수를 알린다.
//
// 실패해도 실행을 되돌리지 않는다. 보고가 유실되면 control-plane 이 이 실행을
// "접수되지 않음" 으로 오판할 수 있지만, 그때도 K8s Job 은 계속 돌아 결과 콜백으로
// 최종 상태가 정정된다 — 보고 실패 때문에 실행을 죽이는 것이 더 나쁘다.
func (a *Agent) reportExecutionClaim(workflowID, executionID, delegatedTo string) {
	if a.controlPlaneURL == "" || executionID == "" {
		return
	}

	body, err := json.Marshal(map[string]string{
		"agent_id":     a.ID,
		"delegated_to": delegatedTo,
	})
	if err != nil {
		slog.Error("failed to marshal execution claim", "execution_id", executionID, "error", err)
		return
	}

	url := fmt.Sprintf("%s/api/v1/internal/workflows/%s/executions/%s/claim",
		a.controlPlaneURL, workflowID, executionID)

	ctx, cancel := context.WithTimeout(a.ctx, claimReportTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create claim request", "execution_id", executionID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		slog.Warn("failed to report execution claim — control plane may treat it as unclaimed",
			"execution_id", executionID, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("claim report rejected",
			"execution_id", executionID, "status_code", resp.StatusCode)
		return
	}

	slog.Debug("execution claim reported",
		"execution_id", executionID, "delegated_to", delegatedTo)
}
