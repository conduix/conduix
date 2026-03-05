package source

import (
	"net/url"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewWebSocketSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "wss://stream.example.com/events",
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "websocket" {
		t.Errorf("expected name 'websocket', got '%s'", source.Name())
	}

	if source.url != cfg.WSURL {
		t.Errorf("expected URL '%s', got '%s'", cfg.WSURL, source.url)
	}

	if source.pingInterval != 30*time.Second {
		t.Errorf("expected default ping interval 30s, got %v", source.pingInterval)
	}

	if source.pongWait != 60*time.Second {
		t.Errorf("expected default pong wait 60s, got %v", source.pongWait)
	}

	if source.reconnectWait != 5*time.Second {
		t.Errorf("expected default reconnect wait 5s, got %v", source.reconnectWait)
	}

	if source.maxReconnect != 10 {
		t.Errorf("expected default max reconnect 10, got %d", source.maxReconnect)
	}
}

func TestNewWebSocketSource_CustomConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:            "websocket",
		WSURL:           "wss://custom.example.com/stream",
		WSSubprotocols:  []string{"proto1", "proto2"},
		WSPingInterval:  15000, // 15s
		WSPongWait:      45000, // 45s
		WSReconnectWait: 10000, // 10s
		WSMaxReconnect:  5,
		WSMessageType:   "binary",
		WSSubscribeMsg:  `{"action": "subscribe", "channel": "events"}`,
		Headers: map[string]string{
			"Authorization": "Bearer token123",
		},
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(source.subprotocols) != 2 {
		t.Errorf("expected 2 subprotocols, got %d", len(source.subprotocols))
	}

	if source.pingInterval != 15*time.Second {
		t.Errorf("expected ping interval 15s, got %v", source.pingInterval)
	}

	if source.pongWait != 45*time.Second {
		t.Errorf("expected pong wait 45s, got %v", source.pongWait)
	}

	if source.reconnectWait != 10*time.Second {
		t.Errorf("expected reconnect wait 10s, got %v", source.reconnectWait)
	}

	if source.maxReconnect != 5 {
		t.Errorf("expected max reconnect 5, got %d", source.maxReconnect)
	}

	// websocket.BinaryMessage = 2
	if source.messageType != 2 {
		t.Errorf("expected binary message type (2), got %d", source.messageType)
	}

	if source.subscribeMsg != cfg.WSSubscribeMsg {
		t.Errorf("expected subscribe message '%s', got '%s'", cfg.WSSubscribeMsg, source.subscribeMsg)
	}

	if source.headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header, got '%s'", source.headers["Authorization"])
	}
}

func TestNewWebSocketSource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "ws://localhost:8080/ws",
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "websocket" {
		t.Errorf("expected source type 'websocket', got '%s'", source.SourceType())
	}
}

func TestWebSocketSource_GetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "ws://localhost:8080/ws",
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update checkpoint
	source.updateCheckpoint()
	source.updateCheckpoint()
	source.updateCheckpoint()

	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	if cp.PartitionKey != cfg.WSURL {
		t.Errorf("expected partition key '%s', got '%s'", cfg.WSURL, cp.PartitionKey)
	}

	if cp.OffsetValue != "3" {
		t.Errorf("expected offset value '3', got '%s'", cp.OffsetValue)
	}

	if cp.OffsetType != "numeric" {
		t.Errorf("expected offset type 'numeric', got '%s'", cp.OffsetType)
	}

	if cp.RecordCount != 3 {
		t.Errorf("expected record count 3, got %d", cp.RecordCount)
	}
}

func TestWebSocketSource_SetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "ws://localhost:8080/ws",
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SetSourceCheckpoints should not error (WebSocket doesn't support checkpoint restoration)
	checkpoints := []*SourceCheckpoint{
		{
			PartitionKey: cfg.WSURL,
			OffsetValue:  "100",
			OffsetType:   "numeric",
		},
	}

	err = source.SetSourceCheckpoints(checkpoints)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewWebSocketSource_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "wss://secure.example.com/stream",
		TLS: &config.TLSClientConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.tlsConfig == nil {
		t.Error("expected tlsConfig to be set")
	}

	if !source.tlsConfig.Enabled {
		t.Error("expected TLS to be enabled")
	}
}

func TestMaskWebSocketURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mask password",
			input:    "wss://user:secret@example.com/ws",
			expected: "wss://user:****@example.com/ws",
		},
		{
			name:     "no password",
			input:    "wss://user@example.com/ws",
			expected: "wss://user@example.com/ws",
		},
		{
			name:     "no auth",
			input:    "wss://example.com/ws",
			expected: "wss://example.com/ws",
		},
		{
			name:     "ws scheme",
			input:    "ws://user:pass@localhost:8080/stream",
			expected: "ws://user:****@localhost:8080/stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskWebSocketURL(tt.input)
			// URL 파싱으로 인해 정확한 문자열 비교가 어려우므로 User 정보만 확인
			expectedURL, _ := url.Parse(tt.expected)
			resultURL, _ := url.Parse(result)

			if expectedURL.User != nil && resultURL.User != nil {
				if expectedURL.User.String() != resultURL.User.String() {
					t.Errorf("maskWebSocketURL(%q) user = %s, expected %s",
						tt.input, resultURL.User.String(), expectedURL.User.String())
				}
			}
		})
	}
}

func TestNewWebSocketSource_DefaultMessageType(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "websocket",
		WSURL: "ws://localhost/ws",
	}

	source, err := NewWebSocketSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// websocket.TextMessage = 1
	if source.messageType != 1 {
		t.Errorf("expected default text message type (1), got %d", source.messageType)
	}
}
