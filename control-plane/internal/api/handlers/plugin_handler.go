package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/pipeline-core/pkg/stream"
	"github.com/conduix/conduix/shared/types"
)

// PluginHandler 플러그인 API 핸들러
type PluginHandler struct {
	db              *database.DB
	revisionService *services.RevisionService
	logger          *slog.Logger
}

// NewPluginHandler 플러그인 핸들러 생성
func NewPluginHandler(db *database.DB) *PluginHandler {
	return &PluginHandler{
		db:              db,
		revisionService: services.NewRevisionService(db.DB),
		logger:          slog.Default(),
	}
}

// CreatePluginRequest 플러그인 등록 요청
type CreatePluginRequest struct {
	Name        string               `json:"name" binding:"required"`
	Version     string               `json:"version,omitempty"`
	Image       string               `json:"image,omitempty"`
	Description string               `json:"description,omitempty"`
	SourceRepo  string               `json:"source_repo,omitempty"`
	Stages      []CreateStageRequest `json:"stages,omitempty"`
}

// CreateStageRequest Stage 등록 요청
type CreateStageRequest struct {
	StageType    string `json:"type" binding:"required"`
	Category     string `json:"category,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	Description  string `json:"description,omitempty"`
	ConfigSchema any    `json:"config_schema" binding:"required"` // JSON Schema
	UISchema     any    `json:"ui_schema,omitempty"`              // UI Schema for react-jsonschema-form
	Icon         string `json:"icon,omitempty"`
	Color        string `json:"color,omitempty"`
}

// UpdatePluginRequest 플러그인 수정 요청
type UpdatePluginRequest struct {
	Version     string `json:"version,omitempty"`
	Image       string `json:"image,omitempty"`
	Description string `json:"description,omitempty"`
	SourceRepo  string `json:"source_repo,omitempty"`
	Status      string `json:"status,omitempty"` // active, inactive, deprecated
}

// PluginResponse 플러그인 응답 (Stage 수 포함)
type PluginResponse struct {
	models.Plugin
	StageCount int `json:"stage_count"`
}

// ListPlugins GET /api/v1/plugins
// @Summary 플러그인 목록 조회
// @Tags plugins
// @Accept json
// @Produce json
// @Param status query string false "Status filter (active, inactive, deprecated)"
// @Param search query string false "Search by name"
// @Success 200 {object} types.APIResponse[[]PluginResponse]
// @Router /plugins [get]
func (h *PluginHandler) ListPlugins(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var plugins []models.Plugin
	query := h.db.Model(&models.Plugin{}).Preload("Stages")

	// 필터링
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

	if err := query.Find(&plugins).Error; err != nil {
		h.logger.Error("Failed to fetch plugins", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch plugins")
		return
	}

	// 응답 생성
	responses := make([]PluginResponse, 0, len(plugins))
	for _, plugin := range plugins {
		responses = append(responses, PluginResponse{
			Plugin:     plugin,
			StageCount: len(plugin.Stages),
		})
	}

	middleware.SuccessResponse(c, responses)
}

// GetPlugin GET /api/v1/plugins/:name
// @Summary 플러그인 상세 조회
// @Tags plugins
// @Accept json
// @Produce json
// @Param name path string true "Plugin Name"
// @Success 200 {object} types.APIResponse[models.Plugin]
// @Router /plugins/{name} [get]
func (h *PluginHandler) GetPlugin(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.Preload("Stages").First(&plugin, "name = ?", pluginName).Error; err != nil {
		h.logger.Warn("Plugin not found", "request_id", requestID, "plugin_name", pluginName)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	middleware.SuccessResponse(c, plugin)
}

// CreatePlugin POST /api/v1/plugins
// @Summary 플러그인 등록 (Helm hook에서 호출)
// @Tags plugins
// @Accept json
// @Produce json
// @Param request body CreatePluginRequest true "Plugin Info"
// @Success 201 {object} types.APIResponse[models.Plugin]
// @Router /plugins [post]
func (h *PluginHandler) CreatePlugin(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req CreatePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// 이름 중복 확인
	var existing models.Plugin
	if err := h.db.First(&existing, "name = ?", req.Name).Error; err == nil {
		// 이미 존재하면 업데이트 (Upsert 동작)
		h.updateExistingPlugin(c, &existing, &req)
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	// 트랜잭션으로 Plugin과 Stages 생성
	tx := h.db.Begin()

	version := req.Version
	if version == "" {
		version = "v0.0.0"
	}

	plugin := &models.Plugin{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Version:     version,
		Image:       req.Image,
		Description: req.Description,
		SourceRepo:  req.SourceRepo,
		Status:      "active",
		CreatedBy:   userIDStr,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := tx.Create(plugin).Error; err != nil {
		tx.Rollback()
		h.logger.Error("Failed to create plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create plugin")
		return
	}

	// Stages 생성
	for _, stageReq := range req.Stages {
		configSchemaJSON, _ := json.Marshal(stageReq.ConfigSchema)
		uiSchemaJSON, _ := json.Marshal(stageReq.UISchema)

		stage := &models.PluginStage{
			ID:           uuid.New().String(),
			PluginID:     plugin.ID,
			StageType:    stageReq.StageType,
			Category:     stageReq.Category,
			DisplayName:  stageReq.DisplayName,
			Description:  stageReq.Description,
			ConfigSchema: string(configSchemaJSON),
			UISchema:     string(uiSchemaJSON),
			Icon:         stageReq.Icon,
			Color:        stageReq.Color,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		// StageType 중복 확인
		var existingStage models.PluginStage
		if err := tx.First(&existingStage, "stage_type = ?", stageReq.StageType).Error; err == nil {
			tx.Rollback()
			h.logger.Warn("Stage type already exists", "request_id", requestID, "stage_type", stageReq.StageType)
			middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeDuplicateResource,
				"Stage type '"+stageReq.StageType+"' already registered by another plugin")
			return
		}

		if err := tx.Create(stage).Error; err != nil {
			tx.Rollback()
			h.logger.Error("Failed to create plugin stage", "request_id", requestID, "error", err)
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create plugin stage")
			return
		}
	}

	tx.Commit()

	// Stages와 함께 다시 조회
	h.db.Preload("Stages").First(&plugin, "id = ?", plugin.ID)

	// Revision 생성 (native plugin인 경우)
	if plugin.Type == "native" && plugin.SourceCode != "" {
		h.createRevision(plugin.ID, plugin.Name, "create", plugin.SourceCode, plugin.GoMod, plugin.SourceHash, "", "", userIDStr)
	}

	h.logger.Info("Plugin created", "request_id", requestID, "plugin_id", plugin.ID, "plugin_name", plugin.Name, "stage_count", len(req.Stages))
	c.JSON(http.StatusCreated, types.APIResponse[models.Plugin]{
		Success: true,
		Data:    *plugin,
	})
}

// updateExistingPlugin 기존 플러그인 업데이트 (Upsert)
func (h *PluginHandler) updateExistingPlugin(c *gin.Context, existing *models.Plugin, req *CreatePluginRequest) {
	requestID := middleware.GetRequestID(c)

	tx := h.db.Begin()

	// 플러그인 업데이트
	existing.Version = req.Version
	existing.Image = req.Image
	existing.Description = req.Description
	existing.SourceRepo = req.SourceRepo
	existing.UpdatedAt = time.Now()

	if err := tx.Save(existing).Error; err != nil {
		tx.Rollback()
		h.logger.Error("Failed to update plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update plugin")
		return
	}

	// 기존 Stages 삭제 후 재생성
	if err := tx.Where("plugin_id = ?", existing.ID).Delete(&models.PluginStage{}).Error; err != nil {
		tx.Rollback()
		h.logger.Error("Failed to delete existing stages", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update plugin stages")
		return
	}

	// 새 Stages 생성
	for _, stageReq := range req.Stages {
		configSchemaJSON, _ := json.Marshal(stageReq.ConfigSchema)
		uiSchemaJSON, _ := json.Marshal(stageReq.UISchema)

		stage := &models.PluginStage{
			ID:           uuid.New().String(),
			PluginID:     existing.ID,
			StageType:    stageReq.StageType,
			Category:     stageReq.Category,
			DisplayName:  stageReq.DisplayName,
			Description:  stageReq.Description,
			ConfigSchema: string(configSchemaJSON),
			UISchema:     string(uiSchemaJSON),
			Icon:         stageReq.Icon,
			Color:        stageReq.Color,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		if err := tx.Create(stage).Error; err != nil {
			tx.Rollback()
			h.logger.Error("Failed to create plugin stage", "request_id", requestID, "error", err)
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create plugin stage")
			return
		}
	}

	tx.Commit()

	// Stages와 함께 다시 조회
	h.db.Preload("Stages").First(existing, "id = ?", existing.ID)

	h.logger.Info("Plugin updated", "request_id", requestID, "plugin_id", existing.ID, "plugin_name", existing.Name, "stage_count", len(req.Stages))
	c.JSON(http.StatusOK, types.APIResponse[models.Plugin]{
		Success: true,
		Data:    *existing,
		Message: "Plugin updated successfully",
	})
}

// UpdatePlugin PUT /api/v1/plugins/:name
// @Summary 플러그인 수정
// @Tags plugins
// @Accept json
// @Produce json
// @Param name path string true "Plugin Name"
// @Param request body UpdatePluginRequest true "Plugin Info"
// @Success 200 {object} types.APIResponse[models.Plugin]
// @Router /plugins/{name} [put]
func (h *PluginHandler) UpdatePlugin(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		h.logger.Warn("Plugin not found", "request_id", requestID, "plugin_name", pluginName)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	// revision용 이전 소스 저장
	oldSource := plugin.SourceCode
	oldSourceHash := plugin.SourceHash

	var req UpdatePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	if req.Version != "" {
		plugin.Version = req.Version
	}
	if req.Image != "" {
		plugin.Image = req.Image
	}
	if req.Description != "" {
		plugin.Description = req.Description
	}
	if req.SourceRepo != "" {
		plugin.SourceRepo = req.SourceRepo
	}
	if req.Status != "" {
		plugin.Status = req.Status
	}

	plugin.UpdatedAt = time.Now()

	if err := h.db.Save(&plugin).Error; err != nil {
		h.logger.Error("Failed to update plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update plugin")
		return
	}

	// Stages와 함께 다시 조회
	h.db.Preload("Stages").First(&plugin, "id = ?", plugin.ID)

	// 소스가 변경된 경우 revision 생성
	userID := c.GetString("user_id")
	if plugin.Type == "native" && plugin.SourceHash != "" && plugin.SourceHash != oldSourceHash {
		h.createRevision(plugin.ID, plugin.Name, "update", plugin.SourceCode, plugin.GoMod, plugin.SourceHash, oldSource, "", userID)
	}

	h.logger.Info("Plugin updated", "request_id", requestID, "plugin_id", plugin.ID)
	middleware.SuccessResponse(c, plugin)
}

// DeletePlugin DELETE /api/v1/plugins/:name
// @Summary 플러그인 삭제
// @Tags plugins
// @Accept json
// @Produce json
// @Param name path string true "Plugin Name"
// @Success 200 {object} types.APIResponse[any]
// @Router /plugins/{name} [delete]
func (h *PluginHandler) DeletePlugin(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		h.logger.Warn("Plugin not found", "request_id", requestID, "plugin_name", pluginName)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	// 트랜잭션으로 Plugin과 Stages 삭제
	tx := h.db.Begin()

	// Stages 먼저 삭제
	if err := tx.Where("plugin_id = ?", plugin.ID).Delete(&models.PluginStage{}).Error; err != nil {
		tx.Rollback()
		h.logger.Error("Failed to delete plugin stages", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete plugin stages")
		return
	}

	// Plugin soft delete
	if err := tx.Delete(&plugin).Error; err != nil {
		tx.Rollback()
		h.logger.Error("Failed to delete plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete plugin")
		return
	}

	tx.Commit()

	// 삭제 revision 생성 (native plugin인 경우)
	userID := c.GetString("user_id")
	if plugin.Type == "native" {
		h.createRevision(plugin.ID, plugin.Name, "delete", "", "", plugin.SourceHash, plugin.SourceCode, "", userID)
	}

	h.logger.Info("Plugin deleted", "request_id", requestID, "plugin_id", plugin.ID, "plugin_name", pluginName)
	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Plugin deleted successfully",
	})
}

// TestScriptRequest Script 테스트 요청
type TestScriptRequest struct {
	Code       string         `json:"code" binding:"required"`
	Timeout    string         `json:"timeout,omitempty"`
	SampleData map[string]any `json:"sample_data" binding:"required"`
}

// TestScriptResponse Script 테스트 결과
type TestScriptResponse struct {
	Success bool           `json:"success"`
	Output  map[string]any `json:"output,omitempty"`
	Dropped bool           `json:"dropped"`
	Error   string         `json:"error,omitempty"`
	Elapsed string         `json:"elapsed"`
}

// TestScript POST /api/v1/plugins/test-script
// @Summary JavaScript 스크립트 테스트 실행
// @Tags plugins
// @Accept json
// @Produce json
// @Param request body TestScriptRequest true "Script Test Request"
// @Success 200 {object} types.APIResponse[TestScriptResponse]
// @Router /plugins/test-script [post]
func (h *PluginHandler) TestScript(c *gin.Context) {
	var req TestScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// JSScriptStage 생성 (컴파일 검증 포함)
	config := map[string]any{"code": req.Code}
	if req.Timeout != "" {
		config["timeout"] = req.Timeout
	}

	stage, err := stream.NewJSScriptStage("test", config)
	if err != nil {
		middleware.SuccessResponse(c, TestScriptResponse{
			Success: false,
			Error:   err.Error(),
			Elapsed: "0s",
		})
		return
	}

	// 샘플 데이터로 실행
	record := &stream.Record{
		Data: req.SampleData,
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	result, err := stage.Process(ctx, record)
	elapsed := time.Since(start)

	resp := TestScriptResponse{
		Elapsed: elapsed.String(),
	}

	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else if result == nil {
		resp.Success = true
		resp.Dropped = true
	} else {
		resp.Success = true
		resp.Output = result.Data
	}

	middleware.SuccessResponse(c, resp)
}

// GetPluginStages GET /api/v1/plugins/:name/stages
// @Summary 플러그인의 Stage 목록 조회
// @Tags plugins
// @Accept json
// @Produce json
// @Param name path string true "Plugin Name"
// @Success 200 {object} types.APIResponse[[]models.PluginStage]
// @Router /plugins/{name}/stages [get]
func (h *PluginHandler) GetPluginStages(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		h.logger.Warn("Plugin not found", "request_id", requestID, "plugin_name", pluginName)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	var stages []models.PluginStage
	if err := h.db.Where("plugin_id = ?", plugin.ID).Find(&stages).Error; err != nil {
		h.logger.Error("Failed to fetch plugin stages", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch plugin stages")
		return
	}

	middleware.SuccessResponse(c, stages)
}

// ListRevisions GET /api/v1/plugins/:name/revisions — plugin revision 히스토리 조회
func (h *PluginHandler) ListRevisions(c *gin.Context) {
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	revisions, err := h.revisionService.ListRevisions(plugin.ID, 50)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch revisions")
		return
	}

	middleware.SuccessResponse(c, revisions)
}

// GetRevision GET /api/v1/plugins/revisions/:revisionId — revision 상세 조회 (소스 해제)
func (h *PluginHandler) GetRevision(c *gin.Context) {
	revisionID := c.Param("revisionId")

	revision, sourceCode, goMod, err := h.revisionService.GetRevision(revisionID)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Revision not found")
		return
	}

	middleware.SuccessResponse(c, gin.H{
		"revision":    revision,
		"source_code": sourceCode,
		"go_mod":      goMod,
	})
}

// createRevision revision 생성 헬퍼 (실패해도 메인 플로우에 영향 없음)
func (h *PluginHandler) createRevision(pluginID, pluginName, action, sourceCode, goMod, sourceHash, oldSource, message, createdBy string) {
	_, err := h.revisionService.CreateRevision(&services.CreateRevisionParams{
		PluginID:   pluginID,
		PluginName: pluginName,
		Action:     action,
		SourceCode: sourceCode,
		GoMod:      goMod,
		SourceHash: sourceHash,
		OldSource:  oldSource,
		Message:    message,
		CreatedBy:  createdBy,
	})
	if err != nil {
		h.logger.Error("Failed to create revision", "plugin_id", pluginID, "action", action, "error", err)
	}
}
