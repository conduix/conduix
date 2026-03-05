package stream

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/snowflakedb/gosnowflake" // Snowflake driver
)

// SnowflakeSink writes to Snowflake Data Warehouse
type SnowflakeSink struct {
	BufferedSink

	// Connection settings
	account   string
	user      string
	password  string
	database  string
	schema    string
	warehouse string
	role      string

	// Table settings
	table             string
	columns           []string          // 컬럼 목록 (빈 값이면 자동 감지)
	columnMap         map[string]string // source field -> snowflake column mapping
	createTable       bool              // 테이블 자동 생성
	onConflict        string            // ignore, update, error (default: error)
	conflictKeys      []string          // MERGE 시 사용할 키 컬럼들
	stageMethod       string            // PUT (stage), INSERT (direct), AUTO (default)
	stageName         string            // Named stage (optional)
	compressionFormat string            // GZIP, ZSTD, NONE (default: GZIP for PUT)

	// Connection
	db *sql.DB
}

// NewSnowflakeSink creates a new Snowflake sink
func NewSnowflakeSink(name string, config map[string]any) *SnowflakeSink {
	s := &SnowflakeSink{
		BufferedSink: BufferedSink{
			BaseSink: BaseSink{
				name:         name,
				typ:          "snowflake",
				config:       config,
				batchSize:    10000, // Snowflake는 큰 배치가 효율적
				flushTimeout: 60 * time.Second,
			},
		},
		columnMap:         make(map[string]string),
		onConflict:        "error",
		stageMethod:       "AUTO",
		compressionFormat: "GZIP",
	}

	// Parse connection settings
	if v, ok := config["account"].(string); ok {
		s.account = expandEnvVars(v)
	}
	if v, ok := config["user"].(string); ok {
		s.user = expandEnvVars(v)
	}
	if v, ok := config["password"].(string); ok {
		s.password = expandEnvVars(v)
	}
	if v, ok := config["database"].(string); ok {
		s.database = v
	}
	if v, ok := config["schema"].(string); ok {
		s.schema = v
	} else {
		s.schema = "PUBLIC"
	}
	if v, ok := config["warehouse"].(string); ok {
		s.warehouse = v
	}
	if v, ok := config["role"].(string); ok {
		s.role = v
	}

	// Parse table settings
	if v, ok := config["table"].(string); ok {
		s.table = v
	}
	if v, ok := config["columns"].([]any); ok {
		for _, col := range v {
			if cs, ok := col.(string); ok {
				s.columns = append(s.columns, cs)
			}
		}
	}
	if v, ok := config["column_map"].(map[string]any); ok {
		for k, val := range v {
			if vs, ok := val.(string); ok {
				s.columnMap[k] = vs
			}
		}
	}
	if v, ok := config["create_table"].(bool); ok {
		s.createTable = v
	}
	if v, ok := config["on_conflict"].(string); ok {
		s.onConflict = v
	}
	if v, ok := config["conflict_keys"].([]any); ok {
		for _, k := range v {
			if ks, ok := k.(string); ok {
				s.conflictKeys = append(s.conflictKeys, ks)
			}
		}
	}
	if v, ok := config["stage_method"].(string); ok {
		s.stageMethod = strings.ToUpper(v)
	}
	if v, ok := config["stage_name"].(string); ok {
		s.stageName = v
	}
	if v, ok := config["compression"].(string); ok {
		s.compressionFormat = strings.ToUpper(v)
	}

	// Parse buffer settings
	if buf, ok := config["buffer"].(map[string]any); ok {
		if me, ok := buf["max_events"].(int); ok {
			s.batchSize = me
		}
		if to, ok := buf["timeout"].(string); ok {
			if d, err := time.ParseDuration(to); err == nil {
				s.flushTimeout = d
			}
		}
	}

	s.init()
	s.writeFunc = s.writeBatch
	s.startFlushLoop()

	return s
}

// Open establishes connection to Snowflake
func (s *SnowflakeSink) Open(ctx context.Context) error {
	// Build DSN
	dsn := fmt.Sprintf("%s:%s@%s/%s/%s",
		s.user,
		s.password,
		s.account,
		s.database,
		s.schema,
	)

	// Add optional parameters
	params := []string{}
	if s.warehouse != "" {
		params = append(params, fmt.Sprintf("warehouse=%s", s.warehouse))
	}
	if s.role != "" {
		params = append(params, fmt.Sprintf("role=%s", s.role))
	}
	if len(params) > 0 {
		dsn += "?" + strings.Join(params, "&")
	}

	// Connect
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return fmt.Errorf("failed to open snowflake connection: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to connect to snowflake: %w", err)
	}

	s.db = db

	// Create table if needed
	if s.createTable && s.table != "" {
		if err := s.ensureTable(ctx); err != nil {
			return fmt.Errorf("failed to ensure table: %w", err)
		}
	}

	fmt.Printf("[snowflake] Connected to %s.%s.%s\n", s.database, s.schema, s.table)
	return nil
}

