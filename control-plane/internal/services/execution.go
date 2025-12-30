package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ExecutionService 워크플로우 실행 서비스
type ExecutionService struct {
	db     *database.DB
	redis  *RedisService
	logger *slog.Logger

	// 실행 중인 워크플로우 추적
	runningWorkflows map[string]*WorkflowExecution
	mu               sync.RWMutex
}

// WorkflowExecution 실행 중인 워크플로우 정보
type WorkflowExecution struct {
	WorkflowID  string
	ExecutionID string
	Type        types.WorkflowType
	StartedAt   time.Time
	Cancel      context.CancelFunc

	// 실시간용 오프셋 (Kafka, DB 등)
	Offsets map[string]int64 `json:"offsets,omitempty"`
}

// NewExecutionService 새 서비스 생성
func NewExecutionService(db *database.DB, redis *RedisService, logger *slog.Logger) *ExecutionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecutionService{
		db:               db,
		redis:            redis,
		logger:           logger,
		runningWorkflows: make(map[string]*WorkflowExecution),
	}
}

// StartExecution 워크플로우 실행 시작
func (s *ExecutionService) StartExecution(ctx context.Context, workflowID, userID string) (*models.WorkflowExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 이미 실행 중인지 확인
	if _, running := s.runningWorkflows[workflowID]; running {
		return nil, fmt.Errorf("workflow is already running")
	}

	// 워크플로우 조회
	var workflow models.Workflow
	if err := s.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		return nil, fmt.Errorf("workflow not found: %w", err)
	}

	// 실행 기록 생성
	execution := &models.WorkflowExecution{
		ID:            uuid.New().String(),
		WorkflowID:    workflowID,
		Status:        string(types.WorkflowStatusRunning),
		StartedAt:     time.Now(),
		TriggeredBy:   "user",
		TriggeredByID: userID,
		CreatedAt:     time.Now(),
	}

	if err := s.db.Create(execution).Error; err != nil {
		return nil, fmt.Errorf("failed to create execution record: %w", err)
	}

	// 워크플로우 상태 업데이트
	workflow.Status = string(types.WorkflowStatusRunning)
	workflow.LastRunAt = &execution.StartedAt
	s.db.Save(&workflow)

	// 실행 컨텍스트 생성
	execCtx, cancel := context.WithCancel(ctx)

	// 실행 추적에 추가
	workflowExec := &WorkflowExecution{
		WorkflowID:  workflowID,
		ExecutionID: execution.ID,
		Type:        types.WorkflowType(workflow.Type),
		StartedAt:   execution.StartedAt,
		Cancel:      cancel,
		Offsets:     make(map[string]int64),
	}
	s.runningWorkflows[workflowID] = workflowExec

	// 타입에 따라 실행
	go s.runExecution(execCtx, workflowExec, &workflow)

	s.logger.Info("Started workflow execution",
		"workflow_id", workflowID,
		"execution_id", execution.ID,
		"type", workflow.Type)

	return execution, nil
}

// StopExecution 워크플로우 실행 중지
func (s *ExecutionService) StopExecution(ctx context.Context, workflowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, running := s.runningWorkflows[workflowID]
	if !running {
		return fmt.Errorf("workflow is not running")
	}

	// 컨텍스트 취소하여 실행 중지
	exec.Cancel()

	// 실시간 파이프라인의 경우 오프셋 저장
	if exec.Type == types.WorkflowTypeRealtime && len(exec.Offsets) > 0 {
		s.saveOffsets(workflowID, exec.ExecutionID, exec.Offsets)
	}

	// 실행 기록 업데이트
	now := time.Now()
	s.db.Model(&models.WorkflowExecution{}).
		Where("id = ?", exec.ExecutionID).
		Updates(map[string]any{
			"status":       string(types.WorkflowStatusStopped),
			"completed_at": now,
			"duration_ms":  now.Sub(exec.StartedAt).Milliseconds(),
		})

	// 워크플로우 상태 업데이트
	s.db.Model(&models.Workflow{}).
		Where("id = ?", workflowID).
		Update("status", string(types.WorkflowStatusStopped))

	// 추적에서 제거
	delete(s.runningWorkflows, workflowID)

	s.logger.Info("Stopped workflow execution",
		"workflow_id", workflowID,
		"execution_id", exec.ExecutionID)

	return nil
}

// PauseExecution 워크플로우 실행 일시 정지
func (s *ExecutionService) PauseExecution(ctx context.Context, workflowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, running := s.runningWorkflows[workflowID]
	if !running {
		return fmt.Errorf("workflow is not running")
	}

	// 상태 업데이트 (실제 일시정지는 에이전트에서 처리)
	s.db.Model(&models.Workflow{}).
		Where("id = ?", workflowID).
		Update("status", string(types.WorkflowStatusPaused))

	s.logger.Info("Paused workflow execution",
		"workflow_id", workflowID,
		"execution_id", exec.ExecutionID)

	return nil
}

