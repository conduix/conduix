package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
	"github.com/jackc/pglogrepl"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// timeNow 는 이벤트 타임스탬프/백오프 계산에 쓴다(테스트 대체 여지 확보).
var timeNow = time.Now

// CDCEventType CDC 이벤트 타입
type CDCEventType string

const (
	CDCEventInsert CDCEventType = "insert"
	CDCEventUpdate CDCEventType = "update"
	CDCEventDelete CDCEventType = "delete"
	CDCEventDDL    CDCEventType = "ddl" // 스키마 변경(ALTER/CREATE/DROP/RENAME)
)

// CDCSource CDC(Change Data Capture) 소스
// MySQL binlog 또는 PostgreSQL WAL을 통해 변경 사항 캡처
type CDCSource struct {
	driver      string // mysql, postgres
	host        string
	port        uint16
	username    string
	password    string
	database    string
	tables      []string // 감시할 테이블 목록 (빈 경우 전체)
	serverID    uint32   // MySQL server ID (binlog용)
	slotName    string   // PostgreSQL replication slot name
	publication string   // PostgreSQL publication name

	// TLS 설정
	tlsConfig *config.DBTLSConfig

	canal    *canal.Canal
	mu       sync.RWMutex
	running  bool
	position mysql.Position // canal read-ahead position (binlog 읽은 위치)

	// committedPos: 파이프라인이 실제 소비(records 채널로 전달)한 마지막 위치.
	// checkpoint 는 이 값을 저장한다 → 재시작 시 read-ahead 로 앞서간 미소비 구간을 다시 읽어 유실 방지.
	committedPos  mysql.Position
	curGTID       string // read-ahead GTID (OnPosSynced 갱신)
	committedGTID string // 소비 완료된 GTID set (있으면 position 보다 우선 — 페일오버 강함)

	// PostgreSQL 논리 복제 소비 완료 LSN. Read 소비 루프가 event.lsn 으로 갱신하고
	// pg 복제기(postgresReplicator)가 standby update/재시작 시작점으로 읽는다.
	committedLSN pglogrepl.LSN
	pg           *postgresReplicator

	// 이벤트 핸들러
	eventCh chan *CDCEvent
	errorCh chan error

	// 종료 신호. OnRow의 eventCh 블로킹 전송이 Stop 시 빠져나가는 데 쓴다.
	// (채널이 가득 차도 이벤트를 drop 하지 않고 backpressure 를 걸되, 종료는 가능하게.)
	stopCh chan struct{}

	// at-least-once ack: 채널 전송 시점이 아니라 Ack(sink flush 성공) 시점에 committed 를 전진시킨다.
	// CDC 는 단일 순서 스트림이라 seq(단조 증가) 로 이벤트를 식별하고, Ack 된 최대 seq 의 위치까지 커밋.
	// pending: seq → 이벤트의 위치(pos/gtid/lsn). Metadata.Offset 에 seq 를 실어 executor 가 Ack 로 돌려준다.
	ackMu    sync.Mutex
	ackSeq   uint64                  // 다음 부여할 seq
	pendingP map[uint64]cdcAckOffset // seq → 위치
}

// cdcAckOffset 은 한 CDC 이벤트의 커밋 위치(ack 시 committed 로 반영).
type cdcAckOffset struct {
	pos  mysql.Position
	gtid string
	lsn  pglogrepl.LSN
}

// CDCEvent CDC 이벤트
type CDCEvent struct {
	Type           CDCEventType   `json:"type"`
	Database       string         `json:"database"`
	Table          string         `json:"table"`
	Timestamp      time.Time      `json:"timestamp"`
	Data           map[string]any `json:"data"`                // 현재 데이터 (INSERT, UPDATE)
	OldData        map[string]any `json:"old_data"`            // 이전 데이터 (UPDATE, DELETE)
	PrimaryKey     []any          `json:"primary_key"`         // PK 값들
	PrimaryKeyCols []string       `json:"primary_key_columns"` // PK 컬럼명 (delete WHERE 구성용)

	// 이 이벤트 시점의 binlog position/GTID. checkpoint 커밋 기준으로 쓴다
	// (canal read-ahead 가 아니라, 파이프라인이 실제 소비한 위치).
	pos  mysql.Position `json:"-"`
	gtid string         `json:"-"`

	// PostgreSQL 논리 복제 커밋 기준 LSN(MySQL 경로에선 0).
	lsn pglogrepl.LSN `json:"-"`
}

