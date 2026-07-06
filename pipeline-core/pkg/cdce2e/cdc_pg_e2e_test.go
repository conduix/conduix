//go:build cdcintegration

// 실 PostgreSQL(논리복제) 대상 CDC 통합 테스트: 이벤트 타입별 캡처 + SQL 싱크 delete 반영.
package cdce2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/output"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func pgDSN() string {
	if v := os.Getenv("CDC_PG_DSN"); v != "" {
		return v
	}
	return "host=127.0.0.1 port=15432 user=postgres password=pgpw dbname=cdcdb sslmode=disable"
}

func pgSourceCfg(table, slot, pub string) config.SourceV2 {
	return config.SourceV2{
		Driver:      "postgres",
		Host:        "127.0.0.1",
		Port:        15432,
		Username:    "postgres",
		Password:    "pgpw",
		Database:    "cdcdb",
		Tables:      []string{table},
		SlotName:    slot,
		Publication: pub,
	}
}

// collectPGRecords 는 pg CDC 레코드를 want 개수만큼(또는 타임아웃) 모은다.
func collectPGRecords(t *testing.T, records <-chan source.Record, want int, timeout time.Duration) []source.Record {
	t.Helper()
	var got []source.Record
	deadline := time.After(timeout)
	for len(got) < want {
		select {
		case r := <-records:
			t := fmt.Sprint(r.Data["_cdc_type"])
			if t == "ddl" || t == "" {
				continue
			}
			got = append(got, r)
		case <-deadline:
			return got
		}
	}
	return got
}

// PostgreSQL insert/update/delete 이벤트 타입별 캡처 검증.
func TestIntegration_PostgresCDC_EventTypes(t *testing.T) {
	db, err := sql.Open("postgres", pgDSN())
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping pg (컨테이너 기동 필요): %v", err)
	}

	// 슬롯/퍼블리케이션 초기화(이전 실행 잔여 정리).
	_, _ = db.Exec("SELECT pg_drop_replication_slot('pg_evt_slot') FROM pg_replication_slots WHERE slot_name='pg_evt_slot'")
	_, _ = db.Exec("DROP PUBLICATION IF EXISTS pg_evt_pub")
	mustExec(t, db, "DROP TABLE IF EXISTS pg_orders")
	// REPLICA IDENTITY DEFAULT(=PK): delete/update old 튜플에 PK 만 실려 키가 [id] 로 온다.
	// (FULL 이면 모든 컬럼이 키로 표시되어 _primary_key_columns 가 전체 컬럼이 된다.)
	mustExec(t, db, "CREATE TABLE pg_orders (id INT PRIMARY KEY, customer TEXT, amount INT)")

	src, err := source.NewCDCSource(pgSourceCfg("public.pg_orders", "pg_evt_slot", "pg_evt_pub"))
	if err != nil {
		t.Fatalf("new pg cdc source: %v", err)
	}
	if err := src.Open(context.Background()); err != nil {
		t.Fatalf("open pg cdc: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records, errs := src.Read(ctx)

	time.Sleep(3 * time.Second) // 슬롯 생성 + 복제 스트림 시작 대기

	mustExec(t, db, "INSERT INTO pg_orders (id, customer, amount) VALUES (1, 'alice', 100)")
	mustExec(t, db, "UPDATE pg_orders SET amount = 250 WHERE id = 1")
	mustExec(t, db, "DELETE FROM pg_orders WHERE id = 1")

	got := collectPGRecords(t, records, 3, 20*time.Second)
	select {
	case e := <-errs:
		if e != nil {
			t.Fatalf("pg cdc error: %v", e)
		}
	default:
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 events (insert/update/delete), got %d: %+v", len(got), got)
	}

	if got[0].Data["_cdc_type"] != string(source.CDCEventInsert) {
		t.Errorf("event[0] type = %v, want insert", got[0].Data["_cdc_type"])
	}
	if fmt.Sprint(got[0].Data["customer"]) != "alice" {
		t.Errorf("insert customer = %v, want alice", got[0].Data["customer"])
	}
	if got[1].Data["_cdc_type"] != string(source.CDCEventUpdate) {
		t.Errorf("event[1] type = %v, want update", got[1].Data["_cdc_type"])
	}
	if fmt.Sprint(got[1].Data["amount"]) != "250" {
		t.Errorf("update amount = %v, want 250", got[1].Data["amount"])
	}
	if got[2].Data["_cdc_type"] != string(source.CDCEventDelete) {
		t.Errorf("event[2] type = %v, want delete", got[2].Data["_cdc_type"])
	}
	pkCols, _ := got[2].Data["_primary_key_columns"].([]string)
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Errorf("delete _primary_key_columns = %v, want [id]", got[2].Data["_primary_key_columns"])
	}
	if got[2].Data["_primary_key"] == nil {
		t.Errorf("delete missing _primary_key: %+v", got[2].Data)
	}
}

