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
		&models.PluginStage{},
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
		plugins.GET("/:name/stages", h.GetPluginStages)
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
		Stages: []CreateStageRequest{
			{
				StageType:   "test-stage",
				Category:    "transform",
				DisplayName: "Test Stage",
				Description: "A test stage",
				ConfigSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"threshold": map[string]any{
							"type":    "number",
							"default": 0.8,
						},
					},
				},
			},
		},
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
	assert.Len(t, response.Data.Stages, 1)
	assert.Equal(t, "test-stage", response.Data.Stages[0].StageType)
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
		Stages: []CreateStageRequest{
			{
				StageType:    "stage-a",
				ConfigSchema: map[string]any{"type": "object"},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// 같은 이름으로 다시 등록 (Upsert)
	reqBody.Version = "v2.0.0"
	reqBody.Stages = []CreateStageRequest{
		{
			StageType:    "stage-b",
			ConfigSchema: map[string]any{"type": "object"},
		},
		{
			StageType:    "stage-c",
			ConfigSchema: map[string]any{"type": "object"},
		},
	}

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
	assert.Len(t, response.Data.Stages, 2)
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
			Stages: []CreateStageRequest{
				{StageType: "stage-a1", ConfigSchema: map[string]any{"type": "object"}},
				{StageType: "stage-a2", ConfigSchema: map[string]any{"type": "object"}},
			},
		},
		{
			Name:    "plugin-b",
			Version: "v1.0.0",
			Image:   "registry/plugin-b:v1.0.0",
			Stages: []CreateStageRequest{
				{StageType: "stage-b1", ConfigSchema: map[string]any{"type": "object"}},
			},
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
		Success bool             `json:"success"`
		Data    []PluginResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Len(t, response.Data, 2)

	// StageCount 검증
	for _, p := range response.Data {
		switch p.Name {
		case "plugin-a":
			assert.Equal(t, 2, p.StageCount)
		case "plugin-b":
			assert.Equal(t, 1, p.StageCount)
		}
	}
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
		Stages: []CreateStageRequest{
			{
				StageType:   "test-stage",
				DisplayName: "Test Stage",
				ConfigSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"threshold": map[string]any{"type": "number"},
					},
				},
			},
		},
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
	assert.Len(t, response.Data.Stages, 1)
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
		Stages: []CreateStageRequest{
			{StageType: "test-stage", ConfigSchema: map[string]any{"type": "object"}},
		},
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

func TestGetPluginStages(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 플러그인 생성
	reqBody := CreatePluginRequest{
		Name:    "test-plugin",
		Version: "v1.0.0",
		Image:   "myregistry/test-plugin:v1.0.0",
		Stages: []CreateStageRequest{
			{
				StageType:   "stage-1",
				Category:    "transform",
				DisplayName: "Stage One",
				ConfigSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"field1": map[string]any{"type": "string"},
					},
				},
			},
			{
				StageType:   "stage-2",
				Category:    "filter",
				DisplayName: "Stage Two",
				ConfigSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"field2": map[string]any{"type": "number"},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Stage 목록 조회
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/test-plugin/stages", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Success bool                 `json:"success"`
		Data    []models.PluginStage `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Len(t, response.Data, 2)
}

func TestTestScript_Success(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	reqBody := TestScriptRequest{
		Code: `
def process(record):
    record["greeting"] = "hello " + record.get("name", "")
    return record
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

	assert.True(t, response.Data.Success)
	assert.False(t, response.Data.Dropped)
	assert.Equal(t, "hello world", response.Data.Output["greeting"])
}

func TestTestScript_Drop(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	reqBody := TestScriptRequest{
		Code: `
def process(record):
    return None
`,
		SampleData: map[string]any{"key": "value"},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/test-script", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Data TestScriptResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Data.Success)
	assert.True(t, response.Data.Dropped)
}

func TestTestScript_CompileError(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	reqBody := TestScriptRequest{
		Code:       `def process(record)  return record`,
		SampleData: map[string]any{"key": "value"},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/test-script", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Data TestScriptResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.False(t, response.Data.Success)
	assert.NotEmpty(t, response.Data.Error)
}

func TestUpdatePlugin(t *testing.T) {
	db := setupTestDB(t)
	handler := NewPluginHandler(db)
	router := setupTestRouter(handler)

	// 플러그인 생성
	reqBody := CreatePluginRequest{
		Name:    "test-plugin",
		Version: "v1.0.0",
		Image:   "myregistry/test-plugin:v1.0.0",
		Stages: []CreateStageRequest{
			{StageType: "test-stage", ConfigSchema: map[string]any{"type": "object"}},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 업데이트
	updateReq := UpdatePluginRequest{
		Version: "v2.0.0",
		Status:  "deprecated",
	}

	body, _ = json.Marshal(updateReq)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/plugins/test-plugin", bytes.NewReader(body))
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

	assert.True(t, response.Success)
	assert.Equal(t, "v2.0.0", response.Data.Version)
	assert.Equal(t, "deprecated", response.Data.Status)
}
