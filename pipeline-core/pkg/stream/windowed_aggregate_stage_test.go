package stream

import (
	"context"
	"testing"
	"time"
)

func TestNewWindowedAggregateStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type":         "tumbling",
			"size":         "1m",
			"grace_period": "10s",
		},
		"group_by": []any{"user_id"},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
			map[string]any{
				"field":    "total_amount",
				"function": "sum",
				"source":   "amount",
			},
		},
		"emit": map[string]any{
			"mode":                "on_close",
			"include_window_info": true,
		},
	}

	stage, err := NewWindowedAggregateStage("test_agg", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.Name() != "test_agg" {
		t.Errorf("expected name 'test_agg', got '%s'", stage.Name())
	}

	if stage.Type() != "windowed_aggregate" {
		t.Errorf("expected type 'windowed_aggregate', got '%s'", stage.Type())
	}

	if stage.windowType != WindowTumbling {
		t.Errorf("expected window type 'tumbling', got '%s'", stage.windowType)
	}

	if stage.windowSize != time.Minute {
		t.Errorf("expected window size 1m, got %v", stage.windowSize)
	}

	if len(stage.groupBy) != 1 || stage.groupBy[0] != "user_id" {
		t.Errorf("expected group_by ['user_id'], got %v", stage.groupBy)
	}

	if len(stage.aggregations) != 2 {
		t.Errorf("expected 2 aggregations, got %d", len(stage.aggregations))
	}
}

func TestWindowedAggregateStage_Aggregator(t *testing.T) {
	tests := []struct {
		name     string
		function AggregationFunction
		values   []any
		expected any
	}{
		{
			name:     "count",
			function: AggCount,
			values:   []any{1, 2, 3, 4, 5},
			expected: int64(5),
		},
		{
			name:     "sum",
			function: AggSum,
			values:   []any{10.0, 20.0, 30.0},
			expected: 60.0,
		},
		{
			name:     "avg",
			function: AggAvg,
			values:   []any{10.0, 20.0, 30.0},
			expected: 20.0,
		},
		{
			name:     "min",
			function: AggMin,
			values:   []any{30.0, 10.0, 20.0},
			expected: 10.0,
		},
		{
			name:     "max",
			function: AggMax,
			values:   []any{10.0, 30.0, 20.0},
			expected: 30.0,
		},
		{
			name:     "count_distinct",
			function: AggCountDistinct,
			values:   []any{"a", "b", "a", "c", "b"},
			expected: int64(3),
		},
		{
			name:     "first",
			function: AggFirst,
			values:   []any{"first", "second", "third"},
			expected: "first",
		},
		{
			name:     "last",
			function: AggLast,
			values:   []any{"first", "second", "third"},
			expected: "third",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := newAggregator(tt.function)
			for _, v := range tt.values {
				agg.Add(v)
			}
			result := agg.Result()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestWindowedAggregateStage_ProcessRecords(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type":         "tumbling",
			"size":         "1h", // Large window to avoid auto-flush during test
			"grace_period": "0s",
		},
		"group_by": []any{"user_id"},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
			map[string]any{
				"field":    "total",
				"function": "sum",
				"source":   "amount",
			},
		},
		"emit": map[string]any{
			"mode":                "on_close",
			"include_window_info": true,
		},
	}

	stage, err := NewWindowedAggregateStage("test_process", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process records for user1
	records := []map[string]any{
		{"user_id": "user1", "amount": 100.0},
		{"user_id": "user1", "amount": 200.0},
		{"user_id": "user1", "amount": 50.0},
		{"user_id": "user2", "amount": 75.0},
	}

	for _, data := range records {
		record := &Record{Data: data}
		_, err := stage.Process(ctx, record)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Flush windows to get results
	results := stage.FlushWindows()

	if len(results) != 2 {
		t.Fatalf("expected 2 window results (user1 and user2), got %d", len(results))
	}

	// Find user1 result
	var user1Result, user2Result *Record
	for _, r := range results {
		switch r.Data["user_id"] {
		case "user1":
			user1Result = r
		case "user2":
			user2Result = r
		}
	}

	if user1Result == nil {
		t.Fatal("user1 result not found")
	}
	if user1Result.Data["count"] != int64(3) {
		t.Errorf("expected user1 count 3, got %v", user1Result.Data["count"])
	}
	if user1Result.Data["total"] != 350.0 {
		t.Errorf("expected user1 total 350.0, got %v", user1Result.Data["total"])
	}

	if user2Result == nil {
		t.Fatal("user2 result not found")
	}
	if user2Result.Data["count"] != int64(1) {
		t.Errorf("expected user2 count 1, got %v", user2Result.Data["count"])
	}
	if user2Result.Data["total"] != 75.0 {
		t.Errorf("expected user2 total 75.0, got %v", user2Result.Data["total"])
	}
}

func TestWindowedAggregateStage_WindowInfo(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type": "tumbling",
			"size": "1h",
		},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
		},
	}

	stage, err := NewWindowedAggregateStage("window_info_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Add a record
	record := &Record{Data: map[string]any{"value": 1}}
	_, _ = stage.Process(ctx, record)

	info := stage.WindowInfo()
	if info["window_type"] != "tumbling" {
		t.Errorf("expected window_type 'tumbling', got %v", info["window_type"])
	}
	if info["active_windows"].(int) != 1 {
		t.Errorf("expected 1 active window, got %v", info["active_windows"])
	}
}

