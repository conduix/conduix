package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPipelineStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   PipelineStatus
		expected string
	}{
		{"pending", PipelineStatusPending, "pending"},
		{"running", PipelineStatusRunning, "running"},
		{"paused", PipelineStatusPaused, "paused"},
		{"completed", PipelineStatusCompleted, "completed"},
		{"failed", PipelineStatusFailed, "failed"},
		{"stopped", PipelineStatusStopped, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.status)
			}
		})
	}
}

func TestPipelineType(t *testing.T) {
	tests := []struct {
		name     string
		ptype    PipelineType
		expected string
	}{
		{"flat", PipelineTypeFlat, "flat"},
		{"actor", PipelineTypeActor, "actor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.ptype) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.ptype)
			}
		})
	}
}

func TestPipelineJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	pipeline := Pipeline{
		ID:          "test-id",
		Name:        "Test Pipeline",
		Description: "A test pipeline",
		Type:        PipelineTypeFlat,
		ConfigYAML:  "version: '1.0'",
		CreatedBy:   "user1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Marshal
	data, err := json.Marshal(pipeline)
	if err != nil {
		t.Fatalf("failed to marshal pipeline: %v", err)
	}

	// Unmarshal
	var decoded Pipeline
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal pipeline: %v", err)
	}

	// Verify
	if decoded.ID != pipeline.ID {
		t.Errorf("ID mismatch: expected %s, got %s", pipeline.ID, decoded.ID)
	}
	if decoded.Name != pipeline.Name {
		t.Errorf("Name mismatch: expected %s, got %s", pipeline.Name, decoded.Name)
	}
	if decoded.Type != pipeline.Type {
		t.Errorf("Type mismatch: expected %s, got %s", pipeline.Type, decoded.Type)
	}
}

func TestPipelineRunJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	run := PipelineRun{
		ID:             "run-1",
		PipelineID:     "pipeline-1",
		Status:         PipelineStatusRunning,
		AgentID:        "agent-1",
		StartedAt:      &now,
		ProcessedCount: 1000,
		ErrorCount:     5,
	}

	// Marshal
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("failed to marshal pipeline run: %v", err)
	}

	// Unmarshal
	var decoded PipelineRun
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal pipeline run: %v", err)
	}

	// Verify
	if decoded.ID != run.ID {
		t.Errorf("ID mismatch: expected %s, got %s", run.ID, decoded.ID)
	}
	if decoded.Status != run.Status {
		t.Errorf("Status mismatch: expected %s, got %s", run.Status, decoded.Status)
	}
	if decoded.ProcessedCount != run.ProcessedCount {
		t.Errorf("ProcessedCount mismatch: expected %d, got %d", run.ProcessedCount, decoded.ProcessedCount)
	}
}

func TestPipelineMetrics(t *testing.T) {
	metrics := PipelineMetrics{
		PipelineID:       "pipeline-1",
		EventsIn:         10000,
		EventsOut:        9500,
		BytesIn:          1024000,
		BytesOut:         950000,
		ErrorsTotal:      50,
		ProcessingTimeMs: 5000,
		LastUpdated:      time.Now(),
	}

	// Marshal
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("failed to marshal metrics: %v", err)
	}

	// Unmarshal
	var decoded PipelineMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal metrics: %v", err)
	}

	// Verify
	if decoded.EventsIn != metrics.EventsIn {
		t.Errorf("EventsIn mismatch: expected %d, got %d", metrics.EventsIn, decoded.EventsIn)
	}
	if decoded.EventsOut != metrics.EventsOut {
		t.Errorf("EventsOut mismatch: expected %d, got %d", metrics.EventsOut, decoded.EventsOut)
	}
}

func TestScheduleJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	schedule := Schedule{
		ID:             "schedule-1",
		PipelineID:     "pipeline-1",
		CronExpression: "0 0 * * *",
		Enabled:        true,
		LastRunAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Marshal
	data, err := json.Marshal(schedule)
	if err != nil {
		t.Fatalf("failed to marshal schedule: %v", err)
	}

	// Unmarshal
	var decoded Schedule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal schedule: %v", err)
	}

	// Verify
	if decoded.CronExpression != schedule.CronExpression {
		t.Errorf("CronExpression mismatch: expected %s, got %s", schedule.CronExpression, decoded.CronExpression)
	}
	if decoded.Enabled != schedule.Enabled {
		t.Errorf("Enabled mismatch: expected %v, got %v", schedule.Enabled, decoded.Enabled)
	}
}
