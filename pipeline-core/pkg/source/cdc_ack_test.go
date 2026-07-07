package source

import (
	"testing"

	"github.com/go-mysql-org/go-mysql/mysql"
)

// CDC ack: Ack 된 최대 seq 의 위치까지만 committedPos 가 전진한다(채널 전송이 아니라 ack 기준).
func TestCDCAck_AdvancesCommittedOnAckOnly(t *testing.T) {
	s := &CDCSource{
		driver:   "mysql",
		database: "shop",
		pendingP: make(map[uint64]cdcAckOffset),
	}

	// 3개 이벤트가 채널로 나갔다고 가정(seq 1,2,3). 각기 다른 binlog 위치.
	s.pendingP[1] = cdcAckOffset{pos: mysql.Position{Name: "bin.001", Pos: 100}}
	s.pendingP[2] = cdcAckOffset{pos: mysql.Position{Name: "bin.001", Pos: 200}}
	s.pendingP[3] = cdcAckOffset{pos: mysql.Position{Name: "bin.001", Pos: 300}}
	s.ackSeq = 3

	// ack 전: committed 는 비어 있어야(전송만으로 커밋 안 됨).
	if s.committedPos.Name != "" {
		t.Fatalf("before ack: committedPos = %v, want empty (전송만으로 커밋되면 안 됨)", s.committedPos)
	}

	// seq 2 까지 ack → committed 는 pos=200. seq 3 은 미ack 라 남는다.
	s.Ack([]RecordOffset{{PartitionKey: "shop", Offset: "1"}, {PartitionKey: "shop", Offset: "2"}})
	if s.committedPos.Pos != 200 {
		t.Errorf("after ack seq<=2: committedPos.Pos = %d, want 200", s.committedPos.Pos)
	}
	s.ackMu.Lock()
	_, has3 := s.pendingP[3]
	_, has1 := s.pendingP[1]
	s.ackMu.Unlock()
	if !has3 {
		t.Error("seq 3 은 미ack 라 pending 에 남아야 함")
	}
	if has1 {
		t.Error("seq 1 은 ack 됐으니 pending 에서 제거돼야 함")
	}

	// seq 3 ack → committed 전진 300.
	s.Ack([]RecordOffset{{PartitionKey: "shop", Offset: "3"}})
	if s.committedPos.Pos != 300 {
		t.Errorf("after ack seq 3: committedPos.Pos = %d, want 300", s.committedPos.Pos)
	}
}

// checkpoint 는 committed(=ack 반영) 기준으로 나온다.
func TestCDCAck_CheckpointReflectsCommitted(t *testing.T) {
	s := &CDCSource{
		driver:   "mysql",
		database: "shop",
		pendingP: make(map[uint64]cdcAckOffset),
	}
	s.pendingP[1] = cdcAckOffset{pos: mysql.Position{Name: "bin.005", Pos: 4567}}
	s.ackSeq = 1

	// ack 전 checkpoint: committed 비어 있으니 position(=시작점, 여기선 empty) 기준.
	s.Ack([]RecordOffset{{PartitionKey: "shop", Offset: "1"}})

	cps := s.GetSourceCheckpoints()
	if len(cps) == 0 {
		t.Fatal("no checkpoints")
	}
	// binlog position checkpoint 에 4567 이 반영돼야 함.
	found := false
	for _, cp := range cps {
		if cp.OffsetType == "string" && cp.OffsetValue == "bin.005:4567" {
			found = true
		}
	}
	if !found {
		t.Errorf("checkpoint 에 ack 된 위치(bin.005:4567) 미반영: %+v", cps)
	}
}
