// Package services 스케줄러 서비스
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// SchedulerService 배치 워크플로우 스케줄러 서비스
type SchedulerService struct {
	db              *database.DB
	redisService    *RedisService
	cron            *cron.Cron
	jobs            map[string]cron.EntryID // workflowID -> cron entry ID
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	running         bool
	refreshInterval time.Duration // stale 실행 감지 주기
	staleGrace      time.Duration // 실행 시작 후 이 시간 이전은 감지 대상 제외(하트비트 등록 유예)
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

	ctx, cancel := context.WithCancel(context.Background())

	return &SchedulerService{
		db:              db,
		redisService:    redisService,
		cron:            cron.New(cron.WithLocation(time.UTC)),
		jobs:            make(map[string]cron.EntryID),
		ctx:             ctx,
		cancel:          cancel,
		refreshInterval: cfg.RefreshInterval,
		staleGrace:      2 * time.Minute, // 실행 직후 하트비트 등록 유예
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
		slog.Warn("Failed to load schedules", "error", err)
	}

	// Cron 스케줄러 시작
	s.cron.Start()
	slog.Info("Scheduler started", "active_schedules", len(s.jobs))

	// stale 실행 감지 루프 시작 (claim한 에이전트 크래시로 유실된 실행 복구)
	go s.staleExecutionLoop()

	return nil
}

// staleExecutionLoop은 주기적으로 stale(담당 에이전트가 사라진) running 실행을 감지해 정리한다.
func (s *SchedulerService) staleExecutionLoop() {
	interval := s.refreshInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.detectStaleExecutions()
		}
	}
}

