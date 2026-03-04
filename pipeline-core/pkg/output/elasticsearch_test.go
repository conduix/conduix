// Package output Elasticsearch Output 테스트
package output

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func TestNewElasticsearchOutput(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.OutputConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"addresses": []interface{}{"http://localhost:9200"},
					"index":     "test-index",
				},
			},
			wantErr: false,
		},
		{
			name: "missing addresses",
			cfg: config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"index": "test-index",
				},
			},
			wantErr: true,
		},
		{
			name: "missing index",
			cfg: config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"addresses": []interface{}{"http://localhost:9200"},
				},
			},
			wantErr: true,
		},
		{
			name: "with template index",
			cfg: config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"addresses": []interface{}{"http://localhost:9200"},
					"index":     "events-{{ .date }}",
				},
			},
			wantErr: false,
		},
		{
			name: "with auth config",
			cfg: config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"addresses": []interface{}{"http://localhost:9200"},
					"index":     "test-index",
					"auth": map[string]interface{}{
						"type":     "basic",
						"username": "elastic",
						"password": "changeme",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := NewElasticsearchOutput(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewElasticsearchOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && out == nil {
				t.Error("NewElasticsearchOutput() returned nil output")
			}
		})
	}
}

func TestElasticsearchOutput_BulkIndex(t *testing.T) {
	// 모의 Elasticsearch 서버
	bulkCalled := false
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && r.Method == "GET":
			// Ping
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cluster_name": "test"}`))
		case r.URL.Path == "/_bulk" && r.Method == "POST":
			bulkCalled = true
			receivedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors": false, "items": [{"index": {"_id": "1", "status": 201}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Output 생성
	cfg := config.OutputConfig{
		Type: "elasticsearch",
		Config: map[string]interface{}{
			"addresses": []interface{}{server.URL},
			"index":     "test-index",
			"id_field":  "id",
			"bulk_size": 10,
		},
	}

	out, err := NewElasticsearchOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	// Open
	ctx := context.Background()
	if err := out.Open(ctx); err != nil {
		t.Fatalf("Failed to open output: %v", err)
	}
	defer func() { _ = out.Close() }()

	// Write records
	records := []source.Record{
		{Data: map[string]interface{}{"id": "1", "name": "test1"}},
		{Data: map[string]interface{}{"id": "2", "name": "test2"}},
	}

	for _, r := range records {
		if err := out.Write(ctx, r); err != nil {
			t.Errorf("Write() error = %v", err)
		}
	}

	// Flush
	if err := out.Flush(ctx); err != nil {
		t.Errorf("Flush() error = %v", err)
	}

	// 검증
	if !bulkCalled {
		t.Error("Bulk API was not called")
	}

	// NDJSON 형식 검증
	lines := strings.Split(strings.TrimSpace(string(receivedBody)), "\n")
	if len(lines) != 4 { // 2 records * 2 lines (meta + data)
		t.Errorf("Expected 4 lines in bulk body, got %d", len(lines))
	}

	// 첫 번째 메타 라인 검증
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Errorf("Failed to parse meta line: %v", err)
	}
	if _, ok := meta["index"]; !ok {
		t.Error("Meta line missing 'index' action")
	}
}

func TestElasticsearchOutput_WriteBatch(t *testing.T) {
	bulkCalled := false
	var itemCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/_bulk":
			bulkCalled = true
			body, _ := io.ReadAll(r.Body)
			lines := strings.Split(strings.TrimSpace(string(body)), "\n")
			itemCount = len(lines) / 2 // Each record has 2 lines
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors": false, "items": []}`))
		}
	}))
	defer server.Close()

	cfg := config.OutputConfig{
		Type: "elasticsearch",
		Config: map[string]interface{}{
			"addresses": []interface{}{server.URL},
			"index":     "test-index",
		},
	}

	out, err := NewElasticsearchOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	ctx := context.Background()
	if err := out.Open(ctx); err != nil {
		t.Fatalf("Failed to open output: %v", err)
	}
	defer func() { _ = out.Close() }()

	// WriteBatch 테스트
	records := []source.Record{
		{Data: map[string]interface{}{"field": "value1"}},
		{Data: map[string]interface{}{"field": "value2"}},
		{Data: map[string]interface{}{"field": "value3"}},
	}

	if err := out.WriteBatch(ctx, records); err != nil {
		t.Errorf("WriteBatch() error = %v", err)
	}

	if !bulkCalled {
		t.Error("Bulk API was not called for WriteBatch")
	}

	if itemCount != 3 {
		t.Errorf("Expected 3 items in bulk, got %d", itemCount)
	}
}

