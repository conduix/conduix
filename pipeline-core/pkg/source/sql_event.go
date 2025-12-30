package source

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// SQLEventSource SQL 이벤트 테이블 폴링 소스
// 이벤트 테이블을 주기적으로 폴링하여 새로운 이벤트를 가져옴
type SQLEventSource struct {
	driver       string
	dsn          string
	table        string
	idColumn     string        // 이벤트 ID 컬럼 (증가하는 값)
	timestampCol string        // 타임스탬프 컬럼 (선택)
	columns      []string      // 선택할 컬럼 (빈 경우 *)
	whereClause  string        // 추가 WHERE 조건
	orderBy      string        // 정렬 기준
	batchSize    int           // 한 번에 가져올 레코드 수
	pollInterval time.Duration // 폴링 간격

	db            *sql.DB
	mu            sync.RWMutex
	lastID        int64     // 마지막으로 처리한 ID
	lastTimestamp time.Time // 마지막으로 처리한 타임스탬프
	running       bool
}

// NewSQLEventSource SQL 이벤트 소스 생성
func NewSQLEventSource(cfg config.SourceV2) (*SQLEventSource, error) {
	idColumn := cfg.IDColumn
	if idColumn == "" {
		idColumn = "id"
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	pollInterval := time.Second
	if cfg.PollInterval > 0 {
		pollInterval = time.Duration(cfg.PollInterval) * time.Millisecond
	}

	orderBy := cfg.OrderBy
	if orderBy == "" {
		orderBy = idColumn + " ASC"
	}

	return &SQLEventSource{
		driver:       cfg.Driver,
		dsn:          cfg.DSN,
		table:        cfg.Table,
		idColumn:     idColumn,
		timestampCol: cfg.TimestampColumn,
		columns:      cfg.Columns,
		whereClause:  cfg.Where,
		orderBy:      orderBy,
		batchSize:    batchSize,
		pollInterval: pollInterval,
	}, nil
}

func (s *SQLEventSource) Name() string {
	return "sql_event"
}

func (s *SQLEventSource) Open(ctx context.Context) error {
	db, err := sql.Open(s.driver, s.dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	s.mu.Lock()
	s.db = db
	s.mu.Unlock()

	return nil
}

func (s *SQLEventSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	go func() {
		defer close(records)
		defer close(errs)

		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()

		// 초기 폴링
		if err := s.poll(ctx, records); err != nil {
			select {
			case errs <- err:
			default:
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.RLock()
				running := s.running
				s.mu.RUnlock()

				if !running {
					return
				}

				if err := s.poll(ctx, records); err != nil {
					select {
					case errs <- err:
					default:
					}
				}
			}
		}
	}()

	return records, errs
}

func (s *SQLEventSource) poll(ctx context.Context, records chan<- Record) error {
	s.mu.RLock()
	db := s.db
	lastID := s.lastID
	s.mu.RUnlock()

	if db == nil {
		return fmt.Errorf("database not connected")
	}

	// 쿼리 생성
	query := s.buildQuery(lastID)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	var maxID int64
	rowCount := 0

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 값 스캔
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("scan failed: %w", err)
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

			// ID 컬럼 값 추적
			if col == s.idColumn {
				if id, ok := toInt64(val); ok && id > maxID {
					maxID = id
				}
			}
		}

		record := Record{
			Data: data,
			Metadata: Metadata{
				Source:    "sql_event",
				Origin:    s.table,
				Timestamp: time.Now().UnixMilli(),
			},
		}

		// ID를 오프셋으로 설정
		if id, ok := data[s.idColumn]; ok {
			record.Metadata.Offset = fmt.Sprintf("%v", id)
		}

		select {
		case records <- record:
			rowCount++
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	// 마지막 ID 업데이트
	if maxID > 0 {
		s.mu.Lock()
		if maxID > s.lastID {
			s.lastID = maxID
		}
		s.mu.Unlock()
	}

	return nil
}

func (s *SQLEventSource) buildQuery(lastID int64) string {
	// 컬럼 선택
	selectCols := "*"
	if len(s.columns) > 0 {
		selectCols = ""
		for i, col := range s.columns {
			if i > 0 {
				selectCols += ", "
			}
			selectCols += col
		}
	}

	// 기본 쿼리
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s > %d",
		selectCols, s.table, s.idColumn, lastID)

	// 추가 WHERE 조건
	if s.whereClause != "" {
		query += " AND (" + s.whereClause + ")"
	}

	// ORDER BY
	query += " ORDER BY " + s.orderBy

	// LIMIT
	query += fmt.Sprintf(" LIMIT %d", s.batchSize)

	return query
}

// GetCheckpoint 현재 체크포인트 반환
func (s *SQLEventSource) GetCheckpoint() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"last_id":        s.lastID,
		"last_timestamp": s.lastTimestamp,
	}
}

// SetCheckpoint 체크포인트 설정 (복구용)
func (s *SQLEventSource) SetCheckpoint(checkpoint map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lastID, ok := checkpoint["last_id"]; ok {
		if id, ok := toInt64(lastID); ok {
			s.lastID = id
		}
	}

	if lastTS, ok := checkpoint["last_timestamp"]; ok {
		if ts, ok := lastTS.(time.Time); ok {
			s.lastTimestamp = ts
		}
	}

	return nil
}

func (s *SQLEventSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false

	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// toInt64 interface{}를 int64로 변환
func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int32:
		return int64(val), true
	case int64:
		return val, true
	case uint:
		return int64(val), true
	case uint32:
		return int64(val), true
	case uint64:
		return int64(val), true
	case float32:
		return int64(val), true
	case float64:
		return int64(val), true
	default:
		return 0, false
	}
}
