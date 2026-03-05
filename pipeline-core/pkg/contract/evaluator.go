// Package contract 데이터 계약 검증 패키지
// ECL (Extract-Contextualize-Link) 패러다임의 Contextualize 단계 지원
package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// Evaluator 계약 검증기
type Evaluator struct {
	contract *types.DataContract
	compiled map[string]*compiledRule
}

// compiledRule 컴파일된 규칙
type compiledRule struct {
	rule      types.BusinessRule
	evaluator func(data map[string]any) (bool, string)
}

// NewEvaluator 새 검증기 생성
func NewEvaluator(contract *types.DataContract) (*Evaluator, error) {
	e := &Evaluator{
		contract: contract,
		compiled: make(map[string]*compiledRule),
	}

	// 비즈니스 규칙 컴파일
	for _, rule := range contract.Rules {
		compiled, err := e.compileRule(rule)
		if err != nil {
			return nil, fmt.Errorf("failed to compile rule '%s': %w", rule.Name, err)
		}
		e.compiled[rule.Name] = compiled
	}

	return e, nil
}

// Validate 데이터 검증
func (e *Evaluator) Validate(data map[string]any) *types.ContractValidationResult {
	result := &types.ContractValidationResult{
		Valid: true,
	}

	// 스키마 검증
	if e.contract.Schema != nil {
		schemaViolations := e.validateSchema(data)
		for _, v := range schemaViolations {
			if v.Severity == types.SeverityError {
				result.Valid = false
				result.Violations = append(result.Violations, v)
			} else {
				result.Warnings = append(result.Warnings, v)
			}
		}
	}

	// 비즈니스 규칙 검증
	for name, compiled := range e.compiled {
		valid, message := compiled.evaluator(data)
		if !valid {
			violation := types.ContractViolation{
				Timestamp:  time.Now(),
				ContractID: e.contract.Name,
				RuleName:   name,
				Severity:   compiled.rule.Severity,
				Message:    message,
			}

			if compiled.rule.Severity == types.SeverityError || compiled.rule.Severity == "" {
				result.Valid = false
				result.Violations = append(result.Violations, violation)
			} else {
				result.Warnings = append(result.Warnings, violation)
			}
		}
	}

	return result
}

// validateSchema 스키마 검증
func (e *Evaluator) validateSchema(data map[string]any) []types.ContractViolation {
	var violations []types.ContractViolation
	schema := e.contract.Schema

	// 필드별 검증
	for _, field := range schema.Fields {
		value, exists := getNestedField(data, field.Name)

		// 필수 필드 검사
		if field.Required && !exists {
			violations = append(violations, types.ContractViolation{
				Timestamp:  time.Now(),
				ContractID: e.contract.Name,
				RuleName:   "schema_required",
				Severity:   types.SeverityError,
				Message:    fmt.Sprintf("required field '%s' is missing", field.Name),
				FieldErrors: []types.FieldViolation{
					{Field: field.Name, Message: "field is required"},
				},
			})
			continue
		}

		if !exists {
			continue
		}

		// 타입 검증
		if err := validateFieldType(field, value); err != nil {
			violations = append(violations, types.ContractViolation{
				Timestamp:  time.Now(),
				ContractID: e.contract.Name,
				RuleName:   "schema_type",
				Severity:   types.SeverityError,
				Message:    err.Error(),
				FieldErrors: []types.FieldViolation{
					{Field: field.Name, Value: value, Message: err.Error()},
				},
			})
		}

		// 추가 제약 조건 검증
		if errs := validateFieldConstraints(field, value); len(errs) > 0 {
			for _, err := range errs {
				violations = append(violations, types.ContractViolation{
					Timestamp:  time.Now(),
					ContractID: e.contract.Name,
					RuleName:   "schema_constraint",
					Severity:   types.SeverityError,
					Message:    err.Error(),
					FieldErrors: []types.FieldViolation{
						{Field: field.Name, Value: value, Message: err.Error()},
					},
				})
			}
		}
	}

	// Strict 모드: 정의되지 않은 필드 검사
	if schema.Strict {
		definedFields := make(map[string]bool)
		for _, field := range schema.Fields {
			definedFields[field.Name] = true
		}

		for key := range data {
			if !definedFields[key] {
				violations = append(violations, types.ContractViolation{
					Timestamp:  time.Now(),
					ContractID: e.contract.Name,
					RuleName:   "schema_strict",
					Severity:   types.SeverityError,
					Message:    fmt.Sprintf("unexpected field '%s' in strict mode", key),
					FieldErrors: []types.FieldViolation{
						{Field: key, Message: "field not defined in schema"},
					},
				})
			}
		}
	}

	return violations
}

// compileRule 규칙을 평가 함수로 컴파일
func (e *Evaluator) compileRule(rule types.BusinessRule) (*compiledRule, error) {
	evaluator, err := parseCondition(rule.Condition)
	if err != nil {
		return nil, err
	}

	// 기본 심각도 설정
	severity := rule.Severity
	if severity == "" {
		severity = types.SeverityError
	}

	return &compiledRule{
		rule: types.BusinessRule{
			Name:        rule.Name,
			Description: rule.Description,
			Condition:   rule.Condition,
			Severity:    severity,
			Tags:        rule.Tags,
		},
		evaluator: evaluator,
	}, nil
}

