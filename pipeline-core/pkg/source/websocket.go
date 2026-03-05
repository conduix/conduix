package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// WebSocketSource WebSocket 데이터 소스
type WebSocketSource struct {
	url           string
	headers       map[string]string
	subprotocols  []string
	pingInterval  time.Duration
	pongWait      time.Duration
	reconnectWait time.Duration
	maxReconnect  int
	messageType   int // websocket.TextMessage or websocket.BinaryMessage
	subscribeMsg  string
	tlsConfig     *config.TLSClientConfig

	conn           *websocket.Conn
	mu             sync.RWMutex
	reconnectCount int

	// 체크포인트
	lastMessageID  string
	processedCount int64
	checkpointMu   sync.RWMutex
}

// NewWebSocketSource WebSocket 소스 생성
func NewWebSocketSource(cfg config.SourceV2) (*WebSocketSource, error) {
	pingInterval := 30 * time.Second
	if cfg.WSPingInterval > 0 {
		pingInterval = time.Duration(cfg.WSPingInterval) * time.Millisecond
	}

	pongWait := 60 * time.Second
	if cfg.WSPongWait > 0 {
		pongWait = time.Duration(cfg.WSPongWait) * time.Millisecond
	}

	reconnectWait := 5 * time.Second
	if cfg.WSReconnectWait > 0 {
		reconnectWait = time.Duration(cfg.WSReconnectWait) * time.Millisecond
	}

	maxReconnect := 10
	if cfg.WSMaxReconnect > 0 {
		maxReconnect = cfg.WSMaxReconnect
	}

	messageType := websocket.TextMessage
	if cfg.WSMessageType == "binary" {
		messageType = websocket.BinaryMessage
	}

	return &WebSocketSource{
		url:           expandEnvVars(cfg.WSURL),
		headers:       cfg.Headers,
		subprotocols:  cfg.WSSubprotocols,
		pingInterval:  pingInterval,
		pongWait:      pongWait,
		reconnectWait: reconnectWait,
		maxReconnect:  maxReconnect,
		messageType:   messageType,
		subscribeMsg:  cfg.WSSubscribeMsg,
		tlsConfig:     cfg.TLS,
	}, nil
}

func (s *WebSocketSource) Name() string {
	return "websocket"
}

func (s *WebSocketSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.connect(ctx); err != nil {
		return err
	}

	fmt.Printf("[websocket] Connected to %s, ping_interval=%v, pong_wait=%v\n",
		maskWebSocketURL(s.url), s.pingInterval, s.pongWait)

	return nil
}

func (s *WebSocketSource) connect(ctx context.Context) error {
	// URL 파싱
	u, err := url.Parse(s.url)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Dialer 설정
	dialer := websocket.Dialer{
		Subprotocols:     s.subprotocols,
		HandshakeTimeout: 10 * time.Second,
	}

	// TLS 설정
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		tlsCfg, err := buildHTTPTLSConfig(s.tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		dialer.TLSClientConfig = tlsCfg
	}

	// 헤더 설정
	header := http.Header{}
	for k, v := range s.headers {
		header.Set(k, expandEnvVars(v))
	}

	// 연결
	conn, resp, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to connect (status %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to connect: %w", err)
	}
	s.conn = conn

	// Pong 핸들러 설정
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(s.pongWait))
	})

	// 구독 메시지 전송 (있는 경우)
	if s.subscribeMsg != "" {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(s.subscribeMsg)); err != nil {
			_ = conn.Close()
			return fmt.Errorf("failed to send subscribe message: %w", err)
		}
		fmt.Printf("[websocket] Sent subscribe message\n")
	}

	s.reconnectCount = 0
	return nil
}

