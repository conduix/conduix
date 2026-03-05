package stream

import (
	"testing"
	"time"
)

func TestNewAsyncEnrichStage_RequiresSource(t *testing.T) {
	config := map[string]any{
		"join_field":   "user_id",
		"target_field": "user_info",
	}

	_, err := NewAsyncEnrichStage("test", config)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestNewAsyncEnrichStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address":      "localhost:6379",
				"key_template": "user:{{.user_id}}",
			},
		},
		"join_field":   "user_id",
		"target_field": "user_info",
		"async": map[string]any{
			"workers":       8,
			"queue_size":    500,
			"ordered":       true,
			"max_in_flight": 50,
		},
		"cache": map[string]any{
			"enabled":  true,
			"ttl":      "15m",
			"max_size": 20000,
		},
	}

	stage, err := NewAsyncEnrichStage("async_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.Name() != "async_test" {
		t.Errorf("expected name 'async_test', got '%s'", stage.Name())
	}

	if stage.Type() != "async_enrich" {
		t.Errorf("expected type 'async_enrich', got '%s'", stage.Type())
	}

	if stage.workers != 8 {
		t.Errorf("expected workers 8, got %d", stage.workers)
	}

	if stage.queueSize != 500 {
		t.Errorf("expected queue size 500, got %d", stage.queueSize)
	}

	if !stage.ordered {
		t.Error("expected ordered to be true")
	}

	if stage.maxInFlight != 50 {
		t.Errorf("expected max_in_flight 50, got %d", stage.maxInFlight)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled")
	}

	if stage.cacheTTL != 15*time.Minute {
		t.Errorf("expected cache TTL 15m, got %v", stage.cacheTTL)
	}

	if stage.cacheMaxSize != 20000 {
		t.Errorf("expected cache max size 20000, got %d", stage.cacheMaxSize)
	}
}

func TestAsyncEnrichStage_DefaultValues(t *testing.T) {
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

	stage, err := NewAsyncEnrichStage("default_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	// Check defaults
	if stage.workers != 4 {
		t.Errorf("expected default workers 4, got %d", stage.workers)
	}

	if stage.queueSize != 1000 {
		t.Errorf("expected default queue size 1000, got %d", stage.queueSize)
	}

	if stage.ordered {
		t.Error("expected ordered to be false by default")
	}

	if stage.maxInFlight != 100 {
		t.Errorf("expected default max_in_flight 100, got %d", stage.maxInFlight)
	}

	if !stage.cacheEnabled {
		t.Error("expected cache to be enabled by default")
	}
}

func TestAsyncEnrichStage_HTTPConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url_template": "http://api.example.com/users/{{.user_id}}",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
		},
		"join_field":   "user_id",
		"target_field": "user",
		"timeout":      "10s",
	}

	stage, err := NewAsyncEnrichStage("http_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.sourceType != "http" {
		t.Errorf("expected source type 'http', got '%s'", stage.sourceType)
	}

	if stage.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", stage.timeout)
	}
}

func TestAsyncEnrichStage_OnMissing(t *testing.T) {
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

			stage, err := NewAsyncEnrichStage("on_missing_test", config)
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

func TestAsyncEnrichStage_AsyncStats(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "redis",
			"config": map[string]any{
				"address": "localhost:6379",
			},
		},
		"join_field":   "id",
		"target_field": "data",
		"async": map[string]any{
			"workers":    6,
			"queue_size": 200,
			"ordered":    true,
		},
	}

	stage, err := NewAsyncEnrichStage("stats_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	stats := stage.AsyncStats()

	if stats["workers"] != 6 {
		t.Errorf("expected workers 6, got %v", stats["workers"])
	}

	if stats["queue_size"] != 200 {
		t.Errorf("expected queue_size 200, got %v", stats["queue_size"])
	}

	if stats["ordered"] != true {
		t.Errorf("expected ordered true, got %v", stats["ordered"])
	}

	if stats["cache_enabled"] != true {
		t.Errorf("expected cache_enabled true, got %v", stats["cache_enabled"])
	}

	if stats["queue_length"] != 0 {
		t.Errorf("expected queue_length 0, got %v", stats["queue_length"])
	}
}

func TestAsyncEnrichStage_QueueLength(t *testing.T) {
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

	stage, err := NewAsyncEnrichStage("queue_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	// Initial queue should be empty
	if stage.QueueLength() != 0 {
		t.Errorf("expected queue length 0, got %d", stage.QueueLength())
	}
}

func TestAsyncEnrichStage_Close(t *testing.T) {
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

	stage, err := NewAsyncEnrichStage("close_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should close without error
	if err := stage.Close(); err != nil {
		// Redis connection error is expected in test environment
		t.Logf("close error (expected in test env): %v", err)
	}
}

func TestAsyncEnrichStage_GetResults(t *testing.T) {
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

	stage, err := NewAsyncEnrichStage("results_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	// Initially no results
	results := stage.GetResults()
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