// parseCondition 조건 표현식을 평가 함수로 파싱
// 지원하는 표현식:
//   - field >= value, field <= value, field == value, field != value
//   - field > value, field < value
//   - field exists, field not_exists
//   - field matches "pattern"
//   - field in [value1, value2]
//   - expr1 AND expr2, expr1 OR expr2
func parseCondition(condition string) (func(map[string]any) (bool, string), error) {
	condition = strings.TrimSpace(condition)

	// AND/OR 연산자 처리
	if idx := findLogicalOperator(condition, " AND "); idx != -1 {
		left := condition[:idx]
		right := condition[idx+5:]
		leftEval, err := parseCondition(left)
		if err != nil {
			return nil, err
		}
		rightEval, err := parseCondition(right)
		if err != nil {
			return nil, err
		}
		return func(data map[string]any) (bool, string) {
			valid, msg := leftEval(data)
			if !valid {
				return false, msg
			}
			return rightEval(data)
		}, nil
	}

	if idx := findLogicalOperator(condition, " OR "); idx != -1 {
		left := condition[:idx]
		right := condition[idx+4:]
		leftEval, err := parseCondition(left)
		if err != nil {
			return nil, err
		}
		rightEval, err := parseCondition(right)
		if err != nil {
			return nil, err
		}
		return func(data map[string]any) (bool, string) {
			valid, _ := leftEval(data)
			if valid {
				return true, ""
			}
			return rightEval(data)
		}, nil
	}

	// 단일 조건 파싱
	return parseSingleCondition(condition)
}

// findLogicalOperator AND/OR 연산자 위치 찾기 (문자열 리터럴 내부 무시)
func findLogicalOperator(s, op string) int {
	inQuote := false
	for i := 0; i < len(s)-len(op)+1; i++ {
		if s[i] == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && strings.HasPrefix(s[i:], op) {
			return i
		}
	}
	return -1
}

// parseSingleCondition 단일 조건 파싱
func parseSingleCondition(condition string) (func(map[string]any) (bool, string), error) {
	condition = strings.TrimSpace(condition)

	// exists 체크
	if strings.HasSuffix(condition, " exists") {
		field := strings.TrimSuffix(condition, " exists")
		field = strings.TrimSpace(field)
		return func(data map[string]any) (bool, string) {
			_, exists := getNestedField(data, field)
			if !exists {
				return false, fmt.Sprintf("field '%s' does not exist", field)
			}
			return true, ""
		}, nil
	}

	// not_exists 체크
	if strings.HasSuffix(condition, " not_exists") {
		field := strings.TrimSuffix(condition, " not_exists")
		field = strings.TrimSpace(field)
		return func(data map[string]any) (bool, string) {
			_, exists := getNestedField(data, field)
			if exists {
				return false, fmt.Sprintf("field '%s' should not exist", field)
			}
			return true, ""
		}, nil
	}

	// matches 연산자 (정규식)
	if idx := strings.Index(condition, " matches "); idx != -1 {
		field := strings.TrimSpace(condition[:idx])
		pattern := strings.TrimSpace(condition[idx+9:])
		pattern = strings.Trim(pattern, "\"'")
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern '%s': %w", pattern, err)
		}
		return func(data map[string]any) (bool, string) {
			value, exists := getNestedField(data, field)
			if !exists {
				return false, fmt.Sprintf("field '%s' does not exist", field)
			}
			str, ok := value.(string)
			if !ok {
				return false, fmt.Sprintf("field '%s' is not a string", field)
			}
			if !re.MatchString(str) {
				return false, fmt.Sprintf("field '%s' does not match pattern '%s'", field, pattern)
			}
			return true, ""
		}, nil
	}

	// in 연산자
	if idx := strings.Index(condition, " in "); idx != -1 {
		field := strings.TrimSpace(condition[:idx])
		valuesPart := strings.TrimSpace(condition[idx+4:])
		values, err := parseArrayLiteral(valuesPart)
		if err != nil {
			return nil, err
		}
		return func(data map[string]any) (bool, string) {
			value, exists := getNestedField(data, field)
			if !exists {
				return false, fmt.Sprintf("field '%s' does not exist", field)
			}
			for _, v := range values {
				if compareValues(value, v) == 0 {
					return true, ""
				}
			}
			return false, fmt.Sprintf("field '%s' value not in allowed list", field)
		}, nil
	}

	// 비교 연산자
	operators := []string{">=", "<=", "!=", "==", ">", "<"}
	for _, op := range operators {
		if idx := strings.Index(condition, op); idx != -1 {
			field := strings.TrimSpace(condition[:idx])
			valueStr := strings.TrimSpace(condition[idx+len(op):])
			expectedValue := parseValue(valueStr)

			return createComparisonEvaluator(field, op, expectedValue), nil
		}
	}

	return nil, fmt.Errorf("unsupported condition format: %s", condition)
}

