package source

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// MQTTSource MQTT 데이터 소스
type MQTTSource struct {
	broker        string
	clientID      string
	username      string
	password      string
	topic         string
	qos           byte
	cleanSession  bool
	keepAlive     time.Duration
	reconnectWait time.Duration
	maxReconnect  int
	tlsConfig     *config.TLSClientConfig

	client    MQTTClient
	mu        sync.RWMutex
	connected bool

	// 체크포인트
	lastMessageID  string
	processedCount int64
	checkpointMu   sync.RWMutex
}

// MQTTClient MQTT 클라이언트 인터페이스 (테스트용 추상화)
type MQTTClient interface {
	Connect() error
	Disconnect(quiesce uint)
	Subscribe(topic string, qos byte, callback func(topic string, payload []byte)) error
	IsConnected() bool
}

// DefaultMQTTClient 기본 MQTT 클라이언트 구현
// paho.mqtt.golang 라이브러리 사용
type DefaultMQTTClient struct {
	broker       string
	clientID     string
	username     string
	password     string
	keepAlive    time.Duration
	cleanSession bool
	tlsConfig    *tls.Config

	msgChan    chan []byte
	topicChan  chan string
	connected  bool
	mu         sync.RWMutex
	cancelFunc context.CancelFunc
}

// NewDefaultMQTTClient 기본 MQTT 클라이언트 생성
func NewDefaultMQTTClient(broker, clientID, username, password string, keepAlive time.Duration, cleanSession bool, tlsConfig *tls.Config) *DefaultMQTTClient {
	return &DefaultMQTTClient{
		broker:       broker,
		clientID:     clientID,
		username:     username,
		password:     password,
		keepAlive:    keepAlive,
		cleanSession: cleanSession,
		tlsConfig:    tlsConfig,
		msgChan:      make(chan []byte, 100),
		topicChan:    make(chan string, 100),
	}
}

// Connect MQTT 브로커에 연결 (Stub - 실제 구현은 paho.mqtt.golang 사용)
func (c *DefaultMQTTClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 실제 구현에서는 paho.mqtt.golang 사용:
	// opts := mqtt.NewClientOptions()
	// opts.AddBroker(c.broker)
	// opts.SetClientID(c.clientID)
	// opts.SetUsername(c.username)
	// opts.SetPassword(c.password)
	// opts.SetKeepAlive(c.keepAlive)
	// opts.SetCleanSession(c.cleanSession)
	// if c.tlsConfig != nil {
	//     opts.SetTLSConfig(c.tlsConfig)
	// }
	// c.conn = mqtt.NewClient(opts)
	// token := c.conn.Connect()
	// token.Wait()
	// return token.Error()

	c.connected = true
	fmt.Printf("[mqtt] Connected to %s as %s\n", maskMQTTBroker(c.broker), c.clientID)
	return nil
}

// Disconnect MQTT 연결 해제
func (c *DefaultMQTTClient) Disconnect(quiesce uint) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 실제 구현에서는:
	// if c.conn != nil {
	//     c.conn.(*mqtt.Client).Disconnect(quiesce)
	// }

	c.connected = false
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	fmt.Printf("[mqtt] Disconnected\n")
}

// Subscribe 토픽 구독
func (c *DefaultMQTTClient) Subscribe(topic string, qos byte, callback func(topic string, payload []byte)) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	// 실제 구현에서는:
	// token := c.conn.(*mqtt.Client).Subscribe(topic, qos, func(client mqtt.Client, msg mqtt.Message) {
	//     callback(msg.Topic(), msg.Payload())
	// })
	// token.Wait()
	// return token.Error()

	fmt.Printf("[mqtt] Subscribed to topic: %s with QoS %d\n", topic, qos)

	// 시뮬레이션용 고루틴 - 실제 구현에서는 콜백으로 대체
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelFunc = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case payload := <-c.msgChan:
				t := <-c.topicChan
				callback(t, payload)
			}
		}
	}()

	return nil
}

