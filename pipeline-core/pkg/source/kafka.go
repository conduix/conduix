package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// KafkaSource Kafka 데이터 소스
type KafkaSource struct {
	brokers        []string
	topics         []string
	groupID        string
	startOffset    int64
	minBytes       int
	maxBytes       int
	maxWait        time.Duration
	commitInterval time.Duration

	readers []*kafka.Reader
	mu      sync.RWMutex

	// 체크포인트 (partition -> offset)
	checkpoints  map[string]int64
	checkpointMu sync.RWMutex
}

// NewKafkaSource Kafka 소스 생성
func NewKafkaSource(cfg config.SourceV2) (*KafkaSource, error) {
	startOffset := kafka.LastOffset // default: latest
	if cfg.StartOffset == "earliest" || cfg.StartOffset == "beginning" {
		startOffset = kafka.FirstOffset
	}

	minBytes := 1 // 1 byte
	if cfg.MinBytes > 0 {
		minBytes = cfg.MinBytes
	}

	maxBytes := 10 * 1024 * 1024 // 10MB default
	if cfg.MaxBytes > 0 {
		maxBytes = cfg.MaxBytes
	}

	maxWait := 500 * time.Millisecond
	if cfg.MaxWait > 0 {
		maxWait = time.Duration(cfg.MaxWait) * time.Millisecond
	}

	commitInterval := time.Second
	if cfg.CommitInterval > 0 {
		commitInterval = time.Duration(cfg.CommitInterval) * time.Millisecond
	}

	return &KafkaSource{
		brokers:        cfg.Brokers,
		topics:         cfg.Topics,
		groupID:        cfg.GroupID,
		startOffset:    startOffset,
		minBytes:       minBytes,
		maxBytes:       maxBytes,
		maxWait:        maxWait,
		commitInterval: commitInterval,
		checkpoints:    make(map[string]int64),
	}, nil
}

func (s *KafkaSource) Name() string {
	return "kafka"
}

func (s *KafkaSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 각 토픽에 대해 reader 생성
	for _, topic := range s.topics {
		readerCfg := kafka.ReaderConfig{
			Brokers:        s.brokers,
			Topic:          topic,
			MinBytes:       s.minBytes,
			MaxBytes:       s.maxBytes,
			MaxWait:        s.maxWait,
			StartOffset:    s.startOffset,
			CommitInterval: s.commitInterval,
		}

		// GroupID가 있으면 consumer group 모드
		if s.groupID != "" {
			readerCfg.GroupID = s.groupID
		}

		reader := kafka.NewReader(readerCfg)
		s.readers = append(s.readers, reader)
	}

	return nil
}

func (s *KafkaSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	var wg sync.WaitGroup

	s.mu.RLock()
	readers := s.readers
	s.mu.RUnlock()

	for _, reader := range readers {
		wg.Add(1)
		go func(r *kafka.Reader) {
			defer wg.Done()
			s.readFromReader(ctx, r, records, errs)
		}(reader)
	}

	go func() {
		wg.Wait()
		close(records)
		close(errs)
	}()

	return records, errs
}

func (s *KafkaSource) readFromReader(ctx context.Context, reader *kafka.Reader, records chan<- Record, errs chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			select {
			case errs <- fmt.Errorf("read message: %w", err):
			default:
			}
			return
		}

		// 체크포인트 업데이트
		s.updateCheckpoint(msg.Topic, msg.Partition, msg.Offset)

		// 데이터 파싱
		var data map[string]any
		if err := json.Unmarshal(msg.Value, &data); err != nil {
			// JSON이 아닌 경우 raw value로 처리
			data = map[string]any{
				"key":   string(msg.Key),
				"value": string(msg.Value),
			}
		}

		// 헤더 추가
		headers := make(map[string]string)
		for _, h := range msg.Headers {
			headers[h.Key] = string(h.Value)
		}
		if len(headers) > 0 {
			data["_headers"] = headers
		}

		// 키 추가
		if len(msg.Key) > 0 {
			data["_key"] = string(msg.Key)
		}

		record := Record{
			Data: data,
			Metadata: Metadata{
				Source:    "kafka",
				Origin:    msg.Topic,
				Offset:    fmt.Sprintf("%d:%d", msg.Partition, msg.Offset),
				Timestamp: msg.Time.UnixMilli(),
			},
		}

		select {
		case records <- record:
		case <-ctx.Done():
			return
		}
	}
}

func (s *KafkaSource) updateCheckpoint(topic string, partition int, offset int64) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	key := fmt.Sprintf("%s-%d", topic, partition)
	s.checkpoints[key] = offset
}

// GetCheckpoints 현재 체크포인트 반환
func (s *KafkaSource) GetCheckpoints() map[string]int64 {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	result := make(map[string]int64)
	for k, v := range s.checkpoints {
		result[k] = v
	}
	return result
}

// SetCheckpoints 체크포인트 설정 (복구용)
func (s *KafkaSource) SetCheckpoints(checkpoints map[string]int64) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()

	for k, v := range checkpoints {
		s.checkpoints[k] = v
	}
}

func (s *KafkaSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, reader := range s.readers {
		if err := reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	s.readers = nil

	if len(errs) > 0 {
		return fmt.Errorf("errors closing readers: %v", errs)
	}
	return nil
}

// Stats Kafka reader 통계 반환
func (s *KafkaSource) Stats() []kafka.ReaderStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats []kafka.ReaderStats
	for _, reader := range s.readers {
		stats = append(stats, reader.Stats())
	}
	return stats
}

// Lag 현재 lag 반환
func (s *KafkaSource) Lag() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalLag int64
	for _, reader := range s.readers {
		totalLag += reader.Stats().Lag
	}
	return totalLag
}

// CommitMessages 메시지 커밋 (consumer group 모드)
func (s *KafkaSource) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.readers) == 0 {
		return nil
	}

	// 첫 번째 reader로 커밋 (같은 consumer group)
	return s.readers[0].CommitMessages(ctx, msgs...)
}
