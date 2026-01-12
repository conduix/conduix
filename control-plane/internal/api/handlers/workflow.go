package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// WorkflowHandler 워크플로우 API 핸들러
type WorkflowHandler struct {
	db           *database.DB
	redisService *services.RedisService
	kafkaService *services.KafkaService
	logger       *slog.Logger
}

// NewWorkflowHandler 핸들러 생성
func NewWorkflowHandler(db *database.DB, redisService *services.RedisService) *WorkflowHandler {
	logger := slog.Default()
	return &WorkflowHandler{
		db:           db,
		redisService: redisService,
		kafkaService: services.NewKafkaService(&services.KafkaServiceConfig{Logger: logger}),
		logger:       logger,
	}
}

// CreateWorkflowRequest 워크플로우 생성 요청
type CreateWorkflowRequest struct {
	ProjectID     string                  `json:"project_id" binding:"required"` // 프로젝트 FK (필수)
	Name          string                  `json:"name" binding:"required"`
	Slug          string                  `json:"slug,omitempty"` // URL 경로명
	Description   string                  `json:"description,omitempty"`
	Type          types.PipelineGroupType `json:"type" binding:"required"`
	ExecutionMode types.ExecutionMode     `json:"execution_mode,omitempty"`
	Schedule      *types.ScheduleConfig   `json:"schedule,omitempty"`
	Pipelines     []types.GroupedPipeline `json:"pipelines,omitempty"` // 빈 배열 허용
	FailurePolicy *types.FailurePolicy    `json:"failure_policy,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
	Tags          []string                `json:"tags,omitempty"`
}

// CreateWorkflow POST /api/v1/workflows
func (h *WorkflowHandler) CreateWorkflow(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req CreateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// Project 존재 확인
	var project models.Project
	if err := h.db.First(&project, "id = ?", req.ProjectID).Error; err != nil {
		h.logger.Warn("Project not found", "request_id", requestID, "project_id", req.ProjectID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Project not found")
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	// 기본값 설정
	executionMode := req.ExecutionMode
	if executionMode == "" {
		executionMode = types.ExecutionModeParallel
	}

	// 파이프라인에 ID 할당 (있는 경우)
	if req.Pipelines == nil {
		req.Pipelines = []types.GroupedPipeline{}
	}
	for i := range req.Pipelines {
		if req.Pipelines[i].ID == "" {
			req.Pipelines[i].ID = uuid.New().String()
		}
	}

	// JSON 직렬화
	pipelinesJSON, _ := json.Marshal(req.Pipelines)
	failurePolicyJSON, _ := json.Marshal(req.FailurePolicy)
	metadataJSON, _ := json.Marshal(req.Metadata)
	tagsJSON, _ := json.Marshal(req.Tags)

	workflow := &models.Workflow{
		ID:              uuid.New().String(),
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		Slug:            req.Slug,
		Description:     req.Description,
		Type:            string(req.Type),
		ExecutionMode:   string(executionMode),
		Status:          string(types.PipelineGroupStatusIdle),
		PipelinesConfig: string(pipelinesJSON),
		FailurePolicy:   string(failurePolicyJSON),
		Metadata:        string(metadataJSON),
		Tags:            string(tagsJSON),
		CreatedBy:       userIDStr,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// 스케줄 설정
	if req.Schedule != nil {
		workflow.ScheduleType = string(req.Schedule.Type)
		workflow.ScheduleCron = req.Schedule.Cron
		workflow.ScheduleInterval = req.Schedule.Interval
		workflow.ScheduleTimezone = req.Schedule.Timezone
		workflow.ScheduleEnabled = req.Schedule.Enabled
	}

	if err := h.db.Create(workflow).Error; err != nil {
		h.logger.Error("Failed to create workflow",
			"request_id", requestID,
			"workflow_id", workflow.ID,
			"project_id", workflow.ProjectID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create workflow")
		return
	}

	h.logger.Info("Workflow created", "request_id", requestID, "workflow_id", workflow.ID)
	middleware.SuccessResponse(c, *workflow)
	c.Status(http.StatusCreated)
}

// ListWorkflows GET /api/v1/workflows
func (h *WorkflowHandler) ListWorkflows(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	var workflows []models.Workflow

	query := h.db.Model(&models.Workflow{})

	// 필터링
	if projectID := c.Query("project_id"); projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if workflowType := c.Query("type"); workflowType != "" {
		query = query.Where("type = ?", workflowType)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if search := c.Query("search"); search != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// 정렬
	orderBy := c.DefaultQuery("order_by", "created_at")
	orderDir := c.DefaultQuery("order_dir", "desc")
	query = query.Order(orderBy + " " + orderDir)

	if err := query.Find(&workflows).Error; err != nil {
		h.logger.Error("Failed to fetch workflows", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch workflows")
		return
	}

	middleware.SuccessResponse(c, workflows)
}

// GetWorkflow GET /api/v1/workflows/:id
func (h *WorkflowHandler) GetWorkflow(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.Preload("Project").First(&workflow, "id = ?", workflowID).Error; err != nil {
		h.logger.Warn("Workflow not found", "request_id", requestID, "workflow_id", workflowID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 파이프라인 설정을 파싱하여 포함
	response := map[string]any{
		"id":               workflow.ID,
		"project_id":       workflow.ProjectID,
		"project":          workflow.Project,
		"name":             workflow.Name,
		"slug":             workflow.Slug,
		"description":      workflow.Description,
		"type":             workflow.Type,
		"execution_mode":   workflow.ExecutionMode,
		"status":           workflow.Status,
		"schedule_enabled": workflow.ScheduleEnabled,
		"pipelines_config": workflow.PipelinesConfig,
		"created_at":       workflow.CreatedAt,
		"updated_at":       workflow.UpdatedAt,
	}

	// 파이프라인 설정 파싱
	var pipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines)
	}
	response["pipelines"] = pipelines

	// 스케줄 설정
	if workflow.ScheduleType != "" {
		response["schedule"] = map[string]any{
			"type":     workflow.ScheduleType,
			"cron":     workflow.ScheduleCron,
			"interval": workflow.ScheduleInterval,
			"timezone": workflow.ScheduleTimezone,
			"enabled":  workflow.ScheduleEnabled,
		}
	}

	// 실패 정책
	if workflow.FailurePolicy != "" {
		var failurePolicy types.FailurePolicy
		_ = json.Unmarshal([]byte(workflow.FailurePolicy), &failurePolicy)
		response["failure_policy"] = failurePolicy
	}

	middleware.SuccessResponse(c, response)
}

// UpdateWorkflowRequest 워크플로우 수정 요청
type UpdateWorkflowRequest struct {
	Name          string                  `json:"name,omitempty"`
	Description   string                  `json:"description,omitempty"`
	ExecutionMode types.ExecutionMode     `json:"execution_mode,omitempty"`
	Schedule      *types.ScheduleConfig   `json:"schedule,omitempty"`
	Pipelines     []types.GroupedPipeline `json:"pipelines,omitempty"`
	FailurePolicy *types.FailurePolicy    `json:"failure_policy,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
	Tags          []string                `json:"tags,omitempty"`
}

