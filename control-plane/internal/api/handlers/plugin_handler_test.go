package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// setupTestDB 테스트용 in-memory SQLite DB 설정
func setupTestDB(t *testing.T) *database.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 테스트용 마이그레이션
	err = db.AutoMigrate(
		&models.Plugin{},
	)
	require.NoError(t, err)

	return &database.DB{DB: db}
}

// setupTestRouter 테스트용 라우터 설정
func setupTestRouter(h *PluginHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	plugins := r.Group("/api/v1/plugins")
	{
		plugins.GET("", h.ListPlugins)
		plugins.POST("", h.CreatePlugin)
		plugins.POST("/test-script", h.TestScript)
		plugins.GET("/:name", h.GetPlugin)
		plugins.PUT("/:name", h.UpdatePlugin)
		plugins.DELETE("/:name", h.DeletePlugin)
	}

	return r
}

func TestCreatePlugin(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 테스트 데이터
	reqBody := CreatePluginRequest{
		Name:        "test-plugin",
		Version:     "v1.0.0",
		Image:       "myregistry/test-plugin:v1.0.0",
		Description: "Test plugin",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response struct {
		Success bool          `json:"success"`
		Data    models.Plugin `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Equal(t, "test-plugin", response.Data.Name)
	assert.Equal(t, "v1.0.0", response.Data.Version)
}

func TestCreatePluginUpsert(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 첫 번째 플러그인 생성
	reqBody := CreatePluginRequest{
		Name:    "test-plugin",
		Version: "v1.0.0",
		Image:   "myregistry/test-plugin:v1.0.0",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// 같은 이름으로 다시 등록 (Upsert)
	reqBody.Version = "v2.0.0"

	body, _ = json.Marshal(reqBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool          `json:"success"`
		Data    models.Plugin `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "v2.0.0", response.Data.Version)
}

func TestListPlugins(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 테스트 데이터 생성
	plugins := []CreatePluginRequest{
		{
			Name:    "plugin-a",
			Version: "v1.0.0",
			Image:   "registry/plugin-a:v1.0.0",
		},
		{
			Name:    "plugin-b",
			Version: "v1.0.0",
			Image:   "registry/plugin-b:v1.0.0",
		},
	}

	for _, p := range plugins {
		body, _ := json.Marshal(p)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	// 목록 조회
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool            `json:"success"`
		Data    []models.Plugin `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Len(t, response.Data, 2)
}

func TestGetPlugin(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 플러그인 생성
	reqBody := CreatePluginRequest{
		Name:    "test-plugin",
		Version: "v1.0.0",
		Image:   "myregistry/test-plugin:v1.0.0",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 상세 조회
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/test-plugin", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool          `json:"success"`
		Data    models.Plugin `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Equal(t, "test-plugin", response.Data.Name)
}

func TestGetPluginNotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/non-existent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePlugin(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 플러그인 생성
	reqBody := CreatePluginRequest{
		Name:    "test-plugin",
		Version: "v1.0.0",
		Image:   "myregistry/test-plugin:v1.0.0",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 삭제
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/test-plugin", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 삭제 확인
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/test-plugin", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTestScript_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	reqBody := TestScriptRequest{
		Code: `
function process(record) {
    record.greeting = "hello " + (record.name || "");
    return record;
}
`,
		SampleData: map[string]any{
			"name": "world",
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/test-script", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool               `json:"success"`
		Data    TestScriptResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.True(t, response.Data.Success)
	assert.Equal(t, "hello world", response.Data.Output["greeting"])
}

func TestTestScript_SyntaxError(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	reqBody := TestScriptRequest{
		Code: `
function process(record {  // syntax error: missing )
    return record;
}
`,
		SampleData: map[string]any{},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/test-script", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool               `json:"success"`
		Data    TestScriptResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.False(t, response.Data.Success)
	assert.NotEmpty(t, response.Data.Error)
}

// BUG#8 회귀: CreatePlugin 이 source_code 를 저장해야 한다. 예전엔 CreatePluginRequest 에
// SourceCode/Type 필드가 없어 web-ui 신규 커스텀 stage 생성 시 빈 소스 plugin 이 만들어졌다.
func TestCreatePluginPersistsSourceCode(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// js_script(script) 타입 — 소스는 저장되지만 빌드 트리거 없음(테스트에서 runnerBuilder 부담 없이 검증).
	reqBody := CreatePluginRequest{
		Name:       "js-custom",
		Type:       "script",
		SourceCode: "function process(r){ r.tag='x'; return r; }",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	// DB 에 source_code 가 실제로 저장됐는지 확인(응답이 아니라 저장 상태).
	var p models.Plugin
	require.NoError(t, db.First(&p, "name = ?", "js-custom").Error)
	assert.Equal(t, "function process(r){ r.tag='x'; return r; }", p.SourceCode, "CreatePlugin must persist source_code")
	assert.Equal(t, "script", p.Type)
}
