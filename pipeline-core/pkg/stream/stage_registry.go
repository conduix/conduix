// Package stream Stage Registry
// 모든 Stage 타입의 Schema를 등록하고 관리
package stream

import (
	"github.com/conduix/conduix/shared/types"
)

// StageRegistry 전역 Stage 레지스트리
var StageRegistry = &stageRegistry{
	schemas: make(map[string]types.StageSchema),
}

type stageRegistry struct {
	schemas map[string]types.StageSchema
}

// Register Stage Schema 등록
func (r *stageRegistry) Register(schema types.StageSchema) {
	r.schemas[schema.Type] = schema
}

// Get Stage Schema 조회
func (r *stageRegistry) Get(stageType string) (types.StageSchema, bool) {
	schema, ok := r.schemas[stageType]
	return schema, ok
}

// All 모든 Stage Schema 조회
func (r *stageRegistry) All() []types.StageSchema {
	schemas := make([]types.StageSchema, 0, len(r.schemas))
	for _, schema := range r.schemas {
		schemas = append(schemas, schema)
	}
	return schemas
}

// AllByCategory 카테고리별 Stage Schema 조회
func (r *stageRegistry) AllByCategory(category types.StageCategory) []types.StageSchema {
	var schemas []types.StageSchema
	for _, schema := range r.schemas {
		if schema.Category == category {
			schemas = append(schemas, schema)
		}
	}
	return schemas
}

// Types 등록된 모든 Stage 타입 목록
func (r *stageRegistry) Types() []string {
	types := make([]string, 0, len(r.schemas))
	for t := range r.schemas {
		types = append(types, t)
	}
	return types
}

// init 모든 기본 Stage Schema 등록
func init() {
	// Transform Stages
	StageRegistry.Register(FilterStageSchema())
	StageRegistry.Register(RemapStageSchema())
	StageRegistry.Register(DropStageSchema())
	StageRegistry.Register(MergeStageSchema())
	StageRegistry.Register(SplitStageSchema())
	StageRegistry.Register(EncryptStageSchema())
	StageRegistry.Register(DedupeStageSchema())
	StageRegistry.Register(DefaultStageSchema())
	StageRegistry.Register(CastStageSchema())
	StageRegistry.Register(TimestampStageSchema())

	// Control Stages
	StageRegistry.Register(ThrottleStageSchema())
	StageRegistry.Register(SampleStageSchema())
	StageRegistry.Register(RouteStageSchema())

	// Validation Stages
	StageRegistry.Register(ValidateStageSchema())
	StageRegistry.Register(ContractStageSchema())

	// Encoding Stages
	StageRegistry.Register(Base64StageSchema())

	// Script Stages
	StageRegistry.Register(JSScriptStageSchema())

	// Output Stages
	StageRegistry.Register(SQLOutputStageSchema())
	StageRegistry.Register(ElasticsearchOutputStageSchema())
	StageRegistry.Register(KafkaOutputStageSchema())
	StageRegistry.Register(MongoDBOutputStageSchema())
	StageRegistry.Register(S3OutputStageSchema())
	StageRegistry.Register(RESTAPIOutputStageSchema())
	StageRegistry.Register(FileOutputStageSchema())
}

// =============================================================================
// Transform Stage Schemas
// =============================================================================

// FilterStageSchema Filter Stage 스키마
func FilterStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "filter",
		DisplayName: "Filter",
		Description: "조건에 맞는 레코드만 통과",
		Category:    types.CategoryTransform,
		Icon:        "FilterAlt",
		Color:       "#1976d2",
		Fields: []types.StageFieldSchema{
			{
				Name:        "condition",
				Type:        types.FieldTypeCode,
				DisplayName: "조건식",
				Description: "VRL 조건식 (예: .status == \"active\" && .age >= 18)",
				Required:    true,
				Placeholder: ".status == \"active\"",
				Multiline:   true,
				Rows:        3,
				MonoSpace:   true,
			},
		},
	}
}

