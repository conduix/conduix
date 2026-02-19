// Package executor 파이프라인 그룹 실행기
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/pipeline-core/pkg/sink"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
	"github.com/conduix/conduix/pipeline-core/pkg/validator"
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

	// Checkpoint 관련
	checkpointClient *checkpoint.Client
	activeSources    map[string]source.Source // pipelineID -> source
	sourcesMu        sync.RWMutex

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

// NewGroupExecutor 그룹 실행기 생성
func NewGroupExecutor(group *types.PipelineGroup, opts ...GroupExecutorOption) *GroupExecutor {
	e := &GroupExecutor{
		group:           group,
		pipelineRunners: make(map[string]*PipelineRunner),
		status:          types.PipelineGroupStatusIdle,
		resultCh:        make(chan *types.PipelineExecutionResult, len(group.Pipelines)),
		errorCh:         make(chan error, len(group.Pipelines)),
		activeSources:   make(map[string]source.Source),
		sampleBuffers:   make(map[string]*SampleBuffer),
		statsCollectors: make(map[string]*StatsCollector),
		parentToChilds:  make(map[string][]link.PipelineLink),
		childToParents:  make(map[string][]link.PipelineLink),
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
		fmt.Printf("[GroupExecutor] Failed to load pipeline links: %v\n", err)
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
		fmt.Printf("[GroupExecutor] Loaded %d pipeline links for workflow %s\n", len(links), e.group.ID)
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

// isSinkStageType sink 역할을 하는 Stage 타입인지 확인
func isSinkStageType(stageType string) bool {
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

	// Stages에서 sink 타입 찾아서 미리 생성 및 열기
	sinkStages := make(map[string]sink.Sink) // stage name -> sink
	for _, stage := range pipeline.Stages {
		if isSinkStageType(stage.Type) {
			fmt.Printf("[runPipeline] Creating sink stage: %s (type: %s), config: %+v\n", stage.Name, stage.Type, stage.Config)
			s, err := e.createSinkFromStage(stage)
			if err != nil {
				result.Status = "failed"
				result.ErrorMessage = fmt.Sprintf("failed to create sink stage %s: %v", stage.Name, err)
				return result, err
			}
			if err := s.Open(ctx); err != nil {
				result.Status = "failed"
				result.ErrorMessage = fmt.Sprintf("failed to open sink stage %s: %v", stage.Name, err)
				return result, err
			}
			sinkStages[stage.Name] = s
			fmt.Printf("[runPipeline] Sink stage opened: %s (type: %s)\n", stage.Name, stage.Type)
		}
	}

	// 주입된 Kafka Sink도 sinkStages에 추가
	maps.Copy(sinkStages, injectedKafkaSinks)

	// 싱크 정리 defer
	defer func() {
		for name, s := range sinkStages {
			if err := s.Flush(ctx); err != nil {
				fmt.Printf("[runPipeline] Flush error for %s: %v\n", name, err)
			}
			if err := s.Close(); err != nil {
				fmt.Printf("[runPipeline] Close error for %s: %v\n", name, err)
			}
		}
	}()

	// 링크 기반 동적 Kafka Source 주입 (자식 파이프라인인 경우)
	sourceConfig := e.injectKafkaSourceForChild(&pipeline)

	// 소스 생성 및 실행
	fmt.Printf("[runPipeline] Creating source: %s (type: %s)\n", sourceConfig.Name, sourceConfig.Type)
	records, errs, src, err := e.createAndRunSource(ctx, pipeline.ID, sourceConfig)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		statsCollector.RecordCollectionError()
		result.Statistics = statsCollector.GetStatistics()
		fmt.Printf("[runPipeline] Source creation failed: %v\n", err)
		return result, err
	}
	fmt.Printf("[runPipeline] Source created successfully\n")

	// 활성 소스 추적
	if src != nil {
		e.sourcesMu.Lock()
		e.activeSources[pipeline.ID] = src
		e.sourcesMu.Unlock()

		defer func() {
			e.sourcesMu.Lock()
			delete(e.activeSources, pipeline.ID)
			e.sourcesMu.Unlock()
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
				fmt.Printf("[checkpoint] Warning: Failed to flush checkpoints: %v\n", err)
			} else if len(sourceCheckpoints) > 0 {
				fmt.Printf("[checkpoint] Saved %d checkpoints for pipeline %s\n", len(sourceCheckpoints), pipeline.ID)
			}
		}
	}

	// 소스 스키마 검증기 생성 (JSONSchema가 있는 경우)
	var sourceValidator *validator.SchemaValidator
	if pipeline.Source.JSONSchema != "" {
		sourceValidator, err = validator.NewSchemaValidator(pipeline.Source.JSONSchema)
		if err != nil {
			fmt.Printf("[runPipeline] Source schema validator creation failed: %v\n", err)
			// 스키마 파싱 실패는 경고로만 처리, 검증 없이 계속 진행
			sourceValidator = nil
		} else {
			fmt.Printf("[runPipeline] Source schema validator created\n")
		}
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

				fmt.Printf("[runPipeline] Completed: read=%d, written=%d, errors=%d\n",
					result.RecordsRead, result.RecordsWritten, result.ErrorCount)
				saveCheckpoints() // 완료 시 체크포인트 저장
				return result, nil
			}

			// 수집량 카운트
			statsCollector.RecordCollected()

			// 소스 스키마 검증 (검증기가 있는 경우)
			if sourceValidator != nil {
				validationResult := sourceValidator.Validate(record.Data)
				if !validationResult.Valid {
					statsCollector.RecordProcessingError()
					fmt.Printf("[runPipeline] Source schema validation failed: %v\n", validationResult.Errors)
					continue // 스키마 검증 실패 시 레코드 건너뛰기
				}
			}

			// Stage 적용 (필터별 처리량 추적)
			data := record.Data
			var filtered bool
			for _, stage := range pipeline.Stages {
				statsCollector.RecordTransformInput(stage.Name, stage.Type)

				// Sink stage인 경우: 데이터를 sink로 전송하고 통과
				if isSinkStageType(stage.Type) {
					if s, ok := sinkStages[stage.Name]; ok {
						if err := e.sendToSink(ctx, data, s); err != nil {
							statsCollector.RecordProcessingError()
							errMsg := fmt.Sprintf("[%s] %v", stage.Name, err)
							fmt.Printf("[runPipeline] Sink stage %s write error: %v\n", stage.Name, err)
							addSinkError(errMsg)
						} else {
							statsCollector.RecordProcessed()
						}
					}
					statsCollector.RecordTransformOutput(stage.Name)
					sampleBuffer.AddSample(stage.Name, data) // 샘플 저장
					continue                                 // sink stage는 데이터를 변환하지 않음, 다음 stage로
				}

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

		case err := <-errs:
			if err != nil {
				statsCollector.RecordCollectionError()
				result.ErrorMessage = err.Error()
				fmt.Printf("[runPipeline] Source error: %v\n", err)
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
			fmt.Printf("[checkpoint] Loading checkpoints for pipeline %s (source type: %s)\n", pipelineID, cpSource.SourceType())

			dbCheckpoints, err := e.checkpointClient.LoadCheckpoints(ctx, pipelineID)
			if err != nil {
				fmt.Printf("[checkpoint] Warning: Failed to load checkpoints: %v\n", err)
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
					fmt.Printf("[checkpoint] Warning: Failed to set checkpoints: %v\n", err)
				} else {
					fmt.Printf("[checkpoint] Restored %d checkpoints for pipeline %s\n", len(sourceCheckpoints), pipelineID)
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
	for _, partition := range gs.Partitions {
		if !partition.Enabled {
			continue
		}

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
		// 기타 타입은 통과
		return data, nil
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
func (e *GroupExecutor) sendToSink(ctx context.Context, data map[string]any, s sink.Sink) error {
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
func (e *GroupExecutor) createSinkFromStage(stage types.Stage) (sink.Sink, error) {
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
	}

	return sink.NewSink(cfg)
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

func createKubernetesSourceFromConfig(config map[string]any) (source.Source, error) {
	cfg := configToSourceV2(config)
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
		e.sourcesMu.RLock()
		if src, ok := e.activeSources[pipelineID]; ok {
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
		e.sourcesMu.RUnlock()

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
func (e *GroupExecutor) injectKafkaSinksForParent(ctx context.Context, pipeline *types.GroupedPipeline) map[string]sink.Sink {
	injectedSinks := make(map[string]sink.Sink)

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
	fmt.Printf("[injectKafkaSinksForParent] Pipeline %s has %d children, injecting Kafka sinks\n", pipeline.ID, len(childLinks))

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
		kafkaSink, err := sink.NewSink(cfg)
		if err != nil {
			fmt.Printf("[injectKafkaSinksForParent] Failed to create Kafka sink for link %s: %v\n", l.ID, err)
			continue
		}

		// Sink 열기
		if err := kafkaSink.Open(ctx); err != nil {
			fmt.Printf("[injectKafkaSinksForParent] Failed to open Kafka sink for link %s: %v\n", l.ID, err)
			continue
		}

		// 주입된 Sink 등록
		sinkName := fmt.Sprintf("kafka_link_%s", l.ChildPipelineID[:8])
		injectedSinks[sinkName] = kafkaSink

		fmt.Printf("[injectKafkaSinksForParent] Injected Kafka sink %s -> topic: %s\n", sinkName, l.KafkaTopic)
	}

	return injectedSinks
}

// injectKafkaSourceForChild 자식 파이프라인에 Kafka Source 주입
// 부모가 있는 경우 원래 소스를 Kafka Source로 교체
func (e *GroupExecutor) injectKafkaSourceForChild(pipeline *types.GroupedPipeline) types.GroupedSource {
	// 링크가 없으면 원래 소스 반환
	if e.linkClient == nil || len(e.pipelineLinks) == 0 {
		return pipeline.Source
	}

	// 이 파이프라인이 자식인지 확인
	e.linkMu.RLock()
	parentLinks, hasParents := e.childToParents[pipeline.ID]
	e.linkMu.RUnlock()

	if !hasParents || len(parentLinks) == 0 {
		// 부모가 없으면 원래 소스 반환
		return pipeline.Source
	}

	// 첫 번째 부모 링크 사용 (여러 부모가 있을 경우 첫 번째 사용)
	l := parentLinks[0]

	fmt.Printf("[injectKafkaSourceForChild] Pipeline %s is a child, replacing source with Kafka source (topic: %s)\n",
		pipeline.ID, l.KafkaTopic)

	// Kafka brokers 환경변수에서 동적으로 가져오기
	brokers := link.GetKafkaBrokers()

	// Kafka Source 설정 생성
	kafkaSourceConfig := types.GroupedSource{
		Type: "kafka",
		Name: fmt.Sprintf("kafka_link_source_%s", l.ParentPipelineID[:8]),
		Config: map[string]any{
			"brokers":        brokers,
			"topics":         []string{l.KafkaTopic},
			"consumer_group": fmt.Sprintf("conduix_%s_%s", e.group.ID, pipeline.ID),
			"start_offset":   "newest", // 또는 "oldest"
		},
	}

	return kafkaSourceConfig
}
