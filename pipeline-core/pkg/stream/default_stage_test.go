package stream

import (
	"context"
	"testing"
)

func TestDefaultStage_SetNilFields(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"status":   "pending",
			"priority": 0,
		},
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"name":   "Alice",
		"status": nil,
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Data["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", result.Data["status"])
	}
	if result.Data["priority"] != 0 {
		t.Errorf("expected priority 0, got %v", result.Data["priority"])
	}
	if result.Data["name"] != "Alice" {
		t.Error("name should remain unchanged")
	}
}

func TestDefaultStage_MissingField(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"region": "us-east-1",
		},
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	// Record has no "region" field at all
	record := &Record{Data: map[string]any{
		"name": "Bob",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Data["region"] != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %v", result.Data["region"])
	}
}

func TestDefaultStage_OnlyNull_True_SkipsEmptyString(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"status": "active",
		},
		"only_null": true,
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"status": "", // empty string, but only_null=true -> keep empty
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// only_null=true: empty string should NOT be replaced
	if result.Data["status"] != "" {
		t.Errorf("expected empty string (only_null=true), got %v", result.Data["status"])
	}
}

func TestDefaultStage_OnlyNull_False_ReplacesEmptyString(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"status": "active",
		},
		"only_null": false,
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"status": "", // empty string, only_null=false -> replace
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Data["status"] != "active" {
		t.Errorf("expected 'active', got %v", result.Data["status"])
	}
}

func TestDefaultStage_DoNotOverrideExistingValue(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"status": "pending",
		},
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{
		"status": "active", // already has a value
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should NOT override existing non-nil value
	if result.Data["status"] != "active" {
		t.Errorf("expected 'active' (existing value), got %v", result.Data["status"])
	}
}

func TestDefaultStage_NoDefaults_Error(t *testing.T) {
	config := map[string]any{}
	_, err := NewDefaultStage("test", config)
	if err == nil {
		t.Error("expected error for missing defaults, got nil")
	}
}

func TestDefaultStage_Stats(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"x": 1,
		},
	}
	stage, err := NewDefaultStage("test", config)
	if err != nil {
		t.Fatalf("NewDefaultStage failed: %v", err)
	}

	record := &Record{Data: map[string]any{}}
	_, err = stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	input, output, errors := stage.Stats()
	if input != 1 || output != 1 || errors != 0 {
		t.Errorf("expected stats (1,1,0), got (%d,%d,%d)", input, output, errors)
	}
}
