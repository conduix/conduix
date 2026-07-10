package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/builder"
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
	runnerBuilder   *builder.RunnerBuilder
	logger          *slog.Logger
}

// NewPluginHandler 플러그인 핸들러 생성
func NewPluginHandler(db *database.DB) *PluginHandler {
	return &PluginHandler{
		db:              db,
		revisionService: services.NewRevisionService(db.DB),
		runnerBuilder:   builder.NewRunnerBuilder(db.DB, nil),
		logger:          slog.Default(),
	}
}

// CreatePluginRequest 플러그인 등록 요청
type CreatePluginRequest struct {
	Name        string `json:"name" binding:"required"`
	Version     string `json:"version,omitempty"`
	Image       string `json:"image,omitempty"`
	Description string `json:"description,omitempty"`
	SourceRepo  string `json:"source_repo,omitempty"`
}

// UpdatePluginRequest 플러그인 수정 요청
type UpdatePluginRequest struct {
	Version     string `json:"version,omitempty"`
	Image       string `json:"image,omitempty"`
	Description string `json:"description,omitempty"`
	SourceRepo  string `json:"source_repo,omitempty"`
	Status      string `json:"status,omitempty"` // active, inactive, deprecated
	SourceCode  string `json:"source_code,omitempty"`
	GoMod       string `json:"go_mod,omitempty"`
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
	query := h.db.Model(&models.Plugin{})

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

	middleware.SuccessResponse(c, plugins)
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
	if err := h.db.First(&plugin, "name = ?", pluginName).Error; err != nil {
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

	if err := h.db.Create(plugin).Error; err != nil {
		h.logger.Error("Failed to create plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create plugin")
		return
	}

	// Revision 생성 (native plugin인 경우)
	if plugin.Type == "native" && plugin.SourceCode != "" {
		h.createRevision(plugin.ID, plugin.Name, "create", plugin.SourceCode, plugin.GoMod, plugin.SourceHash, "", "", userIDStr)
	}

	h.logger.Info("Plugin created", "request_id", requestID, "plugin_id", plugin.ID, "plugin_name", plugin.Name)
	c.JSON(http.StatusCreated, types.APIResponse[models.Plugin]{
		Success: true,
		Data:    *plugin,
	})
}

// updateExistingPlugin 기존 플러그인 업데이트 (Upsert)
func (h *PluginHandler) updateExistingPlugin(c *gin.Context, existing *models.Plugin, req *CreatePluginRequest) {
	requestID := middleware.GetRequestID(c)

	existing.Version = req.Version
	existing.Image = req.Image
	existing.Description = req.Description
	existing.SourceRepo = req.SourceRepo
	existing.UpdatedAt = time.Now()

	if err := h.db.Save(existing).Error; err != nil {
		h.logger.Error("Failed to update plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update plugin")
		return
	}

	h.logger.Info("Plugin updated", "request_id", requestID, "plugin_id", existing.ID, "plugin_name", existing.Name)
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
	if req.SourceCode != "" {
		// D5: import 검증 — 허용 모듈(+표준+conduix 내부) 밖 외부 import 는 거부.
		if err := validateStageImports(h.db, req.SourceCode); err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
			return
		}
		plugin.SourceCode = req.SourceCode
		plugin.Type = "native"
		hash := sha256.Sum256([]byte(req.SourceCode))
		plugin.SourceHash = fmt.Sprintf("%x", hash)
	}
	if req.GoMod != "" {
		plugin.GoMod = req.GoMod
	}

	plugin.UpdatedAt = time.Now()

	if err := h.db.Save(&plugin).Error; err != nil {
		h.logger.Error("Failed to update plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update plugin")
		return
	}

	// 소스가 변경된 경우 revision 생성 + Runner auto-build 트리거
	userID := c.GetString("user_id")
	sourceChanged := plugin.Type == "native" && plugin.SourceHash != "" && plugin.SourceHash != oldSourceHash
	if sourceChanged {
		h.createRevision(plugin.ID, plugin.Name, "update", plugin.SourceCode, plugin.GoMod, plugin.SourceHash, oldSource, "", userID)

		// 비동기 Runner auto-build 트리거
		latestSeq, _ := h.revisionService.GetLatestSeq()
		go func() {
			h.logger.Info("Auto-build triggered by source change",
				"plugin_name", plugin.Name,
				"old_hash", oldSourceHash,
				"new_hash", plugin.SourceHash,
			)
			result, err := h.runnerBuilder.Build(context.Background(), userID)
			if err != nil {
				versionID := ""
				if result != nil {
					versionID = result.VersionID
				}
				h.logger.Error("Auto-build failed", "error", err, "version_id", versionID)
				return
			}
			// trigger를 "auto"로 기록
			h.db.Model(&models.RunnerVersion{}).Where("id = ?", result.VersionID).Updates(map[string]any{
				"trigger":      "auto",
				"revision_seq": latestSeq,
			})
			h.logger.Info("Auto-build completed",
				"version_id", result.VersionID,
				"status", result.Status,
				"duration", result.Duration,
			)
		}()
	}

	h.logger.Info("Plugin updated", "request_id", requestID, "plugin_id", plugin.ID, "auto_build", sourceChanged)
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

	// Plugin soft delete
	if err := h.db.Delete(&plugin).Error; err != nil {
		h.logger.Error("Failed to delete plugin", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete plugin")
		return
	}

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
	PluginName string         `json:"plugin_name,omitempty"` // 기존 플러그인이면 테스트 결과 DB 기록
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

	// 테스트 결과 DB 기록
	h.recordTestResult(req.PluginName, resp.Success, resp.Error)

	middleware.SuccessResponse(c, resp)
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

// TestNativePluginRequest Native Plugin 테스트 요청
type TestNativePluginRequest struct {
	SourceCode string           `json:"source_code" binding:"required"`
	GoMod      string           `json:"go_mod,omitempty"`
	Config     map[string]any   `json:"config,omitempty"`
	SampleData []map[string]any `json:"sample_data" binding:"required"`
	PluginName string           `json:"plugin_name,omitempty"` // 기존 플러그인이면 테스트 결과 DB 기록
}

// TestNativePluginResponse Native Plugin 테스트 결과
type TestNativePluginResponse struct {
	Success       bool                 `json:"success"`
	SecurityCheck *SecurityCheckResult `json:"security_check"`
	BuildOutput   string               `json:"build_output,omitempty"`
	BuildError    string               `json:"build_error,omitempty"`
	BuildElapsed  string               `json:"build_elapsed,omitempty"`
	ExecOutput    []map[string]any     `json:"exec_output,omitempty"`
	ExecError     string               `json:"exec_error,omitempty"`
	ExecElapsed   string               `json:"exec_elapsed,omitempty"`
}

// TestNativePlugin POST /api/v1/plugins/test-native
// @Summary Native Stage 소스 코드 빌드 및 테스트 실행
// @Tags plugins
// @Accept json
// @Produce json
// @Param request body TestNativePluginRequest true "Native Plugin Test Request"
// @Success 200 {object} types.APIResponse[TestNativePluginResponse]
// @Router /plugins/test-native [post]
func (h *PluginHandler) TestNativePlugin(c *gin.Context) {
	var req TestNativePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	resp := TestNativePluginResponse{}

	// 1. 보안 검사 (go/ast)
	secResult := CheckSourceSecurity(req.SourceCode)
	resp.SecurityCheck = secResult
	if !secResult.Passed {
		resp.Success = false
		middleware.SuccessResponse(c, resp)
		return
	}

	// 2. 임시 디렉토리에 소스 작성
	tmpDir, err := os.MkdirTemp("", "conduix-test-*")
	if err != nil {
		h.logger.Error("Failed to create temp dir", "error", err)
		resp.Success = false
		resp.BuildError = "Failed to create temp directory"
		middleware.SuccessResponse(c, resp)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// main.go 작성
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(req.SourceCode), 0o600); err != nil {
		resp.Success = false
		resp.BuildError = "Failed to write source file"
		middleware.SuccessResponse(c, resp)
		return
	}

	// go.mod 작성
	goModContent := req.GoMod
	if goModContent == "" {
		goModContent = `module conduix-plugin-test

go 1.26

require github.com/conduix/conduix/plugin-sdk v0.0.0

replace github.com/conduix/conduix/plugin-sdk => github.com/conduix/conduix/plugin-sdk v0.0.0
`
	}
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0o600); err != nil {
		resp.Success = false
		resp.BuildError = "Failed to write go.mod"
		middleware.SuccessResponse(c, resp)
		return
	}

	// 3. go build (타임아웃 60초)
	buildCtx, buildCancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer buildCancel()

	buildStart := time.Now()
	binPath := filepath.Join(tmpDir, "plugin-test")
	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	buildOutput, buildErr := buildCmd.CombinedOutput()
	resp.BuildElapsed = time.Since(buildStart).String()
	resp.BuildOutput = string(buildOutput)

	if buildErr != nil {
		resp.Success = false
		resp.BuildError = fmt.Sprintf("Build failed: %v\n%s", buildErr, buildOutput)
		middleware.SuccessResponse(c, resp)
		return
	}

	// 4. 바이너리 실행 + sample_data 전달 (타임아웃 10초)
	// 표준 입력으로 JSON 전달, 표준 출력으로 결과 수집
	execCtx, execCancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer execCancel()

	sampleJSON, _ := json.Marshal(map[string]any{
		"config":      req.Config,
		"sample_data": req.SampleData,
	})

	execStart := time.Now()
	execCmd := exec.CommandContext(execCtx, binPath)
	execCmd.Dir = tmpDir
	execCmd.Stdin = strings.NewReader(string(sampleJSON))

	execOutput, execErr := execCmd.CombinedOutput()
	resp.ExecElapsed = time.Since(execStart).String()

	if execErr != nil {
		resp.Success = false
		resp.ExecError = fmt.Sprintf("Execution failed: %v\n%s", execErr, execOutput)
		middleware.SuccessResponse(c, resp)
		return
	}

	// 실행 결과 파싱
	var execResult struct {
		Records []map[string]any `json:"records"`
		Error   string           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(execOutput, &execResult); err != nil {
		resp.Success = false
		resp.ExecError = fmt.Sprintf("Failed to parse output: %v\nRaw output: %s", err, execOutput)
		middleware.SuccessResponse(c, resp)
		return
	}

	if execResult.Error != "" {
		resp.Success = false
		resp.ExecError = execResult.Error
	} else {
		resp.Success = true
		resp.ExecOutput = execResult.Records
	}

	// 테스트 결과 DB 기록 (plugin_name이 있으면)
	h.recordTestResult(req.PluginName, resp.Success, resp.ExecError+resp.BuildError)

	middleware.SuccessResponse(c, resp)
}

// recordTestResult 테스트 결과를 DB에 기록 (plugin_name이 있을 때만)
func (h *PluginHandler) recordTestResult(pluginName string, success bool, errMsg string) {
	if pluginName == "" {
		return
	}
	now := time.Now()
	updates := map[string]any{
		"last_test_passed": success,
		"last_test_at":     now,
	}
	if success {
		updates["last_test_error"] = ""
	} else if errMsg != "" {
		updates["last_test_error"] = errMsg
	}
	if err := h.db.Model(&models.Plugin{}).Where("name = ?", pluginName).Updates(updates).Error; err != nil {
		h.logger.Error("Failed to record test result", "plugin_name", pluginName, "error", err)
	}
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
