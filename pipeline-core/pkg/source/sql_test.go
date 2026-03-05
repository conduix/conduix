package source

import (
	"strings"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewSQLSource_BasicConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "mysql",
		DSN:    "user:pass@tcp(localhost:3306)/testdb",
		Query:  "SELECT * FROM users",
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.Name() != "sql" {
		t.Errorf("expected name 'sql', got '%s'", source.Name())
	}

	if source.driver != "mysql" {
		t.Errorf("expected driver 'mysql', got '%s'", source.driver)
	}

	if source.query != "SELECT * FROM users" {
		t.Errorf("unexpected query: %s", source.query)
	}
}

func TestNewSQLSource_WithParams(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "postgres",
		DSN:    "postgres://user:pass@localhost:5432/testdb",
		Query:  "SELECT * FROM users WHERE status = $1 AND age > $2",
		Params: []string{"active", "18"},
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(source.params) != 2 {
		t.Errorf("expected 2 params, got %d", len(source.params))
	}

	if source.params[0] != "active" || source.params[1] != "18" {
		t.Errorf("unexpected params: %v", source.params)
	}
}

func TestNewSQLSource_WithIncremental(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "mysql",
		DSN:    "user:pass@tcp(localhost:3306)/testdb",
		Query:  "SELECT * FROM users WHERE updated_at > ?",
		Incremental: &config.IncrementalConfig{
			Column:   "updated_at",
			StateKey: "users_last_update",
		},
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.incremental == nil {
		t.Fatal("expected incremental config to be set")
	}

	if source.incremental.Column != "updated_at" {
		t.Errorf("expected column 'updated_at', got '%s'", source.incremental.Column)
	}

	if source.incremental.StateKey != "users_last_update" {
		t.Errorf("expected state key 'users_last_update', got '%s'", source.incremental.StateKey)
	}
}

// TLS Configuration Tests

func TestBuildTLSEnabledDSN_MySQL(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		tlsCfg   *config.DBTLSConfig
		expected string
	}{
		{
			name: "basic TLS",
			dsn:  "user:pass@tcp(localhost:3306)/testdb",
			tlsCfg: &config.DBTLSConfig{
				Enabled: true,
				Mode:    "required",
			},
			expected: "tls=custom-tls",
		},
		{
			name: "DSN with existing params",
			dsn:  "user:pass@tcp(localhost:3306)/testdb?charset=utf8",
			tlsCfg: &config.DBTLSConfig{
				Enabled: true,
				Mode:    "skip-verify",
			},
			expected: "&tls=custom-tls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildTLSEnabledDSN("mysql", tt.dsn, tt.tlsCfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(result, tt.expected) {
				t.Errorf("expected DSN to contain '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestBuildTLSEnabledDSN_PostgreSQL(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		tlsCfg   *config.DBTLSConfig
		expected []string
	}{
		{
			name: "basic TLS",
			dsn:  "postgres://user:pass@localhost:5432/testdb",
			tlsCfg: &config.DBTLSConfig{
				Enabled: true,
				Mode:    "require",
			},
			expected: []string{"sslmode=require"},
		},
		{
			name: "verify-full mode",
			dsn:  "postgres://user:pass@localhost:5432/testdb",
			tlsCfg: &config.DBTLSConfig{
				Enabled: true,
				Mode:    "verify-full",
			},
			expected: []string{"sslmode=verify-full"},
		},
		{
			name: "DSN with existing params",
			dsn:  "postgres://user:pass@localhost:5432/testdb?application_name=app",
			tlsCfg: &config.DBTLSConfig{
				Enabled: true,
				Mode:    "require",
			},
			expected: []string{"&sslmode=require"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildTLSEnabledDSN("postgres", tt.dsn, tt.tlsCfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, exp := range tt.expected {
				if !strings.Contains(result, exp) {
					t.Errorf("expected DSN to contain '%s', got '%s'", exp, result)
				}
			}
		})
	}
}

func TestBuildTLSEnabledDSN_PostgreSQL_WithCerts(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled:    true,
		Mode:       "verify-ca",
		CACert:     "/path/to/ca.crt",
		ClientCert: "/path/to/client.crt",
		ClientKey:  "/path/to/client.key",
	}

	result, err := buildTLSEnabledDSN("postgres", "postgres://user:pass@localhost:5432/testdb", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"sslmode=verify-ca",
		"sslrootcert=/path/to/ca.crt",
		"sslcert=/path/to/client.crt",
		"sslkey=/path/to/client.key",
	}

	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected DSN to contain '%s', got '%s'", exp, result)
		}
	}
}