// RemapStageSchema Remap Stage 스키마
func RemapStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "remap",
		DisplayName: "Remap",
		Description: "필드 이름 변경/매핑",
		Category:    types.CategoryTransform,
		Icon:        "SwapHoriz",
		Color:       "#4caf50",
		Fields: []types.StageFieldSchema{
			{
				Name:        "mappings",
				Type:        types.FieldTypeJSON,
				DisplayName: "매핑",
				Description: "필드 매핑 (예: {\"old_field\": \"new_field\"})",
				Required:    true,
				Placeholder: "{\"id\": \"record_id\", \"name\": \"title\"}",
				Multiline:   true,
				Rows:        5,
				MonoSpace:   true,
			},
		},
	}
}

// DropStageSchema Drop Stage 스키마
func DropStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "drop",
		DisplayName: "Drop",
		Description: "지정한 필드 삭제",
		Category:    types.CategoryTransform,
		Icon:        "RemoveCircle",
		Color:       "#f44336",
		Fields: []types.StageFieldSchema{
			{
				Name:        "fields",
				Type:        types.FieldTypeArray,
				DisplayName: "삭제할 필드",
				Description: "삭제할 필드명 (쉼표로 구분)",
				Required:    true,
				Placeholder: "password, secret_key, internal_id",
			},
		},
	}
}

// MergeStageSchema Merge Stage 스키마
func MergeStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "merge",
		DisplayName: "Merge",
		Description: "여러 필드를 하나로 합치기",
		Category:    types.CategoryTransform,
		Icon:        "Merge",
		Color:       "#00bcd4",
		Fields: []types.StageFieldSchema{
			{
				Name:        "source_fields",
				Type:        types.FieldTypeArray,
				DisplayName: "소스 필드",
				Description: "합칠 필드들 (쉼표로 구분)",
				Required:    true,
				Placeholder: "first_name, last_name",
			},
			{
				Name:        "target_field",
				Type:        types.FieldTypeString,
				DisplayName: "대상 필드",
				Description: "결과 필드 이름",
				Required:    true,
				Placeholder: "full_name",
			},
			{
				Name:        "delimiter",
				Type:        types.FieldTypeString,
				DisplayName: "구분자",
				Description: "필드 사이 구분자",
				Default:     " ",
				Placeholder: " ",
			},
			{
				Name:        "template",
				Type:        types.FieldTypeString,
				DisplayName: "템플릿",
				Description: "템플릿 사용 시 (예: {{first_name}} {{last_name}})",
				Placeholder: "{{first_name}} {{last_name}}",
			},
		},
	}
}

// SplitStageSchema Split Stage 스키마
func SplitStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "split",
		DisplayName: "Split",
		Description: "정규식으로 필드 분리",
		Category:    types.CategoryTransform,
		Icon:        "CallSplit",
		Color:       "#e91e63",
		Fields: []types.StageFieldSchema{
			{
				Name:        "source_field",
				Type:        types.FieldTypeString,
				DisplayName: "소스 필드",
				Description: "분리할 필드",
				Required:    true,
				Placeholder: "full_name",
			},
			{
				Name:        "pattern",
				Type:        types.FieldTypeString,
				DisplayName: "정규식 패턴",
				Description: "분리 패턴 (그룹 사용)",
				Required:    true,
				Placeholder: "^(\\w+)\\s+(\\w+)$",
				MonoSpace:   true,
			},
			{
				Name:        "target_fields",
				Type:        types.FieldTypeArray,
				DisplayName: "대상 필드",
				Description: "결과 필드명 (그룹 순서대로)",
				Required:    true,
				Placeholder: "first_name, last_name",
			},
			{
				Name:        "keep_original",
				Type:        types.FieldTypeBoolean,
				DisplayName: "원본 유지",
				Description: "원본 필드를 유지할지 여부",
				Default:     false,
			},
		},
	}
}

