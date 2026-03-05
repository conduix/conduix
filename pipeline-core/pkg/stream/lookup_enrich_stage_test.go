package stream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewLookupEnrichStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": "http://localhost:8080/api",
			},
		},
		"join_field":   "user_id",
		"target_field": "user_info",
		"on_missing":   "skip",
	}

	stage, err := NewLookupEnrichStage("test_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.Name() != "test_lookup" {
		t.Errorf("expected name 'test_lookup', got '%s'", stage.Name())
	}

	if stage.Type() != "lookup_enrich" {
		t.Errorf("expected type 'lookup_enrich', got '%s'", stage.Type())
	}

	if stage.sourceType != "http" {
		t.Errorf("expected source type 'http', got '%s'", stage.sourceType)
	}

	if stage.joinField != "user_id" {
		t.Errorf("expected join field 'user_id', got '%s'", stage.joinField)
	}

	if stage.targetField != "user_info" {
		t.Errorf("expected target field 'user_info', got '%s'", stage.targetField)
	}
}

func TestLookupEnrichStage_HTTPLookup(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/123" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    123,
				"name":  "John Doe",
				"email": "john@example.com",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": server.URL + "/users",
			},
		},
		"join_field":   "user_id",
		"target_field": "user",
		"on_missing":   "skip",
		"timeout":      "5s",
	}

	stage, err := NewLookupEnrichStage("http_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"user_id": 123,
			"action":  "login",
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	userInfo, ok := result.Data["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user field to be map, got %T", result.Data["user"])
	}

	if userInfo["name"] != "John Doe" {
		t.Errorf("expected name 'John Doe', got '%v'", userInfo["name"])
	}
}

func TestLookupEnrichStage_HTTPNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": server.URL + "/users",
			},
		},
		"join_field":    "user_id",
		"target_field":  "user",
		"on_missing":    "default",
		"default_value": map[string]any{"name": "Unknown"},
	}

	stage, err := NewLookupEnrichStage("http_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"user_id": 999,
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	userInfo, ok := result.Data["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user field to be map, got %T", result.Data["user"])
	}

	if userInfo["name"] != "Unknown" {
		t.Errorf("expected default name 'Unknown', got '%v'", userInfo["name"])
	}
}

func TestLookupEnrichStage_Cache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   123,
			"name": "John",
		})
	}))
	defer server.Close()

	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": server.URL + "/users",
			},
		},
		"join_field":   "user_id",
		"target_field": "user",
		"cache": map[string]any{
			"enabled":  true,
			"ttl":      "1m",
			"max_size": 100,
		},
	}

	stage, err := NewLookupEnrichStage("cached_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// First call - should hit the server
	record1 := &Record{Data: map[string]any{"user_id": 123}}
	_, err = stage.Process(ctx, record1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second call with same key - should use cache
	record2 := &Record{Data: map[string]any{"user_id": 123}}
	_, err = stage.Process(ctx, record2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 server call (cache hit), got %d", callCount)
	}
}

func TestLookupEnrichStage_NestedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"product": "Widget",
			"price":   19.99,
		})
	}))
	defer server.Close()

	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": server.URL + "/products",
			},
		},
		"join_field":   "order.product_id",
		"target_field": "order.product_info",
	}

	stage, err := NewLookupEnrichStage("nested_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"order": map[string]any{
				"product_id": "P001",
				"quantity":   2,
			},
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	order, ok := result.Data["order"].(map[string]any)
	if !ok {
		t.Fatalf("expected order field, got %T", result.Data["order"])
	}

	productInfo, ok := order["product_info"].(map[string]any)
	if !ok {
		t.Fatalf("expected product_info field, got %T", order["product_info"])
	}

	if productInfo["product"] != "Widget" {
		t.Errorf("expected product 'Widget', got '%v'", productInfo["product"])
	}
}

func TestLookupEnrichStage_MissingJoinField(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": "http://localhost/api",
			},
		},
		"join_field":   "missing_field",
		"target_field": "result",
		"on_missing":   "skip",
	}

	stage, err := NewLookupEnrichStage("missing_field_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"some_field": "value",
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should pass through unchanged when join field is missing
	if result == nil {
		t.Fatal("expected record to pass through")
	}

	if _, exists := result.Data["result"]; exists {
		t.Error("expected no result field when join field is missing")
	}
}

func TestLookupEnrichStage_OnMissingError(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": "http://localhost/api",
			},
		},
		"join_field":   "missing_field",
		"target_field": "result",
		"on_missing":   "error",
	}

	stage, err := NewLookupEnrichStage("error_lookup", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"some_field": "value",
		},
	}

	_, err = stage.Process(ctx, record)
	if err == nil {
		t.Error("expected error when join field is missing with on_missing=error")
	}
}

func TestLookupEnrichStage_LRUCache(t *testing.T) {
	cache := newLRUCache(2)

	// Add two items
	cache.Set("key1", "value1", time.Minute)
	cache.Set("key2", "value2", time.Minute)

	// Both should exist
	if v, ok := cache.Get("key1"); !ok || v != "value1" {
		t.Errorf("expected key1=value1, got %v", v)
	}
	if v, ok := cache.Get("key2"); !ok || v != "value2" {
		t.Errorf("expected key2=value2, got %v", v)
	}

	// Add third item - should evict oldest (key1)
	cache.Set("key3", "value3", time.Minute)

	if _, ok := cache.Get("key1"); ok {
		t.Error("expected key1 to be evicted")
	}
	if v, ok := cache.Get("key3"); !ok || v != "value3" {
		t.Errorf("expected key3=value3, got %v", v)
	}
}

func TestLookupEnrichStage_CacheTTL(t *testing.T) {
	cache := newLRUCache(10)

	// Add item with very short TTL
	cache.Set("key1", "value1", 1*time.Millisecond)

	// Should exist immediately
	if v, ok := cache.Get("key1"); !ok || v != "value1" {
		t.Errorf("expected key1=value1, got %v", v)
	}

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	// Should be expired
	if _, ok := cache.Get("key1"); ok {
		t.Error("expected key1 to be expired")
	}
}

func TestLookupEnrichStage_UnsupportedSourceType(t *testing.T) {
	config := map[string]any{
		"source": map[string]any{
			"type": "unsupported",
		},
		"join_field":   "id",
		"target_field": "result",
	}

	_, err := NewLookupEnrichStage("unsupported", config)
	if err == nil {
		t.Error("expected error for unsupported source type")
	}
}

func TestLookupEnrichStage_Stats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "value"})
	}))
	defer server.Close()

	config := map[string]any{
		"source": map[string]any{
			"type": "http",
			"config": map[string]any{
				"url": server.URL + "/api",
			},
		},
		"join_field":   "id",
		"target_field": "result",
	}

	stage, err := NewLookupEnrichStage("stats_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// Process some records
	for i := 0; i < 5; i++ {
		record := &Record{Data: map[string]any{"id": i}}
		_, _ = stage.Process(ctx, record)
	}

	input, output, errors := stage.Stats()
	if input != 5 {
		t.Errorf("expected 5 inputs, got %d", input)
	}
	if output != 5 {
		t.Errorf("expected 5 outputs, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}
