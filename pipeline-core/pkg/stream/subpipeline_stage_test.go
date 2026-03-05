package stream

import (
	"context"
	"testing"
)

func TestNewSubPipelineStage_RequiresStages(t *testing.T) {
	config := map[string]any{}

	_, err := NewSubPipelineStage("test", config)
	if err == nil {
		t.Error("expected error for missing stages")
	}
}

func TestNewSubPipelineStage_BasicConfig(t *testing.T) {
	config := map[string]any{
		"stages": []any{
			map[string]any{
				"type": "remap",
				"config": map[string]any{
					"mappings": map[string]any{
						"new_field": "old_field",
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.Name() != "test_sub" {
		t.Errorf("expected name 'test_sub', got '%s'", stage.Name())
	}

	if stage.Type() != "sub_pipeline" {
		t.Errorf("expected type 'sub_pipeline', got '%s'", stage.Type())
	}

	if len(stage.stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(stage.stages))
	}
}

func TestSubPipelineStage_Process(t *testing.T) {
	config := map[string]any{
		"stages": []any{
			map[string]any{
				"type": "enrich",
				"config": map[string]any{
					"fields": map[string]any{
						"enriched": true,
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"id":   1,
			"name": "test",
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if result.Data["enriched"] != true {
		t.Error("expected enriched field to be true")
	}
}

func TestSubPipelineStage_WithCondition(t *testing.T) {
	config := map[string]any{
		"condition": ".active == true",
		"stages": []any{
			map[string]any{
				"type": "enrich",
				"config": map[string]any{
					"fields": map[string]any{
						"processed": true,
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// Test with matching condition
	activeRecord := &Record{
		Data: map[string]any{
			"id":     1,
			"active": true,
		},
	}

	result, err := stage.Process(ctx, activeRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Data["processed"] != true {
		t.Error("expected processed field for active record")
	}

	// Test with non-matching condition
	inactiveRecord := &Record{
		Data: map[string]any{
			"id":     2,
			"active": false,
		},
	}

	result2, err := stage.Process(ctx, inactiveRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := result2.Data["processed"]; exists {
		t.Error("inactive record should not be processed by sub-pipeline")
	}
}

func TestSubPipelineStage_MergeStrategy_Replace(t *testing.T) {
	config := map[string]any{
		"merge_strategy": "replace",
		"stages": []any{
			map[string]any{
				"type": "remap",
				"config": map[string]any{
					"mappings": map[string]any{},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.mergeStrategy != "replace" {
		t.Errorf("expected merge_strategy 'replace', got '%s'", stage.mergeStrategy)
	}
}

func TestSubPipelineStage_MergeStrategy_Merge(t *testing.T) {
	config := map[string]any{
		"merge_strategy": "merge",
		"stages": []any{
			map[string]any{
				"type": "enrich",
				"config": map[string]any{
					"fields": map[string]any{
						"new_field": "value",
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"original": "data",
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both original and new fields should exist
	if result.Data["original"] != "data" {
		t.Error("expected original field to be preserved")
	}
	if result.Data["new_field"] != "value" {
		t.Error("expected new_field to be added")
	}
}

func TestSubPipelineStage_MergeStrategy_Nested(t *testing.T) {
	config := map[string]any{
		"merge_strategy": "nested",
		"result_field":   "sub_result",
		"stages": []any{
			map[string]any{
				"type": "enrich",
				"config": map[string]any{
					"fields": map[string]any{
						"nested_field": "nested_value",
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	record := &Record{
		Data: map[string]any{
			"original": "data",
		},
	}

	result, err := stage.Process(ctx, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original field should exist
	if result.Data["original"] != "data" {
		t.Error("expected original field to be preserved")
	}

	// Result should be nested
	subResult, ok := result.Data["sub_result"].(map[string]any)
	if !ok {
		t.Fatal("expected sub_result to be a map")
	}

	if subResult["nested_field"] != "nested_value" {
		t.Error("expected nested_field in sub_result")
	}
}

func TestSubPipelineStage_OnError_Passthrough(t *testing.T) {
	config := map[string]any{
		"on_error": "passthrough",
		"stages": []any{
			map[string]any{
				"type": "validate",
				"config": map[string]any{
					"schema": map[string]any{
						"fields": []any{
							map[string]any{
								"name":     "required_field",
								"type":     "string",
								"required": true,
							},
						},
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stage.onError != "passthrough" {
		t.Errorf("expected on_error 'passthrough', got '%s'", stage.onError)
	}
}

func TestSubPipelineStage_NestedPipeline(t *testing.T) {
	// Multi-stage sub-pipeline
	config := map[string]any{
		"stages": []any{
			map[string]any{
				"type": "filter",
				"config": map[string]any{
					"condition": ".level != \"debug\"",
				},
			},
			map[string]any{
				"type": "enrich",
				"config": map[string]any{
					"fields": map[string]any{
						"enriched": true,
					},
				},
			},
		},
	}

	stage, err := NewSubPipelineStage("multi_stage", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stage.stages) != 2 {
		t.Errorf("expected 2 stages, got %d", len(stage.stages))
	}

	ctx := context.Background()

	// Should pass through (not debug)
	infoRecord := &Record{
		Data: map[string]any{
			"level":   "info",
			"message": "test",
		},
	}

	result, err := stage.Process(ctx, infoRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Data["enriched"] != true {
		t.Error("expected enriched field for info level")
	}

	// Should be filtered
	debugRecord := &Record{
		Data: map[string]any{
			"level":   "debug",
			"message": "test",
		},
	}

	result2, err := stage.Process(ctx, debugRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Filtered by sub-pipeline filter stage
	if result2 != nil {
		t.Error("expected nil result for filtered debug record")
	}
}

func TestSubPipelineStage_Stats(t *testing.T) {
	config := map[string]any{
		"stages": []any{
			map[string]any{
				"type":   "passthrough",
				"config": map[string]any{},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		record := &Record{
			Data: map[string]any{"id": i},
		}
		_, _ = stage.Process(ctx, record)
	}

	executions, errors := stage.SubPipelineStats()
	if executions != 10 {
		t.Errorf("expected 10 executions, got %d", executions)
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestSubPipelineStage_Close(t *testing.T) {
	config := map[string]any{
		"stages": []any{
			map[string]any{
				"type":   "passthrough",
				"config": map[string]any{},
			},
		},
	}

	stage, err := NewSubPipelineStage("test_sub", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := stage.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}
