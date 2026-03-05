package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewCDCSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Port:     3306,
		Username: "repl_user",
		Password: "repl_pass",
		Database: "testdb",
		Tables:   []string{"users", "orders"},
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "cdc" {
		t.Errorf("expected name 'cdc', got '%s'", source.Name())
	}

	if source.driver != "mysql" {
		t.Errorf("expected driver 'mysql', got '%s'", source.driver)
	}

	if source.host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", source.host)
	}

	if source.port != 3306 {
		t.Errorf("expected port 3306, got %d", source.port)
	}

	if len(source.tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(source.tables))
	}
}

func TestNewCDCSource_DefaultPort(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default port should be 3306
	if source.port != 3306 {
		t.Errorf("expected default port 3306, got %d", source.port)
	}
}

func TestNewCDCSource_CustomPort(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Port:     3307,
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.port != 3307 {
		t.Errorf("expected port 3307, got %d", source.port)
	}
}

func TestNewCDCSource_ServerID(t *testing.T) {
	tests := []struct {
		name       string
		serverID   uint32
		expectedID uint32
	}{
		{"default server ID", 0, 101},
		{"custom server ID", 12345, 12345},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SourceV2{
				Type:     "cdc",
				Driver:   "mysql",
				Host:     "localhost",
				Username: "user",
				Password: "pass",
				Database: "testdb",
				ServerID: tt.serverID,
			}

			source, err := NewCDCSource(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source.serverID != tt.expectedID {
				t.Errorf("expected server ID %d, got %d", tt.expectedID, source.serverID)
			}
		})
	}
}

func TestNewCDCSource_PostgreSQL(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "postgres",
		Host:     "localhost",
		Port:     5432,
		Username: "repl_user",
		Password: "repl_pass",
		Database: "testdb",
		SlotName: "my_replication_slot",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.driver != "postgres" {
		t.Errorf("expected driver 'postgres', got '%s'", source.driver)
	}

	if source.slotName != "my_replication_slot" {
		t.Errorf("expected slot name 'my_replication_slot', got '%s'", source.slotName)
	}
}

// TLS Configuration Tests

func TestNewCDCSource_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Port:     3306,
		Username: "repl_user",
		Password: "repl_pass",
		Database: "testdb",
		DBTLS: &config.DBTLSConfig{
			Enabled: true,
			Mode:    "required",
		},
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.tlsConfig == nil {
		t.Error("expected tlsConfig to be set")
	}

	if !source.tlsConfig.Enabled {
		t.Error("expected TLS to be enabled")
	}
}

func TestNewCDCSource_TLSWithCerts(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Port:     3306,
		Username: "repl_user",
		Password: "repl_pass",
		Database: "testdb",
		DBTLS: &config.DBTLSConfig{
			Enabled:    true,
			Mode:       "verify-full",
			CACert:     "/path/to/ca.crt",
			ClientCert: "/path/to/client.crt",
			ClientKey:  "/path/to/client.key",
			ServerName: "mysql.example.com",
		},
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.tlsConfig.Mode != "verify-full" {
		t.Errorf("expected mode 'verify-full', got '%s'", source.tlsConfig.Mode)
	}

	if source.tlsConfig.ServerName != "mysql.example.com" {
		t.Errorf("expected server name 'mysql.example.com', got '%s'", source.tlsConfig.ServerName)
	}
}

func TestBuildCDCTLSConfig_Nil(t *testing.T) {
	tlsCfg, err := buildCDCTLSConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config for nil input")
	}
}

func TestBuildCDCTLSConfig_Disabled(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: false,
	}

	tlsCfg, err := buildCDCTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config when disabled")
	}
}

func TestBuildCDCTLSConfig_SkipVerify(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: true,
		Mode:    "skip-verify",
	}

	tlsCfg, err := buildCDCTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true for skip-verify mode")
	}
}

func TestBuildCDCTLSConfig_VerifyFull(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled:    true,
		Mode:       "verify-full",
		ServerName: "mysql.example.com",
	}

	tlsCfg, err := buildCDCTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be false for verify-full mode")
	}

	if tlsCfg.ServerName != "mysql.example.com" {
		t.Errorf("expected ServerName 'mysql.example.com', got '%s'", tlsCfg.ServerName)
	}
}

func TestBuildCDCTLSConfig_InvalidCACert(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: true,
		CACert:  "/nonexistent/ca.crt",
	}

	_, err := buildCDCTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA cert file")
	}
}

