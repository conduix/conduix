// Package filter 필터 레지스트리 (중앙화된 필터 관리)
// 백엔드와 프론트엔드가 동일한 필터 정의를 공유
package filter

import (
	"encoding/json"
	"fmt"
	"sync"
)

// OperatorDef 연산자 정의
type OperatorDef struct {
	// 코드에서 사용하는 ID
	ID string `json:"id"`

	// GUI에서 표시되는 라벨
	Label string `json:"label"`

	// 설명
	Description string `json:"description"`

	// 값이 필요한지 여부
	NeedsValue bool `json:"needsValue"`

	// 값 타입 (string, number, boolean, array, regex)
	ValueType string `json:"valueType,omitempty"`

	// 연산자 카테고리 (comparison, string, existence, collection)
	Category string `json:"category"`
}

// FilterRegistry 필터 레지스트리
type FilterRegistry struct {
	mu        sync.RWMutex
	operators map[string]*OperatorDef
	order     []string // 등록 순서 유지
}

// 전역 레지스트리
var globalRegistry = NewRegistry()

// NewRegistry 새 레지스트리 생성
func NewRegistry() *FilterRegistry {
	r := &FilterRegistry{
		operators: make(map[string]*OperatorDef),
		order:     make([]string, 0),
	}
	r.registerBuiltinOperators()
	return r
}

// Global 전역 레지스트리 반환
func Global() *FilterRegistry {
	return globalRegistry
}

// registerBuiltinOperators 내장 연산자 등록
func (r *FilterRegistry) registerBuiltinOperators() {
	// === 비교 연산자 ===
	_ = r.Register(&OperatorDef{
		ID:          "eq",
		Label:       "같음 (==)",
		Description: "값이 동일한 경우",
		NeedsValue:  true,
		ValueType:   "string",
		Category:    "comparison",
	})

	_ = r.Register(&OperatorDef{
		ID:          "neq",
		Label:       "다름 (!=)",
		Description: "값이 다른 경우",
		NeedsValue:  true,
		ValueType:   "string",
		Category:    "comparison",
	})

	_ = r.Register(&OperatorDef{
		ID:          "gt",
		Label:       "초과 (>)",
		Description: "값보다 큰 경우",
		NeedsValue:  true,
		ValueType:   "number",
		Category:    "comparison",
	})

	_ = r.Register(&OperatorDef{
		ID:          "gte",
		Label:       "이상 (>=)",
		Description: "값 이상인 경우",
		NeedsValue:  true,
		ValueType:   "number",
		Category:    "comparison",
	})

	_ = r.Register(&OperatorDef{
		ID:          "lt",
		Label:       "미만 (<)",
		Description: "값보다 작은 경우",
		NeedsValue:  true,
		ValueType:   "number",
		Category:    "comparison",
	})

	_ = r.Register(&OperatorDef{
		ID:          "lte",
		Label:       "이하 (<=)",
		Description: "값 이하인 경우",
		NeedsValue:  true,
		ValueType:   "number",
		Category:    "comparison",
	})

	// === 문자열 연산자 ===
	_ = r.Register(&OperatorDef{
		ID:          "contains",
		Label:       "포함",
		Description: "문자열에 값이 포함된 경우",
		NeedsValue:  true,
		ValueType:   "string",
		Category:    "string",
	})

	_ = r.Register(&OperatorDef{
		ID:          "startswith",
		Label:       "시작",
		Description: "문자열이 값으로 시작하는 경우",
		NeedsValue:  true,
		ValueType:   "string",
		Category:    "string",
	})

	_ = r.Register(&OperatorDef{
		ID:          "endswith",
		Label:       "끝",
		Description: "문자열이 값으로 끝나는 경우",
		NeedsValue:  true,
		ValueType:   "string",
		Category:    "string",
	})

	_ = r.Register(&OperatorDef{
		ID:          "regex",
		Label:       "정규식",
		Description: "정규식 패턴과 일치하는 경우",
		NeedsValue:  true,
		ValueType:   "regex",
		Category:    "string",
	})

	// === 존재 연산자 ===
	_ = r.Register(&OperatorDef{
		ID:          "exists",
		Label:       "존재함",
		Description: "필드가 존재하는 경우",
		NeedsValue:  false,
		Category:    "existence",
	})

	_ = r.Register(&OperatorDef{
		ID:          "notexists",
		Label:       "존재안함",
		Description: "필드가 존재하지 않는 경우",
		NeedsValue:  false,
		Category:    "existence",
	})

	_ = r.Register(&OperatorDef{
		ID:          "null",
		Label:       "NULL",
		Description: "값이 null인 경우",
		NeedsValue:  false,
		Category:    "existence",
	})

	_ = r.Register(&OperatorDef{
		ID:          "notnull",
		Label:       "NOT NULL",
		Description: "값이 null이 아닌 경우",
		NeedsValue:  false,
		Category:    "existence",
	})

	// === 컬렉션 연산자 ===
	_ = r.Register(&OperatorDef{
		ID:          "in",
		Label:       "목록 포함",
		Description: "값이 목록에 포함된 경우",
		NeedsValue:  true,
		ValueType:   "array",
		Category:    "collection",
	})

	_ = r.Register(&OperatorDef{
		ID:          "notin",
		Label:       "목록 미포함",
		Description: "값이 목록에 포함되지 않은 경우",
		NeedsValue:  true,
		ValueType:   "array",
		Category:    "collection",
	})
}

