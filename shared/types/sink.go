package types

import "time"

// SinkType 저장소 타입
type SinkType string

const (
	// 로컬/기본 저장소
	SinkTypeStub   SinkType = "stub"   // 로깅/디버깅용
	SinkTypeFile   SinkType = "file"   // 로컬 파일
	SinkTypeStdout SinkType = "stdout" // 표준 출력

	// 메시지 큐
	SinkTypeKafka  SinkType = "kafka"
	SinkTypeRedis  SinkType = "redis"
	SinkTypeRabbit SinkType = "rabbitmq"

	// 분산 파일 시스템
	SinkTypeHDFS SinkType = "hdfs"
	SinkTypeS3   SinkType = "s3"
	SinkTypeGCS  SinkType = "gcs" // Google Cloud Storage

	// 데이터베이스
	SinkTypeSQL     SinkType = "sql" // MySQL, PostgreSQL 등
	SinkTypeMongoDB SinkType = "mongodb"
	SinkTypeHBase   SinkType = "hbase"
	SinkTypeElastic SinkType = "elasticsearch"

	// API
	SinkTypeRestAPI SinkType = "rest_api"
	SinkTypeWebhook SinkType = "webhook"
)

// ProvisioningStatus 사전작업 상태
type ProvisioningStatus string

const (
	ProvisioningStatusPending    ProvisioningStatus = "pending"
	ProvisioningStatusInProgress ProvisioningStatus = "in_progress"
	ProvisioningStatusCompleted  ProvisioningStatus = "completed"
	ProvisioningStatusFailed     ProvisioningStatus = "failed"
	ProvisioningStatusSkipped    ProvisioningStatus = "skipped" // 사전작업 불필요
)

// ProvisioningType 사전작업 유형
type ProvisioningType string

const (
	ProvisioningTypeNone          ProvisioningType = "none"           // 사전작업 불필요
	ProvisioningTypeTableCreation ProvisioningType = "table_creation" // 테이블 생성
	ProvisioningTypeTopicCreation ProvisioningType = "topic_creation" // Kafka 토픽 생성
	ProvisioningTypeIndexCreation ProvisioningType = "index_creation" // ES 인덱스 생성
	ProvisioningTypeBucketSetup   ProvisioningType = "bucket_setup"   // S3/GCS 버킷 설정
	ProvisioningTypeAPISetup      ProvisioningType = "api_setup"      // REST API 설정
	ProvisioningTypeExternal      ProvisioningType = "external"       // 외부 페이지에서 처리
)

// ProvisioningRequest 사전작업 요청
type ProvisioningRequest struct {
	ID          string           `json:"id"`
	PipelineID  string           `json:"pipeline_id"`
	SinkType    SinkType         `json:"sink_type"`
	SinkName    string           `json:"sink_name"`
	Type        ProvisioningType `json:"type"`
	Config      map[string]any   `json:"config"`       // 저장소별 설정
	ExternalURL string           `json:"external_url"` // 외부 페이지 URL (type=external인 경우)
	CallbackURL string           `json:"callback_url"` // 완료 후 콜백 URL
	RequestedBy string           `json:"requested_by"`
	RequestedAt time.Time        `json:"requested_at"`
}

