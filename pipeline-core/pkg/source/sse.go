package source

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// SSESource Server-Sent Events 데이터 소스
type SSESource struct {
	url           string
	headers       map[string]string
	auth          *config.AuthConfig
	reconnectWait time.Duration
	maxReconnect  int
	tlsConfig     *config.TLSClientConfig

	client         *http.Client
	mu             sync.RWMutex
	reconnectCount int
	connected      bool
	lastEventID    string

	// 체크포인트
	processedCount int64
	checkpointMu   sync.RWMutex
}

// SSEEvent Server-Sent Event 구조체
type SSEEvent struct {
	ID    string // event id
	Event string // event type
	Data  string // event data
	Retry int    // retry interval (milliseconds)
}

// NewSSESource SSE 소스 생성
func NewSSESource(cfg config.SourceV2) (*SSESource, error) {
	reconnectWait := 3 * time.Second
	if cfg.SSEReconnectWait > 0 {
		reconnectWait = time.Duration(cfg.SSEReconnectWait) * time.Millisecond
	}

	maxReconnect := 10
	if cfg.SSEMaxReconnect > 0 {
		maxReconnect = cfg.SSEMaxReconnect
	}

	return &SSESource{
		url:           expandEnvVars(cfg.SSEURL),
		headers:       cfg.Headers,
		auth:          cfg.Auth,
		reconnectWait: reconnectWait,
		maxReconnect:  maxReconnect,
		tlsConfig:     cfg.TLS,
		lastEventID:   cfg.SSELastEventID,
	}, nil
}

func (s *SSESource) Name() string {
	return "sse"
}

func (s *SSESource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// HTTP 클라이언트 생성
	transport := &http.Transport{}

	// TLS 설정
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		tlsCfg, err := buildHTTPTLSConfig(s.tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		transport.TLSClientConfig = tlsCfg
	}

	s.client = &http.Client{
		Transport: transport,
		Timeout:   0, // SSE는 long-lived connection
	}

	s.connected = true
	fmt.Printf("[sse] Initialized for %s, reconnect_wait=%v, max_reconnect=%d\n",
		maskSSEURL(s.url), s.reconnectWait, s.maxReconnect)

	return nil
}

func (s *SSESource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			err := s.streamEvents(ctx, records, errs)
			if err != nil {
				if ctx.Err() != nil {
					return // context canceled
				}

				s.mu.Lock()
				s.reconnectCount++
				if s.reconnectCount > s.maxReconnect {
					s.mu.Unlock()
					select {
					case errs <- fmt.Errorf("max reconnect attempts (%d) exceeded: %w", s.maxReconnect, err):
					default:
					}
					return
				}
				s.mu.Unlock()

				fmt.Printf("[sse] Connection lost: %v, reconnecting... attempt %d/%d\n",
					err, s.reconnectCount, s.maxReconnect)

				select {
				case <-ctx.Done():
					return
				case <-time.After(s.reconnectWait):
				}
			}
		}
	}()

	return records, errs
}

func (s *SSESource) streamEvents(ctx context.Context, records chan<- Record, errs chan<- error) error {
	// HTTP 요청 생성
	req, err := http.NewRequestWithContext(ctx, "GET", s.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// SSE 헤더 설정
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	// Last-Event-ID 설정 (있는 경우)
	s.checkpointMu.RLock()
	if s.lastEventID != "" {
		req.Header.Set("Last-Event-ID", s.lastEventID)
	}
	s.checkpointMu.RUnlock()

	// 사용자 정의 헤더
	for k, v := range s.headers {
		req.Header.Set(k, expandEnvVars(v))
	}

	// 인증 설정
	if s.auth != nil {
		if err := applyHTTPAuth(req, s.auth); err != nil {
			return fmt.Errorf("failed to apply auth: %w", err)
		}
	}

	// 요청 실행
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Content-Type 확인
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return fmt.Errorf("unexpected content type: %s (expected text/event-stream)", contentType)
	}

	// 연결 성공, 재연결 카운트 리셋
	s.mu.Lock()
	s.reconnectCount = 0
	s.mu.Unlock()

	fmt.Printf("[sse] Connected to %s\n", maskSSEURL(s.url))

	// 이벤트 스트림 파싱
	return s.parseEventStream(ctx, resp.Body, records, errs)
}