// NewCDCSource CDC 소스 생성
func NewCDCSource(cfg config.SourceV2) (*CDCSource, error) {
	// 드라이버 조기 검증: 미지원 드라이버는 실행 시작 전에 실패시킨다(미동작을 실행 후 발견 방지).
	switch cfg.Driver {
	case "mysql", "postgres":
		// 지원됨
	default:
		return nil, fmt.Errorf("unsupported CDC driver: %q (supported: mysql, postgres)", cfg.Driver)
	}

	defaultPort := uint16(3306)
	if cfg.Driver == "postgres" {
		defaultPort = 5432
	}
	port := defaultPort
	if cfg.Port > 0 {
		port = uint16(cfg.Port)
	}

	serverID := uint32(101)
	if cfg.ServerID > 0 {
		serverID = cfg.ServerID
	}

	s := &CDCSource{
		driver:      cfg.Driver,
		host:        cfg.Host,
		port:        port,
		username:    cfg.Username,
		password:    cfg.Password,
		database:    cfg.Database,
		tables:      cfg.Tables,
		serverID:    serverID,
		slotName:    cfg.SlotName,
		publication: cfg.Publication,
		tlsConfig:   cfg.DBTLS,
		eventCh:     make(chan *CDCEvent, 1000),
		errorCh:     make(chan error, 10),
		stopCh:      make(chan struct{}),
		pendingP:    make(map[uint64]cdcAckOffset),
	}

	// 시작 지점 지정(checkpoint 없을 때 시작점). bulk↔CDC 경계 맞춤용.
	// committedPos/GTID(MySQL)·committedLSN(PostgreSQL)로 설정 → Read 가 이 값을 시작점으로 쓴다.
	if cfg.Driver == "postgres" {
		if cfg.StartLSN != "" {
			lsn, err := pglogrepl.ParseLSN(cfg.StartLSN)
			if err != nil {
				return nil, fmt.Errorf("invalid start_lsn %q: %w", cfg.StartLSN, err)
			}
			s.committedLSN = lsn
		}
		s.pg = newPostgresReplicator(s)
		return s, nil
	}

	if cfg.StartGTID != "" {
		if _, err := mysql.ParseMysqlGTIDSet(cfg.StartGTID); err != nil {
			return nil, fmt.Errorf("invalid start_gtid %q: %w", cfg.StartGTID, err)
		}
		s.committedGTID = cfg.StartGTID
		s.curGTID = cfg.StartGTID
	} else if cfg.StartPosition != "" {
		parts := strings.SplitN(cfg.StartPosition, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid start_position %q (want \"binlog_file:pos\")", cfg.StartPosition)
		}
		pos, err := ParseNumeric(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid start_position offset %q: %w", parts[1], err)
		}
		p := mysql.Position{Name: parts[0], Pos: uint32(pos)}
		s.position = p
		s.committedPos = p
	}

	return s, nil
}

func (s *CDCSource) Name() string {
	return "cdc"
}

func (s *CDCSource) Open(ctx context.Context) error {
	switch s.driver {
	case "mysql":
		// canal 은 Read→runCanalOnce 에서 매 (재)연결마다 새로 만든다(끊긴 canal 은 재사용 불가).
		// 여기서 미리 만들면 runCanalOnce 가 그것을 Close 후 재생성해야 해 낭비·경합이다.
		return nil
	case "postgres":
		// pg 도 복제 연결을 Read(run) 시점에 연다(재연결마다 새 연결 필요). 여기선 준비만.
		if s.pg == nil {
			s.pg = newPostgresReplicator(s)
		}
		return nil
	default:
		return fmt.Errorf("unsupported CDC driver: %s", s.driver)
	}
}

func (s *CDCSource) openMySQL() error {
	cfg := canal.NewDefaultConfig()
	cfg.Addr = fmt.Sprintf("%s:%d", s.host, s.port)
	cfg.User = s.username
	cfg.Password = s.password
	cfg.ServerID = s.serverID
	cfg.Flavor = "mysql"

	// 감시할 테이블 설정
	if len(s.tables) > 0 {
		cfg.IncludeTableRegex = s.tables
	}

	// TLS 설정
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		tlsCfg, err := buildCDCTLSConfig(s.tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
		if tlsCfg != nil {
			cfg.TLSConfig = tlsCfg
			slog.Default().Info("CDC MySQL TLS enabled", "host", s.host, "mode", s.tlsConfig.Mode)
		}
	}

	c, err := canal.NewCanal(cfg)
	if err != nil {
		return fmt.Errorf("failed to create canal: %w", err)
	}

	// 이벤트 핸들러 등록
	c.SetEventHandler(&mysqlEventHandler{source: s})

	s.mu.Lock()
	s.canal = c
	s.mu.Unlock()

	return nil
}

