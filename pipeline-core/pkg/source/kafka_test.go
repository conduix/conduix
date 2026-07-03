package source

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewKafkaSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
		GroupID: "test-group",
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "kafka" {
		t.Errorf("expected name 'kafka', got '%s'", source.Name())
	}

	if len(source.brokers) != 1 || source.brokers[0] != "localhost:9092" {
		t.Errorf("unexpected brokers: %v", source.brokers)
	}

	if len(source.topics) != 1 || source.topics[0] != "test-topic" {
		t.Errorf("unexpected topics: %v", source.topics)
	}

	if source.groupID != "test-group" {
		t.Errorf("expected groupID 'test-group', got '%s'", source.groupID)
	}
}

func TestNewKafkaSource_StartOffset(t *testing.T) {
	tests := []struct {
		name        string
		startOffset string
		wantEarly   bool // true if FirstOffset (earliest)
	}{
		{"default (latest)", "", false},
		{"earliest", "earliest", true},
		{"beginning", "beginning", true},
		{"latest", "latest", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:        "kafka",
				Brokers:     []string{"localhost:9092"},
				Topics:      []string{"test"},
				StartOffset: tt.startOffset,
			}

			source, err := NewKafkaSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// kafka.FirstOffset = -2, kafka.LastOffset = -1
			if tt.wantEarly && source.startOffset != -2 {
				t.Errorf("expected FirstOffset (-2), got %d", source.startOffset)
			}
			if !tt.wantEarly && source.startOffset != -1 {
				t.Errorf("expected LastOffset (-1), got %d", source.startOffset)
			}
		})
	}
}

func TestNewKafkaSource_OnParseError(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"default (raw)", "", parseErrorRaw},
		{"raw explicit", "raw", parseErrorRaw},
		{"drop", "drop", parseErrorDrop},
		{"error", "error", parseErrorError},
		{"unknown falls back to raw", "bogus", parseErrorRaw},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewKafkaSource(config.SourceV2{
				Type:         "kafka",
				Brokers:      []string{"localhost:9092"},
				Topics:       []string{"test"},
				OnParseError: tt.in,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source.onParseError != tt.want {
				t.Errorf("OnParseError=%q → %q, want %q", tt.in, source.onParseError, tt.want)
			}
		})
	}
}

func TestNewKafkaSource_ByteSettings(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "kafka",
		Brokers:  []string{"localhost:9092"},
		Topics:   []string{"test"},
		MinBytes: 1024,
		MaxBytes: 5 * 1024 * 1024,
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.minBytes != 1024 {
		t.Errorf("expected minBytes 1024, got %d", source.minBytes)
	}

	if source.maxBytes != 5*1024*1024 {
		t.Errorf("expected maxBytes 5MB, got %d", source.maxBytes)
	}
}

