// Package filter 필터 모델 정의 (GUI/YAML 호환)
package filter

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Operator 비교 연산자
type Operator string

const (
	OpEqual          Operator = "eq"         // ==
	OpNotEqual       Operator = "neq"        // !=
	OpGreaterThan    Operator = "gt"         // >
	OpGreaterOrEqual Operator = "gte"        // >=
	OpLessThan       Operator = "lt"         // <
	OpLessOrEqual    Operator = "lte"        // <=
	OpContains       Operator = "contains"   // 문자열 포함
	OpStartsWith     Operator = "startswith" // 문자열 시작
	OpEndsWith       Operator = "endswith"   // 문자열 끝
	OpRegex          Operator = "regex"      // 정규식
	OpExists         Operator = "exists"     // 필드 존재
	OpNotExists      Operator = "notexists"  // 필드 미존재
	OpIn             Operator = "in"         // 값 목록 포함
	OpNotIn          Operator = "notin"      // 값 목록 미포함
	OpIsNull         Operator = "null"       // null 체크
	OpIsNotNull      Operator = "notnull"    // not null 체크
)

// LogicalOperator 논리 연산자
type LogicalOperator string

const (
	LogicalAnd LogicalOperator = "and"
	LogicalOr  LogicalOperator = "or"
)

// Condition 단일 조건
type Condition struct {
	ID    string   `json:"id,omitempty" yaml:"id,omitempty"`       // GUI용 고유 ID
	Field string   `json:"field" yaml:"field"`                     // 필드 경로 (예: "user.name")
	Op    Operator `json:"op" yaml:"op"`                           // 연산자
	Value any      `json:"value,omitempty" yaml:"value,omitempty"` // 비교값
}

// ConditionGroup 조건 그룹 (AND/OR)
type ConditionGroup struct {
	ID         string          `json:"id,omitempty" yaml:"id,omitempty"` // GUI용 고유 ID
	Operator   LogicalOperator `json:"operator" yaml:"operator"`         // and, or
	Conditions []FilterNode    `json:"conditions" yaml:"conditions"`     // 조건 목록
}

// FilterNode 필터 노드 (조건 또는 그룹)
type FilterNode struct {
	Type      string          `json:"type" yaml:"type"` // "condition" 또는 "group"
	Condition *Condition      `json:"condition,omitempty" yaml:"condition,omitempty"`
	Group     *ConditionGroup `json:"group,omitempty" yaml:"group,omitempty"`
}

// Filter 전체 필터 정의
type Filter struct {
	// 버전 (향후 마이그레이션용)
	Version string `json:"version,omitempty" yaml:"version,omitempty"`

	// 루트 노드 (단일 조건 또는 그룹)
	Root *FilterNode `json:"root,omitempty" yaml:"root,omitempty"`

	// 간단한 문자열 표현 (하위 호환용)
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
}

// FilterConfig YAML 설정에서 사용되는 필터 설정
// 문자열 또는 구조화된 객체 모두 지원
type FilterConfig struct {
	filter *Filter
	raw    string
}

// UnmarshalYAML 커스텀 YAML 언마샬링
func (fc *FilterConfig) UnmarshalYAML(node *yaml.Node) error {
	// 문자열인 경우 (간단한 표현식)
	if node.Kind == yaml.ScalarNode {
		fc.raw = node.Value
		fc.filter = &Filter{
			Expression: node.Value,
		}
		return nil
	}

	// 구조화된 객체인 경우
	var filter Filter
	if err := node.Decode(&filter); err != nil {
		return err
	}
	fc.filter = &filter
	return nil
}

// MarshalYAML 커스텀 YAML 마샬링
func (fc FilterConfig) MarshalYAML() (interface{}, error) {
	// 간단한 표현식만 있는 경우 문자열로 출력
	if fc.filter != nil && fc.filter.Root == nil && fc.filter.Expression != "" {
		return fc.filter.Expression, nil
	}
	return fc.filter, nil
}

