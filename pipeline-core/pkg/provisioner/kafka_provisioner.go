package provisioner

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/shared/types"
)

// KafkaProvisioner Kafka 토픽 생성 Provisioner
type KafkaProvisioner struct {
	sinkType types.SinkType
}

// NewKafkaProvisioner Kafka Provisioner 생성
func NewKafkaProvisioner() *KafkaProvisioner {
	return &KafkaProvisioner{
		sinkType: types.SinkTypeKafka,
	}
}

func (p *KafkaProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *KafkaProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{
		types.ProvisioningTypeTopicCreation,
		types.ProvisioningTypeExternal,
	}
}

// KafkaProvisioningConfig Kafka 프로비저닝 설정
type KafkaProvisioningConfig struct {
	Brokers           []string `json:"brokers"`            // Kafka 브로커 주소 목록
	TopicName         string   `json:"topic_name"`         // 생성할 토픽 이름
	NumPartitions     int      `json:"num_partitions"`     // 파티션 수 (기본: 3)
	ReplicationFactor int      `json:"replication_factor"` // 복제 팩터 (기본: 1)
	RetentionMs       int64    `json:"retention_ms"`       // 보존 기간 (밀리초, 기본: 7일)
}

func (p *KafkaProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 프로비저닝인 경우
	if req.Type == types.ProvisioningTypeExternal {
		return &types.ProvisioningResult{
			ID:         uuid.New().String(),
			RequestID:  req.ID,
			PipelineID: req.PipelineID,
			SinkType:   req.SinkType,
			Status:     types.ProvisioningStatusPending,
			Message:    "Waiting for external Kafka topic provisioning",
			Metadata: map[string]any{
				"external_url": req.ExternalURL,
				"callback_url": req.CallbackURL,
			},
		}, nil
	}

	// 설정 파싱
	config, err := p.parseConfig(req.Config)
	if err != nil {
		return nil, fmt.Errorf("invalid kafka config: %w", err)
	}

	// 토픽 생성
	if err := p.createTopic(ctx, config); err != nil {
		now := time.Now()
		return &types.ProvisioningResult{
			ID:          uuid.New().String(),
			RequestID:   req.ID,
			PipelineID:  req.PipelineID,
			SinkType:    req.SinkType,
			Status:      types.ProvisioningStatusFailed,
			Message:     "Failed to create Kafka topic",
			ErrorDetail: err.Error(),
			CompletedAt: &now,
		}, nil
	}

	now := time.Now()
	return &types.ProvisioningResult{
		ID:         uuid.New().String(),
		RequestID:  req.ID,
		PipelineID: req.PipelineID,
		SinkType:   req.SinkType,
		Status:     types.ProvisioningStatusCompleted,
		TopicName:  config.TopicName,
		Message:    fmt.Sprintf("Kafka topic '%s' created successfully", config.TopicName),
		Metadata: map[string]any{
			"brokers":            config.Brokers,
			"num_partitions":     config.NumPartitions,
			"replication_factor": config.ReplicationFactor,
		},
		CompletedAt: &now,
	}, nil
}

func (p *KafkaProvisioner) parseConfig(config map[string]any) (*KafkaProvisioningConfig, error) {
	cfg := &KafkaProvisioningConfig{
		NumPartitions:     3,
		ReplicationFactor: 1,
		RetentionMs:       7 * 24 * 60 * 60 * 1000, // 7일
	}

	// Brokers
	if brokers, ok := config["brokers"].([]any); ok {
		for _, b := range brokers {
			if bs, ok := b.(string); ok {
				cfg.Brokers = append(cfg.Brokers, bs)
			}
		}
	} else if brokers, ok := config["brokers"].([]string); ok {
		cfg.Brokers = brokers
	}
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("brokers is required")
	}

	// TopicName
	if topicName, ok := config["topic_name"].(string); ok && topicName != "" {
		cfg.TopicName = topicName
	} else {
		return nil, fmt.Errorf("topic_name is required")
	}

	// NumPartitions
	if np, ok := config["num_partitions"].(float64); ok {
		cfg.NumPartitions = int(np)
	} else if np, ok := config["num_partitions"].(int); ok {
		cfg.NumPartitions = np
	}

	// ReplicationFactor
	if rf, ok := config["replication_factor"].(float64); ok {
		cfg.ReplicationFactor = int(rf)
	} else if rf, ok := config["replication_factor"].(int); ok {
		cfg.ReplicationFactor = rf
	}

	// RetentionMs
	if rm, ok := config["retention_ms"].(float64); ok {
		cfg.RetentionMs = int64(rm)
	} else if rm, ok := config["retention_ms"].(int64); ok {
		cfg.RetentionMs = rm
	}

	return cfg, nil
}

func (p *KafkaProvisioner) createTopic(ctx context.Context, config *KafkaProvisioningConfig) error {
	// 컨트롤러 브로커에 연결
	conn, err := kafka.Dial("tcp", config.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer func() { _ = controllerConn.Close() }()

	// 토픽 생성
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             config.TopicName,
			NumPartitions:     config.NumPartitions,
			ReplicationFactor: config.ReplicationFactor,
			ConfigEntries: []kafka.ConfigEntry{
				{
					ConfigName:  "retention.ms",
					ConfigValue: strconv.FormatInt(config.RetentionMs, 10),
				},
			},
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	return nil
}

func (p *KafkaProvisioner) Validate(config map[string]any) error {
	_, err := p.parseConfig(config)
	return err
}

func (p *KafkaProvisioner) RequiresExternalSetup() bool {
	return false // 자동 생성 가능
}

func (p *KafkaProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return req.ExternalURL
}

// CheckTopicExists 토픽 존재 여부 확인
func (p *KafkaProvisioner) CheckTopicExists(brokers []string, topicName string) (bool, error) {
	conn, err := kafka.Dial("tcp", brokers[0])
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

// DeleteTopic 토픽 삭제 (롤백용)
func (p *KafkaProvisioner) DeleteTopic(brokers []string, topicName string) error {
	conn, err := kafka.Dial("tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer func() { _ = controllerConn.Close() }()

	return controllerConn.DeleteTopics(topicName)
}