// UpdateWorkflow PUT /api/v1/workflows/:id
func (h *WorkflowHandler) UpdateWorkflow(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		h.logger.Warn("Workflow not found", "request_id", requestID, "workflow_id", workflowID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 실행 중인 워크플로우는 수정 불가
	if workflow.Status == string(types.PipelineGroupStatusRunning) {
		h.logger.Warn("Cannot update running workflow", "request_id", requestID, "workflow_id", workflowID)
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeWorkflowRunning, "Cannot update running workflow")
		return
	}

	var req UpdateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// 기존 파이프라인 로드 (Kafka 토픽 관리용)
	var oldPipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &oldPipelines)
	}

	// 업데이트
	if req.Name != "" {
		workflow.Name = req.Name
	}
	if req.Description != "" {
		workflow.Description = req.Description
	}
	if req.ExecutionMode != "" {
		workflow.ExecutionMode = string(req.ExecutionMode)
	}

	// 파이프라인 업데이트 시 Kafka 토픽 관리
	if req.Pipelines != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		// 부모-자식 관계 변경에 따른 Kafka 토픽 관리
		updatedPipelines, err := h.managePipelineKafkaTopics(ctx, workflow.Slug, oldPipelines, req.Pipelines)
		if err != nil {
			h.logger.Error("Failed to manage Kafka topics", "request_id", requestID, "workflow_id", workflowID, "error", err)
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeExternalService, "Failed to manage Kafka topics")
			return
		}

		pipelinesJSON, _ := json.Marshal(updatedPipelines)
		workflow.PipelinesConfig = string(pipelinesJSON)
	}

	if req.FailurePolicy != nil {
		failurePolicyJSON, _ := json.Marshal(req.FailurePolicy)
		workflow.FailurePolicy = string(failurePolicyJSON)
	}
	if req.Schedule != nil {
		workflow.ScheduleType = string(req.Schedule.Type)
		workflow.ScheduleCron = req.Schedule.Cron
		workflow.ScheduleInterval = req.Schedule.Interval
		workflow.ScheduleTimezone = req.Schedule.Timezone
		workflow.ScheduleEnabled = req.Schedule.Enabled
	}
	if req.Metadata != nil {
		metadataJSON, _ := json.Marshal(req.Metadata)
		workflow.Metadata = string(metadataJSON)
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		workflow.Tags = string(tagsJSON)
	}

	workflow.UpdatedAt = time.Now()

	if err := h.db.Save(&workflow).Error; err != nil {
		h.logger.Error("Failed to update workflow", "request_id", requestID, "workflow_id", workflowID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update workflow")
		return
	}

	h.logger.Info("Workflow updated", "request_id", requestID, "workflow_id", workflowID)
	middleware.SuccessResponse(c, workflow)
}

