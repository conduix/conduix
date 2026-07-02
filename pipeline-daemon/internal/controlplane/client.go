package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client Control Plane 통신 클라이언트
type Client struct {
	baseURL    string
	agentID    string
	clusterID  string
	httpClient *http.Client
}

// NewClient 클라이언트 생성
func NewClient(baseURL, agentID, clusterID string) *Client {
	return &Client{
		baseURL:   baseURL,
		agentID:   agentID,
		clusterID: clusterID,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// RegisterAgent Agent 등록
func (c *Client) RegisterAgent(ctx context.Context, req *AgentRegistration) error {
	return c.post(ctx, "/api/v1/agents/register", req, nil)
}

// SendHeartbeat 하트비트 전송
func (c *Client) SendHeartbeat(ctx context.Context, heartbeat *Heartbeat) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/heartbeat", c.agentID), heartbeat, nil)
}

// ReportMetrics 클러스터 메트릭 보고
func (c *Client) ReportMetrics(ctx context.Context, metrics *ClusterMetricsReport) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/agents/%s/metrics", c.agentID), metrics, nil)
}

// FetchCommands 명령 조회 (REST 폴백용)
func (c *Client) FetchCommands(ctx context.Context) ([]json.RawMessage, error) {
	var commands []json.RawMessage
	err := c.get(ctx, fmt.Sprintf("/api/v1/agents/%s/commands", c.agentID), &commands)
	if err != nil {
		return nil, err
	}
	return commands, nil
}

// ReportPipelineRunStatus PipelineRun 상태 업데이트
func (c *Client) ReportPipelineRunStatus(ctx context.Context, runID string, status *PipelineRunUpdate) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/pipeline-runs/%s/status", runID), status, nil)
}

// ReportEvent 이벤트 보고 (Job 완료, 에러 등)
func (c *Client) ReportEvent(ctx context.Context, event *AgentEvent) error {
	return c.post(ctx, "/api/v1/agents/events", event, nil)
}

// GetPluginImage stage type에 해당하는 플러그인 이미지 조회
func (c *Client) GetPluginImage(ctx context.Context, stageType string) (string, error) {
	var result struct {
		Image string `json:"image"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/v1/stages/%s/image", stageType), &result)
	if err != nil {
		return "", err
	}
	return result.Image, nil
}

// AgentRegistration Agent 등록 요청
type AgentRegistration struct {
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname"`
	ClusterID string   `json:"cluster_id"`
	Labels    []string `json:"labels,omitempty"`
	Version   string   `json:"version,omitempty"`
	IsLeader  bool     `json:"is_leader"`
}

// Heartbeat 하트비트 데이터
type Heartbeat struct {
	AgentID   string    `json:"agent_id"`
	ClusterID string    `json:"cluster_id"`
	Hostname  string    `json:"hostname"`
	IsLeader  bool      `json:"is_leader"`
	Timestamp time.Time `json:"timestamp"`

	// 관리 중인 리소스 현황
	ManagedJobs        int `json:"managed_jobs"`
	ManagedDeployments int `json:"managed_deployments"`
	ManagedCronJobs    int `json:"managed_cronjobs"`
}

// ClusterMetricsReport 클러스터 메트릭 보고
type ClusterMetricsReport struct {
	AgentID   string    `json:"agent_id"`
	ClusterID string    `json:"cluster_id"`
	Timestamp time.Time `json:"timestamp"`
	Metrics   any       `json:"metrics"` // monitor.ClusterMetrics
}

// PipelineRunUpdate PipelineRun 상태 업데이트
type PipelineRunUpdate struct {
	Status          string `json:"status"`                      // running, completed, failed
	K8sResourceType string `json:"k8s_resource_type,omitempty"` // job, cronjob, deployment
	K8sResourceName string `json:"k8s_resource_name,omitempty"`
	K8sNamespace    string `json:"k8s_namespace,omitempty"`
	RunnerImage     string `json:"runner_image,omitempty"`
	ProcessedCount  *int64 `json:"processed_count,omitempty"`
	ErrorCount      *int64 `json:"error_count,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// AgentEvent Agent 이벤트
type AgentEvent struct {
	AgentID   string         `json:"agent_id"`
	ClusterID string         `json:"cluster_id"`
	Type      string         `json:"type"` // job_completed, job_failed, deployment_ready, resource_warning
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// post HTTP POST 요청
func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// get HTTP GET 요청
func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content는 빈 결과
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