// detectStaleExecutions는 DB의 running 실행 중, 살아있는 에이전트 하트비트에 없고
// 유예시간(staleGrace)을 지난 것을 stale로 판정하여 failed로 전이한다.
// SETNX claim은 TTL로 자연 만료되므로 별도 해제는 불필요하다.
func (s *SchedulerService) detectStaleExecutions() {
	// 살아있는 에이전트들이 현재 실행 중이라고 보고한 execution 집합
	heartbeats, err := s.redisService.GetAllAgentHeartbeats()
	if err != nil {
		slog.Error("stale-check: failed to get heartbeats", "error", err)
		return
	}
	liveExecs := make(map[string]struct{})
	for _, hb := range heartbeats {
		for _, re := range hb.RunningExecs {
			liveExecs[re.ExecutionID] = struct{}{}
		}
	}

	// DB에서 running 실행 조회 (유예시간 지난 것만).
	// batch 워크플로우는 제외한다: bulk 는 agent 가 K8s Job 을 위임 생성만 하고(fire-and-forget)
	// Job 은 agent heartbeat 의 running_execs 에 등록되지 않는다(delegateBatchJob 은 runningExecs 미등록).
	// 따라서 agent heartbeat 기반 stale 판정은 bulk 에 카테고리 오류 — 정상 실행 중인 Job(부모·sub)을
	// 2분 후 stale 로 오판해 error 확정하게 된다. bulk Job 의 생명주기는 K8s 가 관리하고 완료는 콜백으로 반영된다.
	//
	// realtime+native(streaming Deployment) 실행은 여기서 별도 제외하지 않는다 — Type 은 realtime 이라
	// 위 batch 필터에 안 걸리지만, agent reconcile 이 타이밍상 stale 오판을 앞질러 막기 때문이다:
	// streaming pod 도 agent 와 독립적으로 돌아(agent 재시작 시 heartbeat 에서 빠짐) 원리상 batch 와 같은
	// 카테고리지만, agent 부팅 reconcile(+~5s)이 running execution 을 재조회해 runningExecs 에 re-adopt →
	// 다음 heartbeat 부터 다시 live 로 보고된다. 이 재적재는 staleGrace(2분)보다 24배 빨라, stale 감지기가
	// grace 경과를 볼 시점엔 이미 live 라 오판이 성립하지 않는다. (reconcile 이 2분 넘게 실패하는 극단에서만
	// 오판 가능 — 그 경우 실행 자체가 이미 문제이므로 error 확정이 오히려 맞다.) 확실한 제외가 필요해지면
	// WorkflowExecution 에 Delegated 플래그를 두어 batch/streaming 을 함께 거르는 방식으로 강화할 수 있다.
	cutoff := time.Now().Add(-s.staleGrace)
	var execs []models.WorkflowExecution
	if err := s.db.Where("status = ? AND started_at < ? AND workflow_id NOT IN (?)",
		string(types.PipelineGroupStatusRunning), cutoff,
		s.db.Model(&models.Workflow{}).Select("id").Where("type = ?", string(types.WorkflowTypeBatch)),
	).Find(&execs).Error; err != nil {
		slog.Error("stale-check: query failed", "error", err)
		return
	}

	now := time.Now()
	for i := range execs {
		exec := &execs[i]
		if !isStaleExecution(exec.ID, liveExecs) {
			continue // 담당 에이전트가 살아있고 실행 중 → 정상
		}
		// stale: 담당 에이전트 없음 → failed 전이 (조용한 유실 방지)
		exec.Status = string(types.PipelineGroupStatusError)
		exec.CompletedAt = &now
		exec.ErrorMessage = "execution orphaned: owning agent is no longer running it (agent crash or claim expiry)"
		if err := s.db.Save(exec).Error; err != nil {
			slog.Error("stale-check: failed to mark execution failed", "execution_id", exec.ID, "workflow_id", exec.WorkflowID, "error", err)
			continue
		}

		// 파티션 분산: orphan 된 것이 sub-execution 이면, 워크플로우를 직접 풀지 않고
		// 부모 취합을 진행한다(그 sub 를 실패로 카운트). 안 그러면 부모 CompletedSubExecutions 가
		// 영원히 모자라 부모가 미완료로 갇히고, 형제 sub 가 아직 실행 중인데 워크플로우가 조기 idle 된다.
		if exec.ParentExecutionID != "" {
			s.advanceParentOnStaleSub(exec.ParentExecutionID)
			slog.Warn("marked orphaned sub-execution as failed", "execution_id", exec.ID, "parent_execution_id", exec.ParentExecutionID)
			continue
		}

		// 단일 execution: 죽었으면 워크플로우도 running 에서 풀어준다.
		// 안 풀면 workflow.status="running"에 갇혀 재트리거가 영구히 막힌다.
		if err := s.db.Model(&models.Workflow{}).
			Where("id = ? AND status = ?", exec.WorkflowID, string(types.PipelineGroupStatusRunning)).
			Update("status", string(types.PipelineGroupStatusIdle)).Error; err != nil {
			slog.Error("stale-check: failed to reset workflow status", "workflow_id", exec.WorkflowID, "execution_id", exec.ID, "error", err)
		}
		slog.Warn("marked orphaned execution as failed", "execution_id", exec.ID, "workflow_id", exec.WorkflowID)
	}
}

// isStaleExecution은 execution이 살아있는 에이전트 집합에 없으면 stale로 판정한다.
// (순수 함수 — DB 없이 판정 로직을 테스트할 수 있도록 분리)
func isStaleExecution(executionID string, liveExecs map[string]struct{}) bool {
	_, live := liveExecs[executionID]
	return !live
}

