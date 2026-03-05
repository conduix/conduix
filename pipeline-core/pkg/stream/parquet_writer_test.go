package stream

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewParquetWriter_BasicConfig(t *testing.T) {
	config := ParquetWriterConfig{
		Path:        "/tmp/test.parquet",
		Compression: "snappy",
	}

	writer, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	if writer.path != "/tmp/test.parquet" {
		t.Errorf("expected path '/tmp/test.parquet', got '%s'", writer.path)
	}

	if writer.compression != "snappy" {
		t.Errorf("expected compression 'snappy', got '%s'", writer.compression)
	}
}

func TestNewParquetWriter_DefaultValues(t *testing.T) {
	config := ParquetWriterConfig{
		Path: "/tmp/test.parquet",
	}

	writer, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Default compression
	if writer.compression != "snappy" {
		t.Errorf("expected default compression 'snappy', got '%s'", writer.compression)
	}

	// Default row group size (128MB)
	if writer.rowGroupSize != 128*1024*1024 {
		t.Errorf("expected default row group size 128MB, got %d", writer.rowGroupSize)
	}

	// Default max records per file
	if writer.maxRecordsPerFile != 1000000 {
		t.Errorf("expected default max records 1000000, got %d", writer.maxRecordsPerFile)
	}
}

func TestNewParquetWriter_RequiresPath(t *testing.T) {
	config := ParquetWriterConfig{}

	_, err := NewParquetWriter(config)
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestInferSchema(t *testing.T) {
	data := map[string]any{
		"name":   "test",
		"count":  int64(42),
		"active": true,
		"score":  3.14,
	}

	schema := inferSchema(data)

	if len(schema) != 4 {
		t.Errorf("expected 4 fields, got %d", len(schema))
	}

	// Check field types
	typeMap := make(map[string]string)
	for _, f := range schema {
		typeMap[f.Name] = f.Type
	}

	if typeMap["name"] != "string" {
		t.Errorf("expected 'name' type 'string', got '%s'", typeMap["name"])
	}
	if typeMap["count"] != "int64" {
		t.Errorf("expected 'count' type 'int64', got '%s'", typeMap["count"])
	}
	if typeMap["active"] != "boolean" {
		t.Errorf("expected 'active' type 'boolean', got '%s'", typeMap["active"])
	}
	if typeMap["score"] != "float64" {
		t.Errorf("expected 'score' type 'float64', got '%s'", typeMap["score"])
	}
}

func TestInferType(t *testing.T) {
	tests := []struct {
		value    any
		expected string
	}{
		{"hello", "string"},
		{int(42), "int32"},
		{int32(42), "int32"},
		{int64(42), "int64"},
		{float32(3.14), "float32"},
		{float64(3.14), "float64"},
		{true, "boolean"},
		{[]byte("bytes"), "bytes"},
		{map[string]any{"a": 1}, "string"}, // complex types become JSON strings
		{[]any{1, 2, 3}, "string"},
		{nil, "string"}, // nil defaults to string
	}

	for _, tt := range tests {
		result := inferType(tt.value)
		if result != tt.expected {
			t.Errorf("inferType(%v) = %s, expected %s", tt.value, result, tt.expected)
		}
	}
}

func TestNewParquetFileSink_BasicConfig(t *testing.T) {
	config := map[string]any{
		"path":        "/tmp/test-output",
		"compression": "gzip",
	}

	sink := NewParquetFileSink("test-parquet", config)

	if sink.Name() != "test-parquet" {
		t.Errorf("expected name 'test-parquet', got '%s'", sink.Name())
	}

	if sink.Type() != "parquet" {
		t.Errorf("expected type 'parquet', got '%s'", sink.Type())
	}

	if sink.config.Path != "/tmp/test-output" {
		t.Errorf("expected path '/tmp/test-output', got '%s'", sink.config.Path)
	}

	if sink.config.Compression != "gzip" {
		t.Errorf("expected compression 'gzip', got '%s'", sink.config.Compression)
	}
}

func TestNewParquetFileSink_WithSchema(t *testing.T) {
	config := map[string]any{
		"path": "/tmp/test-output",
		"schema": []any{
			map[string]any{
				"name": "id",
				"type": "int64",
			},
			map[string]any{
				"name":           "name",
				"type":           "string",
				"converted_type": "UTF8",
			},
		},
	}

	sink := NewParquetFileSink("test-parquet", config)

	if len(sink.config.Schema) != 2 {
		t.Errorf("expected 2 schema fields, got %d", len(sink.config.Schema))
	}

	if sink.config.Schema[0].Name != "id" {
		t.Errorf("expected first field name 'id', got '%s'", sink.config.Schema[0].Name)
	}

	if sink.config.Schema[0].Type != "int64" {
		t.Errorf("expected first field type 'int64', got '%s'", sink.config.Schema[0].Type)
	}
}

func TestParquetWriter_WriteAndClose(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "parquet-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	config := ParquetWriterConfig{
		Path:        filepath.Join(tmpDir, "test.parquet"),
		Compression: "snappy",
	}

	writer, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write some records
	ctx := context.Background()
	records := []*Record{
		{Data: map[string]any{"id": int64(1), "name": "Alice"}},
		{Data: map[string]any{"id": int64(2), "name": "Bob"}},
		{Data: map[string]any{"id": int64(3), "name": "Charlie"}},
	}

	for _, r := range records {
		if err := writer.Write(ctx, r); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}
	}

	// Close
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	// Verify file was created
	files, err := filepath.Glob(filepath.Join(tmpDir, "*.parquet"))
	if err != nil {
		t.Fatalf("failed to glob files: %v", err)
	}

	if len(files) == 0 {
		t.Error("expected parquet file to be created")
	}

	// Check file size is greater than 0
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	if info.Size() == 0 {
		t.Error("parquet file is empty")
	}
}

func TestGetCompressionType(t *testing.T) {
	tests := []struct {
		compression string
		expected    string
	}{
		{"snappy", "SNAPPY"},
		{"gzip", "GZIP"},
		{"zstd", "ZSTD"},
		{"lz4", "LZ4"},
		{"none", "UNCOMPRESSED"},
		{"uncompressed", "UNCOMPRESSED"},
		{"unknown", "SNAPPY"}, // default
	}

	for _, tt := range tests {
		writer := &ParquetWriter{compression: tt.compression}
		codec := writer.getCompressionType()
		codecName := codec.String()

		if codecName != tt.expected {
			t.Errorf("compression '%s': expected %s, got %s",
				tt.compression, tt.expected, codecName)
		}
	}
}
