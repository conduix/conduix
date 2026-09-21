package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ModuleHandler 커스텀 stage 가 import 할 수 있는 외부 모듈 레지스트리(allowed_modules) API.
// 모듈은 여러 버전을 보유하고 그중 하나가 기본(default)이다. stage 는 자기 버전을 고정하므로
// (Plugin.DepVersions) 기본 버전을 바꿔도 기존 stage 는 깨지지 않는다(ADR-0005).
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

// CreateModuleRequest 모듈 등록 요청. 버전은 받지 않는다 — 플랫폼이 등록 시점 최신을 기본 버전으로 고정(D3).
type CreateModuleRequest struct {
	ModulePath  string `json:"module_path" binding:"required"` // 예: github.com/google/uuid
	Description string `json:"description,omitempty"`
}

// UpdateModuleRequest 기본 버전 변경 요청.
// Version 은 포인터다 — 필드 자체가 없으면(nil) 기본 버전을 유지하고, 빈 문자열이면 최신을 재조회한다.
// 문자열이면 "플래그만 토글" 요청도 빈 값으로 들어와 기본 버전이 @latest 로 튀어 버린다.
// 기존 stage 의 고정 버전은 건드리지 않는다 — 새 stage 만 새 기본값을 받는다.
type UpdateModuleRequest struct {
	Version           *string `json:"version,omitempty"`
	Status            string  `json:"status,omitempty"`
	SingleVersionOnly *bool   `json:"single_version_only,omitempty"`
}

// ModuleVersionRequest 보유 버전 추가/폐기 요청.
type ModuleVersionRequest struct {
	ModulePath string `json:"module_path" binding:"required"`
	Version    string `json:"version,omitempty"` // 추가 시 빈 값이면 @latest
}

// ModuleView 목록 응답 — 모듈 + 보유 버전 + 버전별로 고정한 native stage 수.
type ModuleView struct {
	models.AllowedModule
	Versions []models.AllowedModuleVersion `json:"versions"`
	Usage    map[string]int                `json:"usage"`
}

// ListModules GET /api/v1/modules
func (h *ModuleHandler) ListModules(c *gin.Context) {
	var mods []models.AllowedModule
	if err := h.db.Order("module_path asc").Find(&mods).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to list modules")
		return
	}
	var versions []models.AllowedModuleVersion
	if err := h.db.Order("module_path asc, version asc").Find(&versions).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to list module versions")
		return
	}
	usage, err := moduleUsage(h.db)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to compute module usage")
		return
	}

	byModule := make(map[string][]models.AllowedModuleVersion, len(mods))
	for _, v := range versions {
		byModule[v.ModulePath] = append(byModule[v.ModulePath], v)
	}
	views := make([]ModuleView, 0, len(mods))
	for _, m := range mods {
		u := usage[m.ModulePath]
		if u == nil {
			u = map[string]int{}
		}
		vs := byModule[m.ModulePath]
		if vs == nil {
			vs = []models.AllowedModuleVersion{}
		}
		views = append(views, ModuleView{AllowedModule: m, Versions: vs, Usage: u})
	}
	c.JSON(http.StatusOK, types.APIResponse[[]ModuleView]{Success: true, Data: views})
}

// CreateModule POST /api/v1/modules — 등록 시점 최신 버전을 기본 버전 + 첫 보유 버전으로(D3).
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

	var existing models.AllowedModule
	if err := h.db.First(&existing, "module_path = ?", modulePath).Error; err == nil {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeValidationFailed,
			fmt.Sprintf("module already registered: %s (default %s). use PUT to change default or POST /module-versions to add a version.", modulePath, existing.Version))
		return
	}

	version, err := h.latestVersion(c, modulePath)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadGateway, types.ErrCodeInternalError,
			fmt.Sprintf("failed to resolve latest version for %s: %v", modulePath, err))
		return
	}

	addedBy := userIDFrom(c)
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
	if err := h.ensureVersionRow(modulePath, version, addedBy); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to record module version")
		return
	}
	h.logger.Info("allowed module registered", "module_path", modulePath, "version", version, "added_by", addedBy)
	c.JSON(http.StatusCreated, types.APIResponse[models.AllowedModule]{Success: true, Data: mod, Message: "module registered"})
}

