// Package output SQL 데이터 출력 구현
package output

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// SQLOutput SQL 데이터베이스 출력
type SQLOutput struct {
	driver      string
	dsn         string
	table       string
	columns     []string
	columnMap   map[string]string
	batchSize   int
	onConflict  string
	createTable string

	db     *sql.DB
	buffer []source.Record
	bufMu  sync.Mutex
	stats  OutputStats
}

// NewSQLOutput SQL 출력 생성
func NewSQLOutput(cfg config.OutputConfig) (*SQLOutput, error) {
	if cfg.Driver == "" {
		return nil, fmt.Errorf("sql output requires driver (mysql, postgres)")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("sql output requires dsn")
	}
	if cfg.Table == "" {
		return nil, fmt.Errorf("sql output requires table")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	onConflict := cfg.OnConflict
	if onConflict == "" {
		onConflict = "error"
	}

	return &SQLOutput{
		driver:      cfg.Driver,
		dsn:         cfg.DSN,
		table:       cfg.Table,
		columns:     cfg.Columns,
		columnMap:   cfg.ColumnMap,
		batchSize:   batchSize,
		onConflict:  onConflict,
		createTable: cfg.CreateTable,
		buffer:      make([]source.Record, 0, batchSize),
	}, nil
}

func (o *SQLOutput) Name() string {
	return "sql"
}

func (o *SQLOutput) Open(ctx context.Context) error {
	db, err := sql.Open(o.driver, o.dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool 설정
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 연결 테스트
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	o.db = db

	// CREATE TABLE 실행 (설정된 경우)
	if o.createTable != "" {
		log.Printf("[sql] Executing CREATE TABLE: %s", o.createTable)
		if _, err := o.db.ExecContext(ctx, o.createTable); err != nil {
			return fmt.Errorf("failed to execute CREATE TABLE: %w", err)
		}
		log.Printf("[sql] CREATE TABLE executed successfully")
	}

	log.Printf("[sql] Output opened (driver=%s, table=%s, batch_size=%d)",
		o.driver, o.table, o.batchSize)
	return nil
}

func (o *SQLOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	o.bufMu.Lock()
	o.buffer = append(o.buffer, record)
	shouldFlush := len(o.buffer) >= o.batchSize
	o.bufMu.Unlock()

	if shouldFlush {
		return o.Flush(ctx)
	}

	return nil
}

func (o *SQLOutput) Flush(ctx context.Context) error {
	o.bufMu.Lock()
	if len(o.buffer) == 0 {
		o.bufMu.Unlock()
		return nil
	}
	records := o.buffer
	o.buffer = make([]source.Record, 0, o.batchSize)
	o.bufMu.Unlock()

	// 컬럼 결정 (첫 번째 레코드 기준)
	columns := o.columns
	if len(columns) == 0 && len(records) > 0 {
		columns = o.extractColumns(records[0])
	}

	if len(columns) == 0 {
		return fmt.Errorf("no columns to insert")
	}

	// 배치 INSERT 실행
	if err := o.batchInsert(ctx, records, columns); err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, int64(len(records)))
		return err
	}

	atomic.AddInt64(&o.stats.SuccessRecords, int64(len(records)))
	o.stats.LastWriteTime = time.Now()

	log.Printf("[sql] Flushed %d records to %s", len(records), o.table)
	return nil
}

func (o *SQLOutput) extractColumns(record source.Record) []string {
	columns := make([]string, 0, len(record.Data))
	for key := range record.Data {
		// columnMap이 있으면 매핑된 컬럼명 사용
		if o.columnMap != nil {
			if mappedCol, ok := o.columnMap[key]; ok {
				columns = append(columns, mappedCol)
				continue
			}
		}
		columns = append(columns, key)
	}
	return columns
}

func (o *SQLOutput) batchInsert(ctx context.Context, records []source.Record, columns []string) error {
	if len(records) == 0 {
		return nil
	}

	// INSERT 문 생성
	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = o.quoteIdentifier(col)
		placeholders[i] = o.placeholder(i + 1)
	}

	// 기본 INSERT 쿼리
	baseQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES ",
		o.quoteIdentifier(o.table),
		strings.Join(quotedCols, ", "))

	// ON CONFLICT 처리
	var suffix string
	switch o.onConflict {
	case "ignore":
		if o.driver == "mysql" {
			baseQuery = strings.Replace(baseQuery, "INSERT INTO", "INSERT IGNORE INTO", 1)
		} else {
			suffix = " ON CONFLICT DO NOTHING"
		}
	case "update":
		// MySQL: ON DUPLICATE KEY UPDATE
		// PostgreSQL: ON CONFLICT DO UPDATE (requires unique constraint)
		if o.driver == "mysql" {
			updates := make([]string, len(columns))
			for i, col := range quotedCols {
				updates[i] = fmt.Sprintf("%s=VALUES(%s)", col, col)
			}
			suffix = " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
		}
	}

	// 배치 VALUES 생성
	valueGroups := make([]string, len(records))
	args := make([]any, 0, len(records)*len(columns))

	for i, record := range records {
		rowPlaceholders := make([]string, len(columns))
		for j, col := range columns {
			// columnMap 역매핑으로 원본 필드 찾기
			sourceField := col
			if o.columnMap != nil {
				for src, dest := range o.columnMap {
					if dest == col {
						sourceField = src
						break
					}
				}
			}

			value := record.Data[sourceField]
			if value == nil {
				value = record.Data[col] // 원본 컬럼명으로도 시도
			}

			args = append(args, value)
			rowPlaceholders[j] = o.placeholder(i*len(columns) + j + 1)
		}
		valueGroups[i] = "(" + strings.Join(rowPlaceholders, ", ") + ")"
	}

	query := baseQuery + strings.Join(valueGroups, ", ") + suffix

	// 실행
	_, err := o.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	return nil
}

func (o *SQLOutput) quoteIdentifier(name string) string {
	switch o.driver {
	case "mysql":
		return "`" + name + "`"
	case "postgres":
		return `"` + name + `"`
	default:
		return name
	}
}

func (o *SQLOutput) placeholder(n int) string {
	switch o.driver {
	case "postgres":
		return fmt.Sprintf("$%d", n)
	default:
		return "?"
	}
}

func (o *SQLOutput) Close() error {
	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[sql] Warning: failed to flush remaining records: %v", err)
	}

	if o.db != nil {
		o.db.Close()
	}

	log.Printf("[sql] Output closed. Total: %d, Success: %d, Errors: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords)
	return nil
}

func (o *SQLOutput) Stats() OutputStats {
	return o.stats
}