// EncryptStageSchema Encrypt Stage 스키마
func EncryptStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "encrypt",
		DisplayName: "Encrypt",
		Description: "필드 암호화/마스킹",
		Category:    types.CategoryTransform,
		Icon:        "Lock",
		Color:       "#ffc107",
		Fields: []types.StageFieldSchema{
			{
				Name:        "fields",
				Type:        types.FieldTypeArray,
				DisplayName: "암호화할 필드",
				Description: "암호화할 필드들 (쉼표로 구분)",
				Required:    true,
				Placeholder: "password, ssn, credit_card",
			},
			{
				Name:        "method",
				Type:        types.FieldTypeEnum,
				DisplayName: "암호화 방식",
				Required:    true,
				Default:     "sha256",
				Options: []types.FieldOption{
					{Value: "aes256", Label: "AES-256", Description: "대칭 암호화 (복호화 가능)"},
					{Value: "sha256", Label: "SHA-256", Description: "해시 (복호화 불가)"},
					{Value: "sha512", Label: "SHA-512", Description: "해시 (복호화 불가)"},
					{Value: "bcrypt", Label: "BCrypt", Description: "비밀번호 해싱"},
					{Value: "mask", Label: "마스킹", Description: "문자 마스킹 (예: ****1234)"},
				},
			},
			{
				Name:        "key_env",
				Type:        types.FieldTypeString,
				DisplayName: "키 환경변수",
				Description: "암호화 키가 저장된 환경변수명 (AES용)",
				Placeholder: "ENCRYPTION_KEY",
				ShowWhen:    &types.FieldCondition{Field: "method", Operator: "eq", Value: "aes256"},
			},
			{
				Name:        "mask_char",
				Type:        types.FieldTypeString,
				DisplayName: "마스킹 문자",
				Default:     "*",
				ShowWhen:    &types.FieldCondition{Field: "method", Operator: "eq", Value: "mask"},
			},
			{
				Name:        "mask_keep_first",
				Type:        types.FieldTypeInteger,
				DisplayName: "앞자리 유지",
				Description: "앞에서 유지할 문자 수",
				Default:     0,
				ShowWhen:    &types.FieldCondition{Field: "method", Operator: "eq", Value: "mask"},
			},
			{
				Name:        "mask_keep_last",
				Type:        types.FieldTypeInteger,
				DisplayName: "뒷자리 유지",
				Description: "뒤에서 유지할 문자 수",
				Default:     4,
				ShowWhen:    &types.FieldCondition{Field: "method", Operator: "eq", Value: "mask"},
			},
		},
	}
}

// DedupeStageSchema Dedupe Stage 스키마
func DedupeStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "dedupe",
		DisplayName: "Dedupe",
		Description: "중복 레코드 제거",
		Category:    types.CategoryTransform,
		Icon:        "ContentCopy",
		Color:       "#ff5722",
		Fields: []types.StageFieldSchema{
			{
				Name:        "key_fields",
				Type:        types.FieldTypeArray,
				DisplayName: "키 필드",
				Description: "중복 판단 기준 필드들",
				Required:    true,
				Placeholder: "id, timestamp",
			},
			{
				Name:        "strategy",
				Type:        types.FieldTypeEnum,
				DisplayName: "전략",
				Default:     "keep_first",
				Options: []types.FieldOption{
					{Value: "keep_first", Label: "첫 번째 유지", Description: "처음 들어온 레코드 유지"},
					{Value: "keep_last", Label: "마지막 유지", Description: "마지막 레코드 유지"},
					{Value: "keep_latest", Label: "최신 유지", Description: "타임스탬프 기준 최신 유지"},
				},
			},
			{
				Name:        "window",
				Type:        types.FieldTypeDuration,
				DisplayName: "시간 윈도우",
				Description: "중복 검사 시간 범위 (예: 1h, 30m)",
				Placeholder: "1h",
			},
			{
				Name:        "timestamp_field",
				Type:        types.FieldTypeString,
				DisplayName: "타임스탬프 필드",
				Description: "keep_latest 전략용",
				Placeholder: "created_at",
				ShowWhen:    &types.FieldCondition{Field: "strategy", Operator: "eq", Value: "keep_latest"},
			},
		},
	}
}