// UpdateModule PUT /api/v1/modules/*module — 기본 버전 변경. 버전 미지정 시 최신 재조회.
// 지정한 버전이 보유 목록에 없으면 자동 추가한다. 기존 stage 의 고정값은 바뀌지 않는다.
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
	if req.SingleVersionOnly != nil {
		mod.SingleVersionOnly = *req.SingleVersionOnly
	}
	version := mod.Version
	if req.Version != nil {
		version = strings.TrimSpace(*req.Version)
		if version == "" {
			v, err := h.latestVersion(c, modulePath)
			if err != nil {
				middleware.ErrorResponseWithCode(c, http.StatusBadGateway, types.ErrCodeInternalError,
					fmt.Sprintf("failed to resolve latest version: %v", err))
				return
			}
			version = v
		}
	}

	// single_version_only 모듈은 fork 가 불가능하므로, 기본과 다른 버전에 고정된 stage 가 하나라도
	// 남아 있으면 다음 빌드가 통째로 실패한다(builder.resolveDeps 가 거부). 레지스트리 변경이
	// 빌드를 실패 상태로 남기지 않도록 여기서 막고, 먼저 stage 들을 수렴시키라고 안내한다.
	// 기본 버전 변경과 플래그 켜기 양쪽 모두 이 조건에 걸린다.
	if mod.SingleVersionOnly {
		behind, err := pluginsPinnedElsewhere(h.db, modulePath, version)
		if err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to check version usage")
			return
		}
		if len(behind) > 0 {
			middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeValidationFailed,
				fmt.Sprintf("%s 는 단일 버전만 허용되는데 %d개 stage 가 %s 외의 버전에 고정되어 있습니다: %s — 먼저 POST /module-versions/upgrade-all 로 수렴시키세요",
					modulePath, len(behind), version, strings.Join(behind, ", ")))
			return
		}
	}
	mod.Version = version

	if err := h.db.Save(&mod).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update module")
		return
	}
	if err := h.ensureVersionRow(modulePath, version, userIDFrom(c)); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to record module version")
		return
	}
	h.logger.Info("allowed module default changed", "module_path", modulePath, "version", version)
	c.JSON(http.StatusOK, types.APIResponse[models.AllowedModule]{Success: true, Data: mod, Message: "module updated"})
}

// DeleteModule DELETE /api/v1/modules/*module — 보유 버전 행도 함께 지운다.
func (h *ModuleHandler) DeleteModule(c *gin.Context) {
	modulePath := strings.TrimPrefix(c.Param("module"), "/")
	if err := h.db.Where("module_path = ?", modulePath).Delete(&models.AllowedModuleVersion{}).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete module versions")
		return
	}
	if err := h.db.Where("module_path = ?", modulePath).Delete(&models.AllowedModule{}).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete module")
		return
	}
	c.JSON(http.StatusOK, types.APIResponse[any]{Success: true, Message: "module deleted"})
}

// AddModuleVersion POST /api/v1/module-versions — 보유 버전 추가. 기본 버전은 바꾸지 않는다.
// SingleVersionOnly 모듈은 기본 외 버전을 가질 수 없으므로 거부한다.
func (h *ModuleHandler) AddModuleVersion(c *gin.Context) {
	var req ModuleVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}
	modulePath := strings.TrimSpace(req.ModulePath)
	var mod models.AllowedModule
	if err := h.db.First(&mod, "module_path = ?", modulePath).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "module not found — register it first (POST /modules)")
		return
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
	if mod.SingleVersionOnly && version != mod.Version {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeValidationFailed,
			fmt.Sprintf("module %s is single_version_only (default %s): additional versions cannot coexist in one binary", modulePath, mod.Version))
		return
	}
	if err := h.ensureVersionRow(modulePath, version, userIDFrom(c)); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to add module version")
		return
	}
	h.logger.Info("allowed module version added", "module_path", modulePath, "version", version)
	c.JSON(http.StatusCreated, types.APIResponse[models.AllowedModuleVersion]{
		Success: true,
		Data:    models.AllowedModuleVersion{ModulePath: modulePath, Version: version, Status: "active"},
		Message: "module version added",
	})
}

