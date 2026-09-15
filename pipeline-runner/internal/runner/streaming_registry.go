package runner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/shared/types"
)

// streaming pod 는 상주 인프라다 — 실행마다 뜨는 것이 아니라 하나가 계속 떠서
// 여러 realtime 실행을 고루틴으로 수용한다.
//
// 왜 바꾸는가: 예전에는 Deployment 이름이 conduix-rt-<executionID> 라 realtime 10개면
// 파드 10개가 떴다. 파드당 기본 요청이 500m CPU/512Mi 라 10개면 5 CPU/5Gi 를 점유하고,
// 32MB 바이너리도 파드마다 각각 내려받았다. realtime 은 소스를 따라가는 가벼운 작업이라
// 이 비용이 작업 자체보다 크다.
//
// 파드를 실행마다 분리했던 유일한 이유는 native stage 바이너리 주입이었다(실행 격리가
// 아니다 — docs/archive/REALTIME_STREAMING_POD.md). 바이너리는 RunnerVersion 단위이고
// 같은 시점의 모든 실행이 동일한 버전을 쓰므로, 파드 하나가 그 버전을 갖고 있으면 된다.
// 바이너리가 갱신되면 파드를 교체한다(그 안의 실행들은 재시작되어 새 버전으로 재개).

// streamingExecution 은 이 파드가 수용한 realtime 실행 하나다.
type streamingExecution struct {
	executionID string
	workflowID  string
	exec        *executor.GroupExecutor
	ctx         context.Context // 실행별 컨텍스트. 보조 고루틴이 이것을 따라 함께 끝난다.
	cancel      context.CancelFunc
	cpClient    *checkpoint.Client
	startedAt   time.Time

	// heartbeat 는 "고루틴이 살아 있다"는 신호다. 실행 루프가 주기적으로 갱신한다.
	//
	// 판정 기조: 체크포인트가 정확하므로 재시작 비용이 작다. 애매하면 살려두기보다
	// 죽이고 체크포인트에서 재개하는 쪽이 낫다 — 멈춘 채 살아 있는 실행은 아무도
	// 알아채지 못하고 데이터만 밀린다. 오탐으로 인한 재시작은 체크포인트가 흡수한다.
	heartbeat atomic.Int64 // UnixNano

	// done 은 실행 고루틴이 완전히 정리됐음을 알린다(누수 점검·테스트용).
	done chan struct{}
}

// touch 는 생존 신호를 갱신한다.
func (e *streamingExecution) touch() { e.heartbeat.Store(time.Now().UnixNano()) }

