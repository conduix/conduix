package handlers

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/dependency"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

func newPluginTestHandler(t *testing.T) (*PluginHandler, *database.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(
		&models.AllowedModule{}, &models.AllowedModuleVersion{},
		&models.Plugin{}, &models.StageRevision{},
	))
	db := &database.DB{DB: gdb}
	return &PluginHandler{
		db:              db,
		logger:          slog.Default(),
		revisionService: services.NewRevisionService(gdb),
	}, db
}

func pluginRouter(h *PluginHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/plugins", h.ListPlugins)
	r.GET("/plugins/:name", h.GetPlugin)
	r.POST("/plugins/:name/upgrade-deps", h.UpgradeDeps)
	r.POST("/module-versions/upgrade-all", h.UpgradeAll)
	return r
}

// 이미 기본 버전이면 컴파일을 돌리지 않고 "바뀐 것 없음" 으로 끝나야 한다.
// (여기서 컴파일이 돌면 테스트가 수십 초 걸리므로, 통과 자체가 조기 종료의 증거다.)
func TestUpgradeDeps_NoChangeWhenAlreadyOnDefault(t *testing.T) {
	h, db := newPluginTestHandler(t)
	require.NoError(t, db.Create(&models.AllowedModule{
		ModulePath: "github.com/google/uuid", Version: "v1.6.0", Status: "active",
	}).Error)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p1", Name: "tag", Type: "native", Status: "active",
		SourceCode: "package x\n", DepVersions: `{"github.com/google/uuid":"v1.6.0"}`,
	}).Error)

	w := doJSON(pluginRouter(h), "POST", "/plugins/tag/upgrade-deps", `{}`)
	require.Equal(t, 200, w.Code, w.Body.String())

	var resp struct {
		Data UpgradeDepsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp.Data.Changed)
	require.Equal(t, "v1.6.0", resp.Data.DepVersions["github.com/google/uuid"])
}

func TestUpgradeDeps_RejectsNonNativeStage(t *testing.T) {
	h, db := newPluginTestHandler(t)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p1", Name: "js", Type: "script", Status: "active",
	}).Error)

	w := doJSON(pluginRouter(h), "POST", "/plugins/js/upgrade-deps", `{}`)
	require.Equal(t, 400, w.Code, w.Body.String())
}

func TestUpgradeDeps_NotFound(t *testing.T) {
	h, _ := newPluginTestHandler(t)
	w := doJSON(pluginRouter(h), "POST", "/plugins/missing/upgrade-deps", `{}`)
	require.Equal(t, 404, w.Code)
}