// UnmarshalJSON 커스텀 JSON 언마샬링
func (fc *FilterConfig) UnmarshalJSON(data []byte) error {
	// 문자열인지 확인
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		fc.raw = str
		fc.filter = &Filter{
			Expression: str,
		}
		return nil
	}

	// 구조화된 객체
	var filter Filter
	if err := json.Unmarshal(data, &filter); err != nil {
		return err
	}
	fc.filter = &filter
	return nil
}

// MarshalJSON 커스텀 JSON 마샬링
func (fc FilterConfig) MarshalJSON() ([]byte, error) {
	// GUI에서는 항상 구조화된 형태로 반환
	return json.Marshal(fc.filter)
}

// GetFilter 필터 객체 반환
func (fc *FilterConfig) GetFilter() *Filter {
	return fc.filter
}

// IsStructured 구조화된 필터인지 확인
func (fc *FilterConfig) IsStructured() bool {
	return fc.filter != nil && fc.filter.Root != nil
}

// GetExpression 표현식 문자열 반환
func (fc *FilterConfig) GetExpression() string {
	if fc.filter != nil {
		return fc.filter.Expression
	}
	return fc.raw
}

// NewCondition 새 조건 생성
func NewCondition(field string, op Operator, value any) *FilterNode {
	return &FilterNode{
		Type: "condition",
		Condition: &Condition{
			Field: field,
			Op:    op,
			Value: value,
		},
	}
}

// NewGroup 새 그룹 생성
func NewGroup(op LogicalOperator, conditions ...*FilterNode) *FilterNode {
	nodes := make([]FilterNode, len(conditions))
	for i, c := range conditions {
		nodes[i] = *c
	}
	return &FilterNode{
		Type: "group",
		Group: &ConditionGroup{
			Operator:   op,
			Conditions: nodes,
		},
	}
}

// And AND 그룹 생성 헬퍼
func And(conditions ...*FilterNode) *FilterNode {
	return NewGroup(LogicalAnd, conditions...)
}

// Or OR 그룹 생성 헬퍼
func Or(conditions ...*FilterNode) *FilterNode {
	return NewGroup(LogicalOr, conditions...)
}

// Validate 필터 유효성 검증
func (f *Filter) Validate() error {
	if f.Expression != "" && f.Root != nil {
		return fmt.Errorf("expression과 root는 동시에 사용할 수 없습니다")
	}
	if f.Expression == "" && f.Root == nil {
		return fmt.Errorf("expression 또는 root 중 하나는 필수입니다")
	}
	if f.Root != nil {
		return f.Root.Validate()
	}
	return nil
}

// Validate FilterNode 유효성 검증
func (n *FilterNode) Validate() error {
	switch n.Type {
	case "condition":
		if n.Condition == nil {
			return fmt.Errorf("condition 타입이지만 condition 객체가 없습니다")
		}
		return n.Condition.Validate()
	case "group":
		if n.Group == nil {
			return fmt.Errorf("group 타입이지만 group 객체가 없습니다")
		}
		return n.Group.Validate()
	default:
		return fmt.Errorf("알 수 없는 노드 타입: %s", n.Type)
	}
}

// Validate Condition 유효성 검증
func (c *Condition) Validate() error {
	if c.Field == "" {
		return fmt.Errorf("필드명이 필요합니다")
	}
	if c.Op == "" {
		return fmt.Errorf("연산자가 필요합니다")
	}

	// 값이 필요 없는 연산자
	noValueOps := map[Operator]bool{
		OpExists:    true,
		OpNotExists: true,
		OpIsNull:    true,
		OpIsNotNull: true,
	}

	if !noValueOps[c.Op] && c.Value == nil {
		return fmt.Errorf("연산자 %s는 값이 필요합니다", c.Op)
	}

	return nil
}

// Validate ConditionGroup 유효성 검증
func (g *ConditionGroup) Validate() error {
	if g.Operator != LogicalAnd && g.Operator != LogicalOr {
		return fmt.Errorf("유효하지 않은 논리 연산자: %s", g.Operator)
	}
	if len(g.Conditions) == 0 {
		return fmt.Errorf("그룹에 최소 하나의 조건이 필요합니다")
	}
	for i, cond := range g.Conditions {
		if err := cond.Validate(); err != nil {
			return fmt.Errorf("조건[%d]: %w", i, err)
		}
	}
	return nil
}
