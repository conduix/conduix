package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

// testStage 테스트용 Stage 구현
type testStage struct {
	BaseStage
	multiplier int
}

func (s *testStage) Init(config json.RawMessage) error {
	if len(config) > 0 {
		var cfg struct {
			Multiplier int `json:"multiplier"`
		}
		if err := json.Unmarshal(config, &cfg); err != nil {
			return err
		}
		s.multiplier = cfg.Multiplier
	}
	if s.multiplier == 0 {
		s.multiplier = 1
	}
	return nil
}

func (s *testStage) Process(_ context.Context, record *Record) ([]*Record, error) {
	if val, ok := record.Data["value"]; ok {
		if num, ok := val.(float64); ok {
			record.Data["value"] = num * float64(s.multiplier)
		}
	}
	return []*Record{record}, nil
}

func newTestStageFactory() StageFactory {
	return func() Stage {
		return &testStage{BaseStage: BaseStage{StageType: "test-multiply"}}
	}
}

func TestRegistryRegister(t *testing.T) {
	reg := NewRegistry()

	meta := &StageMetadata{
		Type:        "test-multiply",
		DisplayName: "Test Multiplier",
		Category:    "transform",
	}

	err := reg.Register(meta, newTestStageFactory())
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !reg.Has("test-multiply") {
		t.Error("expected test-multiply to exist")
	}
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	reg := NewRegistry()

	meta := &StageMetadata{Type: "dup", DisplayName: "Dup"}
	_ = reg.Register(meta, newTestStageFactory())

	err := reg.Register(meta, newTestStageFactory())
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistryCreate(t *testing.T) {
	reg := NewRegistry()

	meta := &StageMetadata{Type: "test-multiply", DisplayName: "Test"}
	_ = reg.Register(meta, newTestStageFactory())

	config := json.RawMessage(`{"multiplier": 3}`)
	stage, err := reg.Create("test-multiply", config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	record := &Record{Data: map[string]any{"value": float64(10)}}
	results, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	val := results[0].Data["value"].(float64)
	if val != 30 {
		t.Errorf("expected 30, got %f", val)
	}
}

func TestRegistryCreateUnknown(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Create("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown stage type")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(&StageMetadata{Type: "a", Category: "transform"}, newTestStageFactory())
	_ = reg.Register(&StageMetadata{Type: "b", Category: "filter"}, newTestStageFactory())
	_ = reg.Register(&StageMetadata{Type: "c", Category: "transform"}, newTestStageFactory())

	all := reg.List()
	if len(all) != 3 {
		t.Errorf("expected 3 stages, got %d", len(all))
	}

	transforms := reg.ListByCategory("transform")
	if len(transforms) != 2 {
		t.Errorf("expected 2 transform stages, got %d", len(transforms))
	}
}

func TestRegistryTypes(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(&StageMetadata{Type: "x"}, newTestStageFactory())
	_ = reg.Register(&StageMetadata{Type: "y"}, newTestStageFactory())

	types := reg.Types()
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestRegistryGetMetadata(t *testing.T) {
	reg := NewRegistry()

	meta := &StageMetadata{
		Type:        "test",
		DisplayName: "Test Stage",
		Category:    "transform",
		Icon:        "analytics",
		Color:       "#FF0000",
	}
	_ = reg.Register(meta, newTestStageFactory())

	got, ok := reg.GetMetadata("test")
	if !ok {
		t.Fatal("expected metadata to exist")
	}
	if got.DisplayName != "Test Stage" {
		t.Errorf("expected display name 'Test Stage', got %s", got.DisplayName)
	}
	if got.Color != "#FF0000" {
		t.Errorf("expected color #FF0000, got %s", got.Color)
	}
}

func TestRegistryGet(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&StageMetadata{Type: "find-me"}, newTestStageFactory())

	factory, ok := reg.Get("find-me")
	if !ok || factory == nil {
		t.Error("expected to find factory")
	}

	_, ok = reg.Get("missing")
	if ok {
		t.Error("expected not found for missing type")
	}
}

// testBatchStage ProcessBatch를 오버라이드한 Stage
type testBatchStage struct {
	testStage
}

func (s *testBatchStage) ProcessBatch(ctx context.Context, records []*Record) ([]*Record, error) {
	var results []*Record
	for _, r := range records {
		processed, err := s.Process(ctx, r)
		if err != nil {
			return nil, err
		}
		results = append(results, processed...)
	}
	return results, nil
}

func TestProcessBatch(t *testing.T) {
	stage := &testBatchStage{
		testStage: testStage{
			BaseStage:  BaseStage{StageType: "test-multiply"},
			multiplier: 2,
		},
	}

	records := []*Record{
		{Data: map[string]any{"value": float64(5)}},
		{Data: map[string]any{"value": float64(10)}},
	}

	results, err := stage.ProcessBatch(context.Background(), records)
	if err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Data["value"].(float64) != 10 {
		t.Errorf("expected 10, got %v", results[0].Data["value"])
	}
	if results[1].Data["value"].(float64) != 20 {
		t.Errorf("expected 20, got %v", results[1].Data["value"])
	}
}
