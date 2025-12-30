// Package schema 데이터 스키마 정의 및 검증
package schema

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Schema 스키마 인터페이스
type Schema interface {
	Validate(data map[string]any) error
	ValidateField(field string, value any) error
}

// FieldType 필드 타입
type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeNumber  FieldType = "number"
	FieldTypeInteger FieldType = "integer"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeObject  FieldType = "object"
	FieldTypeArray   FieldType = "array"
	FieldTypeAny     FieldType = "any"
)

// FieldSchema 필드 스키마 정의
type FieldSchema struct {
	Name        string        `json:"name" yaml:"name"`
	Type        FieldType     `json:"type" yaml:"type"`
	Required    bool          `json:"required" yaml:"required"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Pattern     string        `json:"pattern,omitempty" yaml:"pattern,omitempty"`       // 정규식 패턴 (string 타입)
	MinLength   *int          `json:"min_length,omitempty" yaml:"min_length,omitempty"` // 최소 길이
	MaxLength   *int          `json:"max_length,omitempty" yaml:"max_length,omitempty"` // 최대 길이
	Min         *float64      `json:"min,omitempty" yaml:"min,omitempty"`               // 최소값 (number 타입)
	Max         *float64      `json:"max,omitempty" yaml:"max,omitempty"`               // 최대값 (number 타입)
	Enum        []any         `json:"enum,omitempty" yaml:"enum,omitempty"`             // 허용 값 목록
	Items       *FieldSchema  `json:"items,omitempty" yaml:"items,omitempty"`           // 배열 아이템 스키마
	Properties  []FieldSchema `json:"properties,omitempty" yaml:"properties,omitempty"` // 객체 프로퍼티
}

// DataSchema 데이터 스키마
type DataSchema struct {
	Name        string        `json:"name" yaml:"name"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Fields      []FieldSchema `json:"fields" yaml:"fields"`
	Strict      bool          `json:"strict" yaml:"strict"` // true면 정의되지 않은 필드 불허
}

// Validate 데이터 검증
func (s *DataSchema) Validate(data map[string]any) error {
	errors := &ValidationErrors{}

	// 필수 필드 검사
	for _, field := range s.Fields {
		value, exists := getNestedField(data, field.Name)

		if field.Required && !exists {
			errors.Add(field.Name, "필수 필드가 누락되었습니다")
			continue
		}

		if exists {
			if err := s.validateField(&field, value); err != nil {
				errors.Add(field.Name, err.Error())
			}
		}
	}

	// Strict 모드: 정의되지 않은 필드 검사
	if s.Strict {
		definedFields := make(map[string]bool)
		for _, field := range s.Fields {
			definedFields[field.Name] = true
		}

		for key := range data {
			if !definedFields[key] {
				errors.Add(key, "스키마에 정의되지 않은 필드입니다")
			}
		}
	}

	if errors.HasErrors() {
		return errors
	}
	return nil
}

// ValidateField 단일 필드 검증
func (s *DataSchema) ValidateField(fieldName string, value any) error {
	for _, field := range s.Fields {
		if field.Name == fieldName {
			return s.validateField(&field, value)
		}
	}
	return fmt.Errorf("필드 '%s'가 스키마에 정의되지 않았습니다", fieldName)
}

func (s *DataSchema) validateField(field *FieldSchema, value any) error {
	if value == nil {
		if field.Required {
			return fmt.Errorf("null 값은 허용되지 않습니다")
		}
		return nil
	}

	// 타입 검증
	if err := validateType(field.Type, value); err != nil {
		return err
	}

	// 추가 검증
	switch field.Type {
	case FieldTypeString:
		str, _ := value.(string)
		if field.MinLength != nil && len(str) < *field.MinLength {
			return fmt.Errorf("최소 길이 %d 이상이어야 합니다", *field.MinLength)
		}
		if field.MaxLength != nil && len(str) > *field.MaxLength {
			return fmt.Errorf("최대 길이 %d 이하여야 합니다", *field.MaxLength)
		}
		if field.Pattern != "" {
			matched, err := regexp.MatchString(field.Pattern, str)
			if err != nil {
				return fmt.Errorf("패턴 검증 오류: %w", err)
			}
			if !matched {
				return fmt.Errorf("패턴 '%s'와 일치하지 않습니다", field.Pattern)
			}
		}

	case FieldTypeNumber, FieldTypeInteger:
		num := toFloat64(value)
		if field.Min != nil && num < *field.Min {
			return fmt.Errorf("최소값 %v 이상이어야 합니다", *field.Min)
		}
		if field.Max != nil && num > *field.Max {
			return fmt.Errorf("최대값 %v 이하여야 합니다", *field.Max)
		}

	case FieldTypeArray:
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("배열이 아닙니다")
		}
		if field.Items != nil {
			for i, item := range arr {
				if err := s.validateField(field.Items, item); err != nil {
					return fmt.Errorf("배열[%d]: %w", i, err)
				}
			}
		}

	case FieldTypeObject:
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("객체가 아닙니다")
		}
		if len(field.Properties) > 0 {
			for _, prop := range field.Properties {
				propValue, exists := obj[prop.Name]
				if prop.Required && !exists {
					return fmt.Errorf("속성 '%s' 누락", prop.Name)
				}
				if exists {
					if err := s.validateField(&prop, propValue); err != nil {
						return fmt.Errorf("속성 '%s': %w", prop.Name, err)
					}
				}
			}
		}
	}

	// Enum 검증
	if len(field.Enum) > 0 {
		found := false
		for _, enumVal := range field.Enum {
			if reflect.DeepEqual(value, enumVal) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("허용되지 않는 값입니다. 허용 값: %v", field.Enum)
		}
	}

	return nil
}

