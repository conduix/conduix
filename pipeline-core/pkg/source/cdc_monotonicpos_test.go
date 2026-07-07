package source

import (
	"testing"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/jackc/pglogrepl"
)

// mysql: (파일, pos) 쌍이 전체 단조여야 한다(파일 rotation 시 pos 리셋돼도 파일 시퀀스가 커서 앞섬).
func TestMonotonicPos_MySQLAcrossRotation(t *testing.T) {
	// 같은 파일 내 pos 증가 → 값 증가.
	a := monotonicPos("mysql", mysql.Position{Name: "mysql-bin.000007", Pos: 100}, 0)
	b := monotonicPos("mysql", mysql.Position{Name: "mysql-bin.000007", Pos: 500}, 0)
	if b <= a {
		t.Errorf("같은 파일 pos 증가인데 단조 아님: a=%d b=%d", a, b)
	}

	// 파일 rotation: 새 파일 pos 작아도(리셋) 이전 파일 마지막보다 커야 한다.
	last := monotonicPos("mysql", mysql.Position{Name: "mysql-bin.000007", Pos: 99999999}, 0)
	next := monotonicPos("mysql", mysql.Position{Name: "mysql-bin.000008", Pos: 4}, 0)
	if next <= last {
		t.Errorf("파일 rotation 후 단조 깨짐: last(f7,big)=%d next(f8,4)=%d", last, next)
	}
}

// postgres: LSN 이 그대로 단조 int64 로.
func TestMonotonicPos_PostgresLSN(t *testing.T) {
	lo := monotonicPos("postgres", mysql.Position{}, pglogrepl.LSN(1000))
	hi := monotonicPos("postgres", mysql.Position{}, pglogrepl.LSN(2000))
	if lo != 1000 || hi != 2000 {
		t.Errorf("LSN passthrough 실패: lo=%d hi=%d", lo, hi)
	}
	if hi <= lo {
		t.Errorf("LSN 단조 아님: lo=%d hi=%d", lo, hi)
	}
}
