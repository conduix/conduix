package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/schema"
)

// BaseSink provides common sink functionality
type BaseSink struct {
	name         string
	typ          string
	config       map[string]any
	batchSize    int
	flushTimeout time.Duration

	buffer      []*Record
	bufferMu    sync.Mutex
	outputCount int64
	errorCount  int64
	statsMu     sync.Mutex

	flushTimer *time.Timer
	ctx        context.Context
	cancel     context.CancelFunc
}

func (s *BaseSink) Name() string { return s.name }
func (s *BaseSink) Type() string { return s.typ }

func (s *BaseSink) init() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if s.batchSize == 0 {
		s.batchSize = 1000
	}
	if s.flushTimeout == 0 {
		s.flushTimeout = 10 * time.Second
	}
	s.buffer = make([]*Record, 0, s.batchSize)
}

func (s *BaseSink) incrementOutput(n int64) {
	s.statsMu.Lock()
	s.outputCount += n
	s.statsMu.Unlock()
}

func (s *BaseSink) incrementError(n int64) {
	s.statsMu.Lock()
	s.errorCount += n
	s.statsMu.Unlock()
}

func (s *BaseSink) Stats() (output, errors int64) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.outputCount, s.errorCount
}

// ConsoleSink writes records to stdout
type ConsoleSink struct {
	BaseSink
	encoder *json.Encoder
}

func NewConsoleSink(name string, config map[string]any) *ConsoleSink {
	s := &ConsoleSink{
		BaseSink: BaseSink{
			name:         name,
			typ:          "console",
			config:       config,
			batchSize:    1, // Console writes immediately
			flushTimeout: time.Second,
		},
	}
	s.init()
	s.encoder = json.NewEncoder(os.Stdout)
	return s
}

func (s *ConsoleSink) Write(ctx context.Context, record *Record) error {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	if err := s.encoder.Encode(record.Data); err != nil {
		s.incrementError(1)
		return err
	}

	s.incrementOutput(1)
	return nil
}

func (s *ConsoleSink) Flush(ctx context.Context) error {
	// Console writes immediately, nothing to flush
	return nil
}

func (s *ConsoleSink) Close() error {
	return nil
}

// BufferedSink provides buffered writing with batch flushing
type BufferedSink struct {
	BaseSink
	writeFunc func(ctx context.Context, records []*Record) error
}

func (s *BufferedSink) Write(ctx context.Context, record *Record) error {
	s.bufferMu.Lock()
	s.buffer = append(s.buffer, record)
	shouldFlush := len(s.buffer) >= s.batchSize
	s.bufferMu.Unlock()

	if shouldFlush {
		return s.Flush(ctx)
	}

	// Reset flush timer
	if s.flushTimer != nil {
		s.flushTimer.Reset(s.flushTimeout)
	}

	return nil
}

func (s *BufferedSink) Flush(ctx context.Context) error {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return nil
	}

	// Swap buffer
	records := s.buffer
	s.buffer = make([]*Record, 0, s.batchSize)
	s.bufferMu.Unlock()

	// Write batch
	if err := s.writeFunc(ctx, records); err != nil {
		s.incrementError(int64(len(records)))
		return err
	}

	s.incrementOutput(int64(len(records)))
	return nil
}

func (s *BufferedSink) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.flushTimer != nil {
		s.flushTimer.Stop()
	}
	return s.Flush(context.Background())
}

func (s *BufferedSink) startFlushLoop() {
	s.flushTimer = time.NewTimer(s.flushTimeout)
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-s.flushTimer.C:
				_ = s.Flush(s.ctx)
				s.flushTimer.Reset(s.flushTimeout)
			}
		}
	}()
}

// ElasticsearchSink writes to Elasticsearch
type ElasticsearchSink struct {
	BufferedSink
	endpoints []string
	index     string
	client    *http.Client
}

