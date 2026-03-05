// Package types Stage Schema 정의
// Stage 구현과 GUI를 연결하는 메타데이터
package types

// StageSchema Stage 메타데이터 (GUI 자동 생성용)
type StageSchema struct {
	Type        string             `json:"type"`                  // stage 타입 (filter, remap, contract 등)
	DisplayName string             `json:"display_name"`          // UI 표시명
	Description string             `json:"description,omitempty"` // 설명
	Category    StageCategory      `json:"category"`              // 카테고리 (transform, output, validation)
	Icon        string             `json:"icon,omitempty"`        // Material UI 아이콘 이름
	Color       string             `json:"color,omitempty"`       // 테마 색상
	Fields      []StageFieldSchema `json:"fields"`                // 설정 필드들
	// 복잡한 UI가 필요한 경우 커스텀 에디터 지정
	CustomEditor string `json:"custom_editor,omitempty"` // 예: "RuleBuilder", "CircuitBreakerConfig"
}

// StageCategory Stage 카테고리
type StageCategory string

const (
	CategoryTransform  StageCategory = "transform"  // 변환 (filter, remap, drop 등)
	CategoryValidation StageCategory = "validation" // 검증 (validate, contract)
	CategoryOutput     StageCategory = "output"     // 출력 (sql, elasticsearch, kafka 등)
	CategoryControl    StageCategory = "control"    // 제어 (throttle, route, sample)
)

// StageFieldSchema 필드 스키마
type StageFieldSchema struct {
	Name        string         `json:"name"`                  // 필드명 (dot notation 지원: "contract.name")
	Type        FieldType      `json:"type"`                  // 필드 타입
	DisplayName string         `json:"display_name"`          // UI 표시명
	Description string         `json:"description,omitempty"` // 설명/도움말
	Required    bool           `json:"required,omitempty"`    // 필수 여부
	Default     any            `json:"default,omitempty"`     // 기본값
	Placeholder string         `json:"placeholder,omitempty"` // 입력 힌트

	// 타입별 옵션
	Options     []FieldOption `json:"options,omitempty"`      // enum 타입용 선택지
	Min         *float64      `json:"min,omitempty"`          // number 최소값
	Max         *float64      `json:"max,omitempty"`          // number 최대값
	MinLength   *int          `json:"min_length,omitempty"`   // string 최소 길이
	MaxLength   *int          `json:"max_length,omitempty"`   // string 최대 길이
	Pattern     string        `json:"pattern,omitempty"`      // 정규식 패턴
	Multiline   bool          `json:"multiline,omitempty"`    // textarea 사용 여부
	Rows        int           `json:"rows,omitempty"`         // textarea 행 수
	MonoSpace   bool          `json:"monospace,omitempty"`    // 고정폭 폰트 사용

	// 조건부 표시
	ShowWhen *FieldCondition `json:"show_when,omitempty"` // 다른 필드 값에 따라 표시

	// 중첩 필드 (object 타입용)
	Fields []StageFieldSchema `json:"fields,omitempty"`

	// 배열 아이템 스키마 (array 타입용)
	ItemSchema *StageFieldSchema `json:"item_schema,omitempty"`

	// 커스텀 에디터 (복잡한 UI 필요 시)
	CustomEditor string `json:"custom_editor,omitempty"`

	// 연결 테스트 버튼 표시
	TestConnection *TestConnectionConfig `json:"test_connection,omitempty"`
}

// FieldType 필드 타입
type FieldType string

const (
	FieldTypeString   FieldType = "string"   // 텍스트 입력
	FieldTypeNumber   FieldType = "number"   // 숫자 입력
	FieldTypeInteger  FieldType = "integer"  // 정수 입력
	FieldTypeBoolean  FieldType = "boolean"  // 체크박스/스위치
	FieldTypeEnum     FieldType = "enum"     // 드롭다운 선택
	FieldTypeArray    FieldType = "array"    // 배열 (태그 입력 등)
	FieldTypeObject   FieldType = "object"   // 중첩 객체
	FieldTypeJSON     FieldType = "json"     // JSON 에디터
	FieldTypeCode     FieldType = "code"     // 코드 에디터 (Monaco)
	FieldTypeKeyValue FieldType = "keyvalue" // 키-값 쌍 에디터
	FieldTypeDuration FieldType = "duration" // 시간 입력 (예: "30s", "5m")
	FieldTypeSecret   FieldType = "secret"   // 비밀번호 입력
)

// FieldOption enum 타입 옵션
type FieldOption struct {
	Value       string `json:"value"`                 // 실제 값
	Label       string `json:"label"`                 // 표시 텍스트
	Description string `json:"description,omitempty"` // 옵션 설명
}

// FieldCondition 조건부 표시 조건
type FieldCondition struct {
	Field    string `json:"field"`    // 조건 필드
	Operator string `json:"operator"` // 연산자 (eq, neq, in, exists)
	Value    any    `json:"value"`    // 비교 값
}

// TestConnectionConfig 연결 테스트 버튼 설정
type TestConnectionConfig struct {
	Endpoint string   `json:"endpoint"`          // 테스트 API 엔드포인트
	Fields   []string `json:"fields"`            // 테스트에 필요한 필드들
	Label    string   `json:"label,omitempty"`   // 버튼 텍스트
	Method   string   `json:"method,omitempty"`  // HTTP 메서드 (기본: POST)
}

// StageSchemaProvider Stage Schema 제공 인터페이스
// Stage 구현체가 이 인터페이스를 구현하면 GUI 자동 생성
type StageSchemaProvider interface {
	// Schema returns the stage schema for GUI generation
	Schema() StageSchema
}
