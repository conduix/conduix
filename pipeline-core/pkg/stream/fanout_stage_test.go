package stream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// testStage is a simple stage for testing
type testStage struct {
	name     string
	typ      string
	addField string
	addValue any
	delay    time.Duration
	fail     bool
	filter   bool // return nil record
}

func (s *testStage) Name() string { return s.name }
func (s *testStage) Type() string { return s.typ }
func (s *testStage) Close() error { return nil }

func (s *testStage) Process(ctx context.Context, record *Record) (*Record, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if s.fail {
		return nil, errors.New("intentional failure")
	}

	if s.filter {
		return nil, nil
	}

	if s.addField != "" {
		record.Data[s.addField] = s.addValue
	}

	return record, nil
}

func TestFanOutStageBasic(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "branch_a",
				Stages: []Stage{
					&testStage{name: "add_a", typ: "test", addField: "enriched_a", addValue: "value_a"},
				},
			},
			{
				Name: "branch_b",
				Stages: []Stage{
					&testStage{name: "add_b", typ: "test", addField: "enriched_b", addValue: "value_b"},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
	}

	stage, err := NewFanOutStage("test_fanout", cfg)
	if err != nil {
		t.Fatalf("Failed to create fan-out stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{"original": "data"}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Check original data preserved
	if result.Data["original"] != "data" {
		t.Error("Original data not preserved")
	}

	// Check both branches' enrichments
	if result.Data["enriched_a"] != "value_a" {
		t.Error("Branch A enrichment not merged")
	}
	if result.Data["enriched_b"] != "value_b" {
		t.Error("Branch B enrichment not merged")
	}
}

func TestFanOutStageSequential(t *testing.T) {
	var order []string
	var orderMu atomic.Value
	orderMu.Store([]string{})

	// Create stages that track execution order
	branchAStage := &testStage{
		name:     "branch_a_stage",
		typ:      "test",
		addField: "a",
		addValue: 1,
	}
	branchBStage := &testStage{
		name:     "branch_b_stage",
		typ:      "test",
		addField: "b",
		addValue: 2,
	}

	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{Name: "branch_a", Stages: []Stage{branchAStage}},
			{Name: "branch_b", Stages: []Stage{branchBStage}},
		},
		MergeStrategy: MergeDeep,
		Parallel:      false, // Sequential execution
	}

	stage, err := NewFanOutStage("test_sequential", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{"original": true}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result")
	}

	// Both branches should have contributed
	if result.Data["a"] != 1 || result.Data["b"] != 2 {
		t.Errorf("Expected both branch values, got a=%v, b=%v", result.Data["a"], result.Data["b"])
	}

	_ = order // Suppress unused variable warning
}

func TestFanOutStageParallelExecution(t *testing.T) {
	// Create stages with different delays to verify parallel execution
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "slow_branch",
				Stages: []Stage{
					&testStage{name: "slow", typ: "test", delay: 100 * time.Millisecond, addField: "slow", addValue: true},
				},
			},
			{
				Name: "fast_branch",
				Stages: []Stage{
					&testStage{name: "fast", typ: "test", delay: 10 * time.Millisecond, addField: "fast", addValue: true},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
	}

	stage, err := NewFanOutStage("test_parallel", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	start := time.Now()
	result, err := stage.Process(ctx, record)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Parallel execution should take ~100ms (max of both), not ~110ms (sum)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Parallel execution took too long: %v (expected < 200ms)", elapsed)
	}

	// Both results should be present
	if result.Data["slow"] != true || result.Data["fast"] != true {
		t.Error("Not all branch results were merged")
	}
}

func TestFanOutStageBranchError(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "success_branch",
				Stages: []Stage{
					&testStage{name: "success", typ: "test", addField: "success", addValue: true},
				},
			},
			{
				Name: "fail_branch",
				Stages: []Stage{
					&testStage{name: "fail", typ: "test", fail: true},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
		FailOnError:   false, // Continue despite errors
	}

	stage, err := NewFanOutStage("test_error", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Errorf("Process should not fail when FailOnError=false: %v", err)
	}

	// Success branch should still work
	if result == nil || result.Data["success"] != true {
		t.Error("Success branch result should be present")
	}
}

func TestFanOutStageFailOnError(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "fail_branch",
				Stages: []Stage{
					&testStage{name: "fail", typ: "test", fail: true},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
		FailOnError:   true, // Should fail on any error
	}

	stage, err := NewFanOutStage("test_fail_on_error", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	_, err = stage.Process(ctx, record)
	if err == nil {
		t.Error("Expected error when FailOnError=true and branch fails")
	}
}

func TestFanOutStageMergeArray(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "branch_1",
				Stages: []Stage{
					&testStage{name: "add_1", typ: "test", addField: "value", addValue: 1},
				},
			},
			{
				Name: "branch_2",
				Stages: []Stage{
					&testStage{name: "add_2", typ: "test", addField: "value", addValue: 2},
				},
			},
		},
		MergeStrategy: MergeArray,
		Parallel:      true,
	}

	stage, err := NewFanOutStage("test_array", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{"original": true}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	branchResults, ok := result.Data["_branch_results"].([]map[string]any)
	if !ok {
		t.Fatal("Expected _branch_results array")
	}

	if len(branchResults) != 2 {
		t.Errorf("Expected 2 branch results, got %d", len(branchResults))
	}
}