// DefaultStageSchema Default Stage 스키마
func DefaultStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "default",
		DisplayName: "Default",
		Description: "기본값 설정",
		Category:    types.CategoryTransform,
		Icon:        "TextFields",
		Color:       "#3f51b5",
		Fields: []types.StageFieldSchema{
			{
				Name:        "defaults",
				Type:        types.FieldTypeJSON,
				DisplayName: "기본값",
				Description: "필드별 기본값 (예: {\"status\": \"pending\"})",
				Required:    true,
				Placeholder: "{\"status\": \"pending\", \"priority\": 0}",
				Multiline:   true,
				Rows:        5,
				MonoSpace:   true,
			},
			{
				Name:        "only_null",
				Type:        types.FieldTypeBoolean,
				DisplayName: "null만 대체",
				Description: "true: null만, false: null과 빈 문자열 모두",
				Default:     true,
			},
		},
	}
}

// CastStageSchema Cast Stage 스키마
func CastStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "cast",
		DisplayName: "Cast",
		Description: "타입 변환",
		Category:    types.CategoryTransform,
		Icon:        "Numbers",
		Color:       "#cddc39",
		Fields: []types.StageFieldSchema{
			{
				Name:        "casts",
				Type:        types.FieldTypeJSON,
				DisplayName: "타입 변환",
				Description: "필드별 타입 (예: {\"age\": \"int\", \"price\": \"float\"})",
				Required:    true,
				Placeholder: "{\"age\": \"int\", \"active\": \"bool\"}",
				Multiline:   true,
				Rows:        5,
				MonoSpace:   true,
			},
			{
				Name:        "date_format",
				Type:        types.FieldTypeString,
				DisplayName: "날짜 포맷",
				Description: "날짜 파싱 포맷",
				Placeholder: "2006-01-02T15:04:05Z07:00",
				MonoSpace:   true,
			},
			{
				Name:        "error_action",
				Type:        types.FieldTypeEnum,
				DisplayName: "에러 처리",
				Default:     "null",
				Options: []types.FieldOption{
					{Value: "null", Label: "null로 설정", Description: "변환 실패 시 null"},
					{Value: "drop", Label: "레코드 삭제", Description: "변환 실패 시 레코드 드롭"},
					{Value: "keep", Label: "원본 유지", Description: "변환 실패 시 원본 값 유지"},
				},
			},
		},
	}
}

// TimestampStageSchema Timestamp Stage 스키마
func TimestampStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "timestamp",
		DisplayName: "Timestamp",
		Description: "타임스탬프 처리",
		Category:    types.CategoryTransform,
		Icon:        "AccessTime",
		Color:       "#e91e63",
		Fields: []types.StageFieldSchema{
			{
				Name:        "action",
				Type:        types.FieldTypeEnum,
				DisplayName: "동작",
				Required:    true,
				Default:     "add",
				Options: []types.FieldOption{
					{Value: "add", Label: "현재 시간 추가", Description: "현재 타임스탬프 추가"},
					{Value: "convert", Label: "포맷 변환", Description: "타임스탬프 포맷 변환"},
					{Value: "format", Label: "문자열 변환", Description: "특정 포맷으로 출력"},
				},
			},
			{
				Name:        "target_field",
				Type:        types.FieldTypeString,
				DisplayName: "대상 필드",
				Required:    true,
				Placeholder: "processed_at",
			},
			{
				Name:        "source_field",
				Type:        types.FieldTypeString,
				DisplayName: "소스 필드",
				Placeholder: "created_at",
				ShowWhen:    &types.FieldCondition{Field: "action", Operator: "in", Value: []string{"convert", "format"}},
			},
			{
				Name:        "timezone",
				Type:        types.FieldTypeString,
				DisplayName: "타임존",
				Default:     "UTC",
				Placeholder: "Asia/Seoul",
			},
			{
				Name:        "input_format",
				Type:        types.FieldTypeString,
				DisplayName: "입력 포맷",
				Placeholder: "2006-01-02T15:04:05Z07:00",
				MonoSpace:   true,
				ShowWhen:    &types.FieldCondition{Field: "action", Operator: "eq", Value: "convert"},
			},
			{
				Name:        "output_format",
				Type:        types.FieldTypeString,
				DisplayName: "출력 포맷",
				Placeholder: "2006-01-02 15:04:05",
				MonoSpace:   true,
				ShowWhen:    &types.FieldCondition{Field: "action", Operator: "eq", Value: "format"},
			},
		},
	}
}

