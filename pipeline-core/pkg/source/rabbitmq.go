package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// RabbitMQSource RabbitMQ 데이터 소스
type RabbitMQSource struct {
	url           string
	queue         string
	exchange      string
	exchangeType  string
	routingKey    string
	prefetch      int
	autoAck       bool
	exclusive     bool
	durable       bool
	consumerTag   string
	reconnectWait time.Duration

	// TLS 설정
	tlsConfig *config.TLSClientConfig

	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.RWMutex

	// 체크포인트
	lastDeliveryTag uint64
	checkpointMu    sync.RWMutex
}

// NewRabbitMQSource RabbitMQ 소스 생성
func NewRabbitMQSource(cfg config.SourceV2) (*RabbitMQSource, error) {
	prefetch := 10
	if cfg.Prefetch > 0 {
		prefetch = cfg.Prefetch
	}

	reconnectWait := 5 * time.Second
	if cfg.ReconnectWait > 0 {
		reconnectWait = time.Duration(cfg.ReconnectWait) * time.Millisecond
	}

	consumerTag := cfg.ConsumerTag
	if consumerTag == "" {
		consumerTag = fmt.Sprintf("conduix-%d", time.Now().UnixNano())
	}

	return &RabbitMQSource{
		url:           expandEnvVars(cfg.URL),
		queue:         cfg.Queue,
		exchange:      cfg.Exchange,
		exchangeType:  cfg.ExchangeType,
		routingKey:    cfg.RoutingKey,
		prefetch:      prefetch,
		autoAck:       cfg.AutoAck,
		exclusive:     cfg.Exclusive,
		durable:       cfg.Durable,
		consumerTag:   consumerTag,
		reconnectWait: reconnectWait,
		tlsConfig:     cfg.TLS,
	}, nil
}

func (s *RabbitMQSource) Name() string {
	return "rabbitmq"
}

func (s *RabbitMQSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 연결
	conn, err := s.connect()
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	s.conn = conn

	// 채널 생성
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}
	s.channel = ch

	// Prefetch 설정
	if err := ch.Qos(s.prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Exchange가 지정된 경우 선언 및 바인딩
	if s.exchange != "" {
		exchType := s.exchangeType
		if exchType == "" {
			exchType = "direct"
		}

		if err := ch.ExchangeDeclare(
			s.exchange,
			exchType,
			s.durable,
			false, // auto-delete
			false, // internal
			false, // no-wait
			nil,   // arguments
		); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return fmt.Errorf("failed to declare exchange: %w", err)
		}
	}

	// Queue 선언
	_, err = ch.QueueDeclare(
		s.queue,
		s.durable,
		false,       // delete when unused
		s.exclusive, // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Exchange에 바인딩
	if s.exchange != "" {
		routingKey := s.routingKey
		if routingKey == "" {
			routingKey = s.queue
		}

		if err := ch.QueueBind(
			s.queue,
			routingKey,
			s.exchange,
			false, // no-wait
			nil,   // arguments
		); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return fmt.Errorf("failed to bind queue: %w", err)
		}
	}

	slog.Default().Info("RabbitMQ connected",
		"url", maskURL(s.url), "queue", s.queue, "prefetch", s.prefetch)

	return nil
}

func (s *RabbitMQSource) connect() (*amqp.Connection, error) {
	// TLS 연결
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		tlsCfg, err := buildHTTPTLSConfig(s.tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
		return amqp.DialTLS(s.url, tlsCfg)
	}

	// 일반 연결
	return amqp.Dial(s.url)
}

