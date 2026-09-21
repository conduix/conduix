package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// fakeGoProxy 는 @latest 조회에 고정 버전을 돌려준다.
func fakeGoProxy(t *testing.T, version string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/@latest") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"Version": version})
	}))
}

func newModuleTestHandler(t *testing.T, latest string) (*ModuleHandler, *database.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&models.AllowedModule{}, &models.AllowedModuleVersion{}, &models.Plugin{}))
	db := &database.DB{DB: gdb}
	proxy := fakeGoProxy(t, latest)
	t.Cleanup(proxy.Close)
	return &ModuleHandler{
		db:         db,
		logger:     slog.Default(),
		goProxy:    proxy.URL,
		httpClient: proxy.Client(),
	}, db
}

func moduleRouter(h *ModuleHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/modules", h.ListModules)
	r.POST("/modules", h.CreateModule)
	r.PUT("/modules/*module", h.UpdateModule)
	r.DELETE("/modules/*module", h.DeleteModule)
	r.POST("/module-versions", h.AddModuleVersion)
	r.DELETE("/module-versions", h.RetireModuleVersion)
	return r
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func versionsOf(t *testing.T, db *database.DB, modulePath string) []string {
	t.Helper()
	var rows []models.AllowedModuleVersion
	require.NoError(t, db.Where("module_path = ?", modulePath).Order("version asc").Find(&rows).Error)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Version
	}
	return out
}

func TestBackfillModuleVersions_Idempotent(t *testing.T) {
	_, db := newModuleTestHandler(t, "v9.9.9")
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/a", Version: "v1.2.3", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/b", Version: "", Status: "active"}).Error)

	require.NoError(t, database.BackfillModuleVersions(db.DB))
	require.NoError(t, database.BackfillModuleVersions(db.DB)) // 두 번 돌려도 행이 늘지 않아야 한다

	require.Equal(t, []string{"v1.2.3"}, versionsOf(t, db, "example.com/a"))
	require.Empty(t, versionsOf(t, db, "example.com/b"), "빈 기본 버전은 백필하지 않는다")
}

