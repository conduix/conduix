package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// PipelineMode 파이프라인 모드
type PipelineMode string

const (
	ModeBatch    PipelineMode = "batch"
	ModeRealtime PipelineMode = "realtime"
)

// PipelineConfigV2 v2 파이프라인 설정
type PipelineConfigV2 struct {
	Name     string          `yaml:"name"`
	Mode     PipelineMode    `yaml:"type"` // batch | realtime
	Source   SourceV2        `yaml:"source"`
	Realtime *RealtimeConfig `yaml:"realtime,omitempty"`
	Steps    []StepV2        `yaml:"steps"`
	Output   OutputConfig    `yaml:"output"`
}

// SourceV2 데이터 소스 설정
type SourceV2 struct {
	Type string `yaml:"type"` // file, sql, http, kafka, sql_event, cdc

	// File
	Path   string   `yaml:"path,omitempty"`
	Paths  []string `yaml:"paths,omitempty"`
	Format string   `yaml:"format,omitempty"` // json, csv, lines

	// SQL (query-based)
	Driver      string             `yaml:"driver,omitempty"` // mysql, postgres
	DSN         string             `yaml:"dsn,omitempty"`
	Query       string             `yaml:"query,omitempty"`
	Params      []string           `yaml:"params,omitempty"`
	Incremental *IncrementalConfig `yaml:"incremental,omitempty"`

	// HTTP
	URL        string            `yaml:"url,omitempty"`
	Method     string            `yaml:"method,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Body       string            `yaml:"body,omitempty"`
	Auth       *AuthConfig       `yaml:"auth,omitempty"`
	Pagination *PaginationConfig `yaml:"pagination,omitempty"`

	// Kafka
	Brokers        []string `yaml:"brokers,omitempty"`
	Topics         []string `yaml:"topics,omitempty"`
	GroupID        string   `yaml:"group_id,omitempty"`
	StartOffset    string   `yaml:"start_offset,omitempty"` // earliest, latest
	MinBytes       int      `yaml:"min_bytes,omitempty"`
	MaxBytes       int      `yaml:"max_bytes,omitempty"`
	MaxWait        int      `yaml:"max_wait,omitempty"`        // milliseconds
	CommitInterval int      `yaml:"commit_interval,omitempty"` // milliseconds

	// SQL Event Table (polling-based)
	Table           string   `yaml:"table,omitempty"`
	IDColumn        string   `yaml:"id_column,omitempty"` // default: "id"
	TimestampColumn string   `yaml:"timestamp_column,omitempty"`
	Columns         []string `yaml:"columns,omitempty"` // columns to select
	Where           string   `yaml:"where,omitempty"`   // additional WHERE clause
	OrderBy         string   `yaml:"order_by,omitempty"`
	BatchSize       int      `yaml:"batch_size,omitempty"`    // default: 1000
	PollInterval    int      `yaml:"poll_interval,omitempty"` // milliseconds, default: 1000

	// CDC (Change Data Capture)
	Host     string   `yaml:"host,omitempty"`
	Port     int      `yaml:"port,omitempty"`
	Username string   `yaml:"username,omitempty"`
	Password string   `yaml:"password,omitempty"`
	Database string   `yaml:"database,omitempty"`
	Tables   []string `yaml:"tables,omitempty"`    // tables to watch
	ServerID uint32   `yaml:"server_id,omitempty"` // MySQL server ID for binlog
	SlotName string   `yaml:"slot_name,omitempty"` // PostgreSQL replication slot
}

// IncrementalConfig 증분 처리 설정 (SQL용)
type IncrementalConfig struct {
	Column   string `yaml:"column"`
	StateKey string `yaml:"state_key"`
}

// AuthConfig HTTP 인증 설정
type AuthConfig struct {
	Type         string   `yaml:"type"` // basic, bearer, oauth2
	Username     string   `yaml:"username,omitempty"`
	Password     string   `yaml:"password,omitempty"`
	Token        string   `yaml:"token,omitempty"`
	ClientID     string   `yaml:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret,omitempty"`
	TokenURL     string   `yaml:"token_url,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty"`
}

// PaginationConfig HTTP 페이징 설정
type PaginationConfig struct {
	Type      string `yaml:"type"`       // next_url, offset, cursor
	NextField string `yaml:"next_field"` // 응답에서 다음 URL 필드
	DataField string `yaml:"data_field"` // 실제 데이터 필드
	MaxPages  int    `yaml:"max_pages"`  // 최대 페이지 수
}

// RealtimeConfig 실시간 파이프라인 설정
type RealtimeConfig struct {
	IDField        string `yaml:"id_field"`         // 중복 체크용 ID 필드
	EventTypeField string `yaml:"event_type_field"` // CREATE/UPDATE/DELETE 구분
	EntityIDField  string `yaml:"entity_id_field"`  // 엔티티 ID 필드
	DedupStorage   string `yaml:"dedup_storage"`    // redis, memory
	DedupTTL       string `yaml:"dedup_ttl"`        // 중복 ID 보관 기간
}

