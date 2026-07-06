package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// PostgreSQL 논리 복제(logical replication) 기반 CDC.
// MySQL(canal) 경로와 달리 상태를 LSN 으로 관리하므로 별도 구조체로 분리한다.
// eventCh/errorCh/stopCh 는 CDCSource 것을 재사용해 Read 소비 루프는 드라이버 무관하게 동작한다.
type postgresReplicator struct {
	source *CDCSource

	slotName        string
	publicationName string

	standbyTimeout time.Duration

	// relations: RelationID → 릴레이션(테이블) 메타. DML 튜플은 위치 기반이라
	// 컬럼명/키 여부를 이 캐시에서 매핑한다(DML 앞에 먼저 도착).
	relations map[uint32]*pglogrepl.RelationMessage

	// prevRow: (RelationID → PK문자열 → 직전 전체 행). UPDATE 시 unchanged TOAST('u')
	// 컬럼은 값이 없어 이전 값을 재사용해야 null 오염을 막는다.
	prevRow map[uint32]map[string]map[string]any

	mu           sync.Mutex
	committedLSN pglogrepl.LSN // 다운스트림 소비 완료 LSN (checkpoint/슬롯 advance 기준)
	receivedLSN  pglogrepl.LSN // 수신(read-ahead) LSN
}

const (
	pgDefaultSlotName        = "conduix_slot"
	pgDefaultPublicationName = "conduix_pub"
	pgStandbyTimeout         = 10 * time.Second
)

func newPostgresReplicator(s *CDCSource) *postgresReplicator {
	slot := s.slotName
	if slot == "" {
		slot = pgDefaultSlotName
	}
	pub := s.publication
	if pub == "" {
		pub = pgDefaultPublicationName
	}
	return &postgresReplicator{
		source:          s,
		slotName:        slot,
		publicationName: pub,
		standbyTimeout:  pgStandbyTimeout,
		relations:       make(map[uint32]*pglogrepl.RelationMessage),
		prevRow:         make(map[uint32]map[string]map[string]any),
	}
}

// dsn 은 일반 연결용(퍼블리케이션 생성/존재 확인). sslmode 는 TLS 설정 없으면 disable.
func (r *postgresReplicator) dsn(replication bool) string {
	s := r.source
	port := 5432
	if s.port > 0 {
		port = int(s.port)
	}
	sslmode := "disable"
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		sslmode = pgSSLMode(s.tlsConfig.Mode)
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		s.host, port, s.username, s.password, s.database, sslmode)
	if replication {
		dsn += " replication=database"
	}
	return dsn
}

func pgSSLMode(mode string) string {
	switch strings.ToLower(mode) {
	case "skip-verify", "prefer", "allow", "require", "required":
		return "require"
	case "verify-ca":
		return "verify-ca"
	case "verify-identity", "verify-full":
		return "verify-full"
	default:
		return "require"
	}
}

// ensurePublication 은 별도(비복제) 연결에서 퍼블리케이션을 idempotent 하게 만든다.
// CREATE PUBLICATION IF NOT EXISTS 가 없으므로 pg_publication 조회로 가드한다.
func (r *postgresReplicator) ensurePublication(ctx context.Context) error {
	conn, err := pgconn.Connect(ctx, r.dsn(false))
	if err != nil {
		return fmt.Errorf("connect (publication): %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	exists, err := r.publicationExists(ctx, conn)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	var stmt string
	if len(r.source.tables) > 0 {
		stmt = fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s",
			quoteIdent(r.publicationName), strings.Join(quoteTables(r.source.tables), ", "))
	} else {
		stmt = fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", quoteIdent(r.publicationName))
	}
	if err := conn.Exec(ctx, stmt).Close(); err != nil {
		// 경합(다른 인스턴스가 먼저 생성)도 idempotent 하게 흡수.
		if exists, e := r.publicationExists(ctx, conn); e == nil && exists {
			return nil
		}
		return fmt.Errorf("create publication: %w", err)
	}
	slog.Default().Info("CDC pg publication created", "publication", r.publicationName, "tables", r.source.tables)
	return nil
}

func (r *postgresReplicator) publicationExists(ctx context.Context, conn *pgconn.PgConn) (bool, error) {
	res := conn.ExecParams(ctx,
		"SELECT 1 FROM pg_publication WHERE pubname = $1",
		[][]byte{[]byte(r.publicationName)}, nil, nil, nil)
	result := res.Read()
	if result.Err != nil {
		return false, fmt.Errorf("check publication: %w", result.Err)
	}
	return len(result.Rows) > 0, nil
}

// ensureSlot 은 pgoutput 슬롯을 idempotent 하게 만든다(이미 있으면 42710 흡수).
func (r *postgresReplicator) ensureSlot(ctx context.Context, conn *pgconn.PgConn) error {
	_, err := pglogrepl.CreateReplicationSlot(ctx, conn, r.slotName, "pgoutput",
		pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.LogicalReplication})
	if err == nil {
		slog.Default().Info("CDC pg replication slot created", "slot", r.slotName)
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42710" { // duplicate_object → 이미 존재
		return nil
	}
	return fmt.Errorf("create replication slot: %w", err)
}