func (s *SSESource) parseEventStream(ctx context.Context, reader io.Reader, records chan<- Record, errs chan<- error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 최대 1MB 라인

	var event SSEEvent
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// 빈 줄 = 이벤트 디스패치
		if line == "" {
			if len(dataLines) > 0 {
				event.Data = strings.Join(dataLines, "\n")

				record, err := s.convertEvent(&event)
				if err != nil {
					select {
					case errs <- fmt.Errorf("failed to convert event: %w", err):
					default:
					}
				} else {
					// Last-Event-ID 업데이트
					if event.ID != "" {
						s.checkpointMu.Lock()
						s.lastEventID = event.ID
						s.checkpointMu.Unlock()
					}

					s.updateCheckpoint()

					select {
					case records <- record:
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				// 이벤트 리셋
				event = SSEEvent{}
				dataLines = nil
			}
			continue
		}

		// 주석 무시
		if strings.HasPrefix(line, ":") {
			continue
		}

		// 필드 파싱
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			// 콜론 없음 = 필드명만 있는 경우
			s.parseEventField(&event, line, "", &dataLines)
		} else {
			field := line[:colonIdx]
			value := line[colonIdx+1:]
			// 값 앞의 공백 하나 제거 (SSE 스펙)
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			s.parseEventField(&event, field, value, &dataLines)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream read error: %w", err)
	}

	return io.EOF // 정상 종료
}

func (s *SSESource) parseEventField(event *SSEEvent, field, value string, dataLines *[]string) {
	switch field {
	case "event":
		event.Event = value
	case "data":
		*dataLines = append(*dataLines, value)
	case "id":
		// ID에 NULL 문자가 포함된 경우 무시 (SSE 스펙)
		if !strings.Contains(value, "\x00") {
			event.ID = value
		}
	case "retry":
		// retry 값 파싱 (선택적)
		var retry int
		if _, err := fmt.Sscanf(value, "%d", &retry); err == nil {
			event.Retry = retry
		}
	}
}

func (s *SSESource) convertEvent(event *SSEEvent) (Record, error) {
	var dataMap map[string]any

	// JSON 파싱 시도
	if err := json.Unmarshal([]byte(event.Data), &dataMap); err != nil {
		// JSON이 아닌 경우 raw 데이터로 처리
		dataMap = map[string]any{
			"message": event.Data,
		}
	}

	// SSE 메타데이터 추가
	if event.Event != "" {
		dataMap["_sse_event"] = event.Event
	}
	if event.ID != "" {
		dataMap["_sse_id"] = event.ID
	}

	s.checkpointMu.RLock()
	msgID := s.lastEventID
	if msgID == "" {
		msgID = fmt.Sprintf("%d", s.processedCount+1)
	}
	s.checkpointMu.RUnlock()

	return Record{
		Data: dataMap,
		Metadata: Metadata{
			Source:    "sse",
			Origin:    s.url,
			Offset:    msgID,
			Timestamp: time.Now().UnixMilli(),
		},
	}, nil
}

func (s *SSESource) updateCheckpoint() {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.processedCount++
}

// SourceType 소스 타입 반환
func (s *SSESource) SourceType() string {
	return "sse"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *SSESource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	offsetValue := s.lastEventID
	offsetType := "string"
	if offsetValue == "" {
		offsetValue = fmt.Sprintf("%d", s.processedCount)
		offsetType = "numeric"
	}

	return []*SourceCheckpoint{
		{
			PartitionKey: s.url,
			OffsetValue:  offsetValue,
			OffsetType:   offsetType,
			RecordCount:  s.processedCount,
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정
func (s *SSESource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	for _, cp := range checkpoints {
		if cp.PartitionKey == s.url {
			s.checkpointMu.Lock()
			s.lastEventID = cp.OffsetValue
			s.checkpointMu.Unlock()
			fmt.Printf("[sse] Restored checkpoint: last_event_id=%s\n", cp.OffsetValue)
			return nil
		}
	}
	return nil
}

func (s *SSESource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	fmt.Printf("[sse] Closed. Processed: %d events\n", s.processedCount)
	return nil
}

// maskSSEURL SSE URL에서 자격 증명 마스킹
func maskSSEURL(rawURL string) string {
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

// applyHTTPAuth HTTP 요청에 인증 적용
func applyHTTPAuth(req *http.Request, auth *config.AuthConfig) error {
	switch auth.Type {
	case "basic":
		req.SetBasicAuth(expandEnvVars(auth.Username), expandEnvVars(auth.Password))
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+expandEnvVars(auth.Token))
	case "api_key":
		switch auth.APIKeyIn {
		case "header", "":
			req.Header.Set(auth.APIKeyName, expandEnvVars(auth.APIKey))
		case "query":
			q := req.URL.Query()
			q.Set(auth.APIKeyName, expandEnvVars(auth.APIKey))
			req.URL.RawQuery = q.Encode()
		}
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
	return nil
}
