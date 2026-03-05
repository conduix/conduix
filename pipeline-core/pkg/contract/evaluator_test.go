package contract

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

func TestEvaluatorValidData(t *testing.T) {
	minAmount := 0.0
	maxAmount := 1000000.0

	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
				{Name: "amount", Type: "number", Required: true, Min: &minAmount, Max: &maxAmount},
				{Name: "event_type", Type: "string", Required: true},
			},
		},
		Rules: []types.BusinessRule{
			{Name: "valid_amount", Condition: "amount >= 0", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 유효한 데이터
	data := map[string]any{
		"user_id":    "u-123",
		"amount":     500.0,
		"event_type": "purchase",
	}

	result := evaluator.Validate(data)
	if !result.Valid {
		t.Errorf("Expected valid result, got invalid: %v", result.Violations)
	}
}

func TestEvaluatorInvalidData(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
				{Name: "amount", Type: "number", Required: true},
			},
		},
		Rules: []types.BusinessRule{
			{Name: "valid_amount", Condition: "amount >= 0", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 필수 필드 누락
	data := map[string]any{
		"amount": 500.0,
	}

	result := evaluator.Validate(data)
	if result.Valid {
		t.Error("Expected invalid result for missing required field")
	}

	foundViolation := false
	for _, v := range result.Violations {
		if v.RuleName == "schema_required" {
			foundViolation = true
			break
		}
	}
	if !foundViolation {
		t.Error("Expected schema_required violation")
	}
}

func TestEvaluatorBusinessRules(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "positive_amount", Condition: "amount > 0", Severity: types.SeverityError},
			{Name: "max_amount", Condition: "amount <= 10000", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 음수 금액 - 실패
	result := evaluator.Validate(map[string]any{"amount": -100.0})
	if result.Valid {
		t.Error("Expected invalid result for negative amount")
	}

	// 범위 초과 - 실패
	result = evaluator.Validate(map[string]any{"amount": 20000.0})
	if result.Valid {
		t.Error("Expected invalid result for amount > 10000")
	}

	// 정상 범위 - 성공
	result = evaluator.Validate(map[string]any{"amount": 500.0})
	if !result.Valid {
		t.Errorf("Expected valid result, got: %v", result.Violations)
	}
}

func TestEvaluatorANDCondition(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{
				Name:      "valid_range",
				Condition: "amount >= 0 AND amount <= 1000",
				Severity:  types.SeverityError,
			},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 범위 내 - 성공
	result := evaluator.Validate(map[string]any{"amount": 500.0})
	if !result.Valid {
		t.Errorf("Expected valid result, got: %v", result.Violations)
	}

	// 범위 초과 - 실패
	result = evaluator.Validate(map[string]any{"amount": 1500.0})
	if result.Valid {
		t.Error("Expected invalid result for amount > 1000")
	}
}

func TestEvaluatorORCondition(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{
				Name:      "valid_status",
				Condition: "status == \"active\" OR status == \"pending\"",
				Severity:  types.SeverityError,
			},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// active - 성공
	result := evaluator.Validate(map[string]any{"status": "active"})
	if !result.Valid {
		t.Errorf("Expected valid result for active, got: %v", result.Violations)
	}

	// pending - 성공
	result = evaluator.Validate(map[string]any{"status": "pending"})
	if !result.Valid {
		t.Errorf("Expected valid result for pending, got: %v", result.Violations)
	}

	// invalid - 실패
	result = evaluator.Validate(map[string]any{"status": "deleted"})
	if result.Valid {
		t.Error("Expected invalid result for deleted status")
	}
}

func TestEvaluatorExistsCondition(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "has_user", Condition: "user_id exists", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 필드 존재 - 성공
	result := evaluator.Validate(map[string]any{"user_id": "u-123"})
	if !result.Valid {
		t.Errorf("Expected valid result, got: %v", result.Violations)
	}

	// 필드 없음 - 실패
	result = evaluator.Validate(map[string]any{"amount": 100})
	if result.Valid {
		t.Error("Expected invalid result for missing user_id")
	}
}

func TestEvaluatorMatchesCondition(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "valid_email", Condition: "email matches \"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$\"", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 유효한 이메일 - 성공
	result := evaluator.Validate(map[string]any{"email": "test@example.com"})
	if !result.Valid {
		t.Errorf("Expected valid result for valid email, got: %v", result.Violations)
	}

	// 유효하지 않은 이메일 - 실패
	result = evaluator.Validate(map[string]any{"email": "not-an-email"})
	if result.Valid {
		t.Error("Expected invalid result for invalid email")
	}
}

func TestEvaluatorInCondition(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "valid_type", Condition: "event_type in [\"click\", \"purchase\", \"view\"]", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 유효한 타입 - 성공
	result := evaluator.Validate(map[string]any{"event_type": "click"})
	if !result.Valid {
		t.Errorf("Expected valid result for click, got: %v", result.Violations)
	}

	// 유효하지 않은 타입 - 실패
	result = evaluator.Validate(map[string]any{"event_type": "unknown"})
	if result.Valid {
		t.Error("Expected invalid result for unknown event_type")
	}
}

func TestEvaluatorNestedFields(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "has_city", Condition: "user.address.city exists", Severity: types.SeverityError},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 중첩 필드 존재 - 성공
	data := map[string]any{
		"user": map[string]any{
			"address": map[string]any{
				"city": "Seoul",
			},
		},
	}
	result := evaluator.Validate(data)
	if !result.Valid {
		t.Errorf("Expected valid result, got: %v", result.Violations)
	}

	// 중첩 필드 없음 - 실패
	data = map[string]any{
		"user": map[string]any{
			"name": "John",
		},
	}
	result = evaluator.Validate(data)
	if result.Valid {
		t.Error("Expected invalid result for missing nested field")
	}
}

func TestEvaluatorStrictMode(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Schema: &types.ContractSchema{
			Fields: []types.ContractField{
				{Name: "user_id", Type: "string", Required: true},
			},
			Strict: true,
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 정의된 필드만 - 성공
	result := evaluator.Validate(map[string]any{"user_id": "u-123"})
	if !result.Valid {
		t.Errorf("Expected valid result, got: %v", result.Violations)
	}

	// 정의되지 않은 필드 포함 - 실패
	result = evaluator.Validate(map[string]any{"user_id": "u-123", "extra_field": "value"})
	if result.Valid {
		t.Error("Expected invalid result for extra field in strict mode")
	}
}

func TestEvaluatorWarnings(t *testing.T) {
	contract := &types.DataContract{
		Name:    "test_contract",
		Version: "1.0.0",
		Rules: []types.BusinessRule{
			{Name: "high_amount", Condition: "amount <= 5000", Severity: types.SeverityWarning},
		},
	}

	evaluator, err := NewEvaluator(contract)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	// 높은 금액 - 경고 (레코드는 유효)
	result := evaluator.Validate(map[string]any{"amount": 10000.0})
	if !result.Valid {
		t.Error("Expected valid result (warning should not fail validation)")
	}
	if len(result.Warnings) == 0 {
		t.Error("Expected warning for high amount")
	}
}
