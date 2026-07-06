//go:build cdcintegration

// CDC → SQL 싱크 end-to-end 통합 테스트(실 MySQL/PostgreSQL 대상).
// source 와 output 을 함께 import 하므로 두 패키지에 의존성이 없는 별도 패키지에 둔다.
//
// 실행:
//
//	go test -tags cdcintegration ./pkg/cdce2e/ -v
package cdce2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/output"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func mysqlDSN() string {
	if v := os.Getenv("CDC_MYSQL_DSN"); v != "" {
		return v
	}
	return "root:rootpw@tcp(127.0.0.1:13306)/cdcdb"
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func mysqlBinlogPos(t *testing.T, db *sql.DB) string {
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
					if b, ok := vals[i].([]byte); ok {
						file = string(b)
					} else {
						file = fmt.Sprint(vals[i])
					}
				case "Position":
					var v any = vals[i]
					if b, ok := v.([]byte); ok {
						fmt.Sscanf(string(b), "%d", &pos)
					} else {
						fmt.Sscanf(fmt.Sprint(v), "%d", &pos)
					}
				}
			}
			rows.Close()
			return fmt.Sprintf("%s:%d", file, pos)
		}
		rows.Close()
	}
	t.Fatal("could not read binlog position")
	return ""
}

// 실 MySQL CDC → SQL 싱크: insert/update 반영 + delete 시 타깃 행 삭제(roadmap #1 실경로).
func TestIntegration_MySQLCDC_ToSQLSink_DeleteReflected(t *testing.T) {
	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping mysql: %v", err)
	}

	mustExec(t, db, "DROP TABLE IF EXISTS src_orders")
	mustExec(t, db, "DROP TABLE IF EXISTS sink_orders")
	mustExec(t, db, "CREATE TABLE src_orders (id INT PRIMARY KEY, customer VARCHAR(100), amount INT)")
	mustExec(t, db, "CREATE TABLE sink_orders (id INT PRIMARY KEY, customer VARCHAR(100), amount INT)")

	startPos := mysqlBinlogPos(t, db)

	src, err := source.NewCDCSource(config.SourceV2{
		Driver:        "mysql",
		Host:          "127.0.0.1",
		Port:          13306,
		Username:      "root",
		Password:      "rootpw",
		Database:      "cdcdb",
		Tables:        []string{"cdcdb\\.src_orders"},
		ServerID:      1002,
		StartPosition: startPos,
	})
	if err != nil {
		t.Fatalf("new cdc source: %v", err)
	}
	if err := src.Open(context.Background()); err != nil {
		t.Fatalf("open cdc: %v", err)
	}
	defer src.Close()

	sink, err := output.NewSQLOutput(config.OutputConfig{
		Driver:          "mysql",
		DSN:             mysqlDSN(),
		Table:           "sink_orders",
		Columns:         []string{"id", "customer", "amount"},
		OnConflict:      "update",
		ConflictColumns: []string{"id"},
		BatchSize:       1, // 즉시 flush (테스트 결정성)
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
				if r.Data["_cdc_type"] == "ddl" {
					continue
				}
				if err := sink.Write(ctx, r); err != nil {
					t.Logf("sink write error: %v", err)
				}
			case e := <-errs:
				if e != nil {
					t.Logf("cdc error: %v", e)
				}
			}
		}
	}()

	time.Sleep(2 * time.Second)

	mustExec(t, db, "INSERT INTO src_orders (id, customer, amount) VALUES (1, 'alice', 100)")
	mustExec(t, db, "INSERT INTO src_orders (id, customer, amount) VALUES (2, 'bob', 200)")
	mustExec(t, db, "UPDATE src_orders SET amount = 150 WHERE id = 1")
	mustExec(t, db, "DELETE FROM src_orders WHERE id = 2")

	deadline := time.Now().Add(15 * time.Second)
	for {
		var cnt, amount1, has2 int
		_ = db.QueryRow("SELECT COUNT(*) FROM sink_orders").Scan(&cnt)
		_ = db.QueryRow("SELECT COALESCE((SELECT amount FROM sink_orders WHERE id=1), -1)").Scan(&amount1)
		_ = db.QueryRow("SELECT COUNT(*) FROM sink_orders WHERE id=2").Scan(&has2)

		if cnt == 1 && amount1 == 150 && has2 == 0 {
			return // 수렴: insert+update 반영, delete 로 id=2 삭제
		}
		if time.Now().After(deadline) {
			t.Fatalf("sink did not converge: count=%d amount(id=1)=%d has(id=2)=%d (want 1/150/0)", cnt, amount1, has2)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
