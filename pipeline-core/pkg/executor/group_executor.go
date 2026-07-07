// Package executor 파이프라인 그룹 실행기
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/pipeline-core/pkg/output"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/pipeline-core/pkg/stream"
	"github.com/conduix/conduix/pipeline-core/pkg/validator"
	"github.com/conduix/conduix/shared/metrics"
	"github.com/conduix/conduix/shared/types"
)

// GroupExecutor 파이프라인 그룹 실행기
type GroupExecutor struct {
	group      *types.PipelineGroup
	mu         sync.RWMutex
	status     types.PipelineGroupStatus
	execution  *types.PipelineGroupExecution
	cancelFunc context.CancelFunc
	resultCh   chan *types.PipelineExecutionResult
	errorCh    chan error

	// 일시정지 게이트: paused가 true인 동안 처리 루프는 waitIfPaused에서 대기한다.
	// resumeCh는 Resume/Stop 시 close되어 대기 중인 루프를 깨운다(재개마다 새 채널로 교체).
	pauseMu  sync.Mutex
	paused   bool
	resumeCh chan struct{}

	// Checkpoint 관련
	checkpointClient *checkpoint.Client
	activeInputs     map[string]source.Source // pipelineID -> input source
	inputsMu         sync.RWMutex

	// 파티션 분산 실행: 이 executor 가 처리할 파티션 ID 집합(nil 이면 전체 — 현행).
	assignedPartitions map[string]bool

	// Monitoring 관련
	sampleBuffers   map[string]*SampleBuffer   // pipelineID -> SampleBuffer
	statsCollectors map[string]*StatsCollector // pipelineID -> StatsCollector
	monitoringMu    sync.RWMutex

	// Pipeline Link 관련
	linkClient     *link.Client
	pipelineLinks  []link.PipelineLink // 워크플로우의 모든 링크
	parentToChilds map[string][]link.PipelineLink
	childToParents map[string][]link.PipelineLink
	linkMu         sync.RWMutex

	// Stage 레지스트리 브리지: applyStage가 내장 구현하지 않는 타입(throttle, dedupe,
	// cast, encrypt, validate, route, native plugin 등)을 stream 레지스트리로 위임한다.
	// 파이프라인 실행당 stage 인스턴스를 1회 생성해 재사용하고, 실행 종료 시 Close한다.
	// key: "<pipelineID>\x00<stageName>\x00<stageType>"
	streamStages   map[string]stream.Stage
	streamStagesMu sync.Mutex
}

// GroupExecutorOption GroupExecutor 옵션
type GroupExecutorOption func(*GroupExecutor)

// WithCheckpointClient 체크포인트 클라이언트 설정
func WithCheckpointClient(client *checkpoint.Client) GroupExecutorOption {
	return func(e *GroupExecutor) {
		e.checkpointClient = client
	}
}

// WithLinkClient 링크 클라이언트 설정
func WithLinkClient(client *link.Client) GroupExecutorOption {
	return func(e *GroupExecutor) {
		e.linkClient = client
	}
}

// WithAssignedPartitions 파티션 분산 실행 시, 이 executor 가 처리할 파티션 ID 부분집합을 지정한다.
// 비면(nil/빈) 전체 파티션 실행(현행). partition-distributed-execution 의 sub-execution 용.
func WithAssignedPartitions(ids []string) GroupExecutorOption {
	return func(e *GroupExecutor) {
		if len(ids) == 0 {
			return
		}
		e.assignedPartitions = make(map[string]bool, len(ids))
		for _, id := range ids {
			e.assignedPartitions[id] = true
		}
	}
}

// NewGroupExecutor 그룹 실행기 생성
func NewGroupExecutor(group *types.PipelineGroup, opts ...GroupExecutorOption) *GroupExecutor {
	e := &GroupExecutor{
		group:           group,
		status:          types.PipelineGroupStatusIdle,
		resultCh:        make(chan *types.PipelineExecutionResult, len(group.Pipelines)),
		errorCh:         make(chan error, len(group.Pipelines)),
		activeInputs:    make(map[string]source.Source),
		sampleBuffers:   make(map[string]*SampleBuffer),
		statsCollectors: make(map[string]*StatsCollector),
		parentToChilds:  make(map[string][]link.PipelineLink),
		childToParents:  make(map[string][]link.PipelineLink),
		streamStages:    make(map[string]stream.Stage),
	}
	for _, opt := range opts {
		opt(e)
	}

	// 링크 클라이언트가 설정되어 있으면 링크 정보 로드
	if e.linkClient != nil {
		e.loadPipelineLinks()
	}

	return e
}

// loadPipelineLinks 워크플로우의 링크 정보 로드
func (e *GroupExecutor) loadPipelineLinks() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	links, err := e.linkClient.GetLinksByWorkflow(ctx, e.group.ID)
	if err != nil {
		slog.Error("failed to load pipeline links", "workflow_id", e.group.ID, "error", err)
		return
	}

	e.linkMu.Lock()
	defer e.linkMu.Unlock()

	e.pipelineLinks = links

	// 부모-자식 맵 구성
	for _, l := range links {
		e.parentToChilds[l.ParentPipelineID] = append(e.parentToChilds[l.ParentPipelineID], l)
		e.childToParents[l.ChildPipelineID] = append(e.childToParents[l.ChildPipelineID], l)
	}

	if len(links) > 0 {
		slog.Info("loaded pipeline links", "workflow_id", e.group.ID, "count", len(links))
	}
}

// Start 그룹 실행 시작
func (e *GroupExecutor) Start(ctx context.Context, triggeredBy string) (*types.PipelineGroupExecution, error) {
	e.mu.Lock()
	if e.status == types.PipelineGroupStatusRunning {
		e.mu.Unlock()
		return nil, fmt.Errorf("group is already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	e.cancelFunc = cancel
	e.status = types.PipelineGroupStatusRunning

	// 실행 기록 초기화
	e.execution = &types.PipelineGroupExecution{
		ID:              uuid.New().String(),
		WorkflowID:      e.group.ID,
		Status:          types.PipelineGroupStatusRunning,
		StartedAt:       time.Now(),
		PipelineResults: make([]types.PipelineExecutionResult, 0),
		TriggeredBy:     triggeredBy,
	}
	e.mu.Unlock()

	// 실행 모드에 따라 파이프라인 실행
	go func() {
		var err error
		switch e.group.ExecutionMode {
		case types.ExecutionModeParallel:
			err = e.runParallel(ctx)
		case types.ExecutionModeSequential:
			err = e.runSequential(ctx)
		case types.ExecutionModeDAG:
			err = e.runDAG(ctx)
		default:
			err = e.runParallel(ctx) // 기본값
		}

		e.mu.Lock()
		now := time.Now()
		e.execution.CompletedAt = &now
		duration := now.Sub(e.execution.StartedAt)
		durationPtr := &duration
		e.execution.Duration = durationPtr

		// 파이프라인 결과에서 에러 메시지 수집
		var errorMessages []string
		for _, pr := range e.execution.PipelineResults {
			if pr.ErrorMessage != "" {
				errorMessages = append(errorMessages, fmt.Sprintf("[%s] %s", pr.PipelineName, pr.ErrorMessage))
			}
		}

		if err != nil {
			e.status = types.PipelineGroupStatusError
			e.execution.Status = types.PipelineGroupStatusError
			if len(errorMessages) > 0 {
				e.execution.ErrorMessage = strings.Join(errorMessages, "; ")
			} else {
				e.execution.ErrorMessage = err.Error()
			}
		} else if len(errorMessages) > 0 {
			// 메인 에러는 없지만 파이프라인 에러가 있는 경우
			e.status = types.PipelineGroupStatusError
			e.execution.Status = types.PipelineGroupStatusError
			e.execution.ErrorMessage = strings.Join(errorMessages, "; ")
		} else {
			e.status = types.PipelineGroupStatusCompleted
			e.execution.Status = types.PipelineGroupStatusCompleted
		}
		e.mu.Unlock()
	}()

	return e.execution, nil
}

// runParallel 병렬 실행
func (e *GroupExecutor) runParallel(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(e.group.Pipelines))

	for _, pipeline := range e.group.Pipelines {
		wg.Add(1)
		go func(p types.GroupedPipeline) {
			defer wg.Done()
			result, err := e.runPipeline(ctx, p)
			if err != nil {
				errCh <- fmt.Errorf("pipeline %s failed: %w", p.Name, err)
			}
			if result != nil {
				e.mu.Lock()
				e.execution.PipelineResults = append(e.execution.PipelineResults, *result)
				e.execution.TotalRecords += result.RecordsWritten
				e.execution.FailedRecords += result.ErrorCount
				e.mu.Unlock()
			}
		}(pipeline)
	}

	wg.Wait()
	close(errCh)

	// 에러 수집
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// FailurePolicy에 따라 처리
		if e.group.FailurePolicy != nil && e.group.FailurePolicy.Action == types.FailureActionContinue {
			return nil // 에러 무시하고 계속
		}
		return fmt.Errorf("parallel execution errors: %v", errs)
	}

	return nil
}

