package source

import (
	"testing"

	"github.com/jackc/pglogrepl"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func configPgSource() config.SourceV2 {
	return config.SourceV2{Driver: "postgres", Database: "shop"}
}

func relMsg() *pglogrepl.RelationMessage {
	return &pglogrepl.RelationMessage{
		RelationID:   1,
		Namespace:    "public",
		RelationName: "orders",
		Columns: []*pglogrepl.RelationMessageColumn{
			{Name: "id", Flags: 1}, // 키(replica identity)
			{Name: "amount", Flags: 0},
			{Name: "note", Flags: 0},
		},
	}
}

func textCol(v string) *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeText, Data: []byte(v)}
}

// unchanged TOAST('u') 컬럼은 직전 행 값으로 채워야 null 오염이 없다.
func TestTupleToMap_ToastReuse(t *testing.T) {
	r := newPostgresReplicator(&CDCSource{database: "shop"})
	rel := relMsg()

	// 1) INSERT/최초 UPDATE 로 prevRow 채움: id=1, note="big blob"
	first := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("1"), textCol("100"), textCol("big blob"),
	}}
	m1 := r.tupleToMap(rel.RelationID, rel, first)
	if m1["note"] != "big blob" {
		t.Fatalf("note = %v, want 'big blob'", m1["note"])
	}

	// 2) 다음 UPDATE 에서 note 는 unchanged TOAST('u') → 값 없음. 이전 값 재사용해야 함.
	second := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("1"), textCol("200"),
		{DataType: pglogrepl.TupleDataTypeToast},
	}}
	m2 := r.tupleToMap(rel.RelationID, rel, second)
	if m2["amount"] != "200" {
		t.Errorf("amount = %v, want '200'", m2["amount"])
	}
	if m2["note"] != "big blob" {
		t.Errorf("note = %v, want carried-over 'big blob' (TOAST reuse)", m2["note"])
	}
}

// NULL('n') 컬럼은 nil 로, 텍스트('t')는 string 으로.
func TestTupleToMap_NullAndText(t *testing.T) {
	r := newPostgresReplicator(&CDCSource{})
	rel := relMsg()
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("7"),
		{DataType: pglogrepl.TupleDataTypeNull},
		textCol("hi"),
	}}
	m := r.tupleToMap(rel.RelationID, rel, tuple)
	if m["id"] != "7" {
		t.Errorf("id = %v, want '7'", m["id"])
	}
	if m["amount"] != nil {
		t.Errorf("amount = %v, want nil", m["amount"])
	}
	if m["note"] != "hi" {
		t.Errorf("note = %v, want 'hi'", m["note"])
	}
}

// 키 컬럼(Flags==1)만 PK 로 뽑고, delete(old 만 존재)에서도 값이 나와야 한다.
func TestPgPrimaryKey(t *testing.T) {
	rel := relMsg()
	// delete: newData nil, oldData 에 키.
	cols, vals := pgPrimaryKey(rel, nil, map[string]any{"id": "42", "amount": "9"})
	if len(cols) != 1 || cols[0] != "id" {
		t.Fatalf("cols = %v, want [id]", cols)
	}
	if len(vals) != 1 || vals[0] != "42" {
		t.Errorf("vals = %v, want [42]", vals)
	}

	// insert/update: newData 우선.
	cols, vals = pgPrimaryKey(rel, map[string]any{"id": "1"}, nil)
	if cols[0] != "id" || vals[0] != "1" {
		t.Errorf("cols/vals = %v/%v, want id/1", cols, vals)
	}
}

func TestQuoteTables(t *testing.T) {
	got := quoteTables([]string{"public.orders", "customers"})
	want := []string{`"public"."orders"`, `"customers"`}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// postgres 드라이버는 이제 조기 거부되지 않고 pg 복제기가 세팅된다.
func TestNewCDCSource_PostgresAccepted(t *testing.T) {
	s, err := NewCDCSource(configPgSource())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.pg == nil {
		t.Fatal("pg replicator not initialized for postgres driver")
	}
	if s.pg.slotName != pgDefaultSlotName || s.pg.publicationName != pgDefaultPublicationName {
		t.Errorf("slot/pub = %s/%s, want defaults", s.pg.slotName, s.pg.publicationName)
	}
}

// start_lsn 지정 시 committedLSN 으로 파싱된다. 잘못된 값은 생성 거부.
func TestNewCDCSource_StartLSN(t *testing.T) {
	cfg := configPgSource()
	cfg.StartLSN = "0/1A2B3C4D"
	s, err := NewCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want, _ := pglogrepl.ParseLSN("0/1A2B3C4D")
	if s.committedLSN != want {
		t.Errorf("committedLSN = %v, want %v", s.committedLSN, want)
	}

	bad := configPgSource()
	bad.StartLSN = "not-an-lsn"
	if _, err := NewCDCSource(bad); err == nil {
		t.Error("expected error for invalid start_lsn")
	}
}

// LSN checkpoint 라운드트립: Get → Set 으로 committedLSN 복원.
func TestPostgresLSNCheckpointRoundtrip(t *testing.T) {
	s, _ := NewCDCSource(configPgSource())
	want, _ := pglogrepl.ParseLSN("2/B00D1E00")
	s.committedLSN = want
	s.pg.committedLSN = want

	cps := s.GetSourceCheckpoints()
	if len(cps) != 1 || cps[0].OffsetType != "lsn" {
		t.Fatalf("checkpoints = %+v, want single lsn", cps)
	}

	s2, _ := NewCDCSource(configPgSource())
	if err := s2.SetSourceCheckpoints(cps); err != nil {
		t.Fatalf("SetSourceCheckpoints: %v", err)
	}
	if s2.committedLSN != want {
		t.Errorf("restored committedLSN = %v, want %v", s2.committedLSN, want)
	}
	if s2.pg.committedLSN != want {
		t.Errorf("restored pg.committedLSN = %v, want %v", s2.pg.committedLSN, want)
	}
}