// Register 연산자 등록
func (r *FilterRegistry) Register(op *OperatorDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.operators[op.ID]; exists {
		return fmt.Errorf("operator %s already registered", op.ID)
	}

	r.operators[op.ID] = op
	r.order = append(r.order, op.ID)
	return nil
}

// Unregister 연산자 제거
func (r *FilterRegistry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.operators[id]; !exists {
		return fmt.Errorf("operator %s not found", id)
	}

	delete(r.operators, id)

	// order에서도 제거
	newOrder := make([]string, 0, len(r.order)-1)
	for _, oid := range r.order {
		if oid != id {
			newOrder = append(newOrder, oid)
		}
	}
	r.order = newOrder

	return nil
}

// Get 연산자 조회
func (r *FilterRegistry) Get(id string) (*OperatorDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	op, ok := r.operators[id]
	return op, ok
}

// List 모든 연산자 목록 (등록 순서대로)
func (r *FilterRegistry) List() []*OperatorDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*OperatorDef, 0, len(r.order))
	for _, id := range r.order {
		if op, ok := r.operators[id]; ok {
			result = append(result, op)
		}
	}
	return result
}

// ListByCategory 카테고리별 연산자 목록
func (r *FilterRegistry) ListByCategory(category string) []*OperatorDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*OperatorDef
	for _, id := range r.order {
		if op, ok := r.operators[id]; ok && op.Category == category {
			result = append(result, op)
		}
	}
	return result
}

// Categories 모든 카테고리 목록
func (r *FilterRegistry) Categories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categorySet := make(map[string]bool)
	var categories []string

	for _, id := range r.order {
		if op, ok := r.operators[id]; ok {
			if !categorySet[op.Category] {
				categorySet[op.Category] = true
				categories = append(categories, op.Category)
			}
		}
	}
	return categories
}

// IsValid 연산자 ID가 유효한지 확인
func (r *FilterRegistry) IsValid(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.operators[id]
	return ok
}

// ToJSON 레지스트리를 JSON으로 내보내기 (프론트엔드 동기화용)
func (r *FilterRegistry) ToJSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	output := struct {
		Operators  []*OperatorDef `json:"operators"`
		Categories []string       `json:"categories"`
	}{
		Operators:  r.List(),
		Categories: r.Categories(),
	}

	return json.MarshalIndent(output, "", "  ")
}

// ToTypeScript TypeScript 타입 정의 생성 (프론트엔드 동기화용)
func (r *FilterRegistry) ToTypeScript() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb string
	sb += "// 자동 생성됨 - 직접 수정하지 마세요\n"
	sb += "// go generate로 생성됨\n\n"

	// Operator union type
	sb += "export type Operator =\n"
	for i, id := range r.order {
		if i == len(r.order)-1 {
			sb += fmt.Sprintf("  | '%s';\n\n", id)
		} else {
			sb += fmt.Sprintf("  | '%s'\n", id)
		}
	}

	// Operator metadata
	sb += "export interface OperatorMeta {\n"
	sb += "  id: Operator;\n"
	sb += "  label: string;\n"
	sb += "  description: string;\n"
	sb += "  needsValue: boolean;\n"
	sb += "  valueType?: 'string' | 'number' | 'boolean' | 'array' | 'regex';\n"
	sb += "  category: string;\n"
	sb += "}\n\n"

	// Operator list
	sb += "export const OPERATORS: OperatorMeta[] = [\n"
	for _, id := range r.order {
		if op, ok := r.operators[id]; ok {
			valueType := ""
			if op.ValueType != "" {
				valueType = fmt.Sprintf(", valueType: '%s'", op.ValueType)
			}
			sb += fmt.Sprintf("  { id: '%s', label: '%s', description: '%s', needsValue: %v%s, category: '%s' },\n",
				op.ID, op.Label, op.Description, op.NeedsValue, valueType, op.Category)
		}
	}
	sb += "];\n\n"

	// Category type
	sb += "export type OperatorCategory =\n"
	categories := r.Categories()
	for i, cat := range categories {
		if i == len(categories)-1 {
			sb += fmt.Sprintf("  | '%s';\n", cat)
		} else {
			sb += fmt.Sprintf("  | '%s'\n", cat)
		}
	}

	return sb
}

// RegisterCustom 커스텀 연산자 등록 헬퍼
func RegisterCustom(id, label, description string, needsValue bool, valueType, category string) error {
	return Global().Register(&OperatorDef{
		ID:          id,
		Label:       label,
		Description: description,
		NeedsValue:  needsValue,
		ValueType:   valueType,
		Category:    category,
	})
}

// UnregisterCustom 커스텀 연산자 제거 헬퍼
func UnregisterCustom(id string) error {
	return Global().Unregister(id)
}
