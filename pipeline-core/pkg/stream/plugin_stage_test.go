package stream

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPluginStage_MissingPluginName(t *testing.T) {
	_, err := NewPluginStage("test", map[string]any{})
	if err == nil {
		t.Error("expected error for missing plugin_name")
	}
}

func TestNewPluginStage_Valid(t *testing.T) {
	ps, err := NewPluginStage("test", map[string]any{
		"plugin_name": "my-plugin",
		"batch_size":  float64(500),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps.Name() != "test" {
		t.Errorf("expected name=test, got %s", ps.Name())
	}
	if ps.Type() != "plugin" {
		t.Errorf("expected type=plugin, got %s", ps.Type())
	}
	if ps.batchSize != 500 {
		t.Errorf("expected batchSize=500, got %d", ps.batchSize)
	}
}

func TestPluginStage_ProcessWithoutStart(t *testing.T) {
	ps, _ := NewPluginStage("test", map[string]any{
		"plugin_name": "my-plugin",
	})
	_, err := ps.Process(context.TODO(), &Record{Data: map[string]any{"key": "value"}})
	if err == nil {
		t.Error("expected error when processing without start")
	}
}

func TestStreamToSDKRecord(t *testing.T) {
	r := &Record{
		Data: map[string]any{"name": "alice", "score": 0.95},
		Metadata: RecordMetadata{
			Source:    "kafka",
			Key:       "key1",
			Partition: 3,
			Offset:    100,
		},
	}

	sdkR := streamToSDKRecord(r)
	if sdkR.Data["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", sdkR.Data["name"])
	}
	if sdkR.Metadata["source"] != "kafka" {
		t.Errorf("expected source=kafka, got %v", sdkR.Metadata["source"])
	}
	if sdkR.Metadata["partition"] != "3" {
		t.Errorf("expected partition=3, got %v", sdkR.Metadata["partition"])
	}
}

func TestCacheBinary(t *testing.T) {
	// Create a temp dir for the test
	tmpDir := t.TempDir()
	t.Setenv("CONDUIX_PLUGIN_CACHE_DIR", tmpDir)

	data := []byte("fake-binary-content")
	path, err := CacheBinary("test-plugin", "v1.0.0", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %s", path)
	}

	// Check file exists
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cached binary not found: %v", err)
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("expected size %d, got %d", len(data), info.Size())
	}

	// Cache again should return same path
	path2, err := CacheBinary("test-plugin", "v1.0.0", data)
	if err != nil {
		t.Fatalf("unexpected error on second cache: %v", err)
	}
	if path != path2 {
		t.Errorf("expected same path, got %s vs %s", path, path2)
	}
}

func TestPluginBinaryChecksum(t *testing.T) {
	data := []byte("hello world")
	checksum := pluginBinaryChecksum(data)

	if len(checksum) != 64 { // SHA256 hex string
		t.Errorf("expected 64 char checksum, got %d", len(checksum))
	}

	// Same data should produce same checksum
	checksum2 := pluginBinaryChecksum(data)
	if checksum != checksum2 {
		t.Errorf("expected same checksum, got %s vs %s", checksum, checksum2)
	}

	// Different data should produce different checksum
	checksum3 := pluginBinaryChecksum([]byte("different"))
	if checksum == checksum3 {
		t.Error("expected different checksums for different data")
	}
}
