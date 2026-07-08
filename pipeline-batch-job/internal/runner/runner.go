// Package runner Pipeline Runner 코어
// batch/streaming 실행 모드 통합 지원
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/conduix/conduix/pipeline-batch-job/internal/config"
	"github.com/conduix/conduix/pipeline-batch-job/internal/health"
	"github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/shared/types"
)

// Runner Pipeline Runner
type Runner struct {
	cfg          *config.RunnerConfig
	healthServer *health.Server
	httpClient   *http.Client
}

// New Runner 생성
func New(cfg *config.RunnerConfig) *Runner {
	return &Runner{
		cfg:          cfg,
		healthServer: health.NewServer(cfg.HealthPort, string(cfg.Mode)),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Run 파이프라인 실행 (메인 루프)
func (r *Runner) Run(ctx context.Context) error {
	// 헬스체크 서버 시작
	if err := r.healthServer.Start(); err != nil {
		return fmt.Errorf("failed to start health server: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.healthServer.Stop(shutdownCtx)
	}()

	switch r.cfg.Mode {
	case config.ModeBatch:
		return r.runBatch(ctx)
	case config.ModeStreaming:
		return r.runStreaming(ctx)
	default:
		return fmt.Errorf("unknown execution mode: %s", r.cfg.Mode)
	}
}

// runBatch 배치 모드 실행 (1회 실행 후 종료)
func (r *Runner) runBatch(ctx context.Context) error {
	startTime := time.Now()
	podName := os.Getenv("HOSTNAME")

	slog.Info("starting batch execution", "workflow_id", r.cfg.WorkflowID, "execution_id", r.cfg.ExecutionID)

	r.healthServer.SetStatus("running")

	// 타임아웃 적용
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	// GroupExecutor 생성 및 실행
	result, err := r.executeWorkflow(ctx)
	if err != nil {
		r.healthServer.SetStatus("error")
		return r.sendBatchResult(startTime, podName, nil, err)
	}

	r.healthServer.SetStatus("completed")
	return r.sendBatchResult(startTime, podName, result, nil)
}

// runStreaming 스트리밍 모드 실행 (지속 실행)
func (r *Runner) runStreaming(ctx context.Context) error {
	slog.Info("starting streaming execution", "workflow_id", r.cfg.WorkflowID)

	// 체크포인트 클라이언트 생성
	var cpClient *checkpoint.Client
	cpEndpoint := r.cfg.CheckpointEndpoint
	if cpEndpoint == "" {
		cpEndpoint = r.cfg.ControlPlaneURL
	}
	if cpEndpoint != "" {
		cpClient = checkpoint.NewClient(cpEndpoint)
		cpClient.StartPeriodicFlush(ctx, 30*time.Second)
	}

	// GroupExecutor 생성
	var opts []executor.GroupExecutorOption
	if r.cfg.ControlPlaneURL != "" {
		opts = append(opts, executor.WithLinkClient(link.NewClient(r.cfg.ControlPlaneURL)))
	}
	if cpClient != nil {
		opts = append(opts, executor.WithCheckpointClient(cpClient))
	}
	// 파티션 분산: batch sub-execution 이면 배정된 파티션만 실행(비면 전체 — 현행).
	if len(r.cfg.AssignedPartitions) > 0 {
		opts = append(opts, executor.WithAssignedPartitions(r.cfg.AssignedPartitions))
	}

	groupExec := executor.NewGroupExecutor(r.cfg.Workflow, opts...)

	r.healthServer.SetStatus("running")

	_, err := groupExec.Start(ctx, "streaming-runner")
	if err != nil {
		r.healthServer.SetStatus("error")
		return fmt.Errorf("failed to start streaming execution: %w", err)
	}

	slog.Info("streaming pipeline running, waiting for context cancellation", "workflow_id", r.cfg.WorkflowID)

	// 컨텍스트 종료 대기
	<-ctx.Done()

	slog.Info("shutting down streaming pipeline", "workflow_id", r.cfg.WorkflowID)
	r.healthServer.SetStatus("stopping")

	if err := groupExec.Stop(); err != nil {
		slog.Error("error stopping execution", "workflow_id", r.cfg.WorkflowID, "error", err)
	}

	// 최종 체크포인트 flush
	if cpClient != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer flushCancel()
		if err := cpClient.FlushCheckpoints(flushCtx); err != nil {
			slog.Error("final checkpoint flush error", "workflow_id", r.cfg.WorkflowID, "error", err)
		}
	}

	return nil
}

// executeWorkflow 워크플로우 실행 및 완료 대기
func (r *Runner) executeWorkflow(ctx context.Context) (*types.PipelineGroupExecution, error) {
	var opts []executor.GroupExecutorOption
	if r.cfg.ControlPlaneURL != "" {
		opts = append(opts, executor.WithLinkClient(link.NewClient(r.cfg.ControlPlaneURL)))
	}
	// 파티션 분산: batch sub-execution 이면 배정된 파티션만 실행(비면 전체 — 현행).
	// 누락 시 각 sub 가 전체 파티션을 실행해 분산이 무효화되고 데이터가 중복 적재된다(runStreaming 과 동일 배선).
	if len(r.cfg.AssignedPartitions) > 0 {
		opts = append(opts, executor.WithAssignedPartitions(r.cfg.AssignedPartitions))
	}

	groupExec := executor.NewGroupExecutor(r.cfg.Workflow, opts...)

	_, err := groupExec.Start(ctx, "batch-runner")
	if err != nil {
		return nil, fmt.Errorf("failed to start execution: %w", err)
	}

	// 완료 대기 (폴링)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = groupExec.Stop()
			return nil, fmt.Errorf("execution timed out")
		case <-ticker.C:
			exec := groupExec.Execution()
			if exec == nil {
				continue
			}
			switch exec.Status {
			case types.PipelineGroupStatusCompleted:
				return exec, nil
			case types.PipelineGroupStatusError, types.PipelineGroupStatusStopped:
				return exec, fmt.Errorf("execution failed: %s", exec.ErrorMessage)
			}
		}
	}
}

// sendBatchResult 배치 실행 결과를 Control Plane으로 전송
func (r *Runner) sendBatchResult(startTime time.Time, podName string, exec *types.PipelineGroupExecution, execErr error) error {
	completedAt := time.Now()

	result := &types.JobExecutionResult{
		ExecutionID: r.cfg.ExecutionID,
		WorkflowID:  r.cfg.WorkflowID,
		AgentID:     r.cfg.AgentID, // 위임 agent(노드) — 분산 현황 모니터링용
		JobName:     fmt.Sprintf("runner-%s", r.cfg.ExecutionID[:min(8, len(r.cfg.ExecutionID))]),
		PodName:     podName,
		StartedAt:   startTime,
		CompletedAt: completedAt,
		DurationMs:  completedAt.Sub(startTime).Milliseconds(),
	}

	if execErr != nil {
		result.Status = types.JobStatusFailed
		result.ErrorMessage = execErr.Error()
	} else if exec != nil {
		result.Status = types.JobStatusCompleted
		result.PipelineResults = exec.PipelineResults
		result.TotalRecords = exec.TotalRecords
		result.FailedRecords = exec.FailedRecords
	}

	slog.Info("sending result", "workflow_id", r.cfg.WorkflowID, "execution_id", r.cfg.ExecutionID, "status", result.Status, "records", result.TotalRecords)

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, r.cfg.CallbackURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send result: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("callback returned %d: %s", resp.StatusCode, string(body))
	}

	return execErr
}
