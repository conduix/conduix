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

	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// WorkflowHandler 워크플로우 API 핸들러
type WorkflowHandler struct {
	db           *database.DB
	kafkaService *services.KafkaService
	logger       *slog.Logger
}

// NewWorkflowHandler 핸들러 생성
func NewWorkflowHandler(db *database.DB) *WorkflowHandler {
	logger := slog.Default()
	return &WorkflowHandler{
		db:           db,
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
	var req CreateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Project 존재 확인
	var project models.Project
	if err := h.db.First(&project, "id = ?", req.ProjectID).Error; err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   "Project not found",
		})
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
		fmt.Printf("[WorkflowHandler] CreateWorkflow error: %+v\n", err)
		fmt.Printf("[WorkflowHandler] Workflow data: id=%s, project_id=%s, name=%s, type=%s\n",
			workflow.ID, workflow.ProjectID, workflow.Name, workflow.Type)
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to create workflow: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, types.APIResponse[models.Workflow]{
		Success: true,
		Data:    *workflow,
	})
}

// ListWorkflows GET /api/v1/workflows
func (h *WorkflowHandler) ListWorkflows(c *gin.Context) {
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
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch workflows",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.Workflow]{
		Success: true,
		Data:    workflows,
	})
}

// GetWorkflow GET /api/v1/workflows/:id
func (h *WorkflowHandler) GetWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.Preload("Project").First(&workflow, "id = ?", workflowID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
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

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data:    response,
	})
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
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	// 실행 중인 워크플로우는 수정 불가
	if workflow.Status == string(types.PipelineGroupStatusRunning) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Cannot update running workflow",
		})
		return
	}

	var req UpdateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
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
			h.logger.Error("Failed to manage Kafka topics", "workflow_id", workflowID, "error", err)
			c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
				Success: false,
				Error:   fmt.Sprintf("Failed to manage Kafka topics: %v", err),
			})
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
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to update workflow",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.Workflow]{
		Success: true,
		Data:    workflow,
	})
}

// DeleteWorkflow DELETE /api/v1/workflows/:id
func (h *WorkflowHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	// 실행 중인 워크플로우는 삭제 불가
	if workflow.Status == string(types.PipelineGroupStatusRunning) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Cannot delete running workflow",
		})
		return
	}

	// Soft delete
	if err := h.db.Delete(&workflow).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to delete workflow",
		})
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

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	// 이미 실행 중인 경우
	if workflow.Status == string(types.PipelineGroupStatusRunning) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow is already running",
		})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	// 실행 기록 생성
	execution := &models.WorkflowExecution{
		ID:            uuid.New().String(),
		WorkflowID:    workflowID,
		Status:        string(types.PipelineGroupStatusRunning),
		StartedAt:     time.Now(),
		TriggeredBy:   "user",
		TriggeredByID: userIDStr,
		CreatedAt:     time.Now(),
	}

	if err := h.db.Create(execution).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to create execution record",
		})
		return
	}

	// 워크플로우 상태 업데이트
	workflow.Status = string(types.PipelineGroupStatusRunning)
	workflow.LastRunAt = &execution.StartedAt
	h.db.Save(&workflow)

	// TODO: 실제 실행은 에이전트에 위임 (Redis Pub/Sub 또는 gRPC)
	// 여기서는 실행 기록만 생성

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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	// 실행 중이 아닌 경우
	if workflow.Status != string(types.PipelineGroupStatusRunning) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow is not running",
		})
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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	if workflow.Status != string(types.PipelineGroupStatusRunning) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow is not running",
		})
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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	if workflow.Status != string(types.PipelineGroupStatusPaused) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow is not paused",
		})
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
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch executions",
		})
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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Execution not found",
		})
		return
	}

	// 파이프라인 결과 파싱
	response := map[string]any{
		"id":             execution.ID,
		"workflow_id":    execution.WorkflowID,
		"status":         execution.Status,
		"started_at":     execution.StartedAt,
		"completed_at":   execution.CompletedAt,
		"duration_ms":    execution.DurationMs,
		"total_records":  execution.TotalRecords,
		"failed_records": execution.FailedRecords,
		"error_message":  execution.ErrorMessage,
		"triggered_by":   execution.TriggeredBy,
	}

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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	var newPipeline types.GroupedPipeline
	if err := c.ShouldBindJSON(&newPipeline); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
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
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to add pipeline",
		})
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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
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
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Pipeline not found in workflow",
		})
		return
	}

	// 저장
	pipelinesJSON, _ := json.Marshal(newPipelines)
	workflow.PipelinesConfig = string(pipelinesJSON)
	workflow.UpdatedAt = time.Now()

	if err := h.db.Save(&workflow).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to remove pipeline",
		})
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
			h.addKafkaSinkToParent(parentPipeline, p.Name, topicName)

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
				h.removeKafkaSinkFromParent(newParent, oldP.Name)
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

// addKafkaSinkToParent 부모 파이프라인에 Kafka sink 추가
func (h *WorkflowHandler) addKafkaSinkToParent(parent *types.GroupedPipeline, childName, topicName string) {
	sinkName := fmt.Sprintf("kafka_to_%s", childName)

	// 이미 존재하는지 확인
	for _, sink := range parent.Sinks {
		if sink.Name == sinkName {
			return
		}
	}

	kafkaSink := types.WorkflowSink{
		Type: "kafka",
		Name: sinkName,
		Config: map[string]any{
			"brokers": h.kafkaService.GetBrokers(),
			"topic":   topicName,
		},
	}

	parent.Sinks = append(parent.Sinks, kafkaSink)
}

// removeKafkaSinkFromParent 부모 파이프라인에서 Kafka sink 제거
func (h *WorkflowHandler) removeKafkaSinkFromParent(parent *types.GroupedPipeline, childName string) {
	sinkName := fmt.Sprintf("kafka_to_%s", childName)

	newSinks := make([]types.WorkflowSink, 0, len(parent.Sinks))
	for _, sink := range parent.Sinks {
		if sink.Name != sinkName {
			newSinks = append(newSinks, sink)
		}
	}
	parent.Sinks = newSinks
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