// DeleteWorkflow DELETE /api/v1/workflows/:id
func (h *WorkflowHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 실행 중인 워크플로우는 삭제 불가
	if workflow.Status == string(types.PipelineGroupStatusRunning) {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeWorkflowRunning, "Cannot delete running workflow")
		return
	}

	// Soft delete
	if err := h.db.Delete(&workflow).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete workflow")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Workflow deleted successfully",
	})
}

// StartWorkflow POST /api/v1/workflows/:id/start
func (h *WorkflowHandler) StartWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	var workflow models.Workflow
	var execution *models.WorkflowExecution

	// 트랜잭션으로 동시성 제어 (SELECT FOR UPDATE)
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE로 행 잠금 획득
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&workflow, "id = ?", workflowID).Error; err != nil {
			return err
		}

		// 이미 실행 중인 경우 에러 반환
		if workflow.Status == string(types.PipelineGroupStatusRunning) {
			return fmt.Errorf("WORKFLOW_RUNNING")
		}

		// 이전에 running 상태로 남아있는 실행 기록 정리 (비정상 종료된 실행)
		now := time.Now()
		tx.Model(&models.WorkflowExecution{}).
			Where("workflow_id = ? AND status = ?", workflowID, string(types.PipelineGroupStatusRunning)).
			Updates(map[string]any{
				"status":        string(types.PipelineGroupStatusStopped),
				"completed_at":  now,
				"error_message": "Terminated: new execution started",
			})

		// 실행 기록 생성 (파이프라인 설정 스냅샷 포함)
		execution = &models.WorkflowExecution{
			ID:                uuid.New().String(),
			WorkflowID:        workflowID,
			Status:            string(types.PipelineGroupStatusRunning),
			StartedAt:         time.Now(),
			PipelinesSnapshot: workflow.PipelinesConfig, // 실행 시점 파이프라인 설정 저장
			TriggeredBy:       "user",
			TriggeredByID:     userIDStr,
			CreatedAt:         time.Now(),
		}

		if err := tx.Create(execution).Error; err != nil {
			return err
		}

		// 워크플로우 상태 업데이트
		workflow.Status = string(types.PipelineGroupStatusRunning)
		workflow.LastRunAt = &execution.StartedAt
		return tx.Save(&workflow).Error
	})

	if err != nil {
		if err.Error() == "WORKFLOW_RUNNING" {
			middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeWorkflowRunning, "Workflow is already running")
			return
		}
		if err == gorm.ErrRecordNotFound {
			middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
			return
		}
		h.logger.Error("Failed to start workflow", "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to start workflow")
		return
	}

	fmt.Printf("[StartWorkflow] Workflow status updated to running: %s\n", workflowID)

	// 파이프라인 설정 파싱
	var pipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		if err := json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines); err != nil {
			fmt.Printf("[StartWorkflow] Failed to parse pipelines config: %v\n", err)
		}
	}
	fmt.Printf("[StartWorkflow] Parsed %d pipelines\n", len(pipelines))

	// Redis를 통해 에이전트에 실행 명령 전송
	cmd := &types.WorkflowExecutionCommand{
		ID:          uuid.New().String(),
		WorkflowID:  workflowID,
		ExecutionID: execution.ID,
		TriggeredBy: "user",
		UserID:      userIDStr,
		WorkflowConfig: &types.Workflow{
			ID:            workflow.ID,
			ProjectID:     workflow.ProjectID,
			Name:          workflow.Name,
			Type:          types.PipelineGroupType(workflow.Type),
			ExecutionMode: types.ExecutionMode(workflow.ExecutionMode),
			Pipelines:     pipelines,
		},
		Timestamp: time.Now(),
	}

	fmt.Printf("[StartWorkflow] Publishing workflow execution command to Redis...\n")
	if err := h.redisService.PublishWorkflowExecution(cmd); err != nil {
		fmt.Printf("[StartWorkflow] Failed to publish: %v\n", err)
	} else {
		fmt.Printf("[StartWorkflow] Successfully published workflow execution: %s\n", execution.ID)
	}

	c.JSON(http.StatusAccepted, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"execution_id": execution.ID,
			"workflow_id":  workflowID,
			"status":       execution.Status,
			"started_at":   execution.StartedAt,
		},
	})
}

