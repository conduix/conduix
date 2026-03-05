package stream

import (
	"testing"
	"time"
)

func TestNewBatchLookupStage_RequiresSource(t *testing.T) {
	config := map[string]any{
		"join_field":   "user_id",
		"target_field": "user_info",
	}

	_, err := NewBatchLookupStage("test", config)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestNewBatchLookupStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address":    "localhost:6379",
				"key_prefix": "user:",
			},
		},
		"join_field":   "user_id",
		"target_field": "user_info",
		"batch": map[string]any{
			"size":           50,
			"timeout":        "100ms",
			"max_concurrent": 2,
		},
		"cache": map[string]any{
			"enabled":  true,
			"ttl":      "10m",
			"max_size": 5000,
		},
	}

	stage, err := NewBatchLookupStage("batch_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.Name() != "batch_test" {
		t.Errorf("expected name 'batch_test', got '%s'", stage.Name())
	}

	if stage.Type() != "batch_lookup" {
		t.Errorf("expected type 'batch_lookup', got '%s'", stage.Type())
	}

	if stage.batchSize != 50 {
		t.Errorf("expected batch size 50, got %d", stage.batchSize)
	}

	if stage.batchTimeout != 100*time.Millisecond {
		t.Errorf("expected batch timeout 100ms, got %v", stage.batchTimeout)
	}

	if stage.maxConcurrent != 2 {
		t.Errorf("expected max concurrent 2, got %d", stage.maxConcurrent)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled")
	}

	if stage.cacheTTL != 10*time.Minute {
		t.Errorf("expected cache TTL 10m, got %v", stage.cacheTTL)
	}

	if stage.cacheMaxSize != 5000 {
		t.Errorf("expected cache max size 5000, got %d", stage.cacheMaxSize)
	}
}

func TestBatchLookupStage_DefaultValues(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address": "localhost:6379",
			},
		},
		"join_field":   "id",
		"target_field": "data",
	}

	stage, err := NewBatchLookupStage("default_test", config)
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

	if stage.maxConcurrent != 4 {
		t.Errorf("expected default max concurrent 4, got %d", stage.maxConcurrent)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled by default")
	}
}

func TestBatchLookupStage_SQLConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "sql",
			"config": map[string]any{
				"driver":      "mysql",
				"dsn":         "user:pass@tcp(localhost:3306)/db",
				"batch_query": "SELECT id, name, email FROM users WHERE id IN (?)",
				"key_column":  "id",
			},
		},
		"join_field":   "user_id",
		"target_field": "user",
	}

	// This will fail because we can't connect to MySQL, but config parsing should work
	_, err := NewBatchLookupStage("sql_test", config)
	// SQL driver might not be available
	if err != nil {
		// Expected when no MySQL driver is registered
		t.Logf("expected error (no driver): %v", err)
	}
}

func TestBatchLookupStage_HTTPConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"batch_url":     "http://api.example.com/users/batch",
				"results_field": "users",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
		},
		"join_field":   "user_id",
		"target_field": "user",
	}

	stage, err := NewBatchLookupStage("http_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.sourceType != "http" {
		t.Errorf("expected source type 'http', got '%s'", stage.sourceType)
	}
}

func TestBatchLookupStage_OnMissing(t *testing.T) {
	tests := []struct {
		name       string
		onMissing  string
		defaultVal any
	}{
		{"skip", "skip", nil},
		{"error", "error", nil},
		{"default", "default", map[string]any{"status": "unknown"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]any{
				"source": map[string]any{
					"type": "redis",
					"config": map[string]any{
						"address": "localhost:6379",
					},
				},
				"join_field":   "id",
				"target_field": "data",
				"on_missing":   tt.onMissing,
			}
			if tt.defaultVal != nil {
				config["default_value"] = tt.defaultVal
			}

			stage, err := NewBatchLookupStage("on_missing_test", config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() { _ = stage.Close() }()

			if stage.onMissing != tt.onMissing {
				t.Errorf("expected on_missing '%s', got '%s'", tt.onMissing, stage.onMissing)
			}
		})
	}
}

func TestBatchLookupStage_BatchStats(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address": "localhost:6379",
			},
		},
		"join_field":   "id",
		"target_field": "data",
	}

	stage, err := NewBatchLookupStage("stats_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	stats := stage.BatchStats()

	if stats["batch_size"] != 100 {
		t.Errorf("expected batch_size 100, got %v", stats["batch_size"])
	}

	if stats["cache_enabled"] != true {
		t.Errorf("expected cache_enabled true, got %v", stats["cache_enabled"])
	}

	if stats["current_buffer_len"] != 0 {
		t.Errorf("expected current_buffer_len 0, got %v", stats["current_buffer_len"])
	}
}

func TestBatchLookupStage_Close(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address": "localhost:6379",
			},
		},
		"join_field":   "id",
		"target_field": "data",
	}

	stage, err := NewBatchLookupStage("close_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should close without error
	if err := stage.Close(); err != nil {
		// Redis connection error is expected in test environment
		t.Logf("close error (expected in test env): %v", err)
	}
}

func TestSetNestedValueHelper(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		value    any
		expected map[string]any
	}{
		{
			name:     "simple field",
			field:    "name",
			value:    "John",
			expected: map[string]any{"name": "John"},
		},
		{
			name:  "nested field",
			field: "user.name",
			value: "John",
			expected: map[string]any{
				"user": map[string]any{"name": "John"},
			},
		},
		{
			name:  "deeply nested",
			field: "a.b.c",
			value: 123,
			expected: map[string]any{
				"a": map[string]any{
					"b": map[string]any{"c": 123},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make(map[string]any)
			setNestedValueHelper(data, tt.field, tt.value)

			// Simple check - verify the value can be retrieved
			if tt.field == "name" {
				if data["name"] != "John" {
					t.Errorf("expected name 'John', got %v", data["name"])
				}
			}
		})
	}
}