// lastBeat 는 마지막 생존 신호 시각이다.
func (e *streamingExecution) lastBeat() time.Time {
	ns := e.heartbeat.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// streamingRegistry 는 파드 안의 실행들을 관리한다.
//
// agent 의 runningExecs(map[executionID]*RunningExecution)와 같은 역할이다 —
// 그쪽은 이미 한 프로세스에서 여러 realtime 을 고루틴으로 돌리고 있었다(executeGroup).
// 여기서는 그 구조를 streaming pod 안으로 옮긴다.
type streamingRegistry struct {
	mu   sync.RWMutex
	byID map[string]*streamingExecution

	controlPlaneURL    string
	checkpointEndpoint string
}

func newStreamingRegistry(controlPlaneURL, checkpointEndpoint string) *streamingRegistry {
	return &streamingRegistry{
		byID:               make(map[string]*streamingExecution),
		controlPlaneURL:    controlPlaneURL,
		checkpointEndpoint: checkpointEndpoint,
	}
}

// ErrExecutionExists 는 같은 실행을 두 번 시작하려 할 때 반환한다.
// 명령 재전송(at-least-once)으로 중복 시작되면 같은 소스를 이중 소비한다.
var ErrExecutionExists = fmt.Errorf("execution already running in this pod")

// Start 는 실행을 파드에 추가하고 고루틴으로 돌린다.
//
// parent 는 파드 수명 컨텍스트다. 실행별 cancel 은 그 하위에 만들어, 실행 하나를 멈춰도
// 다른 실행과 파드는 살아남는다 — 예전 구조에서는 stop 이 프로세스 전체를 죽였다.
func (r *streamingRegistry) Start(parent context.Context, spec *types.WorkflowExecutionCommand) error {
	if spec == nil || spec.ExecutionID == "" {
		return fmt.Errorf("execution id is required")
	}
	if spec.WorkflowConfig == nil {
		return fmt.Errorf("workflow config is required")
	}

	r.mu.Lock()
	if _, dup := r.byID[spec.ExecutionID]; dup {
		r.mu.Unlock()
		return ErrExecutionExists
	}
	// 자리를 먼저 잡아 같은 실행의 동시 요청이 둘 다 통과하지 않게 한다.
	placeholder := &streamingExecution{executionID: spec.ExecutionID, workflowID: spec.WorkflowID}
	r.byID[spec.ExecutionID] = placeholder
	r.mu.Unlock()

	execCtx, cancel := context.WithCancel(parent)

	// checkpoint client 는 실행마다 만든다. 공유하면 캐시 키가 pipelineID:partitionKey 라
	// 서로 다른 워크플로우가 같은 pipelineID 를 쓸 때 충돌한다.
	var cpClient *checkpoint.Client
	cpEndpoint := r.checkpointEndpoint
	if cpEndpoint == "" {
		cpEndpoint = r.controlPlaneURL
	}
	if cpEndpoint != "" {
		cpClient = checkpoint.NewClient(cpEndpoint)
		cpClient.StartPeriodicFlush(execCtx, 30*time.Second)
	}

	var opts []executor.GroupExecutorOption
	if r.controlPlaneURL != "" {
		opts = append(opts, executor.WithLinkClient(link.NewClient(r.controlPlaneURL)))
	}
	if cpClient != nil {
		opts = append(opts, executor.WithCheckpointClient(cpClient))
	}
	if len(spec.AssignedPartitions) > 0 {
		opts = append(opts, executor.WithAssignedPartitions(spec.AssignedPartitions))
	}

	groupExec := executor.NewGroupExecutor(spec.WorkflowConfig, opts...)

	if _, err := groupExec.Start(execCtx, "streaming-runner"); err != nil {
		cancel()
		r.mu.Lock()
		delete(r.byID, spec.ExecutionID)
		r.mu.Unlock()
		return fmt.Errorf("start execution: %w", err)
	}

	r.mu.Lock()
	placeholder.exec = groupExec
	placeholder.ctx = execCtx
	placeholder.cancel = cancel
	placeholder.cpClient = cpClient
	placeholder.startedAt = time.Now()
	placeholder.done = make(chan struct{})
	r.mu.Unlock()
	placeholder.touch()

	// 생존 신호 루프. 실행별 ctx 를 따라 끝나므로 stop 하면 함께 사라진다
	// (파드 ctx 를 쓰면 실행이 멈춰도 고루틴이 남아 누수가 된다).
	//
	// panic 을 여기서 잡는다: 한 실행의 panic 이 파드 전체를 죽이면 무관한 realtime
	// 실행까지 끊긴다. 잡은 뒤에는 그 실행만 정리하고 파드는 유지한다.
	go func() {
		defer close(placeholder.done)
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in execution liveness loop; stopping that execution only",
					"execution_id", spec.ExecutionID, "panic", rec)
				_ = r.Stop(spec.ExecutionID)
			}
		}()

		ticker := time.NewTicker(livenessBeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-execCtx.Done():
				return
			case <-ticker.C:
				// GroupExecutor 가 응답하면 살아 있는 것이다. 처리량이 0이어도 상관없다 —
				// 느린 소스와 죽은 고루틴을 여기서 구분한다.
				if placeholder.exec != nil && placeholder.exec.Execution() != nil {
					placeholder.touch()
				}
			}
		}
	}()

	slog.Info("streaming execution started in shared pod",
		"execution_id", spec.ExecutionID, "workflow_id", spec.WorkflowID,
		"total_in_pod", r.Count())
	return nil
}

// livenessBeatInterval 은 실행이 생존 신호를 갱신하는 주기다.
const livenessBeatInterval = 15 * time.Second

