package config

import (
	"fmt"
	"os"
	"strings"
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
	Input    InputV2         `yaml:"input"`
	Source   *InputV2        `yaml:"source,omitempty"` // Deprecated: Input을 사용하세요 (하위호환성)
	Realtime *RealtimeConfig `yaml:"realtime,omitempty"`
	Contract *ContractConfig `yaml:"contract,omitempty"` // Data Contract 설정
	Steps    []StepV2        `yaml:"steps"`
	Output   OutputConfig    `yaml:"output"`
}

// GetInput Input을 반환 (하위 호환성: Source가 설정된 경우 Source 반환)
func (c *PipelineConfigV2) GetInput() InputV2 {
	if c.Source != nil && c.Input.Type != "" {
		return *c.Source
	}
	return c.Input
}

// InputV2 데이터 입력 설정 (구 SourceV2)
// RateLimitInputConfig 입력 레벨 레이트 리밋 설정
type RateLimitInputConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Rate     int    `yaml:"rate" json:"rate"`                             // 단위 시간당 처리량
	Interval string `yaml:"interval" json:"interval"`                     // second, minute, hour
	Burst    int    `yaml:"burst,omitempty" json:"burst,omitempty"`       // 버스트 허용량 (토큰 버킷)
	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"` // token_bucket, sliding_window, fixed_window
}

// RateLimitSourceConfig는 RateLimitInputConfig의 별칭 (하위 호환성)
// Deprecated: RateLimitInputConfig를 사용하세요
type RateLimitSourceConfig = RateLimitInputConfig