func TestCreateModule_RecordsDefaultAsFirstVersion(t *testing.T) {
	h, db := newModuleTestHandler(t, "v1.6.0")
	r := moduleRouter(h)

	w := doJSON(r, http.MethodPost, "/modules", `{"module_path":"github.com/google/uuid"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var mod models.AllowedModule
	require.NoError(t, db.First(&mod, "module_path = ?", "github.com/google/uuid").Error)
	require.Equal(t, "v1.6.0", mod.Version)
	require.Equal(t, []string{"v1.6.0"}, versionsOf(t, db, "github.com/google/uuid"))

	w = doJSON(r, http.MethodPost, "/modules", `{"module_path":"github.com/google/uuid"}`)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestUpdateModule_ChangesDefaultAndKeepsOldVersion(t *testing.T) {
	h, db := newModuleTestHandler(t, "v2.0.0")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/m", Version: "v1.0.0", Status: "active"}).Error)
	require.NoError(t, database.BackfillModuleVersions(db.DB))

	// 명시 버전으로 기본 변경 → 새 버전 행 추가, 옛 버전 행 유지
	w := doJSON(r, http.MethodPut, "/modules/example.com/m", `{"version":"v1.5.0"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"v1.0.0", "v1.5.0"}, versionsOf(t, db, "example.com/m"))

	// 빈 버전 → @latest
	w = doJSON(r, http.MethodPut, "/modules/example.com/m", `{"single_version_only":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var mod models.AllowedModule
	require.NoError(t, db.First(&mod, "module_path = ?", "example.com/m").Error)
	require.Equal(t, "v2.0.0", mod.Version)
	require.True(t, mod.SingleVersionOnly)
	require.Equal(t, []string{"v1.0.0", "v1.5.0", "v2.0.0"}, versionsOf(t, db, "example.com/m"))
}

func TestAddModuleVersion_RejectsWhenSingleVersionOnly(t *testing.T) {
	h, db := newModuleTestHandler(t, "v3.0.0")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/sql", Version: "v1.0.0", Status: "active", SingleVersionOnly: true}).Error)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/ok", Version: "v1.0.0", Status: "active"}).Error)

	w := doJSON(r, http.MethodPost, "/module-versions", `{"module_path":"example.com/sql","version":"v0.9.0"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	w = doJSON(r, http.MethodPost, "/module-versions", `{"module_path":"example.com/ok","version":"v0.9.0"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	w = doJSON(r, http.MethodPost, "/module-versions", `{"module_path":"example.com/ok"}`) // @latest
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"v0.9.0", "v3.0.0"}, versionsOf(t, db, "example.com/ok"))

	w = doJSON(r, http.MethodPost, "/module-versions", `{"module_path":"example.com/none","version":"v1.0.0"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRetireModuleVersion_Guards(t *testing.T) {
	h, db := newModuleTestHandler(t, "v1.0.0")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/m", Version: "v1.0.0", Status: "active"}).Error)
	for _, v := range []string{"v1.0.0", "v0.8.0", "v0.7.0"} {
		require.NoError(t, db.Create(&models.AllowedModuleVersion{ModulePath: "example.com/m", Version: v, Status: "active"}).Error)
	}
	// v0.8.0 을 고정한 stage 하나
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p1", Name: "legacy-stage", Type: "native", Version: "v1", Status: "active",
		DepVersions: `{"example.com/m":"v0.8.0"}`,
	}).Error)

	w := doJSON(r, http.MethodDelete, "/module-versions?module_path=example.com/m&version=v1.0.0", "")
	require.Equal(t, http.StatusBadRequest, w.Code, "기본 버전은 폐기 불가")

	w = doJSON(r, http.MethodDelete, "/module-versions?module_path=example.com/m&version=v0.8.0", "")
	require.Equal(t, http.StatusConflict, w.Code, "고정한 stage 가 있으면 폐기 불가")
	require.Contains(t, w.Body.String(), "legacy-stage")

	w = doJSON(r, http.MethodDelete, "/module-versions?module_path=example.com/m&version=v0.7.0", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"v0.8.0", "v1.0.0"}, versionsOf(t, db, "example.com/m"))

	w = doJSON(r, http.MethodDelete, "/module-versions?module_path=example.com/m&version=v0.7.0", "")
	require.Equal(t, http.StatusNotFound, w.Code)

	// 폐기한 버전을 다시 추가할 수 있어야 한다(soft-delete 잔재로 PK 충돌이 나면 안 된다)
	w = doJSON(r, http.MethodPost, "/module-versions", `{"module_path":"example.com/m","version":"v0.7.0"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"v0.7.0", "v0.8.0", "v1.0.0"}, versionsOf(t, db, "example.com/m"))
}

func TestListModules_IncludesVersionsAndUsage(t *testing.T) {
	h, db := newModuleTestHandler(t, "v1.0.0")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/m", Version: "v1.0.0", Status: "active"}).Error)
	require.NoError(t, database.BackfillModuleVersions(db.DB))
	require.NoError(t, db.Create(&models.AllowedModuleVersion{ModulePath: "example.com/m", Version: "v0.8.0", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.Plugin{ID: "p1", Name: "a", Type: "native", Version: "v1", Status: "active", DepVersions: `{"example.com/m":"v0.8.0"}`}).Error)
	require.NoError(t, db.Create(&models.Plugin{ID: "p2", Name: "b", Type: "native", Version: "v1", Status: "active", DepVersions: `{"example.com/m":"v0.8.0"}`}).Error)
	require.NoError(t, db.Create(&models.Plugin{ID: "p3", Name: "c", Type: "native", Version: "v1", Status: "active", DepVersions: `{"example.com/m":"v1.0.0"}`}).Error)
	require.NoError(t, db.Create(&models.Plugin{ID: "p4", Name: "d", Type: "native", Version: "v1", Status: "active", DepVersions: `not-json`}).Error)
	require.NoError(t, db.Create(&models.Plugin{ID: "p5", Name: "legacy", Type: "native", Version: "v1", Status: "active"}).Error)

	w := doJSON(r, http.MethodGet, "/modules", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Data []ModuleView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	m := resp.Data[0]
	require.Equal(t, "v1.0.0", m.Version)
	require.Len(t, m.Versions, 2)
	require.Equal(t, map[string]int{"v0.8.0": 2, "v1.0.0": 1}, m.Usage, "손상 JSON 과 레거시(빈 값)는 집계에서 제외")
}