func TestNewKafkaSource_TimingSettings(t *testing.T) {
	cfg := config.SourceV2{
		Type:           "kafka",
		Brokers:        []string{"localhost:9092"},
		Topics:         []string{"test"},
		MaxWait:        1000, // 1000ms
		CommitInterval: 5000, // 5000ms
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.maxWait != 1000*time.Millisecond {
		t.Errorf("expected maxWait 1000ms, got %v", source.maxWait)
	}

	if source.commitInterval != 5000*time.Millisecond {
		t.Errorf("expected commitInterval 5000ms, got %v", source.commitInterval)
	}
}

// SASL Authentication Tests

func TestBuildSASLMechanism_Plain(t *testing.T) {
	cfg := &config.SASLConfig{
		Mechanism: "PLAIN",
		Username:  "testuser",
		Password:  "testpass",
	}

	mechanism, err := buildSASLMechanism(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mechanism == nil {
		t.Fatal("expected non-nil mechanism")
	}

	// Check mechanism name
	if mechanism.Name() != "PLAIN" {
		t.Errorf("expected mechanism name 'PLAIN', got '%s'", mechanism.Name())
	}
}

func TestBuildSASLMechanism_ScramSHA256(t *testing.T) {
	cfg := &config.SASLConfig{
		Mechanism: "SCRAM-SHA-256",
		Username:  "testuser",
		Password:  "testpass",
	}

	mechanism, err := buildSASLMechanism(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mechanism == nil {
		t.Fatal("expected non-nil mechanism")
	}

	// SCRAM mechanism name includes SHA algorithm
	if mechanism.Name() != "SCRAM-SHA-256" {
		t.Errorf("expected mechanism name 'SCRAM-SHA-256', got '%s'", mechanism.Name())
	}
}

func TestBuildSASLMechanism_ScramSHA512(t *testing.T) {
	cfg := &config.SASLConfig{
		Mechanism: "SCRAM-SHA-512",
		Username:  "testuser",
		Password:  "testpass",
	}

	mechanism, err := buildSASLMechanism(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mechanism == nil {
		t.Fatal("expected non-nil mechanism")
	}

	if mechanism.Name() != "SCRAM-SHA-512" {
		t.Errorf("expected mechanism name 'SCRAM-SHA-512', got '%s'", mechanism.Name())
	}
}

func TestBuildSASLMechanism_Nil(t *testing.T) {
	mechanism, err := buildSASLMechanism(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mechanism != nil {
		t.Error("expected nil mechanism for nil config")
	}
}

func TestBuildSASLMechanism_UnsupportedMechanism(t *testing.T) {
	cfg := &config.SASLConfig{
		Mechanism: "GSSAPI",
		Username:  "testuser",
		Password:  "testpass",
	}

	_, err := buildSASLMechanism(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported mechanism")
	}

	expectedMsg := "unsupported SASL mechanism: GSSAPI"
	if err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildSASLMechanism_EnvVarExpansion(t *testing.T) {
	// Set environment variables
	t.Setenv("KAFKA_USER", "envuser")
	t.Setenv("KAFKA_PASS", "envpass")

	cfg := &config.SASLConfig{
		Mechanism: "PLAIN",
		Username:  "${KAFKA_USER}",
		Password:  "${KAFKA_PASS}",
	}

	mechanism, err := buildSASLMechanism(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mechanism == nil {
		t.Fatal("expected non-nil mechanism")
	}

	// The mechanism should have been created with expanded values
	// We can't directly inspect PLAIN credentials, but we verify no error
}

// TLS Configuration Tests

func TestBuildTLSConfig_Nil(t *testing.T) {
	tlsCfg, err := buildTLSConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config for nil input")
	}
}

func TestBuildTLSConfig_Disabled(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled: false,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config when disabled")
	}
}

func TestBuildTLSConfig_BasicEnabled(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		SkipVerify: true,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildTLSConfig_ServerName(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		ServerName: "kafka.example.com",
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg.ServerName != "kafka.example.com" {
		t.Errorf("expected ServerName 'kafka.example.com', got '%s'", tlsCfg.ServerName)
	}
}

func TestBuildTLSConfig_InvalidCACert(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled: true,
		CACert:  "/nonexistent/ca.crt",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA cert file")
	}
}

func TestBuildTLSConfig_InvalidClientCert(t *testing.T) {
	cfg := &config.TLSClientConfig{
		Enabled:    true,
		ClientCert: "/nonexistent/client.crt",
		ClientKey:  "/nonexistent/client.key",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent client cert files")
	}
}

// Environment Variable Expansion Tests

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envKey   string
		envValue string
		expected string
	}{
		{
			name:     "no env var",
			input:    "plain-string",
			expected: "plain-string",
		},
		{
			name:     "single env var",
			input:    "${TEST_VAR}",
			envKey:   "TEST_VAR",
			envValue: "test-value",
			expected: "test-value",
		},
		{
			name:     "env var with prefix",
			input:    "prefix-${TEST_VAR}",
			envKey:   "TEST_VAR",
			envValue: "suffix",
			expected: "prefix-suffix",
		},
		{
			name:     "multiple env vars",
			input:    "${VAR1}-${VAR2}",
			envKey:   "VAR1",
			envValue: "first",
			expected: "first-", // VAR2 not set
		},
		{
			name:     "empty env var",
			input:    "${EMPTY_VAR}",
			envKey:   "EMPTY_VAR",
			envValue: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envValue)
			}

			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// KafkaSource with SASL/TLS Integration Tests

func TestNewKafkaSource_WithSASL(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
		SASL: &config.SASLConfig{
			Mechanism: "PLAIN",
			Username:  "testuser",
			Password:  "testpass",
		},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.saslMechanism == nil {
		t.Error("expected SASL mechanism to be configured")
	}

	if source.dialer == nil {
		t.Error("expected dialer to be configured when SASL is enabled")
	}
}

func TestNewKafkaSource_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9093"},
		Topics:  []string{"test-topic"},
		TLS: &config.TLSClientConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.tlsConfig == nil {
		t.Error("expected TLS config to be set")
	}

	if source.dialer == nil {
		t.Error("expected dialer to be configured when TLS is enabled")
	}
}

func TestNewKafkaSource_WithSASLAndTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9093"},
		Topics:  []string{"test-topic"},
		SASL: &config.SASLConfig{
			Mechanism: "SCRAM-SHA-256",
			Username:  "testuser",
			Password:  "testpass",
		},
		TLS: &config.TLSClientConfig{
			Enabled:    true,
			SkipVerify: true,
			ServerName: "kafka.example.com",
		},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.saslMechanism == nil {
		t.Error("expected SASL mechanism to be configured")
	}

	if source.tlsConfig == nil {
		t.Error("expected TLS config to be set")
	}

	if source.dialer == nil {
		t.Error("expected dialer to be configured")
	}

	// Verify TLS config settings
	if source.tlsConfig.ServerName != "kafka.example.com" {
		t.Errorf("expected ServerName 'kafka.example.com', got '%s'", source.tlsConfig.ServerName)
	}
}

func TestNewKafkaSource_InvalidSASL(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
		SASL: &config.SASLConfig{
			Mechanism: "INVALID",
			Username:  "testuser",
			Password:  "testpass",
		},
	}

	_, err := NewKafkaSource(cfg)
	if err == nil {
		t.Fatal("expected error for invalid SASL mechanism")
	}
}

// Checkpoint Tests

func TestKafkaSource_Checkpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial checkpoints should be empty
	checkpoints := source.GetCheckpoints()
	if len(checkpoints) != 0 {
		t.Errorf("expected empty checkpoints, got %v", checkpoints)
	}

	// Update checkpoint
	source.updateCheckpoint("test-topic", 0, 100)
	source.updateCheckpoint("test-topic", 1, 200)

	checkpoints = source.GetCheckpoints()
	if len(checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(checkpoints))
	}

	if checkpoints["test-topic-0"] != 100 {
		t.Errorf("expected offset 100 for partition 0, got %d", checkpoints["test-topic-0"])
	}

	if checkpoints["test-topic-1"] != 200 {
		t.Errorf("expected offset 200 for partition 1, got %d", checkpoints["test-topic-1"])
	}
}

