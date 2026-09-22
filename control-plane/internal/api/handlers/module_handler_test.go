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

	// 빈 문자열 버전 → @latest (필드 생략은 유지 — TestUpdateModule_KeepsVersionWhenFieldOmitted)
	w = doJSON(r, http.MethodPut, "/modules/example.com/m", `{"version":"","single_version_only":true}`)
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

// 플래그만 토글하는 요청(version 필드 없음)은 기본 버전을 건드리면 안 된다.
// 문자열 필드였을 때는 빈 값 → @latest 로 튀어 기본 버전이 바뀌었다(리뷰 2번).
func TestUpdateModule_KeepsVersionWhenFieldOmitted(t *testing.T) {
	h, db := newModuleTestHandler(t, "v9.9.9")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/m", Version: "v1.0.0", Status: "active"}).Error)

	w := doJSON(r, http.MethodPut, "/modules/example.com/m", `{"single_version_only":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var mod models.AllowedModule
	require.NoError(t, db.First(&mod, "module_path = ?", "example.com/m").Error)
	require.Equal(t, "v1.0.0", mod.Version, "version 필드가 없으면 유지")
	require.True(t, mod.SingleVersionOnly)

	// 빈 문자열은 여전히 "최신 재조회" 다.
	w = doJSON(r, http.MethodPut, "/modules/example.com/m", `{"version":""}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.First(&mod, "module_path = ?", "example.com/m").Error)
	require.Equal(t, "v9.9.9", mod.Version)
}

// single_version_only 모듈은 fork 가 불가능하므로, 기본과 다른 버전에 고정된 stage 가 있으면
// 기본 변경·플래그 켜기 모두 거부해야 한다 — 통과시키면 다음 빌드가 통째로 실패한다(리뷰 1번).
func TestUpdateModule_RejectsSingleVersionOnlyWithPinnedStages(t *testing.T) {
	h, db := newModuleTestHandler(t, "v2.0.0")
	r := moduleRouter(h)
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/sql", Version: "v1.0.0", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p1", Name: "old-driver-stage", Type: "native", Version: "v1", Status: "active",
		DepVersions: `{"example.com/sql":"v0.9.0"}`,
	}).Error)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p2", Name: "current-stage", Type: "native", Version: "v1", Status: "active",
		DepVersions: `{"example.com/sql":"v1.0.0"}`,
	}).Error)

	// 플래그 켜기: v0.9.0 에 고정된 stage 가 있어 거부
	w := doJSON(r, http.MethodPut, "/modules/example.com/sql", `{"single_version_only":true}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "old-driver-stage")
	require.NotContains(t, w.Body.String(), "current-stage")
	var mod models.AllowedModule
	require.NoError(t, db.First(&mod, "module_path = ?", "example.com/sql").Error)
	require.False(t, mod.SingleVersionOnly, "거부됐으면 플래그가 저장되지 않아야 한다")

	// 그 stage 가 기본으로 수렴하면 플래그 켜기 허용
	require.NoError(t, db.Model(&models.Plugin{}).Where("id = ?", "p1").Update("dep_versions", `{"example.com/sql":"v1.0.0"}`).Error)
	w = doJSON(r, http.MethodPut, "/modules/example.com/sql", `{"single_version_only":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 플래그가 켜진 상태에서 기본 변경: 모든 stage 가 v1.0.0 이라 v2.0.0 으로 바꾸면 전부 뒤처짐 → 거부
	w = doJSON(r, http.MethodPut, "/modules/example.com/sql", `{"version":"v2.0.0"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.NoError(t, db.First(&mod, "module_path = ?", "example.com/sql").Error)
	require.Equal(t, "v1.0.0", mod.Version, "거부됐으면 기본 버전이 바뀌지 않아야 한다")

	// 일반 모듈(플래그 없음)은 같은 상황에서 기본 변경 허용 — fork 로 공존하기 때문
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "example.com/ok", Version: "v1.0.0", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.Plugin{
		ID: "p3", Name: "ok-stage", Type: "native", Version: "v1", Status: "active",
		DepVersions: `{"example.com/ok":"v1.0.0"}`,
	}).Error)
	w = doJSON(r, http.MethodPut, "/modules/example.com/ok", `{"version":"v2.0.0"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// GOPROXY 대역: 모듈 루트 집합에만 @latest 200, 그 외(서브패키지·상위 접두사)는 404.
func fakeGoProxyWithModules(t *testing.T, modules map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/@latest")
		v, ok := modules[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"Version": v})
	}))
}

// import 경로에서 모듈 루트를 찾는다 — 서브패키지를 import 했을 때 사용자가 모듈 경로를 추측하지 않게.
func TestResolveModulePath_FindsModuleRootFromSubpackage(t *testing.T) {
	h, db := newModuleTestHandler(t, "unused")
	proxy := fakeGoProxyWithModules(t, map[string]string{
		"github.com/aws/aws-sdk-go-v2": "v1.30.0",
		"gopkg.in/yaml.v3":             "v3.0.1",
	})
	t.Cleanup(proxy.Close)
	h.goProxy, h.httpClient = proxy.URL, proxy.Client()
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "gopkg.in/yaml.v3", Version: "v3.0.1", Status: "active"}).Error)

	r := gin.New()
	r.POST("/modules/resolve", h.ResolveModulePath)

	w := doJSON(r, http.MethodPost, "/modules/resolve", `{"import_path":"github.com/aws/aws-sdk-go-v2/service/s3/types"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct{ Data ResolveModuleResponse }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "github.com/aws/aws-sdk-go-v2", resp.Data.ModulePath)
	require.Equal(t, "v1.30.0", resp.Data.LatestVersion)
	require.False(t, resp.Data.Registered)
	require.False(t, resp.Data.Heuristic)

	w = doJSON(r, http.MethodPost, "/modules/resolve", `{"import_path":"gopkg.in/yaml.v3"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Data.Registered)

	// 프록시가 모르는 경로: 휴리스틱(github 3세그먼트)으로 채우고 heuristic=true
	w = doJSON(r, http.MethodPost, "/modules/resolve", `{"import_path":"github.com/nobody/private/internal/x"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "github.com/nobody/private", resp.Data.ModulePath)
	require.True(t, resp.Data.Heuristic)
	require.NotEmpty(t, resp.Data.Error)
}

func TestModulePathCandidates_AndHeuristic(t *testing.T) {
	require.Equal(t, []string{"a.com/b/c/d", "a.com/b/c", "a.com/b"}, modulePathCandidates("a.com/b/c/d"))
	require.Equal(t, "github.com/o/r", heuristicModulePath("github.com/o/r/pkg/sub"))
	require.Equal(t, "golang.org/x/net", heuristicModulePath("golang.org/x/net/html"))
	require.Equal(t, "example.org/lib/v2", heuristicModulePath("example.org/lib/v2"), "모르는 호스트는 원문 유지")
}
