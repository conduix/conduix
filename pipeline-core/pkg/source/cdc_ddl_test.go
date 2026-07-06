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
