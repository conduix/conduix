package stream

import (
	"context"
	"testing"

	"github.com/conduix/conduix/shared/types"
)

func TestContractStageValidData(t *testing.T) {
	minAmount := 0.0
	maxAmount := 1000.0

	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
				{Name: "amount", Type: "number", Required: true, Min: &minAmount, Max: &maxAmount},
			},
		},
		Rules: []types.BusinessRule{
			{Name: "valid_amount", Condition: "amount >= 0", Severity: types.SeverityError},
		},
	}

	cfg := &ContractStageConfig{
		Contract: contract,
		Action:   types.ActionDrop,
	}

	stage, err := NewContractStage("test_contract_stage", cfg)
	if err != nil {
		t.Fatalf("Failed to create contract stage: %v", err)
	}

	// 유효한 데이터
	record := &Record{
		Data: map[string]any{
			"user_id": "u-123",
			"amount":  500.0,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Error("Expected record to pass through, got nil")
	}

	// 메트릭스 확인
	metrics := stage.GetMetrics()
	if metrics.TotalRecords != 1 {
		t.Errorf("Expected TotalRecords=1, got %d", metrics.TotalRecords)
	}
	if metrics.ValidRecords != 1 {
		t.Errorf("Expected ValidRecords=1, got %d", metrics.ValidRecords)
	}
}

func TestContractStageDropAction(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
			},
		},
	}

	cfg := &ContractStageConfig{
		Contract: contract,
		Action:   types.ActionDrop,
	}

	stage, err := NewContractStage("test_contract_stage", cfg)
	if err != nil {
		t.Fatalf("Failed to create contract stage: %v", err)
	}

	// 필수 필드 누락
	record := &Record{
		Data: map[string]any{
			"amount": 500.0,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != nil {
		t.Error("Expected record to be dropped (nil), got a record")
	}

	// 메트릭스 확인
	metrics := stage.GetMetrics()
	if metrics.InvalidRecords != 1 {
		t.Errorf("Expected InvalidRecords=1, got %d", metrics.InvalidRecords)
	}
}

func TestContractStageTagAction(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "positive_amount", Condition: "amount > 0", Severity: types.SeverityError},
		},
	}

	cfg := &ContractStageConfig{
		Contract: contract,
		Action:   types.ActionTag,
		TagField: "_violations",
	}

	stage, err := NewContractStage("test_contract_stage", cfg)
	if err != nil {
		t.Fatalf("Failed to create contract stage: %v", err)
	}

	// 음수 금액 (위반)
	record := &Record{
		Data: map[string]any{
			"amount": -100.0,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected record to pass through with tag, got nil")
	}

	// 태그 필드 확인
	if _, ok := result.Data["_violations"]; !ok {
		t.Error("Expected _violations tag field")
	}
}

func TestContractStageErrorAction(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
			},
		},
	}

	cfg := &ContractStageConfig{
		Contract: contract,
		Action:   types.ActionError,
	}

	stage, err := NewContractStage("test_contract_stage", cfg)
	if err != nil {
		t.Fatalf("Failed to create contract stage: %v", err)
	}

	// 필수 필드 누락
	record := &Record{
		Data: map[string]any{
			"amount": 500.0,
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err == nil {
		t.Error("Expected error for invalid data with error action")
	}
	if result != nil {
		t.Error("Expected nil result on error")
	}
}

func TestContractStageFromConfig(t *testing.T) {
	config := map[string]any{
		"contract": map[string]any{
			"name":    "test_contract",
			"version": "1.0.0",
			"schema": map[string]any{
				"fields": []any{
					map[string]any{
						"name":     "user_id",
						"type":     "string",
						"required": true,
					},
				},
			},
			"rules": []any{
				map[string]any{
					"name":      "valid_user",
					"condition": "user_id exists",
					"severity":  "error",
				},
			},
		},
		"on_violation": "drop",
	}

	stage, err := NewContractStageFromConfig("test_stage", config)
	if err != nil {
		t.Fatalf("Failed to create contract stage from config: %v", err)
	}

	if stage.Name() != "test_stage" {
		t.Errorf("Expected name 'test_stage', got '%s'", stage.Name())
	}
	if stage.Type() != "contract" {
		t.Errorf("Expected type 'contract', got '%s'", stage.Type())
	}

	// 유효한 데이터 테스트
	record := &Record{
		Data: map[string]any{
			"user_id": "u-123",
		},
	}

	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Error("Expected valid record to pass through")
	}
}

func TestContractStageMetrics(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "positive_amount", Condition: "amount > 0", Severity: types.SeverityError},
			{Name: "max_amount", Condition: "amount <= 1000", Severity: types.SeverityWarning},
		},
	}

	cfg := &ContractStageConfig{
		Contract: contract,
		Action:   types.ActionDrop,
	}

	stage, err := NewContractStage("test_contract_stage", cfg)
	if err != nil {
		t.Fatalf("Failed to create contract stage: %v", err)
	}

	ctx := context.Background()

	// 유효한 레코드
	_, _ = stage.Process(ctx, &Record{Data: map[string]any{"amount": 500.0}})

	// 위반 레코드 (음수)
	_, _ = stage.Process(ctx, &Record{Data: map[string]any{"amount": -100.0}})

	// 경고 레코드 (높은 금액) - Warning은 Valid로 카운트됨
	_, _ = stage.Process(ctx, &Record{Data: map[string]any{"amount": 2000.0}})

	metrics := stage.GetMetrics()

	if metrics.TotalRecords != 3 {
		t.Errorf("Expected TotalRecords=3, got %d", metrics.TotalRecords)
	}
	// Warning 레코드는 Valid로 카운트됨 (2개: 500.0과 2000.0)
	if metrics.ValidRecords != 2 {
		t.Errorf("Expected ValidRecords=2, got %d", metrics.ValidRecords)
	}
	if metrics.InvalidRecords != 1 {
		t.Errorf("Expected InvalidRecords=1, got %d", metrics.InvalidRecords)
	}
	// 경고 레코드는 유효로 카운트되지만, Warning도 카운트됨
	if metrics.WarningRecords != 1 {
		t.Errorf("Expected WarningRecords=1, got %d", metrics.WarningRecords)
	}
}
