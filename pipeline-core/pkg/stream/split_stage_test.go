package stream

import (
	"context"
	"testing"
)

func TestSplitStage_BasicSplit(t *testing.T) {
	config := map[string]any{
		"source_field":  "full_name",
		"pattern":       `^(\w+)\s+(\w+)$`,
		"target_fields": []any{"first_name", "last_name"},
	}

	stage, err := NewSplitStage("test_split", config)
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{
		Data: map[string]any{
			"full_name": "John Doe",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["first_name"] != "John" {
		t.Errorf("expected first_name='John', got '%v'", result.Data["first_name"])
	}
	if result.Data["last_name"] != "Doe" {
		t.Errorf("expected last_name='Doe', got '%v'", result.Data["last_name"])
	}
	// source field should be deleted (keep_original defaults to false)
	if _, ok := result.Data["full_name"]; ok {
		t.Error("expected full_name to be deleted")
	}
}

func TestSplitStage_KeepOriginal(t *testing.T) {
	config := map[string]any{
		"source_field":  "full_name",
		"pattern":       `^(\w+)\s+(\w+)$`,
		"target_fields": []any{"first_name", "last_name"},
		"keep_original": true,
	}

	stage, err := NewSplitStage("test_split_keep", config)
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{
		Data: map[string]any{
			"full_name": "Jane Smith",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["first_name"] != "Jane" {
		t.Errorf("expected first_name='Jane', got '%v'", result.Data["first_name"])
	}
	if result.Data["full_name"] != "Jane Smith" {
		t.Errorf("expected full_name to be kept, got '%v'", result.Data["full_name"])
	}
}

func TestSplitStage_NoMatch(t *testing.T) {
	config := map[string]any{
		"source_field":  "email",
		"pattern":       `^(\w+)@(\w+)\.(\w+)$`,
		"target_fields": []any{"user", "domain", "tld"},
	}

	stage, err := NewSplitStage("test_split_nomatch", config)
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{
		Data: map[string]any{
			"email": "not-an-email",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No match — record passes through unchanged
	if result.Data["email"] != "not-an-email" {
		t.Errorf("expected email to remain unchanged")
	}
	if _, ok := result.Data["user"]; ok {
		t.Error("expected no 'user' field on no-match")
	}
}

func TestSplitStage_InvalidRegex(t *testing.T) {
	config := map[string]any{
		"source_field":  "data",
		"pattern":       `[invalid`,
		"target_fields": []any{"a"},
	}

	_, err := NewSplitStage("test_split_bad_regex", config)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestSplitStage_MissingSourceField(t *testing.T) {
	config := map[string]any{
		"source_field":  "nonexistent",
		"pattern":       `(.+)`,
		"target_fields": []any{"result"},
	}

	stage, err := NewSplitStage("test_split_missing", config)
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{
		Data: map[string]any{
			"other_field": "value",
		},
	}

	_, err = stage.Process(context.Background(), record)
	if err == nil {
		t.Fatal("expected error for missing source field")
	}
}
