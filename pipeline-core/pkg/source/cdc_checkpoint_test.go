package source

import (
	"testing"

	"github.com/go-mysql-org/go-mysql/mysql"
)

// checkpoint 는 read-ahead(position)가 아니라 committedPos(소비된 위치)를 반환해야 한다.
// 그래야 재시작 시 read-ahead 로 앞서간 미소비 구간을 다시 읽어 유실을 막는다.
func TestCDC_Checkpoint_UsesCommittedNotReadAhead(t *testing.T) {
	s := &CDCSource{
		database:     "db",
		position:     mysql.Position{Name: "bin.000009", Pos: 5000}, // canal read-ahead (앞서감)
		committedPos: mysql.Position{Name: "bin.000009", Pos: 1200}, // 실제 소비된 위치
	}

	cps := s.GetSourceCheckpoints()
	if len(cps) == 0 {
		t.Fatal("expected at least one checkpoint")
	}
	// committedPos(1200)를 저장해야 함, read-ahead(5000)가 아니라.
	if got := cps[0].OffsetValue; got != "bin.000009:1200" {
		t.Errorf("checkpoint offset = %q, want committed bin.000009:1200 (not read-ahead 5000)", got)
	}
}

// committedPos 가 비어있으면(아직 소비 전) 시작 position 으로 폴백한다.
func TestCDC_Checkpoint_FallbackToStartPosition(t *testing.T) {
	s := &CDCSource{
		database: "db",
		position: mysql.Position{Name: "bin.000001", Pos: 4},
	}
	cps := s.GetSourceCheckpoints()
	if cps[0].OffsetValue != "bin.000001:4" {
		t.Errorf("fallback offset = %q, want bin.000001:4", cps[0].OffsetValue)
	}
}

// GTID checkpoint round-trip: 커밋된 GTID 를 저장하고 복원하면 그대로 돌아와야 한다.
func TestCDC_Checkpoint_GTIDRoundTrip(t *testing.T) {
	gtid := "3E11FA47-71CA-11E1-9E33-C80AA9429562:1-100"
	s1 := &CDCSource{
		database:      "db",
		committedPos:  mysql.Position{Name: "bin.000009", Pos: 1200},
		committedGTID: gtid,
	}
	cps := s1.GetSourceCheckpoints()

	var gotGTID string
	for _, cp := range cps {
		if cp.OffsetType == "gtid" {
			gotGTID = cp.OffsetValue
		}
	}
	if gotGTID != gtid {
		t.Fatalf("GTID checkpoint = %q, want %q", gotGTID, gtid)
	}

	// 복원
	s2 := &CDCSource{database: "db"}
	if err := s2.SetSourceCheckpoints(cps); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if s2.committedGTID != gtid {
		t.Errorf("restored committedGTID = %q, want %q", s2.committedGTID, gtid)
	}
	// position 도 함께 복원
	if s2.position.Name != "bin.000009" || s2.position.Pos != 1200 {
		t.Errorf("restored position = %v, want bin.000009:1200", s2.position)
	}
}
