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
	driver          string
	dsn             string
	table           string
	columns         []string
	columnMap       map[string]string
	batchSize       int
	onConflict      string
	conflictColumns []string
	createTable     string
	cdcDelete       bool   // CDC delete 이벤트(_cdc_type=delete)를 타깃 DELETE 로 반영할지(기본 true)
	versionColumn   string // 버전 가드 upsert 용 단조 컬럼(빈 값이면 미사용). incoming>existing 일 때만 덮음.
	syncedAtColumn  string // 매 upsert 에 실행 시작시각 T 를 주입할 컬럼(빈 값이면 미사용)
	sweep           *config.SweepConfig
	runStartedAt    time.Time // T. Open 시각 — synced_at 스탬프와 sweep 판정이 같은 값을 공유해야 오탐이 없다

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

	// CDC delete 반영: 기본 켜짐. cdc_delete=false 로 끌 수 있다(soft-delete 표시만 원하는 경우).
	cdcDelete := true
	if v, ok := cfg.Config["cdc_delete"].(bool); ok {
		cdcDelete = v
	}

	// sweep 은 완전 opt-in — 미설정이면 아래 검증·기본값 어느 것도 적용되지 않는다.
	var sweep *config.SweepConfig
	if cfg.Sweep != nil {
		s := *cfg.Sweep // 호출자 설정 변경 방지를 위한 복사
		if s.Mode != "delete" && s.Mode != "soft" {
			return nil, fmt.Errorf("sql sweep.mode must be \"delete\" or \"soft\", got %q", s.Mode)
		}
		if s.Column == "" {
			s.Column = cfg.SyncedAtColumn
		}
		if s.Column == "" {
			return nil, fmt.Errorf("sql sweep requires synced_at_column or sweep.column")
		}
		if s.Mode == "soft" && s.SoftColumn == "" {
			s.SoftColumn = "deleted_at"
		}
		sweep = &s
	}

	return &SQLOutput{
		driver:          cfg.Driver,
		dsn:             cfg.DSN,
		table:           cfg.Table,
		columns:         cfg.Columns,
		columnMap:       cfg.ColumnMap,
		batchSize:       batchSize,
		onConflict:      onConflict,
		conflictColumns: cfg.ConflictColumns,
		createTable:     cfg.CreateTable,
		cdcDelete:       cdcDelete,
		versionColumn:   cfg.VersionColumn,
		syncedAtColumn:  cfg.SyncedAtColumn,
		sweep:           sweep,
		buffer:          make([]source.Record, 0, batchSize),
	}, nil
}

func (o *SQLOutput) Name() string {
	return "sql"
}

func (o *SQLOutput) Open(ctx context.Context) error {
	// T 확정: 이 시각 이전의 synced_at 을 가진 행이 sweep 대상이 된다.
	// 수집 중 upsert 되는 모든 행은 T 로 스탬프되므로 sweep 에서 안전하다.
	o.runStartedAt = time.Now()

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

	// CDC delete 이벤트를 분리한다. cdcDelete=true 면 타깃에서 DELETE, 아니면 일반 레코드로 취급.
	// (분리해도 배치 내 순서는 upsert→delete 로 처리 — 같은 PK 의 update 후 delete 면 delete 가 최종.)
	upserts, deletes := o.partitionCDC(records)

	// synced_at 스탬프는 컬럼 추출보다 먼저 — 추출이 먼저면 스탬프 컬럼이 INSERT 에서 빠진다.
	upserts = o.stampRecords(upserts)

	// 컬럼 결정 (첫 번째 upsert 레코드 기준)
	columns := o.columns
	if len(columns) == 0 && len(upserts) > 0 {
		columns = o.extractColumns(upserts[0])
	} else if len(columns) > 0 {
		columns = o.ensureStampColumns(columns)
	}

	if len(upserts) > 0 {
		if len(columns) == 0 {
			return fmt.Errorf("no columns to insert")
		}
		if err := o.batchInsert(ctx, upserts, columns); err != nil {
			// 실패 시 버퍼를 복원해 다음 flush(예: 종료 시 drain)가 재시도할 수 있게 한다.
			o.bufMu.Lock()
			o.buffer = append(records, o.buffer...)
			o.bufMu.Unlock()
			atomic.AddInt64(&o.stats.ErrorRecords, int64(len(records)))
			return err
		}
	}

	if len(deletes) > 0 {
		if err := o.batchDelete(ctx, deletes); err != nil {
			// upsert 는 이미 반영됨. delete 만 재시도하도록 delete 레코드만 버퍼 복원.
			o.bufMu.Lock()
			o.buffer = append(deletes, o.buffer...)
			o.bufMu.Unlock()
			atomic.AddInt64(&o.stats.ErrorRecords, int64(len(deletes)))
			return err
		}
	}

	atomic.AddInt64(&o.stats.SuccessRecords, int64(len(records)))
	o.stats.LastWriteTime = time.Now()

	log.Printf("[sql] Flushed %d records to %s (upsert=%d, delete=%d)", len(records), o.table, len(upserts), len(deletes))
	return nil
}