// =============================================================================
// Control Stage Schemas
// =============================================================================

// ThrottleStageSchema Throttle Stage 스키마
func ThrottleStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "throttle",
		DisplayName: "Throttle",
		Description: "처리량 제한",
		Category:    types.CategoryControl,
		Icon:        "Speed",
		Color:       "#ff9800",
		Fields: []types.StageFieldSchema{
			{
				Name:        "rate",
				Type:        types.FieldTypeInteger,
				DisplayName: "처리량",
				Description: "단위 시간당 처리량",
				Required:    true,
				Default:     100,
				Min:         pointerFloat64(1),
			},
			{
				Name:        "interval",
				Type:        types.FieldTypeEnum,
				DisplayName: "단위",
				Default:     "second",
				Options: []types.FieldOption{
					{Value: "second", Label: "초당"},
					{Value: "minute", Label: "분당"},
					{Value: "hour", Label: "시간당"},
				},
			},
			{
				Name:        "burst",
				Type:        types.FieldTypeInteger,
				DisplayName: "버스트 허용",
				Description: "일시적으로 허용할 초과량",
			},
			{
				Name:        "strategy",
				Type:        types.FieldTypeEnum,
				DisplayName: "알고리즘",
				Default:     "token_bucket",
				Options: []types.FieldOption{
					{Value: "token_bucket", Label: "Token Bucket", Description: "버스트 허용, 점진적 복구"},
					{Value: "sliding_window", Label: "Sliding Window", Description: "정확한 시간 기반"},
					{Value: "fixed_window", Label: "Fixed Window", Description: "고정 시간 창"},
				},
			},
			{
				Name:        "drop_on_limit",
				Type:        types.FieldTypeBoolean,
				DisplayName: "초과 시 삭제",
				Description: "true: 초과 레코드 삭제, false: 대기",
				Default:     false,
			},
		},
	}
}

// SampleStageSchema Sample Stage 스키마
func SampleStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "sample",
		DisplayName: "Sample",
		Description: "레코드 샘플링",
		Category:    types.CategoryControl,
		Icon:        "Science",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "rate",
				Type:        types.FieldTypeNumber,
				DisplayName: "샘플링 비율",
				Description: "0.0 ~ 1.0 (예: 0.1 = 10%)",
				Required:    true,
				Default:     0.1,
				Min:         pointerFloat64(0),
				Max:         pointerFloat64(1),
			},
		},
	}
}

// RouteStageSchema Route Stage 스키마
func RouteStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "route",
		DisplayName: "Route",
		Description: "이벤트 타입별 라우팅 (CDC용)",
		Category:    types.CategoryControl,
		Icon:        "ForkRight",
		Color:       "#00bcd4",
		Fields: []types.StageFieldSchema{
			{
				Name:        "field",
				Type:        types.FieldTypeString,
				DisplayName: "라우팅 필드",
				Description: "라우팅 기준 필드 (예: _op)",
				Required:    true,
				Default:     "_op",
			},
			{
				Name:         "routes",
				Type:         types.FieldTypeArray,
				DisplayName:  "라우팅 규칙",
				CustomEditor: "RouteRuleEditor",
			},
		},
	}
}

