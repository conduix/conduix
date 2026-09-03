package output

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func sweepCfg(mode string) config.OutputConfig {
	return config.OutputConfig{
		Type: "sql", Driver: "mysql", DSN: "dsn", Table: "t",
		SyncedAtColumn: "synced_at",
		Sweep:          &config.SweepConfig{Mode: mode},
	}
}

// sweep 미설정 파이프라인은 검증·기본값 어느 것에도 걸리지 않아야 한다 (opt-in 보장)
func TestNewSQLOutput_NoSweepNoEffect(t *testing.T) {
	o, err := NewSQLOutput(config.OutputConfig{Type: "sql", Driver: "mysql", DSN: "dsn", Table: "t"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.sweep != nil || o.syncedAtColumn != "" {
		t.Errorf("expected zero sweep config, got sweep=%+v syncedAt=%q", o.sweep, o.syncedAtColumn)
	}
	// Finalize 는 sweep 미설정이면 db 없이도 no-op 이어야 한다
	if err := o.Finalize(context.Background(), true); err != nil {
		t.Errorf("expected no-op finalize, got %v", err)
	}
}

func TestNewSQLOutput_SweepValidation(t *testing.T) {
	if _, err := NewSQLOutput(sweepCfg("truncate")); err == nil {
		t.Error("expected error for invalid sweep.mode")
	}

	noCol := config.OutputConfig{
		Type: "sql", Driver: "mysql", DSN: "dsn", Table: "t",
		Sweep: &config.SweepConfig{Mode: "delete"},
	}
	if _, err := NewSQLOutput(noCol); err == nil {
		t.Error("expected error when neither synced_at_column nor sweep.column set")
	}

	o, err := NewSQLOutput(sweepCfg("soft"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.sweep.Column != "synced_at" {
		t.Errorf("expected sweep.column fallback to synced_at_column, got %q", o.sweep.Column)
	}
	if o.sweep.SoftColumn != "deleted_at" {
		t.Errorf("expected default soft_column deleted_at, got %q", o.sweep.SoftColumn)
	}
}

func TestStampRecords(t *testing.T) {
	o, _ := NewSQLOutput(sweepCfg("soft"))
	o.runStartedAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	original := map[string]any{"id": 1}
	stamped := o.stampRecords([]source.Record{{Data: original}})

	if got := stamped[0].Data["synced_at"]; got != o.runStartedAt {
		t.Errorf("expected synced_at stamped with T, got %v", got)
	}
	if v, ok := stamped[0].Data["deleted_at"]; !ok || v != nil {
		t.Errorf("expected soft flag reset to NULL on upsert, got %v (ok=%v)", v, ok)
	}
	// fan-out 시 다른 sink 와 공유되는 원본 맵은 오염되면 안 된다
	if _, ok := original["synced_at"]; ok {
		t.Error("expected original record data untouched (copy-on-write)")
	}
}

func TestStampRecords_NoopWithoutSyncedAt(t *testing.T) {
	o := &SQLOutput{}
	in := []source.Record{{Data: map[string]any{"id": 1}}}
	out := o.stampRecords(in)
	if &out[0] != &in[0] {
		t.Error("expected same slice returned when synced_at_column unset")
	}
}

func TestEnsureStampColumns(t *testing.T) {
	o, _ := NewSQLOutput(sweepCfg("soft"))
	cols := o.ensureStampColumns([]string{"id", "name"})
	joined := strings.Join(cols, ",")
	if !strings.Contains(joined, "synced_at") || !strings.Contains(joined, "deleted_at") {
		t.Errorf("expected stamp columns appended, got %v", cols)
	}
	// 이미 있으면 중복 추가하지 않는다
	cols2 := o.ensureStampColumns([]string{"id", "synced_at", "deleted_at"})
	if len(cols2) != 3 {
		t.Errorf("expected no duplicate columns, got %v", cols2)
	}
}

func TestBuildSweepSQL_DeleteMySQL(t *testing.T) {
	o, _ := NewSQLOutput(sweepCfg("delete"))
	o.runStartedAt = time.Now()
	query, args := o.buildSweepSQL()
	if query != "DELETE FROM `t` WHERE `synced_at` < ?" {
		t.Errorf("unexpected query: %s", query)
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestBuildSweepSQL_SoftPostgres(t *testing.T) {
	cfg := sweepCfg("soft")
	cfg.Driver = "postgres"
	o, _ := NewSQLOutput(cfg)
	o.runStartedAt = time.Now()
	query, args := o.buildSweepSQL()
	want := `UPDATE "t" SET "deleted_at" = $1 WHERE "synced_at" < $2 AND "deleted_at" IS NULL`
	if query != want {
		t.Errorf("unexpected query:\n got: %s\nwant: %s", query, want)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

// 부분 실패 시 sweep 이 절대 실행되지 않아야 한다 — db 가 nil 이어도 안전해야
// 실행 경로에 도달하지 않았음이 증명된다.
func TestFinalize_SkipsOnFailure(t *testing.T) {
	o, _ := NewSQLOutput(sweepCfg("delete"))
	if err := o.Finalize(context.Background(), false); err != nil {
		t.Errorf("expected skip without error, got %v", err)
	}
}
