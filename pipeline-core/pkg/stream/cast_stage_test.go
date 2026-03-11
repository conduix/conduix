package stream

import (
	"context"
	"testing"
	"time"
)

func TestCastStage_StringToInt(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"age": "int",
		},
	}

	stage := NewCastStage("test_cast_int", config)

	record := &Record{
		Data: map[string]any{
			"age": "25",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["age"] != int64(25) {
		t.Errorf("expected int64(25), got %v (%T)", result.Data["age"], result.Data["age"])
	}
}

func TestCastStage_FloatToInt(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"count": "int",
		},
	}

	stage := NewCastStage("test_cast_float_to_int", config)

	record := &Record{
		Data: map[string]any{
			"count": 42.7,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["count"] != int64(42) {
		t.Errorf("expected int64(42), got %v (%T)", result.Data["count"], result.Data["count"])
	}
}

func TestCastStage_StringToFloat(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"price": "float",
		},
	}

	stage := NewCastStage("test_cast_float", config)

	record := &Record{
		Data: map[string]any{
			"price": "19.99",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["price"] != 19.99 {
		t.Errorf("expected 19.99, got %v", result.Data["price"])
	}
}

func TestCastStage_StringToBool(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"active": "bool",
		},
	}

	stage := NewCastStage("test_cast_bool", config)

	record := &Record{
		Data: map[string]any{
			"active": "true",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["active"] != true {
		t.Errorf("expected true, got %v", result.Data["active"])
	}
}

func TestCastStage_DateParsing(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"created_at": "date",
		},
		"date_format": "2006-01-02",
	}

	stage := NewCastStage("test_cast_date", config)

	record := &Record{
		Data: map[string]any{
			"created_at": "2024-06-15",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, ok := result.Data["created_at"].(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", result.Data["created_at"])
	}
	if parsed.Year() != 2024 || parsed.Month() != 6 || parsed.Day() != 15 {
		t.Errorf("expected 2024-06-15, got %v", parsed)
	}
}

func TestCastStage_ErrorActionNull(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"age": "int",
		},
		"error_action": "null",
	}

	stage := NewCastStage("test_cast_null", config)

	record := &Record{
		Data: map[string]any{
			"age": "not_a_number",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["age"] != nil {
		t.Errorf("expected nil, got %v", result.Data["age"])
	}
}

func TestCastStage_ErrorActionDrop(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"age": "int",
		},
		"error_action": "drop",
	}

	stage := NewCastStage("test_cast_drop", config)

	record := &Record{
		Data: map[string]any{
			"age": "invalid",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil (dropped record)")
	}
}

func TestCastStage_ErrorActionKeep(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"age": "int",
		},
		"error_action": "keep",
	}

	stage := NewCastStage("test_cast_keep", config)

	record := &Record{
		Data: map[string]any{
			"age": "not_a_number",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["age"] != "not_a_number" {
		t.Errorf("expected original value 'not_a_number', got %v", result.Data["age"])
	}
}

func TestCastStage_MultipleCasts(t *testing.T) {
	config := map[string]any{
		"casts": map[string]any{
			"age":    "int",
			"price":  "float",
			"active": "bool",
			"name":   "string",
		},
	}

	stage := NewCastStage("test_cast_multi", config)

	record := &Record{
		Data: map[string]any{
			"age":    "30",
			"price":  "9.99",
			"active": "true",
			"name":   12345,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data["age"] != int64(30) {
		t.Errorf("age: expected int64(30), got %v (%T)", result.Data["age"], result.Data["age"])
	}
	if result.Data["price"] != 9.99 {
		t.Errorf("price: expected 9.99, got %v", result.Data["price"])
	}
	if result.Data["active"] != true {
		t.Errorf("active: expected true, got %v", result.Data["active"])
	}
	if result.Data["name"] != "12345" {
		t.Errorf("name: expected '12345', got %v", result.Data["name"])
	}
}