// StepV2 처리 단계
type StepV2 struct {
	Name      string       `yaml:"name"`
	Transform string       `yaml:"transform,omitempty"` // Bloblang 변환
	Filter    FilterConfig `yaml:"filter,omitempty"`    // 필터 조건 (문자열 또는 구조화)
	Sample    float64      `yaml:"sample,omitempty"`    // 샘플링 비율
	Select    []string     `yaml:"select,omitempty"`    // 필드 선택
	Exclude   []string     `yaml:"exclude,omitempty"`   // 필드 제외
}

// FilterConfig 필터 설정 (문자열 또는 구조화된 형식)
type FilterConfig struct {
	// 문자열 표현식 (간단한 경우)
	Expression string `yaml:"-" json:"expression,omitempty"`

	// 구조화된 필터 (GUI 편집용)
	Root *FilterNode `yaml:"-" json:"root,omitempty"`

	// 원본 데이터
	raw any
}

// FilterNode 필터 노드
type FilterNode struct {
	Type      string           `yaml:"type" json:"type"` // "condition" 또는 "group"
	Condition *FilterCondition `yaml:"condition,omitempty" json:"condition,omitempty"`
	Group     *FilterGroup     `yaml:"group,omitempty" json:"group,omitempty"`
}

// FilterCondition 단일 조건
type FilterCondition struct {
	ID    string `yaml:"id,omitempty" json:"id,omitempty"`
	Field string `yaml:"field" json:"field"`
	Op    string `yaml:"op" json:"op"`
	Value any    `yaml:"value,omitempty" json:"value,omitempty"`
}

// FilterGroup 조건 그룹
type FilterGroup struct {
	ID         string       `yaml:"id,omitempty" json:"id,omitempty"`
	Operator   string       `yaml:"operator" json:"operator"` // "and" 또는 "or"
	Conditions []FilterNode `yaml:"conditions" json:"conditions"`
}

// UnmarshalYAML 커스텀 YAML 언마샬링 (문자열/구조체 모두 지원)
func (fc *FilterConfig) UnmarshalYAML(node *yaml.Node) error {
	// 문자열인 경우
	if node.Kind == yaml.ScalarNode {
		fc.Expression = node.Value
		fc.raw = node.Value
		return nil
	}

	// 구조화된 객체인 경우
	var structured struct {
		Root *FilterNode `yaml:"root"`
	}
	if err := node.Decode(&structured); err != nil {
		return err
	}
	fc.Root = structured.Root
	fc.raw = structured
	return nil
}

// MarshalYAML 커스텀 YAML 마샬링
func (fc FilterConfig) MarshalYAML() (interface{}, error) {
	// 문자열 표현식만 있는 경우
	if fc.Root == nil && fc.Expression != "" {
		return fc.Expression, nil
	}
	// 구조화된 필터
	if fc.Root != nil {
		return map[string]any{"root": fc.Root}, nil
	}
	return nil, nil
}

// IsEmpty 필터가 비어있는지 확인
func (fc *FilterConfig) IsEmpty() bool {
	return fc.Expression == "" && fc.Root == nil
}

// GetExpression 표현식 반환 (구조화된 경우 변환)
func (fc *FilterConfig) GetExpression() string {
	if fc.Expression != "" {
		return fc.Expression
	}
	// TODO: 구조화된 필터를 표현식으로 변환
	return ""
}

// OutputConfig 출력 설정 (Stub)
type OutputConfig struct {
	Type      string               `yaml:"type"` // stub
	LogLevel  string               `yaml:"log_level,omitempty"`
	LogFormat string               `yaml:"log_format,omitempty"`
	Metrics   *MetricsOutputConfig `yaml:"metrics,omitempty"`
	Callback  *CallbackConfig      `yaml:"callback,omitempty"`
}

// MetricsOutputConfig 메트릭 출력 설정
type MetricsOutputConfig struct {
	Enabled bool   `yaml:"enabled"`
	Prefix  string `yaml:"prefix,omitempty"`
}

// CallbackConfig 콜백 설정
type CallbackConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url,omitempty"`
}

// LoadConfigV2 v2 설정 파일 로드
func LoadConfigV2(path string) (*PipelineConfigV2, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return ParseConfigV2(data)
}