// stampRecords upsert 레코드에 실행 시작시각 T(synced_at)를 주입하고, soft sweep
// 모드면 삭제 플래그를 NULL 로 복원한다(사라졌다 재등장한 행 살리기).
// Record.Data 맵은 다른 sink 와 공유되므로 원본을 변경하지 않고 복사한다.
// synced_at_column 미설정이면 입력을 그대로 반환한다(zero-cost opt-in).
func (o *SQLOutput) stampRecords(records []source.Record) []source.Record {
	if o.syncedAtColumn == "" {
		return records
	}
	stamped := make([]source.Record, len(records))
	for i, r := range records {
		data := make(map[string]any, len(r.Data)+2)
		for k, v := range r.Data {
			data[k] = v
		}
		data[o.syncedAtColumn] = o.runStartedAt
		if o.sweep != nil && o.sweep.Mode == "soft" {
			data[o.sweep.SoftColumn] = nil
		}
		r.Data = data
		stamped[i] = r
	}
	return stamped
}

// ensureStampColumns 명시적 columns 화이트리스트 사용 시 스탬프 컬럼이 빠져
// INSERT 에서 유실되지 않도록 보장한다.
func (o *SQLOutput) ensureStampColumns(columns []string) []string {
	if o.syncedAtColumn == "" {
		return columns
	}
	appendMissing := func(cols []string, col string) []string {
		for _, c := range cols {
			if c == col {
				return cols
			}
		}
		return append(cols, col)
	}
	// 원본 슬라이스 공유 회피
	result := append(make([]string, 0, len(columns)+2), columns...)
	result = appendMissing(result, o.syncedAtColumn)
	if o.sweep != nil && o.sweep.Mode == "soft" {
		result = appendMissing(result, o.sweep.SoftColumn)
	}
	return result
}

// partitionCDC 는 레코드를 upsert 대상과 delete 대상으로 나눈다.
// cdcDelete=false 이거나 _cdc_type 이 delete 가 아니면 모두 upsert 로 취급한다.
func (o *SQLOutput) partitionCDC(records []source.Record) (upserts, deletes []source.Record) {
	if !o.cdcDelete {
		return records, nil
	}
	for _, r := range records {
		if t, ok := r.Data["_cdc_type"].(string); ok && t == string(source.CDCEventDelete) {
			deletes = append(deletes, r)
		} else {
			upserts = append(upserts, r)
		}
	}
	return upserts, deletes
}

// deleteKey 는 delete 레코드에서 (컬럼명, 값) 쌍을 뽑는다.
// 우선순위: CDC 가 넣은 _primary_key_columns+_primary_key(위치 대응) → conflict_columns(값은 _old_data/본문에서).
func (o *SQLOutput) deleteKey(r source.Record) (cols []string, vals []any) {
	oldData, _ := r.Data["_old_data"].(map[string]any)
	lookup := func(col string) (any, bool) {
		if oldData != nil {
			if v, ok := oldData[col]; ok {
				return v, true
			}
		}
		v, ok := r.Data[col]
		return v, ok
	}

	if pkCols, ok := r.Data["_primary_key_columns"].([]string); ok && len(pkCols) > 0 {
		if pkVals, ok := r.Data["_primary_key"].([]any); ok && len(pkVals) == len(pkCols) {
			return pkCols, pkVals
		}
		// 값 배열이 없거나 길이가 안 맞으면 이름으로 조회
		for _, c := range pkCols {
			if v, ok := lookup(c); ok {
				cols = append(cols, c)
				vals = append(vals, v)
			}
		}
		return cols, vals
	}

	// PK 정보가 없으면 conflict_columns 로 폴백
	for _, c := range o.conflictColumns {
		if v, ok := lookup(c); ok {
			cols = append(cols, c)
			vals = append(vals, v)
		}
	}
	return cols, vals
}

