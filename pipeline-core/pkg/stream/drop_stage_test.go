package stream

import (
	"context"
	"testing"
)

func TestDropStage_SingleField(t *testing.T) {
	config := map[string]any{
		"fields": []any{"password"},
	}
	stage, err := NewDropStage("test", config)
	if err != nil {
		t.Fatalf("NewDropStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"name":     "Alice",
		"password": "secret",
		"email":    "alice@test.com",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if _, exists := result.Data["password"]; exists {
		t.Error("password field should have been dropped")
	}
	if result.Data["name"] != "Alice" {
		t.Error("name field should remain unchanged")
	}
	if result.Data["email"] != "alice@test.com" {
		t.Error("email field should remain unchanged")
	}
}

func TestDropStage_MultipleFields(t *testing.T) {
	config := map[string]any{
		"fields": []any{"password", "secret_key", "internal_id"},
	}
	stage, err := NewDropStage("test", config)
	if err != nil {
		t.Fatalf("NewDropStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"name":        "Alice",
		"password":    "secret",
		"secret_key":  "abc123",
		"internal_id": 42,
		"email":       "alice@test.com",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	for _, field := range []string{"password", "secret_key", "internal_id"} {
		if _, exists := result.Data[field]; exists {
			t.Errorf("field %q should have been dropped", field)
		}
	}
	if result.Data["name"] != "Alice" {
		t.Error("name field should remain unchanged")
	}
	if result.Data["email"] != "alice@test.com" {
		t.Error("email field should remain unchanged")
	}
}

func TestDropStage_NonexistentField(t *testing.T) {
	config := map[string]any{
		"fields": []any{"nonexistent"},
	}
	stage, err := NewDropStage("test", config)
	if err != nil {
		t.Fatalf("NewDropStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"name":  "Alice",
		"email": "alice@test.com",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should pass through without error
	if result.Data["name"] != "Alice" {
		t.Error("name field should remain unchanged")
	}
	if result.Data["email"] != "alice@test.com" {
		t.Error("email field should remain unchanged")
	}
}

func TestDropStage_NoFields_Error(t *testing.T) {
	config := map[string]any{}
	_, err := NewDropStage("test", config)
	if err == nil {
		t.Error("expected error for missing fields, got nil")
	}
}

func TestDropStage_Stats(t *testing.T) {
	config := map[string]any{
		"fields": []any{"secret"},
	}
	stage, err := NewDropStage("test", config)
	if err != nil {
		t.Fatalf("NewDropStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"secret": "value", "keep": "yes"}}
	_, err = stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	input, output, errors := stage.Stats()
	if input != 1 {
		t.Errorf("expected input count 1, got %d", input)
	}
	if output != 1 {
		t.Errorf("expected output count 1, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected error count 0, got %d", errors)
	}
}
