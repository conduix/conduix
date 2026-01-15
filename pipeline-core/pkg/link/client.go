package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// PipelineLink Pipeline Link Information
type PipelineLink struct {
	ID               string    `json:"id"`
	WorkflowID       string    `json:"workflow_id"`
	ParentPipelineID string    `json:"parent_pipeline_id"`
	ChildPipelineID  string    `json:"child_pipeline_id"`
	KafkaTopic       string    `json:"kafka_topic"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	// Note: kafka_brokers removed - use GetKafkaBrokers() to fetch from environment
}

// Client PipelineLink API Client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates new client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetLinksByWorkflow gets all links for workflow
func (c *Client) GetLinksByWorkflow(ctx context.Context, workflowID string) ([]PipelineLink, error) {
	url := fmt.Sprintf("%s/api/v1/pipeline-links/workflow/%s", c.baseURL, workflowID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []PipelineLink{}, nil
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool           `json:"success"`
		Data    []PipelineLink `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned success=false")
	}

	return response.Data, nil
}

// GetLinksByParent gets links for parent pipeline
func (c *Client) GetLinksByParent(ctx context.Context, parentPipelineID string) ([]PipelineLink, error) {
	url := fmt.Sprintf("%s/api/v1/pipeline-links/parent/%s", c.baseURL, parentPipelineID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []PipelineLink{}, nil
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool           `json:"success"`
		Data    []PipelineLink `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned success=false")
	}

	return response.Data, nil
}

// GetLinksByChild gets links for child pipeline
func (c *Client) GetLinksByChild(ctx context.Context, childPipelineID string) ([]PipelineLink, error) {
	url := fmt.Sprintf("%s/api/v1/pipeline-links/child/%s", c.baseURL, childPipelineID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []PipelineLink{}, nil
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool           `json:"success"`
		Data    []PipelineLink `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned success=false")
	}

	return response.Data, nil
}

// GetKafkaBrokers gets Kafka brokers from environment
// This is a centralized way to fetch Kafka brokers instead of storing in DB
func GetKafkaBrokers() []string {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		// Default to localhost if not set
		return []string{"localhost:9092"}
	}

	// Support comma-separated list
	parts := strings.Split(brokers, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
