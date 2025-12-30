package filter

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Evaluator 필터 평가기
type Evaluator struct {
	filter *Filter
}

// NewEvaluator 평가기 생성
func NewEvaluator(filter *Filter) (*Evaluator, error) {
	if filter == nil {
		return nil, fmt.Errorf("필터가 nil입니다")
	}
	return &Evaluator{filter: filter}, nil
}

// Evaluate 레코드에 대해 필터 평가
func (e *Evaluator) Evaluate(data map[string]any) (bool, error) {
	// 구조화된 필터
	if e.filter.Root != nil {
		return e.evaluateNode(e.filter.Root, data)
	}

	// 문자열 표현식
	if e.filter.Expression != "" {
		return e.evaluateExpression(e.filter.Expression, data)
	}

	// 필터가 없으면 통과
	return true, nil
}

// evaluateNode 노드 평가
func (e *Evaluator) evaluateNode(node *FilterNode, data map[string]any) (bool, error) {
	switch node.Type {
	case "condition":
		return e.evaluateCondition(node.Condition, data)
	case "group":
		return e.evaluateGroup(node.Group, data)
	default:
		return false, fmt.Errorf("알 수 없는 노드 타입: %s", node.Type)
	}
}

// evaluateGroup 그룹 평가
func (e *Evaluator) evaluateGroup(group *ConditionGroup, data map[string]any) (bool, error) {
	if len(group.Conditions) == 0 {
		return true, nil
	}

	switch group.Operator {
	case LogicalAnd:
		for _, cond := range group.Conditions {
			result, err := e.evaluateNode(&cond, data)
			if err != nil {
				return false, err
			}
			if !result {
				return false, nil // 하나라도 false면 전체 false
			}
		}
		return true, nil

	case LogicalOr:
		for _, cond := range group.Conditions {
			result, err := e.evaluateNode(&cond, data)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil // 하나라도 true면 전체 true
			}
		}
		return false, nil

	default:
		return false, fmt.Errorf("알 수 없는 논리 연산자: %s", group.Operator)
	}
}

// evaluateCondition 조건 평가
func (e *Evaluator) evaluateCondition(cond *Condition, data map[string]any) (bool, error) {
	// 필드 값 가져오기
	fieldValue, exists := getNestedValue(data, cond.Field)

	// 존재 여부 연산자
	switch cond.Op {
	case OpExists:
		return exists, nil
	case OpNotExists:
		return !exists, nil
	case OpIsNull:
		return !exists || fieldValue == nil, nil
	case OpIsNotNull:
		return exists && fieldValue != nil, nil
	}

	// 필드가 없으면 false
	if !exists {
		return false, nil
	}

	// 비교 연산
	return e.compare(fieldValue, cond.Op, cond.Value)
}

// compare 값 비교
func (e *Evaluator) compare(fieldValue any, op Operator, compareValue any) (bool, error) {
	switch op {
	case OpEqual:
		return equals(fieldValue, compareValue), nil

	case OpNotEqual:
		return !equals(fieldValue, compareValue), nil

	case OpGreaterThan:
		result, err := compareNumbers(fieldValue, compareValue)
		return result > 0, err

	case OpGreaterOrEqual:
		result, err := compareNumbers(fieldValue, compareValue)
		return result >= 0, err

	case OpLessThan:
		result, err := compareNumbers(fieldValue, compareValue)
		return result < 0, err

	case OpLessOrEqual:
		result, err := compareNumbers(fieldValue, compareValue)
		return result <= 0, err

	case OpContains:
		return strings.Contains(toString(fieldValue), toString(compareValue)), nil

	case OpStartsWith:
		return strings.HasPrefix(toString(fieldValue), toString(compareValue)), nil

	case OpEndsWith:
		return strings.HasSuffix(toString(fieldValue), toString(compareValue)), nil

	case OpRegex:
		re, err := regexp.Compile(toString(compareValue))
		if err != nil {
			return false, fmt.Errorf("유효하지 않은 정규식: %w", err)
		}
		return re.MatchString(toString(fieldValue)), nil

	case OpIn:
		return inArray(fieldValue, compareValue), nil

	case OpNotIn:
		return !inArray(fieldValue, compareValue), nil

	default:
		return false, fmt.Errorf("지원하지 않는 연산자: %s", op)
	}
}

