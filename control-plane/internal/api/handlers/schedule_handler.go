package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ScheduleHandler 스케줄 API 핸들러
type ScheduleHandler struct {
	db               *database.DB
	schedulerService *services.SchedulerService
}

// NewScheduleHandler 새 스케줄 핸들러 생성
func NewScheduleHandler(db *database.DB, schedulerService *services.SchedulerService) *ScheduleHandler {
	return &ScheduleHandler{
		db:               db,
		schedulerService: schedulerService,
	}
}

// UpdateScheduleRequest 스케줄 업데이트 요청
type UpdateScheduleRequest struct {
	Type       string `json:"type"` // cron, interval, manual
	Cron       string `json:"cron,omitempty"`
	Interval   string `json:"interval,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	MaxRetries int    `json:"max_retries,omitempty"`
}

// ScheduleResponse 스케줄 응답
type ScheduleResponse struct {
	WorkflowID   string     `json:"workflow_id"`
	WorkflowName string     `json:"workflow_name"`
	Type         string     `json:"type"`
	Cron         string     `json:"cron,omitempty"`
	Interval     string     `json:"interval,omitempty"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	Status       string     `json:"status"`
}

// GetSchedule GET /api/v1/workflows/:id/schedule
// 워크플로우 스케줄 설정 조회
func (h *ScheduleHandler) GetSchedule(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 배치 워크플로우만 스케줄 지원
	if workflow.Type != "batch" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Only batch workflows support scheduling")
		return
	}

	response := ScheduleResponse{
		WorkflowID:   workflow.ID,
		WorkflowName: workflow.Name,
		Type:         workflow.ScheduleType,
		Cron:         workflow.ScheduleCron,
		Interval:     workflow.ScheduleInterval,
		Timezone:     workflow.ScheduleTimezone,
		Enabled:      workflow.ScheduleEnabled,
		LastRunAt:    workflow.LastRunAt,
		NextRunAt:    workflow.NextRunAt,
		Status:       workflow.Status,
	}

	if response.Timezone == "" {
		response.Timezone = "UTC"
	}

	c.JSON(http.StatusOK, types.APIResponse[ScheduleResponse]{
		Success: true,
		Data:    response,
	})
}

// UpdateSchedule PUT /api/v1/workflows/:id/schedule
// 워크플로우 스케줄 설정 변경
func (h *ScheduleHandler) UpdateSchedule(c *gin.Context) {
	workflowID := c.Param("id")

	var req UpdateScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	// 배치 워크플로우만 스케줄 지원
	if workflow.Type != "batch" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Only batch workflows support scheduling")
		return
	}

	// 업데이트할 필드
	updates := map[string]any{
		"updated_at": time.Now(),
	}

	if req.Type != "" {
		updates["schedule_type"] = req.Type
	}
	if req.Cron != "" {
		updates["schedule_cron"] = req.Cron
	}
	if req.Interval != "" {
		updates["schedule_interval"] = req.Interval
	}
	if req.Timezone != "" {
		// 타임존 유효성 검사
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Invalid timezone")
			return
		}
		updates["schedule_timezone"] = req.Timezone
	}
	if req.Enabled != nil {
		updates["schedule_enabled"] = *req.Enabled
	}

	// DB 업데이트
	if err := h.db.Model(&workflow).Updates(updates).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update schedule")
		return
	}

	// 변경된 워크플로우 다시 조회
	h.db.First(&workflow, "id = ?", workflowID)

	// 스케줄러 서비스에 업데이트 알림
	if h.schedulerService != nil {
		if err := h.schedulerService.UpdateSchedule(&workflow); err != nil {
			// 경고만 표시, 에러 반환하지 않음
			c.Header("X-Schedule-Warning", err.Error())
		}
	}

	response := ScheduleResponse{
		WorkflowID:   workflow.ID,
		WorkflowName: workflow.Name,
		Type:         workflow.ScheduleType,
		Cron:         workflow.ScheduleCron,
		Interval:     workflow.ScheduleInterval,
		Timezone:     workflow.ScheduleTimezone,
		Enabled:      workflow.ScheduleEnabled,
		LastRunAt:    workflow.LastRunAt,
		NextRunAt:    workflow.NextRunAt,
		Status:       workflow.Status,
	}

	if response.Timezone == "" {
		response.Timezone = "UTC"
	}

	c.JSON(http.StatusOK, types.APIResponse[ScheduleResponse]{
		Success: true,
		Data:    response,
	})
}

// EnableSchedule POST /api/v1/workflows/:id/schedule/enable
// 스케줄 활성화
func (h *ScheduleHandler) EnableSchedule(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	if workflow.Type != "batch" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Only batch workflows support scheduling")
		return
	}

	if workflow.ScheduleCron == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "No cron expression configured")
		return
	}

	h.db.Model(&workflow).Update("schedule_enabled", true)

	// 스케줄러에 등록
	if h.schedulerService != nil {
		workflow.ScheduleEnabled = true
		_ = h.schedulerService.AddSchedule(&workflow)
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Data:    map[string]any{"enabled": true},
	})
}

// DisableSchedule POST /api/v1/workflows/:id/schedule/disable
// 스케줄 비활성화
func (h *ScheduleHandler) DisableSchedule(c *gin.Context) {
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	h.db.Model(&workflow).Update("schedule_enabled", false)

	// 스케줄러에서 제거
	if h.schedulerService != nil {
		h.schedulerService.RemoveSchedule(workflowID)
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Data:    map[string]any{"enabled": false},
	})
}

// TriggerNow POST /api/v1/workflows/:id/trigger
// 즉시 실행 (수동 트리거)
func (h *ScheduleHandler) TriggerNow(c *gin.Context) {
	workflowID := c.Param("id")

	// 사용자 ID
	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	if h.schedulerService == nil {
		middleware.ErrorResponseWithCode(c, http.StatusServiceUnavailable, types.ErrCodeExternalService, "Scheduler service is not available")
		return
	}

	execution, err := h.schedulerService.TriggerNow(workflowID, userIDStr)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[*models.WorkflowExecution]{
		Success: true,
		Data:    execution,
	})
}

// ListSchedules GET /api/v1/schedules
// 전체 스케줄 목록 조회
func (h *ScheduleHandler) ListSchedules(c *gin.Context) {
	// 배치 워크플로우 중 스케줄이 설정된 것들 조회
	var workflows []models.Workflow
	query := h.db.Where("type = ?", "batch")

	// 필터: enabled only
	if c.Query("enabled") == "true" {
		query = query.Where("schedule_enabled = ?", true)
	}

	// project 필터
	if projectID := c.Query("project_id"); projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}

	if err := query.Find(&workflows).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch schedules")
		return
	}

	responses := make([]ScheduleResponse, 0, len(workflows))
	for _, workflow := range workflows {
		response := ScheduleResponse{
			WorkflowID:   workflow.ID,
			WorkflowName: workflow.Name,
			Type:         workflow.ScheduleType,
			Cron:         workflow.ScheduleCron,
			Interval:     workflow.ScheduleInterval,
			Timezone:     workflow.ScheduleTimezone,
			Enabled:      workflow.ScheduleEnabled,
			LastRunAt:    workflow.LastRunAt,
			NextRunAt:    workflow.NextRunAt,
			Status:       workflow.Status,
		}
		if response.Timezone == "" {
			response.Timezone = "UTC"
		}
		responses = append(responses, response)
	}

	c.JSON(http.StatusOK, types.APIResponse[[]ScheduleResponse]{
		Success: true,
		Data:    responses,
	})
}
