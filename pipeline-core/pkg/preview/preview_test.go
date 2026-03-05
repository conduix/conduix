package preview

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/stream"
)

func TestPreviewer_Preview_FilterStage(t *testing.T) {
	// Filter Stage 생성
	stage := stream.NewFilterStage("test_filter", map[string]any{
		"condition": ".status == \"active\"",
	})

	previewer := NewPreviewer([]stream.Stage{stage}, DefaultPreviewOptions())

	sampleData := []map[string]any{
		{"id": "1", "status": "active", "name": "Item 1"},
		{"id": "2", "status": "inactive", "name": "Item 2"},
		{"id": "3", "status": "active", "name": "Item 3"},
	}

	ctx := context.Background()
	results, err := previewer.Preview(ctx, sampleData)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.StageName != "test_filter" {
		t.Errorf("expected stage name 'test_filter', got '%s'", result.StageName)
	}
	if result.StageType != "filter" {
		t.Errorf("expected stage type 'filter', got '%s'", result.StageType)
	}

	// 입력 레코드 수 확인
	if len(result.InputRecords) != 3 {
		t.Errorf("expected 3 input records, got %d", len(result.InputRecords))
	}
}

func TestPreviewer_Preview_RemapStage(t *testing.T) {
	// Remap Stage 생성
	stage := stream.NewRemapStage("test_remap", map[string]any{
		"mappings": map[string]any{
			"user_id":   ".id",
			"user_name": ".name",
		},
	})

	previewer := NewPreviewer([]stream.Stage{stage}, PreviewOptions{
		MaxRecords:  5,
		IncludeDiff: true,
		Timeout:     10 * time.Second,
	})

	sampleData := []map[string]any{
		{"id": "123", "name": "John", "email": "john@example.com"},
		{"id": "456", "name": "Jane", "email": "jane@example.com"},
	}

	ctx := context.Background()
	results, err := previewer.Preview(ctx, sampleData)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]

	// Diff 포함 여부 확인
	if len(result.Diff) == 0 {
		t.Error("expected diff to be included")
	}
}

func TestPreviewer_Preview_MultiplieStages(t *testing.T) {
	// 여러 Stage 파이프라인
	stages := []stream.Stage{
		stream.NewPassthroughStage("stage1", nil),
		stream.NewPassthroughStage("stage2", nil),
	}

	previewer := NewPreviewer(stages, DefaultPreviewOptions())

	sampleData := []map[string]any{
		{"id": "1", "value": 100},
		{"id": "2", "value": 200},
	}

	ctx := context.Background()
	results, err := previewer.Preview(ctx, sampleData)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per stage), got %d", len(results))
	}
}

func TestPreviewer_Preview_StageFilter(t *testing.T) {
	stages := []stream.Stage{
		stream.NewPassthroughStage("stage1", nil),
		stream.NewPassthroughStage("stage2", nil),
		stream.NewPassthroughStage("stage3", nil),
	}

	// 특정 Stage만 미리보기
	previewer := NewPreviewer(stages, PreviewOptions{
		MaxRecords:  10,
		IncludeDiff: false,
		Timeout:     10 * time.Second,
		StageNames:  []string{"stage2"},
	})

	sampleData := []map[string]any{
		{"id": "1"},
	}

	ctx := context.Background()
	results, err := previewer.Preview(ctx, sampleData)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result (filtered to stage2), got %d", len(results))
	}

	if results[0].StageName != "stage2" {
		t.Errorf("expected stage 'stage2', got '%s'", results[0].StageName)
	}
}

