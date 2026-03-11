package stream

import (
	"context"
	"testing"
)

func TestMergeStage_DelimiterMode(t *testing.T) {
	config := map[string]any{
		"source_fields": []any{"first_name", "last_name"},
		"target_field":  "full_name",
		"delimiter":     " ",
	}

	stage := NewMergeStage("test_merge", config)

	record := &Record{
		Data: map[string]any{
			"first_name": "John",
			"last_name":  "Doe",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Data["full_name"] != "John Doe" {
		t.Errorf("expected 'John Doe', got '%v'", result.Data["full_name"])
	}
}

func TestMergeStage_TemplateMode(t *testing.T) {
	config := map[string]any{
		"source_fields": []any{"first_name", "last_name"},
		"target_field":  "greeting",
		"template":      "Hello, {{first_name}} {{last_name}}!",
	}

	stage := NewMergeStage("test_merge_template", config)

	record := &Record{
		Data: map[string]any{
			"first_name": "Jane",
			"last_name":  "Smith",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["greeting"] != "Hello, Jane Smith!" {
		t.Errorf("expected 'Hello, Jane Smith!', got '%v'", result.Data["greeting"])
	}
}

func TestMergeStage_CustomDelimiter(t *testing.T) {
	config := map[string]any{
		"source_fields": []any{"city", "state", "country"},
		"target_field":  "address",
		"delimiter":     ", ",
	}

	stage := NewMergeStage("test_merge_custom", config)

	record := &Record{
		Data: map[string]any{
			"city":    "Seoul",
			"state":   "Gangnam",
			"country": "Korea",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["address"] != "Seoul, Gangnam, Korea" {
		t.Errorf("expected 'Seoul, Gangnam, Korea', got '%v'", result.Data["address"])
	}
}

func TestMergeStage_MissingTargetField(t *testing.T) {
	config := map[string]any{
		"source_fields": []any{"a", "b"},
	}

	stage := NewMergeStage("test_merge_no_target", config)

	record := &Record{
		Data: map[string]any{"a": "1", "b": "2"},
	}

	_, err := stage.Process(context.Background(), record)
	if err == nil {
		t.Fatal("expected error for missing target_field")
	}
}

func TestMergeStage_MissingSourceField(t *testing.T) {
	config := map[string]any{
		"source_fields": []any{"first_name", "middle_name", "last_name"},
		"target_field":  "full_name",
		"delimiter":     " ",
	}

	stage := NewMergeStage("test_merge_partial", config)

	record := &Record{
		Data: map[string]any{
			"first_name": "John",
			"last_name":  "Doe",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// middle_name 없으므로 "John Doe" (skip missing)
	if result.Data["full_name"] != "John Doe" {
		t.Errorf("expected 'John Doe', got '%v'", result.Data["full_name"])
	}
}
