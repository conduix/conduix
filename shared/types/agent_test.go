package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   AgentStatus
		expected string
	}{
		{"online", AgentStatusOnline, "online"},
		{"offline", AgentStatusOffline, "offline"},
		{"unknown", AgentStatusUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.status)
			}
		})
	}
}

func TestCommandType(t *testing.T) {
	tests := []struct {
		name     string
		cmdType  CommandType
		expected string
	}{
		{"start", CommandStartPipeline, "start_pipeline"},
		{"stop", CommandStopPipeline, "stop_pipeline"},
		{"pause", CommandPausePipeline, "pause_pipeline"},
		{"resume", CommandResumePipeline, "resume_pipeline"},
		{"update_config", CommandUpdateConfig, "update_config"},
		{"shutdown", CommandShutdown, "shutdown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.cmdType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.cmdType)
			}
		})
	}
}

func TestAgentJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	agent := Agent{
		ID:            "agent-1",
		Hostname:      "worker-node-01",
		IPAddress:     "192.168.1.100",
		Status:        AgentStatusOnline,
		LastHeartbeat: &now,
		RegisteredAt:  now,
		Version:       "1.0.0",
		Labels:        []string{"production", "us-west"},
	}

	// Marshal
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("failed to marshal agent: %v", err)
	}

	// Unmarshal
	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal agent: %v", err)
	}

	// Verify
	if decoded.ID != agent.ID {
		t.Errorf("ID mismatch: expected %s, got %s", agent.ID, decoded.ID)
	}
	if decoded.Hostname != agent.Hostname {
		t.Errorf("Hostname mismatch: expected %s, got %s", agent.Hostname, decoded.Hostname)
	}
	if decoded.Status != agent.Status {
		t.Errorf("Status mismatch: expected %s, got %s", agent.Status, decoded.Status)
	}
	if len(decoded.Labels) != len(agent.Labels) {
		t.Errorf("Labels length mismatch: expected %d, got %d", len(agent.Labels), len(decoded.Labels))
	}
}

func TestAgentHeartbeatJSON(t *testing.T) {
	heartbeat := AgentHeartbeat{
		AgentID:     "agent-1",
		Timestamp:   time.Now().Truncate(time.Second),
		CPUUsage:    45.5,
		MemoryUsage: 60.2,
		DiskUsage:   30.0,
		Pipelines:   []string{"pipeline-1", "pipeline-2"},
		PipelineStats: []PipelineStatShort{
			{
				PipelineID:     "pipeline-1",
				Status:         PipelineStatusRunning,
				ProcessedCount: 10000,
				ErrorCount:     5,
			},
		},
	}

	// Marshal
	data, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("failed to marshal heartbeat: %v", err)
	}

	// Unmarshal
	var decoded AgentHeartbeat
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal heartbeat: %v", err)
	}

	// Verify
	if decoded.AgentID != heartbeat.AgentID {
		t.Errorf("AgentID mismatch: expected %s, got %s", heartbeat.AgentID, decoded.AgentID)
	}
	if decoded.CPUUsage != heartbeat.CPUUsage {
		t.Errorf("CPUUsage mismatch: expected %f, got %f", heartbeat.CPUUsage, decoded.CPUUsage)
	}
	if len(decoded.Pipelines) != len(heartbeat.Pipelines) {
		t.Errorf("Pipelines length mismatch: expected %d, got %d", len(heartbeat.Pipelines), len(decoded.Pipelines))
	}
	if len(decoded.PipelineStats) != len(heartbeat.PipelineStats) {
		t.Errorf("PipelineStats length mismatch: expected %d, got %d", len(heartbeat.PipelineStats), len(decoded.PipelineStats))
	}
}

func TestAgentCommandJSON(t *testing.T) {
	cmd := AgentCommand{
		ID:         "cmd-1",
		Type:       CommandStartPipeline,
		PipelineID: "pipeline-1",
		Payload: map[string]string{
			"config_yaml": "version: '1.0'",
		},
		Timestamp: time.Now().Truncate(time.Second),
	}

	// Marshal
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal command: %v", err)
	}

	// Unmarshal
	var decoded AgentCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal command: %v", err)
	}

	// Verify
	if decoded.ID != cmd.ID {
		t.Errorf("ID mismatch: expected %s, got %s", cmd.ID, decoded.ID)
	}
	if decoded.Type != cmd.Type {
		t.Errorf("Type mismatch: expected %s, got %s", cmd.Type, decoded.Type)
	}
	if decoded.PipelineID != cmd.PipelineID {
		t.Errorf("PipelineID mismatch: expected %s, got %s", cmd.PipelineID, decoded.PipelineID)
	}
}

func TestAgentCommandResponseJSON(t *testing.T) {
	tests := []struct {
		name     string
		response AgentCommandResponse
	}{
		{
			name: "success",
			response: AgentCommandResponse{
				CommandID: "cmd-1",
				Success:   true,
				Message:   "Pipeline started successfully",
			},
		},
		{
			name: "failure",
			response: AgentCommandResponse{
				CommandID: "cmd-2",
				Success:   false,
				Error:     "Failed to start pipeline: config invalid",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("failed to marshal response: %v", err)
			}

			// Unmarshal
			var decoded AgentCommandResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Verify
			if decoded.CommandID != tt.response.CommandID {
				t.Errorf("CommandID mismatch: expected %s, got %s", tt.response.CommandID, decoded.CommandID)
			}
			if decoded.Success != tt.response.Success {
				t.Errorf("Success mismatch: expected %v, got %v", tt.response.Success, decoded.Success)
			}
		})
	}
}
