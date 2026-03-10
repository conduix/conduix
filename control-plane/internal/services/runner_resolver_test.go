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
	}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestDefaultRunnerImage(t *testing.T) {
	if DefaultRunnerImage == "" {
		t.Error("DefaultRunnerImage should not be empty")
	}
}
