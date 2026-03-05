package source

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewMQTTSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:         "mqtt",
		MQTTBroker:   "tcp://localhost:1883",
		MQTTClientID: "test-client",
		MQTTTopic:    "sensors/+/temperature",
		MQTTQoS:      1,
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "mqtt" {
		t.Errorf("expected name 'mqtt', got '%s'", source.Name())
	}

	if source.broker != "tcp://localhost:1883" {
		t.Errorf("expected broker 'tcp://localhost:1883', got '%s'", source.broker)
	}

	if source.clientID != "test-client" {
		t.Errorf("expected client_id 'test-client', got '%s'", source.clientID)
	}

	if source.topic != "sensors/+/temperature" {
		t.Errorf("expected topic 'sensors/+/temperature', got '%s'", source.topic)
	}

	if source.qos != 1 {
		t.Errorf("expected qos 1, got %d", source.qos)
	}
}

func TestMQTTSource_DefaultValues(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
		MQTTQoS:    -1, // 명시적으로 잘못된 값 설정 시 기본값 사용
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 기본값 확인 (MQTTQoS가 잘못된 값이면 기본값 1)
	if source.qos != 1 {
		t.Errorf("expected default qos 1, got %d", source.qos)
	}

	if source.keepAlive != 60*time.Second {
		t.Errorf("expected default keepAlive 60s, got %v", source.keepAlive)
	}

	if source.reconnectWait != 5*time.Second {
		t.Errorf("expected default reconnectWait 5s, got %v", source.reconnectWait)
	}

	if source.maxReconnect != 10 {
		t.Errorf("expected default maxReconnect 10, got %d", source.maxReconnect)
	}
}

func TestMQTTSource_OpenAndClose(t *testing.T) {
	cfg := config.SourceV2{
		Type:         "mqtt",
		MQTTBroker:   "tcp://localhost:1883",
		MQTTClientID: "test-client",
		MQTTTopic:    "test/topic",
	}

	source, err := NewMQTTSource(cfg)
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

func TestMQTTSource_ConvertMessage(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name        string
		topic       string
		payload     []byte
		expectKey   string
		expectValue any
	}{
		{
			name:        "JSON payload",
			topic:       "sensors/room1/temperature",
			payload:     []byte(`{"temperature": 25.5, "unit": "celsius"}`),
			expectKey:   "temperature",
			expectValue: 25.5,
		},
		{
			name:        "Non-JSON payload",
			topic:       "sensors/room1/status",
			payload:     []byte("online"),
			expectKey:   "message",
			expectValue: "online",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := source.convertMessage(tt.topic, tt.payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if record.Metadata.Source != "mqtt" {
				t.Errorf("expected source 'mqtt', got '%s'", record.Metadata.Source)
			}

			if record.Data["_mqtt_topic"] != tt.topic {
				t.Errorf("expected topic '%s', got '%v'", tt.topic, record.Data["_mqtt_topic"])
			}

			if record.Data[tt.expectKey] != tt.expectValue {
				t.Errorf("expected %s=%v, got %v", tt.expectKey, tt.expectValue, record.Data[tt.expectKey])
			}
		})
	}
}

func TestMQTTSource_Checkpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 초기 체크포인트
	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	if checkpoints[0].RecordCount != 0 {
		t.Errorf("expected record count 0, got %d", checkpoints[0].RecordCount)
	}

	// 메시지 처리 시뮬레이션
	source.updateCheckpoint()
	source.updateCheckpoint()
	source.updateCheckpoint()

	checkpoints = source.GetSourceCheckpoints()
	if checkpoints[0].RecordCount != 3 {
		t.Errorf("expected record count 3, got %d", checkpoints[0].RecordCount)
	}

	if checkpoints[0].OffsetValue != "3" {
		t.Errorf("expected offset '3', got '%s'", checkpoints[0].OffsetValue)
	}
}

func TestMQTTSource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "mqtt" {
		t.Errorf("expected source type 'mqtt', got '%s'", source.SourceType())
	}
}