// =============================================================================
// Validation Stage Schemas
// =============================================================================

// ValidateStageSchema Validate Stage 스키마
func ValidateStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "validate",
		DisplayName: "Validate",
		Description: "JSON Schema 검증",
		Category:    types.CategoryValidation,
		Icon:        "CheckCircle",
		Color:       "#9e9e9e",
		Fields: []types.StageFieldSchema{
			{
				Name:        "schema",
				Type:        types.FieldTypeJSON,
				DisplayName: "스키마",
				Description: "JSON Schema",
				Required:    true,
				Placeholder: "{\"type\": \"object\", \"required\": [\"id\"]}",
				Multiline:   true,
				Rows:        10,
				MonoSpace:   true,
			},
			{
				Name:        "drop_on_fail",
				Type:        types.FieldTypeBoolean,
				DisplayName: "실패 시 삭제",
				Description: "검증 실패 레코드 삭제 여부",
				Default:     false,
			},
		},
	}
}

// ContractStageSchema Contract Stage 스키마
func ContractStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "contract",
		DisplayName: "Contract",
		Description: "Data Contract 검증 (비즈니스 규칙 + 스키마)",
		Category:    types.CategoryValidation,
		Icon:        "Gavel",
		Color:       "#673ab7",
		// Contract는 복잡한 UI가 필요하므로 CustomEditor 사용
		CustomEditor: "ContractStageEditor",
		Fields:       []types.StageFieldSchema{}, // CustomEditor가 전체 폼 담당
	}
}

// =============================================================================
// Output Stage Schemas
// =============================================================================

// SQLOutputStageSchema SQL Output Stage 스키마
func SQLOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "sql",
		DisplayName: "SQL Output",
		Description: "SQL 데이터베이스 출력",
		Category:    types.CategoryOutput,
		Icon:        "Storage",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "connection_string",
				Type:        types.FieldTypeSecret,
				DisplayName: "연결 문자열",
				Required:    true,
				Placeholder: "postgres://user:password@localhost:5432/dbname",
				MonoSpace:   true,
				TestConnection: &types.TestConnectionConfig{
					Endpoint: "/utils/test-db-connection",
					Fields:   []string{"connection_string"},
					Label:    "연결 테스트",
				},
			},
			{
				Name:        "table",
				Type:        types.FieldTypeString,
				DisplayName: "테이블",
				Required:    true,
				Placeholder: "my_table",
			},
			{
				Name:        "batch_size",
				Type:        types.FieldTypeInteger,
				DisplayName: "배치 크기",
				Default:     100,
				Min:         pointerFloat64(1),
				Max:         pointerFloat64(10000),
			},
			{
				Name:        "upsert",
				Type:        types.FieldTypeBoolean,
				DisplayName: "Upsert 사용",
				Description: "중복 키 시 업데이트",
				Default:     true,
			},
			{
				Name:        "conflict_columns",
				Type:        types.FieldTypeArray,
				DisplayName: "충돌 컬럼",
				Description: "upsert 시 충돌 판단 컬럼",
				Placeholder: "id",
				ShowWhen:    &types.FieldCondition{Field: "upsert", Operator: "eq", Value: true},
			},
			{
				Name:        "create_table",
				Type:        types.FieldTypeCode,
				DisplayName: "테이블 생성 SQL",
				Description: "테이블이 없을 때 실행할 CREATE TABLE",
				Multiline:   true,
				Rows:        5,
				MonoSpace:   true,
			},
		},
	}
}