// ensureTable creates the table if it doesn't exist
func (s *SnowflakeSink) ensureTable(ctx context.Context) error {
	if len(s.columns) == 0 {
		// 컬럼 정의가 없으면 VARIANT 타입으로 단일 컬럼 생성
		query := fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s.%s.%s (
				data VARIANT,
				_loaded_at TIMESTAMP_LTZ DEFAULT CURRENT_TIMESTAMP()
			)
		`, s.database, s.schema, s.table)

		_, err := s.db.ExecContext(ctx, query)
		return err
	}

	// 컬럼 정의가 있으면 해당 컬럼으로 테이블 생성
	// 기본적으로 모든 컬럼을 VARIANT로 생성 (스키마 유연성)
	var colDefs []string
	for _, col := range s.columns {
		colDefs = append(colDefs, fmt.Sprintf("%s VARIANT", col))
	}
	colDefs = append(colDefs, "_loaded_at TIMESTAMP_LTZ DEFAULT CURRENT_TIMESTAMP()")

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s.%s (%s)",
		s.database, s.schema, s.table,
		strings.Join(colDefs, ", "))

	_, err := s.db.ExecContext(ctx, query)
	return err
}

// writeBatch writes a batch of records to Snowflake
func (s *SnowflakeSink) writeBatch(ctx context.Context, records []*Record) error {
	if s.db == nil {
		// Lazy initialization
		if err := s.Open(ctx); err != nil {
			return err
		}
	}

	if len(records) == 0 {
		return nil
	}

	// Determine method
	method := s.stageMethod
	if method == "AUTO" {
		if len(records) >= 1000 {
			method = "PUT" // Large batch: use stage
		} else {
			method = "INSERT" // Small batch: direct insert
		}
	}

	switch method {
	case "PUT":
		return s.writeBatchWithStage(ctx, records)
	default:
		return s.writeBatchDirect(ctx, records)
	}
}

// writeBatchDirect uses direct INSERT VALUES
func (s *SnowflakeSink) writeBatchDirect(ctx context.Context, records []*Record) error {
	if len(s.columns) == 0 {
		// VARIANT 모드: JSON으로 삽입
		return s.writeBatchVariant(ctx, records)
	}

	// Column-based insert
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build INSERT statement
	placeholders := make([]string, len(s.columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf("INSERT INTO %s.%s.%s (%s) VALUES (%s)",
		s.database, s.schema, s.table,
		strings.Join(s.columns, ", "),
		strings.Join(placeholders, ", "),
	)

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, record := range records {
		values := make([]any, len(s.columns))
		for i, col := range s.columns {
			// Check column mapping
			sourceField := col
			if mapped, ok := s.columnMap[col]; ok {
				sourceField = mapped
			}

			if v, ok := record.Data[sourceField]; ok {
				// JSON으로 변환 (Snowflake VARIANT 지원)
				if _, isMap := v.(map[string]any); isMap {
					jsonBytes, _ := json.Marshal(v)
					values[i] = string(jsonBytes)
				} else if _, isSlice := v.([]any); isSlice {
					jsonBytes, _ := json.Marshal(v)
					values[i] = string(jsonBytes)
				} else {
					values[i] = v
				}
			} else {
				values[i] = nil
			}
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("failed to execute insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("[snowflake] Inserted %d records into %s.%s.%s\n",
		len(records), s.database, s.schema, s.table)
	return nil
}

// writeBatchVariant uses VARIANT column for flexible schema
func (s *SnowflakeSink) writeBatchVariant(ctx context.Context, records []*Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := fmt.Sprintf("INSERT INTO %s.%s.%s (data) SELECT PARSE_JSON(?)",
		s.database, s.schema, s.table)

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, record := range records {
		jsonBytes, err := json.Marshal(record.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal record: %w", err)
		}

		if _, err := stmt.ExecContext(ctx, string(jsonBytes)); err != nil {
			return fmt.Errorf("failed to execute insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("[snowflake] Inserted %d records (VARIANT) into %s.%s.%s\n",
		len(records), s.database, s.schema, s.table)
	return nil
}

// writeBatchWithStage uses PUT staging for large batches
func (s *SnowflakeSink) writeBatchWithStage(ctx context.Context, records []*Record) error {
	// Create temporary CSV file
	tmpFile, err := os.CreateTemp("", "snowflake-*.csv")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	// Write CSV
	writer := csv.NewWriter(tmpFile)

	// Write header if using columns
	if len(s.columns) > 0 {
		if err := writer.Write(s.columns); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}
	}

	// Write data
	for _, record := range records {
		if len(s.columns) > 0 {
			row := make([]string, len(s.columns))
			for i, col := range s.columns {
				sourceField := col
				if mapped, ok := s.columnMap[col]; ok {
					sourceField = mapped
				}
				if v, ok := record.Data[sourceField]; ok {
					row[i] = fmt.Sprintf("%v", v)
				}
			}
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		} else {
			// VARIANT mode: write JSON
			jsonBytes, _ := json.Marshal(record.Data)
			if err := writer.Write([]string{string(jsonBytes)}); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		}
	}
	writer.Flush()
	tmpFile.Close()

	// Determine stage
	stageName := s.stageName
	if stageName == "" {
		stageName = fmt.Sprintf("@%s.%s.%%", s.database, s.schema) // Table stage
	}

	// PUT file to stage
	putQuery := fmt.Sprintf("PUT file://%s %s AUTO_COMPRESS=TRUE",
		tmpFile.Name(), stageName)
	if _, err := s.db.ExecContext(ctx, putQuery); err != nil {
		return fmt.Errorf("failed to PUT file to stage: %w", err)
	}

	// COPY INTO table
	copyQuery := fmt.Sprintf(`
		COPY INTO %s.%s.%s
		FROM %s
		FILE_FORMAT = (TYPE = CSV SKIP_HEADER = 1)
		PURGE = TRUE
	`, s.database, s.schema, s.table, stageName)

	if _, err := s.db.ExecContext(ctx, copyQuery); err != nil {
		return fmt.Errorf("failed to COPY INTO table: %w", err)
	}

	fmt.Printf("[snowflake] Staged and copied %d records into %s.%s.%s\n",
		len(records), s.database, s.schema, s.table)
	return nil
}

// Close closes the Snowflake connection
func (s *SnowflakeSink) Close() error {
	if err := s.BufferedSink.Close(); err != nil {
		return err
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// expandEnvVars expands environment variables in a string
func expandEnvVars(s string) string {
	return os.ExpandEnv(s)
}
