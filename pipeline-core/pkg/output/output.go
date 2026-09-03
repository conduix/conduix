// Package output 데이터 출력 구현
package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// Output 데이터 출력 인터페이스
type Output interface {
	Open(ctx context.Context) error
	Write(ctx context.Context, record source.Record) error
	Flush(ctx context.Context) error
	Close() error
	Name() string
	Stats() OutputStats
}

// Finalizable 파이프라인 종료 시 성공 여부와 함께 호출되는 후처리를 가진 Output.
// executor 가 flush 후·close 전에 한 번 호출한다. sweep 처럼 "전체 성공 시에만
// 실행해야 하는" 마무리 작업용이며, 구현하지 않은 Output 은 아무 영향이 없다.
type Finalizable interface {
	Finalize(ctx context.Context, success bool) error
}

// BatchOutput 배치 쓰기를 지원하는 Output 인터페이스
type BatchOutput interface {
	Output
	// WriteBatch 여러 레코드를 한 번에 쓰기
	WriteBatch(ctx context.Context, records []source.Record) error
	// SupportsBatch 배치 쓰기 지원 여부
	SupportsBatch() bool
	// BatchConfig 배치 설정 반환
	BatchConfig() BatchConfig
}

// BatchConfig 배치 처리 설정
type BatchConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	Size          int           `yaml:"size" json:"size"`                     // 배치 크기 (기본: 100)
	FlushInterval time.Duration `yaml:"flush_interval" json:"flush_interval"` // 플러시 주기 (기본: 5s)
	Format        string        `yaml:"format" json:"format"`                 // ndjson, array (REST API용)
}

// DefaultBatchConfig 기본 배치 설정
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       false,
		Size:          100,
		FlushInterval: 5 * time.Second,
		Format:        "ndjson",
	}
}

// OutputStats 출력 통계
type OutputStats struct {
	TotalRecords   int64
	SuccessRecords int64
	ErrorRecords   int64
	LastWriteTime  time.Time
	BatchCount     int64 // 배치 처리 횟수
}

// StubOutput Stub 출력 (로깅 + 메트릭만)
type StubOutput struct {
	logLevel  string
	logFormat string
	metrics   bool
	callback  *CallbackHandler

	stats OutputStats
}

// NewStubOutput Stub 출력 생성
func NewStubOutput(cfg config.OutputConfig) (*StubOutput, error) {
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	logFormat := cfg.LogFormat
	if logFormat == "" {
		logFormat = "json"
	}

	var callback *CallbackHandler
	if cfg.Callback != nil && cfg.Callback.Enabled {
		callback = &CallbackHandler{
			url: cfg.Callback.URL,
		}
	}

	metricsEnabled := cfg.Metrics != nil && cfg.Metrics.Enabled

	return &StubOutput{
		logLevel:  logLevel,
		logFormat: logFormat,
		metrics:   metricsEnabled,
		callback:  callback,
	}, nil
}

func (o *StubOutput) Name() string {
	return "stub"
}

func (o *StubOutput) Open(ctx context.Context) error {
	log.Printf("[stub] Output opened (log_level=%s, format=%s, metrics=%v)",
		o.logLevel, o.logFormat, o.metrics)
	return nil
}

func (o *StubOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	// 로깅
	if o.logLevel != "none" {
		o.logRecord(record)
	}

	// 콜백 (비동기)
	if o.callback != nil {
		go o.callback.Send(record)
	}

	atomic.AddInt64(&o.stats.SuccessRecords, 1)
	o.stats.LastWriteTime = time.Now()

	return nil
}

func (o *StubOutput) logRecord(record source.Record) {
	switch o.logFormat {
	case "json":
		out := map[string]any{
			"data":     record.Data,
			"metadata": record.Metadata,
		}
		jsonBytes, _ := json.Marshal(out)
		log.Printf("[stub] %s", string(jsonBytes))

	case "pretty":
		jsonBytes, _ := json.MarshalIndent(record.Data, "", "  ")
		log.Printf("[stub] Record from %s:\n%s", record.Metadata.Origin, string(jsonBytes))

	default: // simple
		log.Printf("[stub] Record: source=%s, origin=%s, fields=%d",
			record.Metadata.Source, record.Metadata.Origin, len(record.Data))
	}
}

func (o *StubOutput) Flush(ctx context.Context) error {
	return nil
}

func (o *StubOutput) Close() error {
	log.Printf("[stub] Output closed. Total: %d, Success: %d, Errors: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords)
	return nil
}

func (o *StubOutput) Stats() OutputStats {
	return o.stats
}

// CallbackHandler 콜백 핸들러
type CallbackHandler struct {
	url string
}

func (h *CallbackHandler) Send(record source.Record) {
	// TODO: HTTP POST로 콜백 전송
	// payload, _ := json.Marshal(record)
	// http.Post(h.url, "application/json", bytes.NewReader(payload))
	_ = fmt.Sprintf("callback to %s", h.url) // placeholder
}

// NewOutput 설정에서 출력 생성
func NewOutput(cfg config.OutputConfig) (Output, error) {
	switch cfg.Type {
	case "stub", "":
		return NewStubOutput(cfg)
	case "sql":
		return NewSQLOutput(cfg)
	case "kafka":
		return NewKafkaOutput(cfg)
	case "rest_api", "http":
		return NewRestAPIOutput(cfg)
	case "elasticsearch", "es":
		return NewElasticsearchOutput(cfg)
	case "mongodb", "mongo":
		return NewMongoDBOutput(cfg)
	case "s3":
		return NewS3Output(cfg)
	case "gcs":
		return NewGCSOutput(cfg)
	case "bigquery", "bq":
		return NewBigQueryOutput(cfg)
	default:
		return nil, fmt.Errorf("unsupported output type: %s", cfg.Type)
	}
}
