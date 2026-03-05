package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewSSESource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "sse" {
		t.Errorf("expected name 'sse', got '%s'", source.Name())
	}

	if source.url != "https://example.com/events" {
		t.Errorf("expected url 'https://example.com/events', got '%s'", source.url)
	}
}

func TestSSESource_DefaultValues(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.reconnectWait != 3*time.Second {
		t.Errorf("expected default reconnectWait 3s, got %v", source.reconnectWait)
	}

	if source.maxReconnect != 10 {
		t.Errorf("expected default maxReconnect 10, got %d", source.maxReconnect)
	}
}

func TestSSESource_OpenAndClose(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// Open
	err = source.Open(ctx)
	if err != nil {
		t.Fatalf("unexpected error on Open: %v", err)
	}

	if !source.connected {
		t.Error("expected connected to be true after Open")
	}

	// Close
	err = source.Close()
	if err != nil {
		t.Fatalf("unexpected error on Close: %v", err)
	}

	if source.connected {
		t.Error("expected connected to be false after Close")
	}
}

func TestSSESource_ConvertEvent(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name        string
		event       *SSEEvent
		expectKey   string
		expectValue any
	}{
		{
			name: "JSON data",
			event: &SSEEvent{
				ID:    "123",
				Event: "message",
				Data:  `{"user": "john", "action": "login"}`,
			},
			expectKey:   "user",
			expectValue: "john",
		},
		{
			name: "Non-JSON data",
			event: &SSEEvent{
				ID:    "456",
				Event: "ping",
				Data:  "pong",
			},
			expectKey:   "message",
			expectValue: "pong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := source.convertEvent(tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if record.Metadata.Source != "sse" {
				t.Errorf("expected source 'sse', got '%s'", record.Metadata.Source)
			}

			if tt.event.Event != "" && record.Data["_sse_event"] != tt.event.Event {
				t.Errorf("expected event '%s', got '%v'", tt.event.Event, record.Data["_sse_event"])
			}

			if tt.event.ID != "" && record.Data["_sse_id"] != tt.event.ID {
				t.Errorf("expected id '%s', got '%v'", tt.event.ID, record.Data["_sse_id"])
			}

			if record.Data[tt.expectKey] != tt.expectValue {
				t.Errorf("expected %s=%v, got %v", tt.expectKey, tt.expectValue, record.Data[tt.expectKey])
			}
		})
	}
}

func TestSSESource_ParseEventField(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := &SSEEvent{}
	var dataLines []string

	// event field
	source.parseEventField(event, "event", "update", &dataLines)
	if event.Event != "update" {
		t.Errorf("expected event 'update', got '%s'", event.Event)
	}

	// id field
	source.parseEventField(event, "id", "abc123", &dataLines)
	if event.ID != "abc123" {
		t.Errorf("expected id 'abc123', got '%s'", event.ID)
	}

	// data field (multiple lines)
	source.parseEventField(event, "data", "line1", &dataLines)
	source.parseEventField(event, "data", "line2", &dataLines)
	if len(dataLines) != 2 {
		t.Errorf("expected 2 data lines, got %d", len(dataLines))
	}

	// retry field
	source.parseEventField(event, "retry", "3000", &dataLines)
	if event.Retry != 3000 {
		t.Errorf("expected retry 3000, got %d", event.Retry)
	}
}

func TestSSESource_Checkpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 초기 체크포인트
	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	// 이벤트 처리 시뮬레이션
	source.updateCheckpoint()
	source.checkpointMu.Lock()
	source.lastEventID = "event-100"
	source.checkpointMu.Unlock()
	source.updateCheckpoint()

	checkpoints = source.GetSourceCheckpoints()
	if checkpoints[0].OffsetValue != "event-100" {
		t.Errorf("expected offset 'event-100', got '%s'", checkpoints[0].OffsetValue)
	}

	if checkpoints[0].OffsetType != "string" {
		t.Errorf("expected offset type 'string', got '%s'", checkpoints[0].OffsetType)
	}
}

func TestSSESource_SetCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 체크포인트 복원
	checkpoints := []*SourceCheckpoint{
		{
			PartitionKey: "https://example.com/events",
			OffsetValue:  "last-event-id-123",
			OffsetType:   "string",
			RecordCount:  50,
		},
	}

	err = source.SetSourceCheckpoints(checkpoints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.lastEventID != "last-event-id-123" {
		t.Errorf("expected lastEventID 'last-event-id-123', got '%s'", source.lastEventID)
	}
}

func TestSSESource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "sse" {
		t.Errorf("expected source type 'sse', got '%s'", source.SourceType())
	}
}

func TestSSESource_WithHeaders(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.headers["X-Custom-Header"] != "custom-value" {
		t.Errorf("expected header 'custom-value', got '%s'", source.headers["X-Custom-Header"])
	}
}

func TestSSESource_WithAuth(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
		Auth: &config.AuthConfig{
			Type:  "bearer",
			Token: "test-token",
		},
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.auth == nil {
		t.Fatal("expected auth to be set")
	}

	if source.auth.Token != "test-token" {
		t.Errorf("expected token 'test-token', got '%s'", source.auth.Token)
	}
}