func TestFanOutStageMergeFirst(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "first_branch",
				Stages: []Stage{
					&testStage{name: "first", typ: "test", addField: "from", addValue: "first"},
				},
			},
			{
				Name: "second_branch",
				Stages: []Stage{
					&testStage{name: "second", typ: "test", addField: "from", addValue: "second"},
				},
			},
		},
		MergeStrategy: MergeFirst,
		Parallel:      false, // Sequential to ensure order
	}

	stage, err := NewFanOutStage("test_first", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// First should win
	if result.Data["from"] != "first" {
		t.Errorf("Expected 'from' to be 'first', got %v", result.Data["from"])
	}
}

func TestFanOutStageTimeout(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "slow_branch",
				Stages: []Stage{
					&testStage{name: "slow", typ: "test", delay: 5 * time.Second},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
		Timeout:       100 * time.Millisecond, // Short timeout
	}

	stage, err := NewFanOutStage("test_timeout", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	start := time.Now()
	_, _ = stage.Process(ctx, record)
	elapsed := time.Since(start)

	// Should complete quickly due to timeout
	if elapsed > 500*time.Millisecond {
		t.Errorf("Timeout not working, elapsed: %v", elapsed)
	}
}

func TestFanOutStageBranchTimeout(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name:    "slow_branch",
				Timeout: 50 * time.Millisecond,
				Stages: []Stage{
					&testStage{name: "slow", typ: "test", delay: 5 * time.Second},
				},
			},
			{
				Name: "fast_branch",
				Stages: []Stage{
					&testStage{name: "fast", typ: "test", addField: "fast", addValue: true},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
		FailOnError:   false,
	}

	stage, err := NewFanOutStage("test_branch_timeout", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	start := time.Now()
	result, err := stage.Process(ctx, record)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	// Should complete quickly
	if elapsed > 500*time.Millisecond {
		t.Errorf("Branch timeout not working, elapsed: %v", elapsed)
	}

	// Fast branch should succeed
	if result.Data["fast"] != true {
		t.Error("Fast branch result not present")
	}
}

func TestFanOutStageFilteredRecord(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "filter_branch",
				Stages: []Stage{
					&testStage{name: "filter", typ: "test", filter: true},
				},
			},
			{
				Name: "pass_branch",
				Stages: []Stage{
					&testStage{name: "pass", typ: "test", addField: "passed", addValue: true},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
	}

	stage, err := NewFanOutStage("test_filter", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Pass branch should still work
	if result.Data["passed"] != true {
		t.Error("Pass branch result not present")
	}
}

func TestFanOutStageMetrics(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{Name: "branch_a", Stages: []Stage{&testStage{name: "a", typ: "test"}}},
			{Name: "branch_b", Stages: []Stage{&testStage{name: "b", typ: "test"}}},
		},
		MergeStrategy: MergeDeep,
		Parallel:      true,
	}

	stage, err := NewFanOutStage("test_metrics", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()

	// Process multiple records
	for i := 0; i < 10; i++ {
		record := &Record{Data: map[string]any{"id": i}}
		_, _ = stage.Process(ctx, record)
	}

	metrics := stage.GetBranchMetrics()

	if metrics["branch_a"]["processed"] != 10 {
		t.Errorf("Expected branch_a processed=10, got %d", metrics["branch_a"]["processed"])
	}
	if metrics["branch_b"]["processed"] != 10 {
		t.Errorf("Expected branch_b processed=10, got %d", metrics["branch_b"]["processed"])
	}
}

func TestFanOutStageFromConfig(t *testing.T) {
	config := map[string]any{
		"parallel":       true,
		"merge_strategy": "deep_merge",
		"timeout":        "5s",
		"branches": []any{
			map[string]any{
				"name": "enrich_branch",
				"stages": []any{
					map[string]any{
						"type": "enrich",
						"name": "add_fields",
						"config": map[string]any{
							"fields": map[string]any{
								"source": "branch_1",
							},
						},
					},
				},
			},
		},
	}

	stage, err := NewFanOutStageFromConfig("test_from_config", config)
	if err != nil {
		t.Fatalf("Failed to create stage from config: %v", err)
	}

	if stage.Name() != "test_from_config" {
		t.Errorf("Expected name 'test_from_config', got '%s'", stage.Name())
	}

	if stage.Type() != "fan_out" {
		t.Errorf("Expected type 'fan_out', got '%s'", stage.Type())
	}

	if len(stage.branches) != 1 {
		t.Errorf("Expected 1 branch, got %d", len(stage.branches))
	}
}

func TestFanOutStageDeepMerge(t *testing.T) {
	cfg := &FanOutStageConfig{
		Branches: []FanOutBranch{
			{
				Name: "branch_a",
				Stages: []Stage{
					&testStage{name: "a", typ: "test", addField: "nested", addValue: map[string]any{"a": 1, "shared": "from_a"}},
				},
			},
			{
				Name: "branch_b",
				Stages: []Stage{
					&testStage{name: "b", typ: "test", addField: "nested", addValue: map[string]any{"b": 2, "shared": "from_b"}},
				},
			},
		},
		MergeStrategy: MergeDeep,
		Parallel:      false, // Sequential to ensure predictable merge order
	}

	stage, err := NewFanOutStage("test_deep_merge", cfg)
	if err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	ctx := context.Background()
	record := &Record{Data: map[string]any{}}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	nested, ok := result.Data["nested"].(map[string]any)
	if !ok {
		t.Fatal("Expected nested map")
	}

	// Both a and b should be present from deep merge
	if nested["a"] != 1 {
		t.Error("Deep merge lost 'a' field")
	}
	if nested["b"] != 2 {
		t.Error("Deep merge lost 'b' field")
	}
	// Last branch should win for shared fields
	if nested["shared"] != "from_b" {
		t.Errorf("Expected shared='from_b', got %v", nested["shared"])
	}
}
