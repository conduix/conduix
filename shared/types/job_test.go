package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultJobConfig(t *testing.T) {
	config := DefaultJobConfig()

	if config.CPU != "500m" {
		t.Errorf("Expected CPU to be '500m', got '%s'", config.CPU)
	}
	if config.Memory != "512Mi" {
		t.Errorf("Expected Memory to be '512Mi', got '%s'", config.Memory)
	}
	if config.CPULimit != "1000m" {
		t.Errorf("Expected CPULimit to be '1000m', got '%s'", config.CPULimit)
	}
	if config.MemoryLimit != "1Gi" {
		t.Errorf("Expected MemoryLimit to be '1Gi', got '%s'", config.MemoryLimit)
	}
	if config.TimeoutSeconds != 3600 {
		t.Errorf("Expected TimeoutSeconds to be 3600, got %d", config.TimeoutSeconds)
	}
	if config.BackoffLimit != 3 {
		t.Errorf("Expected BackoffLimit to be 3, got %d", config.BackoffLimit)
	}
	if config.TTLAfterFinished != 300 {
		t.Errorf("Expected TTLAfterFinished to be 300, got %d", config.TTLAfterFinished)
	}
	if config.Namespace != "conduix" {
		t.Errorf("Expected Namespace to be 'conduix', got '%s'", config.Namespace)
	}
}

func TestJobConfigJSON(t *testing.T) {
	config := JobConfig{
		CPU:              "500m",
		Memory:           "512Mi",
		CPULimit:         "1000m",
		MemoryLimit:      "1Gi",
		TimeoutSeconds:   3600,
		BackoffLimit:     3,
		TTLAfterFinished: 300,
		Namespace:        "test-ns",
		NodeSelector:     map[string]string{"app": "batch"},
	}

	// 직렬화
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal JobConfig: %v", err)
	}

	// 역직렬화
	var parsed JobConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JobConfig: %v", err)
	}

	if parsed.CPU != config.CPU {
		t.Errorf("CPU mismatch: expected '%s', got '%s'", config.CPU, parsed.CPU)
	}
	if parsed.Memory != config.Memory {
		t.Errorf("Memory mismatch: expected '%s', got '%s'", config.Memory, parsed.Memory)
	}
	if parsed.Namespace != config.Namespace {
		t.Errorf("Namespace mismatch: expected '%s', got '%s'", config.Namespace, parsed.Namespace)
	}
	if parsed.NodeSelector["app"] != "batch" {
		t.Errorf("NodeSelector mismatch: expected 'batch', got '%s'", parsed.NodeSelector["app"])
	}
}

func TestJobExecutionResultJSON(t *testing.T) {
	now := time.Now()
	result := JobExecutionResult{
		ExecutionID:  "exec-123",
		WorkflowID:   "wf-456",
		JobName:      "batch-wf-456-exec-123",
		PodName:      "batch-wf-456-exec-123-abc",
		Status:       JobStatusCompleted,
		TotalRecords: 1000,
		StartedAt:    now,
		CompletedAt:  now.Add(time.Minute * 5),
		DurationMs:   300000,
	}

	// 직렬화
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal JobExecutionResult: %v", err)
	}

	// 역직렬화
	var parsed JobExecutionResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JobExecutionResult: %v", err)
	}

	if parsed.ExecutionID != result.ExecutionID {
		t.Errorf("ExecutionID mismatch: expected '%s', got '%s'", result.ExecutionID, parsed.ExecutionID)
	}
	if parsed.Status != JobStatusCompleted {
		t.Errorf("Status mismatch: expected '%s', got '%s'", JobStatusCompleted, parsed.Status)
	}
	if parsed.TotalRecords != 1000 {
		t.Errorf("TotalRecords mismatch: expected 1000, got %d", parsed.TotalRecords)
	}
}

func TestJobStatusConstants(t *testing.T) {
	statuses := map[string]string{
		JobStatusPending:   "pending",
		JobStatusRunning:   "running",
		JobStatusCompleted: "completed",
		JobStatusFailed:    "failed",
		JobStatusTimeout:   "timeout",
	}

	for constant, expected := range statuses {
		if constant != expected {
			t.Errorf("Status constant mismatch: expected '%s', got '%s'", expected, constant)
		}
	}
}