// StopWorkflow POST /api/v1/workflows/:id/stop
func (h *WorkflowHandler) StopWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 실행 중이 아닌 경우
	if workflow.Status != string(types.PipelineGroupStatusRunning) {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeWorkflowNotRunning, "Workflow is not running")
		return
	}

	// 워크플로우 상태 업데이트
	workflow.Status = string(types.PipelineGroupStatusStopped)
	h.db.Save(&workflow)

	// 현재 실행 중인 execution 업데이트
	now := time.Now()
	h.db.Model(&models.WorkflowExecution{}).
		Where("workflow_id = ? AND status = ?", workflowID, string(types.PipelineGroupStatusRunning)).
		Updates(map[string]any{
			"status":       string(types.PipelineGroupStatusStopped),
			"completed_at": now,
		})

	// TODO: 에이전트에 중지 명령 전송

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Workflow stopped",
	})
}

// PauseWorkflow POST /api/v1/workflows/:id/pause
func (h *WorkflowHandler) PauseWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	if workflow.Status != string(types.PipelineGroupStatusRunning) {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeWorkflowNotRunning, "Workflow is not running")
		return
	}

	workflow.Status = string(types.PipelineGroupStatusPaused)
	h.db.Save(&workflow)

	// TODO: 에이전트에 일시정지 명령 전송

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Workflow paused",
	})
}

// ResumeWorkflow POST /api/v1/workflows/:id/resume
func (h *WorkflowHandler) ResumeWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	if workflow.Status != string(types.PipelineGroupStatusPaused) {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeInvalidState, "Workflow is not paused")
		return
	}

	workflow.Status = string(types.PipelineGroupStatusRunning)
	h.db.Save(&workflow)

	// TODO: 에이전트에 재개 명령 전송

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Workflow resumed",
	})
}

