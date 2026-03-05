package stream

import (
	"testing"
)

func TestNewSnowflakeSink_BasicConfig(t *testing.T) {
	config := map[string]any{
		"account":   "myaccount",
		"user":      "myuser",
		"password":  "mypassword",
		"database":  "mydb",
		"schema":    "public",
		"warehouse": "compute_wh",
		"table":     "events",
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	if sink.Name() != "test-snowflake" {
		t.Errorf("expected name 'test-snowflake', got '%s'", sink.Name())
	}

	if sink.Type() != "snowflake" {
		t.Errorf("expected type 'snowflake', got '%s'", sink.Type())
	}

	if sink.account != "myaccount" {
		t.Errorf("expected account 'myaccount', got '%s'", sink.account)
	}

	if sink.user != "myuser" {
		t.Errorf("expected user 'myuser', got '%s'", sink.user)
	}

	if sink.database != "mydb" {
		t.Errorf("expected database 'mydb', got '%s'", sink.database)
	}

	if sink.schema != "public" {
		t.Errorf("expected schema 'public', got '%s'", sink.schema)
	}

	if sink.warehouse != "compute_wh" {
		t.Errorf("expected warehouse 'compute_wh', got '%s'", sink.warehouse)
	}

	if sink.table != "events" {
		t.Errorf("expected table 'events', got '%s'", sink.table)
	}
}

func TestNewSnowflakeSink_WithColumns(t *testing.T) {
	config := map[string]any{
		"account":  "myaccount",
		"user":     "myuser",
		"password": "mypassword",
		"database": "mydb",
		"table":    "events",
		"columns":  []any{"id", "name", "value"},
		"column_map": map[string]any{
			"id":    "event_id",
			"name":  "event_name",
			"value": "event_value",
		},
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	if len(sink.columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(sink.columns))
	}

	if sink.columns[0] != "id" {
		t.Errorf("expected first column 'id', got '%s'", sink.columns[0])
	}

	if sink.columnMap["id"] != "event_id" {
		t.Errorf("expected column mapping 'id' -> 'event_id', got '%s'", sink.columnMap["id"])
	}
}

func TestNewSnowflakeSink_WithConflictHandling(t *testing.T) {
	config := map[string]any{
		"account":       "myaccount",
		"user":          "myuser",
		"password":      "mypassword",
		"database":      "mydb",
		"table":         "events",
		"on_conflict":   "update",
		"conflict_keys": []any{"id", "timestamp"},
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	if sink.onConflict != "update" {
		t.Errorf("expected on_conflict 'update', got '%s'", sink.onConflict)
	}

	if len(sink.conflictKeys) != 2 {
		t.Errorf("expected 2 conflict keys, got %d", len(sink.conflictKeys))
	}
}

func TestNewSnowflakeSink_WithStageMethod(t *testing.T) {
	testCases := []struct {
		method   string
		expected string
	}{
		{"PUT", "PUT"},
		{"put", "PUT"},
		{"INSERT", "INSERT"},
		{"insert", "INSERT"},
		{"AUTO", "AUTO"},
		// 빈 문자열은 설정하지 않으므로 테스트에서 제외
		// 기본값은 NewSnowflakeSink에서 "AUTO"로 설정됨
	}

	for _, tc := range testCases {
		config := map[string]any{
			"account":      "myaccount",
			"user":         "myuser",
			"password":     "mypassword",
			"database":     "mydb",
			"table":        "events",
			"stage_method": tc.method,
		}

		sink := NewSnowflakeSink("test-snowflake", config)

		if sink.stageMethod != tc.expected {
			t.Errorf("stage_method '%s': expected '%s', got '%s'",
				tc.method, tc.expected, sink.stageMethod)
		}
	}
}

func TestNewSnowflakeSink_WithBuffer(t *testing.T) {
	config := map[string]any{
		"account":  "myaccount",
		"user":     "myuser",
		"password": "mypassword",
		"database": "mydb",
		"table":    "events",
		"buffer": map[string]any{
			"max_events": 5000,
			"timeout":    "30s",
		},
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	if sink.batchSize != 5000 {
		t.Errorf("expected batch_size 5000, got %d", sink.batchSize)
	}
}

func TestNewSnowflakeSink_DefaultValues(t *testing.T) {
	config := map[string]any{
		"account":  "myaccount",
		"user":     "myuser",
		"password": "mypassword",
		"database": "mydb",
		"table":    "events",
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	// Default schema
	if sink.schema != "PUBLIC" {
		t.Errorf("expected default schema 'PUBLIC', got '%s'", sink.schema)
	}

	// Default on_conflict
	if sink.onConflict != "error" {
		t.Errorf("expected default on_conflict 'error', got '%s'", sink.onConflict)
	}

	// Default stage_method
	if sink.stageMethod != "AUTO" {
		t.Errorf("expected default stage_method 'AUTO', got '%s'", sink.stageMethod)
	}

	// Default compression
	if sink.compressionFormat != "GZIP" {
		t.Errorf("expected default compression 'GZIP', got '%s'", sink.compressionFormat)
	}

	// Default batch size (large for Snowflake efficiency)
	if sink.batchSize != 10000 {
		t.Errorf("expected default batch_size 10000, got %d", sink.batchSize)
	}
}

func TestNewSnowflakeSink_EnvVarExpansion(t *testing.T) {
	// Set test env vars
	t.Setenv("SNOWFLAKE_ACCOUNT", "test-account")
	t.Setenv("SNOWFLAKE_USER", "test-user")
	t.Setenv("SNOWFLAKE_PASSWORD", "test-password")

	config := map[string]any{
		"account":  "$SNOWFLAKE_ACCOUNT",
		"user":     "$SNOWFLAKE_USER",
		"password": "$SNOWFLAKE_PASSWORD",
		"database": "mydb",
		"table":    "events",
	}

	sink := NewSnowflakeSink("test-snowflake", config)

	if sink.account != "test-account" {
		t.Errorf("expected account 'test-account', got '%s'", sink.account)
	}

	if sink.user != "test-user" {
		t.Errorf("expected user 'test-user', got '%s'", sink.user)
	}

	if sink.password != "test-password" {
		t.Errorf("expected password 'test-password', got '%s'", sink.password)
	}
}