// createComparisonEvaluator 비교 연산자 평가기 생성
func createComparisonEvaluator(field, op string, expected any) func(map[string]any) (bool, string) {
	return func(data map[string]any) (bool, string) {
		value, exists := getNestedField(data, field)
		if !exists {
			return false, fmt.Sprintf("field '%s' does not exist", field)
		}

		cmp := compareValues(value, expected)
		var valid bool

		switch op {
		case "==":
			valid = cmp == 0
		case "!=":
			valid = cmp != 0
		case ">":
			valid = cmp > 0
		case "<":
			valid = cmp < 0
		case ">=":
			valid = cmp >= 0
		case "<=":
			valid = cmp <= 0
		}

		if !valid {
			return false, fmt.Sprintf("field '%s' (%v) does not satisfy condition %s %v", field, value, op, expected)
		}
		return true, ""
	}
}

// compareValues 두 값 비교 (-1, 0, 1)
func compareValues(a, b any) int {
	// 숫자 비교
	aNum, aIsNum := toFloat64(a)
	bNum, bIsNum := toFloat64(b)
	if aIsNum && bIsNum {
		if aNum < bNum {
			return -1
		} else if aNum > bNum {
			return 1
		}
		return 0
	}

	// 문자열 비교
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.Compare(aStr, bStr)
}

// toFloat64 숫자로 변환
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// parseValue 문자열 값을 적절한 타입으로 파싱
func parseValue(s string) any {
	s = strings.TrimSpace(s)

	// 문자열 리터럴
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return s[1 : len(s)-1]
	}

	// 불리언
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	// null
	if s == "null" || s == "nil" {
		return nil
	}

	// 숫자
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		// 정수인 경우
		if f == float64(int64(f)) {
			return int64(f)
		}
		return f
	}

	return s
}

// parseArrayLiteral 배열 리터럴 파싱 [value1, value2, ...]
func parseArrayLiteral(s string) ([]any, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("invalid array literal: %s", s)
	}

	s = s[1 : len(s)-1]
	if s == "" {
		return []any{}, nil
	}

	parts := strings.Split(s, ",")
	values := make([]any, 0, len(parts))
	for _, part := range parts {
		values = append(values, parseValue(part))
	}
	return values, nil
}

// getNestedField 중첩 필드 가져오기 (예: "user.address.city")
func getNestedField(data map[string]any, fieldPath string) (any, bool) {
	parts := strings.Split(fieldPath, ".")
	current := any(data)

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, exists := v[part]
			if !exists {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}

	return current, true
}

// validateFieldType 필드 타입 검증
func validateFieldType(field types.ContractField, value any) error {
	if value == nil {
		return nil
	}

	switch field.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field '%s' expected string, got %T", field.Name, value)
		}
	case "number":
		if _, ok := toFloat64(value); !ok {
			return fmt.Errorf("field '%s' expected number, got %T", field.Name, value)
		}
	case "integer":
		if f, ok := toFloat64(value); !ok || f != float64(int64(f)) {
			return fmt.Errorf("field '%s' expected integer, got %T", field.Name, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field '%s' expected boolean, got %T", field.Name, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("field '%s' expected object, got %T", field.Name, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("field '%s' expected array, got %T", field.Name, value)
		}
	case "any":
		// any 타입은 항상 통과
	}
	return nil
}

// validateFieldConstraints 필드 제약 조건 검증
func validateFieldConstraints(field types.ContractField, value any) []error {
	var errs []error

	switch field.Type {
	case "string":
		str, _ := value.(string)

		if field.MinLength != nil && len(str) < *field.MinLength {
			errs = append(errs, fmt.Errorf("field '%s' length %d is less than minimum %d", field.Name, len(str), *field.MinLength))
		}
		if field.MaxLength != nil && len(str) > *field.MaxLength {
			errs = append(errs, fmt.Errorf("field '%s' length %d exceeds maximum %d", field.Name, len(str), *field.MaxLength))
		}
		if field.Pattern != "" {
			matched, err := regexp.MatchString(field.Pattern, str)
			if err != nil {
				errs = append(errs, fmt.Errorf("field '%s' pattern error: %w", field.Name, err))
			} else if !matched {
				errs = append(errs, fmt.Errorf("field '%s' does not match pattern '%s'", field.Name, field.Pattern))
			}
		}

	case "number", "integer":
		num, _ := toFloat64(value)

		if field.Min != nil && num < *field.Min {
			errs = append(errs, fmt.Errorf("field '%s' value %v is less than minimum %v", field.Name, num, *field.Min))
		}
		if field.Max != nil && num > *field.Max {
			errs = append(errs, fmt.Errorf("field '%s' value %v exceeds maximum %v", field.Name, num, *field.Max))
		}
	}

	// Enum 검증
	if len(field.Enum) > 0 {
		found := false
		for _, allowed := range field.Enum {
			if compareValues(value, allowed) == 0 {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("field '%s' value not in allowed values %v", field.Name, field.Enum))
		}
	}

	return errs
}
