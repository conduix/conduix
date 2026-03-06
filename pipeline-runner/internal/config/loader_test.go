package config

import (
	"os"
	"testing"
)

func clearEnv() {
	for _, key := range []string{
		"EXECUTION_MODE", "WORKFLOW_ID", "EXECUTION_ID",
		"PIPELINES_CONFIG", "CONTROL_PLANE_URL", "CALLBACK_URL",
		"CHECKPOINT_ENDPOINT", "TIMEOUT_SECONDS", "HEALTH_PORT",
	} {
		os.Unsetenv(key)
	}
}

func setRequiredEnv(mode string) {
	os.Setenv("EXECUTION_MODE", mode)
	os.Setenv("WORKFLOW_ID", "wf-001")
	os.Setenv("PIPELINES_CONFIG", `[{"id":"p1","name":"test-pipeline"}]`)
	os.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	if mode == "batch" {
		os.Setenv("EXECUTION_ID", "exec-001")
	}
}

func TestLoadFromEnvBatch(t *testing.T) {
	clearEnv()
	setRequiredEnv("batch")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Mode != ModeBatch {
		t.Errorf("expected mode batch, got %s", cfg.Mode)
	}
	if cfg.WorkflowID != "wf-001" {
		t.Errorf("expected workflow ID wf-001, got %s", cfg.WorkflowID)
	}
	if cfg.ExecutionID != "exec-001" {
		t.Errorf("expected execution ID exec-001, got %s", cfg.ExecutionID)
	}
	if cfg.Workflow == nil {
		t.Fatal("workflow should not be nil")
	}
	if len(cfg.Workflow.Pipelines) != 1 {
		t.Errorf("expected 1 pipeline, got %d", len(cfg.Workflow.Pipelines))
	}
}

func TestLoadFromEnvStreaming(t *testing.T) {
	clearEnv()
	setRequiredEnv("streaming")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Mode != ModeStreaming {
		t.Errorf("expected mode streaming, got %s", cfg.Mode)
	}
}

func TestLoadFromEnvMissingWorkflowID(t *testing.T) {
	clearEnv()
	os.Setenv("EXECUTION_MODE", "batch")
	os.Setenv("PIPELINES_CONFIG", `[]`)
	os.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	defer clearEnv()

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing WORKFLOW_ID")
	}
}

func TestLoadFromEnvMissingExecutionIDForBatch(t *testing.T) {
	clearEnv()
	os.Setenv("EXECUTION_MODE", "batch")
	os.Setenv("WORKFLOW_ID", "wf-001")
	os.Setenv("PIPELINES_CONFIG", `[{"id":"p1"}]`)
	os.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	defer clearEnv()

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing EXECUTION_ID in batch mode")
	}
}

func TestLoadFromEnvInvalidMode(t *testing.T) {
	clearEnv()
	os.Setenv("EXECUTION_MODE", "invalid")
	os.Setenv("WORKFLOW_ID", "wf-001")
	os.Setenv("PIPELINES_CONFIG", `[]`)
	os.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	defer clearEnv()

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestLoadFromEnvCallbackURLDefault(t *testing.T) {
	clearEnv()
	setRequiredEnv("batch")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	expected := "http://localhost:8080/api/v1/internal/job-result"
	if cfg.CallbackURL != expected {
		t.Errorf("expected callback URL %s, got %s", expected, cfg.CallbackURL)
	}
}

func TestLoadFromEnvCustomTimeout(t *testing.T) {
	clearEnv()
	setRequiredEnv("batch")
	os.Setenv("TIMEOUT_SECONDS", "7200")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.TimeoutSeconds != 7200 {
		t.Errorf("expected timeout 7200, got %d", cfg.TimeoutSeconds)
	}
}

func TestLoadFromEnvDefaultHealthPort(t *testing.T) {
	clearEnv()
	setRequiredEnv("batch")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.HealthPort != 8082 {
		t.Errorf("expected health port 8082, got %d", cfg.HealthPort)
	}
}

func TestLoadFromEnvWorkflowJSON(t *testing.T) {
	clearEnv()
	os.Setenv("EXECUTION_MODE", "batch")
	os.Setenv("WORKFLOW_ID", "wf-full")
	os.Setenv("EXECUTION_ID", "exec-full")
	os.Setenv("PIPELINES_CONFIG", `{"id":"wf-full","name":"Full Workflow","type":"batch","pipelines":[{"id":"p1","name":"pipe1"}]}`)
	os.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	defer clearEnv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Workflow.Name != "Full Workflow" {
		t.Errorf("expected workflow name 'Full Workflow', got %s", cfg.Workflow.Name)
	}
}

func TestGetEnv(t *testing.T) {
	os.Unsetenv("TEST_KEY_LOADER")
	if v := getEnv("TEST_KEY_LOADER", "default"); v != "default" {
		t.Errorf("expected default, got %s", v)
	}

	os.Setenv("TEST_KEY_LOADER", "custom")
	defer os.Unsetenv("TEST_KEY_LOADER")
	if v := getEnv("TEST_KEY_LOADER", "default"); v != "custom" {
		t.Errorf("expected custom, got %s", v)
	}
}

func TestGetEnvInt64(t *testing.T) {
	os.Unsetenv("TEST_INT_LOADER")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}

	os.Setenv("TEST_INT_LOADER", "100")
	defer os.Unsetenv("TEST_INT_LOADER")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 100 {
		t.Errorf("expected 100, got %d", v)
	}

	os.Setenv("TEST_INT_LOADER", "invalid")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 42 {
		t.Errorf("expected 42 for invalid, got %d", v)
	}
}
