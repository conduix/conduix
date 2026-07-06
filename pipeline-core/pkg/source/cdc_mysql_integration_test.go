//go:build cdcintegration

// 실 MySQL(binlog) 대상 CDC 통합 테스트. 일반 유닛 테스트와 분리(빌드 태그).
// 실행: CDC_MYSQL_DSN 로 접속 정보 제공.
//
//	go test -tags cdcintegration ./pkg/source/ -run TestIntegration_MySQLCDC -v
package source

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func mysqlDSN() string {
	if v := os.Getenv("CDC_MYSQL_DSN"); v != "" {
		return v
	}
	return "root:rootpw@tcp(127.0.0.1:13306)/cdcdb"
}

func mysqlCfg() config.SourceV2 {
	return config.SourceV2{
		Driver:   "mysql",
		Host:     envOrDefault("CDC_MYSQL_HOST", "127.0.0.1"),
		Port:     atoiOrDefault("CDC_MYSQL_PORT", 13306),
		Username: envOrDefault("CDC_MYSQL_USER", "root"),
		Password: envOrDefault("CDC_MYSQL_PASS", "rootpw"),
		Database: "cdcdb",
		Tables:   []string{"cdcdb\\.orders"},
		ServerID: 1001,
	}
}

func envOrDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func atoiOrDefault(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return d
}

// collectEvents 는 records 채널에서 CDC 레코드를 want 개수만큼(또는 타임아웃) 모은다.
// DDL(_cdc_type=ddl)은 스킵한다(테이블 셋업 잔여).
func collectRecords(t *testing.T, records <-chan Record, want int, timeout time.Duration) []Record {
	t.Helper()
	var got []Record
	deadline := time.After(timeout)
	for len(got) < want {
		select {
		case r := <-records:
			if r.Data["_cdc_type"] == "ddl" {
				continue
			}
			got = append(got, r)
		case <-deadline:
			return got
		}
	}
	return got
}

// insert/update/delete 를 실 MySQL 에 수행하고, CDCSource 가 이벤트 타입별로 정확히 캡처하는지 검증.
func TestIntegration_MySQLCDC_EventTypes(t *testing.T) {
	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping mysql (컨테이너 기동 필요): %v", err)
	}

	// 깨끗한 테이블.
	mustExec(t, db, "DROP TABLE IF EXISTS orders")
	mustExec(t, db, `CREATE TABLE orders (
		id INT PRIMARY KEY,
		customer VARCHAR(100),
		amount INT,
		blob_col VARBINARY(16)
	)`)

	// binlog 시작 위치를 write 전에 확보한다(canal 자체 GetMasterPos 는 시작 타이밍
	// 레이스가 있어 이벤트를 놓칠 수 있다 → start_position 으로 명시. roadmap #4 실검증).
	startPos := currentBinlogPos(t, db)

	cfg := mysqlCfg()
	cfg.StartPosition = startPos
	src, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("new cdc source: %v", err)
	}
	if err := src.Open(context.Background()); err != nil {
		t.Fatalf("open cdc: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records, errs := src.Read(ctx)

	// canal 이 지정 position 부터 스트리밍을 시작할 시간을 준다.
	time.Sleep(2 * time.Second)

	// 이벤트 타입별 write: insert, update, delete + 바이너리.
	binary := []byte{0x00, 0xff, 0x10, 0x80}
	mustExec(t, db, "INSERT INTO orders (id, customer, amount, blob_col) VALUES (1, 'alice', 100, ?)", binary)
	mustExec(t, db, "UPDATE orders SET amount = 250 WHERE id = 1")
	mustExec(t, db, "DELETE FROM orders WHERE id = 1")

	got := collectRecords(t, records, 3, 15*time.Second)
	select {
	case e := <-errs:
		if e != nil {
			t.Fatalf("cdc error: %v", e)
		}
	default:
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 events (insert/update/delete), got %d: %+v", len(got), got)
	}

	// 1) INSERT
	ins := got[0]
	if ins.Data["_cdc_type"] != string(CDCEventInsert) {
		t.Errorf("event[0] type = %v, want insert", ins.Data["_cdc_type"])
	}
	if fmt.Sprint(ins.Data["customer"]) != "alice" {
		t.Errorf("insert customer = %v, want alice", ins.Data["customer"])
	}
	// 바이너리 컬럼은 []byte 로 보존되어야 한다(roadmap #3).
	if b, ok := ins.Data["blob_col"].([]byte); !ok {
		t.Errorf("blob_col type = %T, want []byte (binary preserved)", ins.Data["blob_col"])
	} else if len(b) != len(binary) || b[1] != 0xff {
		t.Errorf("blob_col = %#v, want %#v (binary corrupted)", b, binary)
	}

	// 2) UPDATE — after=250, old=100
	upd := got[1]
	if upd.Data["_cdc_type"] != string(CDCEventUpdate) {
		t.Errorf("event[1] type = %v, want update", upd.Data["_cdc_type"])
	}
	if fmt.Sprint(upd.Data["amount"]) != "250" {
		t.Errorf("update amount(after) = %v, want 250", upd.Data["amount"])
	}
	if old, ok := upd.Data["_old_data"].(map[string]any); ok {
		if fmt.Sprint(old["amount"]) != "100" {
			t.Errorf("update _old_data.amount = %v, want 100", old["amount"])
		}
	} else {
		t.Errorf("update missing _old_data: %+v", upd.Data)
	}

	// 3) DELETE — PK 정보 필수(싱크가 DELETE WHERE 구성).
	del := got[2]
	if del.Data["_cdc_type"] != string(CDCEventDelete) {
		t.Errorf("event[2] type = %v, want delete", del.Data["_cdc_type"])
	}
	pkCols, _ := del.Data["_primary_key_columns"].([]string)
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Errorf("delete _primary_key_columns = %v, want [id]", del.Data["_primary_key_columns"])
	}
	if del.Data["_primary_key"] == nil {
		t.Errorf("delete missing _primary_key: %+v", del.Data)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// currentBinlogPos 는 "file:pos" 형식으로 현재 binlog 위치를 반환한다.
// MySQL 8.0.22+ 는 SHOW MASTER STATUS, 8.4+ 는 SHOW BINARY LOG STATUS.
func currentBinlogPos(t *testing.T, db *sql.DB) string {
	t.Helper()
	for _, q := range []string{"SHOW BINARY LOG STATUS", "SHOW MASTER STATUS"} {
		rows, err := db.Query(q)
		if err != nil {
			continue
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatalf("scan binlog status: %v", err)
			}
			var file string
			var pos int64
			for i, c := range cols {
				switch c {
				case "File":
					file = fmt.Sprint(derefBytes(vals[i]))
				case "Position":
					fmt.Sscanf(fmt.Sprint(derefBytes(vals[i])), "%d", &pos)
				}
			}
			rows.Close()
			return fmt.Sprintf("%s:%d", file, pos)
		}
		rows.Close()
	}
	t.Fatal("could not read binlog position (SHOW MASTER/BINARY LOG STATUS)")
	return ""
}

func derefBytes(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
