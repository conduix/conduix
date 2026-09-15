// Package config Pipeline Runner 설정 로더
// 환경변수 및 ConfigMap에서 파이프라인 실행 설정을 로드
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/conduix/conduix/shared/types"
)

// ExecutionMode 실행 모드
type ExecutionMode string

const (
	ModeBatch     ExecutionMode = "batch"
	ModeStreaming ExecutionMode = "streaming"
)

// RunnerConfig Pipeline Runner 설정
type RunnerConfig struct {
	// 실행 모드
	Mode ExecutionMode `json:"mode"`

	// 워크플로우/실행 식별
	WorkflowID  string `json:"workflow_id"`
	ExecutionID string `json:"execution_id,omitempty"` // batch only
	AgentID     string `json:"agent_id,omitempty"`     // 이 Job 을 위임 생성한 agent(노드) — 결과 콜백에 담아 분산 현황 노출

	// 파이프라인 설정
	Workflow *types.Workflow `json:"workflow,omitempty"`

	// Control Plane 연결
	ControlPlaneURL string `json:"control_plane_url"`
	CallbackURL     string `json:"callback_url,omitempty"` // batch only

	// 체크포인트
	CheckpointEndpoint string `json:"checkpoint_endpoint,omitempty"`

	// 타임아웃 (batch only, 초)
	TimeoutSeconds int64 `json:"timeout_seconds,omitempty"`

	// 헬스체크 포트
	HealthPort int `json:"health_port,omitempty"`

	// 파티션 분산: 이 batch sub-execution 이 처리할 파티션 ID 부분집합(비면 전체 — 현행).
	AssignedPartitions []string `json:"assigned_partitions,omitempty"`
}

// LoadFromEnv 환경변수에서 설정 로드
func LoadFromEnv() (*RunnerConfig, error) {
	mode := ExecutionMode(getEnv("EXECUTION_MODE", "batch"))
	if mode != ModeBatch && mode != ModeStreaming {
		return nil, fmt.Errorf("invalid EXECUTION_MODE: %s (expected batch or streaming)", mode)
	}

	// streaming 은 cluster 당 상주 파드다. 기동 시점에는 담당 실행이 없고,
	// 실행은 나중에 POST /executions 로 배정된다 — 그래서 실행에 종속된
	// WORKFLOW_ID/PIPELINES_CONFIG 를 기동 조건으로 요구하면 안 된다.
	// batch 는 Job 하나가 실행 하나라 여전히 필수다.
	workflowID := os.Getenv("WORKFLOW_ID")
	pipelinesConfig := os.Getenv("PIPELINES_CONFIG")
	if mode == ModeBatch {
		if workflowID == "" {
			return nil, fmt.Errorf("WORKFLOW_ID environment variable is required")
		}
		if pipelinesConfig == "" {
			return nil, fmt.Errorf("PIPELINES_CONFIG environment variable is required")
		}
	}

	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	callbackURL := os.Getenv("CALLBACK_URL")
	if controlPlaneURL == "" && callbackURL == "" {
		return nil, fmt.Errorf("CONTROL_PLANE_URL or CALLBACK_URL is required")
	}
	if callbackURL == "" && controlPlaneURL != "" {
		callbackURL = fmt.Sprintf("%s/api/v1/internal/job-result", controlPlaneURL)
	}

	cfg := &RunnerConfig{
		Mode:               mode,
		WorkflowID:         workflowID,
		ExecutionID:        os.Getenv("EXECUTION_ID"),
		AgentID:            os.Getenv("AGENT_ID"),
		ControlPlaneURL:    controlPlaneURL,
		CallbackURL:        callbackURL,
		CheckpointEndpoint: os.Getenv("CHECKPOINT_ENDPOINT"),
		TimeoutSeconds:     getEnvInt64("TIMEOUT_SECONDS", 3600),
		HealthPort:         int(getEnvInt64("HEALTH_PORT", 8082)),
		AssignedPartitions: splitAndTrim(os.Getenv("ASSIGNED_PARTITIONS")),
	}

	// batch 모드는 EXECUTION_ID 필수
	if mode == ModeBatch && cfg.ExecutionID == "" {
		return nil, fmt.Errorf("EXECUTION_ID is required for batch mode")
	}

	// 상주 streaming 파드는 기동 시 담당 워크플로가 없다. 설정은 배정 요청이
	// 실어 오므로 여기서는 파싱할 것이 없고, 빈 값을 파싱하면 실패한다.
	if pipelinesConfig != "" {
		workflow, err := parsePipelinesConfig(pipelinesConfig, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pipelines config: %w", err)
		}
		cfg.Workflow = workflow
	}

	return cfg, nil
}

// parsePipelinesConfig JSON 파이프라인 설정 파싱
func parsePipelinesConfig(raw string, cfg *RunnerConfig) (*types.Workflow, error) {
	// Workflow 전체 파싱 시도
	var workflow types.Workflow
	if err := json.Unmarshal([]byte(raw), &workflow); err == nil && workflow.ID != "" {
		return &workflow, nil
	}

	// WorkflowPipeline 배열 파싱 시도
	var pipelines []types.WorkflowPipeline
	if err := json.Unmarshal([]byte(raw), &pipelines); err != nil {
		return nil, fmt.Errorf("failed to parse as Workflow or []WorkflowPipeline: %w", err)
	}

	wfType := types.WorkflowTypeBatch
	if cfg.Mode == ModeStreaming {
		wfType = types.WorkflowTypeRealtime
	}

	return &types.Workflow{
		ID:        cfg.WorkflowID,
		Name:      fmt.Sprintf("runner-%s", cfg.WorkflowID),
		Type:      wfType,
		Pipelines: pipelines,
	}, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// splitAndTrim 은 콤마 구분 문자열을 트림된 비어있지 않은 항목 슬라이스로 나눈다(빈 입력 → nil).
func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func getEnvInt64(key string, defaultVal int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return n
}
