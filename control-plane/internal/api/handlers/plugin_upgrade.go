package handlers

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/dependency"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// UpgradeDepsRequest 는 올릴 모듈을 고른다. 비우면 이 stage 가 고정한 모듈 전부.
type UpgradeDepsRequest struct {
	Modules []string `json:"modules,omitempty"`
}

// UpgradeDepsResponse 는 올리기 결과다.
type UpgradeDepsResponse struct {
	PluginName  string            `json:"plugin_name"`
	Changed     map[string]string `json:"changed,omitempty"` // module_path → 새 버전
	DepVersions map[string]string `json:"dep_versions"`
	BuildOutput string            `json:"build_output,omitempty"`
}

// PinnedBehind 는 stage 가 기본보다 낮은(또는 다른) 버전에 머물러 있는 모듈이다.
type PinnedBehind struct {
	ModulePath string `json:"module_path"`
	Pinned     string `json:"pinned"`
	Default    string `json:"default"`
}

// UpgradeDeps POST /api/v1/plugins/:name/upgrade-deps — stage 의 고정 버전을 기본 버전으로 올린다.
// 정책은 upgradeStageDeps 에 있다. 컴파일 실패는 400(사용자가 고칠 문제), 그 외는 500.
func (h *PluginHandler) UpgradeDeps(c *gin.Context) {
	name := c.Param("name")
	var req UpgradeDepsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
			return
		}
	}

	var plugin models.Plugin
	if err := h.db.Where("name = ?", name).First(&plugin).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "plugin not found: "+name)
		return
	}
	if plugin.Type != "native" || plugin.SourceCode == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed,
			"native stage 만 의존성 버전을 올릴 수 있습니다")
		return
	}

	res, err := h.upgradeStageDeps(c.Request.Context(), &plugin, req.Modules, c.GetString("user_id"))
	if err != nil {
		status := http.StatusBadRequest
		code := types.ErrCodeValidationFailed
		if !isCompileFailure(err) {
			status, code = http.StatusInternalServerError, types.ErrCodeInternalError
		}
		middleware.ErrorResponseWithCode(c, status, code, err.Error())
		return
	}
	middleware.SuccessResponse(c, res)
}

// upgradeStageDeps 는 stage 의 고정 버전을 기본으로 올린다(핸들러와 일괄 처리가 공유).
//
// 올린 버전으로 실제 컴파일이 되는지 먼저 확인하고, 성공했을 때만 저장한다. 컴파일이
// 깨지면 저장값을 그대로 둔다 — stage 소유자가 코드를 고칠 때까지 기존 버전으로 계속
// 동작해야 하기 때문이다(ADR-0005 W5).
func (h *PluginHandler) upgradeStageDeps(ctx context.Context, plugin *models.Plugin, modules []string, userID string) (*UpgradeDepsResponse, error) {
	allowed, err := activeAllowedModules(h.db)
	if err != nil {
		return nil, err
	}
	defaults := dependency.Defaults(allowed)
	current := dependency.ParsePins(plugin.DepVersions)

	target, changed := upgradedPins(current, defaults, modules)
	if len(changed) == 0 {
		return &UpgradeDepsResponse{PluginName: plugin.Name, DepVersions: current}, nil
	}

	buildCtx, cancel := context.WithTimeout(ctx, stageCompileTimeout)
	defer cancel()

	built, buildErr := compileStage(buildCtx, plugin.SourceCode, target)
	output := ""
	if built != nil {
		defer built.Cleanup()
		output = built.Output
	}
	if buildErr != nil {
		return nil, &compileFailure{output: output}
	}

	encoded, err := target.Encode()
	if err != nil {
		return nil, err
	}
	plugin.DepVersions = encoded
	plugin.UpdatedAt = time.Now()
	if err := h.db.Save(plugin).Error; err != nil {
		return nil, err
	}

	// 소스는 그대로지만 빌드 입력이 바뀌었으므로 이력에 남긴다.
	h.createRevision(plugin.ID, plugin.Name, "update", plugin.SourceCode, plugin.GoMod,
		plugin.SourceHash, plugin.SourceCode, "의존성 버전 올림: "+changedSummary(changed), userID)

	return &UpgradeDepsResponse{
		PluginName: plugin.Name, Changed: changed, DepVersions: target, BuildOutput: output,
	}, nil
}

// compileFailure 는 "기본 버전으로는 컴파일이 안 된다" 는 사용자 입력 문제다.
// 저장 실패·DB 오류(서버 문제)와 구분해야 응답 코드가 갈린다.
type compileFailure struct{ output string }

func (e *compileFailure) Error() string {
	return "기본 버전으로 컴파일되지 않습니다. 고정 버전은 그대로 둡니다.\n" + e.output
}

func isCompileFailure(err error) bool {
	var cf *compileFailure
	return errors.As(err, &cf)
}

