package filter

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Converter 필터 변환기 (Expression ↔ Structured)
type Converter struct{}

// NewConverter 변환기 생성
func NewConverter() *Converter {
	return &Converter{}
}

// ExpressionToStructured 문자열 표현식을 구조화된 필터로 변환
func (c *Converter) ExpressionToStructured(expr string) (*Filter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("빈 표현식입니다")
	}

	root, err := c.parseExpression(expr)
	if err != nil {
		return nil, err
	}

	return &Filter{
		Version: "1",
		Root:    root,
	}, nil
}

// StructuredToExpression 구조화된 필터를 문자열 표현식으로 변환
func (c *Converter) StructuredToExpression(filter *Filter) (string, error) {
	if filter == nil {
		return "", fmt.Errorf("필터가 nil입니다")
	}

	if filter.Expression != "" {
		return filter.Expression, nil
	}

	if filter.Root == nil {
		return "", fmt.Errorf("루트 노드가 없습니다")
	}

	return c.nodeToExpression(filter.Root)
}

// parseExpression 표현식 파싱
func (c *Converter) parseExpression(expr string) (*FilterNode, error) {
	expr = strings.TrimSpace(expr)

	// OR 분할 (우선순위 낮음)
	if strings.Contains(expr, "||") {
		parts := splitLogical(expr, "||")
		if len(parts) > 1 {
			return c.parseLogicalGroup(parts, LogicalOr)
		}
	}

	// AND 분할
	if strings.Contains(expr, "&&") {
		parts := splitLogical(expr, "&&")
		if len(parts) > 1 {
			return c.parseLogicalGroup(parts, LogicalAnd)
		}
	}

	// 단일 조건
	return c.parseSingleCondition(expr)
}

// parseLogicalGroup 논리 그룹 파싱
func (c *Converter) parseLogicalGroup(parts []string, op LogicalOperator) (*FilterNode, error) {
	conditions := make([]FilterNode, 0, len(parts))

	for _, part := range parts {
		node, err := c.parseExpression(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, *node)
	}

	return &FilterNode{
		Type: "group",
		Group: &ConditionGroup{
			ID:         generateID(),
			Operator:   op,
			Conditions: conditions,
		},
	}, nil
}

// parseSingleCondition 단일 조건 파싱
func (c *Converter) parseSingleCondition(expr string) (*FilterNode, error) {
	expr = strings.TrimSpace(expr)

	// 괄호 제거
	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		return c.parseExpression(expr[1 : len(expr)-1])
	}

	// exists 체크
	if strings.HasSuffix(expr, " exists") {
		field := strings.TrimSuffix(expr, " exists")
		field = strings.TrimPrefix(strings.TrimSpace(field), ".")
		return &FilterNode{
			Type: "condition",
			Condition: &Condition{
				ID:    generateID(),
				Field: field,
				Op:    OpExists,
			},
		}, nil
	}

	// 연산자 파싱
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
		if idx := strings.Index(expr, opDef.symbol); idx > 0 {
			field := strings.TrimPrefix(strings.TrimSpace(expr[:idx]), ".")
			value := strings.Trim(strings.TrimSpace(expr[idx+len(opDef.symbol):]), "'\"")

			return &FilterNode{
				Type: "condition",
				Condition: &Condition{
					ID:    generateID(),
					Field: field,
					Op:    opDef.op,
					Value: value,
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("파싱할 수 없는 조건: %s", expr)
}

// nodeToExpression 노드를 표현식으로 변환
func (c *Converter) nodeToExpression(node *FilterNode) (string, error) {
	switch node.Type {
	case "condition":
		return c.conditionToExpression(node.Condition)
	case "group":
		return c.groupToExpression(node.Group)
	default:
		return "", fmt.Errorf("알 수 없는 노드 타입: %s", node.Type)
	}
}

// conditionToExpression 조건을 표현식으로 변환
func (c *Converter) conditionToExpression(cond *Condition) (string, error) {
	field := "." + cond.Field

	switch cond.Op {
	case OpExists:
		return fmt.Sprintf("%s exists", field), nil
	case OpNotExists:
		return fmt.Sprintf("!(%s exists)", field), nil
	case OpIsNull:
		return fmt.Sprintf("%s == null", field), nil
	case OpIsNotNull:
		return fmt.Sprintf("%s != null", field), nil
	case OpEqual:
		return fmt.Sprintf("%s == '%v'", field, cond.Value), nil
	case OpNotEqual:
		return fmt.Sprintf("%s != '%v'", field, cond.Value), nil
	case OpGreaterThan:
		return fmt.Sprintf("%s > %v", field, cond.Value), nil
	case OpGreaterOrEqual:
		return fmt.Sprintf("%s >= %v", field, cond.Value), nil
	case OpLessThan:
		return fmt.Sprintf("%s < %v", field, cond.Value), nil
	case OpLessOrEqual:
		return fmt.Sprintf("%s <= %v", field, cond.Value), nil
	case OpContains:
		return fmt.Sprintf("%s contains '%v'", field, cond.Value), nil
	case OpStartsWith:
		return fmt.Sprintf("%s startswith '%v'", field, cond.Value), nil
	case OpEndsWith:
		return fmt.Sprintf("%s endswith '%v'", field, cond.Value), nil
	case OpRegex:
		return fmt.Sprintf("%s ~= '%v'", field, cond.Value), nil
	case OpIn:
		return fmt.Sprintf("%s in %v", field, cond.Value), nil
	case OpNotIn:
		return fmt.Sprintf("%s notin %v", field, cond.Value), nil
	default:
		return "", fmt.Errorf("지원하지 않는 연산자: %s", cond.Op)
	}
}

// groupToExpression 그룹을 표현식으로 변환
func (c *Converter) groupToExpression(group *ConditionGroup) (string, error) {
	if len(group.Conditions) == 0 {
		return "", nil
	}

	if len(group.Conditions) == 1 {
		return c.nodeToExpression(&group.Conditions[0])
	}

	var separator string
	switch group.Operator {
	case LogicalAnd:
		separator = " && "
	case LogicalOr:
		separator = " || "
	default:
		return "", fmt.Errorf("알 수 없는 논리 연산자: %s", group.Operator)
	}

	parts := make([]string, len(group.Conditions))
	for i, cond := range group.Conditions {
		expr, err := c.nodeToExpression(&cond)
		if err != nil {
			return "", err
		}
		// 중첩 그룹은 괄호로 감싸기
		if cond.Type == "group" && cond.Group.Operator != group.Operator {
			expr = "(" + expr + ")"
		}
		parts[i] = expr
	}

	return strings.Join(parts, separator), nil
}

// splitLogical 논리 연산자로 분할 (괄호 고려)
func splitLogical(expr string, sep string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	for i := 0; i < len(expr); i++ {
		ch := expr[i]

		if ch == '(' {
			depth++
			current.WriteByte(ch)
		} else if ch == ')' {
			depth--
			current.WriteByte(ch)
		} else if depth == 0 && i+len(sep) <= len(expr) && expr[i:i+len(sep)] == sep {
			parts = append(parts, current.String())
			current.Reset()
			i += len(sep) - 1
		} else {
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// generateID GUI용 고유 ID 생성
func generateID() string {
	return uuid.New().String()[:8]
}
