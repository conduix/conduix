package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// KafkaOutput Kafka 출력
type KafkaOutput struct {
	brokers      []string
	topic        string
	name         string
	batchConfig  BatchConfig
	keyPartition bool   // true 면 메시지 key 해시로 파티션 결정(같은 key=같은 파티션, 셔플용)
	keyField     string // key 로 쓸 필드(비면 _key/id 폴백)

	writer *kafka.Writer
	stats  OutputStats
}

// NewKafkaOutput Kafka 출력 생성
func NewKafkaOutput(cfg config.OutputConfig) (*KafkaOutput, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers not configured")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("kafka topic not configured")
	}

	// 배치 설정
	batchConfig := DefaultBatchConfig()
	if cfg.BatchEnabled {
		batchConfig.Enabled = true
		if cfg.BatchSize > 0 {
			batchConfig.Size = cfg.BatchSize
		}
	}

	return &KafkaOutput{
		brokers:      cfg.Brokers,
		topic:        cfg.Topic,
		name:         "kafka",
		batchConfig:  batchConfig,
		keyPartition: cfg.Partitioning == "key",
		keyField:     cfg.KeyField,
	}, nil
}

func (o *KafkaOutput) Name() string {
	return o.name
}

func (o *KafkaOutput) Open(ctx context.Context) error {
	// kafka-go Writer 설정 (내장 배칭 기능 활용)
	batchSize := 100
	batchTimeout := 10 * time.Millisecond
	if o.batchConfig.Enabled {
		batchSize = o.batchConfig.Size
		batchTimeout = 50 * time.Millisecond // 배치 모드에서는 더 길게 대기
	}

	// Balancer: 기본은 크기 기반 로드밸런싱(LeastBytes). partitioning=key 면 Hash 로 바꿔
	// "같은 메시지 key = 같은 파티션" 을 보장한다 → Kafka 를 셔플 매체로 쓰는 분산 GROUP BY/JOIN 성립.
	var balancer kafka.Balancer = &kafka.LeastBytes{}
	if o.keyPartition {
		balancer = &kafka.Hash{}
	}
	o.writer = &kafka.Writer{
		Addr:         kafka.TCP(o.brokers...),
		Topic:        o.topic,
		Balancer:     balancer,
		BatchSize:    batchSize,
		BatchTimeout: batchTimeout,
		Async:        false, // 동기 모드로 에러 추적
		RequiredAcks: kafka.RequireOne,
	}

	log.Printf("[kafka] Output opened (brokers=%v, topic=%s, batch_size=%d, batch_enabled=%v)",
		o.brokers, o.topic, batchSize, o.batchConfig.Enabled)
	return nil
}

func (o *KafkaOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	value, err := json.Marshal(record.Data)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// 레코드 키 추출 (Data에서 "id" 또는 "_key" 필드 사용)
	key := o.recordKey(record.Data)

	msg := kafka.Message{
		Key:   key,
		Value: value,
	}

	if err := o.writer.WriteMessages(ctx, msg); err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to write to kafka: %w", err)
	}

	atomic.AddInt64(&o.stats.SuccessRecords, 1)
	o.stats.LastWriteTime = time.Now()
	return nil
}

// recordKey 는 sink 설정(keyField)을 반영해 메시지 key 를 뽑는다. keyField 가 지정되면 그 필드를,
// 아니면 _key/id 순으로 폴백. key 파티셔닝(shuffle) 시 이 key 가 파티션을 결정한다.
func (o *KafkaOutput) recordKey(data map[string]any) []byte {
	if o.keyField != "" {
		if v, ok := data[o.keyField]; ok {
			return []byte(fmt.Sprintf("%v", v))
		}
	}
	return extractRecordKey(data)
}

// extractRecordKey Data에서 Kafka 메시지 키 추출(_key/id 폴백)
func extractRecordKey(data map[string]any) []byte {
	// _key 필드가 있으면 우선 사용
	if key, ok := data["_key"]; ok {
		return []byte(fmt.Sprintf("%v", key))
	}
	// id 필드가 있으면 사용
	if id, ok := data["id"]; ok {
		return []byte(fmt.Sprintf("%v", id))
	}
	return nil
}

func (o *KafkaOutput) Flush(ctx context.Context) error {
	// kafka-go Writer는 내부적으로 배칭하므로 별도 flush 불필요
	return nil
}

func (o *KafkaOutput) Close() error {
	log.Printf("[kafka] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)

	if o.writer != nil {
		return o.writer.Close()
	}
	return nil
}

func (o *KafkaOutput) Stats() OutputStats {
	return o.stats
}

// SupportsBatch 배치 쓰기 지원 여부
func (o *KafkaOutput) SupportsBatch() bool {
	return o.batchConfig.Enabled
}

// BatchConfig 배치 설정 반환
func (o *KafkaOutput) BatchConfig() BatchConfig {
	return o.batchConfig
}

// WriteBatch 여러 메시지를 한 번에 전송
func (o *KafkaOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	messages := make([]kafka.Message, 0, len(records))
	for _, record := range records {
		value, err := json.Marshal(record.Data)
		if err != nil {
			atomic.AddInt64(&o.stats.ErrorRecords, 1)
			continue
		}

		// Data에서 키 추출
		key := o.recordKey(record.Data)

		messages = append(messages, kafka.Message{
			Key:   key,
			Value: value,
		})
	}

	if err := o.writer.WriteMessages(ctx, messages...); err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, int64(len(messages)))
		return fmt.Errorf("failed to write batch to kafka: %w", err)
	}

	atomic.AddInt64(&o.stats.SuccessRecords, int64(len(messages)))
	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	log.Printf("[kafka] Batch sent successfully (messages=%d)", len(messages))
	return nil
}