// ElasticsearchOutputStageSchema Elasticsearch Output Stage 스키마
func ElasticsearchOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "elasticsearch",
		DisplayName: "Elasticsearch Output",
		Description: "Elasticsearch 출력",
		Category:    types.CategoryOutput,
		Icon:        "Storage",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "addresses",
				Type:        types.FieldTypeArray,
				DisplayName: "주소",
				Required:    true,
				Placeholder: "http://localhost:9200",
				TestConnection: &types.TestConnectionConfig{
					Endpoint: "/utils/test-elasticsearch",
					Fields:   []string{"addresses", "username", "password"},
					Label:    "연결 테스트",
				},
			},
			{
				Name:        "index",
				Type:        types.FieldTypeString,
				DisplayName: "인덱스",
				Required:    true,
				Placeholder: "my-index",
			},
			{
				Name:        "batch_size",
				Type:        types.FieldTypeInteger,
				DisplayName: "배치 크기",
				Default:     100,
			},
			{
				Name:        "username",
				Type:        types.FieldTypeString,
				DisplayName: "사용자명",
			},
			{
				Name:        "password",
				Type:        types.FieldTypeSecret,
				DisplayName: "비밀번호",
			},
		},
	}
}

// KafkaOutputStageSchema Kafka Output Stage 스키마
func KafkaOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "kafka",
		DisplayName: "Kafka Output",
		Description: "Kafka 출력",
		Category:    types.CategoryOutput,
		Icon:        "Storage",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "brokers",
				Type:        types.FieldTypeArray,
				DisplayName: "Brokers",
				Required:    true,
				Placeholder: "localhost:9092",
				TestConnection: &types.TestConnectionConfig{
					Endpoint: "/utils/test-kafka",
					Fields:   []string{"brokers"},
					Label:    "연결 테스트",
				},
			},
			{
				Name:        "topic",
				Type:        types.FieldTypeString,
				DisplayName: "Topic",
				Required:    true,
				Placeholder: "my-topic",
			},
		},
	}
}

// MongoDBOutputStageSchema MongoDB Output Stage 스키마
func MongoDBOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "mongodb",
		DisplayName: "MongoDB Output",
		Description: "MongoDB 출력",
		Category:    types.CategoryOutput,
		Icon:        "Storage",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "uri",
				Type:        types.FieldTypeSecret,
				DisplayName: "URI",
				Required:    true,
				Placeholder: "mongodb://localhost:27017",
				MonoSpace:   true,
				TestConnection: &types.TestConnectionConfig{
					Endpoint: "/utils/test-mongodb",
					Fields:   []string{"uri", "database"},
					Label:    "연결 테스트",
				},
			},
			{
				Name:        "database",
				Type:        types.FieldTypeString,
				DisplayName: "데이터베이스",
				Required:    true,
			},
			{
				Name:        "collection",
				Type:        types.FieldTypeString,
				DisplayName: "컬렉션",
				Required:    true,
			},
		},
	}
}

// S3OutputStageSchema S3 Output Stage 스키마
func S3OutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "s3",
		DisplayName: "S3 Output",
		Description: "S3 출력",
		Category:    types.CategoryOutput,
		Icon:        "CloudUpload",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "bucket",
				Type:        types.FieldTypeString,
				DisplayName: "버킷",
				Required:    true,
			},
			{
				Name:        "region",
				Type:        types.FieldTypeString,
				DisplayName: "리전",
				Required:    true,
				Placeholder: "ap-northeast-2",
			},
			{
				Name:        "prefix",
				Type:        types.FieldTypeString,
				DisplayName: "경로 Prefix",
				Placeholder: "data/",
			},
			{
				Name:        "endpoint",
				Type:        types.FieldTypeString,
				DisplayName: "커스텀 Endpoint",
				Description: "MinIO 등 S3 호환 서비스용",
				Placeholder: "http://localhost:9000",
			},
		},
	}
}

