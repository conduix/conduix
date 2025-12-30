// Package executor 파이프라인 그룹 실행기
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/shared/types"
)

// GroupExecutor 파이프라인 그룹 실행기
type GroupExecutor struct {
	group           *types.PipelineGroup
	pipelineRunners map[string]*PipelineRunner
	mu              sync.RWMutex
	status          types.PipelineGroupStatus
	execution       *types.PipelineGroupExecution
	cancelFunc      context.CancelFunc
	resultCh        chan *types.PipelineExecutionResult
	errorCh         chan error
}

// NewGroupExecutor 그룹 실행기 생성
func NewGroupExecutor(group *types.PipelineGroup) *GroupExecutor {
	return &GroupExecutor{
		group:           group,
		pipelineRunners: make(map[string]*PipelineRunner),
		status:          types.PipelineGroupStatusIdle,
		resultCh:        make(chan *types.PipelineExecutionResult, len(group.Pipelines)),
		errorCh:         make(chan error, len(group.Pipelines)),
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

		if err != nil {
			e.status = types.PipelineGroupStatusError
			e.execution.Status = types.PipelineGroupStatusError
			e.execution.ErrorMessage = err.Error()
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

		result, err := e.runPipeline(ctx, pipeline)
		if result != nil {
			e.mu.Lock()
			e.execution.PipelineResults = append(e.execution.PipelineResults, *result)
			e.execution.TotalRecords += result.RecordsWritten
			e.execution.FailedRecords += result.ErrorCount
			e.mu.Unlock()
		}

		if err != nil {
			// FailurePolicy에 따라 처리
			if e.group.FailurePolicy != nil {
				switch e.group.FailurePolicy.Action {
				case types.FailureActionContinue:
					continue
				case types.FailureActionSkip:
					continue
				case types.FailureActionRetry:
					// TODO: 재시도 로직
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

// runPipeline 개별 파이프라인 실행
func (e *GroupExecutor) runPipeline(ctx context.Context, pipeline types.GroupedPipeline) (*types.PipelineExecutionResult, error) {
	// 통계 수집기 초기화
	statsCollector := NewStatsCollector(pipeline.ID, pipeline.Name)

	result := &types.PipelineExecutionResult{
		PipelineID:   pipeline.ID,
		PipelineName: pipeline.Name,
		Status:       "running",
		StartedAt:    time.Now(),
	}

	// 소스 생성 및 실행
	records, errs, err := e.createAndRunSource(ctx, pipeline.Source)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		statsCollector.RecordCollectionError()
		result.Statistics = statsCollector.GetStatistics()
		return result, err
	}

	// 레코드 처리
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
				return result, nil
			}

			// 수집량 카운트
			statsCollector.RecordCollected()

			// Stage 적용 (필터별 처리량 추적)
			data := record.Data
			var filtered bool
			for _, stage := range pipeline.Stages {
				statsCollector.RecordTransformInput(stage.Name, stage.Type)

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
				data = transformed
			}

			// 필터링된 레코드는 Sink로 전송하지 않음
			if filtered {
				continue
			}

			// Sink로 전송 (처리량 추적)
			for _, sink := range pipeline.Sinks {
				if err := e.sendToSink(ctx, data, sink); err != nil {
					statsCollector.RecordProcessingError()
				} else {
					statsCollector.RecordProcessed()
				}
			}

		case err := <-errs:
			if err != nil {
				statsCollector.RecordCollectionError()
				result.ErrorMessage = err.Error()
			}
		}
	}
}

// createAndRunSource 소스 생성 및 실행
func (e *GroupExecutor) createAndRunSource(ctx context.Context, gs types.GroupedSource) (<-chan source.Record, <-chan error, error) {
	// 파티션이 있는 경우 멀티 소스 처리
	if len(gs.Partitions) > 0 {
		return e.runMultiPartitionSource(ctx, gs)
	}

	// 단일 소스
	src, err := e.createSource(gs)
	if err != nil {
		return nil, nil, err
	}

	if err := src.Open(ctx); err != nil {
		return nil, nil, err
	}

	records, errs := src.Read(ctx)
	return records, errs, nil
}

// runMultiPartitionSource 멀티 파티션 소스 실행
func (e *GroupExecutor) runMultiPartitionSource(ctx context.Context, gs types.GroupedSource) (<-chan source.Record, <-chan error, error) {
	records := make(chan source.Record, 1000)
	errs := make(chan error, len(gs.Partitions))

	var wg sync.WaitGroup
	for _, partition := range gs.Partitions {
		if !partition.Enabled {
			continue
		}

		wg.Add(1)
		go func(p types.PartitionConfig) {
			defer wg.Done()

			// 파티션별 설정 병합
			config := make(map[string]any)
			for k, v := range gs.Config {
				config[k] = v
			}
			for k, v := range p.Config {
				config[k] = v
			}

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

	// config를 SourceV2 형식으로 변환
	// 실제 구현에서는 config 패키지의 SourceV2를 사용
	switch gs.Type {
	case "kafka":
		// Kafka 소스 설정
		return createKafkaSourceFromConfig(gs.Config)
	case "rest_api", "http":
		return createHTTPSourceFromConfig(gs.Config)
	case "sql":
		return createSQLSourceFromConfig(gs.Config)
	case "sql_event":
		return createSQLEventSourceFromConfig(gs.Config)
	case "cdc":
		return createCDCSourceFromConfig(gs.Config)
	case "file":
		return createFileSourceFromConfig(gs.Config)
	default:
		return nil, fmt.Errorf("unsupported source type: %s (config: %s)", gs.Type, string(configJSON))
	}
}

// applyStage Stage 적용
func (e *GroupExecutor) applyStage(data map[string]any, stage types.Stage) (map[string]any, error) {
	// TODO: 실제 변환 로직 구현
	// Bloblang, filter, sample, aggregate 등
	return data, nil
}

// sendToSink 싱크로 전송
func (e *GroupExecutor) sendToSink(ctx context.Context, data map[string]any, sink types.GroupedSink) error {
	// TODO: 조건부 라우팅 확인 (sink.Condition)
	// TODO: 실제 싱크 전송 로직
	_ = ctx
	_ = data
	_ = sink
	return nil
}

// Stop 그룹 실행 중지
func (e *GroupExecutor) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cancelFunc != nil {
		e.cancelFunc()
	}
	e.status = types.PipelineGroupStatusStopped
	return nil
}

// Pause 그룹 실행 일시정지
func (e *GroupExecutor) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status = types.PipelineGroupStatusPaused
	// TODO: 실제 일시정지 로직
	return nil
}

// Resume 그룹 실행 재개
func (e *GroupExecutor) Resume() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status != types.PipelineGroupStatusPaused {
		return fmt.Errorf("group is not paused")
	}
	e.status = types.PipelineGroupStatusRunning
	// TODO: 실제 재개 로직
	return nil
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

// PipelineRunner 개별 파이프라인 실행기
// TODO: 개별 파이프라인 실행 구현시 사용
type PipelineRunner struct {
	pipeline types.GroupedPipeline //nolint:unused
	source   source.Source         //nolint:unused
	status   string                //nolint:unused
	mu       sync.RWMutex          //nolint:unused
}

// 소스 생성 헬퍼 함수들
func createKafkaSourceFromConfig(config map[string]any) (source.Source, error) {
	// config를 pkg/config.SourceV2로 변환
	cfg := configToSourceV2(config)
	cfg.Type = "kafka"
	return source.NewKafkaSource(cfg)
}

func createHTTPSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
	cfg.Type = "http"
	return source.NewHTTPSource(cfg)
}

func createSQLSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
	cfg.Type = "sql"
	return source.NewSQLSource(cfg)
}

func createSQLEventSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
	cfg.Type = "sql_event"
	return source.NewSQLEventSource(cfg)
}

func createCDCSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
	cfg.Type = "cdc"
	return source.NewCDCSource(cfg)
}

func createFileSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
	cfg.Type = "file"
	return source.NewFileSource(cfg)
}
