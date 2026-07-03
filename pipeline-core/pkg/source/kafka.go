package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

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

	// 보안 설정
	saslMechanism sasl.Mechanism
	tlsConfig     *tls.Config
	dialer        *kafka.Dialer

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

	source := &KafkaSource{
		brokers:        cfg.Brokers,
		topics:         cfg.Topics,
		groupID:        cfg.GroupID,
		startOffset:    startOffset,
		minBytes:       minBytes,
		maxBytes:       maxBytes,
		maxWait:        maxWait,
		commitInterval: commitInterval,
		checkpoints:    make(map[string]int64),
	}

	// SASL 인증 설정
	if cfg.SASL != nil {
		mechanism, err := buildSASLMechanism(cfg.SASL)
		if err != nil {
			return nil, fmt.Errorf("failed to configure SASL: %w", err)
		}
		source.saslMechanism = mechanism
	}

	// TLS 설정
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg, err := buildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
		source.tlsConfig = tlsCfg
	}

	// Dialer 생성 (SASL 또는 TLS가 있는 경우)
	if source.saslMechanism != nil || source.tlsConfig != nil {
		source.dialer = &kafka.Dialer{
			Timeout:       10 * time.Second,
			DualStack:     true,
			SASLMechanism: source.saslMechanism,
			TLS:           source.tlsConfig,
		}
	}

	return source, nil
}

// buildSASLMechanism SASL 메커니즘 생성
func buildSASLMechanism(cfg *config.SASLConfig) (sasl.Mechanism, error) {
	if cfg == nil {
		return nil, nil
	}

	// 환경변수 치환
	username := expandEnvVars(cfg.Username)
	password := expandEnvVars(cfg.Password)

	switch strings.ToUpper(cfg.Mechanism) {
	case "PLAIN":
		return plain.Mechanism{
			Username: username,
			Password: password,
		}, nil

	case "SCRAM-SHA-256":
		mechanism, err := scram.Mechanism(scram.SHA256, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-256 mechanism: %w", err)
		}
		return mechanism, nil

	case "SCRAM-SHA-512":
		mechanism, err := scram.Mechanism(scram.SHA512, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-512 mechanism: %w", err)
		}
		return mechanism, nil

	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s (supported: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", cfg.Mechanism)
	}
}

// buildTLSConfig TLS 설정 생성
func buildTLSConfig(cfg *config.TLSClientConfig) (*tls.Config, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.SkipVerify,
	}

	// 서버 이름 (SNI)
	if cfg.ServerName != "" {
		tlsCfg.ServerName = cfg.ServerName
	}

	// CA 인증서
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(expandEnvVars(cfg.CACert))
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsCfg.RootCAs = caCertPool
	}

	// 클라이언트 인증서 (mTLS)
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(
			expandEnvVars(cfg.ClientCert),
			expandEnvVars(cfg.ClientKey),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// expandEnvVars 환경변수 치환 (${VAR} 형식)
func expandEnvVars(s string) string {
	if strings.Contains(s, "${") {
		return os.ExpandEnv(s)
	}
	return s
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

		// SASL/TLS가 설정된 경우 Dialer 사용
		if s.dialer != nil {
			readerCfg.Dialer = s.dialer
		}

		reader := kafka.NewReader(readerCfg)
		s.readers = append(s.readers, reader)
	}

	// 보안 설정 로깅 (비밀번호 마스킹)
	if s.saslMechanism != nil || s.tlsConfig != nil {
		authInfo := []string{}
		if s.saslMechanism != nil {
			authInfo = append(authInfo, "SASL enabled")
		}
		if s.tlsConfig != nil {
			authInfo = append(authInfo, "TLS enabled")
			if len(s.tlsConfig.Certificates) > 0 {
				authInfo = append(authInfo, "mTLS enabled")
			}
		}
		slog.Default().Info("Kafka security", "brokers", s.brokers, "security", strings.Join(authInfo, ", "))
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
	maps.Copy(result, s.checkpoints)
	return result
}

// SetCheckpointsLegacy 체크포인트 설정 (복구용) - 레거시 API
func (s *KafkaSource) SetCheckpointsLegacy(checkpoints map[string]int64) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()

	maps.Copy(s.checkpoints, checkpoints)
}

// SourceType 소스 타입 반환 (CheckpointableSource 구현)
func (s *KafkaSource) SourceType() string {
	return "kafka"
}

// GetSourceCheckpoints 현재 모든 체크포인트 반환 (CheckpointableSource 구현)
func (s *KafkaSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	checkpoints := make([]*SourceCheckpoint, 0, len(s.checkpoints))
	for key, offset := range s.checkpoints {
		checkpoints = append(checkpoints, &SourceCheckpoint{
			PartitionKey: key, // format: "topic-partition"
			OffsetValue:  fmt.Sprintf("%d", offset),
			OffsetType:   "numeric",
			RecordCount:  offset, // Kafka에서는 offset이 대략적인 레코드 수
			UpdatedAt:    time.Now(),
		})
	}
	return checkpoints
}

// SetSourceCheckpoints 체크포인트 설정 (CheckpointableSource 구현)
func (s *KafkaSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()

	for _, cp := range checkpoints {
		if cp.OffsetType != "numeric" {
			continue // Kafka 소스는 숫자 오프셋만 지원
		}

		offset, err := ParseNumeric(cp.OffsetValue)
		if err != nil {
			slog.Default().Error("Kafka failed to parse checkpoint offset",
				"topics", s.topics, "offset_value", cp.OffsetValue, "error", err)
			continue
		}

		s.checkpoints[cp.PartitionKey] = offset
		slog.Default().Info("Kafka restored checkpoint",
			"topics", s.topics, "partition_key", cp.PartitionKey, "offset", offset)
	}

	return nil
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