type InputV2 struct {
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
	Brokers        []string         `yaml:"brokers,omitempty"`
	Topics         []string         `yaml:"topics,omitempty"`
	GroupID        string           `yaml:"group_id,omitempty"`
	StartOffset    string           `yaml:"start_offset,omitempty"` // earliest, latest
	MinBytes       int              `yaml:"min_bytes,omitempty"`
	MaxBytes       int              `yaml:"max_bytes,omitempty"`
	MaxWait        int              `yaml:"max_wait,omitempty"`        // milliseconds
	CommitInterval int              `yaml:"commit_interval,omitempty"` // milliseconds
	OnParseError   string           `yaml:"on_parse_error,omitempty"`  // raw(기본), drop, error — JSON 파싱 실패 시 처리
	SASL           *SASLConfig      `yaml:"sasl,omitempty" json:"sasl,omitempty"`
	TLS            *TLSClientConfig `yaml:"tls,omitempty" json:"tls,omitempty"`

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

	// Database TLS (SQL, CDC 공통 - TLS 필드는 Kafka에서도 사용)
	// DSN 대신 개별 필드로 TLS 설정 시 사용
	// MySQL: tls=custom, PostgreSQL: sslmode=verify-full
	DBTLS *DBTLSConfig `yaml:"db_tls,omitempty" json:"db_tls,omitempty"`

	// Kubernetes Logs
	K8sNamespace     string   `yaml:"namespace,omitempty" json:"namespace,omitempty"`           // target namespace (empty = all namespaces)
	K8sPodSelector   string   `yaml:"pod_selector,omitempty" json:"pod_selector,omitempty"`     // label selector (e.g., "app=nginx")
	K8sPodNames      []string `yaml:"pod_names,omitempty" json:"pod_names,omitempty"`           // specific pod names
	K8sContainerName string   `yaml:"container_name,omitempty" json:"container_name,omitempty"` // container name (for multi-container pods)
	K8sFollow        bool     `yaml:"follow,omitempty" json:"follow,omitempty"`                 // stream logs (realtime)
	K8sSinceSeconds  int64    `yaml:"since_seconds,omitempty" json:"since_seconds,omitempty"`   // logs from last N seconds
	K8sTailLines     int64    `yaml:"tail_lines,omitempty" json:"tail_lines,omitempty"`         // last N lines
	K8sKubeconfig    string   `yaml:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`         // external cluster kubeconfig path
	K8sContext       string   `yaml:"context,omitempty" json:"context,omitempty"`               // kubeconfig context name
	K8sLogFormat     string   `yaml:"log_format,omitempty" json:"log_format,omitempty"`         // auto, json, text (default: auto)
	K8sLogPattern    string   `yaml:"log_pattern,omitempty" json:"log_pattern,omitempty"`       // regex pattern with named groups for text logs

	// 동적 파티션 설정 (batch 소스용)
	Partition *PartitionConfig `yaml:"partition,omitempty" json:"partition,omitempty"`

	// Rate Limiting (공통)
	RateLimit *RateLimitSourceConfig `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`

	// RabbitMQ
	Queue         string `yaml:"queue,omitempty" json:"queue,omitempty"`
	Exchange      string `yaml:"exchange,omitempty" json:"exchange,omitempty"`
	ExchangeType  string `yaml:"exchange_type,omitempty" json:"exchange_type,omitempty"`   // direct, fanout, topic, headers
	RoutingKey    string `yaml:"routing_key,omitempty" json:"routing_key,omitempty"`       // queue binding routing key
	Prefetch      int    `yaml:"prefetch,omitempty" json:"prefetch,omitempty"`             // prefetch count (default: 10)
	AutoAck       bool   `yaml:"auto_ack,omitempty" json:"auto_ack,omitempty"`             // auto acknowledge messages
	Exclusive     bool   `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`           // exclusive queue
	Durable       bool   `yaml:"durable,omitempty" json:"durable,omitempty"`               // durable queue/exchange
	ConsumerTag   string `yaml:"consumer_tag,omitempty" json:"consumer_tag,omitempty"`     // consumer identifier
	ReconnectWait int    `yaml:"reconnect_wait,omitempty" json:"reconnect_wait,omitempty"` // reconnect wait in milliseconds

	// SQS (AWS Simple Queue Service)
	SQSQueueURL          string `yaml:"sqs_queue_url,omitempty" json:"sqs_queue_url,omitempty"`                   // SQS Queue URL
	SQSRegion            string `yaml:"sqs_region,omitempty" json:"sqs_region,omitempty"`                         // AWS Region
	SQSAccessKeyID       string `yaml:"sqs_access_key_id,omitempty" json:"sqs_access_key_id,omitempty"`           // AWS Access Key ID
	SQSSecretAccessKey   string `yaml:"sqs_secret_access_key,omitempty" json:"sqs_secret_access_key,omitempty"`   // AWS Secret Access Key
	SQSSessionToken      string `yaml:"sqs_session_token,omitempty" json:"sqs_session_token,omitempty"`           // AWS Session Token (임시 자격 증명용)
	SQSMaxMessages       int    `yaml:"sqs_max_messages,omitempty" json:"sqs_max_messages,omitempty"`             // 한 번에 수신할 최대 메시지 수 (1-10, default: 10)
	SQSWaitTimeSeconds   int    `yaml:"sqs_wait_time_seconds,omitempty" json:"sqs_wait_time_seconds,omitempty"`   // Long polling 대기 시간 (0-20, default: 20)
	SQSVisibilityTimeout int    `yaml:"sqs_visibility_timeout,omitempty" json:"sqs_visibility_timeout,omitempty"` // Visibility timeout in seconds (default: 30)
	SQSDeleteOnReceive   bool   `yaml:"sqs_delete_on_receive,omitempty" json:"sqs_delete_on_receive,omitempty"`   // 수신 후 자동 삭제
	SQSEndpoint          string `yaml:"sqs_endpoint,omitempty" json:"sqs_endpoint,omitempty"`                     // 커스텀 엔드포인트 (LocalStack 등)

	// WebSocket
	WSURL           string   `yaml:"ws_url,omitempty" json:"ws_url,omitempty"`                       // WebSocket URL (ws:// or wss://)
	WSSubprotocols  []string `yaml:"ws_subprotocols,omitempty" json:"ws_subprotocols,omitempty"`     // 서브프로토콜 목록
	WSPingInterval  int      `yaml:"ws_ping_interval,omitempty" json:"ws_ping_interval,omitempty"`   // Ping 간격 (milliseconds, default: 30000)
	WSPongWait      int      `yaml:"ws_pong_wait,omitempty" json:"ws_pong_wait,omitempty"`           // Pong 대기 시간 (milliseconds, default: 60000)
	WSReconnectWait int      `yaml:"ws_reconnect_wait,omitempty" json:"ws_reconnect_wait,omitempty"` // 재연결 대기 시간 (milliseconds, default: 5000)
	WSMaxReconnect  int      `yaml:"ws_max_reconnect,omitempty" json:"ws_max_reconnect,omitempty"`   // 최대 재연결 시도 횟수 (default: 10)
	WSMessageType   string   `yaml:"ws_message_type,omitempty" json:"ws_message_type,omitempty"`     // 메시지 타입: text, binary (default: text)
	WSSubscribeMsg  string   `yaml:"ws_subscribe_msg,omitempty" json:"ws_subscribe_msg,omitempty"`   // 연결 후 전송할 구독 메시지

	// MQTT
	MQTTBroker        string   `yaml:"mqtt_broker,omitempty" json:"mqtt_broker,omitempty"`                 // MQTT Broker URL (tcp://, ssl://, ws://, wss://)
	MQTTClientID      string   `yaml:"mqtt_client_id,omitempty" json:"mqtt_client_id,omitempty"`           // 클라이언트 ID
	MQTTUsername      string   `yaml:"mqtt_username,omitempty" json:"mqtt_username,omitempty"`             // 인증 사용자명
	MQTTPassword      string   `yaml:"mqtt_password,omitempty" json:"mqtt_password,omitempty"`             // 인증 비밀번호
	MQTTTopic         string   `yaml:"mqtt_topic,omitempty" json:"mqtt_topic,omitempty"`                   // 구독 토픽 (와일드카드 지원: +, #)
	MQTTTopics        []string `yaml:"mqtt_topics,omitempty" json:"mqtt_topics,omitempty"`                 // 다중 토픽 구독 (와일드카드 지원)
	MQTTQoS           int      `yaml:"mqtt_qos,omitempty" json:"mqtt_qos,omitempty"`                       // QoS 레벨 (0, 1, 2)
	MQTTCleanSession  bool     `yaml:"mqtt_clean_session,omitempty" json:"mqtt_clean_session,omitempty"`   // Clean session 여부
	MQTTKeepAlive     int      `yaml:"mqtt_keep_alive,omitempty" json:"mqtt_keep_alive,omitempty"`         // Keep-alive 간격 (seconds, default: 60)
	MQTTReconnectWait int      `yaml:"mqtt_reconnect_wait,omitempty" json:"mqtt_reconnect_wait,omitempty"` // 재연결 대기 시간 (milliseconds, default: 5000)
	MQTTMaxReconnect  int      `yaml:"mqtt_max_reconnect,omitempty" json:"mqtt_max_reconnect,omitempty"`   // 최대 재연결 시도 횟수 (default: 10)
	MQTTTopicFilter   string   `yaml:"mqtt_topic_filter,omitempty" json:"mqtt_topic_filter,omitempty"`     // 토픽 필터 정규식 패턴
	MQTTIncludeTopics []string `yaml:"mqtt_include_topics,omitempty" json:"mqtt_include_topics,omitempty"` // 포함할 토픽 목록 (와일드카드 지원)
	MQTTExcludeTopics []string `yaml:"mqtt_exclude_topics,omitempty" json:"mqtt_exclude_topics,omitempty"` // 제외할 토픽 목록 (와일드카드 지원)

	// SSE (Server-Sent Events)
	SSEURL           string `yaml:"sse_url,omitempty" json:"sse_url,omitempty"`                       // SSE 엔드포인트 URL
	SSEReconnectWait int    `yaml:"sse_reconnect_wait,omitempty" json:"sse_reconnect_wait,omitempty"` // 재연결 대기 시간 (milliseconds, default: 3000)
	SSEMaxReconnect  int    `yaml:"sse_max_reconnect,omitempty" json:"sse_max_reconnect,omitempty"`   // 최대 재연결 시도 횟수 (default: 10)
	SSELastEventID   string `yaml:"sse_last_event_id,omitempty" json:"sse_last_event_id,omitempty"`   // 마지막 이벤트 ID (재시작 시 복원용)

	// Google Cloud Pub/Sub
	PubSubProjectID              string `yaml:"pubsub_project_id,omitempty" json:"pubsub_project_id,omitempty"`                             // GCP 프로젝트 ID
	PubSubSubscription           string `yaml:"pubsub_subscription,omitempty" json:"pubsub_subscription,omitempty"`                         // 구독 이름
	PubSubCredentialsFile        string `yaml:"pubsub_credentials_file,omitempty" json:"pubsub_credentials_file,omitempty"`                 // 서비스 계정 JSON 파일 경로
	PubSubMaxOutstandingMessages int    `yaml:"pubsub_max_outstanding_messages,omitempty" json:"pubsub_max_outstanding_messages,omitempty"` // 최대 동시 처리 메시지 수 (default: 1000)
	PubSubMaxOutstandingBytes    int    `yaml:"pubsub_max_outstanding_bytes,omitempty" json:"pubsub_max_outstanding_bytes,omitempty"`       // 최대 동시 처리 바이트 (default: 100MB)
	PubSubMaxExtension           string `yaml:"pubsub_max_extension,omitempty" json:"pubsub_max_extension,omitempty"`                       // ack deadline 최대 연장 시간 (default: "10m")
	PubSubNumGoroutines          int    `yaml:"pubsub_num_goroutines,omitempty" json:"pubsub_num_goroutines,omitempty"`                     // 메시지 처리 고루틴 수 (default: 10)
	PubSubSynchronous            bool   `yaml:"pubsub_synchronous,omitempty" json:"pubsub_synchronous,omitempty"`                           // 동기 처리 모드 (default: false)

	// MongoDB CDC (Change Stream)
	MongoDBURI                string `yaml:"mongodb_uri,omitempty" json:"mongodb_uri,omitempty"`                                   // MongoDB URI
	MongoDBDatabase           string `yaml:"mongodb_database,omitempty" json:"mongodb_database,omitempty"`                         // Database 이름
	MongoDBCollection         string `yaml:"mongodb_collection,omitempty" json:"mongodb_collection,omitempty"`                     // Collection 이름 (빈 값이면 database 전체)
	MongoDBFullDocument       string `yaml:"mongodb_full_document,omitempty" json:"mongodb_full_document,omitempty"`               // updateLookup, whenAvailable, required
	MongoDBFullDocumentBefore string `yaml:"mongodb_full_document_before,omitempty" json:"mongodb_full_document_before,omitempty"` // off, whenAvailable, required
	MongoDBBatchSize          int    `yaml:"mongodb_batch_size,omitempty" json:"mongodb_batch_size,omitempty"`                     // Change Stream 배치 크기
	MongoDBMaxAwaitTime       string `yaml:"mongodb_max_await_time,omitempty" json:"mongodb_max_await_time,omitempty"`             // 최대 대기 시간
	MongoDBResumeAfter        string `yaml:"mongodb_resume_after,omitempty" json:"mongodb_resume_after,omitempty"`                 // Resume token
	MongoDBReconnectWait      int    `yaml:"mongodb_reconnect_wait,omitempty" json:"mongodb_reconnect_wait,omitempty"`             // 재연결 대기 시간 (milliseconds)
	MongoDBMaxReconnect       int    `yaml:"mongodb_max_reconnect,omitempty" json:"mongodb_max_reconnect,omitempty"`               // 최대 재연결 시도 횟수

	// Redis Stream
	RedisAddress         string `yaml:"redis_address,omitempty" json:"redis_address,omitempty"`                     // Redis 주소
	RedisPassword        string `yaml:"redis_password,omitempty" json:"redis_password,omitempty"`                   // Redis 비밀번호
	RedisDB              int    `yaml:"redis_db,omitempty" json:"redis_db,omitempty"`                               // Redis DB 번호
	RedisUsername        string `yaml:"redis_username,omitempty" json:"redis_username,omitempty"`                   // Redis 사용자 이름 (6.0+)
	RedisStream          string `yaml:"redis_stream,omitempty" json:"redis_stream,omitempty"`                       // Stream 이름
	RedisGroup           string `yaml:"redis_group,omitempty" json:"redis_group,omitempty"`                         // Consumer Group 이름
	RedisConsumer        string `yaml:"redis_consumer,omitempty" json:"redis_consumer,omitempty"`                   // Consumer 이름
	RedisCount           int    `yaml:"redis_count,omitempty" json:"redis_count,omitempty"`                         // 한 번에 읽을 메시지 수
	RedisBlock           string `yaml:"redis_block,omitempty" json:"redis_block,omitempty"`                         // Block 대기 시간
	RedisNoAck           bool   `yaml:"redis_no_ack,omitempty" json:"redis_no_ack,omitempty"`                       // Auto-ack 여부
	RedisStartID         string `yaml:"redis_start_id,omitempty" json:"redis_start_id,omitempty"`                   // 시작 ID (>, 0, $)
	RedisClaimMinIdle    string `yaml:"redis_claim_min_idle,omitempty" json:"redis_claim_min_idle,omitempty"`       // Pending claim 최소 유휴 시간
	RedisAutoCreateGroup bool   `yaml:"redis_auto_create_group,omitempty" json:"redis_auto_create_group,omitempty"` // Consumer Group 자동 생성
	RedisReconnectWait   int    `yaml:"redis_reconnect_wait,omitempty" json:"redis_reconnect_wait,omitempty"`       // 재연결 대기 시간 (milliseconds)
	RedisMaxReconnect    int    `yaml:"redis_max_reconnect,omitempty" json:"redis_max_reconnect,omitempty"`         // 최대 재연결 시도 횟수
	RedisTLSEnabled      bool   `yaml:"redis_tls_enabled,omitempty" json:"redis_tls_enabled,omitempty"`             // TLS 활성화
	RedisTLSSkipVerify   bool   `yaml:"redis_tls_skip_verify,omitempty" json:"redis_tls_skip_verify,omitempty"`     // TLS 인증서 검증 무시
}

