// Package sink 데이터 출력 구현
package sink

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

// Sink 데이터 출력 인터페이스
type Sink interface {
	Open(ctx context.Context) error
	Write(ctx context.Context, record source.Record) error
	Flush(ctx context.Context) error
	Close() error
	Name() string
	Stats() SinkStats
}

// SinkStats 출력 통계
type SinkStats struct {
	TotalRecords   int64
	SuccessRecords int64
	ErrorRecords   int64
	LastWriteTime  time.Time
}

// StubSink Stub 출력 (로깅 + 메트릭만)
type StubSink struct {
	logLevel  string
	logFormat string
	metrics   bool
	callback  *CallbackHandler

	stats SinkStats
}

// NewStubSink Stub 싱크 생성
func NewStubSink(cfg config.OutputConfig) (*StubSink, error) {
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

	return &StubSink{
		logLevel:  logLevel,
		logFormat: logFormat,
		metrics:   metricsEnabled,
		callback:  callback,
	}, nil
}

func (s *StubSink) Name() string {
	return "stub"
}

func (s *StubSink) Open(ctx context.Context) error {
	log.Printf("[stub] Sink opened (log_level=%s, format=%s, metrics=%v)",
		s.logLevel, s.logFormat, s.metrics)
	return nil
}

func (s *StubSink) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&s.stats.TotalRecords, 1)

	// 로깅
	if s.logLevel != "none" {
		s.logRecord(record)
	}

	// 콜백 (비동기)
	if s.callback != nil {
		go s.callback.Send(record)
	}

	atomic.AddInt64(&s.stats.SuccessRecords, 1)
	s.stats.LastWriteTime = time.Now()

	return nil
}

func (s *StubSink) logRecord(record source.Record) {
	switch s.logFormat {
	case "json":
		output := map[string]any{
			"data":     record.Data,
			"metadata": record.Metadata,
		}
		jsonBytes, _ := json.Marshal(output)
		log.Printf("[stub] %s", string(jsonBytes))

	case "pretty":
		jsonBytes, _ := json.MarshalIndent(record.Data, "", "  ")
		log.Printf("[stub] Record from %s:\n%s", record.Metadata.Origin, string(jsonBytes))

	default: // simple
		log.Printf("[stub] Record: source=%s, origin=%s, fields=%d",
			record.Metadata.Source, record.Metadata.Origin, len(record.Data))
	}
}

func (s *StubSink) Flush(ctx context.Context) error {
	return nil
}

func (s *StubSink) Close() error {
	log.Printf("[stub] Sink closed. Total: %d, Success: %d, Errors: %d",
		s.stats.TotalRecords, s.stats.SuccessRecords, s.stats.ErrorRecords)
	return nil
}

func (s *StubSink) Stats() SinkStats {
	return s.stats
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

// NewSink 설정에서 싱크 생성
func NewSink(cfg config.OutputConfig) (Sink, error) {
	switch cfg.Type {
	case "stub", "":
		return NewStubSink(cfg)
	default:
		return nil, fmt.Errorf("unsupported sink type: %s", cfg.Type)
	}
}