// ParseConfigV2 v2 설정 파싱
func ParseConfigV2(data []byte) (*PipelineConfigV2, error) {
	// 환경 변수 치환
	expanded := os.ExpandEnv(string(data))

	var config PipelineConfigV2
	if err := yaml.Unmarshal([]byte(expanded), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate 설정 검증
func (c *PipelineConfigV2) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}

	if c.Mode == "" {
		c.Mode = ModeBatch // 기본값
	}

	if c.Mode != ModeBatch && c.Mode != ModeRealtime {
		return fmt.Errorf("invalid pipeline mode: %s (must be 'batch' or 'realtime')", c.Mode)
	}

	// 소스 검증
	if err := c.validateSource(); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	// 실시간 모드 검증
	if c.Mode == ModeRealtime {
		if err := c.validateRealtime(); err != nil {
			return fmt.Errorf("realtime: %w", err)
		}
	}

	// 출력 기본값
	if c.Output.Type == "" {
		c.Output.Type = "stub"
	}

	return nil
}

func (c *PipelineConfigV2) validateSource() error {
	switch c.Source.Type {
	case "file":
		if c.Source.Path == "" && len(c.Source.Paths) == 0 {
			return fmt.Errorf("file path is required")
		}
		if c.Source.Format == "" {
			c.Source.Format = "json"
		}

	case "sql":
		if c.Source.Driver == "" {
			return fmt.Errorf("sql driver is required")
		}
		if c.Source.DSN == "" {
			return fmt.Errorf("sql dsn is required")
		}
		if c.Source.Query == "" {
			return fmt.Errorf("sql query is required")
		}

	case "http", "rest_api":
		if c.Source.URL == "" {
			return fmt.Errorf("http url is required")
		}
		if c.Source.Method == "" {
			c.Source.Method = "GET"
		}
		if c.Source.Auth != nil {
			if err := c.validateAuth(); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}

	case "kafka":
		if len(c.Source.Brokers) == 0 {
			return fmt.Errorf("kafka brokers are required")
		}
		if len(c.Source.Topics) == 0 {
			return fmt.Errorf("kafka topics are required")
		}
		if c.Source.GroupID == "" {
			c.Source.GroupID = c.Name + "-consumer"
		}

	case "sql_event":
		if c.Source.Driver == "" {
			return fmt.Errorf("sql_event driver is required")
		}
		if c.Source.DSN == "" {
			return fmt.Errorf("sql_event dsn is required")
		}
		if c.Source.Table == "" {
			return fmt.Errorf("sql_event table is required")
		}
		if c.Source.IDColumn == "" {
			c.Source.IDColumn = "id"
		}
		if c.Source.BatchSize <= 0 {
			c.Source.BatchSize = 1000
		}
		if c.Source.PollInterval <= 0 {
			c.Source.PollInterval = 1000
		}

	case "cdc":
		if c.Source.Driver == "" {
			return fmt.Errorf("cdc driver is required (mysql or postgres)")
		}
		if c.Source.Host == "" {
			return fmt.Errorf("cdc host is required")
		}
		if c.Source.Username == "" {
			return fmt.Errorf("cdc username is required")
		}
		if c.Source.Port <= 0 {
			switch c.Source.Driver {
			case "mysql":
				c.Source.Port = 3306
			case "postgres":
				c.Source.Port = 5432
			}
		}
		if c.Source.ServerID == 0 {
			c.Source.ServerID = 101
		}

	default:
		return fmt.Errorf("unsupported source type: %s", c.Source.Type)
	}

	return nil
}

func (c *PipelineConfigV2) validateAuth() error {
	auth := c.Source.Auth
	switch auth.Type {
	case "basic":
		if auth.Username == "" || auth.Password == "" {
			return fmt.Errorf("basic auth requires username and password")
		}
	case "bearer":
		if auth.Token == "" {
			return fmt.Errorf("bearer auth requires token")
		}
	case "oauth2":
		if auth.ClientID == "" || auth.ClientSecret == "" || auth.TokenURL == "" {
			return fmt.Errorf("oauth2 requires client_id, client_secret, and token_url")
		}
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
	return nil
}

func (c *PipelineConfigV2) validateRealtime() error {
	if c.Realtime == nil {
		return fmt.Errorf("realtime config is required for realtime mode")
	}

	if c.Realtime.IDField == "" {
		return fmt.Errorf("id_field is required for deduplication")
	}

	if c.Realtime.DedupStorage == "" {
		c.Realtime.DedupStorage = "memory"
	}

	if c.Realtime.DedupTTL == "" {
		c.Realtime.DedupTTL = "24h"
	}

	// TTL 파싱 검증
	if _, err := time.ParseDuration(c.Realtime.DedupTTL); err != nil {
		return fmt.Errorf("invalid dedup_ttl format: %w", err)
	}

	return nil
}

// IsBatch 배치 모드 여부
func (c *PipelineConfigV2) IsBatch() bool {
	return c.Mode == ModeBatch
}

// IsRealtime 실시간 모드 여부
func (c *PipelineConfigV2) IsRealtime() bool {
	return c.Mode == ModeRealtime
}