// upgradedPins 는 대상 모듈을 기본 버전으로 바꾼 pins 와 바뀐 목록을 돌려준다.
// modules 가 비면 고정된 모듈 전부가 대상이다.
func upgradedPins(current dependency.Pins, defaults map[string]string, modules []string) (dependency.Pins, map[string]string) {
	wanted := map[string]bool{}
	for _, m := range modules {
		wanted[m] = true
	}

	target := dependency.Pins{}
	changed := map[string]string{}
	for mod, v := range current {
		target[mod] = v
		if len(wanted) > 0 && !wanted[mod] {
			continue
		}
		if def, ok := defaults[mod]; ok && def != v {
			target[mod] = def
			changed[mod] = def
		}
	}
	return target, changed
}

func changedSummary(changed map[string]string) string {
	mods := make([]string, 0, len(changed))
	for m := range changed {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	out := ""
	for i, m := range mods {
		if i > 0 {
			out += ", "
		}
		out += m + "@" + changed[m]
	}
	return out
}

// pinnedBehindOf 는 stage 가 기본과 다른 버전에 머문 모듈 목록이다(UI 배지 재료).
func pinnedBehindOf(depVersions string, defaults map[string]string) []PinnedBehind {
	pins := dependency.ParsePins(depVersions)
	if pins == nil {
		return nil
	}
	var out []PinnedBehind
	for _, mod := range pins.SortedModules() {
		def, ok := defaults[mod]
		if !ok || def == pins[mod] {
			continue
		}
		out = append(out, PinnedBehind{ModulePath: mod, Pinned: pins[mod], Default: def})
	}
	return out
}

// defaultsForResponse 는 목록/단건 응답에 붙일 기본 버전 맵이다.
// 조회 실패는 빈 맵으로 — 배지가 안 보일 뿐 조회 자체를 실패시키지 않는다.
func defaultsForResponse(db *database.DB) map[string]string {
	allowed, err := activeAllowedModules(db)
	if err != nil {
		return map[string]string{}
	}
	return dependency.Defaults(allowed)
}

// PluginView 는 Plugin 에 파생 필드를 얹은 응답 형태다.
// 임베딩이라 기존 JSON 필드는 그대로고 pinned_behind 만 추가된다 — 클라이언트 하위호환 유지.
type PluginView struct {
	models.Plugin
	PinnedBehind []PinnedBehind `json:"pinned_behind,omitempty"`
}

// pluginViews 는 플러그인 목록에 pinned_behind 를 붙인다.
func pluginViews(plugins []models.Plugin, defaults map[string]string) []PluginView {
	out := make([]PluginView, len(plugins))
	for i, p := range plugins {
		out[i] = PluginView{Plugin: p, PinnedBehind: pinnedBehindOf(p.DepVersions, defaults)}
	}
	return out
}

// UpgradeAllRequest 는 일괄 올리기 대상 모듈이다.
type UpgradeAllRequest struct {
	ModulePath string `json:"module_path" binding:"required"`
}

// UpgradeAllResult 는 stage 하나의 처리 결과다.
type UpgradeAllResult struct {
	PluginName string            `json:"plugin_name"`
	Success    bool              `json:"success"`
	Changed    map[string]string `json:"changed,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// UpgradeAll POST /api/v1/module-versions/upgrade-all — 그 모듈을 비기본 버전으로 고정한
// 모든 stage 를 기본 버전으로 올린다(admin).
//
// stage 마다 독립적으로 컴파일해 보고, 실패한 stage 는 기존 고정값 그대로 둔다 —
// 하나가 깨져도 나머지는 수렴시키는 것이 목적이다(ADR-0005 W5).
func (h *PluginHandler) UpgradeAll(c *gin.Context) {
	var req UpgradeAllRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	allowed, err := activeAllowedModules(h.db)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
		return
	}
	defaults := dependency.Defaults(allowed)
	def, known := defaults[req.ModulePath]
	if !known {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound,
			"등록되지 않은 모듈입니다: "+req.ModulePath)
		return
	}

	var plugins []models.Plugin
	if err := h.db.Where("type = ? AND status = ?", "native", "active").
		Order("name asc").Find(&plugins).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
		return
	}

	userID := c.GetString("user_id")
	results := make([]UpgradeAllResult, 0)
	for i := range plugins {
		p := &plugins[i]
		if p.SourceCode == "" {
			continue // 소스 없는 stage 는 컴파일 검증 자체가 불가능하다
		}
		pins := dependency.ParsePins(p.DepVersions)
		if v, pinned := pins[req.ModulePath]; !pinned || v == def {
			continue
		}
		res, uerr := h.upgradeStageDeps(c.Request.Context(), p, []string{req.ModulePath}, userID)
		if uerr != nil {
			results = append(results, UpgradeAllResult{PluginName: p.Name, Error: uerr.Error()})
			continue
		}
		results = append(results, UpgradeAllResult{PluginName: p.Name, Success: true, Changed: res.Changed})
	}

	middleware.SuccessResponse(c, gin.H{
		"module_path":     req.ModulePath,
		"default_version": def,
		"results":         results,
	})
}