// batchDelete 는 delete 레코드들을 PK/conflict 키 기준으로 삭제한다.
// 각 레코드마다 DELETE FROM t WHERE k1=? AND k2=? 를 하나의 트랜잭션으로 실행한다.
func (o *SQLOutput) batchDelete(ctx context.Context, deletes []source.Record) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, r := range deletes {
		cols, vals := o.deleteKey(r)
		if len(cols) == 0 {
			// 키를 못 구하면 삭제 대상을 특정할 수 없다 → 조용히 넘기지 않고 에러.
			return fmt.Errorf("cdc delete: no key columns (set conflict_columns or ensure source table has PK) for table %s", o.table)
		}
		conds := make([]string, len(cols))
		for i, c := range cols {
			conds[i] = fmt.Sprintf("%s = %s", o.quoteIdentifier(c), o.placeholder(i+1))
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE %s", o.quoteIdentifier(o.table), strings.Join(conds, " AND "))
		if _, err := tx.ExecContext(ctx, query, vals...); err != nil {
			return fmt.Errorf("cdc delete exec: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete commit: %w", err)
	}
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
		// MySQL: ON DUPLICATE KEY UPDATE / PostgreSQL: ON CONFLICT (cols) DO UPDATE
		switch o.driver {
		case "mysql":
			vcol := o.quoteIdentifier(o.versionColumn)
			updates := make([]string, len(columns))
			for i, col := range quotedCols {
				if o.versionColumn != "" {
					// 버전 가드: 들어온 버전이 더 클 때만 값 갱신. 그 외엔 기존값 유지(=수렴, 순서 무관).
					updates[i] = fmt.Sprintf("%s=IF(VALUES(%s)>%s, VALUES(%s), %s)", col, vcol, vcol, col, col)
				} else {
					updates[i] = fmt.Sprintf("%s=VALUES(%s)", col, col)
				}
			}
			suffix = " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
		case "postgres":
			// PostgreSQL 은 충돌 대상 컬럼(unique/PK)을 명시해야 한다. conflict_columns 미지정 시
			// 어느 제약으로 수렴할지 알 수 없어 plain INSERT 로 두면 중복키 에러가 난다 → 명시 요구.
			if len(o.conflictColumns) == 0 {
				return fmt.Errorf("postgres upsert(on_conflict=update) requires conflict_columns")
			}
			conflictCols := make([]string, len(o.conflictColumns))
			for i, c := range o.conflictColumns {
				conflictCols[i] = o.quoteIdentifier(c)
			}
			updates := make([]string, 0, len(quotedCols))
			for _, col := range quotedCols {
				updates = append(updates, fmt.Sprintf("%s=EXCLUDED.%s", col, col))
			}
			suffix = fmt.Sprintf(" ON CONFLICT (%s) DO UPDATE SET %s",
				strings.Join(conflictCols, ", "), strings.Join(updates, ", "))
			if o.versionColumn != "" {
				// 버전 가드: 들어온 버전이 기존보다 클 때만 UPDATE(작거나 같으면 무시 → snapshot race 방지).
				vcol := o.quoteIdentifier(o.versionColumn)
				suffix += fmt.Sprintf(" WHERE %s.%s < EXCLUDED.%s", o.quoteIdentifier(o.table), vcol, vcol)
			}
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

// Finalize 파이프라인 종료 시 executor 가 호출한다 (Finalizable 구현).
// sweep 미설정이면 항상 no-op — sweep 을 안 쓰는 파이프라인은 영향이 없다.
// 부분 실패(success=false) 시 sweep 하면 못 받은 페이지의 행이 전부 오탐
// 삭제되므로, 전체 성공일 때만 실행한다.
func (o *SQLOutput) Finalize(ctx context.Context, success bool) error {
	if o.sweep == nil {
		return nil
	}
	if !success {
		log.Printf("[sql] sweep skipped: pipeline did not complete successfully (table=%s)", o.table)
		return nil
	}
	// sweep 판정 전에 잔여 버퍼가 반영되어야 마지막 배치의 행이 오탐되지 않는다.
	if err := o.Flush(ctx); err != nil {
		return fmt.Errorf("sweep aborted: final flush failed: %w", err)
	}

	query, args := o.buildSweepSQL()
	res, err := o.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sweep failed: %w", err)
	}
	affected, _ := res.RowsAffected()
	log.Printf("[sql] Sweep done (table=%s, mode=%s, affected=%d, before=%s)",
		o.table, o.sweep.Mode, affected, o.runStartedAt.Format(time.RFC3339))
	return nil
}

// buildSweepSQL sweep 모드별 정리 쿼리를 만든다.
//   - delete: 이번 실행에 나타나지 않은 행을 물리 삭제
//   - soft:   삭제 플래그 컬럼에 T 기록 (이미 플래그된 행은 최초 시각 보존을 위해 제외)
func (o *SQLOutput) buildSweepSQL() (string, []any) {
	table := o.quoteIdentifier(o.table)
	col := o.quoteIdentifier(o.sweep.Column)
	switch o.sweep.Mode {
	case "soft":
		soft := o.quoteIdentifier(o.sweep.SoftColumn)
		return fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s < %s AND %s IS NULL",
				table, soft, o.placeholder(1), col, o.placeholder(2), soft),
			[]any{o.runStartedAt, o.runStartedAt}
	default: // delete (NewSQLOutput 에서 검증됨)
		return fmt.Sprintf("DELETE FROM %s WHERE %s < %s",
			table, col, o.placeholder(1)), []any{o.runStartedAt}
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
