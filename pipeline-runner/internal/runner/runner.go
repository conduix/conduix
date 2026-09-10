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

	"github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/pipeline-runner/internal/config"
	"github.com/conduix/conduix/pipeline-runner/internal/health"
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

	// stop 명령으로 무한 대기를 풀기 위한 취소 컨텍스트.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// REST /monitoring → 이 pod 의 실시간 진행 정보. agent 가 label 로 pod 를 찾아 pull 한다.
	r.healthServer.SetMonitoringHandler(r.monitoringHandler(groupExec))

	// 시간 버킷 통계 전송. realtime 은 종료 콜백이 영구히 발생하지 않으므로 이 경로가
	// 없으면 시간당 수집량·에러량이 어디에도 남지 않는다.
	go newStatsReporter(r.cfg.ControlPlaneURL, r.cfg.WorkflowID).run(ctx, groupExec)

	// REST /commands → GroupExecutor 제어 연결(stop/pause/resume). C1: pod 가 REST 로 명령 수신.
	r.healthServer.SetCommandHandler(func(cmd string) error {
		switch cmd {
		case "stop":
			slog.Info("stop command received", "workflow_id", r.cfg.WorkflowID)
			cancel() // 무한 대기 해제 → graceful shutdown 경로로
			return nil
		case "pause":
			slog.Info("pause command received", "workflow_id", r.cfg.WorkflowID)
			return groupExec.Pause()
		case "resume":
			slog.Info("resume command received", "workflow_id", r.cfg.WorkflowID)
			return groupExec.Resume()
		default:
			return fmt.Errorf("unknown command: %s", cmd)
		}
	})

	r.healthServer.SetStatus("running")

	startTime := time.Now()
	_, err := groupExec.Start(ctx, "streaming-runner")
	if err != nil {
		r.healthServer.SetStatus("error")
		return fmt.Errorf("failed to start streaming execution: %w", err)
	}

	// streaming 도 스스로 끝날 수 있다(DDL 방어의 schema_changed, 소스 오류 등).
	// 감시가 없으면 실행은 죽었는데 파드는 사는 좀비가 되고, control-plane 의 status 가
	// running 에 머물러 UI 에 성공도 실패도 안 보인다(실측).
	go r.watchStreamingCompletion(ctx, groupExec, startTime, cancel)

	slog.Info("streaming pipeline running, waiting for context cancellation", "workflow_id", r.cfg.WorkflowID)

	// 컨텍스트 종료 대기 (SIGTERM 또는 stop 명령)
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

	// REST /monitoring → batch Job 도 실행 중 진행률을 노출한다(streaming 과 동일 배선).
	// 없으면 agent 가 위임 실행의 진행 정보를 얻을 방법이 없어 라이브 모니터링이 빈다.
	r.healthServer.SetMonitoringHandler(r.monitoringHandler(groupExec))

	// batch 도 같은 배선으로 시간 버킷을 남긴다. 결과 콜백은 실행 총량만 담아
	// "몇 시에 얼마나 처리했나" 를 답하지 못한다.
	go newStatsReporter(r.cfg.ControlPlaneURL, r.cfg.WorkflowID).run(ctx, groupExec)

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

// monitoringHandler 는 GET /monitoring 응답을 만든다. GroupExecutor 의 진행 정보에
// AgentID 를 덧붙이는데, 이 값은 위임 생성한 agent 가 AGENT_ID 로 주입한 것이다.
// realtime(streaming) 은 무한 실행이라 종료 결과 콜백(sendBatchResult)이 영구히 발생하지
// 않아 agent_id 를 DB 에 남길 다른 경로가 없다. batch 도 실행 중에는 결과 콜백 전이므로
// 같은 배선을 쓴다.
func (r *Runner) monitoringHandler(groupExec *executor.GroupExecutor) func() any {
	return func() any {
		info := groupExec.GetMonitoringInfo()
		if info == nil {
			return nil
		}
		info.AgentID = r.cfg.AgentID
		return info
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