func (s *WebSocketSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// Ping 루틴 시작
		pingDone := make(chan struct{})
		go s.pingLoop(ctx, pingDone)
		defer close(pingDone)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			s.mu.RLock()
			conn := s.conn
			s.mu.RUnlock()

			if conn == nil {
				// 재연결 시도
				if err := s.reconnect(ctx); err != nil {
					select {
					case errs <- fmt.Errorf("reconnect failed: %w", err):
					default:
					}
					return
				}
				continue
			}

			// Read deadline 설정
			_ = conn.SetReadDeadline(time.Now().Add(s.pongWait))

			// 메시지 수신
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					fmt.Printf("[websocket] Connection closed normally\n")
					return
				}

				// 연결 끊김, 재연결 시도
				fmt.Printf("[websocket] Read error: %v, attempting reconnect\n", err)
				s.mu.Lock()
				if s.conn != nil {
					_ = s.conn.Close()
					s.conn = nil
				}
				s.mu.Unlock()
				continue
			}

			// 메시지 변환
			record, err := s.convertMessage(msgType, data)
			if err != nil {
				select {
				case errs <- fmt.Errorf("failed to convert message: %w", err):
				default:
				}
				continue
			}

			// 체크포인트 업데이트
			s.updateCheckpoint()

			select {
			case records <- record:
			case <-ctx.Done():
				return
			}
		}
	}()

	return records, errs
}

func (s *WebSocketSource) pingLoop(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			s.mu.RLock()
			conn := s.conn
			s.mu.RUnlock()

			if conn != nil {
				_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			}
		}
	}
}

func (s *WebSocketSource) reconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reconnectCount++
	if s.reconnectCount > s.maxReconnect {
		return fmt.Errorf("max reconnect attempts (%d) exceeded", s.maxReconnect)
	}

	fmt.Printf("[websocket] Reconnecting... attempt %d/%d\n", s.reconnectCount, s.maxReconnect)

	// 대기
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.reconnectWait):
	}

	return s.connect(ctx)
}

func (s *WebSocketSource) convertMessage(msgType int, data []byte) (Record, error) {
	var dataMap map[string]any

	// JSON 파싱 시도
	if err := json.Unmarshal(data, &dataMap); err != nil {
		// JSON이 아닌 경우 raw 데이터로 처리
		dataMap = map[string]any{
			"message": string(data),
		}
	}

	// 메시지 타입 추가
	switch msgType {
	case websocket.TextMessage:
		dataMap["_message_type"] = "text"
	case websocket.BinaryMessage:
		dataMap["_message_type"] = "binary"
	}

	s.checkpointMu.RLock()
	msgID := fmt.Sprintf("%d", s.processedCount+1)
	s.checkpointMu.RUnlock()

	return Record{
		Data: dataMap,
		Metadata: Metadata{
			Source:    "websocket",
			Origin:    s.url,
			Offset:    msgID,
			Timestamp: time.Now().UnixMilli(),
		},
	}, nil
}

func (s *WebSocketSource) updateCheckpoint() {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.processedCount++
	s.lastMessageID = fmt.Sprintf("%d", s.processedCount)
}

// SourceType 소스 타입 반환
func (s *WebSocketSource) SourceType() string {
	return "websocket"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *WebSocketSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	return []*SourceCheckpoint{
		{
			PartitionKey: s.url,
			OffsetValue:  s.lastMessageID,
			OffsetType:   "numeric",
			RecordCount:  s.processedCount,
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정 (WebSocket은 재시작 시 체크포인트 복원 미지원)
func (s *WebSocketSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	// WebSocket은 스트리밍 프로토콜이므로 체크포인트 복원 불가
	fmt.Printf("[websocket] Checkpoint restoration not supported for WebSocket (streaming protocol)\n")
	return nil
}

func (s *WebSocketSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		// 정상 종료 메시지 전송
		_ = s.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		_ = s.conn.Close()
		s.conn = nil
	}

	fmt.Printf("[websocket] Closed. Processed: %d messages\n", s.processedCount)
	return nil
}

// SendMessage WebSocket을 통해 메시지 전송
func (s *WebSocketSource) SendMessage(data []byte) error {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	return conn.WriteMessage(s.messageType, data)
}

// maskWebSocketURL WebSocket URL에서 자격 증명 마스킹
func maskWebSocketURL(rawURL string) string {
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
