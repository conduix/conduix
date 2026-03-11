package stream

import (
	"context"
	"testing"
)

func TestBase64Stage_Encode(t *testing.T) {
	config := map[string]any{
		"fields": []any{"data"},
		"action": "encode",
	}

	stage, err := NewBase64Stage("test-encode", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"data": "hello world"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["data"] != "aGVsbG8gd29ybGQ=" {
		t.Errorf("expected aGVsbG8gd29ybGQ=, got %v", result.Data["data"])
	}
}

func TestBase64Stage_Decode(t *testing.T) {
	config := map[string]any{
		"fields": []any{"data"},
		"action": "decode",
	}

	stage, err := NewBase64Stage("test-decode", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"data": "aGVsbG8gd29ybGQ="}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["data"] != "hello world" {
		t.Errorf("expected hello world, got %v", result.Data["data"])
	}
}

func TestBase64Stage_DefaultEncode(t *testing.T) {
	config := map[string]any{
		"fields": []any{"value"},
	}

	stage, err := NewBase64Stage("test-default", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}
	if !stage.encode {
		t.Error("expected default action=encode")
	}
}

func TestBase64Stage_MultipleFields(t *testing.T) {
	config := map[string]any{
		"fields": []any{"a", "b"},
		"action": "encode",
	}

	stage, err := NewBase64Stage("test-multi", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"a": "foo", "b": "bar"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result.Data["a"] != "Zm9v" {
		t.Errorf("expected Zm9v, got %v", result.Data["a"])
	}
	if result.Data["b"] != "YmFy" {
		t.Errorf("expected YmFy, got %v", result.Data["b"])
	}
}

func TestBase64Stage_SkipMissingField(t *testing.T) {
	config := map[string]any{
		"fields": []any{"missing"},
		"action": "encode",
	}

	stage, err := NewBase64Stage("test-skip", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"other": "value"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if _, exists := result.Data["missing"]; exists {
		t.Error("missing field should not be created")
	}
}

func TestBase64Stage_InvalidDecodeInput(t *testing.T) {
	config := map[string]any{
		"fields": []any{"data"},
		"action": "decode",
	}

	stage, err := NewBase64Stage("test-invalid", config)
	if err != nil {
		t.Fatalf("NewBase64Stage failed: %v", err)
	}

	record := &Record{Data: map[string]any{"data": "!!!invalid!!!"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	// invalid input → field unchanged, error counted
	if result.Data["data"] != "!!!invalid!!!" {
		t.Errorf("expected original value on decode error, got %v", result.Data["data"])
	}
}

func TestBase64Stage_NoFields_Error(t *testing.T) {
	config := map[string]any{}
	_, err := NewBase64Stage("test-no-fields", config)
	if err == nil {
		t.Error("expected error for missing fields")
	}
}
