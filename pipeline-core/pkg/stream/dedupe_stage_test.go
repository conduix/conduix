package stream

import (
	"context"
	"testing"
	"time"
)

func TestDedupeStage_KeepFirst(t *testing.T) {
	stage, err := NewDedupeStage("dedupe-first", map[string]any{
		"key_fields": []any{"id"},
		"strategy":   "keep_first",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// 첫 번째 레코드 통과
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a", "val": 1}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected first record to pass, got nil")
	}

	// 같은 키 → 드롭
	r2, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a", "val": 2}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 != nil {
		t.Error("expected duplicate to be dropped, got record")
	}

	// 다른 키 → 통과
	r3, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "b", "val": 3}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r3 == nil {
		t.Fatal("expected different key to pass, got nil")
	}
}

func TestDedupeStage_KeepLast(t *testing.T) {
	stage, err := NewDedupeStage("dedupe-last", map[string]any{
		"key_fields": []any{"id"},
		"strategy":   "keep_last",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// keep_last는 모든 레코드를 통과시킴 (스트리밍 환경에서는 마지막 것이 유효)
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a", "val": 1}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected record to pass")
	}

	r2, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a", "val": 2}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 == nil {
		t.Fatal("expected record to pass for keep_last")
	}
	if r2.Data["val"] != 2 {
		t.Errorf("expected val=2, got %v", r2.Data["val"])
	}
}

func TestDedupeStage_KeepLatest(t *testing.T) {
	stage, err := NewDedupeStage("dedupe-latest", map[string]any{
		"key_fields":      []any{"id"},
		"strategy":        "keep_latest",
		"timestamp_field": "ts",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	// 첫 레코드 (earlier timestamp)
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{
		"id": "a", "val": 1,
		"ts": now.Add(-time.Hour).Format(time.RFC3339),
	}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected first record to pass")
	}

	// 더 최신 레코드 → 통과
	r2, err := stage.Process(ctx, &Record{Data: map[string]any{
		"id": "a", "val": 2,
		"ts": now.Format(time.RFC3339),
	}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 == nil {
		t.Fatal("expected newer record to pass")
	}

	// 더 오래된 레코드 → 드롭
	r3, err := stage.Process(ctx, &Record{Data: map[string]any{
		"id": "a", "val": 3,
		"ts": now.Add(-2 * time.Hour).Format(time.RFC3339),
	}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r3 != nil {
		t.Error("expected older record to be dropped")
	}
}

func TestDedupeStage_Window(t *testing.T) {
	stage, err := NewDedupeStage("dedupe-window", map[string]any{
		"key_fields": []any{"id"},
		"strategy":   "keep_first",
		"window":     "100ms",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// 첫 레코드 통과
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a"}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected first record to pass")
	}

	// 같은 키 → 드롭
	r2, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a"}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 != nil {
		t.Error("expected duplicate within window to be dropped")
	}

	// 윈도우 만료 후 → 통과
	time.Sleep(150 * time.Millisecond)
	r3, err := stage.Process(ctx, &Record{Data: map[string]any{"id": "a"}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r3 == nil {
		t.Fatal("expected record after window expiry to pass")
	}
}

func TestDedupeStage_CompositeKey(t *testing.T) {
	stage, err := NewDedupeStage("dedupe-composite", map[string]any{
		"key_fields": []any{"tenant", "id"},
		"strategy":   "keep_first",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// tenant=A, id=1
	r1, _ := stage.Process(ctx, &Record{Data: map[string]any{"tenant": "A", "id": "1"}})
	if r1 == nil {
		t.Fatal("expected first record to pass")
	}

	// tenant=B, id=1 → 다른 composite key이므로 통과
	r2, _ := stage.Process(ctx, &Record{Data: map[string]any{"tenant": "B", "id": "1"}})
	if r2 == nil {
		t.Fatal("expected different composite key to pass")
	}

	// tenant=A, id=1 → 중복
	r3, _ := stage.Process(ctx, &Record{Data: map[string]any{"tenant": "A", "id": "1"}})
	if r3 != nil {
		t.Error("expected composite key duplicate to be dropped")
	}
}

func TestDedupeStage_MissingKeyFields(t *testing.T) {
	_, err := NewDedupeStage("dedupe-err", map[string]any{
		"strategy": "keep_first",
	})
	if err == nil {
		t.Fatal("expected error for missing key_fields")
	}
}