// livenessTimeout 은 이 시간 넘게 신호가 없으면 죽은 것으로 본다.
//
// 짧게 잡는다: 체크포인트가 정확해 재시작 비용이 작고, 멈춘 채 살아 있는 실행은
// 아무도 모르게 데이터만 밀리게 한다. 오탐 재시작은 체크포인트가 흡수한다.
const livenessTimeout = 90 * time.Second

// Stop 은 실행 하나만 멈춘다. 파드와 다른 실행은 유지된다.
func (r *streamingRegistry) Stop(executionID string) error {
	r.mu.Lock()
	e, ok := r.byID[executionID]
	if ok {
		delete(r.byID, executionID)
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("execution %s not found in this pod", executionID)
	}

	if e.exec != nil {
		if err := e.exec.Stop(); err != nil {
			slog.Warn("error stopping execution", "execution_id", executionID, "error", err)
		}
	}
	if e.cancel != nil {
		e.cancel()
	}
	// 남은 체크포인트를 흘려보낸다. 안 하면 재시작 시 이미 처리한 구간을 다시 읽는다.
	if e.cpClient != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := e.cpClient.FlushCheckpoints(flushCtx); err != nil {
			slog.Error("final checkpoint flush failed", "execution_id", executionID, "error", err)
		}
		flushCancel()
	}

	slog.Info("streaming execution stopped", "execution_id", executionID, "remaining_in_pod", r.Count())
	return nil
}

// Pause/Resume 은 실행 단위로 적용한다.
func (r *streamingRegistry) Pause(executionID string) error {
	e := r.get(executionID)
	if e == nil || e.exec == nil {
		return fmt.Errorf("execution %s not found in this pod", executionID)
	}
	return e.exec.Pause()
}

func (r *streamingRegistry) Resume(executionID string) error {
	e := r.get(executionID)
	if e == nil || e.exec == nil {
		return fmt.Errorf("execution %s not found in this pod", executionID)
	}
	return e.exec.Resume()
}

// Monitoring 은 실행 하나의 모니터링 정보를 반환한다.
// executionID 가 비면 이 파드의 첫 실행을 반환한다(단일 실행 파드의 기존 호출 호환).
func (r *streamingRegistry) Monitoring(executionID string) *types.ExecutionMonitoringInfo {
	if executionID != "" {
		e := r.get(executionID)
		if e == nil || e.exec == nil {
			return nil
		}
		return e.exec.GetMonitoringInfo()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.byID {
		if e.exec != nil {
			return e.exec.GetMonitoringInfo()
		}
	}
	return nil
}

// MonitoringAll 은 파드가 수용한 모든 실행의 정보를 반환한다.
// "이 파드에서 몇 개가 돌고 어느 것이 바쁜가" 를 한 번에 보기 위한 경로다.
func (r *streamingRegistry) MonitoringAll() []*types.ExecutionMonitoringInfo {
	r.mu.RLock()
	execs := make([]*streamingExecution, 0, len(r.byID))
	for _, e := range r.byID {
		execs = append(execs, e)
	}
	r.mu.RUnlock()

	out := make([]*types.ExecutionMonitoringInfo, 0, len(execs))
	for _, e := range execs {
		if e.exec == nil {
			continue
		}
		if info := e.exec.GetMonitoringInfo(); info != nil {
			out = append(out, info)
		}
	}
	return out
}

// IDs 는 이 파드가 들고 있는 실행 목록이다(heartbeat·정리용).
func (r *streamingRegistry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	return out
}

func (r *streamingRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}

func (r *streamingRegistry) get(executionID string) *streamingExecution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[executionID]
}

// StopAll 은 파드 종료 시 모든 실행을 정리한다.
// 순차 처리한다 — 동시 flush 는 graceful period 안에 끝나지 않을 수 있다.
func (r *streamingRegistry) StopAll() {
	for _, id := range r.IDs() {
		if err := r.Stop(id); err != nil {
			slog.Warn("stop during shutdown failed", "execution_id", id, "error", err)
		}
	}
}
