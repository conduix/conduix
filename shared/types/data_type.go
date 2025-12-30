package types

import "time"

// DeleteMode 삭제 모드
type DeleteMode string

const (
	// DeleteModePhysical 물리 삭제 - 실제로 레코드 삭제
	DeleteModePhysical DeleteMode = "physical"
	// DeleteModeSoft 논리 삭제 - 삭제 플래그 설정
	DeleteModeSoft DeleteMode = "soft"
	// DeleteModeIgnore 삭제 이벤트 무시
	DeleteModeIgnore DeleteMode = "ignore"
)

// SoftDeleteFieldType 논리 삭제 필드 타입
type SoftDeleteFieldType string

const (
	// SoftDeleteFieldTimestamp deleted_at 타임스탬프 필드 (null -> timestamp)
	SoftDeleteFieldTimestamp SoftDeleteFieldType = "timestamp"
	// SoftDeleteFieldBoolean is_deleted 불린 필드 (false -> true)
	SoftDeleteFieldBoolean SoftDeleteFieldType = "boolean"
	// SoftDeleteFieldStatus status 필드 ('A' -> 'D')
	SoftDeleteFieldStatus SoftDeleteFieldType = "status"
	// SoftDeleteFieldCustom 커스텀 필드/값
	SoftDeleteFieldCustom SoftDeleteFieldType = "custom"
)

// DeleteStrategy 삭제 전략 설정
type DeleteStrategy struct {
	Mode DeleteMode `json:"mode" yaml:"mode"`

	// 논리 삭제 설정 (Mode가 soft일 때)
	SoftDelete *SoftDeleteConfig `json:"soft_delete,omitempty" yaml:"soft_delete,omitempty"`

	// 삭제 이벤트 감지 설정
	Detection *DeleteDetectionConfig `json:"detection,omitempty" yaml:"detection,omitempty"`
}

// SoftDeleteConfig 논리 삭제 상세 설정
type SoftDeleteConfig struct {
	// 필드 타입 (timestamp, boolean, status, custom)
	FieldType SoftDeleteFieldType `json:"field_type" yaml:"field_type"`

	// 삭제 플래그 필드명 (예: deleted_at, is_deleted, status)
	FieldName string `json:"field_name" yaml:"field_name"`

	// 삭제 시 설정할 값 (custom 타입일 때 사용)
	// timestamp: 자동 (현재 시간)
	// boolean: 자동 (true)
	// status: 기본값 "D"
	// custom: 이 값 사용
	DeleteValue string `json:"delete_value,omitempty" yaml:"delete_value,omitempty"`

	// 활성 상태 값 (status, custom 타입일 때 사용)
	ActiveValue string `json:"active_value,omitempty" yaml:"active_value,omitempty"`
}

// DeleteDetectionConfig 삭제 이벤트 감지 설정
type DeleteDetectionConfig struct {
	// 감지 방식
	// - "null_body": 본문이 null이면 삭제 (ID만 있는 경우)
	// - "flag_field": 특정 필드 값으로 감지
	// - "event_type": 이벤트 타입 필드로 감지
	Method string `json:"method" yaml:"method"`

	// flag_field 방식: 삭제 여부를 나타내는 필드명
	FlagField string `json:"flag_field,omitempty" yaml:"flag_field,omitempty"`

	// flag_field 방식: 삭제를 나타내는 값 (예: "DELETE", "D", true)
	FlagValue string `json:"flag_value,omitempty" yaml:"flag_value,omitempty"`

	// event_type 방식: 이벤트 타입 필드명 (예: "__op", "event_type")
	EventTypeField string `json:"event_type_field,omitempty" yaml:"event_type_field,omitempty"`

	// event_type 방식: 삭제 이벤트 타입 값 (예: "d", "delete", "DELETE")
	DeleteEventType string `json:"delete_event_type,omitempty" yaml:"delete_event_type,omitempty"`
}

