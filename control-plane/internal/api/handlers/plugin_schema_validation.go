package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/conduix/conduix/shared/types"
)

// knownFieldTypes 는 GUI 폼이 렌더할 수 있는 필드 타입 집합.
// 여기 없는 타입이 스키마에 들어오면 폼이 깨지므로 등록 단계에서 거부한다.
var knownFieldTypes = map[types.FieldType]bool{
	types.FieldTypeString:   true,
	types.FieldTypeNumber:   true,
	types.FieldTypeInteger:  true,
	types.FieldTypeBoolean:  true,
	types.FieldTypeEnum:     true,
	types.FieldTypeArray:    true,
	types.FieldTypeObject:   true,
	types.FieldTypeJSON:     true,
	types.FieldTypeCode:     true,
	types.FieldTypeKeyValue: true,
	types.FieldTypeDuration: true,
	types.FieldTypeSecret:   true,
}

// validateConfigSchema 는 플러그인 등록 시 받은 config_schema(JSON)가 GUI 폼 생성에
// 안전한지 검증한다. 비면 통과(nullable — 폼은 JSON 폴백). 있으면 types.StageSchema 로
// 언마샬되는지 + 모든 필드(중첩 포함) Type 이 알려진 FieldType 인지 확인한다.
// 잘못된 스키마가 저장되면 GUI 폼이 깨지므로 저장 전에 막는다.
func validateConfigSchema(raw string) error {
	if raw == "" {
		return nil
	}
	var schema types.StageSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return fmt.Errorf("config_schema 가 types.StageSchema 로 파싱되지 않습니다: %w", err)
	}
	if schema.Type == "" {
		return fmt.Errorf("config_schema.type 이 비어 있습니다")
	}
	return validateSchemaFields(schema.Fields)
}

func validateSchemaFields(fields []types.StageFieldSchema) error {
	for _, f := range fields {
		if f.Name == "" {
			return fmt.Errorf("config_schema 필드에 name 이 없습니다")
		}
		if !knownFieldTypes[f.Type] {
			return fmt.Errorf("config_schema 필드 %q 의 알 수 없는 타입 %q", f.Name, f.Type)
		}
		// 중첩 object 필드 재귀 검증
		if len(f.Fields) > 0 {
			if err := validateSchemaFields(f.Fields); err != nil {
				return err
			}
		}
	}
	return nil
}
