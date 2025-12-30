// Package services 스케줄러 서비스
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// SchedulerService 배치 워크플로우 스케줄러 서비스
type SchedulerService struct {
	db           *database.DB
	redisService *RedisService
	cron         *cron.Cron
	jobs         map[string]cron.EntryID // workflowID -> cron entry ID
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	running      bool
}

// SchedulerConfig 스케줄러 설정
type SchedulerConfig struct {
	RefreshInterval time.Duration // DB 변경 감지 주기 (기본: 30초)
}

// DefaultSchedulerConfig 기본 스케줄러 설정
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		RefreshInterval: 30 * time.Second,
	}
}

// NewSchedulerService 새 스케줄러 서비스 생성
func NewSchedulerService(db *database.DB, redisService *RedisService, cfg *SchedulerConfig) *SchedulerService {
	if cfg == nil {
		cfg = DefaultSchedulerConfig()
	}
	// TODO: cfg.RefreshInterval을 사용한 DB 변경 감지 구현
	_ = cfg

	ctx, cancel := context.WithCancel(context.Background())

	return &SchedulerService{
		db:           db,
		redisService: redisService,
		cron:         cron.New(cron.WithLocation(time.UTC)),
		jobs:         make(map[string]cron.EntryID),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start 스케줄러 시작
func (s *SchedulerService) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// DB에서 활성화된 스케줄 로드
	if err := s.loadSchedules(); err != nil {
		fmt.Printf("[Scheduler] Warning: Failed to load schedules: %v\n", err)
	}

	// Cron 스케줄러 시작
	s.cron.Start()
	fmt.Printf("[Scheduler] Started with %d active schedules\n", len(s.jobs))

	return nil
}

// Stop 스케줄러 중지
func (s *SchedulerService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.cancel()
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false

	fmt.Println("[Scheduler] Stopped")
	return nil
}

// loadSchedules DB에서 활성화된 스케줄 로드
func (s *SchedulerService) loadSchedules() error {
	var workflows []models.Workflow
	err := s.db.Where("type = ? AND schedule_enabled = ? AND schedule_type = ?", "batch", true, "cron").Find(&workflows).Error
	if err != nil {
		return fmt.Errorf("failed to query workflows: %w", err)
	}

	for _, workflow := range workflows {
		if err := s.addScheduleInternal(&workflow); err != nil {
			fmt.Printf("[Scheduler] Failed to add schedule for workflow %s: %v\n", workflow.ID, err)
		}
	}

	return nil
}

// AddSchedule 스케줄 추가
func (s *SchedulerService) AddSchedule(workflow *models.Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addScheduleInternal(workflow)
}

// addScheduleInternal 스케줄 추가 (내부)
func (s *SchedulerService) addScheduleInternal(workflow *models.Workflow) error {
	// 기존 스케줄 제거
	if entryID, exists := s.jobs[workflow.ID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, workflow.ID)
	}

	if !workflow.ScheduleEnabled || workflow.ScheduleCron == "" {
		return nil
	}

	// 타임존 처리
	loc := time.UTC
	if workflow.ScheduleTimezone != "" {
		var err error
		loc, err = time.LoadLocation(workflow.ScheduleTimezone)
		if err != nil {
			fmt.Printf("[Scheduler] Invalid timezone %s for workflow %s, using UTC\n", workflow.ScheduleTimezone, workflow.ID)
			loc = time.UTC
		}
	}

	// Job 생성
	job := &scheduledJob{
		scheduler:  s,
		workflowID: workflow.ID,
		timezone:   loc,
	}

	// Cron 표현식 파싱 및 등록
	// robfig/cron v3는 5자리 (분 시 일 월 요일) 또는 6자리 (초 분 시 일 월 요일) 지원
	cronExpr := workflow.ScheduleCron
	entryID, err := s.cron.AddJob(cronExpr, cron.NewChain(cron.Recover(cron.DefaultLogger)).Then(job))
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", cronExpr, err)
	}

	s.jobs[workflow.ID] = entryID

	// NextRunAt 업데이트
	entry := s.cron.Entry(entryID)
	nextRun := entry.Next
	s.db.Model(&models.Workflow{}).Where("id = ?", workflow.ID).Update("next_run_at", nextRun)

	fmt.Printf("[Scheduler] Added schedule for workflow %s: %s (next: %s)\n", workflow.ID, cronExpr, nextRun.In(loc).Format(time.RFC3339))
	return nil
}

