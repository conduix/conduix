package stream

import (
	"testing"
	"time"
)

func TestNewElasticsearchLookupStage_RequiresEndpoint(t *testing.T) {
	config := map[string]any{
		"index":        "users",
		"join_field":   "user_id",
		"target_field": "user_info",
	}

	_, err := NewElasticsearchLookupStage("test", config)
	if err == nil {
		t.Error("expected error for missing endpoint")
	}
}

func TestNewElasticsearchLookupStage_RequiresIndex(t *testing.T) {
	config := map[string]any{
		"endpoints":    []any{"http://localhost:9200"},
		"join_field":   "user_id",
		"target_field": "user_info",
	}

	_, err := NewElasticsearchLookupStage("test", config)
	if err == nil {
		t.Error("expected error for missing index")
	}
}

func TestNewElasticsearchLookupStage_RequiresJoinField(t *testing.T) {
	config := map[string]any{
		"endpoints":    []any{"http://localhost:9200"},
		"index":        "users",
		"target_field": "user_info",
	}

	_, err := NewElasticsearchLookupStage("test", config)
	if err == nil {
		t.Error("expected error for missing join_field")
	}
}

func TestNewElasticsearchLookupStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"endpoints":     []any{"http://localhost:9200", "http://localhost:9201"},
		"index":         "users",
		"join_field":    "user_id",
		"target_field":  "user",
		"query_field":   "id",
		"source_fields": []any{"name", "email", "role"},
		"timeout":       "10s",
		"auth": map[string]any{
			"username": "elastic",
			"password": "secret",
		},
		"cache": map[string]any{
			"enabled":  true,
			"ttl":      "30m",
			"max_size": 50000,
		},
	}

	stage, err := NewElasticsearchLookupStage("es_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.Name() != "es_test" {
		t.Errorf("expected name 'es_test', got '%s'", stage.Name())
	}

	if stage.Type() != "es_lookup" {
		t.Errorf("expected type 'es_lookup', got '%s'", stage.Type())
	}

	if len(stage.endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(stage.endpoints))
	}

	if stage.index != "users" {
		t.Errorf("expected index 'users', got '%s'", stage.index)
	}

	if stage.queryField != "id" {
		t.Errorf("expected query_field 'id', got '%s'", stage.queryField)
	}

	if len(stage.sourceFields) != 3 {
		t.Errorf("expected 3 source_fields, got %d", len(stage.sourceFields))
	}

	if stage.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", stage.timeout)
	}

	if stage.username != "elastic" {
		t.Errorf("expected username 'elastic', got '%s'", stage.username)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled")
	}

	if stage.cacheTTL != 30*time.Minute {
		t.Errorf("expected cache TTL 30m, got %v", stage.cacheTTL)
	}

	if stage.cacheMaxSize != 50000 {
		t.Errorf("expected cache max size 50000, got %d", stage.cacheMaxSize)
	}
}

func TestNewElasticsearchLookupStage_DefaultValues(t *testing.T) {
	config := map[string]any{
		"endpoint":   "http://localhost:9200",
		"index":      "users",
		"join_field": "user_id",
	}

	stage, err := NewElasticsearchLookupStage("default_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults
	if stage.targetField != "_lookup" {
		t.Errorf("expected default target_field '_lookup', got '%s'", stage.targetField)
	}

	if stage.queryField != "_id" {
		t.Errorf("expected default query_field '_id', got '%s'", stage.queryField)
	}

	if stage.timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", stage.timeout)
	}

	if stage.onMissing != "skip" {
		t.Errorf("expected default on_missing 'skip', got '%s'", stage.onMissing)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled by default")
	}
}

func TestNewElasticsearchLookupStage_APIKeyAuth(t *testing.T) {
	config := map[string]any{
		"endpoint":   "http://localhost:9200",
		"index":      "users",
		"join_field": "user_id",
		"auth": map[string]any{
			"api_key": "my-api-key",
		},
	}

	stage, err := NewElasticsearchLookupStage("apikey_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.apiKey != "my-api-key" {
		t.Errorf("expected api_key 'my-api-key', got '%s'", stage.apiKey)
	}
}

func TestNewElasticsearchLookupStage_QueryTemplate(t *testing.T) {
	config := map[string]any{
		"endpoint":       "http://localhost:9200",
		"index":          "users",
		"join_field":     "user_id",
		"query_template": `{"query":{"match":{"email":"{{.email}}"}}}`,
	}

	stage, err := NewElasticsearchLookupStage("template_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.queryTemplate == nil {
		t.Error("expected query_template to be parsed")
	}
}

func TestNewElasticsearchLookupStage_InvalidQueryTemplate(t *testing.T) {
	config := map[string]any{
		"endpoint":       "http://localhost:9200",
		"index":          "users",
		"join_field":     "user_id",
		"query_template": `{{.invalid`,
	}

	_, err := NewElasticsearchLookupStage("invalid_template_test", config)
	if err == nil {
		t.Error("expected error for invalid query_template")
	}
}

// ESLookupBatchStage tests

func TestNewESLookupBatchStage_RequiresEndpoint(t *testing.T) {
	config := map[string]any{
		"index":        "users",
		"join_field":   "user_id",
		"target_field": "user_info",
	}

	_, err := NewESLookupBatchStage("test", config)
	if err == nil {
		t.Error("expected error for missing endpoint")
	}
}

func TestNewESLookupBatchStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"endpoints":    []any{"http://localhost:9200"},
		"index":        "users",
		"join_field":   "user_id",
		"target_field": "user",
		"batch": map[string]any{
			"size":    50,
			"timeout": "100ms",
		},
		"cache": map[string]any{
			"enabled": true,
		},
	}

	stage, err := NewESLookupBatchStage("batch_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.Name() != "batch_test" {
		t.Errorf("expected name 'batch_test', got '%s'", stage.Name())
	}

	if stage.Type() != "es_lookup_batch" {
		t.Errorf("expected type 'es_lookup_batch', got '%s'", stage.Type())
	}

	if stage.batchSize != 50 {
		t.Errorf("expected batch size 50, got %d", stage.batchSize)
	}

	if stage.batchTimeout != 100*time.Millisecond {
		t.Errorf("expected batch timeout 100ms, got %v", stage.batchTimeout)
	}
}

func TestESLookupBatchStage_DefaultValues(t *testing.T) {
	config := map[string]any{
		"endpoint":   "http://localhost:9200",
		"index":      "users",
		"join_field": "user_id",
	}

	stage, err := NewESLookupBatchStage("default_batch_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	// Check defaults
	if stage.batchSize != 100 {
		t.Errorf("expected default batch size 100, got %d", stage.batchSize)
	}

	if stage.batchTimeout != 50*time.Millisecond {
		t.Errorf("expected default batch timeout 50ms, got %v", stage.batchTimeout)
	}

	if stage.targetField != "_lookup" {
		t.Errorf("expected default target_field '_lookup', got '%s'", stage.targetField)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled by default")
	}
}

func TestESLookupBatchStage_Close(t *testing.T) {
	config := map[string]any{
		"endpoint":   "http://localhost:9200",
		"index":      "users",
		"join_field": "user_id",
	}

	stage, err := NewESLookupBatchStage("close_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should close without error
	if err := stage.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}