// DataType 데이터 유형 정의
// 같은 유형의 데이터는 동일한 스키마, 삭제 전략, 저장소를 공유
type DataType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`         // 데이터 유형명 (예: user, order, log)
	DisplayName string `json:"display_name"` // 표시명 (예: 사용자 정보, 주문 내역)
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"` // 카테고리 (예: master, transaction, log)

	// 삭제 전략
	DeleteStrategy *DeleteStrategy `json:"delete_strategy,omitempty"`

	// ID 필드 설정 (삭제 시 어떤 필드로 대상 식별)
	IDFields []string `json:"id_fields,omitempty"` // 복합키 지원 (예: ["user_id"], ["order_id", "item_id"])

	// 스키마 정보 (선택적)
	Schema *DataTypeSchema `json:"schema,omitempty"`

	// 저장소 설정
	Storage *DataTypeStorage `json:"storage,omitempty"`

	// 사전작업 목록
	Preworks []DataTypePrework `json:"preworks,omitempty"`

	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DataTypeSchema 데이터 유형 스키마
type DataTypeSchema struct {
	// 스키마 정의 방식
	// - "json_schema": JSON Schema
	// - "avro": Avro Schema
	// - "infer": 자동 추론
	Type string `json:"type" yaml:"type"`

	// 스키마 정의 (JSON 문자열)
	Definition string `json:"definition,omitempty" yaml:"definition,omitempty"`

	// 필드 목록 (간단한 정의용)
	Fields []DataTypeField `json:"fields,omitempty" yaml:"fields,omitempty"`
}