// runSequential 순차 실행
func (e *GroupExecutor) runSequential(ctx context.Context) error {
	// 우선순위로 정렬
	pipelines := e.sortByPriority(e.group.Pipelines)

	for _, pipeline := range pipelines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := e.runPipelineWithRetry(ctx, pipeline)
		if result != nil {
			e.mu.Lock()
			e.execution.PipelineResults = append(e.execution.PipelineResults, *result)
			e.execution.TotalRecords += result.RecordsWritten
			e.execution.FailedRecords += result.ErrorCount
			e.mu.Unlock()
		}

		if err != nil {
			// FailurePolicy에 따라 처리 (retry는 runPipelineWithRetry에서 이미 소진됨)
			if e.group.FailurePolicy != nil {
				switch e.group.FailurePolicy.Action {
				case types.FailureActionContinue, types.FailureActionSkip, types.FailureActionRetry:
					continue
				default:
					return fmt.Errorf("pipeline %s failed: %w", pipeline.Name, err)
				}
			}
			return fmt.Errorf("pipeline %s failed: %w", pipeline.Name, err)
		}
	}

	return nil
}

// runPipelineWithRetry는 FailurePolicy.Action이 retry일 때 MaxRetries만큼 재시도한다.
// retry가 아니면 1회만 실행한다. RetryDelay(예: "5s","1m")를 재시도 간 대기로 쓴다.
// ctx가 취소되면 즉시 중단한다.
func (e *GroupExecutor) runPipelineWithRetry(ctx context.Context, pipeline types.GroupedPipeline) (*types.PipelineExecutionResult, error) {
	fp := e.group.FailurePolicy
	maxAttempts := 1
	var retryDelay time.Duration
	if fp != nil && fp.Action == types.FailureActionRetry {
		if fp.MaxRetries > 0 {
			maxAttempts = fp.MaxRetries + 1 // 최초 1회 + 재시도 N회
		}
		if fp.RetryDelay != "" {
			if d, perr := time.ParseDuration(fp.RetryDelay); perr == nil {
				retryDelay = d
			}
		}
	}

	var result *types.PipelineExecutionResult
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err = e.runPipeline(ctx, pipeline)
		if err == nil {
			return result, nil
		}
		if attempt < maxAttempts {
			metrics.PipelineRetriesTotal.Inc()
			// 지수 백오프 + jitter: 여러 파이프라인이 동시에 실패해도 재시도가 몰리지 않게(thundering herd 방지)
			delay := backoffWithJitter(retryDelay, attempt)
			slog.Warn("pipeline attempt failed, retrying",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "pipeline", pipeline.Name,
				"attempt", attempt, "max_attempts", maxAttempts, "backoff", delay, "error", err)
			if delay > 0 {
				select {
				case <-ctx.Done():
					return result, ctx.Err()
				case <-time.After(delay):
				}
			} else if ctx.Err() != nil {
				return result, ctx.Err()
			}
		}
	}
	return result, err
}

// backoffWithJitter는 base 딜레이에 지수 백오프(2^(attempt-1))와 ±20% jitter를 적용한다.
// base가 0이면 0을 반환(딜레이 없음). 최대 5분으로 상한을 둔다.
func backoffWithJitter(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	const maxBackoff = 5 * time.Minute
	// 2^(attempt-1) 배수 (attempt는 1부터). 오버플로우 방지 위해 시프트 상한.
	shift := attempt - 1
	if shift > 20 {
		shift = 20
	}
	d := base * time.Duration(1<<uint(shift))
	if d > maxBackoff {
		d = maxBackoff
	}
	// ±20% jitter
	jitter := time.Duration(rand.Int63n(int64(d)/5*2+1)) - d/5
	d += jitter
	if d < 0 {
		d = base
	}
	return d
}

// runDAG DAG 기반 의존성 실행
func (e *GroupExecutor) runDAG(ctx context.Context) error {
	// 파이프라인 ID -> 파이프라인 맵
	pipelineMap := make(map[string]types.GroupedPipeline)
	for _, p := range e.group.Pipelines {
		pipelineMap[p.ID] = p
	}

	// 의존성 그래프 구성
	dependencies := make(map[string][]string) // pipeline -> depends on
	dependents := make(map[string][]string)   // pipeline -> depended by
	for _, p := range e.group.Pipelines {
		dependencies[p.ID] = p.DependsOn
		for _, dep := range p.DependsOn {
			dependents[dep] = append(dependents[dep], p.ID)
		}
	}

	// 완료된 파이프라인 추적
	completed := make(map[string]bool)
	var completedMu sync.Mutex

	// 실행 가능한 파이프라인 찾기 (의존성 없거나 모든 의존성 완료)
	canRun := func(pID string) bool {
		deps := dependencies[pID]
		if len(deps) == 0 {
			return true
		}
		completedMu.Lock()
		defer completedMu.Unlock()
		for _, dep := range deps {
			if !completed[dep] {
				return false
			}
		}
		return true
	}

	// 반복 실행
	remaining := make(map[string]bool)
	for _, p := range e.group.Pipelines {
		remaining[p.ID] = true
	}

	for len(remaining) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 실행 가능한 파이프라인 찾기
		var toRun []string
		for pID := range remaining {
			if canRun(pID) {
				toRun = append(toRun, pID)
			}
		}

		if len(toRun) == 0 && len(remaining) > 0 {
			return fmt.Errorf("circular dependency detected")
		}

		// 병렬 실행
		var wg sync.WaitGroup
		errCh := make(chan error, len(toRun))

		for _, pID := range toRun {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				p := pipelineMap[id]
				result, err := e.runPipeline(ctx, p)
				if result != nil {
					e.mu.Lock()
					e.execution.PipelineResults = append(e.execution.PipelineResults, *result)
					e.execution.TotalRecords += result.RecordsWritten
					e.execution.FailedRecords += result.ErrorCount
					e.mu.Unlock()
				}
				if err != nil {
					errCh <- fmt.Errorf("pipeline %s failed: %w", p.Name, err)
				} else {
					completedMu.Lock()
					completed[id] = true
					completedMu.Unlock()
				}
			}(pID)
		}

		wg.Wait()
		close(errCh)

		// 에러 확인
		for err := range errCh {
			if e.group.FailurePolicy == nil || e.group.FailurePolicy.Action == types.FailureActionStopAll {
				return err
			}
		}

		// 완료된 파이프라인 제거
		for _, pID := range toRun {
			delete(remaining, pID)
		}
	}

	return nil
}

// isOutputType Output 타입인지 확인
func isOutputType(stageType string) bool {
	switch stageType {
	case "sql", "elasticsearch", "kafka", "mongodb", "s3", "rest_api", "file":
		return true
	default:
		return false
	}
}