// RetireModuleVersion DELETE /api/v1/module-versions?module_path=&version= — 보유 버전 폐기.
// 기본 버전이거나 어떤 stage 가 고정하고 있으면 거부한다(그 stage 목록을 알려준다).
func (h *ModuleHandler) RetireModuleVersion(c *gin.Context) {
	modulePath := strings.TrimSpace(c.Query("module_path"))
	version := strings.TrimSpace(c.Query("version"))
	if modulePath == "" || version == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "module_path and version are required")
		return
	}
	var mod models.AllowedModule
	if err := h.db.First(&mod, "module_path = ?", modulePath).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "module not found")
		return
	}
	if mod.Version == version {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed,
			"cannot retire the default version — change the default first (PUT /modules/*module)")
		return
	}
	users, err := pluginsPinnedTo(h.db, modulePath, version)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to check version usage")
		return
	}
	if len(users) > 0 {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeValidationFailed,
			fmt.Sprintf("version %s of %s is pinned by %d stage(s): %s", version, modulePath, len(users), strings.Join(users, ", ")))
		return
	}
	res := h.db.Where("module_path = ? AND version = ?", modulePath, version).Delete(&models.AllowedModuleVersion{})
	if res.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to retire module version")
		return
	}
	if res.RowsAffected == 0 {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "module version not found")
		return
	}
	h.logger.Info("allowed module version retired", "module_path", modulePath, "version", version)
	c.JSON(http.StatusOK, types.APIResponse[any]{Success: true, Message: "module version retired"})
}

// ensureVersionRow 는 (module_path, version) 보유 행을 멱등하게 만든다.
func (h *ModuleHandler) ensureVersionRow(modulePath, version, addedBy string) error {
	row := models.AllowedModuleVersion{ModulePath: modulePath, Version: version}
	return h.db.Where(&models.AllowedModuleVersion{ModulePath: modulePath, Version: version}).
		Attrs(models.AllowedModuleVersion{Status: "active", AddedBy: addedBy}).
		FirstOrCreate(&row).Error
}

// moduleUsage 는 native stage 들의 DepVersions 를 모아 module_path → version → stage 수 를 만든다.
// 레거시(DepVersions 빈 값) stage 는 기본 버전을 쓰는 것으로 보고 집계하지 않는다.
func moduleUsage(db *database.DB) (map[string]map[string]int, error) {
	pins, err := nativePluginPins(db)
	if err != nil {
		return nil, err
	}
	usage := make(map[string]map[string]int)
	for _, p := range pins {
		for mod, ver := range p.pins {
			if usage[mod] == nil {
				usage[mod] = map[string]int{}
			}
			usage[mod][ver]++
		}
	}
	return usage, nil
}

// pluginsPinnedTo 는 특정 (module, version) 에 고정된 native stage 이름 목록을 돌려준다.
func pluginsPinnedTo(db *database.DB, modulePath, version string) ([]string, error) {
	pins, err := nativePluginPins(db)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range pins {
		if p.pins[modulePath] == version {
			names = append(names, p.name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// pluginsPinnedElsewhere 는 모듈을 고정했지만 그 버전이 version 이 아닌 native stage 이름 목록이다.
func pluginsPinnedElsewhere(db *database.DB, modulePath, version string) ([]string, error) {
	pins, err := nativePluginPins(db)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range pins {
		if v, ok := p.pins[modulePath]; ok && v != version {
			names = append(names, p.name)
		}
	}
	sort.Strings(names)
	return names, nil
}

type pluginPins struct {
	name string
	pins map[string]string
}

// nativePluginPins 는 DepVersions 가 있는 native stage 의 고정 버전 맵을 파싱한다.
// 손상된 JSON 은 그 stage 만 건너뛴다 — 목록 API 전체를 막을 이유는 아니다.
func nativePluginPins(db *database.DB) ([]pluginPins, error) {
	var plugins []models.Plugin
	if err := db.Select("name", "dep_versions").
		Where("type = ? AND dep_versions <> ''", "native").
		Find(&plugins).Error; err != nil {
		return nil, err
	}
	out := make([]pluginPins, 0, len(plugins))
	for _, p := range plugins {
		var pins map[string]string
		if err := json.Unmarshal([]byte(p.DepVersions), &pins); err != nil || len(pins) == 0 {
			continue
		}
		out = append(out, pluginPins{name: p.Name, pins: pins})
	}
	return out, nil
}

func userIDFrom(c *gin.Context) string {
	if v, ok := c.Get("user_id"); ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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