// SourceV2는 InputV2의 별칭 (하위 호환성)
// Deprecated: InputV2를 사용하세요
type SourceV2 = InputV2

// IncrementalConfig 증분 처리 설정 (SQL용)
type IncrementalConfig struct {
	Column   string `yaml:"column"`
	StateKey string `yaml:"state_key"`
}

// AuthConfig HTTP 인증 설정
type AuthConfig struct {
	Type         string   `yaml:"type"` // basic, bearer, oauth2, api_key, mtls
	Username     string   `yaml:"username,omitempty"`
	Password     string   `yaml:"password,omitempty"`
	Token        string   `yaml:"token,omitempty"`
	ClientID     string   `yaml:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret,omitempty"`
	TokenURL     string   `yaml:"token_url,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty"`

	// OAuth2 확장 설정
	GrantType    string `yaml:"grant_type,omitempty" json:"grant_type,omitempty"`       // client_credentials, authorization_code, refresh_token
	RefreshToken string `yaml:"refresh_token,omitempty" json:"refresh_token,omitempty"` // Refresh token (환경변수 지원)
	AuthURL      string `yaml:"auth_url,omitempty" json:"auth_url,omitempty"`           // Authorization endpoint (PKCE용)
	RedirectURL  string `yaml:"redirect_url,omitempty" json:"redirect_url,omitempty"`   // Redirect URL (PKCE용)

	// PKCE 설정
	UsePKCE             bool   `yaml:"use_pkce,omitempty" json:"use_pkce,omitempty"`                           // PKCE 사용 여부
	PKCECodeVerifier    string `yaml:"pkce_code_verifier,omitempty" json:"pkce_code_verifier,omitempty"`       // Code verifier (자동 생성 가능)
	PKCEChallengeMethod string `yaml:"pkce_challenge_method,omitempty" json:"pkce_challenge_method,omitempty"` // S256 또는 plain (기본: S256)

	// API Key 인증
	APIKey     string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	APIKeyIn   string `yaml:"api_key_in,omitempty" json:"api_key_in,omitempty"`     // header or query
	APIKeyName string `yaml:"api_key_name,omitempty" json:"api_key_name,omitempty"` // header/query parameter name
	// mTLS 인증 (TLSClientConfig 참조)
	TLS *TLSClientConfig `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// SASLConfig Kafka SASL 인증 설정
type SASLConfig struct {
	// Mechanism SASL 메커니즘: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Mechanism string `yaml:"mechanism" json:"mechanism"`
	// Username SASL 사용자 이름
	Username string `yaml:"username" json:"username"`
	// Password SASL 비밀번호 (환경변수: ${VAR} 형식 지원)
	Password string `yaml:"password" json:"password"`
}

// TLSClientConfig TLS/SSL 클라이언트 설정
type TLSClientConfig struct {
	// Enabled TLS 사용 여부
	Enabled bool `yaml:"enabled" json:"enabled"`
	// CACert CA 인증서 파일 경로
	CACert string `yaml:"ca_cert,omitempty" json:"ca_cert,omitempty"`
	// ClientCert 클라이언트 인증서 파일 경로 (mTLS용)
	ClientCert string `yaml:"client_cert,omitempty" json:"client_cert,omitempty"`
	// ClientKey 클라이언트 개인키 파일 경로 (mTLS용)
	ClientKey string `yaml:"client_key,omitempty" json:"client_key,omitempty"`
	// SkipVerify 서버 인증서 검증 건너뛰기 (개발용, 운영 비권장)
	SkipVerify bool `yaml:"skip_verify,omitempty" json:"skip_verify,omitempty"`
	// ServerName TLS SNI 서버 이름 (선택)
	ServerName string `yaml:"server_name,omitempty" json:"server_name,omitempty"`
}

// DBTLSConfig 데이터베이스 TLS 설정 (MySQL, PostgreSQL 공통)
// SQL과 CDC 소스에서 DSN 대신 개별 필드로 TLS 설정 시 사용
type DBTLSConfig struct {
	// Enabled TLS 사용 여부
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Mode TLS 모드
	// MySQL: skip-verify, preferred, required (= verify-ca), verify-identity (= verify-full)
	// PostgreSQL: disable, allow, prefer, require, verify-ca, verify-full
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// CACert CA 인증서 파일 경로
	CACert string `yaml:"ca_cert,omitempty" json:"ca_cert,omitempty"`
	// ClientCert 클라이언트 인증서 파일 경로 (mTLS용)
	ClientCert string `yaml:"client_cert,omitempty" json:"client_cert,omitempty"`
	// ClientKey 클라이언트 개인키 파일 경로 (mTLS용)
	ClientKey string `yaml:"client_key,omitempty" json:"client_key,omitempty"`
	// ServerName 서버 이름 검증용 (verify-identity/verify-full에서 사용)
	ServerName string `yaml:"server_name,omitempty" json:"server_name,omitempty"`
}

// PaginationConfig HTTP 페이징 설정
type PaginationConfig struct {
	Type      string `yaml:"type" json:"type"`             // next_url, offset, page_increment, next_offset
	NextField string `yaml:"next_field" json:"next_field"` // 응답에서 다음 URL 필드
	DataField string `yaml:"data_field" json:"data_field"` // 실제 데이터 필드
	MaxPages  int    `yaml:"max_pages" json:"max_pages"`   // 최대 페이지 수

	// Offset/Page 페이지네이션
	PageParam    string `yaml:"page_param" json:"page_param"`         // 페이지 번호 파라미터 (기본: page)
	ParamName    string `yaml:"param_name" json:"param_name"`         // UI 호환: page_increment용 파라미터명
	PerPageParam string `yaml:"per_page_param" json:"per_page_param"` // 페이지 크기 파라미터 (기본: perPage)
	PerPage      int    `yaml:"per_page" json:"per_page"`             // 페이지당 항목 수 (기본: 10)
	StartPage    int    `yaml:"start_page" json:"start_page"`         // 시작 페이지 (기본: 1)
	StartValue   int    `yaml:"start_value" json:"start_value"`       // UI 호환: page_increment용 시작값
	TotalField   string `yaml:"total_field" json:"total_field"`       // 총 개수 필드 (자동 종료용)

	// Next Offset 페이지네이션
	OffsetParam string `yaml:"offset_param" json:"offset_param"` // offset 쿼리 파라미터 (기본: offset)
	OffsetPath  string `yaml:"offset_path" json:"offset_path"`   // 응답에서 다음 offset 경로 (예: meta.next_offset)

	// Next URL 페이지네이션
	URLPath string `yaml:"url_path" json:"url_path"` // 응답에서 다음 URL 경로 (예: links.next)
}

// PartitionConfig 동적 파티션 설정 (batch 소스용)
// HTTP/SQL 소스에서 파티션 목록을 동적으로 조회 후 병렬 처리
type PartitionConfig struct {
	// 공통 설정
	Parallelism int `yaml:"parallelism,omitempty" json:"parallelism,omitempty"` // 동시 처리 파티션 수 (기본: 4)

	// HTTP 파티션 디스커버리
	DiscoveryURL     string            `yaml:"discovery_url,omitempty" json:"discovery_url,omitempty"`         // 파티션 목록 조회 URL
	DiscoveryMethod  string            `yaml:"discovery_method,omitempty" json:"discovery_method,omitempty"`   // HTTP 메서드 (기본: GET)
	DiscoveryHeaders map[string]string `yaml:"discovery_headers,omitempty" json:"discovery_headers,omitempty"` // 추가 헤더
	DiscoveryAuth    *AuthConfig       `yaml:"discovery_auth,omitempty" json:"discovery_auth,omitempty"`       // 인증 (없으면 소스 auth 사용)

	// 파티션 ID 경로 (JSONPath-like)
	// 예: "partitions.[*].url" → partitions 배열의 각 요소에서 url 필드 추출
	// 예: "data.items" → data.items 배열 요소 자체가 파티션 ID
	// 예: "[*].name" → 루트 배열의 각 요소에서 name 필드 추출
	PartitionIDPath string `yaml:"partition_id_path,omitempty" json:"partition_id_path,omitempty"`

	// Deprecated: partition_id_path로 대체됨 (하위 호환성 유지)
	PartitionListPath string `yaml:"partition_list_path,omitempty" json:"partition_list_path,omitempty"`
	PartitionIDField  string `yaml:"partition_id_field,omitempty" json:"partition_id_field,omitempty"`

	// SQL 파티션 디스커버리
	DiscoveryQuery string `yaml:"discovery_query,omitempty" json:"discovery_query,omitempty"` // 파티션 목록 조회 쿼리

	// 파티션별 데이터 조회 템플릿 (${partition} 변수 사용)
	URLTemplate   string `yaml:"url_template,omitempty" json:"url_template,omitempty"`     // HTTP: 파티션별 URL 템플릿
	QueryTemplate string `yaml:"query_template,omitempty" json:"query_template,omitempty"` // SQL: 파티션별 쿼리 템플릿

	// 정적 파티션 목록 (디스커버리 대신 직접 지정)
	StaticPartitions []string `yaml:"static_partitions,omitempty" json:"static_partitions,omitempty"`
}

// GetPartitionListPath 하위 호환성: partition_id_path에서 list path 부분 추출
// "partitions.[*].url" → "partitions"
// "data.items" → "data.items"
// "[*].name" → "" (루트 배열)
func (c *PartitionConfig) GetPartitionListPath() string {
	if c.PartitionIDPath != "" {
		return parseListPath(c.PartitionIDPath)
	}
	// 하위 호환성: 기존 필드 사용
	if c.PartitionListPath != "" {
		return c.PartitionListPath
	}
	return "partitions"
}

// GetPartitionIDField 하위 호환성: partition_id_path에서 ID field 부분 추출
// "partitions.[*].url" → "url"
// "data.items" → "" (배열 요소 자체)
// "[*].name" → "name"
func (c *PartitionConfig) GetPartitionIDField() string {
	if c.PartitionIDPath != "" {
		return parseIDField(c.PartitionIDPath)
	}
	// 하위 호환성: 기존 필드 사용
	return c.PartitionIDField
}

// parseListPath "[*]" 앞부분을 list path로 추출
// "partitions.[*].url" → "partitions"
// "data.items.[*].id" → "data.items"
// "data.items" → "data.items" ([*] 없으면 전체가 list path)
// "[*].name" → "" (루트 배열)
func parseListPath(idPath string) string {
	idx := strings.Index(idPath, ".[*]")
	if idx >= 0 {
		return idPath[:idx]
	}
	if strings.HasPrefix(idPath, "[*]") {
		return ""
	}
	// [*]가 없으면 전체가 list path (배열 요소 자체가 ID)
	return idPath
}

// parseIDField "[*]" 뒷부분을 ID field로 추출
// "partitions.[*].url" → "url"
// "data.items.[*].nested.id" → "nested.id"
// "data.items" → "" (배열 요소 자체)
// "[*].name" → "name"
func parseIDField(idPath string) string {
	idx := strings.Index(idPath, "[*]")
	if idx < 0 {
		return ""
	}
	after := idPath[idx+3:]
	if strings.HasPrefix(after, ".") {
		return after[1:]
	}
	return ""
}

// RealtimeConfig 실시간 파이프라인 설정
type RealtimeConfig struct {
	IDField        string `yaml:"id_field"`         // 중복 체크용 ID 필드
	EventTypeField string `yaml:"event_type_field"` // CREATE/UPDATE/DELETE 구분
	EntityIDField  string `yaml:"entity_id_field"`  // 엔티티 ID 필드
	DedupStorage   string `yaml:"dedup_storage"`    // redis, memory
	DedupTTL       string `yaml:"dedup_ttl"`        // 중복 ID 보관 기간
	DedupRedisAddr string `yaml:"dedup_redis_addr"` // redis storage 사용 시 Redis 주소 (기본 localhost:6379)
}

// ContractConfig Data Contract 설정
type ContractConfig struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Owner       string            `yaml:"owner,omitempty"`
	Team        string            `yaml:"team,omitempty"`
	SLA         *ContractSLA      `yaml:"sla,omitempty"`
	Schema      *ContractSchema   `yaml:"schema,omitempty"`
	Rules       []BusinessRule    `yaml:"rules,omitempty"`
	OnViolation string            `yaml:"on_violation,omitempty"` // drop, quarantine, tag, error
	DLQ         *DLQConfig        `yaml:"dlq,omitempty"`
	Tags        map[string]string `yaml:"tags,omitempty"`
}

// ContractSLA 서비스 수준 계약
type ContractSLA struct {
	Freshness    string  `yaml:"freshness,omitempty"`
	Completeness float64 `yaml:"completeness,omitempty"`
	Accuracy     float64 `yaml:"accuracy,omitempty"`
}

// ContractSchema 스키마 정의
type ContractSchema struct {
	Fields []ContractField `yaml:"fields"`
	Strict bool            `yaml:"strict,omitempty"`
}

// ContractField 필드 스키마
type ContractField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Pattern     string   `yaml:"pattern,omitempty"`
	MinLength   *int     `yaml:"min_length,omitempty"`
	MaxLength   *int     `yaml:"max_length,omitempty"`
	Min         *float64 `yaml:"min,omitempty"`
	Max         *float64 `yaml:"max,omitempty"`
	Enum        []any    `yaml:"enum,omitempty"`
}

// BusinessRule 비즈니스 규칙 정의
type BusinessRule struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Condition   string   `yaml:"condition"`
	Severity    string   `yaml:"severity,omitempty"` // error, warning, info
	Tags        []string `yaml:"tags,omitempty"`
}

// DLQConfig Dead Letter Queue 설정
type DLQConfig struct {
	Enabled       bool              `yaml:"enabled"`
	Type          string            `yaml:"type"` // kafka, file, http
	MaxRetries    int               `yaml:"max_retries,omitempty"`
	RetryInterval string            `yaml:"retry_interval,omitempty"`
	Brokers       []string          `yaml:"brokers,omitempty"`
	Topic         string            `yaml:"topic,omitempty"`
	Path          string            `yaml:"path,omitempty"`
	Format        string            `yaml:"format,omitempty"`
	URL           string            `yaml:"url,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty"`
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
func (fc FilterConfig) MarshalYAML() (any, error) {
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
	Type      string               `yaml:"type"` // stub, sql, kafka, elasticsearch, mongodb, s3
	LogLevel  string               `yaml:"log_level,omitempty"`
	LogFormat string               `yaml:"log_format,omitempty"`
	Metrics   *MetricsOutputConfig `yaml:"metrics,omitempty"`
	Callback  *CallbackConfig      `yaml:"callback,omitempty"`

	// 범용 설정 맵 (elasticsearch, mongodb, s3 등에서 사용)
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`

	// SQL Sink
	Driver          string            `yaml:"driver,omitempty"`           // mysql, postgres
	DSN             string            `yaml:"dsn,omitempty"`              // database connection string
	Table           string            `yaml:"table,omitempty"`            // target table name
	Columns         []string          `yaml:"columns,omitempty"`          // columns to insert (optional, auto-detect if empty)
	ColumnMap       map[string]string `yaml:"column_map,omitempty"`       // source field -> db column mapping
	BatchSize       int               `yaml:"batch_size,omitempty"`       // batch insert size (default: 100)
	OnConflict      string            `yaml:"on_conflict,omitempty"`      // ignore, update, error (default: error)
	ConflictColumns []string          `yaml:"conflict_columns,omitempty"` // columns to check for conflict (upsert key)
	CreateTable     string            `yaml:"create_table,omitempty"`     // CREATE TABLE SQL to execute on open

	// Kafka Sink
	Brokers []string `yaml:"brokers,omitempty"` // Kafka broker addresses
	Topic   string   `yaml:"topic,omitempty"`   // Kafka topic name

	// REST API Sink
	URL            string            `yaml:"url,omitempty"`             // Target URL
	Method         string            `yaml:"method,omitempty"`          // HTTP method (POST, PUT, PATCH)
	Headers        map[string]string `yaml:"headers,omitempty"`         // HTTP headers
	Timeout        string            `yaml:"timeout,omitempty"`         // Request timeout (default: 30s)
	RetryCount     int               `yaml:"retry_count,omitempty"`     // Number of retries (default: 3)
	RetryDelay     string            `yaml:"retry_delay,omitempty"`     // Delay between retries (default: 1s)
	SuccessCodes   []int             `yaml:"success_codes,omitempty"`   // HTTP status codes considered success (default: 200, 201, 202, 204)
	BatchEnabled   bool              `yaml:"batch_enabled,omitempty"`   // Enable batch mode
	BatchSizeHTTP  int               `yaml:"batch_size_http,omitempty"` // Batch size for HTTP (default: 1)
	BatchDelimiter string            `yaml:"batch_delimiter,omitempty"` // Delimiter for batch (default: newline)
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

	// 하위 호환성: Source가 설정되어 있으면 Input으로 복사
	if c.Source != nil && c.Input.Type != "" && c.Input.Type == "" {
		c.Input = *c.Source
	}

	if c.Mode == "" {
		c.Mode = ModeBatch // 기본값
	}

	if c.Mode != ModeBatch && c.Mode != ModeRealtime {
		return fmt.Errorf("invalid pipeline mode: %s (must be 'batch' or 'realtime')", c.Mode)
	}

	// 입력 검증
	if err := c.validateInput(); err != nil {
		return fmt.Errorf("input: %w", err)
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

func (c *PipelineConfigV2) validateInput() error {
	switch c.Input.Type {
	case "file":
		if c.Input.Path == "" && len(c.Input.Paths) == 0 {
			return fmt.Errorf("file path is required")
		}
		if c.Input.Format == "" {
			c.Input.Format = "json"
		}

	case "sql":
		if c.Input.Driver == "" {
			return fmt.Errorf("sql driver is required")
		}
		if c.Input.DSN == "" {
			return fmt.Errorf("sql dsn is required")
		}
		if c.Input.Query == "" {
			return fmt.Errorf("sql query is required")
		}

	case "http", "rest_api":
		if c.Input.URL == "" {
			return fmt.Errorf("http url is required")
		}
		if c.Input.Method == "" {
			c.Input.Method = "GET"
		}
		if c.Input.Auth != nil {
			if err := c.validateAuth(); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}

	case "kafka":
		if len(c.Input.Brokers) == 0 {
			return fmt.Errorf("kafka brokers are required")
		}
		if len(c.Input.Topics) == 0 {
			return fmt.Errorf("kafka topics are required")
		}
		if c.Input.GroupID == "" {
			c.Input.GroupID = c.Name + "-consumer"
		}

	case "sql_event":
		if c.Input.Driver == "" {
			return fmt.Errorf("sql_event driver is required")
		}
		if c.Input.DSN == "" {
			return fmt.Errorf("sql_event dsn is required")
		}
		if c.Input.Table == "" {
			return fmt.Errorf("sql_event table is required")
		}
		if c.Input.IDColumn == "" {
			c.Input.IDColumn = "id"
		}
		if c.Input.BatchSize <= 0 {
			c.Input.BatchSize = 1000
		}
		if c.Input.PollInterval <= 0 {
			c.Input.PollInterval = 1000
		}

	case "cdc":
		if c.Input.Driver == "" {
			return fmt.Errorf("cdc driver is required (mysql or postgres)")
		}
		if c.Input.Host == "" {
			return fmt.Errorf("cdc host is required")
		}
		if c.Input.Username == "" {
			return fmt.Errorf("cdc username is required")
		}
		if c.Input.Port <= 0 {
			switch c.Input.Driver {
			case "mysql":
				c.Input.Port = 3306
			case "postgres":
				c.Input.Port = 5432
			}
		}
		if c.Input.ServerID == 0 {
			c.Input.ServerID = 101
		}

	case "kubernetes", "k8s_logs":
		// pod_selector 또는 pod_names 중 하나는 필수
		if c.Input.K8sPodSelector == "" && len(c.Input.K8sPodNames) == 0 {
			return fmt.Errorf("kubernetes source requires pod_selector or pod_names")
		}
		// 기본값 설정
		if c.Input.K8sTailLines == 0 && c.Input.K8sSinceSeconds == 0 && !c.Input.K8sFollow {
			c.Input.K8sTailLines = 100 // 기본: 최근 100줄
		}

	case "partitioned_http":
		if err := c.validatePartitionedHTTP(); err != nil {
			return err
		}

	case "partitioned_sql":
		if err := c.validatePartitionedSQL(); err != nil {
			return err
		}

	case "rabbitmq":
		if c.Input.URL == "" {
			return fmt.Errorf("rabbitmq url is required")
		}
		if c.Input.Queue == "" {
			return fmt.Errorf("rabbitmq queue is required")
		}
		if c.Input.Prefetch <= 0 {
			c.Input.Prefetch = 10
		}

	case "sqs":
		if c.Input.SQSQueueURL == "" {
			return fmt.Errorf("sqs queue_url is required")
		}
		if c.Input.SQSMaxMessages <= 0 || c.Input.SQSMaxMessages > 10 {
			c.Input.SQSMaxMessages = 10
		}
		if c.Input.SQSWaitTimeSeconds < 0 || c.Input.SQSWaitTimeSeconds > 20 {
			c.Input.SQSWaitTimeSeconds = 20
		}
		if c.Input.SQSVisibilityTimeout <= 0 {
			c.Input.SQSVisibilityTimeout = 30
		}

	case "pubsub":
		if c.Input.PubSubProjectID == "" {
			return fmt.Errorf("pubsub project_id is required")
		}
		if c.Input.PubSubSubscription == "" {
			return fmt.Errorf("pubsub subscription is required")
		}
		if c.Input.PubSubMaxOutstandingMessages <= 0 {
			c.Input.PubSubMaxOutstandingMessages = 1000
		}
		if c.Input.PubSubNumGoroutines <= 0 {
			c.Input.PubSubNumGoroutines = 10
		}

	case "websocket":
		if c.Input.WSURL == "" {
			return fmt.Errorf("websocket url is required")
		}
		if c.Input.WSPingInterval <= 0 {
			c.Input.WSPingInterval = 30000 // 30 seconds
		}
		if c.Input.WSPongWait <= 0 {
			c.Input.WSPongWait = 60000 // 60 seconds
		}
		if c.Input.WSReconnectWait <= 0 {
			c.Input.WSReconnectWait = 5000 // 5 seconds
		}
		if c.Input.WSMaxReconnect <= 0 {
			c.Input.WSMaxReconnect = 10
		}

	case "mqtt":
		if c.Input.MQTTBroker == "" {
			return fmt.Errorf("mqtt broker is required")
		}
		if c.Input.MQTTTopic == "" {
			return fmt.Errorf("mqtt topic is required")
		}
		if c.Input.MQTTClientID == "" {
			c.Input.MQTTClientID = c.Name + "-mqtt-client"
		}
		if c.Input.MQTTQoS < 0 || c.Input.MQTTQoS > 2 {
			c.Input.MQTTQoS = 1
		}
		if c.Input.MQTTKeepAlive <= 0 {
			c.Input.MQTTKeepAlive = 60
		}
		if c.Input.MQTTReconnectWait <= 0 {
			c.Input.MQTTReconnectWait = 5000
		}
		if c.Input.MQTTMaxReconnect <= 0 {
			c.Input.MQTTMaxReconnect = 10
		}

	case "sse":
		if c.Input.SSEURL == "" {
			return fmt.Errorf("sse url is required")
		}
		if c.Input.SSEReconnectWait <= 0 {
			c.Input.SSEReconnectWait = 3000
		}
		if c.Input.SSEMaxReconnect <= 0 {
			c.Input.SSEMaxReconnect = 10
		}

	default:
		return fmt.Errorf("unsupported source type: %s", c.Input.Type)
	}

	// http/sql + partition 설정 검증
	if c.Input.Partition != nil {
		if err := c.validatePartitionConfig(); err != nil {
			return fmt.Errorf("partition: %w", err)
		}
	}

	return nil
}

func (c *PipelineConfigV2) validateAuth() error {
	auth := c.Input.Auth
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
		if auth.ClientID == "" {
			return fmt.Errorf("oauth2 requires client_id")
		}
		if auth.TokenURL == "" {
			return fmt.Errorf("oauth2 requires token_url")
		}
		// grant_type에 따른 추가 검증
		grantType := auth.GrantType
		if grantType == "" {
			grantType = "client_credentials"
		}
		switch grantType {
		case "client_credentials":
			if auth.ClientSecret == "" {
				return fmt.Errorf("oauth2 client_credentials requires client_secret")
			}
		case "refresh_token":
			if auth.RefreshToken == "" {
				return fmt.Errorf("oauth2 refresh_token grant requires refresh_token")
			}
		case "authorization_code":
			if !auth.UsePKCE && auth.ClientSecret == "" {
				return fmt.Errorf("oauth2 authorization_code requires client_secret or PKCE")
			}
			if auth.AuthURL == "" {
				return fmt.Errorf("oauth2 authorization_code requires auth_url")
			}
		}
		// PKCE 검증
		if auth.UsePKCE {
			if auth.PKCEChallengeMethod != "" && auth.PKCEChallengeMethod != "S256" && auth.PKCEChallengeMethod != "plain" {
				return fmt.Errorf("pkce_challenge_method must be 'S256' or 'plain'")
			}
		}
	case "api_key":
		if auth.APIKey == "" {
			return fmt.Errorf("api_key auth requires api_key")
		}
	case "mtls":
		if auth.TLS == nil || !auth.TLS.Enabled {
			return fmt.Errorf("mtls auth requires tls configuration")
		}
		if auth.TLS.ClientCert == "" || auth.TLS.ClientKey == "" {
			return fmt.Errorf("mtls auth requires client_cert and client_key")
		}
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
	return nil
}

// validatePartitionedHTTP partitioned_http 소스 검증
func (c *PipelineConfigV2) validatePartitionedHTTP() error {
	if c.Input.Partition == nil {
		return fmt.Errorf("partition config is required for partitioned_http source")
	}
	p := c.Input.Partition

	// 디스커버리 또는 정적 파티션 필수
	if p.DiscoveryURL == "" && len(p.StaticPartitions) == 0 {
		return fmt.Errorf("either discovery_url or static_partitions is required")
	}

	// URL 템플릿 또는 기본 URL 필수
	if p.URLTemplate == "" && c.Input.URL == "" {
		return fmt.Errorf("url_template or base url is required")
	}

	// 기본값 설정
	if p.Parallelism <= 0 {
		p.Parallelism = 4
	}
	if p.DiscoveryMethod == "" {
		p.DiscoveryMethod = "GET"
	}

	return nil
}

// validatePartitionedSQL partitioned_sql 소스 검증
func (c *PipelineConfigV2) validatePartitionedSQL() error {
	if c.Input.Driver == "" {
		return fmt.Errorf("sql driver is required")
	}
	if c.Input.DSN == "" {
		return fmt.Errorf("sql dsn is required")
	}
	if c.Input.Partition == nil {
		return fmt.Errorf("partition config is required for partitioned_sql source")
	}
	p := c.Input.Partition

	// 디스커버리 또는 정적 파티션 필수
	if p.DiscoveryQuery == "" && len(p.StaticPartitions) == 0 {
		return fmt.Errorf("either discovery_query or static_partitions is required")
	}

	// 쿼리 템플릿 또는 기본 쿼리 필수
	if p.QueryTemplate == "" && c.Input.Query == "" {
		return fmt.Errorf("query_template or base query is required")
	}

	// 기본값 설정
	if p.Parallelism <= 0 {
		p.Parallelism = 4
	}

	return nil
}

// validatePartitionConfig 공통 파티션 설정 검증 (http/sql + partition)
func (c *PipelineConfigV2) validatePartitionConfig() error {
	p := c.Input.Partition
	if p == nil {
		return nil
	}

	// 기본값 설정
	if p.Parallelism <= 0 {
		p.Parallelism = 4
	}

	switch c.Input.Type {
	case "http", "rest_api":
		if p.DiscoveryURL == "" && len(p.StaticPartitions) == 0 {
			return fmt.Errorf("either discovery_url or static_partitions is required for HTTP partition")
		}
		if p.URLTemplate == "" && c.Input.URL == "" {
			return fmt.Errorf("url_template or base url is required for HTTP partition")
		}
		if p.DiscoveryMethod == "" {
			p.DiscoveryMethod = "GET"
		}

	case "sql":
		if p.DiscoveryQuery == "" && len(p.StaticPartitions) == 0 {
			return fmt.Errorf("either discovery_query or static_partitions is required for SQL partition")
		}
		if p.QueryTemplate == "" && c.Input.Query == "" {
			return fmt.Errorf("query_template or base query is required for SQL partition")
		}
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