// GetWorkflowExecutions GET /api/v1/workflows/:id/executions
func (h *WorkflowHandler) GetWorkflowExecutions(c *gin.Context) {
	workflowID := c.Param("id")

	var executions []models.WorkflowExecution
	query := h.db.Where("workflow_id = ?", workflowID).Order("started_at DESC")

	// 상태 필터
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// Limit
	limit := 50
	query = query.Limit(limit)

	if err := query.Find(&executions).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch executions")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.WorkflowExecution]{
		Success: true,
		Data:    executions,
	})
}

// GetWorkflowExecution GET /api/v1/workflows/:id/executions/:execId
func (h *WorkflowHandler) GetWorkflowExecution(c *gin.Context) {
	workflowID := c.Param("id")
	execID := c.Param("execId")

	var execution models.WorkflowExecution
	if err := h.db.Where("id = ? AND workflow_id = ?", execID, workflowID).First(&execution).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Execution not found")
		return
	}

	// 파이프라인 결과 파싱
	response := map[string]any{
		"id":              execution.ID,
		"workflow_id":     execution.WorkflowID,
		"status":          execution.Status,
		"started_at":      execution.StartedAt,
		"completed_at":    execution.CompletedAt,
		"duration_ms":     execution.DurationMs,
		"total_records":   execution.TotalRecords,
		"failed_records":  execution.FailedRecords,
		"error_message":   execution.ErrorMessage,
		"triggered_by":    execution.TriggeredBy,
		"triggered_by_id": execution.TriggeredByID,
		"created_at":      execution.CreatedAt,
	}

	// 파이프라인 설정 스냅샷 파싱
	if execution.PipelinesSnapshot != "" {
		var pipelines []types.GroupedPipeline
		_ = json.Unmarshal([]byte(execution.PipelinesSnapshot), &pipelines)
		response["pipelines_snapshot"] = pipelines
	}

	// 파이프라인 실행 결과 파싱
	if execution.PipelineResults != "" {
		var results []types.PipelineExecutionResult
		_ = json.Unmarshal([]byte(execution.PipelineResults), &results)
		response["pipeline_results"] = results
	}

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data:    response,
	})
}

// AddPipelineToWorkflow POST /api/v1/workflows/:id/pipelines
func (h *WorkflowHandler) AddPipelineToWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	var newPipeline types.GroupedPipeline
	if err := c.ShouldBindJSON(&newPipeline); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// ID 할당
	if newPipeline.ID == "" {
		newPipeline.ID = uuid.New().String()
	}

	// 기존 파이프라인 로드
	var pipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines)
	}

	// 추가
	pipelines = append(pipelines, newPipeline)

	// 저장
	pipelinesJSON, _ := json.Marshal(pipelines)
	workflow.PipelinesConfig = string(pipelinesJSON)
	workflow.UpdatedAt = time.Now()

	if err := h.db.Save(&workflow).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to add pipeline")
		return
	}

	c.JSON(http.StatusCreated, types.APIResponse[types.GroupedPipeline]{
		Success: true,
		Data:    newPipeline,
	})
}

// RemovePipelineFromWorkflow DELETE /api/v1/workflows/:id/pipelines/:pipelineId
func (h *WorkflowHandler) RemovePipelineFromWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	pipelineID := c.Param("pipelineId")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 기존 파이프라인 로드
	var pipelines []types.GroupedPipeline
	if workflow.PipelinesConfig != "" {
		_ = json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines)
	}

	// 파이프라인 제거
	found := false
	newPipelines := make([]types.GroupedPipeline, 0)
	for _, p := range pipelines {
		if p.ID != pipelineID {
			newPipelines = append(newPipelines, p)
		} else {
			found = true
		}
	}

	if !found {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found in workflow")
		return
	}

	// 저장
	pipelinesJSON, _ := json.Marshal(newPipelines)
	workflow.PipelinesConfig = string(pipelinesJSON)
	workflow.UpdatedAt = time.Now()

	if err := h.db.Save(&workflow).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to remove pipeline")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline removed from workflow",
	})
}

