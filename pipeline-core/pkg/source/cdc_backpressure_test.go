package source

import (
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/schema"
)

// eventCh 가 가득 차도 OnRow 가 이벤트를 drop 하지 않고 blocking(backpressure)하며,
// Close 시 그 blocking 에서 빠져나오는지 검증한다.
// (이전 구현은 채널이 가득 차면 default 로 이벤트를 조용히 버려 유실됐다.)
func TestCDC_OnRow_BackpressureNoDrop(t *testing.T) {
	s := &CDCSource{
		running: true,
		eventCh: make(chan *CDCEvent, 1), // 용량 1로 즉시 포화 유도
		stopCh:  make(chan struct{}),
	}
	h := &mysqlEventHandler{source: s}

	tbl := &schema.Table{
		Schema:  "db",
		Name:    "t",
		Columns: []schema.TableColumn{{Name: "id"}},
	}
	mkEvent := func() *canal.RowsEvent {
		return &canal.RowsEvent{Action: canal.InsertAction, Table: tbl, Rows: [][]any{{int64(1)}}}
	}

	// 첫 이벤트는 버퍼(용량1)에 들어가 OnRow 즉시 반환.
	done1 := make(chan struct{})
	go func() { _ = h.OnRow(mkEvent()); close(done1) }()
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first OnRow should return (buffer has room)")
	}

	// 두 번째 이벤트는 버퍼가 꽉 차 blocking 되어야 한다(drop 아님).
	done2 := make(chan struct{})
	go func() { _ = h.OnRow(mkEvent()); close(done2) }()
	select {
	case <-done2:
		t.Fatal("second OnRow returned while channel full — event was dropped (regression)")
	case <-time.After(300 * time.Millisecond):
		// 기대: 아직 blocking 중 → backpressure 정상
	}

	// Close 하면 blocking 이 풀려 OnRow 가 반환되어야 한다(hang 방지).
	_ = s.Close()
	select {
	case <-done2:
		// 정상: stopCh 로 탈출
	case <-time.After(2 * time.Second):
		t.Fatal("OnRow did not unblock after Close — would hang on shutdown")
	}
}