// ResumeExecution 워크플로우 실행 재개
func (s *ExecutionService) ResumeExecution(ctx context.Context, workflowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, running := s.runningWorkflows[workflowID]
	if !running {
		return fmt.Errorf("workflow is not running")
	}

	// 상태 업데이트
	s.db.Model(&models.Workflow{}).
		Where("id = ?", workflowID).
		Update("status", string(types.WorkflowStatusRunning))

	s.logger.Info("Resumed workflow execution",
		"workflow_id", workflowID,
		"execution_id", exec.ExecutionID)

	return nil
}

// runExecution 실제 실행 로직
func (s *ExecutionService) runExecution(ctx context.Context, exec *WorkflowExecution, workflow *models.Workflow) {
	defer s.cleanupExecution(exec.WorkflowID, exec.ExecutionID)

	switch exec.Type {
	case types.WorkflowTypeBatch:
		s.runBatchExecution(ctx, exec, workflow)
	case types.WorkflowTypeRealtime:
		s.runRealtimeExecution(ctx, exec, workflow)
	default:
		s.logger.Error("Unknown workflow type", "type", exec.Type)
	}
}

// runBatchExecution 배치 파이프라인 실행
// 배치는 한 번 실행 후 완료됨
// 계층형 파이프라인 지원: 부모 파이프라인 완료 후 자식 파이프라인을 부모 출력 레코드마다 확장 실행
func (s *ExecutionService) runBatchExecution(ctx context.Context, exec *WorkflowExecution, workflow *models.Workflow) {
	s.logger.Info("Running batch execution",
		"workflow_id", workflow.ID,
		"execution_id", exec.ExecutionID)

	// 파이프라인 설정 파싱
	var pipelines []types.WorkflowPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines)
	}

	// 파이프라인 계층 정렬 (부모 먼저 실행)
	sortedPipelines := s.sortPipelinesByHierarchy(pipelines)

	totalRecords := int64(0)
	failedRecords := int64(0)
	pipelineResults := make([]types.PipelineExecutionResult, 0)

	// 부모 파이프라인 출력 저장 (자식 확장용)
	parentOutputs := make(map[string][]map[string]any)

	// 각 파이프라인 실행
	for _, p := range sortedPipelines {
		select {
		case <-ctx.Done():
			// 중지 요청
			s.logger.Info("Batch execution canceled",
				"workflow_id", workflow.ID,
				"pipeline_id", p.ID)
			return
		default:
			// 자식 파이프라인이고 확장 모드가 for_each_record인 경우
			if p.ParentPipelineID != nil && p.ExpansionMode == types.ExpansionModeForEachRecord {
				// 부모 출력 또는 DataType에서 레코드 조회
				records := s.getExpansionRecords(p, parentOutputs)

				s.logger.Info("Expanding child pipeline",
					"pipeline_id", p.ID,
					"pipeline_name", p.Name,
					"parent_id", *p.ParentPipelineID,
					"expansion_count", len(records))

				// 각 레코드에 대해 파이프라인 실행
				for i, record := range records {
					// 파라미터 바인딩 적용
					expandedPipeline := s.applyParameterBindings(p, record, i)

					result := s.executePipeline(ctx, &expandedPipeline, workflow)
					result.PipelineName = fmt.Sprintf("%s[%d]", p.Name, i)
					pipelineResults = append(pipelineResults, result)
					totalRecords += result.RecordsProcessed
					failedRecords += result.RecordsFailed
				}
			} else {
				// 일반 파이프라인 실행
				result := s.executePipeline(ctx, &p, workflow)
				pipelineResults = append(pipelineResults, result)
				totalRecords += result.RecordsProcessed
				failedRecords += result.RecordsFailed

				// 자식이 있는 부모 파이프라인의 경우 출력 저장
				if s.hasChildPipelines(p.ID, pipelines) && p.TargetDataTypeID != nil {
					outputs := s.queryDataTypeRecords(*p.TargetDataTypeID)
					parentOutputs[p.ID] = outputs
				}
			}
		}
	}

	// 실행 완료
	now := time.Now()
	resultsJSON, _ := json.Marshal(pipelineResults)

	s.db.Model(&models.WorkflowExecution{}).
		Where("id = ?", exec.ExecutionID).
		Updates(map[string]any{
			"status":           string(types.WorkflowStatusCompleted),
			"completed_at":     now,
			"duration_ms":      now.Sub(exec.StartedAt).Milliseconds(),
			"total_records":    totalRecords,
			"failed_records":   failedRecords,
			"pipeline_results": string(resultsJSON),
		})

	s.db.Model(&models.Workflow{}).
		Where("id = ?", workflow.ID).
		Update("status", string(types.WorkflowStatusCompleted))

	s.logger.Info("Batch execution completed",
		"workflow_id", workflow.ID,
		"execution_id", exec.ExecutionID,
		"total_records", totalRecords,
		"failed_records", failedRecords)
}

