package stream

import (
	"context"
	"testing"
	"time"
)

func TestNewStreamJoinStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"join_type":    "inner",
		"left_key":     "user_id",
		"right_key":    "customer_id",
		"left_stream":  "orders",
		"right_stream": "customers",
		"window": map[string]any{
			"before": "10s",
			"after":  "10s",
		},
	}

	stage, err := NewStreamJoinStage("test_join", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	if stage.Name() != "test_join" {
		t.Errorf("expected name 'test_join', got '%s'", stage.Name())
	}

	if stage.Type() != "stream_join" {
		t.Errorf("expected type 'stream_join', got '%s'", stage.Type())
	}

	if stage.joinType != JoinInner {
		t.Errorf("expected join type 'inner', got '%s'", stage.joinType)
	}

	if stage.leftKey != "user_id" {
		t.Errorf("expected left key 'user_id', got '%s'", stage.leftKey)
	}

	if stage.rightKey != "customer_id" {
		t.Errorf("expected right key 'customer_id', got '%s'", stage.rightKey)
	}
}

func TestStreamJoinStage_InnerJoin(t *testing.T) {
	config := map[string]any{
		"join_type":    "inner",
		"left_key":     "id",
		"right_key":    "id",
		"left_stream":  "left",
		"right_stream": "right",
		"left_prefix":  "l_",
		"right_prefix": "r_",
		"window": map[string]any{
			"before": "1m",
			"after":  "1m",
		},
	}

	stage, err := NewStreamJoinStage("inner_join", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left record
	leftRecord := &Record{Data: map[string]any{
		"id":     123,
		"name":   "Order A",
		"amount": 100.0,
	}}
	result, _ := stage.ProcessLeft(ctx, leftRecord)

	// Inner join: should not emit yet (no matching right record)
	if result != nil {
		t.Error("expected nil result for inner join without match")
	}

	// Process matching right record
	rightRecord := &Record{Data: map[string]any{
		"id":      123,
		"product": "Widget",
		"qty":     5,
	}}
	result, _ = stage.ProcessRight(ctx, rightRecord)

	// Should emit joined record
	if result == nil {
		t.Fatal("expected joined result")
	}

	// Check prefixed fields
	if result.Data["l_name"] != "Order A" {
		t.Errorf("expected l_name 'Order A', got '%v'", result.Data["l_name"])
	}
	if result.Data["r_product"] != "Widget" {
		t.Errorf("expected r_product 'Widget', got '%v'", result.Data["r_product"])
	}
}

func TestStreamJoinStage_LeftJoin(t *testing.T) {
	config := map[string]any{
		"join_type":    "left",
		"left_key":     "id",
		"left_stream":  "left",
		"right_stream": "right",
		"window": map[string]any{
			"before": "1s",
			"after":  "1s",
		},
	}

	stage, err := NewStreamJoinStage("left_join", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left record with no matching right
	leftRecord := &Record{Data: map[string]any{
		"id":   999,
		"name": "Orphan Order",
	}}
	result, _ := stage.ProcessLeft(ctx, leftRecord)

	// Left join: should emit even without match
	if result == nil {
		t.Fatal("expected result for left join without match")
	}

	if result.Data["left_name"] != "Orphan Order" {
		t.Errorf("expected left_name 'Orphan Order', got '%v'", result.Data["left_name"])
	}
}

func TestStreamJoinStage_RightJoin(t *testing.T) {
	config := map[string]any{
		"join_type":    "right",
		"left_key":     "id",
		"left_stream":  "left",
		"right_stream": "right",
		"window": map[string]any{
			"before": "1s",
			"after":  "1s",
		},
	}

	stage, err := NewStreamJoinStage("right_join", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process right record with no matching left
	rightRecord := &Record{Data: map[string]any{
		"id":      888,
		"product": "Orphan Product",
	}}
	result, _ := stage.ProcessRight(ctx, rightRecord)

	// Right join: should emit even without match
	if result == nil {
		t.Fatal("expected result for right join without match")
	}

	if result.Data["right_product"] != "Orphan Product" {
		t.Errorf("expected right_product 'Orphan Product', got '%v'", result.Data["right_product"])
	}
}

func TestStreamJoinStage_OuterJoin(t *testing.T) {
	config := map[string]any{
		"join_type":    "outer",
		"left_key":     "id",
		"left_stream":  "left",
		"right_stream": "right",
		"window": map[string]any{
			"before": "1s",
			"after":  "1s",
		},
	}

	stage, err := NewStreamJoinStage("outer_join", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left without match
	leftRecord := &Record{Data: map[string]any{
		"id":   1,
		"name": "Left Only",
	}}
	result1, _ := stage.ProcessLeft(ctx, leftRecord)

	if result1 == nil {
		t.Error("outer join should emit unmatched left record")
	}

	// Process right without match
	rightRecord := &Record{Data: map[string]any{
		"id":      2,
		"product": "Right Only",
	}}
	result2, _ := stage.ProcessRight(ctx, rightRecord)

	if result2 == nil {
		t.Error("outer join should emit unmatched right record")
	}
}

func TestStreamJoinStage_WindowExpiry(t *testing.T) {
	config := map[string]any{
		"join_type": "inner",
		"left_key":  "id",
		"window": map[string]any{
			"before": "1ms",
			"after":  "1ms",
		},
	}

	stage, err := NewStreamJoinStage("window_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left record
	leftRecord := &Record{Data: map[string]any{
		"id":   123,
		"name": "Old Order",
	}}
	_, _ = stage.ProcessLeft(ctx, leftRecord)

	// Wait for window to expire
	time.Sleep(10 * time.Millisecond)

	// Process right record (outside window)
	rightRecord := &Record{Data: map[string]any{
		"id":      123,
		"product": "New Product",
	}}
	result, _ := stage.ProcessRight(ctx, rightRecord)

	// Should not match due to window expiry
	if result != nil {
		t.Error("expected no match after window expiry")
	}
}

func TestStreamJoinStage_JoinInfo(t *testing.T) {
	config := map[string]any{
		"join_type":    "inner",
		"left_key":     "user_id",
		"right_key":    "customer_id",
		"left_stream":  "orders",
		"right_stream": "customers",
	}

	stage, err := NewStreamJoinStage("info_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	info := stage.JoinInfo()

	if info["join_type"] != "inner" {
		t.Errorf("expected join_type 'inner', got '%v'", info["join_type"])
	}
	if info["left_key"] != "user_id" {
		t.Errorf("expected left_key 'user_id', got '%v'", info["left_key"])
	}
	if info["right_key"] != "customer_id" {
		t.Errorf("expected right_key 'customer_id', got '%v'", info["right_key"])
	}
}

func TestStreamJoinStage_NoPrefix(t *testing.T) {
	config := map[string]any{
		"join_type":    "inner",
		"left_key":     "id",
		"left_prefix":  "",
		"right_prefix": "",
		"window": map[string]any{
			"before": "1m",
			"after":  "1m",
		},
	}

	stage, err := NewStreamJoinStage("no_prefix", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left
	leftRecord := &Record{Data: map[string]any{
		"id":   1,
		"name": "Left Name",
	}}
	_, _ = stage.ProcessLeft(ctx, leftRecord)

	// Process right
	rightRecord := &Record{Data: map[string]any{
		"id":      1,
		"product": "Right Product",
	}}
	result, _ := stage.ProcessRight(ctx, rightRecord)

	if result == nil {
		t.Fatal("expected joined result")
	}

	// Without prefix, fields should be at root level
	if result.Data["name"] != "Left Name" {
		t.Errorf("expected name 'Left Name', got '%v'", result.Data["name"])
	}
	if result.Data["product"] != "Right Product" {
		t.Errorf("expected product 'Right Product', got '%v'", result.Data["product"])
	}
}

func TestStreamJoinStage_OutputFields(t *testing.T) {
	config := map[string]any{
		"join_type":     "inner",
		"left_key":      "id",
		"left_prefix":   "l_",
		"right_prefix":  "r_",
		"output_fields": []any{"l_name", "r_product"},
		"window": map[string]any{
			"before": "1m",
			"after":  "1m",
		},
	}

	stage, err := NewStreamJoinStage("output_fields_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left
	leftRecord := &Record{Data: map[string]any{
		"id":     1,
		"name":   "Order",
		"amount": 100.0,
	}}
	_, _ = stage.ProcessLeft(ctx, leftRecord)

	// Process right
	rightRecord := &Record{Data: map[string]any{
		"id":      1,
		"product": "Widget",
		"price":   19.99,
	}}
	result, _ := stage.ProcessRight(ctx, rightRecord)

	if result == nil {
		t.Fatal("expected joined result")
	}

	// Only specified fields should be included
	if result.Data["l_name"] != "Order" {
		t.Errorf("expected l_name 'Order', got '%v'", result.Data["l_name"])
	}
	if result.Data["r_product"] != "Widget" {
		t.Errorf("expected r_product 'Widget', got '%v'", result.Data["r_product"])
	}

	// Other fields should be excluded
	if _, exists := result.Data["l_amount"]; exists {
		t.Error("l_amount should not be in output")
	}
	if _, exists := result.Data["r_price"]; exists {
		t.Error("r_price should not be in output")
	}
}

func TestStreamJoinStage_MissingLeftKey(t *testing.T) {
	config := map[string]any{
		"join_type": "inner",
		// left_key is missing
	}

	_, err := NewStreamJoinStage("missing_key", config)
	if err == nil {
		t.Error("expected error for missing left_key")
	}
}

func TestStreamJoinStage_InvalidJoinType(t *testing.T) {
	config := map[string]any{
		"join_type": "invalid",
		"left_key":  "id",
	}

	_, err := NewStreamJoinStage("invalid_type", config)
	if err == nil {
		t.Error("expected error for invalid join_type")
	}
}

func TestStreamJoinStage_Stats(t *testing.T) {
	config := map[string]any{
		"join_type": "outer",
		"left_key":  "id",
		"window": map[string]any{
			"before": "1m",
			"after":  "1m",
		},
	}

	stage, err := NewStreamJoinStage("stats_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process some records
	for i := 0; i < 5; i++ {
		record := &Record{Data: map[string]any{"id": i, "value": i * 10}}
		_, _ = stage.ProcessLeft(ctx, record)
	}

	input, output, errors := stage.Stats()
	if input != 5 {
		t.Errorf("expected 5 inputs, got %d", input)
	}
	if output != 5 { // outer join emits all
		t.Errorf("expected 5 outputs, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestJoinBuffer_AddAndFind(t *testing.T) {
	buffer := newJoinBuffer(time.Minute)

	now := time.Now()
	record1 := &Record{Data: map[string]any{"value": 1}}
	record2 := &Record{Data: map[string]any{"value": 2}}
	record3 := &Record{Data: map[string]any{"value": 3}}

	buffer.Add(record1, "key1", now)
	buffer.Add(record2, "key1", now.Add(time.Second))
	buffer.Add(record3, "key2", now.Add(2*time.Second))

	// Find all for key1
	matches := buffer.FindMatches("key1", now.Add(-time.Minute), now.Add(time.Minute))
	if len(matches) != 2 {
		t.Errorf("expected 2 matches for key1, got %d", len(matches))
	}

	// Find for key2
	matches = buffer.FindMatches("key2", now.Add(-time.Minute), now.Add(time.Minute))
	if len(matches) != 1 {
		t.Errorf("expected 1 match for key2, got %d", len(matches))
	}

	// Find for non-existent key
	matches = buffer.FindMatches("key3", now.Add(-time.Minute), now.Add(time.Minute))
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for key3, got %d", len(matches))
	}
}

func TestJoinBuffer_Cleanup(t *testing.T) {
	buffer := newJoinBuffer(time.Second)

	now := time.Now()
	oldRecord := &Record{Data: map[string]any{"value": "old"}}
	newRecord := &Record{Data: map[string]any{"value": "new"}}

	buffer.Add(oldRecord, "key1", now.Add(-time.Hour))
	buffer.Add(newRecord, "key1", now)

	// Cleanup old records
	cutoff := now.Add(-time.Minute)
	buffer.Cleanup(cutoff)

	// Only new record should remain
	matches := buffer.FindMatches("key1", now.Add(-time.Hour), now.Add(time.Hour))
	if len(matches) != 1 {
		t.Errorf("expected 1 match after cleanup, got %d", len(matches))
	}
	if matches[0].Data["value"] != "new" {
		t.Errorf("expected 'new' record to remain")
	}
}

func TestStreamJoinStage_FlushPending(t *testing.T) {
	config := map[string]any{
		"join_type": "left",
		"left_key":  "id",
		"window": map[string]any{
			"before": "1h", // Long window to prevent auto-match
			"after":  "1h",
		},
	}

	stage, err := NewStreamJoinStage("flush_test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stage.Close() }()

	ctx := context.Background()

	// Process left records that won't match
	for i := 0; i < 3; i++ {
		record := &Record{Data: map[string]any{"id": i, "name": "Unmatched"}}
		_, _ = stage.ProcessLeft(ctx, record)
	}

	// Flush pending
	results := stage.FlushPending()

	if len(results) != 3 {
		t.Errorf("expected 3 flushed records, got %d", len(results))
	}
}

func TestIsInternalField(t *testing.T) {
	tests := []struct {
		field    string
		expected bool
	}{
		{"_timestamp", true},
		{"_stream", true},
		{"name", false},
		{"user_id", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isInternalField(tt.field)
		if result != tt.expected {
			t.Errorf("isInternalField(%q) = %v, expected %v", tt.field, result, tt.expected)
		}
	}
}