func TestElasticsearchOutput_SupportsBatch(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "elasticsearch",
		Config: map[string]interface{}{
			"addresses": []interface{}{"http://localhost:9200"},
			"index":     "test-index",
		},
	}

	out, err := NewElasticsearchOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	if !out.SupportsBatch() {
		t.Error("Elasticsearch output should support batch")
	}

	batchCfg := out.BatchConfig()
	if !batchCfg.Enabled {
		t.Error("Batch should be enabled")
	}
	if batchCfg.Format != "ndjson" {
		t.Errorf("Expected format 'ndjson', got '%s'", batchCfg.Format)
	}
}

func TestElasticsearchOutput_GetIndexName(t *testing.T) {
	// 정적 인덱스
	cfg1 := config.OutputConfig{
		Type: "elasticsearch",
		Config: map[string]interface{}{
			"addresses": []interface{}{"http://localhost:9200"},
			"index":     "static-index",
		},
	}

	out1, _ := NewElasticsearchOutput(cfg1)
	record := source.Record{Data: map[string]interface{}{"name": "test"}}

	indexName := out1.getIndexName(record)
	if indexName != "static-index" {
		t.Errorf("Expected 'static-index', got '%s'", indexName)
	}

	// 템플릿 인덱스
	cfg2 := config.OutputConfig{
		Type: "elasticsearch",
		Config: map[string]interface{}{
			"addresses": []interface{}{"http://localhost:9200"},
			"index":     "events-{{ .date }}",
		},
	}

	out2, _ := NewElasticsearchOutput(cfg2)
	indexName2 := out2.getIndexName(record)
	// 형식: events-YYYY.MM.DD
	if !strings.HasPrefix(indexName2, "events-") {
		t.Errorf("Expected index to start with 'events-', got '%s'", indexName2)
	}
}

func TestElasticsearchOutput_AuthHeaders(t *testing.T) {
	tests := []struct {
		name       string
		authConfig map[string]interface{}
		wantHeader string
	}{
		{
			name: "basic auth",
			authConfig: map[string]interface{}{
				"type":     "basic",
				"username": "user",
				"password": "pass",
			},
			wantHeader: "Basic",
		},
		{
			name: "api key auth",
			authConfig: map[string]interface{}{
				"type":    "api_key",
				"api_key": "my-api-key",
			},
			wantHeader: "ApiKey",
		},
		{
			name: "bearer auth",
			authConfig: map[string]interface{}{
				"type":   "bearer",
				"bearer": "my-token",
			},
			wantHeader: "Bearer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var authHeader string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authHeader = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"errors": false, "items": []}`))
			}))
			defer server.Close()

			cfg := config.OutputConfig{
				Type: "elasticsearch",
				Config: map[string]interface{}{
					"addresses": []interface{}{server.URL},
					"index":     "test-index",
					"auth":      tt.authConfig,
				},
			}

			out, _ := NewElasticsearchOutput(cfg)
			ctx := context.Background()
			_ = out.Open(ctx)
			defer func() { _ = out.Close() }()

			_ = out.Write(ctx, source.Record{Data: map[string]interface{}{"test": "data"}})
			_ = out.Flush(ctx)

			if !strings.Contains(authHeader, tt.wantHeader) {
				t.Errorf("Expected auth header to contain '%s', got '%s'", tt.wantHeader, authHeader)
			}
		})
	}
}
