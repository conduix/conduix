package source

import (
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
)

// OnDDL 은 스키마 변경을 조용히 무시하지 않고 ddl 이벤트로 파이프라인에 흘려야 한다.
func TestCDC_OnDDL_EmitsEvent(t *testing.T) {
	s := &CDCSource{
		running:  true,
		eventCh:  make(chan *CDCEvent, 4),
		stopCh:   make(chan struct{}),
		position: mysql.Position{Name: "bin.000009", Pos: 1500},
	}
	h := &mysqlEventHandler{source: s}

	qe := &replication.QueryEvent{
		Schema: []byte("shop"),
		Query:  []byte("ALTER TABLE orders ADD COLUMN memo VARCHAR(255)"),
	}
	if err := h.OnDDL(nil, mysql.Position{Name: "bin.000009", Pos: 1600}, qe); err != nil {
		t.Fatalf("OnDDL: %v", err)
	}

	select {
	case ev := <-s.eventCh:
		if ev.Type != CDCEventDDL {
			t.Errorf("event type = %q, want ddl", ev.Type)
		}
		if ev.Database != "shop" {
			t.Errorf("database = %q, want shop", ev.Database)
		}
		if ev.Data["ddl"] != "ALTER TABLE orders ADD COLUMN memo VARCHAR(255)" {
			t.Errorf("ddl payload = %v", ev.Data["ddl"])
		}
		// 커밋 기준 position 도 실려야 함
		if ev.pos.Name != "bin.000009" {
			t.Errorf("event pos = %v, want bin.000009", ev.pos)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnDDL did not emit a ddl event (schema change silently ignored)")
	}
}

// running=false 면 DDL 이벤트를 내지 않는다.
func TestCDC_OnDDL_SkipsWhenNotRunning(t *testing.T) {
	s := &CDCSource{running: false, eventCh: make(chan *CDCEvent, 1), stopCh: make(chan struct{})}
	h := &mysqlEventHandler{source: s}
	_ = h.OnDDL(nil, mysql.Position{}, &replication.QueryEvent{Query: []byte("DROP TABLE t")})
	select {
	case <-s.eventCh:
		t.Fatal("should not emit when not running")
	default:
	}
}

// 구독 범위 밖의 DDL 은 OnDDL 이 이벤트를 만들지 않아야 한다.
//
// 실측: restrooms 만 구독하는 realtime 파이프라인이 같은 DB 의 child_meal_stores 를
// CREATE 하자 DDL 방어가 발동해 schema_changed 로 정지했다. canal 은 스키마 캐시 갱신을
// 위해 구독 범위와 무관하게 모든 DDL 을 OnDDL 로 보내기 때문이다.
func TestCDC_OnDDL_FiltersOutOfScopeTable(t *testing.T) {
	s := &CDCSource{
		running:  true,
		database: "targetdb",
		tables:   []string{`targetdb\.restrooms`},
		eventCh:  make(chan *CDCEvent, 4),
		stopCh:   make(chan struct{}),
	}
	h := &mysqlEventHandler{source: s}

	qe := &replication.QueryEvent{
		Schema: []byte("targetdb"),
		Query:  []byte("CREATE TABLE IF NOT EXISTS targetdb.child_meal_stores (id INT)"),
	}
	if err := h.OnDDL(nil, mysql.Position{}, qe); err != nil {
		t.Fatalf("OnDDL: %v", err)
	}

	select {
	case ev := <-s.eventCh:
		t.Fatalf("구독하지 않는 %s.%s DDL 이 흘러나왔다 — 파이프라인이 오정지한다",
			ev.Database, ev.Table)
	default:
	}
}

// 구독 중인 테이블의 DDL 은 계속 흘러야 한다 — 필터가 방어를 뚫으면 안 된다.
func TestCDC_OnDDL_KeepsInScopeTable(t *testing.T) {
	s := &CDCSource{
		running:  true,
		database: "targetdb",
		tables:   []string{`targetdb\.restrooms`},
		eventCh:  make(chan *CDCEvent, 4),
		stopCh:   make(chan struct{}),
	}
	h := &mysqlEventHandler{source: s}

	qe := &replication.QueryEvent{
		Schema: []byte("targetdb"),
		Query:  []byte("ALTER TABLE targetdb.restrooms ADD COLUMN memo VARCHAR(64)"),
	}
	if err := h.OnDDL(nil, mysql.Position{}, qe); err != nil {
		t.Fatalf("OnDDL: %v", err)
	}

	select {
	case ev := <-s.eventCh:
		if ev.Table != "restrooms" {
			t.Errorf("event table = %q, want restrooms", ev.Table)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("구독 테이블의 DDL 이 차단됐다 — 스키마 변경 방어가 무력화된다")
	}
}

// 알아볼 수 없는 DDL 은 통과시킨다 — 파싱 실패를 이유로 방어를 뚫지 않는다.
func TestCDC_OnDDL_PassesUnparseableDDL(t *testing.T) {
	s := &CDCSource{
		running:  true,
		database: "targetdb",
		tables:   []string{`targetdb\.restrooms`},
		eventCh:  make(chan *CDCEvent, 4),
		stopCh:   make(chan struct{}),
	}
	h := &mysqlEventHandler{source: s}

	qe := &replication.QueryEvent{
		Schema: []byte("targetdb"),
		Query:  []byte("CREATE DATABASE somethingelse"),
	}
	if err := h.OnDDL(nil, mysql.Position{}, qe); err != nil {
		t.Fatalf("OnDDL: %v", err)
	}

	select {
	case <-s.eventCh:
	case <-time.After(2 * time.Second):
		t.Fatal("파싱 불가 DDL 이 차단됐다 — 보수적으로 통과시켜야 한다")
	}
}