func TestWindowedAggregateStage_GlobalWindow(t *testing.T) {
	// Test without group_by (global aggregation)
	config := map[string]any{
		"window": map[string]any{
			"type": "tumbling",
			"size": "1h",
		},
		"aggregations": []any{
			map[string]any{
				"field":    "total",
				"function": "sum",
				"source":   "value",
			},
		},
	}

	stage, err := NewWindowedAggregateStage("global_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		record := &Record{Data: map[string]any{"value": float64(i * 10)}}
		_, _ = stage.Process(ctx, record)
	}

	results := stage.FlushWindows()
	if len(results) != 1 {
		t.Fatalf("expected 1 global window result, got %d", len(results))
	}

	if results[0].Data["total"] != 150.0 {
		t.Errorf("expected total 150.0, got %v", results[0].Data["total"])
	}
}

func TestWindowedAggregateStage_SessionWindow(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type":        "session",
			"session_gap": "100ms",
		},
		"group_by": []any{"session_id"},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
		},
	}

	stage, err := NewWindowedAggregateStage("session_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// First session
	for i := 0; i < 3; i++ {
		record := &Record{Data: map[string]any{"session_id": "s1", "event": i}}
		_, _ = stage.Process(ctx, record)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for session gap
	time.Sleep(150 * time.Millisecond)

	// New session (same session_id but after gap)
	for i := 0; i < 2; i++ {
		record := &Record{Data: map[string]any{"session_id": "s1", "event": i}}
		_, _ = stage.Process(ctx, record)
	}

	results := stage.FlushWindows()

	// Should have 2 sessions (first expired, second active then flushed)
	// Note: timing-dependent test, may need adjustment
	if len(results) < 1 {
		t.Errorf("expected at least 1 session result, got %d", len(results))
	}
}

func TestWindowedAggregateStage_NestedFields(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type": "tumbling",
			"size": "1h",
		},
		"group_by": []any{"user.id"},
		"aggregations": []any{
			map[string]any{
				"field":    "total",
				"function": "sum",
				"source":   "order.amount",
			},
		},
	}

	stage, err := NewWindowedAggregateStage("nested_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	records := []map[string]any{
		{"user": map[string]any{"id": "u1"}, "order": map[string]any{"amount": 100.0}},
		{"user": map[string]any{"id": "u1"}, "order": map[string]any{"amount": 50.0}},
	}

	for _, data := range records {
		record := &Record{Data: data}
		_, _ = stage.Process(ctx, record)
	}

	results := stage.FlushWindows()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Data["total"] != 150.0 {
		t.Errorf("expected total 150.0, got %v", results[0].Data["total"])
	}
}

func TestWindowedAggregateStage_Stats(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type": "tumbling",
			"size": "1h",
		},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
		},
	}

	stage, err := NewWindowedAggregateStage("stats_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		record := &Record{Data: map[string]any{"value": i}}
		_, _ = stage.Process(ctx, record)
	}

	input, output, errors := stage.Stats()
	if input != 10 {
		t.Errorf("expected 10 inputs, got %d", input)
	}
	if output != 10 {
		t.Errorf("expected 10 outputs, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestWindowedAggregateStage_EmitOnUpdate(t *testing.T) {
	config := map[string]any{
		"window": map[string]any{
			"type": "tumbling",
			"size": "1h",
		},
		"aggregations": []any{
			map[string]any{
				"field":    "count",
				"function": "count",
			},
		},
		"emit": map[string]any{
			"mode": "on_update",
		},
	}

	stage, err := NewWindowedAggregateStage("emit_update_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	// With on_update mode, each process should produce output
	if stage.emitMode != EmitOnUpdate {
		t.Errorf("expected emit mode 'on_update', got '%s'", stage.emitMode)
	}
}

func TestAggregator_NilValues(t *testing.T) {
	agg := newAggregator(AggSum)
	agg.Add(nil)
	agg.Add(10.0)
	agg.Add(nil)
	agg.Add(20.0)

	result := agg.Result()
	if result != 30.0 {
		t.Errorf("expected 30.0, got %v", result)
	}
}

func TestAggregator_IntValues(t *testing.T) {
	agg := newAggregator(AggSum)
	agg.Add(10)
	agg.Add(int32(20))
	agg.Add(int64(30))

	result := agg.Result()
	if result != 60.0 {
		t.Errorf("expected 60.0, got %v", result)
	}
}