// 그 모듈을 고정하지 않았거나 이미 기본인 stage 는 결과 목록에 아예 나오지 않아야 한다
// (컴파일도 돌지 않는다 — 이 테스트가 빨리 끝나는 것이 그 증거다).
func TestUpgradeAll_SkipsStagesAlreadyOnDefault(t *testing.T) {
	h, db := newPluginTestHandler(t)
	require.NoError(t, db.Create(&models.AllowedModule{
		ModulePath: "github.com/google/uuid", Version: "v1.6.0", Status: "active",
	}).Error)
	for _, p := range []models.Plugin{
		{ID: "p1", Name: "on-default", Type: "native", Status: "active", SourceCode: "package x\n", DepVersions: `{"github.com/google/uuid":"v1.6.0"}`},
		{ID: "p2", Name: "pins-other", Type: "native", Status: "active", SourceCode: "package x\n", DepVersions: `{"github.com/other/mod":"v0.1.0"}`},
		{ID: "p3", Name: "legacy", Type: "native", Status: "active", SourceCode: "package x\n"},
	} {
		require.NoError(t, db.Create(&p).Error)
	}

	w := doJSON(pluginRouter(h), "POST", "/module-versions/upgrade-all",
		`{"module_path":"github.com/google/uuid"}`)
	require.Equal(t, 200, w.Code, w.Body.String())

	var resp struct {
		Data struct {
			DefaultVersion string             `json:"default_version"`
			Results        []UpgradeAllResult `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "v1.6.0", resp.Data.DefaultVersion)
	require.Empty(t, resp.Data.Results, "아무도 올릴 게 없으면 결과가 비어야 한다")
}

func TestUpgradeAll_RejectsUnknownModule(t *testing.T) {
	h, _ := newPluginTestHandler(t)
	w := doJSON(pluginRouter(h), "POST", "/module-versions/upgrade-all",
		`{"module_path":"github.com/nope/nope"}`)
	require.Equal(t, 404, w.Code)
}

// 목록·단건 응답에 pinned_behind 가 붙고, 기존 Plugin 필드는 그대로여야 한다(하위호환).
func TestPluginResponses_CarryPinnedBehind(t *testing.T) {
	h, db := newPluginTestHandler(t)
	require.NoError(t, db.Create(&models.AllowedModule{
		ModulePath: "github.com/google/uuid", Version: "v1.6.0", Status: "active",
	}).Error)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p1", Name: "old", Type: "native", Status: "active",
		SourceCode: "package x\n", DepVersions: `{"github.com/google/uuid":"v1.3.0"}`,
	}).Error)

	r := pluginRouter(h)

	w := doJSON(r, "GET", "/plugins", "")
	require.Equal(t, 200, w.Code)
	var list struct {
		Data []PluginView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, "old", list.Data[0].Name, "임베딩된 Plugin 필드가 유지돼야 한다")
	require.Len(t, list.Data[0].PinnedBehind, 1)
	require.Equal(t, "v1.3.0", list.Data[0].PinnedBehind[0].Pinned)
	require.Equal(t, "v1.6.0", list.Data[0].PinnedBehind[0].Default)

	w = doJSON(r, "GET", "/plugins/old", "")
	require.Equal(t, 200, w.Code)
	var one struct {
		Data PluginView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &one))
	require.Len(t, one.Data.PinnedBehind, 1)
}

// 기본 버전과 같은 stage 에는 배지를 달지 않는다(있으면 UI 가 늘 경고를 띄운다).
func TestPinnedBehindOf_EmptyWhenOnDefault(t *testing.T) {
	defaults := map[string]string{"github.com/google/uuid": "v1.6.0"}
	require.Empty(t, pinnedBehindOf(`{"github.com/google/uuid":"v1.6.0"}`, defaults))
	require.Empty(t, pinnedBehindOf("", defaults), "레거시(고정값 없음)도 배지 없음")
}

func TestUpgradedPins_TargetsOnlyRequestedModules(t *testing.T) {
	current := dependency.Pins{
		"github.com/google/uuid":       "v1.3.0",
		"github.com/go-resty/resty/v2": "v2.7.0",
	}
	defaults := map[string]string{
		"github.com/google/uuid":       "v1.6.0",
		"github.com/go-resty/resty/v2": "v2.11.0",
	}

	target, changed := upgradedPins(current, defaults, []string{"github.com/google/uuid"})
	require.Equal(t, map[string]string{"github.com/google/uuid": "v1.6.0"}, changed)
	require.Equal(t, "v2.7.0", target["github.com/go-resty/resty/v2"], "지정하지 않은 모듈은 그대로")

	target, changed = upgradedPins(current, defaults, nil)
	require.Len(t, changed, 2, "비우면 고정된 모듈 전부가 대상")
	require.Equal(t, "v2.11.0", target["github.com/go-resty/resty/v2"])
}

// 레지스트리에 없는 모듈을 고정한 stage 는 올릴 기준이 없으므로 건드리지 않는다.
func TestUpgradedPins_LeavesUnknownModuleAlone(t *testing.T) {
	current := dependency.Pins{"github.com/gone/mod": "v0.1.0"}
	target, changed := upgradedPins(current, map[string]string{}, nil)
	require.Empty(t, changed)
	require.Equal(t, "v0.1.0", target["github.com/gone/mod"])
}

func TestChangedSummary_IsDeterministic(t *testing.T) {
	changed := map[string]string{"b/mod": "v2", "a/mod": "v1"}
	require.Equal(t, "a/mod@v1, b/mod@v2", changedSummary(changed))
}