// managePipelineKafkaTopics 부모-자식 파이프라인 간 Kafka 토픽 관리
// 새 자식 파이프라인에 Kafka 토픽 생성하고, 부모에 Kafka sink 추가
// 삭제된 자식 파이프라인의 Kafka 토픽 삭제하고, 부모에서 sink 제거
func (h *WorkflowHandler) managePipelineKafkaTopics(
	ctx context.Context,
	workflowSlug string,
	oldPipelines []types.GroupedPipeline,
	newPipelines []types.GroupedPipeline,
) ([]types.GroupedPipeline, error) {
	// 파이프라인 맵 생성
	oldPipelineMap := make(map[string]*types.GroupedPipeline)
	for i := range oldPipelines {
		oldPipelineMap[oldPipelines[i].ID] = &oldPipelines[i]
	}

	newPipelineMap := make(map[string]*types.GroupedPipeline)
	for i := range newPipelines {
		newPipelineMap[newPipelines[i].ID] = &newPipelines[i]
	}

	// 1. 새로 추가된 자식 파이프라인 찾기 (parent_pipeline_id가 새로 설정된 경우)
	for i := range newPipelines {
		p := &newPipelines[i]
		if p.ParentPipelineID == nil || *p.ParentPipelineID == "" {
			continue
		}

		// 이전에 부모가 없었거나, 부모가 변경된 경우
		oldP, existed := oldPipelineMap[p.ID]
		isNewChild := !existed || oldP.ParentPipelineID == nil || *oldP.ParentPipelineID != *p.ParentPipelineID

		if isNewChild {
			parentPipeline, ok := newPipelineMap[*p.ParentPipelineID]
			if !ok {
				h.logger.Warn("Parent pipeline not found", "child_id", p.ID, "parent_id", *p.ParentPipelineID)
				continue
			}

			// Kafka 토픽 생성
			topicName := h.kafkaService.GenerateTopicName(workflowSlug, parentPipeline.Name, p.Name)
			if err := h.kafkaService.CreateTopic(ctx, topicName); err != nil {
				h.logger.Error("Failed to create Kafka topic", "topic", topicName, "error", err)
				return nil, fmt.Errorf("failed to create Kafka topic '%s': %w", topicName, err)
			}

			// 부모 파이프라인에 Kafka sink 추가
			h.addKafkaStageToParent(parentPipeline, p.Name, topicName)

			// 자식 파이프라인에 Kafka source 설정
			h.setKafkaSourceToChild(p, topicName)

			h.logger.Info("Created Kafka topic for parent-child pipeline",
				"topic", topicName,
				"parent", parentPipeline.Name,
				"child", p.Name)
		}
	}

	// 2. 삭제된 자식 파이프라인 또는 부모 관계가 해제된 파이프라인 찾기
	for oldID, oldP := range oldPipelineMap {
		if oldP.ParentPipelineID == nil || *oldP.ParentPipelineID == "" {
			continue
		}

		newP, stillExists := newPipelineMap[oldID]
		// 파이프라인이 삭제되었거나, 부모 관계가 해제된 경우
		isRemoved := !stillExists || newP.ParentPipelineID == nil || *newP.ParentPipelineID == ""
		isParentChanged := stillExists && newP.ParentPipelineID != nil && *newP.ParentPipelineID != *oldP.ParentPipelineID

		if isRemoved || isParentChanged {
			oldParent, ok := oldPipelineMap[*oldP.ParentPipelineID]
			if !ok {
				continue
			}

			// 토픽 이름 생성
			topicName := h.kafkaService.GenerateTopicName(workflowSlug, oldParent.Name, oldP.Name)

			// Kafka 토픽 삭제 (비동기로 처리, 실패해도 계속 진행)
			go func(topic string) {
				deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := h.kafkaService.DeleteTopic(deleteCtx, topic); err != nil {
					h.logger.Warn("Failed to delete Kafka topic (will be cleaned up later)", "topic", topic, "error", err)
				}
			}(topicName)

			// 새 파이프라인 목록에서 부모 파이프라인의 sink 제거
			if newParent, ok := newPipelineMap[*oldP.ParentPipelineID]; ok {
				h.removeKafkaStageFromParent(newParent, oldP.Name)
			}

			h.logger.Info("Cleaned up Kafka topic for removed parent-child relationship",
				"topic", topicName,
				"old_parent", oldParent.Name,
				"old_child", oldP.Name)
		}
	}

	// 수정된 파이프라인 목록 반환
	result := make([]types.GroupedPipeline, 0, len(newPipelines))
	for _, p := range newPipelines {
		result = append(result, *newPipelineMap[p.ID])
	}
	return result, nil
}

