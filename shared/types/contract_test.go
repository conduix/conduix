package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDataContractJSON(t *testing.T) {
	minLen := 1
	maxAmount := 1000000.0

	contract := DataContract{
		Name:        "user_events_v2",
		Version:     "2.0.0",
		Description: "User event data contract",
		Owner:       "data-platform-team",
		Team:        "platform",
		SLA: &ContractSLA{
			Freshness:    "5m",
			Completeness: 0.99,
			Accuracy:     0.999,
		},
		Schema: &ContractSchema{
			Fields: []ContractField{
				{Name: "user_id", Type: "string", Required: true, MinLength: &minLen},
				{Name: "amount", Type: "number", Required: true, Max: &maxAmount},
				{Name: "event_type", Type: "string", Required: true, Enum: []any{"click", "purchase", "view"}},
			},
			Strict: false,
		},
		Rules: []BusinessRule{
			{
				Name:        "valid_amount",
				Description: "Amount must be non-negative",
				Condition:   "amount >= 0",
				Severity:    SeverityError,
			},
			{
				Name:        "valid_user_id",
				Description: "User ID must exist",
				Condition:   "user_id exists",
				Severity:    SeverityError,
			},
		},
		Tags: map[string]string{
			"domain":      "user",
			"sensitivity": "pii",
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 직렬화
	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("Failed to marshal DataContract: %v", err)
	}

	// 역직렬화
	var parsed DataContract
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal DataContract: %v", err)
	}

	// 검증
	if parsed.Name != contract.Name {
		t.Errorf("Name mismatch: expected '%s', got '%s'", contract.Name, parsed.Name)
	}
	if parsed.Version != contract.Version {
		t.Errorf("Version mismatch: expected '%s', got '%s'", contract.Version, parsed.Version)
	}
	if parsed.SLA.Freshness != "5m" {
		t.Errorf("SLA Freshness mismatch: expected '5m', got '%s'", parsed.SLA.Freshness)
	}
	if len(parsed.Schema.Fields) != 3 {
		t.Errorf("Schema Fields count mismatch: expected 3, got %d", len(parsed.Schema.Fields))
	}
	if len(parsed.Rules) != 2 {
		t.Errorf("Rules count mismatch: expected 2, got %d", len(parsed.Rules))
	}
}

func TestContractViolation(t *testing.T) {
	violation := ContractViolation{
		RecordID:   "rec-123",
		Timestamp:  time.Now(),
		ContractID: "user_events_v2",
		RuleName:   "valid_amount",
		Severity:   SeverityError,
		Message:    "Amount is negative",
		FieldErrors: []FieldViolation{
			{Field: "amount", Value: -100, Message: "must be >= 0"},
		},
		OriginalData: map[string]any{
			"user_id": "u-123",
			"amount":  -100,
		},
	}

	// 직렬화
	data, err := json.Marshal(violation)
	if err != nil {
		t.Fatalf("Failed to marshal ContractViolation: %v", err)
	}

	// 역직렬화
	var parsed ContractViolation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ContractViolation: %v", err)
	}

	if parsed.RuleName != "valid_amount" {
		t.Errorf("RuleName mismatch: expected 'valid_amount', got '%s'", parsed.RuleName)
	}
	if parsed.Severity != SeverityError {
		t.Errorf("Severity mismatch: expected '%s', got '%s'", SeverityError, parsed.Severity)
	}
	if len(parsed.FieldErrors) != 1 {
		t.Errorf("FieldErrors count mismatch: expected 1, got %d", len(parsed.FieldErrors))
	}
}

func TestDLQRecord(t *testing.T) {
	dlqRecord := DLQRecord{
		ID:         "dlq-123",
		Timestamp:  time.Now(),
		Source:     "kafka",
		PipelineID: "pipeline-456",
		WorkflowID: "workflow-789",
		ContractID: "user_events_v2",
		Violations: []ContractViolation{
			{
				RuleName: "valid_amount",
				Message:  "Amount is negative",
			},
		},
		OriginalData: map[string]any{
			"user_id": "u-123",
			"amount":  -100,
		},
		Metadata: map[string]string{
			"partition": "0",
			"offset":    "12345",
		},
		RetryCount: 0,
		MaxRetries: 3,
	}

	// 직렬화
	data, err := json.Marshal(dlqRecord)
	if err != nil {
		t.Fatalf("Failed to marshal DLQRecord: %v", err)
	}

	// 역직렬화
	var parsed DLQRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal DLQRecord: %v", err)
	}

	if parsed.ID != "dlq-123" {
		t.Errorf("ID mismatch: expected 'dlq-123', got '%s'", parsed.ID)
	}
	if parsed.PipelineID != "pipeline-456" {
		t.Errorf("PipelineID mismatch: expected 'pipeline-456', got '%s'", parsed.PipelineID)
	}
	if len(parsed.Violations) != 1 {
		t.Errorf("Violations count mismatch: expected 1, got %d", len(parsed.Violations))
	}
	if parsed.MaxRetries != 3 {
		t.Errorf("MaxRetries mismatch: expected 3, got %d", parsed.MaxRetries)
	}
}

func TestSeverityConstants(t *testing.T) {
	severities := map[RuleSeverity]string{
		SeverityError:   "error",
		SeverityWarning: "warning",
		SeverityInfo:    "info",
	}

	for constant, expected := range severities {
		if string(constant) != expected {
			t.Errorf("Severity constant mismatch: expected '%s', got '%s'", expected, constant)
		}
	}
}

func TestViolationActionConstants(t *testing.T) {
	actions := map[ViolationAction]string{
		ActionDrop:       "drop",
		ActionQuarantine: "quarantine",
		ActionTag:        "tag",
		ActionError:      "error",
	}

	for constant, expected := range actions {
		if string(constant) != expected {
			t.Errorf("ViolationAction constant mismatch: expected '%s', got '%s'", expected, constant)
		}
	}
}

func TestContractMetrics(t *testing.T) {
	metrics := ContractMetrics{
		ContractID:     "user_events_v2",
		TotalRecords:   1000,
		ValidRecords:   950,
		InvalidRecords: 30,
		WarningRecords: 20,
		ViolationsByRule: map[string]int64{
			"valid_amount": 15,
			"valid_user":   15,
		},
		LastUpdated: time.Now(),
	}

	// 검증
	validRate := float64(metrics.ValidRecords) / float64(metrics.TotalRecords)
	if validRate < 0.9 || validRate > 1.0 {
		t.Errorf("Unexpected valid rate: %f", validRate)
	}

	if metrics.InvalidRecords+metrics.ValidRecords > metrics.TotalRecords {
		// WarningRecords는 Valid에 포함될 수 있음
		t.Logf("Note: Some records may have both violations and warnings")
	}
}