// run 은 복제 연결을 열고 스트림을 소비한다(블로킹). 재연결은 runCanalWithReconnect 와
// 동일한 패턴으로 상위(runPostgresWithReconnect)에서 감싼다. 반환 nil=정상 종료(ctx/stop).
func (r *postgresReplicator) run(ctx context.Context) error {
	if err := r.ensurePublication(ctx); err != nil {
		return err
	}

	conn, err := pgconn.Connect(ctx, r.dsn(true))
	if err != nil {
		return fmt.Errorf("connect (replication): %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if err := r.ensureSlot(ctx, conn); err != nil {
		return err
	}

	sys, err := pglogrepl.IdentifySystem(ctx, conn)
	if err != nil {
		return fmt.Errorf("identify system: %w", err)
	}

	startLSN := sys.XLogPos
	r.mu.Lock()
	if r.committedLSN != 0 {
		startLSN = r.committedLSN // checkpoint 복원 지점부터 재개
	}
	r.receivedLSN = startLSN
	r.mu.Unlock()

	pluginArgs := []string{
		"proto_version '2'",
		fmt.Sprintf("publication_names '%s'", r.publicationName),
	}
	if err := pglogrepl.StartReplication(ctx, conn, r.slotName, startLSN,
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		return fmt.Errorf("start replication: %w", err)
	}
	slog.Default().Info("CDC pg replication started", "slot", r.slotName, "start_lsn", startLSN)

	return r.receiveLoop(ctx, conn)
}

func (r *postgresReplicator) receiveLoop(ctx context.Context, conn *pgconn.PgConn) error {
	s := r.source
	nextStandby := timeNow().Add(r.standbyTimeout)
	inStream := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.stopCh:
			return nil
		default:
		}

		if timeNow().After(nextStandby) {
			r.mu.Lock()
			committed := r.committedLSN
			r.mu.Unlock()
			if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
				pglogrepl.StandbyStatusUpdate{WALWritePosition: committed}); err != nil {
				return fmt.Errorf("send standby update: %w", err)
			}
			nextStandby = timeNow().Add(r.standbyTimeout)
		}

		rctx, cancel := context.WithDeadline(ctx, nextStandby)
		rawMsg, err := conn.ReceiveMessage(rctx)
		cancel()
		if err != nil {
			if pgconn.Timeout(err) {
				continue // standby 갱신 주기 → 루프 재진입
			}
			select {
			case <-s.stopCh:
				return nil
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("receive message: %w", err)
		}

		switch msg := rawMsg.(type) {
		case *pgproto3.CopyData:
			if len(msg.Data) == 0 {
				continue
			}
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pka, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					return fmt.Errorf("parse keepalive: %w", err)
				}
				if pka.ReplyRequested {
					nextStandby = time.Time{} // 다음 루프에서 즉시 응답
				}
			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					return fmt.Errorf("parse xlogdata: %w", err)
				}
				endLSN := xld.WALStart + pglogrepl.LSN(len(xld.WALData))
				r.mu.Lock()
				r.receivedLSN = endLSN
				r.mu.Unlock()
				if err := r.handleLogical(ctx, xld.WALData, endLSN, &inStream); err != nil {
					if errors.Is(err, errStopped) {
						return nil
					}
					return err
				}
			}
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("postgres replication error: %s", msg.Message)
		}
	}
}

var errStopped = errors.New("cdc stopped")

// handleLogical 은 pgoutput 메시지를 파싱해 CDCEvent 로 eventCh 에 흘린다.
// endLSN 은 이 이벤트의 소비 커밋 기준 LSN(Read 소비 루프가 committedLSN 으로 반영).
func (r *postgresReplicator) handleLogical(ctx context.Context, data []byte, endLSN pglogrepl.LSN, inStream *bool) error {
	logical, err := pglogrepl.ParseV2(data, *inStream)
	if err != nil {
		return fmt.Errorf("parse logical: %w", err)
	}

	switch m := logical.(type) {
	case *pglogrepl.RelationMessageV2:
		r.relations[m.RelationID] = &m.RelationMessage
	case *pglogrepl.InsertMessageV2:
		return r.emit(ctx, CDCEventInsert, m.RelationID, m.Tuple, nil, endLSN)
	case *pglogrepl.UpdateMessageV2:
		return r.emit(ctx, CDCEventUpdate, m.RelationID, m.NewTuple, m.OldTuple, endLSN)
	case *pglogrepl.DeleteMessageV2:
		return r.emit(ctx, CDCEventDelete, m.RelationID, nil, m.OldTuple, endLSN)
	case *pglogrepl.StreamStartMessageV2:
		*inStream = true
	case *pglogrepl.StreamStopMessageV2:
		*inStream = false
	}
	return nil
}

