// Package checkpoint 체크포인트 클라이언트
package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Checkpoint 체크포인트 데이터
type Checkpoint struct {
	ID           string    `json:"id,omitempty"`
	WorkflowID   string    `json:"workflow_id"`
	PipelineID   string    `json:"pipeline_id"`
	PipelineName string    `json:"pipeline_name,omitempty"`
	SourceType   string    `json:"source_type"`
	PartitionKey string    `json:"partition_key"`
	OffsetValue  string    `json:"offset_value"`
	OffsetType   string    `json:"offset_type"` // timestamp, numeric, string
	RecordCount  int64     `json:"record_count"`
	Metadata     string    `json:"metadata,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Client 체크포인트 클라이언트
type Client struct {
	baseURL    string
	httpClient *http.Client
	cache      map[string]*Checkpoint // key: pipelineID:partitionKey
	cacheMu    sync.RWMutex
	dirty      map[string]bool // 변경된 체크포인트 추적
	dirtyMu    sync.Mutex
}

// NewClient 새 체크포인트 클라이언트 생성
func NewClient(controlPlaneURL string) *Client {
	return &Client{
		baseURL: controlPlaneURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*Checkpoint),
		dirty: make(map[string]bool),
	}
}

// LoadCheckpoints 파이프라인의 체크포인트 로드
func (c *Client) LoadCheckpoints(ctx context.Context, pipelineID string) ([]*Checkpoint, error) {
	url := fmt.Sprintf("%s/api/v1/pipelines/%s/checkpoints", c.baseURL, pipelineID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // 체크포인트 없음
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool          `json:"success"`
		Data    []*Checkpoint `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 캐시에 저장
	c.cacheMu.Lock()
	for _, cp := range response.Data {
		key := fmt.Sprintf("%s:%s", cp.PipelineID, cp.PartitionKey)
		c.cache[key] = cp
	}
	c.cacheMu.Unlock()

	return response.Data, nil
}

// GetCheckpoint 특정 파티션의 체크포인트 조회 (캐시 우선)
func (c *Client) GetCheckpoint(pipelineID, partitionKey string) *Checkpoint {
	key := fmt.Sprintf("%s:%s", pipelineID, partitionKey)

	c.cacheMu.RLock()
	cp := c.cache[key]
	c.cacheMu.RUnlock()

	return cp
}

// UpdateCheckpoint 체크포인트 업데이트 (메모리 캐시)
func (c *Client) UpdateCheckpoint(cp *Checkpoint) {
	key := fmt.Sprintf("%s:%s", cp.PipelineID, cp.PartitionKey)

	c.cacheMu.Lock()
	c.cache[key] = cp
	c.cacheMu.Unlock()

	c.dirtyMu.Lock()
	c.dirty[key] = true
	c.dirtyMu.Unlock()
}

// SaveCheckpoint 체크포인트를 즉시 서버에 저장
func (c *Client) SaveCheckpoint(ctx context.Context, cp *Checkpoint) error {
	url := fmt.Sprintf("%s/api/v1/pipelines/%s/checkpoints", c.baseURL, cp.PipelineID)

	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	// dirty 플래그 제거
	key := fmt.Sprintf("%s:%s", cp.PipelineID, cp.PartitionKey)
	c.dirtyMu.Lock()
	delete(c.dirty, key)
	c.dirtyMu.Unlock()

	return nil
}

// FlushCheckpoints 모든 dirty 체크포인트를 서버에 저장
func (c *Client) FlushCheckpoints(ctx context.Context) error {
	c.dirtyMu.Lock()
	dirtyKeys := make([]string, 0, len(c.dirty))
	for k := range c.dirty {
		dirtyKeys = append(dirtyKeys, k)
	}
	c.dirtyMu.Unlock()

	if len(dirtyKeys) == 0 {
		return nil
	}

	var errs []error
	for _, key := range dirtyKeys {
		c.cacheMu.RLock()
		cp := c.cache[key]
		c.cacheMu.RUnlock()

		if cp != nil {
			if err := c.SaveCheckpoint(ctx, cp); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to flush %d checkpoints: %v", len(errs), errs)
	}

	return nil
}

// StartPeriodicFlush 주기적으로 체크포인트 저장 (백그라운드)
func (c *Client) StartPeriodicFlush(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// 종료 전 마지막 flush
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.FlushCheckpoints(flushCtx)
				cancel()
				return
			case <-ticker.C:
				if err := c.FlushCheckpoints(ctx); err != nil {
					slog.Default().Error("checkpoint flush error", "base_url", c.baseURL, "error", err)
				}
			}
		}
	}()
}

// ClearCache 캐시 초기화
func (c *Client) ClearCache() {
	c.cacheMu.Lock()
	c.cache = make(map[string]*Checkpoint)
	c.cacheMu.Unlock()

	c.dirtyMu.Lock()
	c.dirty = make(map[string]bool)
	c.dirtyMu.Unlock()
}