// runPipeline 개별 파이프라인 실행
func (e *GroupExecutor) runPipeline(ctx context.Context, pipeline types.GroupedPipeline) (*types.PipelineExecutionResult, error) {
	// 통계 수집기 초기화
	statsCollector := NewStatsCollector(pipeline.ID, pipeline.Name)

	// 샘플 버퍼 초기화
	sampleBuffer := NewSampleBuffer(DefaultSampleSize)

	// 모니터링 정보 등록
	e.monitoringMu.Lock()
	e.statsCollectors[pipeline.ID] = statsCollector
	e.sampleBuffers[pipeline.ID] = sampleBuffer
	e.monitoringMu.Unlock()

	// 완료 시 정리
	defer func() {
		e.monitoringMu.Lock()
		delete(e.statsCollectors, pipeline.ID)
		delete(e.sampleBuffers, pipeline.ID)
		e.monitoringMu.Unlock()
	}()

	result := &types.PipelineExecutionResult{
		PipelineID:   pipeline.ID,
		PipelineName: pipeline.Name,
		Status:       "running",
		StartedAt:    time.Now(),
	}

	// Prometheus 메트릭: 활성 실행 게이지 + 종료 시 결과 기록.
	// result는 포인터로 in-place 갱신되므로 defer에서 최종 상태를 읽는다.
	metrics.ActiveExecutions.Inc()
	pipelineStart := time.Now()
	defer func() {
		metrics.ActiveExecutions.Dec()
		metrics.RecordExecution(result.Status, time.Since(pipelineStart).Seconds(),
			result.RecordsRead, result.RecordsWritten, result.ErrorCount)
	}()

	// 실패 가드: 실패 카운트/서킷 브레이커/DLQ. FailurePolicy에서 구성.
	guard, err := newFailureGuard(e.group.FailurePolicy, e.group.ID, pipeline.ID, pipeline.Name)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("failed to init failure guard: %v", err)
		return result, err
	}
	defer guard.close()

	// 에러 메시지 수집용 (최대 10개)
	var sinkErrors []string
	var sinkErrorsMu sync.Mutex
	maxSinkErrors := 10

	addSinkError := func(errMsg string) {
		sinkErrorsMu.Lock()
		defer sinkErrorsMu.Unlock()
		if len(sinkErrors) < maxSinkErrors {
			sinkErrors = append(sinkErrors, errMsg)
		}
	}

	// 링크 기반 동적 Kafka Sink 주입 (부모 파이프라인인 경우)
	injectedKafkaSinks := e.injectKafkaSinksForParent(ctx, &pipeline)

	// Output Sink 및 PreStages 매핑
	outputSinks := make(map[string]output.Output) // output name -> sink
	outputsWithSinks := make([]OutputWithSink, 0) // Output + Sink + PreStages

	// 1. Outputs 배열에서 Sink 생성 (권장 방식)
	for _, output := range pipeline.Outputs {
		slog.Info("creating output",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID,
			"output", output.Name, "type", output.Type, "pre_stages", len(output.PreStages))
		s, err := e.createSinkFromOutput(output)
		if err != nil {
			result.Status = "failed"
			result.ErrorMessage = fmt.Sprintf("failed to create output %s: %v", output.Name, err)
			return result, err
		}
		if err := s.Open(ctx); err != nil {
			result.Status = "failed"
			result.ErrorMessage = fmt.Sprintf("failed to open output %s: %v", output.Name, err)
			return result, err
		}
		outputSinks[output.Name] = s
		outputsWithSinks = append(outputsWithSinks, OutputWithSink{
			Output:    output,
			Sink:      s,
			PreStages: output.PreStages,
		})
		slog.Info("output opened",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", output.Name, "type", output.Type)
	}

	// 2. Stages 배열에서 Output 타입 찾기 (레거시 호환)
	for _, stage := range pipeline.Stages {
		if isOutputType(stage.Type) {
			slog.Info("creating output from stages (legacy)",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", stage.Name, "type", stage.Type)
			s, err := e.createSinkFromStage(stage)
			if err != nil {
				result.Status = "failed"
				result.ErrorMessage = fmt.Sprintf("failed to create output %s: %v", stage.Name, err)
				return result, err
			}
			if err := s.Open(ctx); err != nil {
				result.Status = "failed"
				result.ErrorMessage = fmt.Sprintf("failed to open output %s: %v", stage.Name, err)
				return result, err
			}
			outputSinks[stage.Name] = s
			// Stage 기반 Output은 PreStages 없음
			outputsWithSinks = append(outputsWithSinks, OutputWithSink{
				Output: types.Output{
					ID:     stage.ID,
					Name:   stage.Name,
					Type:   stage.Type,
					Config: stage.Config,
				},
				Sink:      s,
				PreStages: nil,
			})
			slog.Info("output opened (legacy)",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", stage.Name, "type", stage.Type)
		}
	}

	// 주입된 Kafka Sink도 outputSinks에 추가
	maps.Copy(outputSinks, injectedKafkaSinks)

	// 싱크 정리 defer
	defer func() {
		// 종료 flush 는 실행 ctx 가 아닌 별도 timeout context 로 한다.
		// stop/취소로 ctx 가 이미 canceled 면 flush 가 실패해 버퍼(batch_size 미만)
		// 잔여 레코드가 유실되기 때문(realtime 소량 유입 시 특히).
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for name, s := range outputSinks {
			if err := s.Flush(flushCtx); err != nil {
				slog.Error("output flush error",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", name, "error", err)
			}
			if err := s.Close(); err != nil {
				slog.Error("output close error",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", name, "error", err)
			}
		}
	}()

	// stream 레지스트리로 위임된 Stage 인스턴스 정리 (리소스 보유 stage: enrich/aggregate 등)
	defer e.closeStreamStages()

	// outputsWithSinks는 아래 배치/레코드 처리 루프에서 Output별 PreStages 적용에 사용된다.

	// 링크 기반 동적 Kafka Input 주입 (자식 파이프라인인 경우)
	inputConfig := e.injectKafkaInputForChild(&pipeline)

	// Input 소스 생성 및 실행
	slog.Info("creating input source",
		"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "source", inputConfig.Name, "type", inputConfig.Type)
	records, errs, src, err := e.createAndRunSource(ctx, pipeline.ID, inputConfig)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		statsCollector.RecordCollectionError()
		result.Statistics = statsCollector.GetStatistics()
		slog.Error("source creation failed",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "error", err)
		return result, err
	}
	slog.Info("source created successfully", "workflow_id", e.group.ID, "pipeline_id", pipeline.ID)

	// 활성 소스 추적
	if src != nil {
		e.inputsMu.Lock()
		e.activeInputs[pipeline.ID] = src
		e.inputsMu.Unlock()

		defer func() {
			e.inputsMu.Lock()
			delete(e.activeInputs, pipeline.ID)
			e.inputsMu.Unlock()
		}()
	}

	// 체크포인트 저장 헬퍼 함수
	saveCheckpoints := func() {
		if e.checkpointClient == nil || src == nil {
			return
		}
		if cpSource, ok := src.(source.CheckpointableSource); ok {
			sourceCheckpoints := cpSource.GetSourceCheckpoints()
			for _, scp := range sourceCheckpoints {
				cp := &checkpoint.Checkpoint{
					PipelineID:   pipeline.ID,
					PipelineName: pipeline.Name,
					SourceType:   cpSource.SourceType(),
					PartitionKey: scp.PartitionKey,
					OffsetValue:  scp.OffsetValue,
					OffsetType:   scp.OffsetType,
					RecordCount:  scp.RecordCount,
				}
				e.checkpointClient.UpdateCheckpoint(cp)
			}
			if err := e.checkpointClient.FlushCheckpoints(context.Background()); err != nil {
				slog.Warn("failed to flush checkpoints",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "error", err)
			} else if len(sourceCheckpoints) > 0 {
				slog.Info("saved checkpoints",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "count", len(sourceCheckpoints))
			}
		}
	}

	// at-least-once ack: 소스가 AckableSource 면, "sink flush 성공" 후에만 offset 을 커밋한다.
	// 채널 전송 시점이 아니라 실제 적재 시점 기준 → 크래시 시 미적재분 재처리(유실 없음).
	ackable, _ := src.(source.AckableSource)
	var pendingOffsets []source.RecordOffset // 아직 flush·ack 안 된 레코드 offset

	// flushAndAck: 모든 sink 를 flush 하고, 전부 성공하면 그동안 쌓인 offset 을 소스에 ack.
	// 하나라도 flush 실패면 ack 하지 않음(그 레코드들은 재처리 → 유실 없음).
	flushAndAck := func() {
		if ackable == nil || len(pendingOffsets) == 0 {
			return
		}
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		allOK := true
		for name, s := range outputSinks {
			if err := s.Flush(flushCtx); err != nil {
				allOK = false
				slog.Warn("flush before ack failed, will reprocess",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "output", name, "error", err)
			}
		}
		if allOK {
			ackable.Ack(pendingOffsets)
			pendingOffsets = pendingOffsets[:0]
			saveCheckpoints()
		}
	}
	// realtime 스트림에서 flush·ack 주기(레코드 수 기준). 너무 크면 미ack 구간(재처리 후보)이 커진다.
	const ackEveryN = 100

	// 입력 스키마 검증기 생성 (JSONSchema가 있는 경우)
	var sourceValidator *validator.SchemaValidator
	input := pipeline.GetInput()

	// DDL 방어: CDC 스키마 변경(_cdc_type=ddl) 감지 시 기본 정지(정합성 우선).
	// on_ddl=allow 면 무시하고 계속(JSON-only 등 스키마 무관 파이프라인 전용).
	onDDL, _ := input.Config["on_ddl"].(string)
	ddlStopEnabled := onDDL != "allow"

	if input.JSONSchema != "" {
		sourceValidator, err = validator.NewSchemaValidator(input.JSONSchema)
		if err != nil {
			slog.Warn("source schema validator creation failed",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "error", err)
			// 스키마 파싱 실패는 경고로만 처리, 검증 없이 계속 진행
			sourceValidator = nil
		} else {
			slog.Info("source schema validator created", "workflow_id", e.group.ID, "pipeline_id", pipeline.ID)
		}
	}

	// 배치 처리 모드: Stage 병렬 처리 + Output bulk/individual 선택
	if pipeline.Batch != nil && pipeline.Batch.Enabled {
		outputMode := pipeline.Batch.OutputMode
		if outputMode == "" {
			outputMode = types.OutputModeBulk // 기본값: bulk 모드
		}
		slog.Info("using batch mode",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "pipeline", pipeline.Name,
			"output_mode", outputMode, "size", pipeline.Batch.Size, "workers", pipeline.Batch.Workers)
		return e.runPipelineBatch(ctx, pipeline, records, errs, outputsWithSinks,
			statsCollector, sampleBuffer, sourceValidator, saveCheckpoints, guard)
	}

	// 기존 레코드 단위 처리 모드
	for {
		select {
		case <-ctx.Done():
			result.Status = "canceled"
			result.ErrorMessage = ctx.Err().Error()
			stats := statsCollector.GetStatistics()
			result.RecordsRead = stats.RecordsCollected
			result.RecordsWritten = stats.RecordsProcessed
			result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
			result.Statistics = stats
			flushAndAck()     // 취소 시에도 적재 성공분은 ack(WithoutCancel flush)
			saveCheckpoints() // 취소 시에도 체크포인트 저장
			return result, ctx.Err()

		case record, ok := <-records:
			if !ok {
				// 완료
				now := time.Now()
				result.CompletedAt = now
				result.Status = "completed"
				stats := statsCollector.GetStatistics()
				result.RecordsRead = stats.RecordsCollected
				result.RecordsWritten = stats.RecordsProcessed
				result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
				result.Statistics = stats

				// sink 에러가 있으면 error message에 추가
				sinkErrorsMu.Lock()
				if len(sinkErrors) > 0 {
					result.ErrorMessage = strings.Join(sinkErrors, "; ")
					if stats.ProcessingErrors > int64(len(sinkErrors)) {
						result.ErrorMessage += fmt.Sprintf(" (... and %d more errors)", stats.ProcessingErrors-int64(len(sinkErrors)))
					}
				}
				sinkErrorsMu.Unlock()

				slog.Info("pipeline completed",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "pipeline", pipeline.Name,
					"records_read", result.RecordsRead, "records_written", result.RecordsWritten,
					"errors", result.ErrorCount)
				flushAndAck()     // 남은 pending offset flush 후 ack(at-least-once)
				saveCheckpoints() // 완료 시 체크포인트 저장
				return result, nil
			}

			e.waitIfPaused(ctx) // 일시정지 중이면 재개/중지될 때까지 대기

			// 수집량 카운트
			statsCollector.RecordCollected()

			// DDL 방어: 스키마 변경 감지 시 파이프라인을 "설정 동작 불가" 사유로 정지한다.
			// 스키마 변경은 전후 데이터 정합성을 깨므로, 자동으로 계속 흘리지 않는다(사람이 판단).
			if ddlStopEnabled {
				if t, ok := record.Data["_cdc_type"].(string); ok && t == string(source.CDCEventDDL) {
					ddlSQL, _ := record.Data["ddl"].(string)
					flushAndAck()                    // DDL 이전까지 적재된 것은 ack(유실 방지)
					saveCheckpoints()                // DDL 이전 위치까지 커밋
					result.Status = "schema_changed" // 스키마 변경으로 설정 동작 불가 → 정지(재개 전 validation 필요)
					result.ErrorMessage = fmt.Sprintf("schema change (DDL) detected — pipeline stopped for validation: %s", ddlSQL)
					stats := statsCollector.GetStatistics()
					result.RecordsRead = stats.RecordsCollected
					result.RecordsWritten = stats.RecordsProcessed
					result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
					result.Statistics = stats
					slog.Warn("CDC schema change → pipeline stopped (validation required)",
						"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "ddl", ddlSQL)
					return result, nil
				}
			}

			// 소스 스키마 검증 (검증기가 있는 경우)
			if sourceValidator != nil {
				validationResult := sourceValidator.Validate(record.Data)
				if !validationResult.Valid {
					statsCollector.RecordProcessingError()
					slog.Warn("source schema validation failed",
						"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "errors", validationResult.Errors)
					continue // 스키마 검증 실패 시 레코드 건너뛰기
				}
			}

			// 1. 공통 Stage 적용 (Output 타입 제외)
			data := record.Data
			var filtered bool
			for _, stage := range pipeline.Stages {
				// Output 타입은 공통 Stage에서 제외 (레거시 호환: Outputs 배열로 이동)
				if isOutputType(stage.Type) {
					continue
				}

				statsCollector.RecordTransformInput(stage.Name, stage.Type)

				// 일반 stage: 데이터 변환
				transformed, err := e.applyStage(data, stage)
				if err != nil {
					statsCollector.RecordTransformError(stage.Name)
					filtered = true
					break
				}

				// Stage에서 nil 반환 = 필터링됨
				if transformed == nil {
					filtered = true
					break
				}

				statsCollector.RecordTransformOutput(stage.Name)
				sampleBuffer.AddSample(stage.Name, transformed) // 샘플 저장
				data = transformed
			}

			// 필터링된 레코드는 처리 완료
			if filtered {
				continue
			}

			// 2. 각 Output에 대해 PreStages 적용 후 전송
			recordSinkOK := true // 이 레코드가 모든 Output 에 성공 전달됐는지(ack 판단)
			for _, ows := range outputsWithSinks {
				outputData := data // 각 Output은 공통 Stage 결과를 복사하여 시작

				// PreStages 적용
				preFiltered := false
				for _, preStage := range ows.PreStages {
					statsCollector.RecordTransformInput(fmt.Sprintf("%s.%s", ows.Output.Name, preStage.Name), preStage.Type)

					transformed, err := e.applyStage(outputData, preStage)
					if err != nil {
						statsCollector.RecordTransformError(fmt.Sprintf("%s.%s", ows.Output.Name, preStage.Name))
						preFiltered = true
						break
					}

					if transformed == nil {
						preFiltered = true
						break
					}

					statsCollector.RecordTransformOutput(fmt.Sprintf("%s.%s", ows.Output.Name, preStage.Name))
					sampleBuffer.AddSample(fmt.Sprintf("%s.%s", ows.Output.Name, preStage.Name), transformed)
					outputData = transformed
				}

				// PreStages에서 필터링된 경우 이 Output 건너뛰기
				if preFiltered {
					continue
				}

				// Output으로 전송
				if err := e.sendToSink(ctx, outputData, ows.Sink); err != nil {
					recordSinkOK = false
					statsCollector.RecordProcessingError()
					errMsg := fmt.Sprintf("[%s] %v", ows.Output.Name, err)
					addSinkError(errMsg)
					// 실패 가드: 카운트/로그/DLQ + 서킷 브레이커
					if guard.recordFailure(ctx, source.Record{Data: outputData}, err, ows.Output.Name) {
						// 서킷 오픈 → 부하 확산 방지 위해 실행 에러 종료
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
				} else {
					statsCollector.RecordProcessed()
					guard.recordSuccess()
				}
				sampleBuffer.AddSample(ows.Output.Name, outputData)
			}

			// 모든 Output 에 성공 전달된 레코드만 ack 대상(offset 은 소스가 부여).
			// flush 성공 후 실제 커밋되므로, 여기선 pending 에만 쌓고 주기적으로 flushAndAck.
			if ackable != nil && recordSinkOK && record.Metadata.PartitionKey != "" {
				pendingOffsets = append(pendingOffsets, source.RecordOffset{
					PartitionKey: record.Metadata.PartitionKey,
					Offset:       record.Metadata.Offset,
				})
				if len(pendingOffsets) >= ackEveryN {
					flushAndAck()
				}
			}

		case err := <-errs:
			if err != nil {
				statsCollector.RecordCollectionError()
				result.ErrorMessage = err.Error()
				slog.Error("source error",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "error", err)
			}
		}
	}
}

// createAndRunSource 소스 생성 및 실행
func (e *GroupExecutor) createAndRunSource(ctx context.Context, pipelineID string, gs types.GroupedSource) (<-chan source.Record, <-chan error, source.Source, error) {
	// 파티션이 있는 경우 멀티 소스 처리
	if len(gs.Partitions) > 0 {
		records, errs, err := e.runMultiPartitionSource(ctx, gs)
		return records, errs, nil, err // 멀티 파티션은 checkpointable source 추적 안함
	}

	// 단일 소스
	src, err := e.createSource(gs)
	if err != nil {
		return nil, nil, nil, err
	}

	// 체크포인트 로드 (CheckpointableSource인 경우)
	if e.checkpointClient != nil {
		if cpSource, ok := src.(source.CheckpointableSource); ok {
			slog.Info("loading checkpoints",
				"workflow_id", e.group.ID, "pipeline_id", pipelineID, "source_type", cpSource.SourceType())

			dbCheckpoints, err := e.checkpointClient.LoadCheckpoints(ctx, pipelineID)
			if err != nil {
				slog.Warn("failed to load checkpoints",
					"workflow_id", e.group.ID, "pipeline_id", pipelineID, "error", err)
			} else if len(dbCheckpoints) > 0 {
				// DB 체크포인트를 소스 체크포인트로 변환
				sourceCheckpoints := make([]*source.SourceCheckpoint, 0, len(dbCheckpoints))
				for _, cp := range dbCheckpoints {
					sourceCheckpoints = append(sourceCheckpoints, &source.SourceCheckpoint{
						PartitionKey: cp.PartitionKey,
						OffsetValue:  cp.OffsetValue,
						OffsetType:   cp.OffsetType,
						RecordCount:  cp.RecordCount,
					})
				}
				if err := cpSource.SetSourceCheckpoints(sourceCheckpoints); err != nil {
					slog.Warn("failed to set checkpoints",
						"workflow_id", e.group.ID, "pipeline_id", pipelineID, "error", err)
				} else {
					slog.Info("restored checkpoints",
						"workflow_id", e.group.ID, "pipeline_id", pipelineID, "count", len(sourceCheckpoints))
				}
			}
		}
	}

	if err := src.Open(ctx); err != nil {
		return nil, nil, nil, err
	}

	records, errs := src.Read(ctx)
	return records, errs, src, nil
}

// runMultiPartitionSource 멀티 파티션 소스 실행
func (e *GroupExecutor) runMultiPartitionSource(ctx context.Context, gs types.GroupedSource) (<-chan source.Record, <-chan error, error) {
	records := make(chan source.Record, 1000)
	errs := make(chan error, len(gs.Partitions))

	var wg sync.WaitGroup
	assignedCount := 0
	for _, partition := range gs.Partitions {
		if !partition.Enabled {
			continue
		}
		// 파티션 분산 실행: 이 executor 에 배정된 파티션만 실행(나머지는 다른 sub-execution 이 담당).
		// assignedPartitions 가 nil 이면 전체 실행(현행 단일 실행).
		if e.assignedPartitions != nil && !e.assignedPartitions[partition.ID] {
			continue
		}
		assignedCount++

		wg.Add(1)
		go func(p types.PartitionConfig) {
			defer wg.Done()

			// 파티션별 설정 병합
			config := make(map[string]any)
			maps.Copy(config, gs.Config)
			maps.Copy(config, p.Config)

			partitionSource := types.GroupedSource{
				Type:   gs.Type,
				Name:   gs.Name + "-" + p.ID,
				Config: config,
			}

			src, err := e.createSource(partitionSource)
			if err != nil {
				errs <- err
				return
			}

			if err := src.Open(ctx); err != nil {
				errs <- err
				return
			}

			srcRecords, srcErrs := src.Read(ctx)

			// 레코드 전달
			for {
				select {
				case <-ctx.Done():
					return
				case record, ok := <-srcRecords:
					if !ok {
						return
					}
					select {
					case records <- record:
					case <-ctx.Done():
						return
					}
				case err := <-srcErrs:
					if err != nil {
						errs <- err
					}
				}
			}
		}(partition)
	}

	if e.assignedPartitions != nil {
		slog.Info("running assigned partition subset",
			"workflow_id", e.group.ID, "assigned", assignedCount, "total", len(gs.Partitions))
	}

	// 모든 파티션 완료 시 채널 닫기
	go func() {
		wg.Wait()
		close(records)
		close(errs)
	}()

	return records, errs, nil
}

// createSource 소스 생성
func (e *GroupExecutor) createSource(gs types.GroupedSource) (source.Source, error) {
	configJSON, _ := json.Marshal(gs.Config)

	// rate_limit을 config에 주입 (WorkflowSource.RateLimit -> config["rate_limit"])
	configWithRateLimit := gs.Config
	if gs.RateLimit != nil && gs.RateLimit.Enabled {
		configWithRateLimit = make(map[string]any)
		maps.Copy(configWithRateLimit, gs.Config)
		configWithRateLimit["rate_limit"] = map[string]any{
			"enabled":  gs.RateLimit.Enabled,
			"rate":     gs.RateLimit.Rate,
			"interval": gs.RateLimit.Interval,
			"burst":    gs.RateLimit.Burst,
			"strategy": gs.RateLimit.Strategy,
		}
	}

	// config를 SourceV2 형식으로 변환
	// 실제 구현에서는 config 패키지의 SourceV2를 사용
	switch gs.Type {
	case "kafka":
		// Kafka 소스 설정
		return createKafkaSourceFromConfig(configWithRateLimit)
	case "rest_api", "http":
		return createHTTPSourceFromConfig(configWithRateLimit)
	case "sql":
		return createSQLSourceFromConfig(configWithRateLimit)
	case "sql_event":
		return createSQLEventSourceFromConfig(configWithRateLimit)
	case "cdc":
		return createCDCSourceFromConfig(configWithRateLimit)
	case "file":
		return createFileSourceFromConfig(configWithRateLimit)
	case "kubernetes", "k8s_logs":
		return createKubernetesSourceFromConfig(configWithRateLimit)
	default:
		return nil, fmt.Errorf("unsupported source type: %s (config: %s)", gs.Type, string(configJSON))
	}
}

// applyStage Stage 적용
func (e *GroupExecutor) applyStage(data map[string]any, stage types.Stage) (map[string]any, error) {
	switch stage.Type {
	case "schema_validate":
		// JSON Schema 검증 Stage
		schemaJSON, ok := stage.Config["json_schema"].(string)
		if !ok || schemaJSON == "" {
			// 스키마 없으면 통과
			return data, nil
		}

		schemaValidator, err := validator.NewSchemaValidator(schemaJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to create schema validator: %w", err)
		}

		result := schemaValidator.Validate(data)
		if !result.Valid {
			// 검증 실패 시 레코드 필터링 (nil 반환)
			onError, _ := stage.Config["on_error"].(string)
			if onError == "pass" {
				// on_error=pass이면 검증 실패해도 통과
				return data, nil
			}
			// 기본: 검증 실패 시 레코드 필터링
			return nil, nil
		}
		return data, nil

	case "filter":
		// 간단한 필터 로직
		if condition, ok := stage.Config["condition"].(string); ok {
			if !e.evaluateCondition(data, condition) {
				return nil, nil // 조건 미충족 시 필터링
			}
		}
		return data, nil

	case "remap":
		// 필드 매핑/변환
		// 형식 1: mapping - {"target_field": ".source_field"} (VRL 스타일)
		if mapping, ok := stage.Config["mapping"].(map[string]any); ok {
			result := make(map[string]any)
			// 기존 데이터 복사
			maps.Copy(result, data)
			// 매핑 적용
			for newField, expr := range mapping {
				if exprStr, ok := expr.(string); ok {
					if after, ok0 := strings.CutPrefix(exprStr, "."); ok0 {
						// 필드 참조
						srcField := after
						if val, ok := data[srcField]; ok {
							result[newField] = val
						}
					} else {
						// 리터럴 값
						result[newField] = exprStr
					}
				}
			}
			return result, nil
		}
		// 형식 2: mappings - {"source_field": "target_field"} (간단한 리네임)
		if mappings, ok := stage.Config["mappings"].(map[string]any); ok {
			result := make(map[string]any)
			// 매핑되지 않은 필드는 그대로 bypass
			for k, v := range data {
				if _, mapped := mappings[k]; !mapped {
					result[k] = v
				}
			}
			// 매핑된 필드는 새 이름으로 변환
			for srcField, targetField := range mappings {
				if targetFieldStr, ok := targetField.(string); ok {
					if val, exists := data[srcField]; exists {
						result[targetFieldStr] = val
					}
				}
			}
			return result, nil
		}
		return data, nil

	case "select":
		// 필드 선택
		if fields, ok := stage.Config["fields"].([]any); ok {
			result := make(map[string]any)
			for _, f := range fields {
				if field, ok := f.(string); ok {
					if val, ok := data[field]; ok {
						result[field] = val
					}
				}
			}
			return result, nil
		}
		return data, nil

	case "exclude":
		// 필드 제외
		if fields, ok := stage.Config["fields"].([]any); ok {
			result := make(map[string]any)
			excludeSet := make(map[string]bool)
			for _, f := range fields {
				if field, ok := f.(string); ok {
					excludeSet[field] = true
				}
			}
			for k, v := range data {
				if !excludeSet[k] {
					result[k] = v
				}
			}
			return result, nil
		}
		return data, nil

	default:
		// 내장 구현이 없는 타입(throttle, dedupe, cast, encrypt, validate, route, timestamp,
		// aggregate, enrich, native plugin 등)은 stream 레지스트리로 위임한다.
		return e.applyStreamStage(stage, data)
	}
}

// applyStreamStage는 stream 레지스트리의 Stage로 데이터를 변환한다.
// 등록되지 않은(=아직 미구현) 타입은 안전하게 통과시켜 하위호환을 유지한다.
// stage 인스턴스는 실행당 1회 생성되어 재사용되며, Close는 closeStreamStages에서 처리한다.
func (e *GroupExecutor) applyStreamStage(stage types.Stage, data map[string]any) (map[string]any, error) {
	s, err := e.getOrCreateStreamStage(stage)
	if err != nil {
		// 알 수 없는 타입: 통과(레거시 default 동작 보존)
		return data, nil
	}

	result, err := s.Process(context.Background(), &stream.Record{Data: data})
	if err != nil {
		return nil, err
	}
	if result == nil {
		// 필터링됨
		return nil, nil
	}
	return result.Data, nil
}

// getOrCreateStreamStage는 stage에 대한 stream.Stage 인스턴스를 캐시에서 얻거나 생성한다.
// 동시 호출(배치 워커) 안전. 생성 실패(미등록 타입)는 에러로 반환한다.
func (e *GroupExecutor) getOrCreateStreamStage(stage types.Stage) (stream.Stage, error) {
	key := stage.Type + "\x00" + stage.Name

	e.streamStagesMu.Lock()
	defer e.streamStagesMu.Unlock()

	if s, ok := e.streamStages[key]; ok {
		return s, nil
	}

	s, err := stream.NewStage(stream.StageConfig{
		Name:   stage.Name,
		Type:   stage.Type,
		Config: stage.Config,
	})
	if err != nil {
		return nil, err
	}
	e.streamStages[key] = s
	return s, nil
}

// closeStreamStages는 실행 중 생성된 모든 stream.Stage를 닫는다(리소스 보유 stage 정리).
func (e *GroupExecutor) closeStreamStages() {
	e.streamStagesMu.Lock()
	defer e.streamStagesMu.Unlock()
	for key, s := range e.streamStages {
		if err := s.Close(); err != nil {
			slog.Error("stream stage close error", "workflow_id", e.group.ID, "stage", key, "error", err)
		}
		delete(e.streamStages, key)
	}
}

// evaluateCondition 간단한 조건 평가
func (e *GroupExecutor) evaluateCondition(data map[string]any, condition string) bool {
	condition = strings.TrimSpace(condition)

	// .field == 'value'
	if strings.Contains(condition, "==") {
		parts := strings.SplitN(condition, "==", 2)
		field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

		if val, ok := data[field]; ok {
			return fmt.Sprintf("%v", val) == value
		}
		return false
	}

	// .field != 'value'
	if strings.Contains(condition, "!=") {
		parts := strings.SplitN(condition, "!=", 2)
		field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

		if val, ok := data[field]; ok {
			return fmt.Sprintf("%v", val) != value
		}
		return true
	}

	// .field exists
	if before, ok := strings.CutSuffix(condition, " exists"); ok {
		field := before
		field = strings.TrimPrefix(strings.TrimSpace(field), ".")
		_, ok := data[field]
		return ok
	}

	return true // 기본: 통과
}

// sendToSink 싱크로 전송
func (e *GroupExecutor) sendToSink(ctx context.Context, data map[string]any, s output.Output) error {
	record := source.Record{
		Data: data,
		Metadata: source.Metadata{
			Source:    s.Name(),
			Timestamp: time.Now().UnixNano(),
		},
	}
	return s.Write(ctx, record)
}

// createSinkFromStage Stage에서 Sink 생성
func (e *GroupExecutor) createSinkFromStage(stage types.Stage) (output.Output, error) {
	cfg := config.OutputConfig{
		Type: stage.Type,
	}

	// Config에서 필드 매핑
	if stage.Config != nil {
		// connection_string이 있으면 파싱하여 driver/dsn 설정
		if connStr, ok := stage.Config["connection_string"].(string); ok && connStr != "" {
			connStr = os.ExpandEnv(connStr)
			driver, dsn, err := parseConnectionString(connStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse connection_string: %w", err)
			}
			cfg.Driver = driver
			cfg.DSN = dsn
		}

		// 명시적 driver/dsn이 있으면 덮어쓰기 (우선순위 높음)
		if driver, ok := stage.Config["driver"].(string); ok {
			cfg.Driver = driver
		}
		if dsn, ok := stage.Config["dsn"].(string); ok {
			// 환경변수 확장 (${VAR} 형식)
			cfg.DSN = os.ExpandEnv(dsn)
		}
		if table, ok := stage.Config["table"].(string); ok {
			cfg.Table = table
		}
		if batchSize, ok := stage.Config["batch_size"].(float64); ok {
			cfg.BatchSize = int(batchSize)
		}
		if onConflict, ok := stage.Config["on_conflict"].(string); ok {
			cfg.OnConflict = onConflict
		}
		// upsert가 true이면 on_conflict를 "update"로 설정
		if upsert, ok := stage.Config["upsert"].(bool); ok && upsert {
			cfg.OnConflict = "update"
		}
		// conflict_columns 처리
		if conflictCols, ok := stage.Config["conflict_columns"].([]any); ok {
			for _, col := range conflictCols {
				if colStr, ok := col.(string); ok {
					cfg.ConflictColumns = append(cfg.ConflictColumns, colStr)
				}
			}
		}
		if columnMap, ok := stage.Config["column_map"].(map[string]any); ok {
			cfg.ColumnMap = make(map[string]string)
			for k, v := range columnMap {
				if str, ok := v.(string); ok {
					cfg.ColumnMap[k] = str
				}
			}
		}
		// create_table SQL 처리
		if createTable, ok := stage.Config["create_table"].(string); ok {
			cfg.CreateTable = createTable
		}

		// REST API Sink 필드 매핑
		if url, ok := stage.Config["url"].(string); ok {
			cfg.URL = os.ExpandEnv(url)
		}
		if method, ok := stage.Config["method"].(string); ok {
			cfg.Method = method
		}
		if headers, ok := stage.Config["headers"].(map[string]any); ok {
			cfg.Headers = make(map[string]string)
			for k, v := range headers {
				if str, ok := v.(string); ok {
					cfg.Headers[k] = str
				}
			}
		}
		if timeout, ok := stage.Config["timeout"].(string); ok {
			cfg.Timeout = timeout
		}
		if retryCount, ok := stage.Config["retry_count"].(float64); ok {
			cfg.RetryCount = int(retryCount)
		}
		if retryDelay, ok := stage.Config["retry_delay"].(string); ok {
			cfg.RetryDelay = retryDelay
		}
		if successCodes, ok := stage.Config["success_codes"].([]any); ok {
			for _, code := range successCodes {
				if codeFloat, ok := code.(float64); ok {
					cfg.SuccessCodes = append(cfg.SuccessCodes, int(codeFloat))
				}
			}
		}

		// 배치 설정
		if batchEnabled, ok := stage.Config["batch_enabled"].(bool); ok {
			cfg.BatchEnabled = batchEnabled
		}
		if batchSizeHTTP, ok := stage.Config["batch_size_http"].(float64); ok {
			cfg.BatchSizeHTTP = int(batchSizeHTTP)
		}
		if batchDelimiter, ok := stage.Config["batch_delimiter"].(string); ok {
			cfg.BatchDelimiter = batchDelimiter
		}
		// flush_interval은 BatchingWrapper에서 사용
		var flushInterval time.Duration
		if flushIntervalStr, ok := stage.Config["flush_interval"].(string); ok {
			if parsed, err := time.ParseDuration(flushIntervalStr); err == nil {
				flushInterval = parsed
			}
		}

		// Sink 생성
		s, err := output.NewOutput(cfg)
		if err != nil {
			return nil, err
		}

		// 배치가 활성화된 경우 BatchingWrapper로 감싸기
		if cfg.BatchEnabled {
			batchConfig := output.BatchConfig{
				Enabled:       true,
				Size:          cfg.BatchSizeHTTP,
				FlushInterval: flushInterval,
				Format:        "ndjson",
			}
			if cfg.BatchDelimiter != "\n" && cfg.BatchDelimiter != "" {
				batchConfig.Format = "array"
			}
			if batchConfig.Size <= 0 {
				batchConfig.Size = 100
			}
			if batchConfig.FlushInterval <= 0 {
				batchConfig.FlushInterval = 5 * time.Second
			}
			return output.WrapWithBatching(s, batchConfig), nil
		}

		return s, nil
	}

	return output.NewOutput(cfg)
}

// createSinkFromOutput Output에서 Sink 생성
func (e *GroupExecutor) createSinkFromOutput(output types.Output) (output.Output, error) {
	// Output을 Stage로 변환하여 기존 로직 재사용
	stage := types.Stage{
		ID:     output.ID,
		Name:   output.Name,
		Type:   output.Type,
		Config: output.Config,
	}
	return e.createSinkFromStage(stage)
}

// OutputWithSink Output과 해당 Sink, PreStages를 묶는 구조체
type OutputWithSink struct {
	Output    types.Output
	Sink      output.Output
	PreStages []types.Stage
}

// Stop 그룹 실행 중지
func (e *GroupExecutor) Stop() error {
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
	e.status = types.PipelineGroupStatusStopped
	e.mu.Unlock()

	// 일시정지 대기 중인 루프를 깨워 취소를 관측하게 한다(paused 상태로 멈춘 실행도 중지되도록).
	e.clearPause()
	return nil
}

// Pause 그룹 실행 일시정지. 처리 루프는 다음 waitIfPaused 지점에서 블록된다.
func (e *GroupExecutor) Pause() error {
	e.mu.Lock()
	e.status = types.PipelineGroupStatusPaused
	e.mu.Unlock()

	e.pauseMu.Lock()
	if !e.paused {
		e.paused = true
		e.resumeCh = make(chan struct{})
	}
	e.pauseMu.Unlock()
	return nil
}

// Resume 그룹 실행 재개
func (e *GroupExecutor) Resume() error {
	e.mu.Lock()
	if e.status != types.PipelineGroupStatusPaused {
		e.mu.Unlock()
		return fmt.Errorf("group is not paused")
	}
	e.status = types.PipelineGroupStatusRunning
	e.mu.Unlock()

	e.clearPause()
	return nil
}

// clearPause는 일시정지를 해제하고 대기 중인 루프를 깨운다(멱등).
func (e *GroupExecutor) clearPause() {
	e.pauseMu.Lock()
	if e.paused {
		e.paused = false
		if e.resumeCh != nil {
			close(e.resumeCh)
			e.resumeCh = nil
		}
	}
	e.pauseMu.Unlock()
}

// waitIfPaused는 일시정지 상태인 동안 블록한다.
// Resume/Stop이 호출되거나 ctx가 취소되면 반환한다(취소 시 처리 루프가 종료를 관측하도록).
func (e *GroupExecutor) waitIfPaused(ctx context.Context) {
	e.pauseMu.Lock()
	if !e.paused {
		e.pauseMu.Unlock()
		return
	}
	ch := e.resumeCh
	e.pauseMu.Unlock()

	select {
	case <-ch:
	case <-ctx.Done():
	}
}

// Status 현재 상태 반환
func (e *GroupExecutor) Status() types.PipelineGroupStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

// Execution 현재 실행 정보 반환
func (e *GroupExecutor) Execution() *types.PipelineGroupExecution {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.execution
}

// sortByPriority 우선순위로 정렬
func (e *GroupExecutor) sortByPriority(pipelines []types.GroupedPipeline) []types.GroupedPipeline {
	sorted := make([]types.GroupedPipeline, len(pipelines))
	copy(sorted, pipelines)
	// 간단한 버블 정렬 (실제로는 sort.Slice 사용)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j].Priority > sorted[j+1].Priority {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}
	return sorted
}

// 소스 생성 헬퍼 함수들
func createKafkaSourceFromConfig(config map[string]any) (source.Source, error) {
	// config를 pkg/config.SourceV2로 변환
	cfg := configToInputV2(config)
	cfg.Type = "kafka"
	return source.NewKafkaSource(cfg)
}

func createHTTPSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "http"
	return source.NewHTTPSource(cfg)
}

func createSQLSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "sql"
	return source.NewSQLSource(cfg)
}

func createSQLEventSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "sql_event"
	return source.NewSQLEventSource(cfg)
}

func createCDCSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "cdc"
	return source.NewCDCSource(cfg)
}

func createFileSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "file"
	return source.NewFileSource(cfg)
}

func createKubernetesSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToInputV2(config)
	cfg.Type = "kubernetes"
	return source.NewKubernetesSource(cfg)
}

// GetMonitoringInfo 실행 중인 파이프라인의 모니터링 정보 조회
func (e *GroupExecutor) GetMonitoringInfo() *types.ExecutionMonitoringInfo {
	e.mu.RLock()
	execution := e.execution
	status := e.status
	e.mu.RUnlock()

	if execution == nil {
		return nil
	}

	result := &types.ExecutionMonitoringInfo{
		ExecutionID: execution.ID,
		WorkflowID:  execution.WorkflowID,
		Status:      string(status),
		Pipelines:   make([]types.PipelineMonitoringInfo, 0),
		UpdatedAt:   time.Now(),
	}

	// 각 파이프라인의 모니터링 정보 수집
	e.monitoringMu.RLock()
	defer e.monitoringMu.RUnlock()

	for pipelineID, statsCollector := range e.statsCollectors {
		pipelineInfo := types.PipelineMonitoringInfo{
			PipelineID:   pipelineID,
			PipelineName: statsCollector.pipelineName,
			Status:       "running",
			Checkpoints:  make([]types.CheckpointInfo, 0),
			Stages:       make([]types.StageMonitorInfo, 0),
			UpdatedAt:    time.Now(),
		}

		// 체크포인트 정보 수집
		e.inputsMu.RLock()
		if src, ok := e.activeInputs[pipelineID]; ok {
			if cpSource, ok := src.(source.CheckpointableSource); ok {
				checkpoints := cpSource.GetSourceCheckpoints()
				for _, cp := range checkpoints {
					pipelineInfo.Checkpoints = append(pipelineInfo.Checkpoints, types.CheckpointInfo{
						PartitionKey: cp.PartitionKey,
						OffsetValue:  cp.OffsetValue,
						OffsetType:   cp.OffsetType,
						RecordCount:  cp.RecordCount,
					})
				}
			}
		}
		e.inputsMu.RUnlock()

		// Stage 통계 수집
		stageStats := statsCollector.GetStageStats()
		sampleBuffer := e.sampleBuffers[pipelineID]
		allSamples := make(map[string][]types.DataSample)
		if sampleBuffer != nil {
			allSamples = sampleBuffer.GetAllSamples()
		}

		for _, stats := range stageStats {
			stageInfo := types.StageMonitorInfo{
				Name:        stats.Name,
				Type:        stats.Type,
				InputCount:  stats.InputCount,
				OutputCount: stats.OutputCount,
				ErrorCount:  stats.ErrorCount,
				Samples:     allSamples[stats.Name],
			}
			pipelineInfo.Stages = append(pipelineInfo.Stages, stageInfo)
		}

		// 통계 정보 추가
		stats := statsCollector.GetStatistics()
		pipelineInfo.Statistics = &types.MonitoringStats{
			RecordsCollected: stats.RecordsCollected,
			RecordsProcessed: stats.RecordsProcessed,
			CollectionErrors: stats.CollectionErrors,
			ProcessingErrors: stats.ProcessingErrors,
		}

		result.Pipelines = append(result.Pipelines, pipelineInfo)
	}

	return result
}

// injectKafkaSinksForParent 부모 파이프라인에 Kafka Sink 주입
// 자식이 있는 경우에만 Kafka Sink를 자동으로 추가
func (e *GroupExecutor) injectKafkaSinksForParent(ctx context.Context, pipeline *types.GroupedPipeline) map[string]output.Output {
	injectedSinks := make(map[string]output.Output)

	// 링크가 없으면 주입하지 않음
	if e.linkClient == nil || len(e.pipelineLinks) == 0 {
		return injectedSinks
	}

	// 이 파이프라인이 부모인지 확인
	e.linkMu.RLock()
	childLinks, hasChildren := e.parentToChilds[pipeline.ID]
	e.linkMu.RUnlock()

	if !hasChildren || len(childLinks) == 0 {
		return injectedSinks
	}

	// 자식이 있으면 각 링크에 대해 Kafka Sink 생성
	slog.Info("injecting Kafka sinks for parent pipeline",
		"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "children", len(childLinks))

	for _, l := range childLinks {
		// Kafka brokers 환경변수에서 동적으로 가져오기
		brokers := link.GetKafkaBrokers()

		// Kafka Sink 설정 생성
		cfg := config.OutputConfig{
			Type:    "kafka",
			Brokers: brokers,
			Topic:   l.KafkaTopic,
		}

		// Kafka Sink 생성
		kafkaSink, err := output.NewOutput(cfg)
		if err != nil {
			slog.Error("failed to create Kafka sink for link",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "link_id", l.ID, "error", err)
			continue
		}

		// Sink 열기
		if err := kafkaSink.Open(ctx); err != nil {
			slog.Error("failed to open Kafka sink for link",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "link_id", l.ID, "error", err)
			continue
		}

		// 주입된 Sink 등록
		sinkName := fmt.Sprintf("kafka_link_%s", l.ChildPipelineID[:8])
		injectedSinks[sinkName] = kafkaSink

		slog.Info("injected Kafka sink",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "sink", sinkName, "topic", l.KafkaTopic)
	}

	return injectedSinks
}

// injectKafkaInputForChild 자식 파이프라인에 Kafka Input 주입
// 부모가 있는 경우 원래 Input을 Kafka Input으로 교체
func (e *GroupExecutor) injectKafkaInputForChild(pipeline *types.GroupedPipeline) types.WorkflowInput {
	// 링크가 없으면 원래 Input 반환
	if e.linkClient == nil || len(e.pipelineLinks) == 0 {
		return pipeline.GetInput()
	}

	// 이 파이프라인이 자식인지 확인
	e.linkMu.RLock()
	parentLinks, hasParents := e.childToParents[pipeline.ID]
	e.linkMu.RUnlock()

	if !hasParents || len(parentLinks) == 0 {
		// 부모가 없으면 원래 Input 반환
		return pipeline.GetInput()
	}

	// 첫 번째 부모 링크 사용 (여러 부모가 있을 경우 첫 번째 사용)
	l := parentLinks[0]

	slog.Info("replacing input with Kafka input for child pipeline",
		"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "topic", l.KafkaTopic)

	// Kafka brokers 환경변수에서 동적으로 가져오기
	brokers := link.GetKafkaBrokers()

	// Kafka Input 설정 생성
	kafkaInputConfig := types.WorkflowInput{
		Type: "kafka",
		Name: fmt.Sprintf("kafka_link_input_%s", l.ParentPipelineID[:8]),
		Config: map[string]any{
			"brokers":        brokers,
			"topics":         []string{l.KafkaTopic},
			"consumer_group": fmt.Sprintf("conduix_%s_%s", e.group.ID, pipeline.ID),
			"start_offset":   "newest", // 또는 "oldest"
		},
	}

	return kafkaInputConfig
}

// runPipelineBatch 배치 처리 모드로 파이프라인 실행
// Stage는 항상 병렬 처리, Output은 output_mode에 따라 bulk 또는 individual 처리
// 구조: 소스 → [N개 수집] → [병렬 Stage] → [결과 모음] → [Output bulk/individual]
// applyPreStages는 한 Output의 PreStages를 배치 레코드에 적용한다.
// PreStage가 에러를 내거나 nil을 반환하면 해당 레코드는 이 Output 대상에서 제외된다.
// PreStages가 없으면 입력을 그대로 반환한다(공통 Stage 결과 공유이므로 방어적으로 복사).
func (e *GroupExecutor) applyPreStages(
	ows OutputWithSink,
	transformed []map[string]any,
	statsCollector *StatsCollector,
	sampleBuffer *SampleBuffer,
) []map[string]any {
	if len(ows.PreStages) == 0 {
		return transformed
	}

	out := make([]map[string]any, 0, len(transformed))
	for _, data := range transformed {
		outputData := data
		dropped := false
		for _, preStage := range ows.PreStages {
			label := fmt.Sprintf("%s.%s", ows.Output.Name, preStage.Name)
			statsCollector.RecordTransformInput(label, preStage.Type)

			result, err := e.applyStage(outputData, preStage)
			if err != nil {
				statsCollector.RecordTransformError(label)
				dropped = true
				break
			}
			if result == nil {
				dropped = true
				break
			}

			statsCollector.RecordTransformOutput(label)
			sampleBuffer.AddSample(label, result)
			outputData = result
		}
		if !dropped {
			out = append(out, outputData)
		}
	}
	return out
}

func (e *GroupExecutor) runPipelineBatch(
	ctx context.Context,
	pipeline types.GroupedPipeline,
	records <-chan source.Record,
	errs <-chan error,
	outputsWithSinks []OutputWithSink,
	statsCollector *StatsCollector,
	sampleBuffer *SampleBuffer,
	sourceValidator *validator.SchemaValidator,
	saveCheckpoints func(),
	guard *failureGuard,
) (*types.PipelineExecutionResult, error) {
	result := &types.PipelineExecutionResult{
		PipelineID:   pipeline.ID,
		PipelineName: pipeline.Name,
		Status:       "running",
		StartedAt:    time.Now(),
	}

	// 배치 설정
	batchSize := 100
	workers := 100
	flushInterval := 5 * time.Second
	outputMode := types.OutputModeBulk // 기본값: bulk

	if pipeline.Batch != nil {
		if pipeline.Batch.Size > 0 {
			batchSize = pipeline.Batch.Size
		}
		if pipeline.Batch.Workers > 0 {
			workers = pipeline.Batch.Workers
		} else {
			workers = batchSize // Workers 미설정 시 Size와 동일
		}
		if pipeline.Batch.FlushInterval != "" {
			if parsed, err := time.ParseDuration(pipeline.Batch.FlushInterval); err == nil {
				flushInterval = parsed
			}
		}
		if pipeline.Batch.OutputMode != "" {
			outputMode = pipeline.Batch.OutputMode
		}
	}

	// 워커 수 제한 (최대 100)
	if workers > 100 {
		workers = 100
	}

	slog.Info("starting batch pipeline",
		"workflow_id", e.group.ID, "pipeline_id", pipeline.ID,
		"size", batchSize, "workers", workers, "output_mode", outputMode, "flush", flushInterval)

	// 에러 메시지 수집용
	var sinkErrors []string
	var sinkErrorsMu sync.Mutex
	maxSinkErrors := 10

	addSinkError := func(errMsg string) {
		sinkErrorsMu.Lock()
		defer sinkErrorsMu.Unlock()
		if len(sinkErrors) < maxSinkErrors {
			sinkErrors = append(sinkErrors, errMsg)
		}
	}

	// 배치 처리 함수 - Stage 병렬 처리 후 Sink에 전송
	processBatch := func(batch []source.Record) {
		if len(batch) == 0 {
			return
		}

		slog.Info("processing batch",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "records", len(batch), "output_mode", outputMode)

		// 병렬 Stage 처리를 위한 채널
		type stageResult struct {
			idx  int
			data map[string]any
			err  error
		}
		resultCh := make(chan stageResult, len(batch))

		// 워커 풀로 Stage 병렬 처리
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, workers) // 동시 실행 제한

		for i, record := range batch {
			wg.Add(1)
			go func(idx int, rec source.Record) {
				defer wg.Done()
				semaphore <- struct{}{}        // 슬롯 확보
				defer func() { <-semaphore }() // 슬롯 반환

				// 소스 스키마 검증
				if sourceValidator != nil {
					validationResult := sourceValidator.Validate(rec.Data)
					if !validationResult.Valid {
						statsCollector.RecordProcessingError()
						resultCh <- stageResult{idx: idx, data: nil, err: fmt.Errorf("validation failed")}
						return
					}
				}

				// Stage 적용 (Sink 제외)
				data := rec.Data
				for _, stage := range pipeline.Stages {
					if isOutputType(stage.Type) {
						continue // Output는 나중에 처리
					}

					statsCollector.RecordTransformInput(stage.Name, stage.Type)
					transformedData, err := e.applyStage(data, stage)
					if err != nil {
						statsCollector.RecordTransformError(stage.Name)
						resultCh <- stageResult{idx: idx, data: nil, err: err}
						return
					}
					if transformedData == nil {
						// 필터링됨
						resultCh <- stageResult{idx: idx, data: nil, err: nil}
						return
					}
					statsCollector.RecordTransformOutput(stage.Name)
					sampleBuffer.AddSample(stage.Name, transformedData)
					data = transformedData
				}

				resultCh <- stageResult{idx: idx, data: data, err: nil}
			}(i, record)
		}

		// 결과 수집 고루틴
		go func() {
			wg.Wait()
			close(resultCh)
		}()

		// Transform 결과 수집 (필터링되지 않은 것만)
		transformed := make([]map[string]any, 0, len(batch))
		for res := range resultCh {
			if res.err == nil && res.data != nil {
				transformed = append(transformed, res.data)
			}
		}

		if len(transformed) == 0 {
			slog.Info("all records filtered out",
				"workflow_id", e.group.ID, "pipeline_id", pipeline.ID)
			return
		}

		slog.Info("records passed transform, sending to outputs",
			"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "records", len(transformed), "output_mode", outputMode)

		// 각 Output에 대해 Output별 PreStages 적용 후 전송
		for _, ows := range outputsWithSinks {
			stageName := ows.Output.Name
			s := ows.Sink

			// Output별 PreStages를 배치에 적용 (nil 반환 시 해당 레코드는 이 Output에서 제외)
			outputData := e.applyPreStages(ows, transformed, statsCollector, sampleBuffer)
			if len(outputData) == 0 {
				continue
			}

			if outputMode == types.OutputModeBulk {
				// Bulk 모드: BatchSink 인터페이스 사용
				if batchSink, ok := s.(output.BatchOutput); ok && batchSink.SupportsBatch() {
					batchRecords := make([]source.Record, len(outputData))
					for i, data := range outputData {
						batchRecords[i] = source.Record{Data: data}
					}
					if err := batchSink.WriteBatch(ctx, batchRecords); err != nil {
						statsCollector.RecordProcessingError()
						addSinkError(fmt.Sprintf("[%s] batch write error: %v", stageName, err))
						// 배치 쓰기 실패: 배치 전체를 실패 이벤트로 기록(DLQ에는 배치 레코드 적재)
						for _, br := range batchRecords {
							guard.recordFailure(ctx, br, err, stageName)
						}
					} else {
						for range outputData {
							statsCollector.RecordProcessed()
						}
						guard.recordSuccess()
					}
				} else {
					// Bulk 미지원 Sink는 개별 처리로 fallback
					for _, data := range outputData {
						if err := e.sendToSink(ctx, data, s); err != nil {
							statsCollector.RecordProcessingError()
							addSinkError(fmt.Sprintf("[%s] %v", stageName, err))
							guard.recordFailure(ctx, source.Record{Data: data}, err, stageName)
						} else {
							statsCollector.RecordProcessed()
							guard.recordSuccess()
						}
					}
				}
			} else {
				// Individual 모드: 1건씩 개별 전송
				for _, data := range outputData {
					if err := e.sendToSink(ctx, data, s); err != nil {
						statsCollector.RecordProcessingError()
						addSinkError(fmt.Sprintf("[%s] %v", stageName, err))
					} else {
						statsCollector.RecordProcessed()
					}
				}
			}
			sampleBuffer.AddSample(stageName, nil)
		}
	}

	// 배치 버퍼
	batch := make([]source.Record, 0, batchSize)
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	// 메인 처리 루프
	for {
		select {
		case <-ctx.Done():
			processBatch(batch) // 남은 배치 처리

			result.Status = "canceled"
			result.ErrorMessage = ctx.Err().Error()
			stats := statsCollector.GetStatistics()
			result.RecordsRead = stats.RecordsCollected
			result.RecordsWritten = stats.RecordsProcessed
			result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
			result.Statistics = stats
			saveCheckpoints()
			return result, ctx.Err()

		case <-flushTicker.C:
			// 시간 기반 flush
			if len(batch) > 0 {
				processBatch(batch)
				batch = batch[:0]
			}
			if guard.isTripped() {
				return e.finishBatchTripped(result, statsCollector, guard, saveCheckpoints)
			}

		case record, ok := <-records:
			if !ok {
				// 소스 종료 - 남은 배치 처리
				processBatch(batch)

				now := time.Now()
				result.CompletedAt = now
				result.Status = "completed"
				stats := statsCollector.GetStatistics()
				result.RecordsRead = stats.RecordsCollected
				result.RecordsWritten = stats.RecordsProcessed
				result.ErrorCount = stats.CollectionErrors + stats.ProcessingErrors
				result.Statistics = stats

				sinkErrorsMu.Lock()
				if len(sinkErrors) > 0 {
					result.ErrorMessage = strings.Join(sinkErrors, "; ")
				}
				sinkErrorsMu.Unlock()

				slog.Info("pipeline completed (batch)",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "pipeline", pipeline.Name,
					"records_read", result.RecordsRead, "records_written", result.RecordsWritten,
					"errors", result.ErrorCount)
				saveCheckpoints()
				return result, nil
			}

			e.waitIfPaused(ctx) // 일시정지 중이면 재개/중지될 때까지 대기

			statsCollector.RecordCollected()
			batch = append(batch, record)

			// 배치가 가득 차면 처리
			if len(batch) >= batchSize {
				processBatch(batch)
				batch = batch[:0]
			}
			if guard.isTripped() {
				return e.finishBatchTripped(result, statsCollector, guard, saveCheckpoints)
			}

		case err := <-errs:
			if err != nil {
				statsCollector.RecordCollectionError()
				result.ErrorMessage = err.Error()
				slog.Error("source error",
					"workflow_id", e.group.ID, "pipeline_id", pipeline.ID, "error", err)
			}
		}
	}
}
