package config

import (
	"testing"
)

// t.Setenv 를 쓴다 — os.Setenv 는 에러를 반환하고(errcheck), 테스트 종료 시 값을
// 되돌리지 않아 뒤 테스트에 값이 새어 나간다. t.Setenv 는 자동 복원하므로
// defer 로 지우는 코드도 필요 없다.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"EXECUTION_MODE", "WORKFLOW_ID", "EXECUTION_ID",
		"PIPELINES_CONFIG", "CONTROL_PLANE_URL", "CALLBACK_URL",
		"CHECKPOINT_ENDPOINT", "TIMEOUT_SECONDS", "HEALTH_PORT",
	} {
		// 로더는 os.Getenv 만 쓰므로 빈 문자열과 미설정을 구분하지 않는다 —
		// 빈 값으로 두면 "설정 안 됨" 과 같은 효과다.
		t.Setenv(key, "")
	}
}

func setRequiredEnv(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("EXECUTION_MODE", mode)
	t.Setenv("WORKFLOW_ID", "wf-001")
	t.Setenv("PIPELINES_CONFIG", `[{"id":"p1","name":"test-pipeline"}]`)
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")
	if mode == "batch" {
		t.Setenv("EXECUTION_ID", "exec-001")
	}
}

func TestLoadFromEnvBatch(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t, "batch")

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
	clearEnv(t)
	setRequiredEnv(t, "streaming")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Mode != ModeStreaming {
		t.Errorf("expected mode streaming, got %s", cfg.Mode)
	}
}

func TestLoadFromEnvMissingWorkflowID(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXECUTION_MODE", "batch")
	t.Setenv("PIPELINES_CONFIG", `[]`)
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing WORKFLOW_ID")
	}
}

func TestLoadFromEnvMissingExecutionIDForBatch(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXECUTION_MODE", "batch")
	t.Setenv("WORKFLOW_ID", "wf-001")
	t.Setenv("PIPELINES_CONFIG", `[{"id":"p1"}]`)
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing EXECUTION_ID in batch mode")
	}
}

func TestLoadFromEnvInvalidMode(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXECUTION_MODE", "invalid")
	t.Setenv("WORKFLOW_ID", "wf-001")
	t.Setenv("PIPELINES_CONFIG", `[]`)
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestLoadFromEnvCallbackURLDefault(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t, "batch")

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
	clearEnv(t)
	setRequiredEnv(t, "batch")
	t.Setenv("TIMEOUT_SECONDS", "7200")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.TimeoutSeconds != 7200 {
		t.Errorf("expected timeout 7200, got %d", cfg.TimeoutSeconds)
	}
}

func TestLoadFromEnvDefaultHealthPort(t *testing.T) {
	clearEnv(t)
	setRequiredEnv(t, "batch")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.HealthPort != 8082 {
		t.Errorf("expected health port 8082, got %d", cfg.HealthPort)
	}
}

func TestLoadFromEnvWorkflowJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXECUTION_MODE", "batch")
	t.Setenv("WORKFLOW_ID", "wf-full")
	t.Setenv("EXECUTION_ID", "exec-full")
	t.Setenv("PIPELINES_CONFIG", `{"id":"wf-full","name":"Full Workflow","type":"batch","pipelines":[{"id":"p1","name":"pipe1"}]}`)
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:8080")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Workflow.Name != "Full Workflow" {
		t.Errorf("expected workflow name 'Full Workflow', got %s", cfg.Workflow.Name)
	}
}

func TestGetEnv(t *testing.T) {
	t.Setenv("TEST_KEY_LOADER", "")
	if v := getEnv("TEST_KEY_LOADER", "default"); v != "default" {
		t.Errorf("expected default, got %s", v)
	}

	t.Setenv("TEST_KEY_LOADER", "custom")
	if v := getEnv("TEST_KEY_LOADER", "default"); v != "custom" {
		t.Errorf("expected custom, got %s", v)
	}
}

func TestGetEnvInt64(t *testing.T) {
	t.Setenv("TEST_INT_LOADER", "")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}

	t.Setenv("TEST_INT_LOADER", "100")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 100 {
		t.Errorf("expected 100, got %d", v)
	}

	t.Setenv("TEST_INT_LOADER", "invalid")
	if v := getEnvInt64("TEST_INT_LOADER", 42); v != 42 {
		t.Errorf("expected 42 for invalid, got %d", v)
	}
}
