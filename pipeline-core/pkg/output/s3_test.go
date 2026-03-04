// Package output S3 Output 테스트
package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func TestNewS3Output(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.OutputConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket": "test-bucket",
					"region": "ap-northeast-2",
				},
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"region": "ap-northeast-2",
				},
			},
			wantErr: true,
		},
		{
			name: "with path template",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":        "test-bucket",
					"path_template": "events/year={{ .year }}/month={{ .month }}/",
				},
			},
			wantErr: false,
		},
		{
			name: "with all options",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":        "test-bucket",
					"region":        "us-west-2",
					"path_template": "data/{{ .year }}/{{ .month }}/",
					"file_format":   "json",
					"compression":   "gzip",
					"partition_by":  []interface{}{"event_type", "region"},
					"max_file_size": "64MB",
					"max_file_age":  "30m",
					"batch_size":    500,
				},
			},
			wantErr: false,
		},
		{
			name: "with minio endpoint",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":            "test-bucket",
					"endpoint":          "http://localhost:9000",
					"access_key_id":     "minioadmin",
					"secret_access_key": "minioadmin",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid max_file_size",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":        "test-bucket",
					"max_file_size": "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max_file_age",
			cfg: config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":       "test-bucket",
					"max_file_age": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := NewS3Output(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewS3Output() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && out == nil {
				t.Error("NewS3Output() returned nil output")
			}
		})
	}
}

func TestS3Output_SupportsBatch(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket": "test-bucket",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	if !out.SupportsBatch() {
		t.Error("S3 output should support batch")
	}

	batchCfg := out.BatchConfig()
	if !batchCfg.Enabled {
		t.Error("Batch should be enabled")
	}
}

func TestS3Output_SerializeNDJSON(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":      "test-bucket",
			"file_format": "ndjson",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	records := []source.Record{
		{Data: map[string]interface{}{"id": "1", "name": "test1"}},
		{Data: map[string]interface{}{"id": "2", "name": "test2"}},
	}

	data, err := out.SerializeRecordsForTest(records)
	if err != nil {
		t.Fatalf("Serialization error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}

	// 각 라인이 유효한 JSON인지 확인
	for _, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("Invalid JSON line: %s", line)
		}
	}
}

func TestS3Output_SerializeJSON(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":      "test-bucket",
			"file_format": "json",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	records := []source.Record{
		{Data: map[string]interface{}{"id": "1", "name": "test1"}},
		{Data: map[string]interface{}{"id": "2", "name": "test2"}},
	}

	data, err := out.SerializeRecordsForTest(records)
	if err != nil {
		t.Fatalf("Serialization error: %v", err)
	}

	// JSON 배열인지 확인
	var arr []map[string]interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Errorf("Invalid JSON array: %v", err)
	}

	if len(arr) != 2 {
		t.Errorf("Expected 2 items, got %d", len(arr))
	}
}

func TestS3Output_SerializeCSV(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":      "test-bucket",
			"file_format": "csv",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	records := []source.Record{
		{Data: map[string]interface{}{"id": "1", "name": "test1"}},
		{Data: map[string]interface{}{"id": "2", "name": "test2"}},
	}

	data, err := out.SerializeRecordsForTest(records)
	if err != nil {
		t.Fatalf("Serialization error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 헤더 + 2 데이터 행
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines (header + 2 data), got %d", len(lines))
	}
}

func TestS3Output_CompressGzip(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":      "test-bucket",
			"compression": "gzip",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	original := []byte("test data for compression")
	compressed, err := out.CompressGzipForTest(original)
	if err != nil {
		t.Fatalf("Compression error: %v", err)
	}

	// 압축된 데이터는 원본보다 작거나 gzip 헤더가 있어야 함
	if len(compressed) == 0 {
		t.Error("Compressed data is empty")
	}

	// 압축 해제 테스트
	decompressed, err := DecompressGzip(compressed)
	if err != nil {
		t.Fatalf("Decompression error: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("Decompressed data doesn't match original")
	}
}

func TestS3Output_GenerateKey(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":        "test-bucket",
			"path_template": "events/year={{ .year }}/month={{ .month }}/",
			"file_format":   "ndjson",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	record := source.Record{Data: map[string]interface{}{"name": "test"}}
	key := out.GenerateKeyForTest("", record)

	// 키가 템플릿 패턴을 따르는지 확인
	if !strings.HasPrefix(key, "events/year=") {
		t.Errorf("Key doesn't start with expected prefix: %s", key)
	}
	if !strings.HasSuffix(key, ".ndjson") {
		t.Errorf("Key doesn't end with .ndjson: %s", key)
	}
}

func TestS3Output_PartitionKey(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":       "test-bucket",
			"partition_by": []interface{}{"event_type", "region"},
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	record := source.Record{
		Data: map[string]interface{}{
			"event_type": "click",
			"region":     "us-east-1",
			"user_id":    "123",
		},
	}

	key := out.getPartitionKey(record)
	expected := "event_type=click/region=us-east-1"
	if key != expected {
		t.Errorf("Expected partition key '%s', got '%s'", expected, key)
	}
}

func TestS3Output_PartitionRecords(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket":       "test-bucket",
			"partition_by": []interface{}{"region"},
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	records := []source.Record{
		{Data: map[string]interface{}{"id": "1", "region": "us-east-1"}},
		{Data: map[string]interface{}{"id": "2", "region": "us-west-2"}},
		{Data: map[string]interface{}{"id": "3", "region": "us-east-1"}},
	}

	partitions := out.partitionRecords(records)

	if len(partitions) != 2 {
		t.Errorf("Expected 2 partitions, got %d", len(partitions))
	}

	usEast1 := partitions["region=us-east-1"]
	if len(usEast1) != 2 {
		t.Errorf("Expected 2 records in us-east-1, got %d", len(usEast1))
	}

	usWest2 := partitions["region=us-west-2"]
	if len(usWest2) != 1 {
		t.Errorf("Expected 1 record in us-west-2, got %d", len(usWest2))
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"128MB", 128 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"512KB", 512 * 1024, false},
		{"100M", 100 * 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseSize(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestS3Output_GetContentType(t *testing.T) {
	tests := []struct {
		format   string
		expected string
	}{
		{"json", "application/json"},
		{"ndjson", "application/x-ndjson"},
		{"jsonl", "application/x-ndjson"},
		{"csv", "text/csv"},
		{"unknown", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			cfg := config.OutputConfig{
				Type: "s3",
				Config: map[string]interface{}{
					"bucket":      "test-bucket",
					"file_format": tt.format,
				},
			}

			out, _ := NewS3Output(cfg)
			contentType := out.getContentType()

			if contentType != tt.expected {
				t.Errorf("getContentType() = %s, want %s", contentType, tt.expected)
			}
		})
	}
}

func TestS3Output_DefaultValues(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "s3",
		Config: map[string]interface{}{
			"bucket": "test-bucket",
		},
	}

	out, err := NewS3Output(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	// 기본값 확인
	if out.region != "us-east-1" {
		t.Errorf("Expected default region 'us-east-1', got '%s'", out.region)
	}
	if out.fileFormat != "ndjson" {
		t.Errorf("Expected default format 'ndjson', got '%s'", out.fileFormat)
	}
	if out.compression != "none" {
		t.Errorf("Expected default compression 'none', got '%s'", out.compression)
	}
	if out.batchSize != 1000 {
		t.Errorf("Expected default batch size 1000, got %d", out.batchSize)
	}
	if out.maxFileSize != 128*1024*1024 {
		t.Errorf("Expected default max file size 128MB, got %d", out.maxFileSize)
	}
}
