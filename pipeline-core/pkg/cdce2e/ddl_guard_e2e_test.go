//go:build cdcintegration

// R3: DDL 방어 — CDC 대상 테이블 ALTER 시 파이프라인이 "schema_changed" 사유로 정지하는지.
package cdce2e

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/shared/types"
)

func mysqlBinlogPos2(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query("SHOW MASTER STATUS")
	if err != nil {
		t.Fatalf("show master status: %v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if !rows.Next() {
		t.Fatal("no master status")
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatalf("scan: %v", err)
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
			if b, ok := vals[i].([]byte); ok {
				fmt.Sscanf(string(b), "%d", &pos)
			} else {
				fmt.Sscanf(fmt.Sprint(vals[i]), "%d", &pos)
			}
		}
	}
	return fmt.Sprintf("%s:%d", file, pos)
}

func TestIntegration_DDLGuard_StopsOnAlter(t *testing.T) {
	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil || db.Ping() != nil {
		t.Fatalf("mysql: %v", err)
	}
	defer db.Close()

	mustExec(t, db, "DROP TABLE IF EXISTS ddl_orders")
	mustExec(t, db, "CREATE TABLE ddl_orders (id INT PRIMARY KEY, amount INT)")
	startPos := mysqlBinlogPos2(t, db)

	pg := &types.PipelineGroup{
		ID:            "ddl-guard",
		Name:          "ddl-guard",
		ExecutionMode: types.ExecutionModeSequential,
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "p1",
				Name: "cdc",
				Input: types.WorkflowInput{
					Type: "cdc",
					Name: "src",
					Config: map[string]any{
						"driver":         "mysql",
						"host":           "127.0.0.1",
						"port":           float64(13306),
						"username":       "root",
						"password":       "rootpw",
						"database":       "cdcdb",
						"tables":         []any{"cdcdb\\.ddl_orders"},
						"server_id":      float64(1010),
						"start_position": startPos,
						// on_ddl 기본(stop)
					},
				},
				Outputs: []types.Output{{Name: "sink", Type: "stub", Config: map[string]any{}}},
			},
		},
	}

	e := executor.NewGroupExecutor(pg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := e.Start(ctx, "test"); err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(3 * time.Second) // canal 스트리밍 시작 대기

	// 정상 변경 몇 건 → 그 다음 ALTER(DDL).
	mustExec(t, db, "INSERT INTO ddl_orders (id, amount) VALUES (1, 100)")
	mustExec(t, db, "UPDATE ddl_orders SET amount = 200 WHERE id = 1")
	time.Sleep(2 * time.Second)
	mustExec(t, db, "ALTER TABLE ddl_orders ADD COLUMN memo VARCHAR(50)")

	// 파이프라인이 schema_changed 로 정지해야 한다.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ex := e.Execution(); ex != nil {
			for _, pr := range ex.PipelineResults {
				if pr.Status == "schema_changed" {
					t.Logf("pipeline stopped: %s", pr.ErrorMessage)
					return // 성공
				}
			}
		}
		if e.Status() != types.PipelineGroupStatusRunning {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 상태 확인(정지 사유).
	ex := e.Execution()
	if ex == nil {
		t.Fatal("no execution")
	}
	for _, pr := range ex.PipelineResults {
		t.Logf("pipeline result status=%s msg=%s", pr.Status, pr.ErrorMessage)
		if pr.Status == "schema_changed" {
			return
		}
	}
	t.Fatalf("DDL 감지 후 schema_changed 정지 안 됨 (group status=%s)", e.Status())
}
