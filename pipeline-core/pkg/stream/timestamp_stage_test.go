package stream

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTimestampStage_Add(t *testing.T) {
	stage, err := NewTimestampStage("ts-add", map[string]any{
		"action":       "add",
		"target_field": "processed_at",
		"timezone":     "Asia/Seoul",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{Data: map[string]any{"msg": "hello"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected record, got nil")
	}

	ts, ok := result.Data["processed_at"].(string)
	if !ok {
		t.Fatal("processed_at not set or not a string")
	}

	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("failed to parse output timestamp: %v", err)
	}

	// Seoul은 UTC+9
	if parsed.Location().String() == "UTC" {
		// RFC3339 파싱 시 offset으로 표현되므로 zone name 대신 offset 확인
		_, offset := parsed.Zone()
		if offset != 9*3600 {
			t.Errorf("expected +09:00 offset, got %d", offset)
		}
	}
}

func TestTimestampStage_Convert(t *testing.T) {
	stage, err := NewTimestampStage("ts-convert", map[string]any{
		"action":       "convert",
		"target_field": "converted_at",
		"source_field": "created_at",
		"input_format": "2006-01-02 15:04:05",
		"timezone":     "America/New_York",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{Data: map[string]any{
		"created_at": "2024-06-15 12:30:00",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected record, got nil")
	}

	converted, ok := result.Data["converted_at"].(string)
	if !ok {
		t.Fatal("converted_at not set")
	}

	// 출력은 RFC3339 형식이어야 함
	if !strings.Contains(converted, "T") {
		t.Errorf("expected RFC3339 format, got %s", converted)
	}

	parsed, err := time.Parse(time.RFC3339, converted)
	if err != nil {
		t.Fatalf("failed to parse converted timestamp: %v", err)
	}

	_, offset := parsed.Zone()
	// New York은 EDT(-4) 또는 EST(-5)
	if offset != -4*3600 && offset != -5*3600 {
		t.Errorf("expected New York offset, got %d", offset)
	}
}

func TestTimestampStage_Format(t *testing.T) {
	stage, err := NewTimestampStage("ts-format", map[string]any{
		"action":        "format",
		"target_field":  "formatted_date",
		"source_field":  "timestamp",
		"output_format": "2006-01-02",
		"timezone":      "UTC",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{Data: map[string]any{
		"timestamp": "2024-06-15T12:30:00+09:00",
	}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected record, got nil")
	}

	formatted, ok := result.Data["formatted_date"].(string)
	if !ok {
		t.Fatal("formatted_date not set")
	}

	// +09:00에서 UTC로 변환하면 2024-06-15 03:30:00 UTC → 날짜는 2024-06-15
	if formatted != "2024-06-15" {
		t.Errorf("expected 2024-06-15, got %s", formatted)
	}
}

func TestTimestampStage_MissingTargetField(t *testing.T) {
	_, err := NewTimestampStage("ts-err", map[string]any{
		"action": "add",
	})
	if err == nil {
		t.Fatal("expected error for missing target_field")
	}
}

func TestTimestampStage_ConvertMissingSource(t *testing.T) {
	stage, err := NewTimestampStage("ts-missing", map[string]any{
		"action":       "convert",
		"target_field": "out",
		"source_field": "nonexistent",
		"input_format": time.RFC3339,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	record := &Record{Data: map[string]any{"other": "value"}}
	_, err = stage.Process(context.Background(), record)
	if err == nil {
		t.Fatal("expected error for missing source field")
	}
}

func TestTimestampStage_Stats(t *testing.T) {
	stage, err := NewTimestampStage("ts-stats", map[string]any{
		"action":       "add",
		"target_field": "ts",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	_, _ = stage.Process(context.Background(), &Record{Data: map[string]any{}})
	_, _ = stage.Process(context.Background(), &Record{Data: map[string]any{}})

	input, output, errors := stage.Stats()
	if input != 2 || output != 2 || errors != 0 {
		t.Errorf("expected stats 2/2/0, got %d/%d/%d", input, output, errors)
	}
}