// sortPipelinesByHierarchy 파이프라인을 계층 순서로 정렬 (부모 먼저)
func (s *ExecutionService) sortPipelinesByHierarchy(pipelines []types.WorkflowPipeline) []types.WorkflowPipeline {
	// 파이프라인 맵 생성
	pipelineMap := make(map[string]types.WorkflowPipeline)
	for _, p := range pipelines {
		pipelineMap[p.ID] = p
	}

	// 방문 여부 추적
	visited := make(map[string]bool)
	result := make([]types.WorkflowPipeline, 0, len(pipelines))

	// DFS로 위상 정렬
	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		p := pipelineMap[id]
		// 부모가 있으면 부모 먼저 방문
		if p.ParentPipelineID != nil {
			visit(*p.ParentPipelineID)
		}
		// DependsOn 의존성도 처리
		for _, depID := range p.DependsOn {
			visit(depID)
		}

		result = append(result, p)
	}

	// 모든 파이프라인 방문
	for _, p := range pipelines {
		visit(p.ID)
	}

	return result
}

// hasChildPipelines 자식 파이프라인이 있는지 확인
func (s *ExecutionService) hasChildPipelines(pipelineID string, pipelines []types.WorkflowPipeline) bool {
	for _, p := range pipelines {
		if p.ParentPipelineID != nil && *p.ParentPipelineID == pipelineID {
			return true
		}
	}
	return false
}

// getExpansionRecords 자식 파이프라인 확장을 위한 레코드 조회
func (s *ExecutionService) getExpansionRecords(pipeline types.WorkflowPipeline, parentOutputs map[string][]map[string]any) []map[string]any {
	// 부모 파이프라인 출력에서 먼저 조회
	if pipeline.ParentPipelineID != nil {
		if outputs, ok := parentOutputs[*pipeline.ParentPipelineID]; ok && len(outputs) > 0 {
			return outputs
		}
	}

	// DataType에서 조회
	if pipeline.TargetDataTypeID != nil {
		return s.queryDataTypeRecords(*pipeline.TargetDataTypeID)
	}

	return nil
}

// queryDataTypeRecords DataType의 레코드 조회
func (s *ExecutionService) queryDataTypeRecords(dataTypeID string) []map[string]any {
	// DataType 조회
	var dataType models.DataType
	if err := s.db.First(&dataType, "id = ?", dataTypeID).Error; err != nil {
		s.logger.Error("Failed to find DataType", "data_type_id", dataTypeID, "error", err)
		return nil
	}

	// Storage 설정에서 테이블/인덱스 정보 추출
	var storage map[string]any
	if dataType.Storage != "" {
		_ = json.Unmarshal([]byte(dataType.Storage), &storage)
	}

	// TODO: 실제 저장소 (MySQL, Elasticsearch 등)에서 레코드 조회
	// 현재는 임시로 빈 배열 반환 - 실제 구현 시 저장소 타입에 따라 쿼리 실행
	s.logger.Info("Querying DataType records",
		"data_type_id", dataTypeID,
		"data_type_name", dataType.Name,
		"storage", storage)

	return nil
}

// applyParameterBindings 부모 레코드 값을 자식 파이프라인 파라미터에 바인딩
func (s *ExecutionService) applyParameterBindings(pipeline types.WorkflowPipeline, record map[string]any, index int) types.WorkflowPipeline {
	// 파이프라인 복사
	expandedPipeline := pipeline
	expandedPipeline.ID = fmt.Sprintf("%s_%d", pipeline.ID, index)

	// Source config에 파라미터 바인딩 적용
	if expandedPipeline.Source.Config == nil {
		expandedPipeline.Source.Config = make(map[string]any)
	}

	for _, binding := range pipeline.ParameterBindings {
		if value, ok := record[binding.ParentField]; ok {
			expandedPipeline.Source.Config[binding.ChildParam] = value
			s.logger.Debug("Applied parameter binding",
				"parent_field", binding.ParentField,
				"child_param", binding.ChildParam,
				"value", value)
		}
	}

	return expandedPipeline
}

