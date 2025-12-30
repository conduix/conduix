package agent

import (
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

func TestCommunicationMode(t *testing.T) {
	tests := []struct {
		mode     CommunicationMode
		expected int
	}{
		{ModeRedis, 0},
		{ModeREST, 1},
		{ModeHybrid, 2},
	}

	for _, tt := range tests {
		if int(tt.mode) != tt.expected {
			t.Errorf("expected %d, got %d", tt.expected, int(tt.mode))
		}
	}
}

func TestConfig(t *testing.T) {
	cfg := &Config{
		ID:                  "test-agent",
		ControlPlaneURL:     "http://localhost:8080",
		RedisHost:           "localhost",
		RedisPort:           6379,
		RedisPassword:       "secret",
		HeartbeatInterval:   10 * time.Second,
		Labels:              []string{"production", "us-west"},
		CommandPollInterval: 5 * time.Second,
		EnableRESTFallback:  true,
	}

	if cfg.ID != "test-agent" {
		t.Errorf("ID mismatch")
	}
	if cfg.RedisPort != 6379 {
		t.Errorf("RedisPort mismatch")
	}
	if len(cfg.Labels) != 2 {
		t.Errorf("Labels length mismatch")
	}
}

func TestNewAgentWithoutRedis(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		ControlPlaneURL:    "http://localhost:8080",
		EnableRESTFallback: true,
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if agent.ID != "test-agent" {
		t.Errorf("ID mismatch: expected 'test-agent', got '%s'", agent.ID)
	}
	if agent.Status != types.AgentStatusOffline {
		t.Errorf("initial status should be offline")
	}
	if agent.commMode != ModeREST {
		t.Errorf("expected REST mode when Redis is not configured, got %d", agent.commMode)
	}
}

func TestNewAgentGeneratesID(t *testing.T) {
	cfg := &Config{
		ControlPlaneURL:    "http://localhost:8080",
		EnableRESTFallback: true,
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if agent.ID == "" {
		t.Error("agent ID should be auto-generated")
	}
}

func TestAgentGetStatus(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		ControlPlaneURL:    "http://localhost:8080",
		Labels:             []string{"test"},
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)
	status := agent.GetStatus()

	if status.ID != "test-agent" {
		t.Errorf("ID mismatch")
	}
	if status.Status != types.AgentStatusOffline {
		t.Errorf("status should be offline")
	}
	if len(status.Labels) != 1 || status.Labels[0] != "test" {
		t.Errorf("labels mismatch")
	}
}

func TestAgentListPipelinesEmpty(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)
	pipelines := agent.ListPipelines()

	if len(pipelines) != 0 {
		t.Errorf("expected 0 pipelines, got %d", len(pipelines))
	}
}

func TestAgentStartStop(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
		HeartbeatInterval:  1 * time.Hour, // Prevent actual heartbeats during test
	}

	agent, _ := NewAgent(cfg)

	// Start
	err := agent.Start()
	if err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	if agent.Status != types.AgentStatusOnline {
		t.Errorf("status should be online after start")
	}

	// Stop
	err = agent.Stop()
	if err != nil {
		t.Fatalf("failed to stop agent: %v", err)
	}
	if agent.Status != types.AgentStatusOffline {
		t.Errorf("status should be offline after stop")
	}
}

func TestAgentGetPipelineStatusNotFound(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	_, err := agent.GetPipelineStatus("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestAgentStopPipelineNotFound(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	err := agent.StopPipeline("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestAgentPausePipelineNotFound(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	err := agent.PausePipeline("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestAgentResumePipelineNotFound(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	err := agent.ResumePipeline("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestAgentGetCommunicationMode(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	mode := agent.GetCommunicationMode()
	if mode != ModeREST {
		t.Errorf("expected REST mode, got %d", mode)
	}
}

func TestAgentIsRedisHealthy(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	// Without Redis configured, should be unhealthy
	if agent.IsRedisHealthy() {
		t.Error("expected Redis to be unhealthy when not configured")
	}
}

func TestAgentGetRedisMetricsNil(t *testing.T) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	metrics := agent.GetRedisMetrics()
	if metrics != nil {
		t.Error("expected nil metrics when Redis is not configured")
	}
}

func TestPipelineInstance(t *testing.T) {
	now := time.Now()
	instance := &PipelineInstance{
		ID:        "pipeline-1",
		Status:    types.PipelineStatusRunning,
		StartTime: now,
	}

	if instance.ID != "pipeline-1" {
		t.Errorf("ID mismatch")
	}
	if instance.Status != types.PipelineStatusRunning {
		t.Errorf("status mismatch")
	}
}

func BenchmarkAgentGetStatus(b *testing.B) {
	cfg := &Config{
		ID:                 "test-agent",
		Labels:             []string{"test1", "test2"},
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agent.GetStatus()
	}
}

func BenchmarkAgentListPipelines(b *testing.B) {
	cfg := &Config{
		ID:                 "test-agent",
		EnableRESTFallback: true,
	}

	agent, _ := NewAgent(cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agent.ListPipelines()
	}
}