// buildCDCTLSConfig CDC용 TLS 설정 생성
func buildCDCTLSConfig(cfg *config.DBTLSConfig) (*tls.Config, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{}

	// SSL 모드에 따른 설정
	switch strings.ToLower(cfg.Mode) {
	case "skip-verify", "prefer", "allow":
		tlsConfig.InsecureSkipVerify = true
	case "required", "require", "verify-ca":
		tlsConfig.InsecureSkipVerify = false
	case "verify-identity", "verify-full":
		tlsConfig.InsecureSkipVerify = false
		if cfg.ServerName != "" {
			tlsConfig.ServerName = cfg.ServerName
		}
	default:
		tlsConfig.InsecureSkipVerify = false
	}

	// CA 인증서 로드
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(expandEnvVars(cfg.CACert))
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// 클라이언트 인증서 (mTLS)
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(
			expandEnvVars(cfg.ClientCert),
			expandEnvVars(cfg.ClientKey),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

func (s *CDCSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	s.mu.Lock()
	s.running = true
	// 재시작 대비: 이전 Stop 에서 닫힌 stopCh 를 새로 연다.
	select {
	case <-s.stopCh: // 이미 닫힘 → 새로 생성
		s.stopCh = make(chan struct{})
	default:
	}
	s.mu.Unlock()

	// 변경 스트림 시작 (연결 끊김 시 지수 백오프 재연결). 드라이버별 1회 실행 함수만 다르다.
	go s.runWithReconnect(ctx)

	// 이벤트 변환 및 전달
	go func() {
		defer close(records)
		defer close(errs)

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-s.eventCh:
				if event == nil {
					continue
				}

				record := s.convertEventToRecord(event)
				// at-least-once: 채널 전송 시점이 아니라 Ack 시점에 committed 를 전진시킨다.
				// 이벤트마다 seq 를 부여해 pending 에 위치를 보관하고, Metadata 로 seq 를 실어 보낸다.
				s.ackMu.Lock()
				s.ackSeq++
				seq := s.ackSeq
				s.pendingP[seq] = cdcAckOffset{pos: event.pos, gtid: event.gtid, lsn: event.lsn}
				s.ackMu.Unlock()
				record.Metadata.Source = "cdc"
				record.Metadata.PartitionKey = s.database
				record.Metadata.Offset = strconv.FormatUint(seq, 10)

				select {
				case records <- record:
				case <-ctx.Done():
					return
				}
			case err := <-s.errorCh:
				if err != nil {
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

// runWithReconnect 는 변경 스트림(MySQL canal / PostgreSQL 논리복제)을 실행하고,
// 연결이 끊기면 지수 백오프로 재연결한다. 재개는 committedPos/GTID/LSN 부터라 유실이 없다.
// ctx 취소·stopCh 종료·연속 실패 상한 도달 시 멈춘다.
func (s *CDCSource) runWithReconnect(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	const maxConsecutiveFailures = 10
	failures := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.mu.RLock()
		stopCh := s.stopCh
		running := s.running
		s.mu.RUnlock()
		if !running {
			return
		}
		select {
		case <-stopCh:
			return
		default:
		}

		err := s.runOnce(ctx)
		if err == nil {
			// 정상 종료(Stop/ctx) — 재연결 불필요.
			return
		}

		failures++
		if failures >= maxConsecutiveFailures {
			s.sendError(fmt.Errorf("cdc: giving up after %d consecutive %s failures: %w", failures, s.driver, err))
			return
		}
		slog.Default().Warn("CDC stream disconnected, will reconnect",
			"driver", s.driver, "host", s.host, "attempt", failures, "backoff", backoff, "error", err)

		// 백오프 대기(취소 가능)
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-stopCh:
			t.Stop()
			return
		case <-t.C:
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runOnce 는 드라이버별 변경 스트림을 1회 실행(블로킹)한다. 반환 nil=정상 종료, err=끊김.
func (s *CDCSource) runOnce(ctx context.Context) error {
	if s.driver == "postgres" {
		if s.pg == nil {
			s.pg = newPostgresReplicator(s)
		}
		return s.pg.run(ctx)
	}
	return s.runCanalOnce(ctx)
}

// runCanalOnce 는 canal 을 committedPos/GTID 부터 1회 실행(블로킹)한다.
// 재연결을 위해 매번 canal 을 새로 만든다(끊긴 canal 은 재사용 불가).
// 반환: nil=정상 종료(Stop/ctx), err=연결 실패/끊김.
func (s *CDCSource) runCanalOnce(ctx context.Context) error {
	// 이전 canal 정리 후 새로 생성. canal.Close() 는 핸들러(s.mu 잡음)를 동기 호출하므로
	// 락 밖에서 Close 한다(Close() 와 동일한 재진입 데드락 회피).
	s.mu.Lock()
	old := s.canal
	s.canal = nil
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if err := s.openMySQL(); err != nil {
		return fmt.Errorf("reopen canal: %w", err)
	}

	s.mu.RLock()
	c := s.canal
	gtidStr := s.committedGTID
	pos := s.committedPos
	if pos.Name == "" {
		pos = s.position
	}
	s.mu.RUnlock()
	if c == nil {
		return fmt.Errorf("canal not initialized")
	}

	// GTID 우선(페일오버·binlog 교체에 강함), 없으면 position.
	if gtidStr != "" {
		gtidSet, err := mysql.ParseMysqlGTIDSet(gtidStr)
		if err != nil {
			return fmt.Errorf("parse GTID %q: %w", gtidStr, err)
		}
		return c.StartFromGTID(gtidSet) // 블로킹: 끊기면 err, Close 되면 nil
	}
	if pos.Name == "" {
		currentPos, err := c.GetMasterPos()
		if err != nil {
			return fmt.Errorf("get master position: %w", err)
		}
		pos = currentPos
	}
	return c.RunFrom(pos) // 블로킹
}

// sendError 는 fatal 에러를 errorCh 로 보낸다(Read 소비 루프가 errs 로 전달).
func (s *CDCSource) sendError(err error) {
	select {
	case s.errorCh <- err:
	default:
	}
}

func (s *CDCSource) convertEventToRecord(event *CDCEvent) Record {
	data := map[string]any{
		"_cdc_type":  string(event.Type),
		"_database":  event.Database,
		"_table":     event.Table,
		"_timestamp": event.Timestamp,
	}

	// 현재 데이터 병합
	maps.Copy(data, event.Data)

	// 이전 데이터가 있으면 _old_ 접두사로 추가
	if event.OldData != nil {
		oldData := make(map[string]any)
		maps.Copy(oldData, event.OldData)
		data["_old_data"] = oldData
	}

	// PK 정보 (delete 반영 시 WHERE 절 구성용: 컬럼명↔값)
	if len(event.PrimaryKey) > 0 {
		data["_primary_key"] = event.PrimaryKey
	}
	if len(event.PrimaryKeyCols) > 0 {
		data["_primary_key_columns"] = event.PrimaryKeyCols
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "cdc",
			Origin:    fmt.Sprintf("%s.%s", event.Database, event.Table),
			Timestamp: event.Timestamp.UnixMilli(),
		},
	}
}

// GetCheckpoint 현재 체크포인트(binlog position) 반환
func (s *CDCSource) GetCheckpoint() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"binlog_file": s.position.Name,
		"binlog_pos":  s.position.Pos,
	}
}

// SetCheckpoint 체크포인트 설정 (복구용)
func (s *CDCSource) SetCheckpoint(checkpoint map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if name, ok := checkpoint["binlog_file"].(string); ok {
		s.position.Name = name
	}
	if pos, ok := checkpoint["binlog_pos"].(uint32); ok {
		s.position.Pos = pos
	} else if pos, ok := checkpoint["binlog_pos"].(float64); ok {
		s.position.Pos = uint32(pos)
	}

	return nil
}

// SourceType 소스 타입 반환 (CheckpointableSource 구현)
func (s *CDCSource) SourceType() string {
	return "cdc"
}

// Ack 는 다운스트림이 sink 적재까지 성공한 레코드(seq)를 받아, committed 를 전진시킨다(AckableSource).
// CDC 는 단일 순서 스트림이라 ack 된 최대 seq 의 위치까지 커밋하면 된다(그 이하는 모두 적재 완료).
// 채널 전송 시점이 아니라 이 시점에 커밋하므로, 크래시 시 미적재분은 committed 이전이라 재처리(유실 없음).
func (s *CDCSource) Ack(offsets []RecordOffset) {
	var maxSeq uint64
	for _, o := range offsets {
		seq, err := strconv.ParseUint(o.Offset, 10, 64)
		if err != nil {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	if maxSeq == 0 {
		return
	}

	s.ackMu.Lock()
	// ack 된 최대 seq 의 위치를 찾고, 그 이하 pending 정리.
	off, ok := s.pendingP[maxSeq]
	for seq := range s.pendingP {
		if seq <= maxSeq {
			delete(s.pendingP, seq)
		}
	}
	s.ackMu.Unlock()
	if !ok {
		return
	}

	// committed 전진(재시작 시작점). GetSourceCheckpoints 가 이 값을 저장한다.
	s.mu.Lock()
	if off.pos.Name != "" {
		s.committedPos = off.pos
	}
	if off.gtid != "" {
		s.committedGTID = off.gtid
	}
	if off.lsn != 0 {
		s.committedLSN = off.lsn
		if s.pg != nil {
			s.pg.mu.Lock()
			s.pg.committedLSN = off.lsn
			s.pg.mu.Unlock()
		}
	}
	s.mu.Unlock()
}

// GetSourceCheckpoints 현재 모든 체크포인트 반환 (CheckpointableSource 구현)
func (s *CDCSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// PostgreSQL 은 LSN 하나로 위치를 표현한다(MySQL 의 file:pos+GTID 와 다름).
	if s.driver == "postgres" {
		return []*SourceCheckpoint{
			{
				PartitionKey: fmt.Sprintf("%s:lsn", s.database),
				OffsetValue:  s.committedLSN.String(),
				OffsetType:   "lsn",
				RecordCount:  int64(s.committedLSN),
				UpdatedAt:    time.Now(),
			},
		}
	}

	// checkpoint 는 read-ahead(s.position)가 아니라 committedPos(파이프라인이 실제 소비한 위치)를
	// 저장한다. 아직 소비 안 된 이벤트가 아직 커밋되지 않아야 재시작 시 다시 읽어 유실을 막는다.
	cp := s.committedPos
	if cp.Name == "" {
		// 아직 아무것도 소비 안 함 → 시작 position 을 그대로(재시작 시 같은 지점부터).
		cp = s.position
	}
	partitionKey := fmt.Sprintf("%s:%s", s.database, cp.Name)
	offsetValue := fmt.Sprintf("%s:%d", cp.Name, cp.Pos)

	cps := []*SourceCheckpoint{
		{
			PartitionKey: partitionKey,
			OffsetValue:  offsetValue,
			OffsetType:   "string", // binlog position은 문자열 형식
			RecordCount:  int64(cp.Pos),
			UpdatedAt:    time.Now(),
		},
	}

	// GTID 도 함께 저장(있으면). 복원 시 GTID 를 우선 사용 — 서버 페일오버·binlog 교체에 강함.
	if s.committedGTID != "" {
		cps = append(cps, &SourceCheckpoint{
			PartitionKey: fmt.Sprintf("%s:gtid", s.database),
			OffsetValue:  s.committedGTID,
			OffsetType:   "gtid",
			UpdatedAt:    time.Now(),
		})
	}
	return cps
}

// SetSourceCheckpoints 체크포인트 설정 (CheckpointableSource 구현)
func (s *CDCSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cp := range checkpoints {
		if cp.OffsetType == "lsn" {
			lsn, err := pglogrepl.ParseLSN(cp.OffsetValue)
			if err != nil {
				slog.Default().Error("CDC failed to parse LSN checkpoint", "host", s.host, "lsn", cp.OffsetValue, "error", err)
				continue
			}
			s.committedLSN = lsn
			if s.pg != nil {
				s.pg.mu.Lock()
				s.pg.committedLSN = lsn
				s.pg.mu.Unlock()
			}
			slog.Default().Info("CDC restored LSN checkpoint", "host", s.host, "lsn", cp.OffsetValue)
			continue
		}
		if cp.OffsetType == "gtid" {
			s.committedGTID = cp.OffsetValue
			s.curGTID = cp.OffsetValue
			slog.Default().Info("CDC restored GTID checkpoint", "host", s.host, "gtid", cp.OffsetValue)
			continue
		}
		if cp.OffsetType != "string" {
			continue
		}

		// offsetValue 형식: "binlog_file:position"
		parts := strings.SplitN(cp.OffsetValue, ":", 2)
		if len(parts) != 2 {
			slog.Default().Warn("CDC invalid checkpoint format", "host", s.host, "offset_value", cp.OffsetValue)
			continue
		}

		s.position.Name = parts[0]
		pos, err := ParseNumeric(parts[1])
		if err != nil {
			slog.Default().Error("CDC failed to parse checkpoint position",
				"host", s.host, "position", parts[1], "error", err)
			continue
		}
		s.position.Pos = uint32(pos)
		s.committedPos = s.position // 복원 지점이 곧 마지막 커밋 지점
		slog.Default().Info("CDC restored checkpoint",
			"host", s.host, "partition_key", cp.PartitionKey,
			"binlog_file", s.position.Name, "position", s.position.Pos)
	}

	return nil
}

func (s *CDCSource) Close() error {
	// canal.Close() 는 동기적으로 이벤트 핸들러(OnPosSynced 등)를 호출하는데, 그 핸들러가
	// s.mu 를 잡는다. 따라서 canal.Close() 를 s.mu 를 쥔 채 호출하면 재진입 데드락이 된다.
	// → 락 안에서는 상태 정리만 하고 canal 참조를 빼낸 뒤, 락을 놓고 Close() 를 호출한다.
	s.mu.Lock()
	s.running = false
	// stopCh 닫아 OnRow 의 블로킹 전송을 깨운다(중복 close 방지).
	select {
	case <-s.stopCh: // 이미 닫힘
	default:
		close(s.stopCh)
	}
	c := s.canal
	s.canal = nil
	s.mu.Unlock()

	if c != nil {
		c.Close() // 락 밖: 핸들러 콜백이 s.mu 를 잡아도 데드락 없음.
	}
	return nil
}

// mysqlEventHandler MySQL binlog 이벤트 핸들러
type mysqlEventHandler struct {
	canal.DummyEventHandler
	source *CDCSource
}

func (h *mysqlEventHandler) OnRow(e *canal.RowsEvent) error {
	h.source.mu.RLock()
	running := h.source.running
	stopCh := h.source.stopCh
	pos := h.source.position // 이 이벤트가 속한 read-ahead position (소비 시 커밋 기준)
	gtid := h.source.curGTID
	h.source.mu.RUnlock()

	if !running {
		return nil
	}

	var eventType CDCEventType
	switch e.Action {
	case canal.InsertAction:
		eventType = CDCEventInsert
	case canal.UpdateAction:
		eventType = CDCEventUpdate
	case canal.DeleteAction:
		eventType = CDCEventDelete
	default:
		return nil
	}

	columns := e.Table.Columns

	// UPDATE는 old/new 쌍으로 온다
	if e.Action == canal.UpdateAction {
		for i := 0; i < len(e.Rows); i += 2 {
			if i+1 >= len(e.Rows) {
				break
			}

			oldRow := e.Rows[i]
			newRow := e.Rows[i+1]

			event := &CDCEvent{
				Type:           eventType,
				Database:       e.Table.Schema,
				Table:          e.Table.Name,
				Timestamp:      time.Now(),
				Data:           rowToMap(columns, newRow),
				OldData:        rowToMap(columns, oldRow),
				PrimaryKey:     getPrimaryKeyValues(e.Table, newRow),
				PrimaryKeyCols: getPrimaryKeyColumns(e.Table),
				pos:            pos,
				gtid:           gtid,
			}

			// 블로킹 전송: 채널이 가득 차면 backpressure(canal 이 느려짐). 종료 시에만 탈출.
			select {
			case h.source.eventCh <- event:
			case <-stopCh:
				return nil
			}
		}
	} else {
		for _, row := range e.Rows {
			event := &CDCEvent{
				Type:           eventType,
				Database:       e.Table.Schema,
				Table:          e.Table.Name,
				Timestamp:      time.Now(),
				PrimaryKey:     getPrimaryKeyValues(e.Table, row),
				PrimaryKeyCols: getPrimaryKeyColumns(e.Table),
				pos:            pos,
				gtid:           gtid,
			}

			if eventType == CDCEventDelete {
				event.OldData = rowToMap(columns, row)
			} else {
				event.Data = rowToMap(columns, row)
			}

			select {
			case h.source.eventCh <- event:
			case <-stopCh:
				return nil
			}
		}
	}

	return nil
}

// OnDDL 은 스키마 변경(ALTER/CREATE/DROP/RENAME)을 감지한다.
// canal 이 스키마 캐시를 갱신하므로 컬럼 매핑은 자동 갱신되지만, 스키마 변경이 조용히 무시되지 않도록
// DDL 이벤트를 파이프라인에 흘리고(type=ddl) 로그로 남긴다(downstream 이 스키마 진화를 인지 가능).
func (h *mysqlEventHandler) OnDDL(header *replication.EventHeader, nextPos mysql.Position, queryEvent *replication.QueryEvent) error {
	h.source.mu.RLock()
	running := h.source.running
	stopCh := h.source.stopCh
	pos := h.source.position
	gtid := h.source.curGTID
	h.source.mu.RUnlock()
	if !running || queryEvent == nil {
		return nil
	}

	ddlSQL := string(queryEvent.Query)
	schema := string(queryEvent.Schema)
	slog.Default().Info("CDC schema change (DDL)", "host", h.source.host, "schema", schema, "query", ddlSQL)

	event := &CDCEvent{
		Type:      CDCEventDDL,
		Database:  schema,
		Timestamp: time.Now(),
		Data:      map[string]any{"ddl": ddlSQL},
		pos:       pos,
		gtid:      gtid,
	}
	select {
	case h.source.eventCh <- event:
	case <-stopCh:
	}
	return nil
}

func (h *mysqlEventHandler) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	h.source.mu.Lock()
	h.source.position = pos
	if set != nil {
		h.source.curGTID = set.String() // read-ahead GTID (커밋은 소비 시점에)
	}
	h.source.mu.Unlock()
	return nil
}

func (h *mysqlEventHandler) String() string {
	return "CDCSourceEventHandler"
}

// rowToMap 행 데이터를 map으로 변환하며 컬럼 타입에 따라 값 표현을 정규화한다.
// go-mysql binlog 디코더는 BINARY/VARBINARY 를 string 으로, BLOB 를 []byte 로 주는 등
// 타입별 표현이 일관되지 않다. 그래서 여기서 결정적으로 맞춘다:
//   - 바이너리 컬럼(TYPE_BINARY: BINARY/VARBINARY/BLOB)  → []byte (비UTF-8 바이트 보존, 싱크가 base64 등)
//   - 그 외 텍스트/JSON 등의 []byte                       → string
//
// (Go string 은 임의 바이트를 손실 없이 담지만, 다운스트림 타입 판별을 위해 바이너리는 []byte 로 통일.)
func rowToMap(columns []schema.TableColumn, row []any) map[string]any {
	data := make(map[string]any)
	for i, col := range columns {
		if i < len(row) {
			val := row[i]
			switch col.Type {
			case schema.TYPE_BINARY:
				if s, ok := val.(string); ok {
					val = []byte(s)
				}
			default:
				if b, ok := val.([]byte); ok {
					val = string(b)
				}
			}
			data[col.Name] = val
		}
	}
	return data
}

// getPrimaryKeyValues PK 값들 추출
func getPrimaryKeyValues(table *schema.Table, row []any) []any {
	var pkValues []any
	for _, idx := range table.PKColumns {
		if idx < len(row) {
			pkValues = append(pkValues, row[idx])
		}
	}
	return pkValues
}

// getPrimaryKeyColumns PK 컬럼명 추출 (delete 반영 시 WHERE 절 구성에 필요)
func getPrimaryKeyColumns(table *schema.Table) []string {
	names := make([]string, 0, len(table.PKColumns))
	for _, idx := range table.PKColumns {
		if idx < len(table.Columns) {
			names = append(names, table.Columns[idx].Name)
		}
	}
	return names
}

// CDCConfig CDC 설정 구조체 (JSON 직렬화용)
type CDCConfig struct {
	Driver      string   `json:"driver"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Database    string   `json:"database"`
	Tables      []string `json:"tables"`
	ServerID    uint32   `json:"server_id"`
	SlotName    string   `json:"slot_name"`
	Publication string   `json:"publication"`
}

// ToJSON 설정을 JSON으로 직렬화
func (c *CDCConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}
