package stream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"

	sdk "github.com/conduix/conduix/plugin-sdk"
)

// PluginStage wraps an external plugin binary as a pipeline Stage.
// It uses HashiCorp go-plugin to launch and communicate with the binary via gRPC.
type PluginStage struct {
	name       string
	pluginName string
	config     map[string]any
	binaryPath string
	client     *plugin.Client
	stage      sdk.Stage
	batchSize  int
	logger     *slog.Logger
	mu         sync.Mutex
}

// PluginBinaryProvider is the interface for retrieving plugin binaries.
// Implementations can fetch from MySQL BLOB, OCI registry, or local cache.
type PluginBinaryProvider interface {
	// GetBinary returns the binary data for a plugin by name/version/platform.
	GetBinary(ctx context.Context, pluginName, version, platform string) ([]byte, error)
}

// pluginCacheDir returns the directory for caching plugin binaries.
func pluginCacheDir() string {
	dir := os.Getenv("CONDUIX_PLUGIN_CACHE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "conduix-plugins")
	}
	return dir
}

// NewPluginStage creates a new PluginStage from configuration.
// Config expected:
//
//	plugin_name: string (required)
//	binary_path: string (optional, for local testing)
//	binary_url:  string (optional, control-plane API URL)
//	version:     string (optional, default "latest")
//	batch_size:  int    (optional, default 1000)
//	config:      map    (optional, passed to plugin Init)
func NewPluginStage(name string, config map[string]any) (*PluginStage, error) {
	pluginName, _ := config["plugin_name"].(string)
	if pluginName == "" {
		return nil, fmt.Errorf("plugin_name is required")
	}

	binaryPath, _ := config["binary_path"].(string)
	batchSize := 1000
	if bs, ok := config["batch_size"].(float64); ok {
		batchSize = int(bs)
	}

	return &PluginStage{
		name:       name,
		pluginName: pluginName,
		config:     config,
		binaryPath: binaryPath,
		batchSize:  batchSize,
		logger:     slog.Default().With("stage", name, "plugin", pluginName),
	}, nil
}

// Name returns the stage name.
func (s *PluginStage) Name() string { return s.name }

// Type returns "plugin".
func (s *PluginStage) Type() string { return "plugin" }

// Start launches the plugin process and initializes it.
func (s *PluginStage) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.binaryPath == "" {
		return fmt.Errorf("binary_path is required (binary download not yet implemented)")
	}

	// Verify binary exists and is executable
	info, err := os.Stat(s.binaryPath)
	if err != nil {
		return fmt.Errorf("plugin binary not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("plugin binary path is a directory")
	}

	// Launch plugin process via go-plugin
	s.client = plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: sdk.Handshake,
		Plugins: map[string]plugin.Plugin{
			"stage": &sdk.StagePluginImpl{},
		},
		Cmd:              exec.Command(s.binaryPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           nil, // Uses hclog default
	})

	// Connect
	rpcClient, err := s.client.Client()
	if err != nil {
		s.client.Kill()
		return fmt.Errorf("plugin connect: %w", err)
	}

	// Get the Stage interface
	raw, err := rpcClient.Dispense("stage")
	if err != nil {
		s.client.Kill()
		return fmt.Errorf("plugin dispense: %w", err)
	}

	stage, ok := raw.(sdk.Stage)
	if !ok {
		s.client.Kill()
		return fmt.Errorf("plugin does not implement Stage interface")
	}

	s.stage = stage

	// Initialize with plugin-specific config
	pluginConfig, _ := s.config["config"].(map[string]any)
	if pluginConfig == nil {
		pluginConfig = make(map[string]any)
	}

	if err := s.stage.Init(pluginConfig); err != nil {
		s.client.Kill()
		return fmt.Errorf("plugin init: %w", err)
	}

	s.logger.Info("Plugin stage started", "binary", s.binaryPath)
	return nil
}