// addKafkaStageToParent 부모 파이프라인에 Kafka sink stage 추가
func (h *WorkflowHandler) addKafkaStageToParent(parent *types.GroupedPipeline, childName, topicName string) {
	stageName := fmt.Sprintf("kafka_to_%s", childName)

	// 이미 존재하는지 확인
	for _, stage := range parent.Stages {
		if stage.Name == stageName {
			return
		}
	}

	kafkaStage := types.Stage{
		Type: "kafka",
		Name: stageName,
		Config: map[string]any{
			"brokers": h.kafkaService.GetBrokers(),
			"topic":   topicName,
		},
	}

	parent.Stages = append(parent.Stages, kafkaStage)
}

// removeKafkaStageFromParent 부모 파이프라인에서 Kafka sink stage 제거
func (h *WorkflowHandler) removeKafkaStageFromParent(parent *types.GroupedPipeline, childName string) {
	stageName := fmt.Sprintf("kafka_to_%s", childName)

	newStages := make([]types.Stage, 0, len(parent.Stages))
	for _, stage := range parent.Stages {
		if stage.Name != stageName {
			newStages = append(newStages, stage)
		}
	}
	parent.Stages = newStages
}

// setKafkaSourceToChild 자식 파이프라인에 Kafka source 설정
func (h *WorkflowHandler) setKafkaSourceToChild(child *types.GroupedPipeline, topicName string) {
	child.Source = types.WorkflowSource{
		Type: "kafka",
		Name: fmt.Sprintf("from_parent_%s", topicName),
		Config: map[string]any{
			"brokers":        h.kafkaService.GetBrokers(),
			"topic":          topicName,
			"consumer_group": fmt.Sprintf("%s_consumer", child.Name),
		},
	}
}

// ReceiveExecutionResult POST /api/v1/workflows/:id/executions/:executionId/result
// Agent에서 실행 결과를 받아 워크플로우 상태를 업데이트
func (h *WorkflowHandler) ReceiveExecutionResult(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("id")
	executionID := c.Param("executionId")

	var result types.GroupExecutionResult
	if err := c.ShouldBindJSON(&result); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":    false,
			"error":      gin.H{"code": "VALIDATION_ERROR", "message": err.Error()},
			"request_id": requestID,
		})
		return
	}

	fmt.Printf("[ReceiveExecutionResult] Received result for workflow=%s, execution=%s, status=%s, records=%d\n",
		workflowID, executionID, result.Status, result.TotalRecords)

	// 워크플로우 상태 업데이트
	var newStatus string
	switch result.Status {
	case types.PipelineGroupStatusCompleted:
		newStatus = "idle"
	case types.PipelineGroupStatusError:
		newStatus = "error"
	case types.PipelineGroupStatusStopped:
		newStatus = "stopped"
	default:
		newStatus = "idle"
	}

	// DB에서 워크플로우 업데이트 (status만)
	if err := h.db.Model(&models.Workflow{}).
		Where("id = ?", workflowID).
		Update("status", newStatus).Error; err != nil {
		fmt.Printf("[ReceiveExecutionResult] Failed to update workflow status: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":    false,
			"error":      gin.H{"code": "DB_ERROR", "message": "Failed to update workflow status"},
			"request_id": requestID,
		})
		return
	}

	// 파이프라인 결과 JSON 직렬화
	var pipelineResultsJSON string
	if len(result.PipelineResults) > 0 {
		pipelineResultsBytes, _ := json.Marshal(result.PipelineResults)
		pipelineResultsJSON = string(pipelineResultsBytes)
	}

	// 실행 기록 업데이트
	updates := map[string]any{
		"status":         string(result.Status),
		"completed_at":   result.CompletedAt,
		"total_records":  result.TotalRecords,
		"failed_records": result.FailedRecords,
		"error_message":  result.ErrorMessage,
	}
	if pipelineResultsJSON != "" {
		updates["pipeline_results"] = pipelineResultsJSON
	}

	if err := h.db.Model(&models.WorkflowExecution{}).
		Where("id = ?", executionID).
		Updates(updates).Error; err != nil {
		fmt.Printf("[ReceiveExecutionResult] Failed to update execution: %v\n", err)
	}

	fmt.Printf("[ReceiveExecutionResult] Workflow %s status updated to: %s\n", workflowID, newStatus)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Execution result received",
		"request_id": requestID,
	})
}