func TestMaskSSEURL(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectContains string
	}{
		{
			name:           "No credentials",
			input:          "https://example.com/events",
			expectContains: "example.com/events",
		},
		{
			name:           "With password",
			input:          "https://user:secret@example.com/events",
			expectContains: "user:", // 패스워드가 마스킹됨
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskSSEURL(tt.input)
			if !strings.Contains(result, tt.expectContains) {
				t.Errorf("expected result to contain '%s', got '%s'", tt.expectContains, result)
			}
			// secret이 포함되지 않아야 함
			if strings.Contains(result, "secret") {
				t.Errorf("expected secret to be masked, got '%s'", result)
			}
		})
	}
}

func TestApplyHTTPAuth(t *testing.T) {
	tests := []struct {
		name          string
		auth          *config.AuthConfig
		expectHeader  string
		expectValue   string
		expectInQuery bool
	}{
		{
			name: "Basic auth",
			auth: &config.AuthConfig{
				Type:     "basic",
				Username: "user",
				Password: "pass",
			},
			expectHeader: "Authorization",
			expectValue:  "Basic",
		},
		{
			name: "Bearer auth",
			auth: &config.AuthConfig{
				Type:  "bearer",
				Token: "my-token",
			},
			expectHeader: "Authorization",
			expectValue:  "Bearer my-token",
		},
		{
			name: "API Key in header",
			auth: &config.AuthConfig{
				Type:       "api_key",
				APIKey:     "api-key-value",
				APIKeyIn:   "header",
				APIKeyName: "X-API-Key",
			},
			expectHeader: "X-API-Key",
			expectValue:  "api-key-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "https://example.com", nil)
			err := applyHTTPAuth(req, tt.auth)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectHeader != "" {
				value := req.Header.Get(tt.expectHeader)
				if !strings.Contains(value, tt.expectValue) {
					t.Errorf("expected header %s to contain '%s', got '%s'", tt.expectHeader, tt.expectValue, value)
				}
			}
		})
	}
}

func TestSSESource_ReadFromServer(t *testing.T) {
	// SSE 서버 생성
	eventCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SSE 헤더 검증
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("expected Accept header 'text/event-stream'")
		}

		// SSE 응답 헤더
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected ResponseWriter to support Flusher")
			return
		}

		// 이벤트 전송
		for i := 0; i < 3; i++ {
			eventCount++
			_, _ = fmt.Fprintf(w, "id: event-%d\n", eventCount)
			_, _ = fmt.Fprintf(w, "event: message\n")
			_, _ = fmt.Fprintf(w, "data: {\"count\": %d}\n", eventCount)
			_, _ = fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: server.URL,
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = source.Open(ctx)
	if err != nil {
		t.Fatalf("unexpected error on Open: %v", err)
	}
	defer func() { _ = source.Close() }()

	records, errs := source.Read(ctx)

	receivedCount := 0
	timeout := time.After(3 * time.Second)

	for receivedCount < 3 {
		select {
		case record, ok := <-records:
			if !ok {
				break
			}
			receivedCount++

			if record.Data["_sse_event"] != "message" {
				t.Errorf("expected event 'message', got '%v'", record.Data["_sse_event"])
			}

			expectedID := fmt.Sprintf("event-%d", receivedCount)
			if record.Data["_sse_id"] != expectedID {
				t.Errorf("expected id '%s', got '%v'", expectedID, record.Data["_sse_id"])
			}

		case err := <-errs:
			t.Logf("Error (may be expected): %v", err)
		case <-timeout:
			t.Fatalf("test timeout, received %d events", receivedCount)
		}
	}

	if receivedCount != 3 {
		t.Errorf("expected 3 events, got %d", receivedCount)
	}
}

func TestSSESource_LastEventID(t *testing.T) {
	receivedLastEventID := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLastEventID = r.Header.Get("Last-Event-ID")

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprintf(w, "id: new-event-1\n")
		_, _ = fmt.Fprintf(w, "data: test\n\n")

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	cfg := config.SourceV2{
		Type:           "sse",
		SSEURL:         server.URL,
		SSELastEventID: "previous-event-100",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = source.Open(ctx)
	if err != nil {
		t.Fatalf("unexpected error on Open: %v", err)
	}
	defer func() { _ = source.Close() }()

	records, _ := source.Read(ctx)

	// 첫 번째 이벤트 수신 대기
	select {
	case <-records:
	case <-ctx.Done():
	}

	// Last-Event-ID 헤더 확인
	if receivedLastEventID != "previous-event-100" {
		t.Errorf("expected Last-Event-ID 'previous-event-100', got '%s'", receivedLastEventID)
	}
}

func TestSSESource_MultilineData(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sse",
		SSEURL: "https://example.com/events",
	}

	source, err := NewSSESource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := &SSEEvent{
		ID:    "multi-1",
		Event: "multiline",
		Data:  "line1\nline2\nline3",
	}

	record, err := source.convertEvent(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 멀티라인 데이터는 JSON 파싱 실패 시 message 필드에 저장
	if record.Data["message"] != "line1\nline2\nline3" {
		t.Errorf("expected multiline data, got '%v'", record.Data["message"])
	}
}
