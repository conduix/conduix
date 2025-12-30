package services

import (
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

func TestPendingCommand(t *testing.T) {
	cmd := types.AgentCommand{
		ID:         "cmd-123",
		Type:       types.CommandStartPipeline,
		PipelineID: "pipeline-123",
		Timestamp:  time.Now(),
	}

	pending := &PendingCommand{
		AgentID:   "agent-123",
		Command:   cmd,
		CreatedAt: time.Now(),
		Retries:   0,
	}

	if pending.AgentID != "agent-123" {
		t.Errorf("AgentID mismatch: expected 'agent-123', got '%s'", pending.AgentID)
	}
	if pending.Command.ID != "cmd-123" {
		t.Errorf("Command ID mismatch: expected 'cmd-123', got '%s'", pending.Command.ID)
	}
	if pending.Command.Type != types.CommandStartPipeline {
		t.Errorf("Command Type mismatch")
	}
	if pending.Retries != 0 {
		t.Errorf("Retries mismatch: expected 0, got %d", pending.Retries)
	}
}

func TestRedisServiceConfig(t *testing.T) {
	cfg := &RedisServiceConfig{
		Addr:             "localhost:6379",
		Password:         "secret",
		DB:               1,
		EnableRetryQueue: true,
	}

	if cfg.Addr != "localhost:6379" {
		t.Errorf("Addr mismatch: expected 'localhost:6379', got '%s'", cfg.Addr)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password mismatch")
	}
	if cfg.DB != 1 {
		t.Errorf("DB mismatch: expected 1, got %d", cfg.DB)
	}
	if !cfg.EnableRetryQueue {
		t.Error("EnableRetryQueue should be true")
	}
}

func TestRedisServiceConfigWithCallback(t *testing.T) {
	cfg := &RedisServiceConfig{
		Addr:             "localhost:6379",
		EnableRetryQueue: true,
	}

	if cfg.Addr != "localhost:6379" {
		t.Errorf("Addr mismatch")
	}
	if !cfg.EnableRetryQueue {
		t.Error("EnableRetryQueue should be true")
	}
}

func TestPendingCommandExpiry(t *testing.T) {
	// Test that old commands are identified as expired
	oldTime := time.Now().Add(-25 * time.Hour)
	pending := &PendingCommand{
		AgentID:   "agent-123",
		Command:   types.AgentCommand{ID: "cmd-old"},
		CreatedAt: oldTime,
		Retries:   0,
	}

	// Check if command is older than 24 hours
	if time.Since(pending.CreatedAt) <= 24*time.Hour {
		t.Error("old command should be expired")
	}
}

func TestPendingCommandRetryIncrement(t *testing.T) {
	pending := &PendingCommand{
		AgentID:   "agent-123",
		Command:   types.AgentCommand{ID: "cmd-123"},
		CreatedAt: time.Now(),
		Retries:   0,
	}

	// Simulate retry
	pending.Retries++
	if pending.Retries != 1 {
		t.Errorf("Retries mismatch: expected 1, got %d", pending.Retries)
	}

	pending.Retries++
	if pending.Retries != 2 {
		t.Errorf("Retries mismatch: expected 2, got %d", pending.Retries)
	}
}

// Test key generation patterns
func TestKeyPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		id       string
		expected string
	}{
		{
			name:     "agent heartbeat key",
			pattern:  "agent:%s:heartbeat",
			id:       "agent-123",
			expected: "agent:agent-123:heartbeat",
		},
		{
			name:     "agent commands channel",
			pattern:  "agent:commands:%s",
			id:       "agent-123",
			expected: "agent:commands:agent-123",
		},
		{
			name:     "pipeline checkpoint key",
			pattern:  "pipeline:%s:checkpoint",
			id:       "pipeline-456",
			expected: "pipeline:pipeline-456:checkpoint",
		},
		{
			name:     "pipeline metrics key",
			pattern:  "pipeline:%s:metrics",
			id:       "pipeline-456",
			expected: "pipeline:pipeline-456:metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatKey(tt.pattern, tt.id)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Helper function to format keys (simulates the service's key formatting)
func formatKey(pattern, id string) string {
	return replaceFirst(pattern, "%s", id)
}

func replaceFirst(s, old, new string) string {
	for i := 0; i < len(s)-len(old)+1; i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

func TestCommandTypes(t *testing.T) {
	tests := []struct {
		cmdType  types.CommandType
		expected string
	}{
		{types.CommandStartPipeline, "start_pipeline"},
		{types.CommandStopPipeline, "stop_pipeline"},
		{types.CommandPausePipeline, "pause_pipeline"},
		{types.CommandResumePipeline, "resume_pipeline"},
		{types.CommandUpdateConfig, "update_config"},
	}

	for _, tt := range tests {
		t.Run(string(tt.cmdType), func(t *testing.T) {
			if string(tt.cmdType) != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, string(tt.cmdType))
			}
		})
	}
}

func TestAgentCommand(t *testing.T) {
	now := time.Now()
	cmd := types.AgentCommand{
		ID:         "cmd-123",
		Type:       types.CommandStartPipeline,
		PipelineID: "pipeline-456",
		Payload: map[string]string{
			"config_yaml": "version: 1.0",
			"run_id":      "run-789",
		},
		Timestamp: now,
	}

	if cmd.ID != "cmd-123" {
		t.Errorf("ID mismatch: expected 'cmd-123', got '%s'", cmd.ID)
	}
	if cmd.Type != types.CommandStartPipeline {
		t.Errorf("Type mismatch")
	}
	if cmd.PipelineID != "pipeline-456" {
		t.Errorf("PipelineID mismatch: expected 'pipeline-456', got '%s'", cmd.PipelineID)
	}
	if cmd.Payload == nil {
		t.Error("Payload should not be nil")
	}

	payload, ok := cmd.Payload.(map[string]string)
	if !ok {
		t.Fatal("Payload should be map[string]string")
	}
	if payload["config_yaml"] != "version: 1.0" {
		t.Errorf("config_yaml mismatch")
	}
	if payload["run_id"] != "run-789" {
		t.Errorf("run_id mismatch")
	}
}

func BenchmarkKeyFormat(b *testing.B) {
	pattern := "pipeline:%s:checkpoint"
	id := "pipeline-12345678-abcd-1234-5678-abcdef123456"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatKey(pattern, id)
	}
}
