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
	// registry 는 streaming 모드에서 이 파드가 수용한 실행들이다.
	// 상주 파드가 여러 realtime 실행을 고루틴으로 돌린다(batch 모드에서는 nil).
	registry *streamingRegistry
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
		if err := r.healthServer.Stop(shutdownCtx); err != nil {
			// 종료 실패를 삼키면 포트가 잡힌 채 남았는지 알 수 없다.
			slog.Warn("health server shutdown failed", "error", err)
		}
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

// runStreaming 스트리밍 모드 실행.
//
// 이 파드는 상주 인프라다 — 실행마다 뜨는 것이 아니라 하나가 계속 떠서 여러 realtime
// 실행을 고루틴으로 수용한다(streamingRegistry). 예전에는 Deployment 이름이
// conduix-rt-<executionID> 라 realtime 10개면 파드 10개가 떴고, 파드당 500m CPU/512Mi 와
// 32MB 바이너리 다운로드가 각각 들었다. realtime 은 소스를 따라가는 가벼운 작업이라
// 이 비용이 작업 자체보다 컸다.
//
// 기동 시 env 에 실행 정보가 있으면(구 경로 호환) 그것을 첫 실행으로 받아들이고,
// 이후 실행은 POST /executions 로 받는다.
func (r *Runner) runStreaming(ctx context.Context) error {
	slog.Info("starting streaming pod (resident, multi-execution)")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	reg := newStreamingRegistry(r.cfg.ControlPlaneURL, r.cfg.CheckpointEndpoint)
	r.registry = reg

	// REST /executions → 새 실행 배정. 상주 파드가 실행을 받아들이는 유일한 경로다.
	r.healthServer.SetAssignHandler(func(body []byte) error {
		var cmd types.WorkflowExecutionCommand
		if err := json.Unmarshal(body, &cmd); err != nil {
			return fmt.Errorf("decode execution command: %w", err)
		}
		if err := reg.Start(ctx, &cmd); err != nil {
			return err
		}
		// 실행별 통계 리포터. realtime 은 종료 콜백이 영구히 발생하지 않으므로
		// 이 경로가 없으면 시간당 수집량·에러량이 어디에도 남지 않는다.
		go r.runStatsReporter(ctx, reg, cmd.ExecutionID, cmd.WorkflowID)
		go r.watchExecutionCompletion(ctx, reg, cmd.ExecutionID, cmd.WorkflowID, time.Now())
		return nil
	})

	// REST /commands → 실행 단위 제어. executionID 가 비면 파드의 단일 실행에 적용한다
	// (구 agent 호환). 예전에는 stop 이 프로세스 전체를 죽여 다른 실행까지 끊겼다.
	r.healthServer.SetCommandHandler(func(executionID, cmd string) error {
		if executionID == "" {
			ids := reg.IDs()
			if len(ids) == 1 {
				executionID = ids[0]
			} else if len(ids) == 0 {
				return fmt.Errorf("no execution running in this pod")
			} else {
				return fmt.Errorf("execution_id required: %d executions in this pod", len(ids))
			}
		}
		switch cmd {
		case "stop":
			slog.Info("stop command received", "execution_id", executionID)
			return reg.Stop(executionID)
		case "pause":
			slog.Info("pause command received", "execution_id", executionID)
			return reg.Pause(executionID)
		case "resume":
			slog.Info("resume command received", "execution_id", executionID)
			return reg.Resume(executionID)
		default:
			return fmt.Errorf("unknown command: %s", cmd)
		}
	})

	// REST /monitoring → 실행별 진행 정보. agent 가 execution_id 로 지정해 pull 한다.
	r.healthServer.SetMonitoringHandler(func(executionID string) any {
		if executionID == "" {
			// 지정이 없으면 파드 전체를 보여준다 — "이 파드에서 몇 개가 도는가" 에 답한다.
			all := reg.MonitoringAll()
			if len(all) == 1 {
				return r.withAgentID(all[0])
			}
			if len(all) == 0 {
				return nil
			}
			for _, info := range all {
				r.withAgentID(info)
			}
			return all
		}
		return r.withAgentID(reg.Monitoring(executionID))
	})

	// 구 경로 호환: env 에 실행 정보가 실려 오면 첫 실행으로 받아들인다.
	// agent 가 /executions 로 배정하도록 바뀌면 이 경로는 비게 된다.
	if r.cfg.WorkflowID != "" && r.cfg.Workflow != nil {
		boot := &types.WorkflowExecutionCommand{
			ExecutionID:        r.cfg.ExecutionID,
			WorkflowID:         r.cfg.WorkflowID,
			WorkflowConfig:     r.cfg.Workflow,
			AssignedPartitions: r.cfg.AssignedPartitions,
		}
		if boot.ExecutionID == "" {
			boot.ExecutionID = r.cfg.WorkflowID // env 경로에 execution id 가 없을 때의 폴백
		}
		if err := reg.Start(ctx, boot); err != nil {
			r.healthServer.SetStatus("error")
			return fmt.Errorf("failed to start bootstrap execution: %w", err)
		}
		go r.runStatsReporter(ctx, reg, boot.ExecutionID, boot.WorkflowID)
		go r.watchExecutionCompletion(ctx, reg, boot.ExecutionID, boot.WorkflowID, time.Now())
	}

	// 감시·보고 루프: 멈춘 실행을 찾아 정리하고(체크포인트에서 재개된다),
	// 실행별 상태·통계를 control-plane 에 주기적으로 올린다.
	go r.superviseExecutions(ctx, reg)

	r.healthServer.SetStatus("running")
	slog.Info("streaming pod ready, waiting for executions", "bootstrapped", reg.Count())

	<-ctx.Done()

	slog.Info("shutting down streaming pod", "executions", reg.Count())
	r.healthServer.SetStatus("stopping")
	reg.StopAll()
	return nil
}

// withAgentID 는 모니터링 응답에 이 파드를 띄운 agent(노드)를 덧붙인다.
// realtime 은 종료 결과 콜백이 없어 agent_id 를 남길 다른 경로가 없다.
func (r *Runner) withAgentID(info *types.ExecutionMonitoringInfo) *types.ExecutionMonitoringInfo {
	if info == nil {
		return nil
	}
	if info.AgentID == "" {
		info.AgentID = r.cfg.AgentID
	}
	return info
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
// monitoringHandler batch 경로용. 실행이 하나뿐이라 executionID 는 무시한다
// (streaming 은 레지스트리 기반으로 별도 처리).
func (r *Runner) monitoringHandler(groupExec *executor.GroupExecutor) func(string) any {
	return func(string) any {
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