// GetExecutionMonitoring GET /api/v1/workflows/:id/executions/:executionId/monitoring
// 실행 중인 워크플로우의 실시간 모니터링 정보 조회 (Agent에 프록시)
func (h *WorkflowHandler) GetExecutionMonitoring(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("id")
	executionID := c.Param("executionId")

	// 실행 정보 조회
	var execution models.WorkflowExecution
	if err := h.db.Where("id = ? AND workflow_id = ?", executionID, workflowID).First(&execution).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success":    false,
			"error":      gin.H{"code": "NOT_FOUND", "message": "Execution not found"},
			"request_id": requestID,
		})
		return
	}

	// 실행 중인지 확인
	if execution.Status != "running" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success":    false,
			"error":      gin.H{"code": "NOT_RUNNING", "message": "Execution is not running"},
			"request_id": requestID,
		})
		return
	}

	// Redis에서 모니터링 정보 조회 시도 (빠른 응답)
	if h.redisService != nil {
		// Redis에서 모든 Agent의 하트비트 조회하여 실행 중인 Agent 찾기
		heartbeats, err := h.redisService.GetAllAgentHeartbeats()
		if err == nil {
			// 각 Agent의 모니터링 정보 확인
			for agentID := range heartbeats {
				monitoringInfo, err := h.redisService.GetExecutionMonitoring(agentID, executionID)
				if err == nil && monitoringInfo != nil {
					// Redis에서 찾았으면 즉시 반환
					c.JSON(http.StatusOK, gin.H{
						"success":    true,
						"data":       monitoringInfo,
						"request_id": requestID,
					})
					return
				}
			}
		}
	}

	// Redis에서 찾지 못한 경우 Agent HTTP 호출 (폴백)
	// Agent 정보 조회 (실행 중인 Agent 찾기)
	var agents []models.Agent
	if err := h.db.Where("status = ?", "online").Find(&agents).Error; err != nil || len(agents) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":    false,
			"error":      gin.H{"code": "NO_AGENT", "message": "No online agent available"},
			"request_id": requestID,
		})
		return
	}

	// 첫 번째 온라인 Agent에게 모니터링 정보 요청
	agent := agents[0]
	// Agent 포트는 기본값 8081 사용 (또는 Redis 하트비트에서 가져올 수 있음)
	agentHost := agent.IPAddress
	if agentHost == "" {
		agentHost = agent.Hostname
	}
	agentPort := 8081 // 기본 포트
	monitoringURL := fmt.Sprintf("http://%s:%d/api/v1/monitoring/%s", agentHost, agentPort, executionID)

	resp, err := http.Get(monitoringURL)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":    false,
			"error":      gin.H{"code": "AGENT_ERROR", "message": "Failed to connect to agent: " + err.Error()},
			"request_id": requestID,
		})
		return
	}
	defer resp.Body.Close()

	// Agent 응답 전달
	var agentResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agentResp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":    false,
			"error":      gin.H{"code": "PARSE_ERROR", "message": "Failed to parse agent response"},
			"request_id": requestID,
		})
		return
	}

	c.JSON(resp.StatusCode, agentResp)
}