func TestKafkaSource_SetCheckpointsLegacy(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set checkpoints using legacy API
	legacyCheckpoints := map[string]int64{
		"test-topic-0": 50,
		"test-topic-1": 75,
	}
	source.SetCheckpointsLegacy(legacyCheckpoints)

	checkpoints := source.GetCheckpoints()
	if checkpoints["test-topic-0"] != 50 {
		t.Errorf("expected offset 50, got %d", checkpoints["test-topic-0"])
	}
}

func TestKafkaSource_CheckpointableSourceInterface(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update some checkpoints
	source.updateCheckpoint("test-topic", 0, 100)

	// Test SourceType
	if source.SourceType() != "kafka" {
		t.Errorf("expected source type 'kafka', got '%s'", source.SourceType())
	}

	// Test GetSourceCheckpoints
	sourceCheckpoints := source.GetSourceCheckpoints()
	if len(sourceCheckpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(sourceCheckpoints))
	}

	cp := sourceCheckpoints[0]
	if cp.PartitionKey != "test-topic-0" {
		t.Errorf("expected partition key 'test-topic-0', got '%s'", cp.PartitionKey)
	}
	if cp.OffsetValue != "100" {
		t.Errorf("expected offset value '100', got '%s'", cp.OffsetValue)
	}
	if cp.OffsetType != "numeric" {
		t.Errorf("expected offset type 'numeric', got '%s'", cp.OffsetType)
	}

	// Test SetSourceCheckpoints
	newCheckpoints := []*SourceCheckpoint{
		{
			PartitionKey: "test-topic-1",
			OffsetValue:  "200",
			OffsetType:   "numeric",
		},
	}
	err = source.SetSourceCheckpoints(newCheckpoints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checkpoints := source.GetCheckpoints()
	if checkpoints["test-topic-1"] != 200 {
		t.Errorf("expected offset 200, got %d", checkpoints["test-topic-1"])
	}
}

// Open and Close Tests (without actual Kafka connection)

func TestKafkaSource_OpenCreatesReaders(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"topic1", "topic2"},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = source.Open(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify readers were created for each topic
	if len(source.readers) != 2 {
		t.Errorf("expected 2 readers, got %d", len(source.readers))
	}

	// Cleanup
	err = source.Close()
	if err != nil {
		t.Fatalf("unexpected error closing: %v", err)
	}
}

func TestKafkaSource_CloseCleanup(t *testing.T) {
	cfg := config.SourceV2{
		Type:    "kafka",
		Brokers: []string{"localhost:9092"},
		Topics:  []string{"test-topic"},
	}

	source, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	_ = source.Open(ctx)

	err = source.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify readers were cleared
	if len(source.readers) != 0 {
		t.Errorf("expected readers to be cleared after close, got %d", len(source.readers))
	}
}