func NewElasticsearchSink(name string, config map[string]any) *ElasticsearchSink {
	var endpoints []string
	if eps, ok := config["endpoints"].([]any); ok {
		for _, ep := range eps {
			if es, ok := ep.(string); ok {
				endpoints = append(endpoints, es)
			}
		}
	}

	index := "logs"
	if idx, ok := config["index"].(string); ok {
		index = idx
	}

	batchSize := 5000
	if buf, ok := config["buffer"].(map[string]any); ok {
		if me, ok := buf["max_events"].(int); ok {
			batchSize = me
		}
	}

	flushTimeout := 10 * time.Second
	if buf, ok := config["buffer"].(map[string]any); ok {
		if to, ok := buf["timeout"].(string); ok {
			if d, err := time.ParseDuration(to); err == nil {
				flushTimeout = d
			}
		}
	}

	s := &ElasticsearchSink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "elasticsearch",
				config:       config,
				batchSize:    batchSize,
				flushTimeout: flushTimeout,
			},
		},
		endpoints: endpoints,
		index:     index,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
	s.init()
	s.writeFunc = s.writeBatch
	s.startFlushLoop()
	return s
}

func (s *ElasticsearchSink) writeBatch(ctx context.Context, records []*Record) error {
	if len(s.endpoints) == 0 {
		return fmt.Errorf("no elasticsearch endpoints configured")
	}

	// Build bulk request
	var buf bytes.Buffer
	for _, record := range records {
		meta := map[string]any{"index": map[string]any{"_index": s.index}}
		metaJSON, _ := json.Marshal(meta)
		dataJSON, _ := json.Marshal(record.Data)
		buf.Write(metaJSON)
		buf.WriteByte('\n')
		buf.Write(dataJSON)
		buf.WriteByte('\n')
	}

	// TODO: Send to Elasticsearch
	// For now, just log
	fmt.Printf("[ES] Would send %d records to %s/%s\n", len(records), s.endpoints[0], s.index)

	return nil
}

// S3Sink writes to S3
type S3Sink struct {
	BufferedSink
	bucket string
	prefix string
}

func NewS3Sink(name string, config map[string]any) *S3Sink {
	bucket := ""
	if b, ok := config["bucket"].(string); ok {
		bucket = b
	}

	prefix := ""
	if p, ok := config["prefix"].(string); ok {
		prefix = p
	}

	batchSize := 10000
	if buf, ok := config["buffer"].(map[string]any); ok {
		if me, ok := buf["max_events"].(int); ok {
			batchSize = me
		}
	}

	s := &S3Sink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "s3",
				config:       config,
				batchSize:    batchSize,
				flushTimeout: 60 * time.Second,
			},
		},
		bucket: bucket,
		prefix: prefix,
	}
	s.init()
	s.writeFunc = s.writeBatch
	s.startFlushLoop()
	return s
}

func (s *S3Sink) writeBatch(ctx context.Context, records []*Record) error {
	// TODO: Implement actual S3 upload
	fmt.Printf("[S3] Would upload %d records to s3://%s/%s\n", len(records), s.bucket, s.prefix)
	return nil
}

// KafkaSink writes to Kafka
type KafkaSink struct {
	BufferedSink
	brokers []string
	topic   string
}

func NewKafkaSink(name string, config map[string]any) *KafkaSink {
	var brokers []string
	if b, ok := config["brokers"].([]any); ok {
		for _, broker := range b {
			if bs, ok := broker.(string); ok {
				brokers = append(brokers, bs)
			}
		}
	}

	topic := ""
	if t, ok := config["topic"].(string); ok {
		topic = t
	}

	s := &KafkaSink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "kafka",
				config:       config,
				batchSize:    1000,
				flushTimeout: 5 * time.Second,
			},
		},
		brokers: brokers,
		topic:   topic,
	}
	s.init()
	s.writeFunc = s.writeBatch
	s.startFlushLoop()
	return s
}

func (s *KafkaSink) writeBatch(ctx context.Context, records []*Record) error {
	// TODO: Implement actual Kafka producer
	fmt.Printf("[Kafka] Would send %d records to %s\n", len(records), s.topic)
	return nil
}

