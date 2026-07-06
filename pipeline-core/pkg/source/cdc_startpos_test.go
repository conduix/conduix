package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// start_position 지정 시 committedPos/position 이 그 지점으로 설정된다(bulk↔CDC 경계 맞춤).
func TestNewCDCSource_StartPosition(t *testing.T) {
	s, err := NewCDCSource(config.SourceV2{Driver: "mysql", StartPosition: "bin.000012:4567"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.position.Name != "bin.000012" || s.position.Pos != 4567 {
		t.Errorf("position = %v, want bin.000012:4567", s.position)
	}
	if s.committedPos.Name != "bin.000012" || s.committedPos.Pos != 4567 {
		t.Errorf("committedPos = %v, want bin.000012:4567", s.committedPos)
	}
}

// start_gtid 지정 시 committedGTID 로 설정된다(Read 가 GTID 우선 시작).
func TestNewCDCSource_StartGTID(t *testing.T) {
	gtid := "3E11FA47-71CA-11E1-9E33-C80AA9429562:1-50"
	s, err := NewCDCSource(config.SourceV2{Driver: "mysql", StartGTID: gtid})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.committedGTID != gtid {
		t.Errorf("committedGTID = %q, want %q", s.committedGTID, gtid)
	}
}

// 잘못된 start_position/start_gtid 는 생성 시 거부한다(실행 후 미동작 방지).
func TestNewCDCSource_InvalidStart(t *testing.T) {
	if _, err := NewCDCSource(config.SourceV2{Driver: "mysql", StartPosition: "nocolon"}); err == nil {
		t.Error("expected error for invalid start_position")
	}
	if _, err := NewCDCSource(config.SourceV2{Driver: "mysql", StartGTID: "not-a-gtid"}); err == nil {
		t.Error("expected error for invalid start_gtid")
	}
}
