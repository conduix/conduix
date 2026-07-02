package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/conduix/conduix/pipeline-core/pkg/output"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/shared/metrics"
	"github.com/conduix/conduix/shared/types"
)

// failureGuard는 파이프라인 실행 중 sink/변환 실패를 추적한다.
//   - 실패 카운트(연속/누적)를 관리하고 로그를 남긴다(필수).
//   - 서킷 브레이커: 설정 임계를 넘으면 tripped=true가 되어 실행을 조기 에러 종료시킨다.
//     실패가 계속 쌓인 채 도는 것이 오히려 부하/문제이므로 차단하는 것이 목적.
//   - DLQ가 설정돼 있으면 실패 레코드를 적재한다(서킷과 독립).
//
// 동시(배치 워커)에서 호출되므로 mutex로 보호한다.
type failureGuard struct {
	mu                  sync.Mutex
	consecutiveFailures int
	totalFailures       int
	tripped             bool
	trippedReason       string

	cb  *types.CircuitBreakerPolicy
	dlq output.DLQOutput

	workflowID string
	pipelineID string
	pipeline   string
}

// newFailureGuard는 FailurePolicy로부터 가드를 만든다. DLQ 설정이 있으면 DLQ 출력을 연다.
func newFailureGuard(fp *types.FailurePolicy, workflowID, pipelineID, pipelineName string) (*failureGuard, error) {
	g := &failureGuard{
		workflowID: workflowID,
		pipelineID: pipelineID,
		pipeline:   pipelineName,
	}
	if fp == nil {
		return g, nil
	}
	if fp.CircuitBreaker != nil && fp.CircuitBreaker.Enabled {
		g.cb = fp.CircuitBreaker
	}
	if fp.DLQ != nil && fp.DLQ.Enabled {
		dlq, err := output.NewDLQOutput(*fp.DLQ)
		if err != nil {
			return nil, fmt.Errorf("failed to create DLQ output: %w", err)
		}
		if err := dlq.Open(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to open DLQ output: %w", err)
		}
		g.dlq = dlq
	}
	return g, nil
}

// recordSuccess는 성공을 기록하고 연속 실패 카운트를 리셋한다.
func (g *failureGuard) recordSuccess() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.consecutiveFailures = 0
	g.mu.Unlock()
}

// recordFailure는 실패를 기록하고, DLQ 설정 시 레코드를 적재하며,
// 서킷 임계 초과 시 tripped을 설정한다. tripped이면 true를 반환한다.
func (g *failureGuard) recordFailure(ctx context.Context, rec source.Record, cause error, outputName string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	g.consecutiveFailures++
	g.totalFailures++
	consecutive := g.consecutiveFailures
	total := g.totalFailures
	dlq := g.dlq
	cb := g.cb
	g.mu.Unlock()

	metrics.PipelineRecordsTotal.WithLabelValues("error").Inc()
	slog.Warn("pipeline record failed",
		"workflow_id", g.workflowID, "pipeline_id", g.pipelineID, "pipeline", g.pipeline,
		"output", outputName, "consecutive_failures", consecutive, "total_failures", total,
		"error", cause)

	// DLQ 적재 (실패 보존 — 서킷과 독립)
	if dlq != nil {
		if derr := dlq.Write(ctx, rec); derr != nil {
			slog.Error("failed to write to DLQ",
				"workflow_id", g.workflowID, "pipeline_id", g.pipelineID, "error", derr)
		}
	}

	// 서킷 브레이커 판정
	if cb != nil {
		trip := false
		reason := ""
		if cb.MaxConsecutiveFailures > 0 && consecutive >= cb.MaxConsecutiveFailures {
			trip = true
			reason = fmt.Sprintf("consecutive failures %d >= %d", consecutive, cb.MaxConsecutiveFailures)
		}
		if cb.MaxTotalFailures > 0 && total >= cb.MaxTotalFailures {
			trip = true
			reason = fmt.Sprintf("total failures %d >= %d", total, cb.MaxTotalFailures)
		}
		if trip {
			g.mu.Lock()
			if !g.tripped {
				g.tripped = true
				g.trippedReason = reason
				metrics.PipelineCircuitTrippedTotal.Inc()
				slog.Error("circuit breaker tripped — terminating pipeline to stop failure cascade",
					"workflow_id", g.workflowID, "pipeline_id", g.pipelineID, "pipeline", g.pipeline,
					"reason", reason)
			}
			g.mu.Unlock()
			return true
		}
	}
	return false
}

// isTripped은 서킷이 열렸는지 반환한다.
func (g *failureGuard) isTripped() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tripped
}

// trippedErr은 서킷 오픈 사유를 담은 에러를 반환한다.
func (g *failureGuard) trippedErr() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.tripped {
		return nil
	}
	return fmt.Errorf("circuit breaker open: %s", g.trippedReason)
}

// close는 DLQ 등 리소스를 정리한다.
func (g *failureGuard) close() {
	if g == nil || g.dlq == nil {
		return
	}
	_ = g.dlq.Flush(context.Background())
	_ = g.dlq.Close()
}

// finishBatchTripped은 서킷이 열렸을 때 배치 실행을 failed로 마감하고 결과를 반환한다.
func (e *GroupExecutor) finishBatchTripped(
	result *types.PipelineExecutionResult,
	statsCollector *StatsCollector,
	guard *failureGuard,
	saveCheckpoints func(),
) (*types.PipelineExecutionResult, error) {
	result.Status = "failed"
	result.ErrorMessage = guard.trippedErr().Error()
	stats := statsCollector.GetStatistics()
	result.RecordsRead = stats.RecordsCollected
	result.RecordsWritten = stats.RecordsProcessed
	result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
	result.Statistics = stats
	saveCheckpoints()
	return result, guard.trippedErr()
}
