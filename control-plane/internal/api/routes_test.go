package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/config"
	"github.com/conduix/conduix/control-plane/pkg/database"
)

// TestNoDuplicateRoutes ensures that no routes are registered twice
// This test prevents panic errors like: "handlers are already registered for path '/api/v1/pipelines/:id/checkpoints'"
func TestNoDuplicateRoutes(t *testing.T) {
	// Setup
	db := &database.DB{} // Mock DB (not connected)

	// Create Redis service with dummy config (won't actually connect in test)
	redisConfig := &services.RedisServiceConfig{
		Addr:             "localhost:6379",
		Password:         "",
		DB:               0,
		EnableRetryQueue: false,
	}
	redisService, _ := services.NewRedisService(redisConfig)

	schedulerConfig := &services.SchedulerConfig{
		RefreshInterval: 30 * time.Second,
	}
	schedulerService := services.NewSchedulerService(db, redisService, schedulerConfig)
	jwtSecret := "test-secret"
	usersConfig := &config.UsersConfig{
		AdminEmails:    []string{},
		OperatorEmails: []string{},
	}
	frontendURL := "http://localhost:3000"

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Route setup panicked with: %v", r)
		}
	}()

	// Create server - this will call setupRoutes()
	server := NewServer(db, redisService, schedulerService, jwtSecret, usersConfig, frontendURL)

	if server == nil {
		t.Fatal("Server creation failed")
	}

	// Get all routes
	routes := server.router.Routes()
	if len(routes) == 0 {
		t.Fatal("No routes registered")
	}

	// Check for duplicate routes (same method + path)
	routeMap := make(map[string]string) // key: method+path, value: path
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if existingPath, exists := routeMap[key]; exists {
			t.Errorf("Duplicate route found: %s %s (also registered as %s)", route.Method, route.Path, existingPath)
		}
		routeMap[key] = route.Path
	}

	// Verify critical routes exist
	criticalRoutes := map[string]bool{
		"GET /":                                           false,
		"GET /health":                                     false,
		"GET /ready":                                      false,
		"POST /api/v1/pipelines/:id/checkpoints":          false,
		"GET /api/v1/pipelines/:id/checkpoints":           false,
		"GET /api/v1/pipeline-links":                      false,
		"POST /api/v1/pipeline-links":                     false,
		"GET /api/v1/pipeline-links/:parent_id/:child_id": false,
	}

	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, exists := criticalRoutes[key]; exists {
			criticalRoutes[key] = true
		}
	}

	// Check that all critical routes were found
	for routeKey, found := range criticalRoutes {
		if !found {
			t.Errorf("Critical route not found: %s", routeKey)
		}
	}
}

// TestCheckpointRoutesNotDuplicated specifically tests that checkpoint routes are not duplicated
func TestCheckpointRoutesNotDuplicated(t *testing.T) {
	// Setup
	db := &database.DB{}

	redisConfig := &services.RedisServiceConfig{
		Addr:             "localhost:6379",
		Password:         "",
		DB:               0,
		EnableRetryQueue: false,
	}
	redisService, _ := services.NewRedisService(redisConfig)

	schedulerConfig := &services.SchedulerConfig{
		RefreshInterval: 30 * time.Second,
	}
	schedulerService := services.NewSchedulerService(db, redisService, schedulerConfig)
	jwtSecret := "test-secret"
	usersConfig := &config.UsersConfig{
		AdminEmails:    []string{},
		OperatorEmails: []string{},
	}
	frontendURL := "http://localhost:3000"

	// Create server
	server := NewServer(db, redisService, schedulerService, jwtSecret, usersConfig, frontendURL)

	routes := server.router.Routes()

	// Count how many times GET /api/v1/pipelines/:id/checkpoints is registered
	getCheckpointCount := 0
	postCheckpointCount := 0

	for _, route := range routes {
		if route.Path == "/api/v1/pipelines/:id/checkpoints" {
			if route.Method == "GET" {
				getCheckpointCount++
			}
			if route.Method == "POST" {
				postCheckpointCount++
			}
		}
	}

	// Should have exactly one GET and one POST
	if getCheckpointCount != 1 {
		t.Errorf("Expected exactly 1 GET /api/v1/pipelines/:id/checkpoints route, found %d", getCheckpointCount)
	}

	if postCheckpointCount != 1 {
		t.Errorf("Expected exactly 1 POST /api/v1/pipelines/:id/checkpoints route, found %d", postCheckpointCount)
	}
}

