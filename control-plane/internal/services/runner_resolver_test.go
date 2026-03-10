package services

import (
	"testing"
)

func TestExtractStageTypes_Basic(t *testing.T) {
	config := `[{
		"stages": [
			{"type": "filter"},
			{"type": "remap"},
			{"type": "crm-enrichment"}
		],
		"outputs": [
			{
				"pre_stages": [
					{"type": "score-classifier"}
				]
			}
		]
	}]`

	types := extractStageTypes(config)
	if len(types) == 0 {
		t.Fatal("expected stage types, got none")
	}

	typeSet := make(map[string]bool)
	for _, tp := range types {
		typeSet[tp] = true
	}

	expected := []string{"filter", "remap", "crm-enrichment", "score-classifier"}
	for _, e := range expected {
		if !typeSet[e] {
			t.Errorf("expected type %q not found in %v", e, types)
		}
	}
}

func TestExtractStageTypes_Empty(t *testing.T) {
	types := extractStageTypes("")
	if len(types) != 0 {
		t.Errorf("expected empty, got %v", types)
	}
}

func TestExtractStageTypes_InvalidJSON(t *testing.T) {
	types := extractStageTypes("{invalid}")
	if len(types) != 0 {
		t.Errorf("expected empty for invalid JSON, got %v", types)
	}
}

func TestExtractStageTypes_NoStages(t *testing.T) {
	config := `[{"outputs": []}]`
	types := extractStageTypes(config)
	if len(types) != 0 {
		t.Errorf("expected empty, got %v", types)
	}
}

func TestExtractStageTypes_Dedup(t *testing.T) {
	config := `[{
		"stages": [
			{"type": "filter"},
			{"type": "filter"},
			{"type": "remap"}
		]
	}]`

	types := extractStageTypes(config)
	typeSet := make(map[string]bool)
	for _, tp := range types {
		typeSet[tp] = true
	}

	if len(typeSet) != 2 {
		t.Errorf("expected 2 unique types, got %d", len(typeSet))
	}
}

func TestBuildRequiredError_Message(t *testing.T) {
	err := &BuildRequiredError{
		PendingPlugins: nil,
		LatestReadySeq: 5,
		LatestSeq:      7,
	}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	// seq 정보가 포함되어야 함
	if !contains(msg, "seq #7") || !contains(msg, "seq #5") {
		t.Errorf("error message should contain seq info, got: %s", msg)
	}
}

func TestDefaultRunnerImage(t *testing.T) {
	if DefaultRunnerImage == "" {
		t.Error("DefaultRunnerImage should not be empty")
	}
}

func TestExtractStageTypesPerPipeline_Basic(t *testing.T) {
	config := `[
		{
			"stages": [{"type": "filter"}, {"type": "crm-enrichment"}],
			"outputs": [{"pre_stages": [{"type": "score-classifier"}]}]
		},
		{
			"stages": [{"type": "remap"}],
			"outputs": []
		}
	]`

	result := extractStageTypesPerPipeline(config)
	if len(result) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(result))
	}

	// 첫 번째 pipeline: filter, crm-enrichment, score-classifier
	if len(result[0]) != 3 {
		t.Errorf("pipeline 0: expected 3 stage types, got %d: %v", len(result[0]), result[0])
	}

	// 두 번째 pipeline: remap
	if len(result[1]) != 1 || result[1][0] != "remap" {
		t.Errorf("pipeline 1: expected [remap], got %v", result[1])
	}
}

func TestExtractStageTypesPerPipeline_Empty(t *testing.T) {
	result := extractStageTypesPerPipeline("")
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