// RESTAPIOutputStageSchema REST API Output Stage 스키마
func RESTAPIOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "rest_api",
		DisplayName: "REST API Output",
		Description: "REST API 출력",
		Category:    types.CategoryOutput,
		Icon:        "Api",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "url",
				Type:        types.FieldTypeString,
				DisplayName: "URL",
				Required:    true,
				Placeholder: "https://api.example.com/data",
				TestConnection: &types.TestConnectionConfig{
					Endpoint: "/utils/test-rest-api",
					Fields:   []string{"url", "headers"},
					Label:    "연결 테스트",
				},
			},
			{
				Name:        "method",
				Type:        types.FieldTypeEnum,
				DisplayName: "HTTP Method",
				Default:     "POST",
				Options: []types.FieldOption{
					{Value: "POST", Label: "POST"},
					{Value: "PUT", Label: "PUT"},
					{Value: "PATCH", Label: "PATCH"},
				},
			},
			{
				Name:        "headers",
				Type:        types.FieldTypeJSON,
				DisplayName: "Headers",
				Placeholder: "{\"Authorization\": \"Bearer xxx\"}",
				MonoSpace:   true,
			},
		},
	}
}

// FileOutputStageSchema File Output Stage 스키마
func FileOutputStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "file",
		DisplayName: "File Output",
		Description: "파일 출력",
		Category:    types.CategoryOutput,
		Icon:        "InsertDriveFile",
		Color:       "#9c27b0",
		Fields: []types.StageFieldSchema{
			{
				Name:        "path",
				Type:        types.FieldTypeString,
				DisplayName: "경로",
				Required:    true,
				Placeholder: "/var/log/output.json",
				MonoSpace:   true,
			},
			{
				Name:        "format",
				Type:        types.FieldTypeEnum,
				DisplayName: "포맷",
				Default:     "json",
				Options: []types.FieldOption{
					{Value: "json", Label: "JSON"},
					{Value: "jsonl", Label: "JSON Lines"},
					{Value: "csv", Label: "CSV"},
				},
			},
		},
	}
}

// =============================================================================
// Script Stage Schemas
// =============================================================================

// =============================================================================
// Encoding Stage Schemas
// =============================================================================

// Base64StageSchema Base64 Stage 스키마
func Base64StageSchema() types.StageSchema {
	return types.StageSchema{
		Type:        "base64",
		DisplayName: "Base64",
		Description: "Base64 인코딩/디코딩",
		Category:    types.CategoryTransform,
		Icon:        "Code",
		Color:       "#795548",
		Fields: []types.StageFieldSchema{
			{
				Name:        "fields",
				Type:        types.FieldTypeArray,
				DisplayName: "대상 필드",
				Description: "인코딩/디코딩할 필드들 (쉼표로 구분)",
				Required:    true,
				Placeholder: "data, payload",
			},
			{
				Name:        "action",
				Type:        types.FieldTypeEnum,
				DisplayName: "동작",
				Default:     "encode",
				Options: []types.FieldOption{
					{Value: "encode", Label: "인코딩", Description: "문자열 → Base64"},
					{Value: "decode", Label: "디코딩", Description: "Base64 → 문자열"},
				},
			},
		},
	}
}

// JSScriptStageSchema JavaScript Stage 스키마 (goja ES5.1+ES6)
func JSScriptStageSchema() types.StageSchema {
	return types.StageSchema{
		Type:         "js_script",
		DisplayName:  "JavaScript",
		Description:  "JavaScript로 레코드 변환 (ES6, JSON/RegExp/Date/Math 내장)",
		Category:     types.CategoryTransform,
		Icon:         "Javascript",
		Color:        "#f7df1e",
		CustomEditor: "JSScriptStageEditor",
		Fields:       []types.StageFieldSchema{}, // CustomEditor가 전체 폼 담당
	}
}

// Helper function
func pointerFloat64(v float64) *float64 {
	return &v
}