// PostgreSQL CDC → SQL 싱크: insert/update 반영 + delete 시 타깃 행 삭제.
func TestIntegration_PostgresCDC_ToSQLSink_DeleteReflected(t *testing.T) {
	db, err := sql.Open("postgres", pgDSN())
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping pg: %v", err)
	}

	_, _ = db.Exec("SELECT pg_drop_replication_slot('pg_sink_slot') FROM pg_replication_slots WHERE slot_name='pg_sink_slot'")
	_, _ = db.Exec("DROP PUBLICATION IF EXISTS pg_sink_pub")
	mustExec(t, db, "DROP TABLE IF EXISTS pg_src")
	mustExec(t, db, "DROP TABLE IF EXISTS pg_sink")
	mustExec(t, db, "CREATE TABLE pg_src (id INT PRIMARY KEY, customer TEXT, amount INT)")
	mustExec(t, db, "CREATE TABLE pg_sink (id INT PRIMARY KEY, customer TEXT, amount INT)")
	mustExec(t, db, "ALTER TABLE pg_src REPLICA IDENTITY FULL")

	src, err := source.NewCDCSource(pgSourceCfg("public.pg_src", "pg_sink_slot", "pg_sink_pub"))
	if err != nil {
		t.Fatalf("new pg cdc source: %v", err)
	}
	if err := src.Open(context.Background()); err != nil {
		t.Fatalf("open pg cdc: %v", err)
	}
	defer src.Close()

	sink, err := output.NewSQLOutput(config.OutputConfig{
		Driver:          "postgres",
		DSN:             pgDSN(),
		Table:           "pg_sink",
		Columns:         []string{"id", "customer", "amount"},
		OnConflict:      "update",
		ConflictColumns: []string{"id"},
		BatchSize:       1,
	})
	if err != nil {
		t.Fatalf("new sql sink: %v", err)
	}
	if err := sink.Open(context.Background()); err != nil {
		t.Fatalf("open sink: %v", err)
	}
	defer sink.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records, errs := src.Read(ctx)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-records:
				if !ok {
					return
				}
				if fmt.Sprint(r.Data["_cdc_type"]) == "ddl" {
					continue
				}
				if err := sink.Write(ctx, r); err != nil {
					t.Logf("sink write error: %v", err)
				}
			case e := <-errs:
				if e != nil {
					t.Logf("pg cdc error: %v", e)
				}
			}
		}
	}()

	time.Sleep(3 * time.Second)

	mustExec(t, db, "INSERT INTO pg_src (id, customer, amount) VALUES (1, 'alice', 100)")
	mustExec(t, db, "INSERT INTO pg_src (id, customer, amount) VALUES (2, 'bob', 200)")
	mustExec(t, db, "UPDATE pg_src SET amount = 150 WHERE id = 1")
	mustExec(t, db, "DELETE FROM pg_src WHERE id = 2")

	deadline := time.Now().Add(20 * time.Second)
	for {
		var cnt, amount1, has2 int
		_ = db.QueryRow("SELECT COUNT(*) FROM pg_sink").Scan(&cnt)
		_ = db.QueryRow("SELECT COALESCE((SELECT amount FROM pg_sink WHERE id=1), -1)").Scan(&amount1)
		_ = db.QueryRow("SELECT COUNT(*) FROM pg_sink WHERE id=2").Scan(&has2)

		if cnt == 1 && amount1 == 150 && has2 == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pg sink did not converge: count=%d amount(id=1)=%d has(id=2)=%d (want 1/150/0)", cnt, amount1, has2)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