func TestPreviewer_calculateStats(t *testing.T) {
	previewer := NewPreviewer(nil, DefaultPreviewOptions())

	result := PreviewResult{
		InputRecords: []PreviewRecord{
			{Index: 0, Data: map[string]any{"a": 1}},
			{Index: 1, Data: map[string]any{"b": 2}},
			{Index: 2, Data: map[string]any{"c": 3}},
			{Index: 3, Data: map[string]any{"d": 4}},
		},
		OutputRecords: []PreviewRecord{
			{Index: 0, Data: map[string]any{"a": 1, "x": 10}},
			{Index: 1, Data: map[string]any{"b": 2, "y": 20}},
		},
		FilteredCount: 1,
		ErrorCount:    1,
	}

	stats := previewer.calculateStats(result)

	if stats.TotalInput != 4 {
		t.Errorf("expected TotalInput 4, got %d", stats.TotalInput)
	}
	if stats.TotalOutput != 2 {
		t.Errorf("expected TotalOutput 2, got %d", stats.TotalOutput)
	}
	if stats.PassRate != 50.0 {
		t.Errorf("expected PassRate 50.0, got %f", stats.PassRate)
	}
	if stats.FilterRate != 25.0 {
		t.Errorf("expected FilterRate 25.0, got %f", stats.FilterRate)
	}
	if stats.ErrorRate != 25.0 {
		t.Errorf("expected ErrorRate 25.0, got %f", stats.ErrorRate)
	}
	if stats.AvgFields != 2.0 {
		t.Errorf("expected AvgFields 2.0, got %f", stats.AvgFields)
	}
}

func TestPreviewer_compareRecords(t *testing.T) {
	previewer := NewPreviewer(nil, DefaultPreviewOptions())

	tests := []struct {
		name     string
		input    map[string]any
		output   map[string]any
		wantType DiffType
	}{
		{
			name:     "unchanged",
			input:    map[string]any{"a": 1, "b": "hello"},
			output:   map[string]any{"a": 1, "b": "hello"},
			wantType: DiffTypeUnchanged,
		},
		{
			name:     "field added",
			input:    map[string]any{"a": 1},
			output:   map[string]any{"a": 1, "b": 2},
			wantType: DiffTypeModified,
		},
		{
			name:     "field removed",
			input:    map[string]any{"a": 1, "b": 2},
			output:   map[string]any{"a": 1},
			wantType: DiffTypeModified,
		},
		{
			name:     "field modified",
			input:    map[string]any{"a": 1},
			output:   map[string]any{"a": 100},
			wantType: DiffTypeModified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := previewer.compareRecords(0, tt.input, tt.output)
			if diff.Type != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, diff.Type)
			}
		})
	}
}

func TestPreviewSingleStage(t *testing.T) {
	stageConfig := map[string]any{
		"type": "passthrough",
		"name": "test_passthrough",
	}

	sampleData := []map[string]any{
		{"id": "1", "value": 100},
		{"id": "2", "value": 200},
	}

	ctx := context.Background()
	result, err := PreviewSingleStage(ctx, stageConfig, sampleData, DefaultPreviewOptions())
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if result.StageName != "test_passthrough" {
		t.Errorf("expected stage name 'test_passthrough', got '%s'", result.StageName)
	}
	if len(result.OutputRecords) != 2 {
		t.Errorf("expected 2 output records, got %d", len(result.OutputRecords))
	}
}

func TestPreviewSingleStage_MissingType(t *testing.T) {
	stageConfig := map[string]any{
		"name": "test_stage",
	}

	sampleData := []map[string]any{
		{"id": "1"},
	}

	ctx := context.Background()
	_, err := PreviewSingleStage(ctx, stageConfig, sampleData, DefaultPreviewOptions())
	if err == nil {
		t.Error("expected error for missing stage type")
	}
}

func TestQuickPreview(t *testing.T) {
	stages := []stream.Stage{
		stream.NewPassthroughStage("quick_test", nil),
	}

	sampleData := []map[string]any{
		{"id": "1"},
	}

	ctx := context.Background()
	results, err := QuickPreview(ctx, stages, sampleData)
	if err != nil {
		t.Fatalf("quick preview failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestDefaultPreviewOptions(t *testing.T) {
	opts := DefaultPreviewOptions()

	if opts.MaxRecords != 10 {
		t.Errorf("expected MaxRecords 10, got %d", opts.MaxRecords)
	}
	if !opts.IncludeDiff {
		t.Error("expected IncludeDiff true")
	}
	if opts.Timeout != 30*time.Second {
		t.Errorf("expected Timeout 30s, got %v", opts.Timeout)
	}
}
