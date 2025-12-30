package source

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// CDCEventType CDC 이벤트 타입
type CDCEventType string

const (
	CDCEventInsert CDCEventType = "insert"
	CDCEventUpdate CDCEventType = "update"
	CDCEventDelete CDCEventType = "delete"
)

// CDCSource CDC(Change Data Capture) 소스
// MySQL binlog 또는 PostgreSQL WAL을 통해 변경 사항 캡처
type CDCSource struct {
	driver   string // mysql, postgres
	host     string
	port     uint16
	username string
	password string
	database string
	tables   []string // 감시할 테이블 목록 (빈 경우 전체)
	serverID uint32   // MySQL server ID (binlog용)
	slotName string   // PostgreSQL replication slot name

	canal    *canal.Canal
	mu       sync.RWMutex
	running  bool
	position mysql.Position // MySQL binlog position

	// 이벤트 핸들러
	eventCh chan *CDCEvent
	errorCh chan error
}

// CDCEvent CDC 이벤트
type CDCEvent struct {
	Type       CDCEventType   `json:"type"`
	Database   string         `json:"database"`
	Table      string         `json:"table"`
	Timestamp  time.Time      `json:"timestamp"`
	Data       map[string]any `json:"data"`        // 현재 데이터 (INSERT, UPDATE)
	OldData    map[string]any `json:"old_data"`    // 이전 데이터 (UPDATE, DELETE)
	PrimaryKey []any          `json:"primary_key"` // PK 값들
}

// NewCDCSource CDC 소스 생성
func NewCDCSource(cfg config.SourceV2) (*CDCSource, error) {
	port := uint16(3306)
	if cfg.Port > 0 {
		port = uint16(cfg.Port)
	}

	serverID := uint32(101)
	if cfg.ServerID > 0 {
		serverID = cfg.ServerID
	}

	return &CDCSource{
		driver:   cfg.Driver,
		host:     cfg.Host,
		port:     port,
		username: cfg.Username,
		password: cfg.Password,
		database: cfg.Database,
		tables:   cfg.Tables,
		serverID: serverID,
		slotName: cfg.SlotName,
		eventCh:  make(chan *CDCEvent, 1000),
		errorCh:  make(chan error, 10),
	}, nil
}

func (s *CDCSource) Name() string {
	return "cdc"
}

func (s *CDCSource) Open(ctx context.Context) error {
	switch s.driver {
	case "mysql":
		return s.openMySQL()
	case "postgres":
		return s.openPostgreSQL()
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

func (s *CDCSource) openPostgreSQL() error {
	// PostgreSQL CDC는 pglogrepl 라이브러리 사용
	// 여기서는 인터페이스만 정의하고 실제 구현은 PostgreSQL specific
	return fmt.Errorf("PostgreSQL CDC not implemented yet - use pglogrepl library")
}

func (s *CDCSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	// MySQL binlog 시작
	go func() {
		s.mu.RLock()
		c := s.canal
		pos := s.position
		s.mu.RUnlock()

		if c == nil {
			errs <- fmt.Errorf("canal not initialized")
			close(records)
			close(errs)
			return
		}

		// Position이 설정되어 있으면 해당 위치부터, 아니면 현재 위치부터
		if pos.Name == "" {
			// 현재 binlog position 가져오기
			currentPos, err := c.GetMasterPos()
			if err != nil {
				errs <- fmt.Errorf("failed to get master position: %w", err)
				close(records)
				close(errs)
				return
			}
			pos = currentPos
		}

		go func() {
			if err := c.RunFrom(pos); err != nil {
				select {
				case errs <- fmt.Errorf("canal run error: %w", err):
				default:
				}
			}
		}()
	}()

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

func (s *CDCSource) convertEventToRecord(event *CDCEvent) Record {
	data := map[string]any{
		"_cdc_type":  string(event.Type),
		"_database":  event.Database,
		"_table":     event.Table,
		"_timestamp": event.Timestamp,
	}

	// 현재 데이터 병합
	for k, v := range event.Data {
		data[k] = v
	}

	// 이전 데이터가 있으면 _old_ 접두사로 추가
	if event.OldData != nil {
		oldData := make(map[string]any)
		for k, v := range event.OldData {
			oldData[k] = v
		}
		data["_old_data"] = oldData
	}

	// PK 정보
	if len(event.PrimaryKey) > 0 {
		data["_primary_key"] = event.PrimaryKey
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

func (s *CDCSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false

	if s.canal != nil {
		s.canal.Close()
		s.canal = nil
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
				Type:       eventType,
				Database:   e.Table.Schema,
				Table:      e.Table.Name,
				Timestamp:  time.Now(),
				Data:       rowToMap(columns, newRow),
				OldData:    rowToMap(columns, oldRow),
				PrimaryKey: getPrimaryKeyValues(e.Table, newRow),
			}

			select {
			case h.source.eventCh <- event:
			default:
				// 채널이 가득 차면 스킵
			}
		}
	} else {
		for _, row := range e.Rows {
			event := &CDCEvent{
				Type:       eventType,
				Database:   e.Table.Schema,
				Table:      e.Table.Name,
				Timestamp:  time.Now(),
				PrimaryKey: getPrimaryKeyValues(e.Table, row),
			}

			if eventType == CDCEventDelete {
				event.OldData = rowToMap(columns, row)
			} else {
				event.Data = rowToMap(columns, row)
			}

			select {
			case h.source.eventCh <- event:
			default:
			}
		}
	}

	return nil
}

func (h *mysqlEventHandler) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	h.source.mu.Lock()
	h.source.position = pos
	h.source.mu.Unlock()
	return nil
}

func (h *mysqlEventHandler) String() string {
	return "CDCSourceEventHandler"
}

// rowToMap 행 데이터를 map으로 변환
func rowToMap(columns []schema.TableColumn, row []any) map[string]any {
	data := make(map[string]any)
	for i, col := range columns {
		if i < len(row) {
			val := row[i]
			// byte slice를 string으로 변환
			if b, ok := val.([]byte); ok {
				val = string(b)
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

// CDCConfig CDC 설정 구조체 (JSON 직렬화용)
type CDCConfig struct {
	Driver   string   `json:"driver"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	Database string   `json:"database"`
	Tables   []string `json:"tables"`
	ServerID uint32   `json:"server_id"`
	SlotName string   `json:"slot_name"`
}

// ToJSON 설정을 JSON으로 직렬화
func (c *CDCConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}
