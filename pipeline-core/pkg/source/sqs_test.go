package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewSQSSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "sqs",
		SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
		SQSRegion:   "us-east-1",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "sqs" {
		t.Errorf("expected name 'sqs', got '%s'", source.Name())
	}

	if source.queueURL != cfg.SQSQueueURL {
		t.Errorf("expected queue URL '%s', got '%s'", cfg.SQSQueueURL, source.queueURL)
	}

	if source.region != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got '%s'", source.region)
	}

	if source.maxMessages != 10 {
		t.Errorf("expected default max messages 10, got %d", source.maxMessages)
	}

	// waitTimeSeconds 기본값은 0 (short polling)
	// long polling을 원하면 명시적으로 1-20 설정 필요
	if source.waitTimeSeconds != 0 {
		t.Errorf("expected default wait time 0, got %d", source.waitTimeSeconds)
	}

	if source.visibilityTimeout != 30 {
		t.Errorf("expected default visibility timeout 30, got %d", source.visibilityTimeout)
	}
}

func TestNewSQSSource_CustomConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:                 "sqs",
		SQSQueueURL:          "https://sqs.eu-west-1.amazonaws.com/987654321098/custom-queue",
		SQSRegion:            "eu-west-1",
		SQSAccessKeyID:       "AKIAIOSFODNN7EXAMPLE",
		SQSSecretAccessKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SQSMaxMessages:       5,
		SQSWaitTimeSeconds:   10,
		SQSVisibilityTimeout: 60,
		SQSDeleteOnReceive:   true,
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.region != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got '%s'", source.region)
	}

	if source.accessKeyID != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected access key ID 'AKIAIOSFODNN7EXAMPLE', got '%s'", source.accessKeyID)
	}

	if source.maxMessages != 5 {
		t.Errorf("expected max messages 5, got %d", source.maxMessages)
	}

	if source.waitTimeSeconds != 10 {
		t.Errorf("expected wait time 10, got %d", source.waitTimeSeconds)
	}

	if source.visibilityTimeout != 60 {
		t.Errorf("expected visibility timeout 60, got %d", source.visibilityTimeout)
	}

	if !source.deleteOnReceive {
		t.Error("expected deleteOnReceive to be true")
	}
}

func TestNewSQSSource_MaxMessagesValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{"zero defaults to 10", 0, 10},
		{"negative defaults to 10", -1, 10},
		{"over 10 defaults to 10", 15, 10},
		{"valid value 1", 1, 1},
		{"valid value 5", 5, 5},
		{"valid value 10", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:           "sqs",
				SQSQueueURL:    "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
				SQSMaxMessages: tt.input,
			}

			source, err := NewSQSSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source.maxMessages != tt.expected {
				t.Errorf("expected max messages %d, got %d", tt.expected, source.maxMessages)
			}
		})
	}
}

func TestNewSQSSource_WaitTimeValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{"negative defaults to 0", -1, 0},
		{"over 20 capped to 20", 25, 20},
		{"valid value 0 (short polling)", 0, 0},
		{"valid value 10", 10, 10},
		{"valid value 20", 20, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:               "sqs",
				SQSQueueURL:        "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
				SQSWaitTimeSeconds: tt.input,
			}

			source, err := NewSQSSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source.waitTimeSeconds != tt.expected {
				t.Errorf("expected wait time %d, got %d", tt.expected, source.waitTimeSeconds)
			}
		})
	}
}

func TestNewSQSSource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "sqs",
		SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "sqs" {
		t.Errorf("expected source type 'sqs', got '%s'", source.SourceType())
	}
}

func TestSQSSource_GetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "sqs",
		SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update checkpoint
	source.updateCheckpoint("msg-12345")

	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	if cp.PartitionKey != cfg.SQSQueueURL {
		t.Errorf("expected partition key '%s', got '%s'", cfg.SQSQueueURL, cp.PartitionKey)
	}

	if cp.OffsetValue != "msg-12345" {
		t.Errorf("expected offset value 'msg-12345', got '%s'", cp.OffsetValue)
	}

	if cp.OffsetType != "string" {
		t.Errorf("expected offset type 'string', got '%s'", cp.OffsetType)
	}

	if cp.RecordCount != 1 {
		t.Errorf("expected record count 1, got %d", cp.RecordCount)
	}
}

func TestSQSSource_SetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "sqs",
		SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SetSourceCheckpoints should not error (SQS doesn't support checkpoint restoration)
	checkpoints := []*SourceCheckpoint{
		{
			PartitionKey: cfg.SQSQueueURL,
			OffsetValue:  "msg-12345",
			OffsetType:   "string",
		},
	}

	err = source.SetSourceCheckpoints(checkpoints)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewSQSSource_WithCustomEndpoint(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "sqs",
		SQSQueueURL: "http://localhost:4566/000000000000/test-queue",
		SQSEndpoint: "http://localhost:4566",
		SQSRegion:   "us-east-1",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.endpoint != "http://localhost:4566" {
		t.Errorf("expected endpoint 'http://localhost:4566', got '%s'", source.endpoint)
	}
}

func TestNewSQSSource_WithSessionToken(t *testing.T) {
	cfg := config.SourceV2{
		Type:               "sqs",
		SQSQueueURL:        "https://sqs.us-east-1.amazonaws.com/123456789012/test-queue",
		SQSAccessKeyID:     "ASIAXXX",
		SQSSecretAccessKey: "secret",
		SQSSessionToken:    "session-token-xxx",
	}

	source, err := NewSQSSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.sessionToken != "session-token-xxx" {
		t.Errorf("expected session token 'session-token-xxx', got '%s'", source.sessionToken)
	}
}
