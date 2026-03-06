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
	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// PluginHandler 플러그인 API 핸들러
type PluginHandler struct {
	db      *database.DB
	builder *builder.Builder
	logger  *slog.Logger
}

// NewPluginHandler 플러그인 핸들러 생성
func NewPluginHandler(db *database.DB) *PluginHandler {
	return &PluginHandler{
		db:      db,
		builder: builder.New(builder.DefaultConfig()),
		logger:  slog.Default(),
	}
}

// CreatePluginRequest 플러그인 등록 요청 (Helm hook에서 호출)
type CreatePluginRequest struct {
	Name        string               `json:"name" binding:"required"`
	Version     string               `json:"version" binding:"required"`
	Image       string               `json:"image" binding:"required"`
	Description string               `json:"description,omitempty"`
	SourceRepo  string               `json:"source_repo,omitempty"`
	Stages      []CreateStageRequest `json:"stages" binding:"required,dive"`
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

	plugin := &models.Plugin{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Version:     req.Version,
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

	h.logger.Info("Plugin deleted", "request_id", requestID, "plugin_id", plugin.ID, "plugin_name", pluginName)
	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Plugin deleted successfully",
	})
}

// BuildPluginRequest 플러그인 빌드 요청
type BuildPluginRequest struct {
	Name       string `json:"name" binding:"required"`
	Version    string `json:"version" binding:"required"`
	SourceCode string `json:"source_code" binding:"required"` // main.go 소스코드
	GoMod      string `json:"go_mod,omitempty"`               // go.mod (optional)
	Platform   string `json:"platform,omitempty"`             // GOOS/GOARCH (default: linux/arm64)
}

// BuildPlugin POST /api/v1/plugins/build
// @Summary 플러그인 빌드 (소스코드 → 바이너리)
// @Tags plugins
// @Accept json
// @Produce json
// @Param request body BuildPluginRequest true "Build Request"
// @Success 200 {object} types.APIResponse[models.PluginBuild]
// @Router /plugins/build [post]
func (h *PluginHandler) BuildPlugin(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req BuildPluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid build request", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// 소스코드 검증
	if _, err := h.builder.ValidateSource(req.SourceCode); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Source validation failed: "+err.Error())
		return
	}

	// Plugin 조회 또는 생성
	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", req.Name).Error; err != nil {
		// 존재하지 않으면 생성
		userID, _ := c.Get("user_id")
		userIDStr := ""
		if userID != nil {
			userIDStr = userID.(string)
		}

		plugin = models.Plugin{
			ID:        uuid.New().String(),
			Name:      req.Name,
			Version:   req.Version,
			Status:    "active",
			CreatedBy: userIDStr,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := h.db.Create(&plugin).Error; err != nil {
			h.logger.Error("Failed to create plugin", "request_id", requestID, "error", err)
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create plugin")
			return
		}
	}

	// 빌드 레코드 생성
	now := time.Now()
	build := &models.PluginBuild{
		ID:         uuid.New().String(),
		PluginID:   plugin.ID,
		Status:     "building",
		SourceCode: req.SourceCode,
		GoMod:      req.GoMod,
		Version:    req.Version,
		Platform:   req.Platform,
		StartedAt:  &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if build.Platform == "" {
		build.Platform = "linux/arm64"
	}

	if err := h.db.Create(build).Error; err != nil {
		h.logger.Error("Failed to create build record", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create build record")
		return
	}

	// 비동기 빌드 실행
	go h.executeBuild(build, &plugin, &req)

	h.logger.Info("Plugin build started", "request_id", requestID, "build_id", build.ID, "plugin", req.Name)
	c.JSON(http.StatusAccepted, types.APIResponse[models.PluginBuild]{
		Success: true,
		Data:    *build,
		Message: "Build started",
	})
}

// executeBuild 비동기 빌드 실행
func (h *PluginHandler) executeBuild(build *models.PluginBuild, plugin *models.Plugin, req *BuildPluginRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := h.builder.Build(ctx, &builder.BuildRequest{
		PluginID:   plugin.ID,
		PluginName: req.Name,
		Version:    req.Version,
		SourceCode: req.SourceCode,
		GoMod:      req.GoMod,
		Platform:   req.Platform,
	})

	now := time.Now()
	build.FinishedAt = &now
	build.UpdatedAt = now

	if err != nil {
		build.Status = "failed"
		build.Error = err.Error()
		if result != nil {
			build.BuildLog = result.BuildLog
			build.DurationMs = int(result.Duration.Milliseconds())
		}
		h.db.Save(build)
		h.logger.Error("Plugin build failed", "build_id", build.ID, "error", err)
		return
	}

	build.Status = "success"
	build.BuildLog = result.BuildLog
	build.DurationMs = int(result.Duration.Milliseconds())
	h.db.Save(build)

	// 바이너리 저장
	binary := &models.PluginBinary{
		ID:         uuid.New().String(),
		PluginID:   plugin.ID,
		Version:    req.Version,
		Platform:   build.Platform,
		BinaryData: result.Binary,
		Checksum:   result.Checksum,
		SizeBytes:  result.Size,
		BuildID:    build.ID,
		CreatedAt:  now,
	}

	if err := h.db.Create(binary).Error; err != nil {
		h.logger.Error("Failed to save binary", "build_id", build.ID, "error", err)
		return
	}

	// Plugin 버전 업데이트
	plugin.Version = req.Version
	plugin.UpdatedAt = now
	h.db.Save(plugin)

	h.logger.Info("Plugin build completed",
		"build_id", build.ID,
		"plugin", plugin.Name,
		"version", req.Version,
		"size_bytes", result.Size,
		"duration_ms", build.DurationMs,
	)
}

// GetBuild GET /api/v1/plugins/builds/:id
// @Summary 빌드 상태 조회
// @Tags plugins
// @Produce json
// @Param id path string true "Build ID"
// @Success 200 {object} types.APIResponse[models.PluginBuild]
// @Router /plugins/builds/{id} [get]
func (h *PluginHandler) GetBuild(c *gin.Context) {
	buildID := c.Param("id")

	var build models.PluginBuild
	if err := h.db.First(&build, "id = ?", buildID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Build not found")
		return
	}

	middleware.SuccessResponse(c, build)
}

// ListBuilds GET /api/v1/plugins/:name/builds
// @Summary 플러그인의 빌드 이력 조회
// @Tags plugins
// @Produce json
// @Param name path string true "Plugin Name"
// @Success 200 {object} types.APIResponse[[]models.PluginBuild]
// @Router /plugins/{name}/builds [get]
func (h *PluginHandler) ListBuilds(c *gin.Context) {
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	var builds []models.PluginBuild
	if err := h.db.Where("plugin_id = ?", plugin.ID).Order("created_at DESC").Find(&builds).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch builds")
		return
	}

	middleware.SuccessResponse(c, builds)
}

