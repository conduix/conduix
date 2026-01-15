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

// KafkaSink Kafka 출력
type KafkaSink struct {
	brokers []string
	topic   string
	name    string

	stats SinkStats
}

// NewKafkaSink Kafka 싱크 생성
func NewKafkaSink(cfg config.OutputConfig) (*KafkaSink, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers not configured")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("kafka topic not configured")
	}

	return &KafkaSink{
		brokers: cfg.Brokers,
		topic:   cfg.Topic,
		name:    "kafka",
	}, nil
}

func (s *KafkaSink) Name() string {
	return s.name
}

func (s *KafkaSink) Open(ctx context.Context) error {
	log.Printf("[kafka] Sink opened (brokers=%v, topic=%s)", s.brokers, s.topic)
	// TODO: Initialize Kafka producer
	return nil
}

func (s *KafkaSink) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&s.stats.TotalRecords, 1)

	// TODO: Send to Kafka
	// For now, just log
	jsonBytes, _ := json.Marshal(record.Data)
	log.Printf("[kafka] Writing to topic %s: %s", s.topic, string(jsonBytes))

	atomic.AddInt64(&s.stats.SuccessRecords, 1)
	s.stats.LastWriteTime = time.Now()

	return nil
}

func (s *KafkaSink) Flush(ctx context.Context) error {
	// TODO: Flush Kafka producer
	return nil
}

func (s *KafkaSink) Close() error {
	log.Printf("[kafka] Sink closed. Total: %d, Success: %d, Errors: %d",
		s.stats.TotalRecords, s.stats.SuccessRecords, s.stats.ErrorRecords)
	// TODO: Close Kafka producer
	return nil
}

func (s *KafkaSink) Stats() SinkStats {
	return s.stats
}