// advanceParentOnStaleSub은 stale(orphan)로 실패 처리된 sub-execution 의 부모 취합을 진행한다.
// 부모 CompletedSubExecutions 를 원자 증분하고 실패 표시하며, 모든 sub 가 끝났으면 부모·워크플로우를 확정한다.
// (handler 의 aggregateSubExecutionResult 와 동일 정합성 규칙 — scheduler 경로용 최소 구현.)
func (s *SchedulerService) advanceParentOnStaleSub(parentID string) {
	now := time.Now()
	// 원자 증분 + 실패 표시(부분 실패).
	if err := s.db.Model(&models.WorkflowExecution{}).Where("id = ?", parentID).
		Updates(map[string]any{
			"completed_sub_executions": gorm.Expr("completed_sub_executions + ?", 1),
			"error_message":            "one or more sub-executions orphaned (agent crash)",
		}).Error; err != nil {
		slog.Error("stale-check: failed to advance parent", "parent_execution_id", parentID, "error", err)
		return
	}

	var parent models.WorkflowExecution
	if err := s.db.First(&parent, "id = ?", parentID).Error; err != nil {
		return
	}
	// == 로 완료 판정(DB 직렬화 증분 → 정확히 하나가 total 에 도달). 아직이면 대기.
	if parent.CompletedSubExecutions != parent.TotalSubExecutions {
		return
	}
	// 모든 sub 완료 → 부모·워크플로우 확정(하나라도 실패면 error).
	finalStatus := string(types.PipelineGroupStatusCompleted)
	wfStatus := string(types.PipelineGroupStatusIdle)
	if parent.ErrorMessage != "" {
		finalStatus = string(types.PipelineGroupStatusError)
		wfStatus = string(types.PipelineGroupStatusError)
	}
	s.db.Model(&models.WorkflowExecution{}).Where("id = ?", parentID).
		Updates(map[string]any{"status": finalStatus, "completed_at": now})
	s.db.Model(&models.Workflow{}).Where("id = ?", parent.WorkflowID).Update("status", wfStatus)
	slog.Warn("parent execution finalized after stale sub", "parent_execution_id", parentID, "status", finalStatus)
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

	slog.Info("Scheduler stopped")
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
			slog.Error("Failed to add schedule for workflow", "workflow_id", workflow.ID, "error", err)
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
			slog.Warn("Invalid timezone, using UTC", "timezone", workflow.ScheduleTimezone, "workflow_id", workflow.ID)
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

	slog.Info("Added schedule for workflow", "workflow_id", workflow.ID, "cron", cronExpr, "next_run", nextRun.In(loc).Format(time.RFC3339))
	return nil
}

// RemoveSchedule 스케줄 제거
func (s *SchedulerService) RemoveSchedule(workflowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[workflowID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, workflowID)
		slog.Info("Removed schedule for workflow", "workflow_id", workflowID)
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

	// 실행 대상 cluster 확정: 즉시 실행(StartWorkflow)과 동일한 정책.
	// 이 값을 발행 채널(cluster:<id>:execute)과 execution 스냅샷에 사용한다.
	clusterID, err := ResolveExecutionCluster(s.db.DB, workflow.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("resolve execution cluster: %w", err)
	}

	// 실행 레코드 생성
	execution := &models.WorkflowExecution{
		ID:            uuid.New().String(),
		WorkflowID:    workflow.ID,
		ClusterID:     clusterID,
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
		slog.Warn("Failed to publish execution command", "workflow_id", workflow.ID, "execution_id", execution.ID, "error", err)
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
		ID:              uuid.New().String(),
		WorkflowID:      workflow.ID,
		ExecutionID:     execution.ID,
		TargetClusterID: execution.ClusterID, // 확정 cluster로 라우팅(누락 시 broadcast로 새어 agent 미수신)
		TriggeredBy:     triggeredBy,
		UserID:          userID,
		WorkflowConfig:  workflowConfig,
		Timestamp:       time.Now(),
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
	slog.Info("Executing scheduled job", "workflow_id", j.workflowID)

	// 최신 워크플로우 정보 조회
	var workflow models.Workflow
	if err := j.scheduler.db.First(&workflow, "id = ?", j.workflowID).Error; err != nil {
		slog.Error("Failed to load workflow", "workflow_id", j.workflowID, "error", err)
		return
	}

	// 스케줄 비활성화 확인
	if !workflow.ScheduleEnabled {
		slog.Info("Schedule disabled, skipping", "workflow_id", j.workflowID)
		j.scheduler.RemoveSchedule(j.workflowID)
		return
	}

	// 이미 실행 중인지 확인
	if workflow.Status == "running" {
		slog.Info("Workflow already running, skipping", "workflow_id", j.workflowID)
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
		slog.Error("Failed to create execution record", "workflow_id", j.workflowID, "execution_id", execution.ID, "error", err)
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
		slog.Warn("Failed to publish execution command", "workflow_id", j.workflowID, "execution_id", execution.ID, "error", err)
	}

	slog.Info("Scheduled execution started", "workflow_id", j.workflowID, "execution_id", execution.ID)
}
