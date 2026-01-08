// Package sink SQL 데이터 출력 구현
package sink

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

// SQLSink SQL 데이터베이스 출력
type SQLSink struct {
	driver     string
	dsn        string
	table      string
	columns    []string
	columnMap  map[string]string
	batchSize  int
	onConflict string

	db     *sql.DB
	buffer []source.Record
	bufMu  sync.Mutex
	stats  SinkStats
}

// NewSQLSink SQL 싱크 생성
func NewSQLSink(cfg config.OutputConfig) (*SQLSink, error) {
	if cfg.Driver == "" {
		return nil, fmt.Errorf("sql sink requires driver (mysql, postgres)")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("sql sink requires dsn")
	}
	if cfg.Table == "" {
		return nil, fmt.Errorf("sql sink requires table")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	onConflict := cfg.OnConflict
	if onConflict == "" {
		onConflict = "error"
	}

	return &SQLSink{
		driver:     cfg.Driver,
		dsn:        cfg.DSN,
		table:      cfg.Table,
		columns:    cfg.Columns,
		columnMap:  cfg.ColumnMap,
		batchSize:  batchSize,
		onConflict: onConflict,
		buffer:     make([]source.Record, 0, batchSize),
	}, nil
}

func (s *SQLSink) Name() string {
	return "sql"
}

func (s *SQLSink) Open(ctx context.Context) error {
	db, err := sql.Open(s.driver, s.dsn)
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

	s.db = db
	log.Printf("[sql] Sink opened (driver=%s, table=%s, batch_size=%d)",
		s.driver, s.table, s.batchSize)
	return nil
}

func (s *SQLSink) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&s.stats.TotalRecords, 1)

	s.bufMu.Lock()
	s.buffer = append(s.buffer, record)
	shouldFlush := len(s.buffer) >= s.batchSize
	s.bufMu.Unlock()

	if shouldFlush {
		return s.Flush(ctx)
	}

	return nil
}

func (s *SQLSink) Flush(ctx context.Context) error {
	s.bufMu.Lock()
	if len(s.buffer) == 0 {
		s.bufMu.Unlock()
		return nil
	}
	records := s.buffer
	s.buffer = make([]source.Record, 0, s.batchSize)
	s.bufMu.Unlock()

	// 컬럼 결정 (첫 번째 레코드 기준)
	columns := s.columns
	if len(columns) == 0 && len(records) > 0 {
		columns = s.extractColumns(records[0])
	}

	if len(columns) == 0 {
		return fmt.Errorf("no columns to insert")
	}

	// 배치 INSERT 실행
	if err := s.batchInsert(ctx, records, columns); err != nil {
		atomic.AddInt64(&s.stats.ErrorRecords, int64(len(records)))
		return err
	}

	atomic.AddInt64(&s.stats.SuccessRecords, int64(len(records)))
	s.stats.LastWriteTime = time.Now()

	log.Printf("[sql] Flushed %d records to %s", len(records), s.table)
	return nil
}

func (s *SQLSink) extractColumns(record source.Record) []string {
	columns := make([]string, 0, len(record.Data))
	for key := range record.Data {
		// columnMap이 있으면 매핑된 컬럼명 사용
		if s.columnMap != nil {
			if mappedCol, ok := s.columnMap[key]; ok {
				columns = append(columns, mappedCol)
				continue
			}
		}
		columns = append(columns, key)
	}
	return columns
}

func (s *SQLSink) batchInsert(ctx context.Context, records []source.Record, columns []string) error {
	if len(records) == 0 {
		return nil
	}

	// INSERT 문 생성
	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = s.quoteIdentifier(col)
		placeholders[i] = s.placeholder(i + 1)
	}

	// 기본 INSERT 쿼리
	baseQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES ",
		s.quoteIdentifier(s.table),
		strings.Join(quotedCols, ", "))

	// ON CONFLICT 처리
	var suffix string
	switch s.onConflict {
	case "ignore":
		if s.driver == "mysql" {
			baseQuery = strings.Replace(baseQuery, "INSERT INTO", "INSERT IGNORE INTO", 1)
		} else {
			suffix = " ON CONFLICT DO NOTHING"
		}
	case "update":
		// MySQL: ON DUPLICATE KEY UPDATE
		// PostgreSQL: ON CONFLICT DO UPDATE (requires unique constraint)
		if s.driver == "mysql" {
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
			if s.columnMap != nil {
				for src, dest := range s.columnMap {
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
			rowPlaceholders[j] = s.placeholder(i*len(columns) + j + 1)
		}
		valueGroups[i] = "(" + strings.Join(rowPlaceholders, ", ") + ")"
	}

	query := baseQuery + strings.Join(valueGroups, ", ") + suffix

	// 실행
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	return nil
}

func (s *SQLSink) quoteIdentifier(name string) string {
	switch s.driver {
	case "mysql":
		return "`" + name + "`"
	case "postgres":
		return `"` + name + `"`
	default:
		return name
	}
}

func (s *SQLSink) placeholder(n int) string {
	switch s.driver {
	case "postgres":
		return fmt.Sprintf("$%d", n)
	default:
		return "?"
	}
}

func (s *SQLSink) Close() error {
	// 남은 버퍼 플러시
	if err := s.Flush(context.Background()); err != nil {
		log.Printf("[sql] Warning: failed to flush remaining records: %v", err)
	}

	if s.db != nil {
		s.db.Close()
	}

	log.Printf("[sql] Sink closed. Total: %d, Success: %d, Errors: %d",
		s.stats.TotalRecords, s.stats.SuccessRecords, s.stats.ErrorRecords)
	return nil
}

func (s *SQLSink) Stats() SinkStats {
	return s.stats
}