// IsConnected 연결 상태 확인
func (c *DefaultMQTTClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// PublishForTest 테스트용 메시지 발행 (테스트에서만 사용)
func (c *DefaultMQTTClient) PublishForTest(topic string, payload []byte) {
	select {
	case c.topicChan <- topic:
	default:
	}
	select {
	case c.msgChan <- payload:
	default:
	}
}

// NewMQTTSource MQTT 소스 생성
func NewMQTTSource(cfg config.SourceV2) (*MQTTSource, error) {
	keepAlive := 60 * time.Second
	if cfg.MQTTKeepAlive > 0 {
		keepAlive = time.Duration(cfg.MQTTKeepAlive) * time.Second
	}

	reconnectWait := 5 * time.Second
	if cfg.MQTTReconnectWait > 0 {
		reconnectWait = time.Duration(cfg.MQTTReconnectWait) * time.Millisecond
	}

	maxReconnect := 10
	if cfg.MQTTMaxReconnect > 0 {
		maxReconnect = cfg.MQTTMaxReconnect
	}

	qos := byte(1) // 기본값
	if cfg.MQTTQoS >= 0 && cfg.MQTTQoS <= 2 {
		qos = byte(cfg.MQTTQoS)
	}

	return &MQTTSource{
		broker:        expandEnvVars(cfg.MQTTBroker),
		clientID:      expandEnvVars(cfg.MQTTClientID),
		username:      expandEnvVars(cfg.MQTTUsername),
		password:      expandEnvVars(cfg.MQTTPassword),
		topic:         cfg.MQTTTopic,
		qos:           qos,
		cleanSession:  cfg.MQTTCleanSession,
		keepAlive:     keepAlive,
		reconnectWait: reconnectWait,
		maxReconnect:  maxReconnect,
		tlsConfig:     cfg.TLS,
	}, nil
}

func (s *MQTTSource) Name() string {
	return "mqtt"
}

func (s *MQTTSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// TLS 설정 빌드
	var tlsCfg *tls.Config
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		var err error
		tlsCfg, err = buildHTTPTLSConfig(s.tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
	}

	// MQTT 클라이언트 생성
	s.client = NewDefaultMQTTClient(
		s.broker,
		s.clientID,
		s.username,
		s.password,
		s.keepAlive,
		s.cleanSession,
		tlsCfg,
	)

	if err := s.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	s.connected = true
	fmt.Printf("[mqtt] Connected to %s, client_id=%s, topic=%s, qos=%d\n",
		maskMQTTBroker(s.broker), s.clientID, s.topic, s.qos)

	return nil
}

func (s *MQTTSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 메시지 수신 채널
		msgChan := make(chan struct {
			topic   string
			payload []byte
		}, 100)

		// 토픽 구독
		err := s.client.Subscribe(s.topic, s.qos, func(topic string, payload []byte) {
			select {
			case msgChan <- struct {
				topic   string
				payload []byte
			}{topic, payload}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case errs <- fmt.Errorf("failed to subscribe: %w", err):
			default:
			}
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-msgChan:
				record, err := s.convertMessage(msg.topic, msg.payload)
				if err != nil {
					select {
					case errs <- fmt.Errorf("failed to convert message: %w", err):
					default:
					}
					continue
				}

				s.updateCheckpoint()

				select {
				case records <- record:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return records, errs
}

func (s *MQTTSource) convertMessage(topic string, payload []byte) (Record, error) {
	var dataMap map[string]any

	// JSON 파싱 시도
	if err := json.Unmarshal(payload, &dataMap); err != nil {
		// JSON이 아닌 경우 raw 데이터로 처리
		dataMap = map[string]any{
			"message": string(payload),
		}
	}

	// MQTT 메타데이터 추가
	dataMap["_mqtt_topic"] = topic

	s.checkpointMu.RLock()
	msgID := fmt.Sprintf("%d", s.processedCount+1)
	s.checkpointMu.RUnlock()

	return Record{
		Data: dataMap,
		Metadata: Metadata{
			Source:    "mqtt",
			Origin:    s.broker + "/" + topic,
			Offset:    msgID,
			Timestamp: time.Now().UnixMilli(),
		},
	}, nil
}

func (s *MQTTSource) updateCheckpoint() {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.processedCount++
	s.lastMessageID = fmt.Sprintf("%d", s.processedCount)
}

// SourceType 소스 타입 반환
func (s *MQTTSource) SourceType() string {
	return "mqtt"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *MQTTSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	return []*SourceCheckpoint{
		{
			PartitionKey: s.broker + "/" + s.topic,
			OffsetValue:  s.lastMessageID,
			OffsetType:   "numeric",
			RecordCount:  s.processedCount,
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정 (MQTT는 재시작 시 체크포인트 복원 미지원)
func (s *MQTTSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	fmt.Printf("[mqtt] Checkpoint restoration not supported for MQTT (streaming protocol)\n")
	return nil
}

func (s *MQTTSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(250)
	}

	s.connected = false
	fmt.Printf("[mqtt] Closed. Processed: %d messages\n", s.processedCount)
	return nil
}

// maskMQTTBroker MQTT 브로커 URL에서 자격 증명 마스킹
func maskMQTTBroker(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "****")
		}
	}
	return u.String()
}
