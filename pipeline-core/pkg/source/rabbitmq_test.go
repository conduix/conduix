package source

import (
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewRabbitMQSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqp://guest:guest@localhost:5672/",
		Queue: "test-queue",
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "rabbitmq" {
		t.Errorf("expected name 'rabbitmq', got '%s'", source.Name())
	}

	if source.queue != "test-queue" {
		t.Errorf("expected queue 'test-queue', got '%s'", source.queue)
	}

	if source.prefetch != 10 {
		t.Errorf("expected default prefetch 10, got %d", source.prefetch)
	}

	if source.reconnectWait != 5*time.Second {
		t.Errorf("expected default reconnect wait 5s, got %v", source.reconnectWait)
	}
}

func TestNewRabbitMQSource_CustomConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:          "rabbitmq",
		URL:           "amqp://user:pass@rabbitmq.example.com:5672/vhost",
		Queue:         "my-queue",
		Exchange:      "my-exchange",
		ExchangeType:  "topic",
		RoutingKey:    "events.#",
		Prefetch:      100,
		AutoAck:       true,
		Exclusive:     true,
		Durable:       true,
		ConsumerTag:   "my-consumer",
		ReconnectWait: 10000, // 10 seconds in milliseconds
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.queue != "my-queue" {
		t.Errorf("expected queue 'my-queue', got '%s'", source.queue)
	}

	if source.exchange != "my-exchange" {
		t.Errorf("expected exchange 'my-exchange', got '%s'", source.exchange)
	}

	if source.exchangeType != "topic" {
		t.Errorf("expected exchange type 'topic', got '%s'", source.exchangeType)
	}

	if source.routingKey != "events.#" {
		t.Errorf("expected routing key 'events.#', got '%s'", source.routingKey)
	}

	if source.prefetch != 100 {
		t.Errorf("expected prefetch 100, got %d", source.prefetch)
	}

	if !source.autoAck {
		t.Error("expected autoAck to be true")
	}

	if !source.exclusive {
		t.Error("expected exclusive to be true")
	}

	if !source.durable {
		t.Error("expected durable to be true")
	}

	if source.consumerTag != "my-consumer" {
		t.Errorf("expected consumer tag 'my-consumer', got '%s'", source.consumerTag)
	}

	if source.reconnectWait != 10*time.Second {
		t.Errorf("expected reconnect wait 10s, got %v", source.reconnectWait)
	}
}

func TestNewRabbitMQSource_DefaultConsumerTag(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqp://localhost/",
		Queue: "test-queue",
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consumer tag should start with "conduix-"
	if len(source.consumerTag) < 8 || source.consumerTag[:8] != "conduix-" {
		t.Errorf("expected consumer tag to start with 'conduix-', got '%s'", source.consumerTag)
	}
}

func TestNewRabbitMQSource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqp://localhost/",
		Queue: "test-queue",
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "rabbitmq" {
		t.Errorf("expected source type 'rabbitmq', got '%s'", source.SourceType())
	}
}

func TestNewRabbitMQSource_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqps://secure.rabbitmq.com:5671/",
		Queue: "secure-queue",
		TLS: &config.TLSClientConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	source, err := NewRabbitMQSource(cfg)
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

func TestRabbitMQSource_GetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqp://localhost/",
		Queue: "test-queue",
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set delivery tag
	source.updateCheckpoint(12345)

	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	if cp.PartitionKey != "test-queue" {
		t.Errorf("expected partition key 'test-queue', got '%s'", cp.PartitionKey)
	}

	if cp.OffsetValue != "12345" {
		t.Errorf("expected offset value '12345', got '%s'", cp.OffsetValue)
	}

	if cp.OffsetType != "numeric" {
		t.Errorf("expected offset type 'numeric', got '%s'", cp.OffsetType)
	}
}

func TestRabbitMQSource_SetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:  "rabbitmq",
		URL:   "amqp://localhost/",
		Queue: "test-queue",
	}

	source, err := NewRabbitMQSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SetSourceCheckpoints should not error (RabbitMQ doesn't support checkpoint restoration)
	checkpoints := []*SourceCheckpoint{
		{
			PartitionKey: "test-queue",
			OffsetValue:  "12345",
			OffsetType:   "numeric",
		},
	}

	err = source.SetSourceCheckpoints(checkpoints)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMaskURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mask password",
			input:    "amqp://user:secret@localhost:5672/",
			expected: "amqp://user:****@localhost:5672/",
		},
		{
			name:     "mask password with special chars",
			input:    "amqp://admin:passw0rd!@rabbitmq.example.com:5672/vhost",
			expected: "amqp://admin:****@rabbitmq.example.com:5672/vhost",
		},
		{
			name:     "no password",
			input:    "amqp://guest@localhost/",
			expected: "amqp://guest@localhost/",
		},
		{
			name:     "no user",
			input:    "amqp://localhost:5672/",
			expected: "amqp://localhost:5672/",
		},
		{
			name:     "amqps scheme",
			input:    "amqps://user:secret@secure.rabbitmq.com:5671/",
			expected: "amqps://user:secret@secure.rabbitmq.com:5671/", // amqps not handled currently
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskURL(tt.input)
			if result != tt.expected {
				t.Errorf("maskURL(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewRabbitMQSource_ExchangeTypes(t *testing.T) {
	exchangeTypes := []string{"direct", "fanout", "topic", "headers"}

	for _, exchType := range exchangeTypes {
		t.Run(exchType, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:         "rabbitmq",
				URL:          "amqp://localhost/",
				Queue:        "test-queue",
				Exchange:     "test-exchange",
				ExchangeType: exchType,
			}

			source, err := NewRabbitMQSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source.exchangeType != exchType {
				t.Errorf("expected exchange type '%s', got '%s'", exchType, source.exchangeType)
			}
		})
	}
}