// GetBinary GET /api/v1/plugins/:name/binary
// @Summary 플러그인 바이너리 다운로드
// @Tags plugins
// @Produce octet-stream
// @Param name path string true "Plugin Name"
// @Param version query string false "Version (default: latest)"
// @Param platform query string false "Platform (default: linux/arm64)"
// @Success 200 {file} binary
// @Router /plugins/{name}/binary [get]
func (h *PluginHandler) GetBinary(c *gin.Context) {
	pluginName := c.Param("name")

	var plugin models.Plugin
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Plugin not found")
		return
	}

	version := c.DefaultQuery("version", plugin.Version)
	platform := c.DefaultQuery("platform", "linux/arm64")

	var binary models.PluginBinary
	if err := h.db.Where("plugin_id = ? AND version = ? AND platform = ?", plugin.ID, version, platform).
		First(&binary).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Binary not found for version/platform")
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+pluginName+"-"+version+".bin")
	c.Header("X-Checksum-SHA256", binary.Checksum)
	c.Data(http.StatusOK, "application/octet-stream", binary.BinaryData)
}

// ValidateSourceRequest 소스코드 검증 요청
type ValidateSourceRequest struct {
	SourceCode string `json:"source_code" binding:"required"`
}

// ValidatePluginSource POST /api/v1/plugins/validate
// @Summary 플러그인 소스코드 검증 (빌드 없이)
// @Tags plugins
// @Accept json
// @Produce json
// @Param request body ValidateSourceRequest true "Source Code"
// @Success 200 {object} types.APIResponse[map[string]any]
// @Router /plugins/validate [post]
func (h *PluginHandler) ValidatePluginSource(c *gin.Context) {
	var req ValidateSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	imports, err := h.builder.ValidateSource(req.SourceCode)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	middleware.SuccessResponse(c, map[string]any{
		"valid":   true,
		"imports": imports,
	})
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
