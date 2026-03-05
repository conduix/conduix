package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewHTTPSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/users",
		Method: "GET",
		Headers: map[string]string{
			"Accept": "application/json",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "http" {
		t.Errorf("expected name 'http', got '%s'", source.Name())
	}

	if source.url != "https://api.example.com/users" {
		t.Errorf("unexpected url: %s", source.url)
	}

	if source.method != "GET" {
		t.Errorf("expected method 'GET', got '%s'", source.method)
	}
}

func TestNewHTTPSource_WithBody(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/search",
		Method: "POST",
		Body:   `{"query": "test"}`,
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.body != `{"query": "test"}` {
		t.Errorf("unexpected body: %s", source.body)
	}
}

// Authentication Tests

func TestSetAuth_Basic(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:     "basic",
			Username: "testuser",
			Password: "testpass",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a test request
	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		t.Error("expected Authorization header to be set")
	}
	if authHeader[:6] != "Basic " {
		t.Errorf("expected Basic auth, got '%s'", authHeader)
	}
}

func TestSetAuth_Bearer(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:  "bearer",
			Token: "my-secret-token",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	authHeader := req.Header.Get("Authorization")
	if authHeader != "Bearer my-secret-token" {
		t.Errorf("expected 'Bearer my-secret-token', got '%s'", authHeader)
	}
}

func TestSetAuth_APIKey_Header(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:       "api_key",
			APIKey:     "secret-api-key-123",
			APIKeyIn:   "header",
			APIKeyName: "X-API-Key",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	apiKey := req.Header.Get("X-API-Key")
	if apiKey != "secret-api-key-123" {
		t.Errorf("expected 'secret-api-key-123', got '%s'", apiKey)
	}
}

func TestSetAuth_APIKey_DefaultHeader(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:   "api_key",
			APIKey: "my-key",
			// APIKeyIn and APIKeyName not set - should use defaults
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default header name should be X-API-Key
	apiKey := req.Header.Get("X-API-Key")
	if apiKey != "my-key" {
		t.Errorf("expected 'my-key' in X-API-Key header, got '%s'", apiKey)
	}
}

func TestSetAuth_APIKey_Query(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:       "api_key",
			APIKey:     "query-api-key",
			APIKeyIn:   "query",
			APIKeyName: "api_key",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// API key should be in query parameter
	queryValue := req.URL.Query().Get("api_key")
	if queryValue != "query-api-key" {
		t.Errorf("expected 'query-api-key' in query param, got '%s'", queryValue)
	}
}

func TestSetAuth_APIKey_EnvVar(t *testing.T) {
	// Set environment variable
	t.Setenv("HTTP_API_KEY", "env-api-key")

	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:       "api_key",
			APIKey:     "${HTTP_API_KEY}",
			APIKeyIn:   "header",
			APIKeyName: "Authorization",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have expanded environment variable
	authHeader := req.Header.Get("Authorization")
	if authHeader != "env-api-key" {
		t.Errorf("expected 'env-api-key', got '%s'", authHeader)
	}
}

func TestSetAuth_Nil(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		// No auth configured
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	err = source.setAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No Authorization header should be set
	authHeader := req.Header.Get("Authorization")
	if authHeader != "" {
		t.Errorf("expected no Authorization header, got '%s'", authHeader)
	}
}

// mTLS Configuration Tests

func TestBuildHTTPClient_NoAuth(t *testing.T) {
	client, err := buildHTTPClient(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Default client should not have custom transport
	if client.Transport != nil {
		t.Error("expected default transport (nil) for non-mTLS client")
	}
}

func TestBuildHTTPClient_NonMTLS(t *testing.T) {
	auth := &config.AuthConfig{
		Type:     "basic",
		Username: "user",
		Password: "pass",
	}

	client, err := buildHTTPClient(auth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-mTLS auth should not have custom transport
	if client.Transport != nil {
		t.Error("expected default transport for non-mTLS auth")
	}
}

func TestBuildHTTPClient_MTLS_Disabled(t *testing.T) {
	auth := &config.AuthConfig{
		Type: "mtls",
		TLS: &config.TLSClientConfig{
			Enabled: false, // TLS disabled
		},
	}

	client, err := buildHTTPClient(auth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Disabled TLS should not set transport
	if client.Transport != nil {
		t.Error("expected default transport when TLS is disabled")
	}
}

func TestBuildHTTPTLSConfig_Nil(t *testing.T) {
	tlsCfg, err := buildHTTPTLSConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config for nil input")
	}
}

func TestBuildHTTPTLSConfig_Disabled(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled: false,
	}

	tlsCfg, err := buildHTTPTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config when disabled")
	}
}

func TestBuildHTTPTLSConfig_SkipVerify(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		SkipVerify: true,
	}

	tlsCfg, err := buildHTTPTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildHTTPTLSConfig_ServerName(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		ServerName: "api.example.com",
	}

	tlsCfg, err := buildHTTPTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg.ServerName != "api.example.com" {
		t.Errorf("expected ServerName 'api.example.com', got '%s'", tlsCfg.ServerName)
	}
}

