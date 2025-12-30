package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	// SQL 드라이버는 사용 시 import
	// _ "github.com/go-sql-driver/mysql"
	// _ "github.com/lib/pq"
)

// SQLSource SQL 데이터 소스
type SQLSource struct {
	driver      string
	dsn         string
	query       string
	params      []string
	incremental *config.IncrementalConfig
	db          *sql.DB
}

// NewSQLSource SQL 소스 생성
func NewSQLSource(cfg config.SourceV2) (*SQLSource, error) {
	return &SQLSource{
		driver:      cfg.Driver,
		dsn:         cfg.DSN,
		query:       cfg.Query,
		params:      cfg.Params,
		incremental: cfg.Incremental,
	}, nil
}

func (s *SQLSource) Name() string {
	return "sql"
}

func (s *SQLSource) Open(ctx context.Context) error {
	db, err := sql.Open(s.driver, s.dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 연결 테스트
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	s.db = db
	return nil
}

func (s *SQLSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 파라미터 준비
		params := make([]any, len(s.params))
		for i, p := range s.params {
			params[i] = p
		}

		rows, err := s.db.QueryContext(ctx, s.query, params...)
		if err != nil {
			errs <- fmt.Errorf("query failed: %w", err)
			return
		}
		defer rows.Close()

		// 컬럼 정보
		columns, err := rows.Columns()
		if err != nil {
			errs <- fmt.Errorf("failed to get columns: %w", err)
			return
		}

		for rows.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 값 스캔
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				errs <- fmt.Errorf("scan failed: %w", err)
				return
			}

			// map으로 변환
			data := make(map[string]any)
			for i, col := range columns {
				val := values[i]
				// []byte를 string으로 변환
				if b, ok := val.([]byte); ok {
					val = string(b)
				}
				data[col] = val
			}

			records <- Record{
				Data: data,
				Metadata: Metadata{
					Source:    "sql",
					Origin:    s.dsn,
					Timestamp: time.Now().UnixMilli(),
				},
			}
		}

		if err := rows.Err(); err != nil {
			errs <- fmt.Errorf("rows error: %w", err)
		}
	}()

	return records, errs
}

func (s *SQLSource) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