func TestBuildTLSEnabledDSN_UnknownDriver(t *testing.T) {
	dsn := "sqlite:///path/to/db.sqlite"
	cfg := &config.DBTLSConfig{
		Enabled: true,
	}

	result, err := buildTLSEnabledDSN("sqlite", dsn, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unknown driver should return DSN unchanged
	if result != dsn {
		t.Errorf("expected DSN unchanged, got '%s'", result)
	}
}

func TestBuildDBTLSConfig_Nil(t *testing.T) {
	tlsCfg, err := buildDBTLSConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config for nil input")
	}
}

func TestBuildDBTLSConfig_Disabled(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: false,
	}

	tlsCfg, err := buildDBTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg != nil {
		t.Error("expected nil TLS config when disabled")
	}
}

func TestBuildDBTLSConfig_SkipVerify(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: true,
		Mode:    "skip-verify",
	}

	tlsCfg, err := buildDBTLSConfig(cfg)
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

func TestBuildDBTLSConfig_VerifyIdentity(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled:    true,
		Mode:       "verify-identity",
		ServerName: "db.example.com",
	}

	tlsCfg, err := buildDBTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}

	if tlsCfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be false for verify-identity mode")
	}

	if tlsCfg.ServerName != "db.example.com" {
		t.Errorf("expected ServerName 'db.example.com', got '%s'", tlsCfg.ServerName)
	}
}

func TestBuildDBTLSConfig_InvalidCACert(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled: true,
		CACert:  "/nonexistent/ca.crt",
	}

	_, err := buildDBTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA cert file")
	}
}

func TestBuildDBTLSConfig_InvalidClientCert(t *testing.T) {
	cfg := &config.DBTLSConfig{
		Enabled:    true,
		ClientCert: "/nonexistent/client.crt",
		ClientKey:  "/nonexistent/client.key",
	}

	_, err := buildDBTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent client cert files")
	}
}

func TestNewSQLSource_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "mysql",
		DSN:    "user:pass@tcp(localhost:3306)/testdb",
		Query:  "SELECT * FROM users",
		DBTLS: &config.DBTLSConfig{
			Enabled: true,
			Mode:    "skip-verify",
		},
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DSN should contain TLS config
	if !strings.Contains(source.dsn, "tls=") {
		t.Errorf("expected DSN to contain TLS config, got '%s'", source.dsn)
	}

	if source.tlsConfig == nil {
		t.Error("expected tlsConfig to be set")
	}
}

func TestNewSQLSource_PostgreSQL_WithTLS(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "postgres",
		DSN:    "postgres://user:pass@localhost:5432/testdb",
		Query:  "SELECT * FROM users",
		DBTLS: &config.DBTLSConfig{
			Enabled: true,
			Mode:    "require",
		},
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DSN should contain SSL mode
	if !strings.Contains(source.dsn, "sslmode=require") {
		t.Errorf("expected DSN to contain sslmode=require, got '%s'", source.dsn)
	}
}

func TestNewSQLSource_WithEnvVarExpansion(t *testing.T) {
	// Set environment variables
	t.Setenv("DB_CA_CERT", "/custom/ca.crt")

	cfg := config.SourceV2{
		Type:   "sql",
		Driver: "postgres",
		DSN:    "postgres://user:pass@localhost:5432/testdb",
		Query:  "SELECT * FROM users",
		DBTLS: &config.DBTLSConfig{
			Enabled: true,
			Mode:    "verify-ca",
			CACert:  "${DB_CA_CERT}",
		},
	}

	source, err := NewSQLSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DSN should contain expanded path
	if !strings.Contains(source.dsn, "sslrootcert=/custom/ca.crt") {
		t.Errorf("expected DSN to contain expanded CA path, got '%s'", source.dsn)
	}
}