func TestBuildHTTPTLSConfig_InvalidCACert(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled: true,
		CACert:  "/nonexistent/ca.crt",
	}

	_, err := buildHTTPTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA cert file")
	}
}

func TestBuildHTTPTLSConfig_InvalidClientCert(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		ClientCert: "/nonexistent/client.crt",
		ClientKey:  "/nonexistent/client.key",
	}

	_, err := buildHTTPTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent client cert files")
	}
}

// HTTP Integration Tests

func TestHTTPSource_DoRequest_Simple(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []map[string]any{
			{"id": 1, "name": "Test 1"},
			{"id": 2, "name": "Test 2"},
		}
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    server.URL,
		Method: "GET",
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	records, errs := source.Read(ctx)

	var received []Record
	for record := range records {
		received = append(received, record)
	}

	// Check for errors
	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	if len(received) != 2 {
		t.Errorf("expected 2 records, got %d", len(received))
	}
}

func TestHTTPSource_DoRequest_WithAPIKeyHeader(t *testing.T) {
	// Create test server that verifies API key
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    server.URL,
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:       "api_key",
			APIKey:     "test-api-key",
			APIKeyIn:   "header",
			APIKeyName: "X-API-Key",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	records, errs := source.Read(ctx)

	var received []Record
	for record := range records {
		received = append(received, record)
	}

	// Check for errors
	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	if len(received) != 1 {
		t.Errorf("expected 1 record, got %d", len(received))
	}

	if received[0].Data["success"] != true {
		t.Error("expected success: true")
	}
}

func TestHTTPSource_DoRequest_WithAPIKeyQuery(t *testing.T) {
	// Create test server that verifies API key in query
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.URL.Query().Get("key")
		if apiKey != "query-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"authorized": true})
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    server.URL,
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:       "api_key",
			APIKey:     "query-key",
			APIKeyIn:   "query",
			APIKeyName: "key",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	records, errs := source.Read(ctx)

	var received []Record
	for record := range records {
		received = append(received, record)
	}

	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	if len(received) != 1 {
		t.Errorf("expected 1 record, got %d", len(received))
	}
}

// Pagination Tests

func TestHTTPSource_Pagination_Offset(t *testing.T) {
	pageCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		page := r.URL.Query().Get("page")
		var data []map[string]any

		switch page {
		case "1":
			data = []map[string]any{{"id": 1}, {"id": 2}}
		case "2":
			data = []map[string]any{{"id": 3}} // Less than perPage, indicates last page
		default:
			data = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    server.URL,
		Method: "GET",
		Pagination: &config.PaginationConfig{
			Type:      "offset",
			MaxPages:  10,
			PerPage:   2,
			StartPage: 1,
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	records, errs := source.Read(ctx)

	var received []Record
	for record := range records {
		received = append(received, record)
	}

	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	if len(received) != 3 {
		t.Errorf("expected 3 records, got %d", len(received))
	}
}

// Rate Limiting Tests

func TestHTTPSource_RateLimit(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		RateLimit: &config.RateLimitSourceConfig{
			Enabled:  true,
			Rate:     10,
			Interval: "second",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.rateLimit == nil {
		t.Error("expected rateLimit to be set")
	}

	if source.rateLimit.Rate != 10 {
		t.Errorf("expected rate 10, got %d", source.rateLimit.Rate)
	}
}

// Helper function tests

func TestExtractItems_Array(t *testing.T) {
	response := []any{
		map[string]any{"id": 1},
		map[string]any{"id": 2},
	}

	items, obj := extractItems(response, "")

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	if obj != nil {
		t.Error("expected nil object for array response")
	}
}

func TestExtractItems_Object(t *testing.T) {
	response := map[string]any{
		"data": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
		"meta": map[string]any{"total": 2},
	}

	items, obj := extractItems(response, "data")

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	if obj == nil {
		t.Error("expected non-nil object")
	}

	if obj["meta"] == nil {
		t.Error("expected meta field in object")
	}
}

func TestGetNestedValue(t *testing.T) {
	data := map[string]any{
		"meta": map[string]any{
			"pagination": map[string]any{
				"next_offset": 100,
			},
		},
	}

	tests := []struct {
		path     string
		expected any
	}{
		{"meta.pagination.next_offset", float64(100)},
		{"meta.pagination", map[string]any{"next_offset": 100}},
		{"nonexistent", nil},
		{"meta.nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := getNestedValue(data, tt.path)

			// For numeric values, convert to comparable type
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			} else if num, ok := tt.expected.(float64); ok {
				if val, ok := result.(int); ok {
					if float64(val) != num {
						t.Errorf("expected %v, got %v", tt.expected, result)
					}
				}
			}
		})
	}
}
