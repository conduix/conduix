package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ModuleHandler 커스텀 stage 가 import 할 수 있는 외부 모듈 레지스트리(allowed_modules) API.
// 의존성 버전을 module 당 하나로 고정해, 여러 stage 를 한 빌드에 합쳐도 충돌이 안 나게 한다.
type ModuleHandler struct {
	db     *database.DB
	logger *slog.Logger
	// goProxy 는 최신 버전 조회용 GOPROXY 베이스 URL(콤마 목록의 첫 http(s) 엔트리).
	goProxy    string
	httpClient *http.Client
}

// NewModuleHandler 모듈 핸들러 생성
func NewModuleHandler(db *database.DB) *ModuleHandler {
	return &ModuleHandler{
		db:         db,
		logger:     slog.Default(),
		goProxy:    "https://proxy.golang.org",
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateModuleRequest 모듈 등록 요청. 버전은 받지 않는다 — 플랫폼이 등록 시점 최신으로 고정(D3).
type CreateModuleRequest struct {
	ModulePath  string `json:"module_path" binding:"required"` // 예: github.com/google/uuid
	Description string `json:"description,omitempty"`
}

// UpdateModuleRequest 버전 갱신 요청. 빈 값이면 최신으로 재조회(D4 전역 버전업).
type UpdateModuleRequest struct {
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ListModules GET /api/v1/modules
func (h *ModuleHandler) ListModules(c *gin.Context) {
	var mods []models.AllowedModule
	if err := h.db.Order("module_path asc").Find(&mods).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to list modules")
		return
	}
	c.JSON(http.StatusOK, types.APIResponse[[]models.AllowedModule]{Success: true, Data: mods})
}

// CreateModule POST /api/v1/modules — 등록 시점 최신 버전으로 고정(D3).
func (h *ModuleHandler) CreateModule(c *gin.Context) {
	var req CreateModuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}
	modulePath := strings.TrimSpace(req.ModulePath)
	if modulePath == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "module_path is required")
		return
	}

	// 이미 등록됐으면 거부(버전은 module 당 하나 — 갱신은 PUT).
	var existing models.AllowedModule
	if err := h.db.First(&existing, "module_path = ?", modulePath).Error; err == nil {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeValidationFailed,
			fmt.Sprintf("module already registered: %s (version %s). use PUT to update.", modulePath, existing.Version))
		return
	}

	version, err := h.latestVersion(c, modulePath)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadGateway, types.ErrCodeInternalError,
			fmt.Sprintf("failed to resolve latest version for %s: %v", modulePath, err))
		return
	}

	userID, _ := c.Get("user_id")
	addedBy := ""
	if userID != nil {
		addedBy, _ = userID.(string)
	}

	mod := models.AllowedModule{
		ModulePath:  modulePath,
		Version:     version,
		Description: req.Description,
		AddedBy:     addedBy,
		Status:      "active",
	}
	if err := h.db.Create(&mod).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create module")
		return
	}
	h.logger.Info("allowed module registered", "module_path", modulePath, "version", version, "added_by", addedBy)
	c.JSON(http.StatusCreated, types.APIResponse[models.AllowedModule]{Success: true, Data: mod, Message: "module registered"})
}

// UpdateModule PUT /api/v1/modules/*module — 버전 갱신(D4). 버전 미지정 시 최신 재조회.
func (h *ModuleHandler) UpdateModule(c *gin.Context) {
	modulePath := strings.TrimPrefix(c.Param("module"), "/")
	var mod models.AllowedModule
	if err := h.db.First(&mod, "module_path = ?", modulePath).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "module not found")
		return
	}

	var req UpdateModuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	if req.Status != "" {
		mod.Status = req.Status
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		v, err := h.latestVersion(c, modulePath)
		if err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadGateway, types.ErrCodeInternalError,
				fmt.Sprintf("failed to resolve latest version: %v", err))
			return
		}
		version = v
	}
	mod.Version = version

	if err := h.db.Save(&mod).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update module")
		return
	}
	h.logger.Info("allowed module updated", "module_path", modulePath, "version", version)
	c.JSON(http.StatusOK, types.APIResponse[models.AllowedModule]{Success: true, Data: mod, Message: "module updated"})
}

// DeleteModule DELETE /api/v1/modules/*module
func (h *ModuleHandler) DeleteModule(c *gin.Context) {
	modulePath := strings.TrimPrefix(c.Param("module"), "/")
	if err := h.db.Where("module_path = ?", modulePath).Delete(&models.AllowedModule{}).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete module")
		return
	}
	c.JSON(http.StatusOK, types.APIResponse[any]{Success: true, Message: "module deleted"})
}

// latestVersion 은 GOPROXY 의 {module}/@latest 를 조회해 최신 버전 문자열을 반환한다.
// module path 는 GOPROXY 규약상 대문자를 !소문자로 인코딩해야 하나, 흔한 모듈은 소문자라
// 우선 그대로 질의하고 실패 시 에러를 그대로 노출한다(대문자 인코딩은 후속).
func (h *ModuleHandler) latestVersion(c *gin.Context, modulePath string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", strings.TrimRight(h.goProxy, "/"), modulePath)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("goproxy %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parse goproxy response: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("empty version from goproxy")
	}
	return info.Version, nil
}