// DataTypeField 데이터 유형 필드
type DataTypeField struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"` // string, int, float, bool, datetime, json
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// DataTypeStorage 데이터 유형 저장소 설정
type DataTypeStorage struct {
	// 대상 저장소 타입 (elasticsearch, postgresql, s3, etc.)
	Type string `json:"type" yaml:"type"`

	// 저장소별 설정
	// Elasticsearch: index, mapping
	// PostgreSQL: table, schema
	// S3: bucket, prefix
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// PreworkType 사전작업 타입
type PreworkType string

const (
	PreworkTypeSQL           PreworkType = "sql"           // SQL 실행
	PreworkTypeHTTP          PreworkType = "http"          // HTTP 요청
	PreworkTypeElasticsearch PreworkType = "elasticsearch" // ES 인덱스/매핑 생성
	PreworkTypeS3            PreworkType = "s3"            // S3 버킷/경로 생성
	PreworkTypeScript        PreworkType = "script"        // 스크립트 실행
)

// PreworkExecutionPhase 사전작업 실행 시점
type PreworkExecutionPhase string

const (
	// PreworkPhaseDataType 데이터 유형 등록 시 실행
	PreworkPhaseDataType PreworkExecutionPhase = "data_type"
	// PreworkPhasePipeline 파이프라인 등록 시 실행
	PreworkPhasePipeline PreworkExecutionPhase = "pipeline"
	// PreworkPhaseManual 수동 실행만
	PreworkPhaseManual PreworkExecutionPhase = "manual"
)

// DataTypePrework 데이터 유형별 사전작업
type DataTypePrework struct {
	ID          string                `json:"id"`
	DataTypeID  string                `json:"data_type_id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Type        PreworkType           `json:"type"`
	Phase       PreworkExecutionPhase `json:"phase"`
	Order       int                   `json:"order"` // 실행 순서

	// 타입별 설정
	Config map[string]any `json:"config"`

	// 실행 상태
	Status     string     `json:"status,omitempty"` // pending, running, completed, failed
	ExecutedAt *time.Time `json:"executed_at,omitempty"`
	ExecutedBy string     `json:"executed_by,omitempty"`
	ErrorMsg   string     `json:"error_msg,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PreworkSQLConfig SQL 사전작업 설정
type PreworkSQLConfig struct {
	// 데이터소스 ID (connections 테이블 참조)
	ConnectionID string `json:"connection_id" yaml:"connection_id"`

	// 실행할 SQL 문 (여러 문장 가능)
	Statements []string `json:"statements" yaml:"statements"`

	// 트랜잭션 사용 여부
	UseTransaction bool `json:"use_transaction,omitempty" yaml:"use_transaction,omitempty"`

	// 실패 시 롤백 여부
	RollbackOnError bool `json:"rollback_on_error,omitempty" yaml:"rollback_on_error,omitempty"`
}

// PreworkElasticsearchConfig Elasticsearch 사전작업 설정
type PreworkElasticsearchConfig struct {
	// 대상 ES 클러스터 ID
	ClusterID string `json:"cluster_id" yaml:"cluster_id"`

	// 작업 종류: create_index, put_mapping, create_template
	Action string `json:"action" yaml:"action"`

	// 인덱스명 (템플릿 변수 지원: {{data_type}}, {{date}})
	IndexName string `json:"index_name,omitempty" yaml:"index_name,omitempty"`

	// 매핑/설정 JSON
	Body string `json:"body,omitempty" yaml:"body,omitempty"`
}

// PreworkHTTPConfig HTTP 사전작업 설정
type PreworkHTTPConfig struct {
	Method  string            `json:"method" yaml:"method"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"`

	// 성공 조건
	ExpectedStatus []int `json:"expected_status,omitempty" yaml:"expected_status,omitempty"`
}

// DeleteStrategyPreset 삭제 전략 프리셋
type DeleteStrategyPreset struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Strategy    *DeleteStrategy `json:"strategy"`
	IsDefault   bool            `json:"is_default,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// 기본 삭제 전략 프리셋들
var DefaultDeleteStrategyPresets = []DeleteStrategyPreset{
	{
		ID:          "physical",
		Name:        "물리 삭제",
		Description: "레코드를 실제로 삭제합니다",
		Strategy: &DeleteStrategy{
			Mode: DeleteModePhysical,
			Detection: &DeleteDetectionConfig{
				Method: "null_body",
			},
		},
		IsDefault: true,
	},
	{
		ID:          "soft_timestamp",
		Name:        "논리 삭제 (타임스탬프)",
		Description: "deleted_at 필드에 삭제 시간을 기록합니다",
		Strategy: &DeleteStrategy{
			Mode: DeleteModeSoft,
			SoftDelete: &SoftDeleteConfig{
				FieldType: SoftDeleteFieldTimestamp,
				FieldName: "deleted_at",
			},
			Detection: &DeleteDetectionConfig{
				Method: "null_body",
			},
		},
	},
	{
		ID:          "soft_boolean",
		Name:        "논리 삭제 (불린)",
		Description: "is_deleted 필드를 true로 설정합니다",
		Strategy: &DeleteStrategy{
			Mode: DeleteModeSoft,
			SoftDelete: &SoftDeleteConfig{
				FieldType:   SoftDeleteFieldBoolean,
				FieldName:   "is_deleted",
				DeleteValue: "true",
				ActiveValue: "false",
			},
			Detection: &DeleteDetectionConfig{
				Method: "null_body",
			},
		},
	},
	{
		ID:          "soft_status",
		Name:        "논리 삭제 (상태값)",
		Description: "status 필드를 'D'로 설정합니다",
		Strategy: &DeleteStrategy{
			Mode: DeleteModeSoft,
			SoftDelete: &SoftDeleteConfig{
				FieldType:   SoftDeleteFieldStatus,
				FieldName:   "status",
				DeleteValue: "D",
				ActiveValue: "A",
			},
			Detection: &DeleteDetectionConfig{
				Method: "null_body",
			},
		},
	},
	{
		ID:          "cdc_soft_timestamp",
		Name:        "CDC 논리 삭제 (타임스탬프)",
		Description: "CDC __op 필드로 삭제 감지, deleted_at 기록",
		Strategy: &DeleteStrategy{
			Mode: DeleteModeSoft,
			SoftDelete: &SoftDeleteConfig{
				FieldType: SoftDeleteFieldTimestamp,
				FieldName: "deleted_at",
			},
			Detection: &DeleteDetectionConfig{
				Method:          "event_type",
				EventTypeField:  "__op",
				DeleteEventType: "d",
			},
		},
	},
	{
		ID:          "ignore",
		Name:        "삭제 무시",
		Description: "삭제 이벤트를 무시합니다 (로그성 데이터용)",
		Strategy: &DeleteStrategy{
			Mode: DeleteModeIgnore,
		},
	},
}