// runRealtimeExecution 실시간 파이프라인 실행
// 실시간은 중지될 때까지 계속 실행됨
func (s *ExecutionService) runRealtimeExecution(ctx context.Context, exec *WorkflowExecution, workflow *models.Workflow) {
	s.logger.Info("Running realtime execution",
		"workflow_id", workflow.ID,
		"execution_id", exec.ExecutionID)

	// 파이프라인 설정 파싱
	var pipelines []types.WorkflowPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines)
	}

	// 마지막 저장된 오프셋 로드
	exec.Offsets = s.loadOffsets(workflow.ID)

	// 무한 폴링 루프
	ticker := time.NewTicker(time.Second) // 1초마다 폴링 (설정 가능)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 중지 요청 - 오프셋 저장
			s.saveOffsets(workflow.ID, exec.ExecutionID, exec.Offsets)
			s.logger.Info("Realtime execution stopped, offsets saved",
				"workflow_id", workflow.ID,
				"offsets", exec.Offsets)
			return

		case <-ticker.C:
			// 폴링 주기마다 실행
			for _, p := range pipelines {
				// 파이프라인 실행 (실제로는 에이전트에 위임)
				result := s.executePipeline(ctx, &p, workflow)

				// 오프셋 업데이트
				if result.Offset > 0 {
					exec.Offsets[p.ID] = result.Offset
				}

				// 통계 업데이트 (hourly stats에 누적)
				s.updateHourlyStats(workflow.ID, p.ID, p.Name, result)
			}
		}
	}
}

// executePipeline 단일 파이프라인 실행
func (s *ExecutionService) executePipeline(ctx context.Context, pipeline *types.WorkflowPipeline, workflow *models.Workflow) types.PipelineExecutionResult {
	// TODO: 실제 실행은 에이전트에 위임
	// 현재는 시뮬레이션
	return types.PipelineExecutionResult{
		PipelineID:       pipeline.ID,
		PipelineName:     pipeline.Name,
		Status:           "completed",
		RecordsProcessed: 0,
		RecordsFailed:    0,
		StartedAt:        time.Now(),
		CompletedAt:      time.Now(),
	}
}

// saveOffsets 오프셋 저장 (Redis 또는 DB)
func (s *ExecutionService) saveOffsets(workflowID, executionID string, offsets map[string]int64) {
	if s.redis != nil {
		key := fmt.Sprintf("workflow:%s:offsets", workflowID)
		data, _ := json.Marshal(offsets)
		_ = s.redis.Set(context.Background(), key, string(data), 0)
	}

	// DB에도 저장 (메타데이터로)
	offsetsJSON, _ := json.Marshal(offsets)
	s.db.Model(&models.WorkflowExecution{}).
		Where("id = ?", executionID).
		Update("metadata", string(offsetsJSON))
}

// loadOffsets 오프셋 로드
func (s *ExecutionService) loadOffsets(workflowID string) map[string]int64 {
	offsets := make(map[string]int64)

	if s.redis != nil {
		key := fmt.Sprintf("workflow:%s:offsets", workflowID)
		data, err := s.redis.Get(context.Background(), key)
		if err == nil && data != "" {
			_ = json.Unmarshal([]byte(data), &offsets)
		}
	}

	return offsets
}

// updateHourlyStats 시간별 통계 업데이트
func (s *ExecutionService) updateHourlyStats(workflowID, pipelineID, pipelineName string, result types.PipelineExecutionResult) {
	bucketHour := time.Now().Truncate(time.Hour)

	var stats models.PipelineHourlyStats
	err := s.db.Where("pipeline_id = ? AND bucket_hour = ?", pipelineID, bucketHour).First(&stats).Error

	if err != nil {
		// 새 버킷 생성
		stats = models.PipelineHourlyStats{
			ID:               uuid.New().String(),
			PipelineID:       pipelineID,
			PipelineName:     pipelineName,
			WorkflowID:       workflowID,
			BucketHour:       bucketHour,
			RecordsCollected: result.RecordsProcessed,
			RecordsProcessed: result.RecordsProcessed,
			CollectionErrors: result.RecordsFailed,
			SampleCount:      1,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.db.Create(&stats)
	} else {
		// 기존 버킷 업데이트
		s.db.Model(&stats).Updates(map[string]any{
			"records_collected": stats.RecordsCollected + result.RecordsProcessed,
			"records_processed": stats.RecordsProcessed + result.RecordsProcessed,
			"collection_errors": stats.CollectionErrors + result.RecordsFailed,
			"sample_count":      stats.SampleCount + 1,
			"updated_at":        time.Now(),
		})
	}
}

// cleanupExecution 실행 정리
func (s *ExecutionService) cleanupExecution(workflowID, executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.runningWorkflows, workflowID)
}

// GetRunningWorkflows 실행 중인 워크플로우 목록 조회
func (s *ExecutionService) GetRunningWorkflows() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workflows := make([]string, 0, len(s.runningWorkflows))
	for workflowID := range s.runningWorkflows {
		workflows = append(workflows, workflowID)
	}
	return workflows
}

// IsRunning 워크플로우 실행 중 여부 확인
func (s *ExecutionService) IsRunning(workflowID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, running := s.runningWorkflows[workflowID]
	return running
}