// FileSink writes to files
type FileSink struct {
	BufferedSink
	path string
	file *os.File
}

func NewFileSink(name string, config map[string]any) *FileSink {
	path := "/tmp/pipeline-output.json"
	if p, ok := config["path"].(string); ok {
		path = p
	}

	s := &FileSink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "file",
				config:       config,
				batchSize:    1000,
				flushTimeout: 10 * time.Second,
			},
		},
		path: path,
	}
	s.init()
	s.writeFunc = s.writeBatch
	s.startFlushLoop()
	return s
}

func (s *FileSink) writeBatch(ctx context.Context, records []*Record) error {
	// TODO: Implement actual file writing
	fmt.Printf("[File] Would write %d records to %s\n", len(records), s.path)
	return nil
}

func (s *FileSink) Close() error {
	if s.file != nil {
		s.file.Close()
	}
	return s.BufferedSink.Close()
}

// ValidatingSink wraps a sink with schema validation
// Used for output validation (before Writer)
type ValidatingSink struct {
	inner      Sink
	schema     *schema.DataSchema
	dropOnFail bool // true면 검증 실패 시 레코드 드롭, false면 에러 반환

	// Stats
	validCount   int64
	invalidCount int64
	statsMu      sync.Mutex
}

// NewValidatingSink creates a validating sink wrapper
func NewValidatingSink(inner Sink, s *schema.DataSchema, dropOnFail bool) *ValidatingSink {
	return &ValidatingSink{
		inner:      inner,
		schema:     s,
		dropOnFail: dropOnFail,
	}
}

func (s *ValidatingSink) Name() string { return s.inner.Name() }
func (s *ValidatingSink) Type() string { return "validating_" + s.inner.Type() }

// Write validates and then writes the record
func (s *ValidatingSink) Write(ctx context.Context, record *Record) error {
	if s.schema != nil {
		if err := s.schema.Validate(record.Data); err != nil {
			s.statsMu.Lock()
			s.invalidCount++
			s.statsMu.Unlock()

			if s.dropOnFail {
				// 드롭 모드: 검증 실패 레코드 무시
				return nil
			}
			return fmt.Errorf("output validation failed: %w", err)
		}
	}

	s.statsMu.Lock()
	s.validCount++
	s.statsMu.Unlock()

	return s.inner.Write(ctx, record)
}

// Flush flushes the underlying sink
func (s *ValidatingSink) Flush(ctx context.Context) error {
	return s.inner.Flush(ctx)
}

// Close closes the underlying sink
func (s *ValidatingSink) Close() error {
	return s.inner.Close()
}

// ValidationStats returns validation statistics
func (s *ValidatingSink) ValidationStats() (valid, invalid int64) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.validCount, s.invalidCount
}

// SetSchema sets or updates the schema
func (s *ValidatingSink) SetSchema(schema *schema.DataSchema) {
	s.schema = schema
}

// NewSink creates a sink from configuration
func NewSink(cfg SinkConfig) (Sink, error) {
	switch cfg.Type {
	case "console":
		return NewConsoleSink(cfg.Name, cfg.Config), nil
	case "elasticsearch":
		return NewElasticsearchSink(cfg.Name, cfg.Config), nil
	case "s3":
		return NewS3Sink(cfg.Name, cfg.Config), nil
	case "kafka":
		return NewKafkaSink(cfg.Name, cfg.Config), nil
	case "file":
		return NewFileSink(cfg.Name, cfg.Config), nil
	default:
		return nil, fmt.Errorf("unknown sink type: %s", cfg.Type)
	}
}

// NewSinkWithValidation creates a sink with optional output schema validation
func NewSinkWithValidation(cfg SinkConfig, outputSchema *schema.DataSchema, dropOnFail bool) (Sink, error) {
	sink, err := NewSink(cfg)
	if err != nil {
		return nil, err
	}

	if outputSchema != nil {
		return NewValidatingSink(sink, outputSchema, dropOnFail), nil
	}

	return sink, nil
}
