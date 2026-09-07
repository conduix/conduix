package output

import (
	"strings"
	"testing"
)

// MySQL 은 CREATE TABLE IF NOT EXISTS 가 아무것도 하지 않은 경우에도 binlog 에 DDL 을 남긴다
// (실측 246 bytes). 같은 DB 를 보는 CDC 파이프라인이 그 이벤트를 스키마 변경으로 판정해 멈추므로,
// 테이블이 이미 있으면 DDL 을 아예 보내지 않아야 한다.
// 배치를 돌릴 때마다 realtime 이 죽던 결함(2026-09-08)의 재발 방지.
//
// 실 DB 없이 검증 가능한 부분은 "어떤 조건으로 존재를 확인하는가" — 테이블명/스키마 분리와
// 드라이버별 placeholder 다. 조회 자체가 DML(information_schema)이라 binlog 를 건드리지 않는다.
func TestTableExistsQuery_SplitsSchemaAndTable(t *testing.T) {
	cases := []struct {
		name       string
		driver     string
		table      string
		wantArgs   []any
		wantInQ    []string
		wantNotInQ []string
	}{
		{
			name: "mysql 단순 이름은 현재 DB 로 한정", driver: "mysql", table: "restrooms",
			wantArgs: []any{"restrooms"},
			wantInQ:  []string{"table_name = ?", "DATABASE()"},
		},
		{
			name: "mysql 스키마 한정 이름은 분리해 인자로", driver: "mysql", table: "targetdb.restrooms",
			wantArgs: []any{"restrooms", "targetdb"},
			wantInQ:  []string{"table_name = ?", "table_schema = ?"},
			// 스키마를 안 넘기면 다른 DB 의 동명 테이블을 있다고 오판한다.
			wantNotInQ: []string{"DATABASE()"},
		},
		{
			name: "postgres 는 $N placeholder", driver: "postgres", table: "public.restrooms",
			wantArgs: []any{"restrooms", "public"},
			wantInQ:  []string{"table_name = $1", "table_schema = $2"},
		},
		{
			name: "따옴표는 벗겨서 비교", driver: "mysql", table: "`targetdb`.`restrooms`",
			wantArgs: []any{"restrooms", "targetdb"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &SQLOutput{driver: tc.driver, table: tc.table}
			q, args := o.tableExistsQuery()

			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("args[%d] = %v, want %v", i, args[i], tc.wantArgs[i])
				}
			}
			for _, s := range tc.wantInQ {
				if !strings.Contains(q, s) {
					t.Errorf("query missing %q:\n%s", s, q)
				}
			}
			for _, s := range tc.wantNotInQ {
				if strings.Contains(q, s) {
					t.Errorf("query should not contain %q:\n%s", s, q)
				}
			}
			if !strings.Contains(q, "information_schema.tables") {
				t.Errorf("must query information_schema (DML, no binlog):\n%s", q)
			}
		})
	}
}
