package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// KafkaService Kafka 토픽 관리 서비스
type KafkaService struct {
	brokers           []string
	numPartitions     int
	replicationFactor int
	retentionMs       int64
	logger            *slog.Logger
}

// KafkaServiceConfig Kafka 서비스 설정
type KafkaServiceConfig struct {
	Brokers           []string
	NumPartitions     int
	ReplicationFactor int
	RetentionMs       int64
	Logger            *slog.Logger
}

// NewKafkaService KafkaService 생성
func NewKafkaService(cfg *KafkaServiceConfig) *KafkaService {
	if cfg == nil {
		cfg = &KafkaServiceConfig{}
	}

	brokers := cfg.Brokers
	if len(brokers) == 0 {
		// 환경변수에서 읽기
		brokersEnv := os.Getenv("KAFKA_BROKERS")
		if brokersEnv != "" {
			brokers = strings.Split(brokersEnv, ",")
		} else {
			brokers = []string{"localhost:9092"}
		}
	}

	numPartitions := cfg.NumPartitions
	if numPartitions <= 0 {
		numPartitions = 3
	}

	replicationFactor := cfg.ReplicationFactor
	if replicationFactor <= 0 {
		replicationFactor = 1
	}

	retentionMs := cfg.RetentionMs
	if retentionMs <= 0 {
		retentionMs = 7 * 24 * 60 * 60 * 1000 // 7일
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &KafkaService{
		brokers:           brokers,
		numPartitions:     numPartitions,
		replicationFactor: replicationFactor,
		retentionMs:       retentionMs,
		logger:            logger,
	}
}

// GenerateTopicName 부모-자식 파이프라인 간 토픽 이름 생성
// 형식: {workflow_slug}_{parent_name}_to_{child_name}
// 63자 제한 (Kafka 토픽 이름 제한)
func (s *KafkaService) GenerateTopicName(workflowSlug, parentName, childName string) string {
	// 기본 형식
	baseName := fmt.Sprintf("%s_%s_to_%s", workflowSlug, parentName, childName)

	// 허용되지 않는 문자 제거 (영숫자, _, -, . 만 허용)
	baseName = sanitizeTopicName(baseName)

	// 63자 제한 확인
	if len(baseName) <= 63 {
		return baseName
	}

	// 길이 초과 시 해시 suffix 추가
	hash := sha256.Sum256([]byte(baseName))
	hashSuffix := hex.EncodeToString(hash[:])[:8]

	// 최대 길이에서 해시 공간 확보 (63 - 9 = 54, 해시 8자 + 구분자 1자)
	maxBaseLen := 54
	if len(baseName) > maxBaseLen {
		baseName = baseName[:maxBaseLen]
	}

	return fmt.Sprintf("%s_%s", baseName, hashSuffix)
}

// sanitizeTopicName 토픽 이름에서 허용되지 않는 문자 제거
func sanitizeTopicName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			result.WriteRune(r)
		} else if r == ' ' {
			result.WriteRune('_')
		}
	}
	return strings.ToLower(result.String())
}

// CreateTopic Kafka 토픽 생성
func (s *KafkaService) CreateTopic(ctx context.Context, topicName string) error {
	if len(s.brokers) == 0 {
		return fmt.Errorf("no kafka brokers configured")
	}

	s.logger.Info("Creating Kafka topic", "topic", topicName, "brokers", s.brokers)

	// 컨트롤러 브로커에 연결
	conn, err := kafka.DialContext(ctx, "tcp", s.brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer func() { _ = controllerConn.Close() }()

	// 토픽 생성
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topicName,
			NumPartitions:     s.numPartitions,
			ReplicationFactor: s.replicationFactor,
			ConfigEntries: []kafka.ConfigEntry{
				{
					ConfigName:  "retention.ms",
					ConfigValue: strconv.FormatInt(s.retentionMs, 10),
				},
			},
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	s.logger.Info("Kafka topic created successfully", "topic", topicName)
	return nil
}

// DeleteTopic Kafka 토픽 삭제
func (s *KafkaService) DeleteTopic(ctx context.Context, topicName string) error {
	if len(s.brokers) == 0 {
		return fmt.Errorf("no kafka brokers configured")
	}

	s.logger.Info("Deleting Kafka topic", "topic", topicName, "brokers", s.brokers)

	conn, err := kafka.DialContext(ctx, "tcp", s.brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer func() { _ = controllerConn.Close() }()

	err = controllerConn.DeleteTopics(topicName)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}

	s.logger.Info("Kafka topic deleted successfully", "topic", topicName)
	return nil
}

// TopicExists 토픽 존재 여부 확인
func (s *KafkaService) TopicExists(ctx context.Context, topicName string) (bool, error) {
	if len(s.brokers) == 0 {
		return false, fmt.Errorf("no kafka brokers configured")
	}

	conn, err := kafka.DialContext(ctx, "tcp", s.brokers[0])
	if err != nil {
		return false, fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer func() { _ = conn.Close() }()

	partitions, err := conn.ReadPartitions(topicName)
	if err != nil {
		// 토픽이 없으면 에러 발생
		return false, nil
	}

	return len(partitions) > 0, nil
}

// CreateTopicWithTimeout 타임아웃과 함께 토픽 생성
func (s *KafkaService) CreateTopicWithTimeout(topicName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.CreateTopic(ctx, topicName)
}

// DeleteTopicWithTimeout 타임아웃과 함께 토픽 삭제
func (s *KafkaService) DeleteTopicWithTimeout(topicName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.DeleteTopic(ctx, topicName)
}

// GetBrokers 브로커 목록 반환
func (s *KafkaService) GetBrokers() []string {
	return s.brokers
}