func (s *RabbitMQSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		s.mu.RLock()
		ch := s.channel
		s.mu.RUnlock()

		if ch == nil {
			errs <- fmt.Errorf("channel not initialized")
			return
		}

		// Consumer 시작
		msgs, err := ch.Consume(
			s.queue,
			s.consumerTag,
			s.autoAck,
			s.exclusive,
			false, // no-local
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			errs <- fmt.Errorf("failed to start consumer: %w", err)
			return
		}

		slog.Default().Info("RabbitMQ consumer started", "queue", s.queue, "consumer_tag", s.consumerTag)

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					// 채널이 닫힌 경우
					errs <- fmt.Errorf("message channel closed")
					return
				}

				record, err := s.convertMessage(msg)
				if err != nil {
					select {
					case errs <- fmt.Errorf("failed to convert message: %w", err):
					default:
					}
					// Nack 처리 (autoAck가 false인 경우)
					if !s.autoAck {
						_ = msg.Nack(false, false) // requeue=false
					}
					continue
				}

				// 체크포인트 업데이트
				s.updateCheckpoint(msg.DeliveryTag)

				select {
				case records <- record:
					// Ack 처리 (autoAck가 false인 경우)
					if !s.autoAck {
						_ = msg.Ack(false)
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return records, errs
}

func (s *RabbitMQSource) convertMessage(msg amqp.Delivery) (Record, error) {
	var data map[string]any

	// JSON 파싱 시도
	if err := json.Unmarshal(msg.Body, &data); err != nil {
		// JSON이 아닌 경우 raw body로 처리
		data = map[string]any{
			"body": string(msg.Body),
		}
	}

	// 메시지 속성 추가
	if msg.MessageId != "" {
		data["_message_id"] = msg.MessageId
	}
	if msg.CorrelationId != "" {
		data["_correlation_id"] = msg.CorrelationId
	}
	if msg.ContentType != "" {
		data["_content_type"] = msg.ContentType
	}
	if msg.ReplyTo != "" {
		data["_reply_to"] = msg.ReplyTo
	}
	if msg.Type != "" {
		data["_type"] = msg.Type
	}

	// 헤더 추가
	if len(msg.Headers) > 0 {
		headers := make(map[string]any)
		for k, v := range msg.Headers {
			headers[k] = v
		}
		data["_headers"] = headers
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "rabbitmq",
			Origin:    fmt.Sprintf("%s/%s", s.exchange, s.queue),
			Offset:    fmt.Sprintf("%d", msg.DeliveryTag),
			Timestamp: time.Now().UnixMilli(),
		},
	}, nil
}

func (s *RabbitMQSource) updateCheckpoint(deliveryTag uint64) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.lastDeliveryTag = deliveryTag
}

// SourceType 소스 타입 반환
func (s *RabbitMQSource) SourceType() string {
	return "rabbitmq"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *RabbitMQSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	return []*SourceCheckpoint{
		{
			PartitionKey: s.queue,
			OffsetValue:  fmt.Sprintf("%d", s.lastDeliveryTag),
			OffsetType:   "numeric",
			RecordCount:  int64(s.lastDeliveryTag),
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정 (RabbitMQ는 재시작 시 체크포인트 복원 미지원)
func (s *RabbitMQSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	// RabbitMQ는 메시지 기반 Ack/Nack을 사용하므로 체크포인트 복원이 의미 없음
	// 대신 메시지는 Ack되지 않으면 재전송됨
	slog.Default().Info("RabbitMQ checkpoint restoration not supported (message-based ack/nack)", "queue", s.queue)
	return nil
}

func (s *RabbitMQSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	if s.channel != nil {
		// Consumer 취소
		if err := s.channel.Cancel(s.consumerTag, false); err != nil {
			errs = append(errs, fmt.Errorf("failed to cancel consumer: %w", err))
		}
		if err := s.channel.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close channel: %w", err))
		}
		s.channel = nil
	}

	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close connection: %w", err))
		}
		s.conn = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing: %v", errs)
	}

	slog.Default().Info("RabbitMQ closed", "queue", s.queue, "last_delivery_tag", s.lastDeliveryTag)
	return nil
}

// maskURL URL에서 비밀번호 마스킹
func maskURL(rawURL string) string {
	// amqp://user:password@host:port/ 형식에서 password 마스킹
	if len(rawURL) > 7 && rawURL[:7] == "amqp://" {
		rest := rawURL[7:]
		atIdx := -1
		for i, c := range rest {
			if c == '@' {
				atIdx = i
				break
			}
		}
		if atIdx > 0 {
			userPass := rest[:atIdx]
			colonIdx := -1
			for i, c := range userPass {
				if c == ':' {
					colonIdx = i
					break
				}
			}
			if colonIdx > 0 {
				return "amqp://" + userPass[:colonIdx+1] + "****@" + rest[atIdx+1:]
			}
		}
	}
	return rawURL
}