func validateType(expectedType FieldType, value any) error {
	if expectedType == FieldTypeAny {
		return nil
	}

	switch expectedType {
	case FieldTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("문자열이어야 합니다")
		}
	case FieldTypeNumber:
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// OK
		default:
			return fmt.Errorf("숫자여야 합니다")
		}
	case FieldTypeInteger:
		switch v := value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// OK
		case float64:
			if v != float64(int64(v)) {
				return fmt.Errorf("정수여야 합니다")
			}
		default:
			return fmt.Errorf("정수여야 합니다")
		}
	case FieldTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("불리언이어야 합니다")
		}
	case FieldTypeArray:
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("배열이어야 합니다")
		}
	case FieldTypeObject:
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("객체여야 합니다")
		}
	}

	return nil
}

func toFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

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

// ValidationErrors 검증 오류 모음
type ValidationErrors struct {
	errors []FieldError
}

// FieldError 필드별 오류
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Add 오류 추가
func (e *ValidationErrors) Add(field, message string) {
	e.errors = append(e.errors, FieldError{Field: field, Message: message})
}

// HasErrors 오류 존재 여부
func (e *ValidationErrors) HasErrors() bool {
	return len(e.errors) > 0
}

// Error error 인터페이스 구현
func (e *ValidationErrors) Error() string {
	if len(e.errors) == 0 {
		return "no validation errors"
	}
	var msgs []string
	for _, err := range e.errors {
		msgs = append(msgs, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}
	return strings.Join(msgs, "; ")
}

// Errors 모든 오류 반환
func (e *ValidationErrors) Errors() []FieldError {
	return e.errors
}

// NewDataSchemaFromConfig 설정에서 DataSchema 생성
func NewDataSchemaFromConfig(config map[string]any) (*DataSchema, error) {
	schema := &DataSchema{}

	if name, ok := config["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := config["description"].(string); ok {
		schema.Description = desc
	}
	if strict, ok := config["strict"].(bool); ok {
		schema.Strict = strict
	}

	if fields, ok := config["fields"].([]any); ok {
		for _, f := range fields {
			if fieldMap, ok := f.(map[string]any); ok {
				field, err := parseFieldSchema(fieldMap)
				if err != nil {
					return nil, err
				}
				schema.Fields = append(schema.Fields, *field)
			}
		}
	}

	return schema, nil
}

func parseFieldSchema(config map[string]any) (*FieldSchema, error) {
	field := &FieldSchema{}

	if name, ok := config["name"].(string); ok {
		field.Name = name
	}
	if typ, ok := config["type"].(string); ok {
		field.Type = FieldType(typ)
	}
	if required, ok := config["required"].(bool); ok {
		field.Required = required
	}
	if desc, ok := config["description"].(string); ok {
		field.Description = desc
	}
	if pattern, ok := config["pattern"].(string); ok {
		field.Pattern = pattern
	}
	if minLen, ok := config["min_length"].(float64); ok {
		v := int(minLen)
		field.MinLength = &v
	}
	if maxLen, ok := config["max_length"].(float64); ok {
		v := int(maxLen)
		field.MaxLength = &v
	}
	if min, ok := config["min"].(float64); ok {
		field.Min = &min
	}
	if max, ok := config["max"].(float64); ok {
		field.Max = &max
	}
	if enum, ok := config["enum"].([]any); ok {
		field.Enum = enum
	}

	// 중첩 items (배열용)
	if items, ok := config["items"].(map[string]any); ok {
		itemSchema, err := parseFieldSchema(items)
		if err != nil {
			return nil, err
		}
		field.Items = itemSchema
	}

	// 중첩 properties (객체용)
	if props, ok := config["properties"].([]any); ok {
		for _, p := range props {
			if propMap, ok := p.(map[string]any); ok {
				propSchema, err := parseFieldSchema(propMap)
				if err != nil {
					return nil, err
				}
				field.Properties = append(field.Properties, *propSchema)
			}
		}
	}

	return field, nil
}
