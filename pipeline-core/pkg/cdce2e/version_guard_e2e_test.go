//go:build cdcintegration

// R1: 초기적재+CDC 동시 실행 시 순서 무관 수렴 검증(sink version guard).
// snapshot(old, 낮은 position)이 CDC(new, 높은 position)를 뒤늦게 덮지 않아야 한다.
package cdce2e

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/output"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// writeRec 는 sink 에 레코드 1건 flush 한다(version guard 동작 확인용).
func writeRec(t *testing.T, sink *output.SQLOutput, data map[string]any) {
	t.Helper()
	if err := sink.Write(context.Background(), source.Record{Data: data}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestIntegration_VersionGuard_MySQL(t *testing.T) {
	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil || db.Ping() != nil {
		t.Fatalf("mysql: %v", err)
	}
	defer db.Close()
	mustExec(t, db, "DROP TABLE IF EXISTS vg_orders")
	mustExec(t, db, "CREATE TABLE vg_orders (id INT PRIMARY KEY, amount INT, _pos BIGINT)")

	sink, err := output.NewSQLOutput(config.OutputConfig{
		Driver: "mysql", DSN: mysqlDSN(), Table: "vg_orders",
		Columns:    []string{"id", "amount", "_pos"},
		OnConflict: "update", ConflictColumns: []string{"id"},
		VersionColumn: "_pos", BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	if err := sink.Open(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sink.Close()

	assertConverges(t, db, sink, "vg_orders")
}

func TestIntegration_VersionGuard_Postgres(t *testing.T) {
	db, err := sql.Open("postgres", pgDSN())
	if err != nil || db.Ping() != nil {
		t.Fatalf("pg: %v", err)
	}
	defer db.Close()
	mustExec(t, db, "DROP TABLE IF EXISTS vg_orders")
	mustExec(t, db, "CREATE TABLE vg_orders (id INT PRIMARY KEY, amount INT, _pos BIGINT)")

	sink, err := output.NewSQLOutput(config.OutputConfig{
		Driver: "postgres", DSN: pgDSN(), Table: "vg_orders",
		Columns:    []string{"id", "amount", "_pos"},
		OnConflict: "update", ConflictColumns: []string{"id"},
		VersionColumn: "_pos", BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	if err := sink.Open(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sink.Close()

	assertConverges(t, db, sink, "vg_orders")
}

// assertConverges: 도착 순서를 뒤섞어도 항상 가장 높은 position 값으로 수렴하는지.
func assertConverges(t *testing.T, db *sql.DB, sink *output.SQLOutput, table string) {
	t.Helper()

	// 시나리오 1: CDC 최신(pos=100, amount=999) 이 먼저 도착 → snapshot old(pos=10, amount=100) 가 뒤늦게.
	// 결과: amount=999 유지(snapshot 이 덮으면 안 됨).
	writeRec(t, sink, map[string]any{"id": 1, "amount": 999, "_pos": 100})
	writeRec(t, sink, map[string]any{"id": 1, "amount": 100, "_pos": 10}) // late snapshot (낮은 pos)

	var amt int
	if err := db.QueryRow("SELECT amount FROM " + table + " WHERE id=1").Scan(&amt); err != nil {
		t.Fatalf("query id=1: %v", err)
	}
	if amt != 999 {
		t.Errorf("id=1 amount=%d, want 999 (late snapshot 이 CDC 최신값을 덮음 — 버전가드 실패)", amt)
	}

	// 시나리오 2: snapshot old(pos=10) 먼저 → CDC 최신(pos=100, amount=555) 뒤에. 결과: amount=555.
	writeRec(t, sink, map[string]any{"id": 2, "amount": 200, "_pos": 10})  // snapshot
	writeRec(t, sink, map[string]any{"id": 2, "amount": 555, "_pos": 100}) // CDC newer

	if err := db.QueryRow("SELECT amount FROM " + table + " WHERE id=2").Scan(&amt); err != nil {
		t.Fatalf("query id=2: %v", err)
	}
	if amt != 555 {
		t.Errorf("id=2 amount=%d, want 555 (CDC 최신값이 반영 안 됨)", amt)
	}

	// 시나리오 3: 같은 pos 재도착(중복 이벤트) — 값 안 바뀜(idempotent).
	writeRec(t, sink, map[string]any{"id": 2, "amount": 555, "_pos": 100})
	if err := db.QueryRow("SELECT amount FROM " + table + " WHERE id=2").Scan(&amt); err != nil {
		t.Fatalf("query id=2 dup: %v", err)
	}
	if amt != 555 {
		t.Errorf("id=2 after dup amount=%d, want 555", amt)
	}
}