// evaluateExpression 문자열 표현식 평가 (하위 호환)
func (e *Evaluator) evaluateExpression(expr string, data map[string]any) (bool, error) {
	expr = strings.TrimSpace(expr)

	// AND 처리
	if strings.Contains(expr, "&&") {
		parts := strings.Split(expr, "&&")
		for _, part := range parts {
			result, err := e.evaluateExpression(strings.TrimSpace(part), data)
			if err != nil {
				return false, err
			}
			if !result {
				return false, nil
			}
		}
		return true, nil
	}

	// OR 처리
	if strings.Contains(expr, "||") {
		parts := strings.Split(expr, "||")
		for _, part := range parts {
			result, err := e.evaluateExpression(strings.TrimSpace(part), data)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil
			}
		}
		return false, nil
	}

	// 단일 조건 파싱
	return e.evaluateSingleExpression(expr, data)
}

// evaluateSingleExpression 단일 표현식 평가
func (e *Evaluator) evaluateSingleExpression(expr string, data map[string]any) (bool, error) {
	expr = strings.TrimSpace(expr)

	// exists 체크
	if strings.HasSuffix(expr, " exists") {
		field := strings.TrimSuffix(expr, " exists")
		field = strings.TrimPrefix(strings.TrimSpace(field), ".")
		_, exists := getNestedValue(data, field)
		return exists, nil
	}

	// 연산자별 처리
	operators := []struct {
		symbol string
		op     Operator
	}{
		{"~=", OpRegex},
		{"!=", OpNotEqual},
		{">=", OpGreaterOrEqual},
		{"<=", OpLessOrEqual},
		{"==", OpEqual},
		{">", OpGreaterThan},
		{"<", OpLessThan},
	}

	for _, opDef := range operators {
		if strings.Contains(expr, opDef.symbol) {
			parts := strings.SplitN(expr, opDef.symbol, 2)
			if len(parts) == 2 {
				field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
				value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

				fieldValue, exists := getNestedValue(data, field)
				if !exists {
					return false, nil
				}

				return e.compare(fieldValue, opDef.op, value)
			}
		}
	}

	return false, fmt.Errorf("파싱할 수 없는 표현식: %s", expr)
}

// getNestedValue 중첩된 필드 값 가져오기 (예: "user.profile.name")
func getNestedValue(data map[string]any, field string) (any, bool) {
	parts := strings.Split(field, ".")
	var current any = data

	for _, part := range parts {
		if part == "" {
			continue
		}

		switch v := current.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}

	return current, true
}

// equals 값 동등 비교
func equals(a, b any) bool {
	// 타입이 다른 경우 문자열로 비교
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return toString(a) == toString(b)
	}
	return reflect.DeepEqual(a, b)
}

// compareNumbers 숫자 비교 (-1, 0, 1 반환)
func compareNumbers(a, b any) (int, error) {
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)

	if !aOk || !bOk {
		// 문자열 비교로 폴백
		aStr := toString(a)
		bStr := toString(b)
		if aStr < bStr {
			return -1, nil
		} else if aStr > bStr {
			return 1, nil
		}
		return 0, nil
	}

	if aFloat < bFloat {
		return -1, nil
	} else if aFloat > bFloat {
		return 1, nil
	}
	return 0, nil
}

// toFloat64 숫자로 변환
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

// toString 문자열로 변환
func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// inArray 배열에 값이 포함되어 있는지 확인
func inArray(value any, arr any) bool {
	switch a := arr.(type) {
	case []any:
		for _, item := range a {
			if equals(value, item) {
				return true
			}
		}
	case []string:
		strVal := toString(value)
		for _, item := range a {
			if strVal == item {
				return true
			}
		}
	}
	return false
}
