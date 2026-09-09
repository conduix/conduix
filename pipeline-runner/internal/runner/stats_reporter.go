package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/shared/types"
)

// statsReportInterval 은 시간 버킷 통계를 control-plane 에 보내는 주기다.
// 버킷은 시간 단위로 합산되므로 이 주기는 "얼마나 최신인가" 만 정한다. 1분이면 UI 지연이
// 체감되지 않고, 시간당 60회 upsert 는 파이프라인당 무시할 만한 부하다.
const statsReportInterval = time.Minute

// statsReporter 는 GroupExecutor 의 누적 통계를 시간 버킷 델타로 바꿔 전송한다.
//
// realtime 은 종료 결과 콜백이 영구히 발생하지 않으므로(무한 실행) 이 경로가 없으면
// pipeline_hourly_stats 에 아무것도 쌓이지 않는다 — 실제로 그 테이블에 쓰는 살아있는
// 코드가 없었다.
//
// 누적값을 그대로 보내면 CP 의 누적 upsert 와 겹쳐 값이 폭증하므로 델타를 보낸다.
// 시간이 바뀌면 델타 기준을 리셋해 새 버킷이 0 에서 시작하게 한다.
type statsReporter struct {
	controlPlaneURL string
	workflowID      string
	httpClient      *http.Client

	// 파이프라인별 마지막 전송 시점의 누적값. 델타 계산 기준이다.
	lastSent map[string]types.MonitoringStats
	// 현재 집계 중인 시간 버킷. 바뀌면 lastSent 를 비워 새 버킷을 0 부터 센다.
	currentHour time.Time
}

func newStatsReporter(controlPlaneURL, workflowID string) *statsReporter {
	return &statsReporter{
		controlPlaneURL: controlPlaneURL,
		workflowID:      workflowID,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		lastSent:        make(map[string]types.MonitoringStats),
		currentHour:     time.Now().Truncate(time.Hour),
	}
}

// run 은 ctx 가 끝날 때까지 주기적으로 전송한다. 종료 직전 마지막 델타도 보낸다 —
// 안 보내면 마지막 주기(최대 1분)분의 수집량이 통계에서 사라진다.
func (r *statsReporter) run(ctx context.Context, groupExec *executor.GroupExecutor) {
	ticker := time.NewTicker(statsReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.reportOnce(context.WithoutCancel(ctx), groupExec)
			return
		case <-ticker.C:
			r.reportOnce(ctx, groupExec)
		}
	}
}

func (r *statsReporter) reportOnce(ctx context.Context, groupExec *executor.GroupExecutor) {
	info := groupExec.GetMonitoringInfo()
	if info == nil {
		return
	}

	hour := time.Now().Truncate(time.Hour)
	if !hour.Equal(r.currentHour) {
		// 시간 경계를 넘었다 — 새 버킷은 0 에서 시작해야 하므로 기준을 비운다.
		// 직전 버킷의 잔여 델타는 이 호출 전에 이미 전송됐다(주기 < 1시간).
		r.lastSent = make(map[string]types.MonitoringStats)
		r.currentHour = hour
	}

	for i := range info.Pipelines {
		p := &info.Pipelines[i]
		if p.Statistics == nil {
			continue
		}
		delta, changed := r.delta(p.PipelineID, *p.Statistics)
		if !changed {
			continue // 변화 없음 — 빈 upsert 로 DB 를 두드리지 않는다.
		}
		sent := r.send(ctx, &types.HourlyStatsBucket{
			PipelineID:       p.PipelineID,
			PipelineName:     p.PipelineName,
			WorkflowID:       r.workflowID,
			BucketHour:       hour,
			RecordsCollected: delta.RecordsCollected,
			RecordsProcessed: delta.RecordsProcessed,
			CollectionErrors: delta.CollectionErrors,
			ProcessingErrors: delta.ProcessingErrors,
			SampleCount:      1,
		})
		if sent {
			// 성공한 뒤에만 기준을 옮긴다. 실패 시 기준을 유지하면 다음 주기에 델타가
			// 누적돼 재시도되므로 유실이 아니라 지연이 된다.
			r.lastSent[p.PipelineID] = *p.Statistics
		}
	}
}

// delta 는 마지막 전송 성공 이후의 증가분을 반환한다(기준 갱신은 호출자가 한다).
//
// 누적값이 줄어들면(executor 재시작으로 collector 가 새로 만들어짐) 기준이 현재값보다
// 커서 차이가 음수가 된다. 이때 각 항목을 0 으로 깎으면 전 항목이 0 이 되어 changed 가
// false 가 되고, 그 시점부터 통계가 영구히 멈춘다. 리셋을 감지해 현재값 전체를 델타로
// 삼아야 한다 — 음수를 그대로 보내면 CP 의 누적 합계가 깎이므로 그것도 안 된다.
func (r *statsReporter) delta(pipelineID string, cur types.MonitoringStats) (types.MonitoringStats, bool) {
	prev, seen := r.lastSent[pipelineID]
	if !seen || counterReset(cur, prev) {
		return cur, hasAny(cur)
	}

	d := types.MonitoringStats{
		RecordsCollected: cur.RecordsCollected - prev.RecordsCollected,
		RecordsProcessed: cur.RecordsProcessed - prev.RecordsProcessed,
		CollectionErrors: cur.CollectionErrors - prev.CollectionErrors,
		ProcessingErrors: cur.ProcessingErrors - prev.ProcessingErrors,
	}
	return d, hasAny(d)
}

// counterReset 은 어느 항목이든 이전보다 작아졌는지 본다 — 누적 카운터가 되감긴 신호다.
func counterReset(cur, prev types.MonitoringStats) bool {
	return cur.RecordsCollected < prev.RecordsCollected ||
		cur.RecordsProcessed < prev.RecordsProcessed ||
		cur.CollectionErrors < prev.CollectionErrors ||
		cur.ProcessingErrors < prev.ProcessingErrors
}

func hasAny(s types.MonitoringStats) bool {
	return s.RecordsCollected > 0 || s.RecordsProcessed > 0 ||
		s.CollectionErrors > 0 || s.ProcessingErrors > 0
}

// send 는 전송 성공 여부를 반환한다 — 호출자가 성공 시에만 델타 기준을 옮긴다.
func (r *statsReporter) send(ctx context.Context, bucket *types.HourlyStatsBucket) bool {
	if r.controlPlaneURL == "" {
		return false
	}
	body, err := json.Marshal(bucket)
	if err != nil {
		return false
	}

	url := fmt.Sprintf("%s/api/v1/internal/stats/hourly", r.controlPlaneURL)
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		// 통계 전송 실패는 파이프라인을 멈추지 않는다.
		slog.Debug("stats report failed", "pipeline_id", bucket.PipelineID, "error", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Debug("stats report rejected", "pipeline_id", bucket.PipelineID, "status", resp.StatusCode)
		return false
	}
	return true
}