// TestPipelineLinkRoutesExist tests that pipeline link routes are registered correctly
func TestPipelineLinkRoutesExist(t *testing.T) {
	// Setup
	db := &database.DB{}

	redisConfig := &services.RedisServiceConfig{
		Addr:             "localhost:6379",
		Password:         "",
		DB:               0,
		EnableRetryQueue: false,
	}
	redisService, _ := services.NewRedisService(redisConfig)

	schedulerConfig := &services.SchedulerConfig{
		RefreshInterval: 30 * time.Second,
	}
	schedulerService := services.NewSchedulerService(db, redisService, schedulerConfig)
	jwtSecret := "test-secret"
	usersConfig := &config.UsersConfig{
		AdminEmails:    []string{},
		OperatorEmails: []string{},
	}
	frontendURL := "http://localhost:3000"

	// Create server
	server := NewServer(db, redisService, schedulerService, jwtSecret, usersConfig, frontendURL)

	routes := server.router.Routes()

	// Pipeline link routes that should exist
	expectedRoutes := []string{
		"GET /api/v1/pipeline-links",
		"POST /api/v1/pipeline-links",
		"GET /api/v1/pipeline-links/:parent_id/:child_id",
		"DELETE /api/v1/pipeline-links/:parent_id/:child_id",
		"GET /api/v1/pipeline-links/parent/:parent_id",
		"GET /api/v1/pipeline-links/child/:child_id",
		"GET /api/v1/pipeline-links/workflow/:workflow_id",
	}

	foundRoutes := make(map[string]bool)
	for _, route := range routes {
		key := route.Method + " " + route.Path
		foundRoutes[key] = true
	}

	for _, expectedRoute := range expectedRoutes {
		if !foundRoutes[expectedRoute] {
			t.Errorf("Expected route not found: %s", expectedRoute)
		}
	}
}

// TestGetLinksByWorkflowIsUnauthenticated는 executor(worker/batch Job)가 실행 시
// 호출하는 링크 조회 API가 JWT 없이 접근 가능한지 검증한다.
// (인증 그룹에 있으면 401 → DAG 워크플로우의 부모-자식 Kafka 링크가 조용히 끊긴다.)
func TestGetLinksByWorkflowIsUnauthenticated(t *testing.T) {
	db := &database.DB{}
	redisService, _ := services.NewRedisService(&services.RedisServiceConfig{Addr: "localhost:6379"})
	schedulerService := services.NewSchedulerService(db, redisService, &services.SchedulerConfig{RefreshInterval: 30 * time.Second})
	server := NewServer(db, redisService, schedulerService, "test-secret", &config.UsersConfig{}, "http://localhost:3000")

	// 토큰 없이 호출 → AuthMiddleware를 거치지 않으므로 401이 아니어야 한다.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline-links/workflow/wf-123", nil)
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("GET /pipeline-links/workflow/:id는 내부 API라 401이면 안 됨 (got 401). 인증 그룹에 잘못 배치됨")
	}

	// 대조군: 인증 필요한 링크 조회는 토큰 없이 401이어야 한다.
	reqAuth := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline-links", nil)
	recAuth := httptest.NewRecorder()
	server.router.ServeHTTP(recAuth, reqAuth)
	if recAuth.Code != http.StatusUnauthorized {
		t.Errorf("GET /pipeline-links(UI 조회)는 토큰 없으면 401이어야 함, got %d", recAuth.Code)
	}
}

// TestIndexRoute tests that the index route returns service info
func TestIndexRoute(t *testing.T) {
	// Setup
	db := &database.DB{}

	redisConfig := &services.RedisServiceConfig{
		Addr:             "localhost:6379",
		Password:         "",
		DB:               0,
		EnableRetryQueue: false,
	}
	redisService, _ := services.NewRedisService(redisConfig)

	schedulerConfig := &services.SchedulerConfig{
		RefreshInterval: 30 * time.Second,
	}
	schedulerService := services.NewSchedulerService(db, redisService, schedulerConfig)
	jwtSecret := "test-secret"
	usersConfig := &config.UsersConfig{
		AdminEmails:    []string{},
		OperatorEmails: []string{},
	}
	frontendURL := "http://localhost:3000"

	// Create server
	server := NewServer(db, redisService, schedulerService, jwtSecret, usersConfig, frontendURL)

	// Make request to index
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	// Check status code
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var info IndexInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify required fields
	if info.Service != "Conduix Control Plane" {
		t.Errorf("Expected service 'Conduix Control Plane', got '%s'", info.Service)
	}

	if info.Version == "" {
		t.Error("Expected version to be set")
	}

	if info.GoVersion == "" {
		t.Error("Expected go_version to be set")
	}

	if len(info.Endpoints) == 0 {
		t.Error("Expected endpoints to be set")
	}

	// Verify key endpoints are listed
	expectedEndpoints := []string{"health", "ready", "api", "pipelines", "workflows"}
	for _, ep := range expectedEndpoints {
		if _, ok := info.Endpoints[ep]; !ok {
			t.Errorf("Expected endpoint '%s' to be in endpoints map", ep)
		}
	}
}