// Process processes a single record through the plugin.
// For better performance, use ProcessBatch directly.
func (s *PluginStage) Process(_ context.Context, record *Record) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stage == nil {
		return nil, fmt.Errorf("plugin not started")
	}

	// Convert stream.Record to sdk.Record
	sdkRecord := streamToSDKRecord(record)

	results, err := s.stage.ProcessBatch([]*sdk.Record{sdkRecord})
	if err != nil {
		return nil, fmt.Errorf("plugin process: %w", err)
	}

	if len(results) == 0 {
		return nil, nil // Filtered out
	}

	return sdkToStreamRecord(results[0], record), nil
}

// ProcessBatch processes multiple records at once for better throughput.
func (s *PluginStage) ProcessBatch(ctx context.Context, records []*Record) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stage == nil {
		return nil, fmt.Errorf("plugin not started")
	}

	// Convert to SDK records
	sdkRecords := make([]*sdk.Record, 0, len(records))
	for _, r := range records {
		sdkRecords = append(sdkRecords, streamToSDKRecord(r))
	}

	// Process in batches
	var allResults []*Record
	for i := 0; i < len(sdkRecords); i += s.batchSize {
		end := min(i+s.batchSize, len(sdkRecords))

		select {
		case <-ctx.Done():
			return allResults, ctx.Err()
		default:
		}

		batch := sdkRecords[i:end]
		results, err := s.stage.ProcessBatch(batch)
		if err != nil {
			return allResults, fmt.Errorf("plugin process batch: %w", err)
		}

		for _, r := range results {
			allResults = append(allResults, sdkToStreamRecord(r, nil))
		}
	}

	return allResults, nil
}

// Close stops the plugin process.
func (s *PluginStage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stage != nil {
		if err := s.stage.Close(); err != nil {
			s.logger.Warn("Plugin close error", "error", err)
		}
	}

	if s.client != nil {
		s.client.Kill()
	}

	s.logger.Info("Plugin stage stopped")
	return nil
}

// streamToSDKRecord converts a stream.Record to an sdk.Record.
func streamToSDKRecord(r *Record) *sdk.Record {
	metadata := map[string]string{
		"source":    r.Metadata.Source,
		"key":       r.Metadata.Key,
		"partition": fmt.Sprintf("%d", r.Metadata.Partition),
		"offset":    fmt.Sprintf("%d", r.Metadata.Offset),
	}

	return &sdk.Record{
		Data:     r.Data,
		Metadata: metadata,
	}
}

// sdkToStreamRecord converts an sdk.Record back to a stream.Record.
// If original is provided, metadata is preserved from it.
func sdkToStreamRecord(r *sdk.Record, original *Record) *Record {
	result := &Record{
		Data:      r.Data,
		Timestamp: time.Now(),
	}

	if original != nil {
		result.Metadata = original.Metadata
		result.Timestamp = original.Timestamp
	} else if r.Metadata != nil {
		result.Metadata.Source = r.Metadata["source"]
		result.Metadata.Key = r.Metadata["key"]
	}

	return result
}

// CacheBinary saves a plugin binary to the local cache directory.
// Returns the path to the cached binary.
func CacheBinary(pluginName, version string, data []byte) (string, error) {
	cacheDir := pluginCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:4]) // Short checksum for filename

	filename := fmt.Sprintf("%s-%s-%s.bin", pluginName, version, checksum)
	binaryPath := filepath.Join(cacheDir, filename)

	// Check if already cached
	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	if err := os.WriteFile(binaryPath, data, 0o755); err != nil {
		return "", fmt.Errorf("write binary: %w", err)
	}

	return binaryPath, nil
}

// NewPluginStageFromConfig creates a PluginStage from a StageConfig.
// This is called by the NewStage factory.
func NewPluginStageFromConfig(name string, config map[string]any) (Stage, error) {
	ps, err := NewPluginStage(name, config)
	if err != nil {
		return nil, err
	}

	// Auto-start if binary_path is provided
	if ps.binaryPath != "" {
		if err := ps.Start(); err != nil {
			return nil, err
		}
	}

	return ps, nil
}

// pluginBinaryChecksum returns the SHA256 checksum of binary data.
func pluginBinaryChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