// RemoveSchedule 스케줄 제거
func (s *SchedulerService) RemoveSchedule(workflowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[workflowID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, workflowID)
		fmt.Printf("[Scheduler] Removed schedule for workflow %s\n", workflowID)
	}
}

// UpdateSchedule 스케줄 업데이트
func (s *SchedulerService) UpdateSchedule(workflow *models.Workflow) error {
	return s.AddSchedule(workflow) // 기존 스케줄 제거 후 재등록
}

// TriggerNow 즉시 실행 (수동)
func (s *SchedulerService) TriggerNow(workflowID, userID string) (*models.WorkflowExecution, error) {
	// 워크플로우 조회
	var workflow models.Workflow
	if err := s.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		return nil, fmt.Errorf("workflow not found: %w", err)
	}

	// 배치 워크플로우만 수동 실행 가능
	if workflow.Type != "batch" {
		return nil, fmt.Errorf("only batch workflows can be triggered manually")
	}

	// 이미 실행 중인지 확인
	if workflow.Status == "running" {
		return nil, fmt.Errorf("workflow is already running")
	}

	// 실행 레코드 생성
	execution := &models.WorkflowExecution{
		ID:            uuid.New().String(),
		WorkflowID:    workflow.ID,
		Status:        "running",
		StartedAt:     time.Now(),
		TriggeredBy:   "user",
		TriggeredByID: userID,
	}

	if err := s.db.Create(execution).Error; err != nil {
		return nil, fmt.Errorf("failed to create execution record: %w", err)
	}

	// 워크플로우 상태 업데이트
	s.db.Model(&workflow).Updates(map[string]any{
		"status":      "running",
		"last_run_at": time.Now(),
	})

	// Redis로 실행 명령 발행
	if err := s.publishWorkflowExecution(&workflow, execution, "user", userID); err != nil {
		// Redis 실패해도 실행 레코드는 유지
		fmt.Printf("[Scheduler] Warning: Failed to publish execution command: %v\n", err)
	}

	return execution, nil
}

// publishWorkflowExecution Redis로 실행 명령 발행
func (s *SchedulerService) publishWorkflowExecution(workflow *models.Workflow, execution *models.WorkflowExecution, triggeredBy, userID string) error {
	if s.redisService == nil || !s.redisService.IsHealthy() {
		return fmt.Errorf("redis service is not available")
	}

	// PipelinesConfig 파싱
	var pipelines []types.WorkflowPipeline
	if workflow.PipelinesConfig != "" {
		if err := json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines); err != nil {
			return fmt.Errorf("failed to parse pipelines config: %w", err)
		}
	}

	// 워크플로우 설정 구성
	workflowConfig := &types.Workflow{
		ID:            workflow.ID,
		Name:          workflow.Name,
		Type:          types.WorkflowType(workflow.Type),
		ExecutionMode: types.ExecutionMode(workflow.ExecutionMode),
		Pipelines:     pipelines,
	}

	cmd := &types.WorkflowExecutionCommand{
		ID:             uuid.New().String(),
		WorkflowID:     workflow.ID,
		ExecutionID:    execution.ID,
		TriggeredBy:    triggeredBy,
		UserID:         userID,
		WorkflowConfig: workflowConfig,
		Timestamp:      time.Now(),
	}

	return s.redisService.PublishWorkflowExecution(cmd)
}