func TestBuildCDCTLSConfig_InvalidClientCert(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled:    true,
		ClientCert: "/nonexistent/client.crt",
		ClientKey:  "/nonexistent/client.key",
	}

	_, err := buildCDCTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent client cert files")
	}
}

// CDC Event Tests

func TestCDCEventType(t *testing.T) {
	tests := []struct {
		eventType CDCEventType
		expected  string
	}{
		{CDCEventInsert, "insert"},
		{CDCEventUpdate, "update"},
		{CDCEventDelete, "delete"},
	}

	for _, tt := range tests {
		if string(tt.eventType) != tt.expected {
			t.Errorf("expected '%s', got '%s'", tt.expected, tt.eventType)
		}
	}
}

func TestCDCSource_SourceType(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.SourceType() != "cdc" {
		t.Errorf("expected source type 'cdc', got '%s'", source.SourceType())
	}
}

func TestCDCSource_Checkpoint(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial checkpoint should be empty
	checkpoint := source.GetCheckpoint()
	if checkpoint["binlog_file"] != "" {
		t.Errorf("expected empty binlog_file, got '%s'", checkpoint["binlog_file"])
	}

	// Set checkpoint
	err = source.SetCheckpoint(map[string]any{
		"binlog_file": "mysql-bin.000001",
		"binlog_pos":  uint32(12345),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify checkpoint
	checkpoint = source.GetCheckpoint()
	if checkpoint["binlog_file"] != "mysql-bin.000001" {
		t.Errorf("expected binlog_file 'mysql-bin.000001', got '%v'", checkpoint["binlog_file"])
	}
	if checkpoint["binlog_pos"].(uint32) != 12345 {
		t.Errorf("expected binlog_pos 12345, got '%v'", checkpoint["binlog_pos"])
	}
}

func TestCDCSource_SetCheckpoint_FloatPos(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set checkpoint with float64 position (JSON unmarshaling produces float64)
	err = source.SetCheckpoint(map[string]any{
		"binlog_file": "mysql-bin.000002",
		"binlog_pos":  float64(67890),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify checkpoint
	checkpoint := source.GetCheckpoint()
	if checkpoint["binlog_pos"].(uint32) != 67890 {
		t.Errorf("expected binlog_pos 67890, got '%v'", checkpoint["binlog_pos"])
	}
}

func TestCDCSource_GetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set position
	err = source.SetCheckpoint(map[string]any{
		"binlog_file": "mysql-bin.000001",
		"binlog_pos":  uint32(100),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checkpoints := source.GetSourceCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	if cp.OffsetType != "string" {
		t.Errorf("expected offset type 'string', got '%s'", cp.OffsetType)
	}
	if cp.OffsetValue != "mysql-bin.000001:100" {
		t.Errorf("expected offset value 'mysql-bin.000001:100', got '%s'", cp.OffsetValue)
	}
}

func TestCDCSource_SetSourceCheckpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checkpoints := []*SourceCheckpoint{
		{
			PartitionKey: "testdb:mysql-bin.000003",
			OffsetValue:  "mysql-bin.000003:500",
			OffsetType:   "string",
		},
	}

	err = source.SetSourceCheckpoints(checkpoints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checkpoint := source.GetCheckpoint()
	if checkpoint["binlog_file"] != "mysql-bin.000003" {
		t.Errorf("expected binlog_file 'mysql-bin.000003', got '%v'", checkpoint["binlog_file"])
	}
	if checkpoint["binlog_pos"].(uint32) != 500 {
		t.Errorf("expected binlog_pos 500, got '%v'", checkpoint["binlog_pos"])
	}
}

func TestCDCSource_EnvVarExpansion(t *testing.T) {
	// Set environment variables
	t.Setenv("CDC_CA_CERT", "/custom/ca.crt")

	cfg := config.SourceV2{
		Type:     "cdc",
		Driver:   "mysql",
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		Database: "testdb",
		DBTLS: &config.DBTLSConfig{
			Enabled: true,
			Mode:    "verify-ca",
			CACert:  "${CDC_CA_CERT}",
		},
	}

	source, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TLS config should be stored (actual expansion happens during Open)
	if source.tlsConfig == nil {
		t.Error("expected tlsConfig to be set")
	}
	if source.tlsConfig.CACert != "${CDC_CA_CERT}" {
		t.Errorf("expected CACert '${CDC_CA_CERT}', got '%s'", source.tlsConfig.CACert)
	}
}