func (r *postgresReplicator) emit(ctx context.Context, typ CDCEventType, relID uint32, newTuple, oldTuple *pglogrepl.TupleData, endLSN pglogrepl.LSN) error {
	rel, ok := r.relations[relID]
	if !ok {
		// RelationMessage 를 아직 못 받음(정상 스트림에선 선행 도착). 드롭하지 않고 로그만.
		slog.Default().Warn("CDC pg unknown relation, skipping event", "relation_id", relID)
		return nil
	}

	var newData, oldData map[string]any
	if newTuple != nil {
		newData = r.tupleToMap(relID, rel, newTuple)
	}
	if oldTuple != nil {
		oldData = tupleToMapRaw(rel, oldTuple)
	}

	pkCols, pkVals := pgPrimaryKey(rel, newData, oldData)

	// delete 는 newTuple==nil → newData==nil(Data 비움). old/PK 로 싱크가 DELETE 구성.
	event := &CDCEvent{
		Type:           typ,
		Database:       r.source.database,
		Table:          fmt.Sprintf("%s.%s", rel.Namespace, rel.RelationName),
		Timestamp:      timeNow(),
		Data:           newData,
		OldData:        oldData,
		PrimaryKey:     pkVals,
		PrimaryKeyCols: pkCols,
		lsn:            endLSN,
	}

	select {
	case r.source.eventCh <- event:
		return nil
	case <-r.source.stopCh:
		return errStopped
	case <-ctx.Done():
		return errStopped
	}
}

// tupleToMap 은 새 튜플을 map 으로 변환하고, unchanged TOAST('u') 는 직전 행 값으로 채운다.
// 완성된 행을 prevRow 에 저장해 다음 UPDATE 의 TOAST 재사용에 쓴다.
func (r *postgresReplicator) tupleToMap(relID uint32, rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) map[string]any {
	data := make(map[string]any, len(rel.Columns))
	var toastCols []string
	for i, col := range rel.Columns {
		if i >= len(tuple.Columns) {
			break
		}
		tc := tuple.Columns[i]
		switch tc.DataType {
		case pglogrepl.TupleDataTypeNull:
			data[col.Name] = nil
		case pglogrepl.TupleDataTypeToast:
			toastCols = append(toastCols, col.Name) // 이전 값으로 채운다(아래)
		default:
			data[col.Name] = string(tc.Data)
		}
	}

	pkKey := pkString(rel, data)
	if len(toastCols) > 0 {
		if prevByKey, ok := r.prevRow[relID]; ok {
			if prev, ok := prevByKey[pkKey]; ok {
				for _, c := range toastCols {
					data[c] = prev[c]
				}
			}
		}
	}

	if r.prevRow[relID] == nil {
		r.prevRow[relID] = make(map[string]map[string]any)
	}
	r.prevRow[relID][pkKey] = data
	return data
}

// tupleToMapRaw 은 old 튜플(키/전체행)을 map 으로 변환한다(TOAST 채움 없음 — old 는 키 위주).
func tupleToMapRaw(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) map[string]any {
	data := make(map[string]any, len(rel.Columns))
	for i, col := range rel.Columns {
		if i >= len(tuple.Columns) {
			break
		}
		tc := tuple.Columns[i]
		switch tc.DataType {
		case pglogrepl.TupleDataTypeNull:
			data[col.Name] = nil
		case pglogrepl.TupleDataTypeToast:
			// old 튜플의 TOAST 는 키가 아니면 없을 수 있음 → 생략.
		default:
			data[col.Name] = string(tc.Data)
		}
	}
	return data
}

// pgPrimaryKey 는 replica identity 키 컬럼(Flags==1)명과 값을 뽑는다.
// 값은 new(있으면) → old 순으로 찾는다(delete 는 old 만 존재).
func pgPrimaryKey(rel *pglogrepl.RelationMessage, newData, oldData map[string]any) ([]string, []any) {
	var cols []string
	var vals []any
	for _, col := range rel.Columns {
		if col.Flags != 1 {
			continue
		}
		cols = append(cols, col.Name)
		if newData != nil {
			if v, ok := newData[col.Name]; ok {
				vals = append(vals, v)
				continue
			}
		}
		if oldData != nil {
			vals = append(vals, oldData[col.Name])
			continue
		}
		vals = append(vals, nil)
	}
	return cols, vals
}

// pkString 은 prevRow 캐시 키(키 컬럼값 결합). 키 없으면 빈 문자열(캐시 무의미하지만 안전).
func pkString(rel *pglogrepl.RelationMessage, data map[string]any) string {
	var b strings.Builder
	for _, col := range rel.Columns {
		if col.Flags != 1 {
			continue
		}
		if v, ok := data[col.Name]; ok {
			fmt.Fprintf(&b, "%v|", v)
		}
	}
	return b.String()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteTables 는 "schema.table" 또는 "table" 을 각 부분 quote 한다.
func quoteTables(tables []string) []string {
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		parts := strings.SplitN(t, ".", 2)
		for i := range parts {
			parts[i] = quoteIdent(parts[i])
		}
		out = append(out, strings.Join(parts, "."))
	}
	return out
}
