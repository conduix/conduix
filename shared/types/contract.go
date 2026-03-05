// Package types Data Contract 타입 정의
// ECL (Extract-Contextualize-Link) 패러다임 지원
package types

import "time"

// DataContract 데이터 계약 정의
// 스키마 검증 + 비즈니스 규칙 + 메타데이터를 포함
type DataContract struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Owner       string            `json:"owner,omitempty" yaml:"owner,omitempty"`
	Team        string            `json:"team,omitempty" yaml:"team,omitempty"`
	SLA         *ContractSLA      `json:"sla,omitempty" yaml:"sla,omitempty"`
	Schema      *ContractSchema   `json:"schema,omitempty" yaml:"schema,omitempty"`
	Rules       []BusinessRule    `json:"rules,omitempty" yaml:"rules,omitempty"`
	Tags        map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// ContractSLA 서비스 수준 계약
type ContractSLA struct {
	Freshness    string  `json:"freshness,omitempty" yaml:"freshness,omitempty"`       // 데이터 신선도 (예: "5m", "1h")
	Completeness float64 `json:"completeness,omitempty" yaml:"completeness,omitempty"` // 완전성 비율 (0.0 ~ 1.0)
	Accuracy     float64 `json:"accuracy,omitempty" yaml:"accuracy,omitempty"`         // 정확도 비율 (0.0 ~ 1.0)
}

// ContractSchema 스키마 정의 (기존 schema 패키지와 호환)
type ContractSchema struct {
	Fields []ContractField `json:"fields" yaml:"fields"`
	Strict bool            `json:"strict,omitempty" yaml:"strict,omitempty"` // true면 정의되지 않은 필드 불허
}

// ContractField 필드 스키마
type ContractField struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type" yaml:"type"` // string, number, integer, boolean, object, array, any
	Required    bool     `json:"required,omitempty" yaml:"required,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Pattern     string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	MinLength   *int     `json:"min_length,omitempty" yaml:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty" yaml:"max_length,omitempty"`
	Min         *float64 `json:"min,omitempty" yaml:"min,omitempty"`
	Max         *float64 `json:"max,omitempty" yaml:"max,omitempty"`
	Enum        []any    `json:"enum,omitempty" yaml:"enum,omitempty"`
}

// BusinessRule 비즈니스 규칙 정의
type BusinessRule struct {
	Name        string       `json:"name" yaml:"name"`
	Description string       `json:"description,omitempty" yaml:"description,omitempty"`
	Condition   string       `json:"condition" yaml:"condition"` // 표현식 (예: "amount >= 0 AND amount <= 1000000")
	Severity    RuleSeverity `json:"severity,omitempty" yaml:"severity,omitempty"`
	Tags        []string     `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// RuleSeverity 규칙 위반 심각도
type RuleSeverity string

const (
	SeverityError   RuleSeverity = "error"   // 레코드 거부
	SeverityWarning RuleSeverity = "warning" // 경고 (레코드는 통과)
	SeverityInfo    RuleSeverity = "info"    // 정보성 로깅만
)

// ContractViolation 계약 위반 정보
type ContractViolation struct {
	RecordID    string            `json:"record_id,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	ContractID  string            `json:"contract_id"`
	RuleName    string            `json:"rule_name"`
	Severity    RuleSeverity      `json:"severity"`
	Message     string            `json:"message"`
	FieldErrors []FieldViolation  `json:"field_errors,omitempty"`
	OriginalData map[string]any   `json:"original_data,omitempty"`
}

// FieldViolation 필드별 위반 정보
type FieldViolation struct {
	Field   string `json:"field"`
	Value   any    `json:"value,omitempty"`
	Message string `json:"message"`
}

// ContractValidationResult 계약 검증 결과
type ContractValidationResult struct {
	Valid      bool                 `json:"valid"`
	Violations []ContractViolation  `json:"violations,omitempty"`
	Warnings   []ContractViolation  `json:"warnings,omitempty"`
}

// ViolationAction 위반 시 처리 방식
type ViolationAction string

const (
	ActionDrop       ViolationAction = "drop"       // 레코드 삭제
	ActionQuarantine ViolationAction = "quarantine" // DLQ로 전송
	ActionTag        ViolationAction = "tag"        // 위반 태그 추가 후 통과
	ActionError      ViolationAction = "error"      // 에러 반환 (파이프라인 중단)
)

// DLQRecord Dead Letter Queue 레코드
type DLQRecord struct {
	ID           string            `json:"id"`
	Timestamp    time.Time         `json:"timestamp"`
	Source       string            `json:"source"`
	PipelineID   string            `json:"pipeline_id"`
	WorkflowID   string            `json:"workflow_id,omitempty"`
	ContractID   string            `json:"contract_id,omitempty"`
	Violations   []ContractViolation `json:"violations"`
	OriginalData map[string]any    `json:"original_data"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	RetryCount   int               `json:"retry_count"`
	MaxRetries   int               `json:"max_retries"`
	LastError    string            `json:"last_error,omitempty"`
}

// DLQConfig Dead Letter Queue 설정
type DLQConfig struct {
	Enabled       bool              `json:"enabled" yaml:"enabled"`
	Type          string            `json:"type" yaml:"type"` // kafka, file, http
	MaxRetries    int               `json:"max_retries,omitempty" yaml:"max_retries,omitempty"`
	RetryInterval string            `json:"retry_interval,omitempty" yaml:"retry_interval,omitempty"`

	// Kafka DLQ
	Brokers      []string `json:"brokers,omitempty" yaml:"brokers,omitempty"`
	Topic        string   `json:"topic,omitempty" yaml:"topic,omitempty"`
	RetentionMs  int64    `json:"retention_ms,omitempty" yaml:"retention_ms,omitempty"` // Kafka topic retention (ms)

	// File DLQ
	Path      string `json:"path,omitempty" yaml:"path,omitempty"`
	Format    string `json:"format,omitempty" yaml:"format,omitempty"`      // json, jsonl
	MaxSizeMB int    `json:"max_size_mb,omitempty" yaml:"max_size_mb,omitempty"` // 파일 최대 크기 (MB), 초과 시 rotation
	MaxAgeDays int   `json:"max_age_days,omitempty" yaml:"max_age_days,omitempty"` // 보관 기간 (일)
	MaxBackups int   `json:"max_backups,omitempty" yaml:"max_backups,omitempty"` // 최대 백업 파일 수

	// HTTP DLQ (webhook)
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// ContractMetrics 계약 검증 메트릭
type ContractMetrics struct {
	ContractID     string    `json:"contract_id"`
	TotalRecords   int64     `json:"total_records"`
	ValidRecords   int64     `json:"valid_records"`
	InvalidRecords int64     `json:"invalid_records"`
	WarningRecords int64     `json:"warning_records"`
	ViolationsByRule map[string]int64 `json:"violations_by_rule"`
	LastUpdated    time.Time `json:"last_updated"`
}