// GetScheduleInfo 스케줄 정보 조회
func (s *SchedulerService) GetScheduleInfo(workflowID string) *ScheduleInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entryID, exists := s.jobs[workflowID]
	if !exists {
		return nil
	}

	entry := s.cron.Entry(entryID)
	return &ScheduleInfo{
		WorkflowID: workflowID,
		NextRunAt:  &entry.Next,
		PrevRunAt:  &entry.Prev,
		IsActive:   true,
	}
}

// ListActiveSchedules 활성 스케줄 목록
func (s *SchedulerService) ListActiveSchedules() []ScheduleInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ScheduleInfo, 0, len(s.jobs))
	for workflowID, entryID := range s.jobs {
		entry := s.cron.Entry(entryID)
		result = append(result, ScheduleInfo{
			WorkflowID: workflowID,
			NextRunAt:  &entry.Next,
			PrevRunAt:  &entry.Prev,
			IsActive:   true,
		})
	}
	return result
}

// GetActiveScheduleCount 활성 스케줄 수
func (s *SchedulerService) GetActiveScheduleCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// ScheduleInfo 스케줄 정보
type ScheduleInfo struct {
	WorkflowID string     `json:"workflow_id"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	PrevRunAt  *time.Time `json:"prev_run_at,omitempty"`
	IsActive   bool       `json:"is_active"`
}

// scheduledJob cron job 구현
type scheduledJob struct {
	scheduler  *SchedulerService
	workflowID string
	timezone   *time.Location
}

// Run cron job 실행
func (j *scheduledJob) Run() {
	fmt.Printf("[Scheduler] Executing scheduled job for workflow %s\n", j.workflowID)

	// 최신 워크플로우 정보 조회
	var workflow models.Workflow
	if err := j.scheduler.db.First(&workflow, "id = ?", j.workflowID).Error; err != nil {
		fmt.Printf("[Scheduler] Failed to load workflow %s: %v\n", j.workflowID, err)
		return
	}

	// 스케줄 비활성화 확인
	if !workflow.ScheduleEnabled {
		fmt.Printf("[Scheduler] Schedule disabled for workflow %s, skipping\n", j.workflowID)
		j.scheduler.RemoveSchedule(j.workflowID)
		return
	}

	// 이미 실행 중인지 확인
	if workflow.Status == "running" {
		fmt.Printf("[Scheduler] Workflow %s is already running, skipping\n", j.workflowID)
		return
	}

	// 실행 레코드 생성
	execution := &models.WorkflowExecution{
		ID:          uuid.New().String(),
		WorkflowID:  workflow.ID,
		Status:      "running",
		StartedAt:   time.Now(),
		TriggeredBy: "schedule",
	}

	if err := j.scheduler.db.Create(execution).Error; err != nil {
		fmt.Printf("[Scheduler] Failed to create execution record for workflow %s: %v\n", j.workflowID, err)
		return
	}

	// 워크플로우 상태 업데이트
	now := time.Now()
	j.scheduler.db.Model(&workflow).Updates(map[string]any{
		"status":      "running",
		"last_run_at": now,
	})

	// NextRunAt 업데이트
	j.scheduler.mu.RLock()
	if entryID, exists := j.scheduler.jobs[j.workflowID]; exists {
		entry := j.scheduler.cron.Entry(entryID)
		j.scheduler.db.Model(&models.Workflow{}).Where("id = ?", j.workflowID).Update("next_run_at", entry.Next)
	}
	j.scheduler.mu.RUnlock()

	// Redis로 실행 명령 발행
	if err := j.scheduler.publishWorkflowExecution(&workflow, execution, "schedule", ""); err != nil {
		fmt.Printf("[Scheduler] Warning: Failed to publish execution command for workflow %s: %v\n", j.workflowID, err)
	}

	fmt.Printf("[Scheduler] Scheduled execution started for workflow %s (execution: %s)\n", j.workflowID, execution.ID)
}
