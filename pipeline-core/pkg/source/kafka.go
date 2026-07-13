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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// JSON 파싱 실패 시 처리 정책.
const (
	parseErrorRaw   = "raw"   // raw key/value 로 통과(기본, 하위호환)
	parseErrorDrop  = "drop"  // 레코드 폐기
	parseErrorError = "error" // 에러 발생시켜 소스 중단
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
	onParseError   string

	// 보안 설정
	saslMechanism sasl.Mechanism
	tlsConfig     *tls.Config
	dialer        *kafka.Dialer

	readers []*kafka.Reader
	mu      sync.RWMutex

	// 체크포인트 (partition -> offset)
	checkpoints  map[string]int64
	checkpointMu sync.RWMutex

	// ack 기반 커밋용. 채널 전송 시점이 아니라 Ack(sink flush 성공) 시점에 커밋한다.
	// pending: partitionKey("topic-partition") -> (offset -> 미ack 메시지). CommitMessages 에 원본 msg 필요.
	// reader: partitionKey -> 해당 파티션을 읽는 reader(커밋 대상).
	ackMu          sync.Mutex
	pending        map[string]map[int64]kafka.Message
	readerByPartn  map[string]*kafka.Reader
	committedByKey map[string]int64 // 파티션별 커밋 완료 offset(워터마크)
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

	onParseError := parseErrorRaw // 기본: 하위호환(raw 통과)
	switch cfg.OnParseError {
	case parseErrorDrop, parseErrorError:
		onParseError = cfg.OnParseError
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
		onParseError:   onParseError,
		checkpoints:    make(map[string]int64),
		pending:        make(map[string]map[int64]kafka.Message),
		readerByPartn:  make(map[string]*kafka.Reader),
		committedByKey: make(map[string]int64),
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
			Brokers:     s.brokers,
			Topic:       topic,
			MinBytes:    s.minBytes,
			MaxBytes:    s.maxBytes,
			MaxWait:     s.maxWait,
			StartOffset: s.startOffset,
		}

		// GroupID가 있으면 consumer group 모드.
		// CommitInterval(주기 자동커밋)은 처리 성공과 무관하게 offset 을 진행시켜
		// 처리 실패 시 유실(at-most-once)을 만든다. 그래서 자동커밋을 끄고(=0),
		// readFromReader 가 레코드를 다운스트림에 넘긴 뒤 명시적으로 커밋한다(at-least-once).
		//
		// WatchPartitionChanges: consumer group join 시점에 토픽이 아직 없으면 kafka-go 는
		// 0 partition 을 배정하고(consumergroup.go assignTopicPartitions: "topic 없으면 no assignment"),
		// WatchPartitionChanges 가 꺼져 있으면 토픽이 나중에 생겨도 재배정하지 않아 영구 무소비가 된다.
		// realtime 은 토픽이 첫 produce 로 늦게 생성되는 경우가 흔하므로(streaming pod 가 먼저 뜸) 반드시 켠다.
		// 파티션 증가에도 재배정해 신규 파티션을 소비한다.
		if s.groupID != "" {
			readerCfg.GroupID = s.groupID
			readerCfg.CommitInterval = 0
			readerCfg.WatchPartitionChanges = true
			readerCfg.PartitionWatchInterval = 5 * time.Second
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

		// FetchMessage 는 offset 을 커밋하지 않는다(ReadMessage 는 자동커밋). 처리 성공(레코드
		// 다운스트림 전달) 후에 CommitMessages 로 명시 커밋 → at-least-once.
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			select {
			case errs <- fmt.Errorf("fetch message: %w", err):
			default:
			}
			return
		}

		// 데이터 파싱
		var data map[string]any
		if err := json.Unmarshal(msg.Value, &data); err != nil {
			// 파싱 실패 처리 정책. raw fallback 은 id 등 필드가 없어 downstream 싱크에서
			// 조용히 반복 실패할 수 있으므로 drop/error 로 방어 가능.
			switch s.onParseError {
			case parseErrorDrop:
				slog.Warn("kafka: dropping unparseable message",
					"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
				continue
			case parseErrorError:
				select {
				case errs <- fmt.Errorf("kafka: parse message at %s[%d]@%d: %w", msg.Topic, msg.Partition, msg.Offset, err):
				default:
				}
				return
			default: // parseErrorRaw
				data = map[string]any{
					"key":   string(msg.Key),
					"value": string(msg.Value),
				}
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

		partnKey := fmt.Sprintf("%s-%d", msg.Topic, msg.Partition)
		record := Record{
			Data: data,
			Metadata: Metadata{
				Source:       "kafka",
				Origin:       msg.Topic,
				Offset:       strconv.FormatInt(msg.Offset, 10),
				PartitionKey: partnKey,
				Timestamp:    msg.Time.UnixMilli(),
			},
		}

		// 커밋은 채널 전송 시점이 아니라 Ack(sink flush 성공) 시점에 한다(진짜 at-least-once).
		// 여기서는 미ack 메시지로만 기록해두고, Ack 가 워터마크까지 CommitMessages 를 호출한다.
		if s.groupID != "" {
			s.trackPending(partnKey, reader, msg)
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

// trackPending 은 아직 ack 안 된 메시지를 파티션별로 보관한다(Ack 시 CommitMessages 에 원본 필요).
func (s *KafkaSource) trackPending(partnKey string, reader *kafka.Reader, msg kafka.Message) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if s.pending[partnKey] == nil {
		s.pending[partnKey] = make(map[int64]kafka.Message)
	}
	s.pending[partnKey][msg.Offset] = msg
	s.readerByPartn[partnKey] = reader
}

// Ack 는 다운스트림이 sink 적재까지 성공한 레코드 offset 을 받아, 파티션별로 커밋을 전진시킨다.
// 커밋은 파티션별 ack 최댓값의 메시지 하나만 CommitMessages 로 반영한다(kafka offset 은 누적 커밋이라
// 최댓값 커밋 = 그 이하 전부 커밋). 미ack(gap 이후)은 남겨 재처리(유실 없음).
func (s *KafkaSource) Ack(offsets []RecordOffset) {
	if s.groupID == "" {
		return
	}
	// 파티션별 ack 최댓값 계산.
	maxByKey := make(map[string]int64)
	for _, o := range offsets {
		off, err := strconv.ParseInt(o.Offset, 10, 64)
		if err != nil {
			continue
		}
		if cur, ok := maxByKey[o.PartitionKey]; !ok || off > cur {
			maxByKey[o.PartitionKey] = off
		}
	}

	for partnKey, maxOff := range maxByKey {
		s.ackMu.Lock()
		// 이미 그 이상 커밋했으면 스킵(중복 ack).
		if committed, ok := s.committedByKey[partnKey]; ok && committed >= maxOff {
			s.ackMu.Unlock()
			continue
		}
		msg, ok := s.pending[partnKey][maxOff]
		reader := s.readerByPartn[partnKey]
		s.ackMu.Unlock()
		if !ok || reader == nil {
			continue
		}

		// 커밋은 소비자 ctx 와 독립. 파이프라인 정지 순간에도 이미 적재된 지점은 커밋되어야
		// 재시작 시 불필요한 재처리를 막는다.
		commitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := reader.CommitMessages(commitCtx, msg)
		cancel()
		if err != nil {
			slog.Warn("kafka: commit failed, will reprocess on restart",
				"partition_key", partnKey, "offset", maxOff, "error", err)
			continue
		}

		// 커밋 성공 → 워터마크 갱신 + 그 이하 pending 정리 + 체크포인트 갱신.
		s.ackMu.Lock()
		s.committedByKey[partnKey] = maxOff
		if p := s.pending[partnKey]; p != nil {
			for off := range p {
				if off <= maxOff {
					delete(p, off)
				}
			}
		}
		s.ackMu.Unlock()

		s.checkpointMu.Lock()
		s.checkpoints[partnKey] = maxOff
		s.checkpointMu.Unlock()
	}
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