func TestMQTTSource_WithAuth(t *testing.T) {
	cfg := config.SourceV2{
		Type:         "mqtt",
		MQTTBroker:   "tcp://localhost:1883",
		MQTTClientID: "auth-test",
		MQTTUsername: "testuser",
		MQTTPassword: "testpass",
		MQTTTopic:    "secure/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", source.username)
	}

	if source.password != "testpass" {
		t.Errorf("expected password 'testpass', got '%s'", source.password)
	}
}

func TestMQTTSource_QoSLevels(t *testing.T) {
	tests := []struct {
		name     string
		inputQoS int
		expected byte
	}{
		{"QoS 0", 0, 0},
		{"QoS 1", 1, 1},
		{"QoS 2", 2, 2},
		{"Invalid QoS (default)", -1, 1},
		{"Invalid QoS high (default)", 3, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:       "mqtt",
				MQTTBroker: "tcp://localhost:1883",
				MQTTTopic:  "test/topic",
				MQTTQoS:    tt.inputQoS,
			}

			source, err := NewMQTTSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source.qos != tt.expected {
				t.Errorf("expected qos %d, got %d", tt.expected, source.qos)
			}
		})
	}
}

func TestMaskMQTTBroker(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectContains string
	}{
		{
			name:           "No credentials",
			input:          "tcp://localhost:1883",
			expectContains: "localhost:1883",
		},
		{
			name:           "With password",
			input:          "tcp://user:secret@localhost:1883",
			expectContains: "user:", // 패스워드가 마스킹됨
		},
		{
			name:           "Only username",
			input:          "tcp://user@localhost:1883",
			expectContains: "user@localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskMQTTBroker(tt.input)
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

// MockMQTTClient 테스트용 Mock MQTT 클라이언트
type MockMQTTClient struct {
	connected    bool
	subscribedTo string
	callback     func(string, []byte)
}

func (m *MockMQTTClient) Connect() error {
	m.connected = true
	return nil
}

func (m *MockMQTTClient) Disconnect(quiesce uint) {
	m.connected = false
}

func (m *MockMQTTClient) Subscribe(topic string, qos byte, callback func(topic string, payload []byte)) error {
	m.subscribedTo = topic
	m.callback = callback
	return nil
}

func (m *MockMQTTClient) IsConnected() bool {
	return m.connected
}

func (m *MockMQTTClient) SimulateMessage(topic string, payload []byte) {
	if m.callback != nil {
		m.callback(topic, payload)
	}
}

func TestMQTTSource_ReadWithMockClient(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mock 클라이언트 주입
	mockClient := &MockMQTTClient{}
	source.client = mockClient

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	records, errs := source.Read(ctx)

	// 메시지 시뮬레이션
	go func() {
		time.Sleep(100 * time.Millisecond)
		mockClient.SimulateMessage("test/topic", []byte(`{"value": 42}`))
	}()

	select {
	case record := <-records:
		if record.Data["value"] != float64(42) {
			t.Errorf("expected value 42, got %v", record.Data["value"])
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-ctx.Done():
		t.Log("Test completed (timeout)")
	}
}

func TestMQTTSource_ComplexJSON(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mqtt",
		MQTTBroker: "tcp://localhost:1883",
		MQTTTopic:  "test/topic",
	}

	source, err := NewMQTTSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	complexPayload := map[string]any{
		"device_id": "sensor-001",
		"readings": map[string]any{
			"temperature": 25.5,
			"humidity":    60.0,
		},
		"tags": []string{"room1", "floor2"},
	}
	payloadBytes, _ := json.Marshal(complexPayload)

	record, err := source.convertMessage("sensors/data", payloadBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if record.Data["device_id"] != "sensor-001" {
		t.Errorf("expected device_id 'sensor-001', got '%v'", record.Data["device_id"])
	}

	readings, ok := record.Data["readings"].(map[string]any)
	if !ok {
		t.Fatal("expected readings to be a map")
	}

	if readings["temperature"] != 25.5 {
		t.Errorf("expected temperature 25.5, got %v", readings["temperature"])
	}
}