// ProvisioningResult 사전작업 결과
type ProvisioningResult struct {
	ID         string             `json:"id"`
	RequestID  string             `json:"request_id"`
	PipelineID string             `json:"pipeline_id"`
	SinkType   SinkType           `json:"sink_type"`
	Status     ProvisioningStatus `json:"status"`

	// 결과 정보 - 저장소별로 다름
	TableName   string `json:"table_name,omitempty"`   // SQL, HBase, MongoDB
	TopicName   string `json:"topic_name,omitempty"`   // Kafka
	IndexName   string `json:"index_name,omitempty"`   // Elasticsearch
	BucketName  string `json:"bucket_name,omitempty"`  // S3, GCS
	FilePath    string `json:"file_path,omitempty"`    // HDFS, File
	APIEndpoint string `json:"api_endpoint,omitempty"` // REST API
	APIKey      string `json:"api_key,omitempty"`      // API 인증키

	// 추가 메타데이터
	Metadata map[string]any `json:"metadata,omitempty"`

	// 상태 정보
	Message     string     `json:"message,omitempty"`
	ErrorDetail string     `json:"error_detail,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CompletedBy string     `json:"completed_by,omitempty"` // 외부에서 완료한 경우 사용자 정보
}

// SinkConfig 저장소 설정
type SinkConfig struct {
	Type SinkType `json:"type" yaml:"type"`
	Name string   `json:"name" yaml:"name"`

	// 공통 설정
	Inputs        []string `json:"inputs,omitempty" yaml:"inputs"`
	BatchSize     int      `json:"batch_size,omitempty" yaml:"batch_size"`
	FlushInterval string   `json:"flush_interval,omitempty" yaml:"flush_interval"`
	RetryCount    int      `json:"retry_count,omitempty" yaml:"retry_count"`
	RetryInterval string   `json:"retry_interval,omitempty" yaml:"retry_interval"`

	// 사전작업 관련
	RequiresProvisioning bool                `json:"requires_provisioning,omitempty" yaml:"requires_provisioning"`
	ProvisioningType     ProvisioningType    `json:"provisioning_type,omitempty" yaml:"provisioning_type"`
	ProvisioningResult   *ProvisioningResult `json:"provisioning_result,omitempty" yaml:"provisioning_result"`

	// 저장소별 설정
	Config map[string]any `json:"config,omitempty" yaml:"config"`
}

// SinkRequirement 저장소 요구사항 정보
type SinkRequirement struct {
	Type                 SinkType           `json:"type"`
	DisplayName          string             `json:"display_name"`
	Description          string             `json:"description"`
	RequiresProvisioning bool               `json:"requires_provisioning"`
	ProvisioningTypes    []ProvisioningType `json:"provisioning_types"`
	ConfigSchema         map[string]any     `json:"config_schema"` // JSON Schema
	ExternalSetupURL     string             `json:"external_setup_url,omitempty"`
}

// GetSinkRequirements 저장소별 요구사항 반환
func GetSinkRequirements() []SinkRequirement {
	return []SinkRequirement{
		{
			Type:                 SinkTypeFile,
			DisplayName:          "File",
			Description:          "로컬 파일 시스템에 저장",
			RequiresProvisioning: false,
		},
		{
			Type:                 SinkTypeKafka,
			DisplayName:          "Kafka",
			Description:          "Apache Kafka 토픽으로 전송",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeTopicCreation, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeHDFS,
			DisplayName:          "HDFS",
			Description:          "Hadoop HDFS에 저장",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeSQL,
			DisplayName:          "SQL Database",
			Description:          "SQL 데이터베이스 (MySQL, PostgreSQL 등)",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeTableCreation, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeMongoDB,
			DisplayName:          "MongoDB",
			Description:          "MongoDB 컬렉션에 저장",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeTableCreation, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeHBase,
			DisplayName:          "HBase",
			Description:          "Apache HBase 테이블에 저장",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeTableCreation, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeElastic,
			DisplayName:          "Elasticsearch",
			Description:          "Elasticsearch 인덱스에 저장",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeIndexCreation, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeS3,
			DisplayName:          "Amazon S3",
			Description:          "AWS S3 버킷에 저장",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeBucketSetup, ProvisioningTypeExternal},
		},
		{
			Type:                 SinkTypeRestAPI,
			DisplayName:          "REST API",
			Description:          "외부 REST API로 전송",
			RequiresProvisioning: true,
			ProvisioningTypes:    []ProvisioningType{ProvisioningTypeAPISetup, ProvisioningTypeExternal},
		},
	}
}
