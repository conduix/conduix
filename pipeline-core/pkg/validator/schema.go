package validator

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

// SchemaValidator JSON Schema 검증기
type SchemaValidator struct {
	schema *gojsonschema.Schema
	raw    string
}

// ValidatorCache 스키마 검증기 캐시
var (
	validatorCache   = make(map[string]*SchemaValidator)
	validatorCacheMu sync.RWMutex
)

// NewSchemaValidator JSON Schema 검증기 생성
func NewSchemaValidator(schemaJSON string) (*SchemaValidator, error) {
	if schemaJSON == "" {
		return nil, fmt.Errorf("schema JSON is empty")
	}

	// 캐시 확인
	validatorCacheMu.RLock()
	if cached, ok := validatorCache[schemaJSON]; ok {
		validatorCacheMu.RUnlock()
		return cached, nil
	}
	validatorCacheMu.RUnlock()

	// 스키마 파싱
	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON schema: %w", err)
	}

	validator := &SchemaValidator{
		schema: schema,
		raw:    schemaJSON,
	}

	// 캐시에 저장
	validatorCacheMu.Lock()
	validatorCache[schemaJSON] = validator
	validatorCacheMu.Unlock()

	return validator, nil
}

// ValidationResult 검증 결과
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// Validate 데이터 검증
func (v *SchemaValidator) Validate(data map[string]any) *ValidationResult {
	documentLoader := gojsonschema.NewGoLoader(data)
	result, err := v.schema.Validate(documentLoader)
	if err != nil {
		return &ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("validation error: %v", err)},
		}
	}

	if result.Valid() {
		return &ValidationResult{Valid: true}
	}

	errors := make([]string, 0, len(result.Errors()))
	for _, desc := range result.Errors() {
		errors = append(errors, desc.String())
	}

	return &ValidationResult{
		Valid:  false,
		Errors: errors,
	}
}

// ValidateJSON JSON 문자열 검증
func (v *SchemaValidator) ValidateJSON(jsonData string) *ValidationResult {
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return &ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("invalid JSON: %v", err)},
		}
	}
	return v.Validate(data)
}

// ValidateBatch 배치 검증 (여러 레코드)
func (v *SchemaValidator) ValidateBatch(records []map[string]any) *BatchValidationResult {
	result := &BatchValidationResult{
		Total:          len(records),
		Valid:          0,
		Invalid:        0,
		InvalidRecords: make([]InvalidRecord, 0),
	}

	for i, record := range records {
		vr := v.Validate(record)
		if vr.Valid {
			result.Valid++
		} else {
			result.Invalid++
			result.InvalidRecords = append(result.InvalidRecords, InvalidRecord{
				Index:  i,
				Errors: vr.Errors,
			})
		}
	}

	return result
}

// BatchValidationResult 배치 검증 결과
type BatchValidationResult struct {
	Total          int             `json:"total"`
	Valid          int             `json:"valid"`
	Invalid        int             `json:"invalid"`
	InvalidRecords []InvalidRecord `json:"invalid_records,omitempty"`
}

// InvalidRecord 유효하지 않은 레코드
type InvalidRecord struct {
	Index  int      `json:"index"`
	Errors []string `json:"errors"`
}

// ClearCache 캐시 초기화
func ClearCache() {
	validatorCacheMu.Lock()
	validatorCache = make(map[string]*SchemaValidator)
	validatorCacheMu.Unlock()
}
