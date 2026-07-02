package stream

import (
	"context"
	"testing"
	"time"
)

func TestThrottleStage_AllowWithinRate(t *testing.T) {
	stage, err := NewThrottleStage("throttle-basic", map[string]any{
		"rate":     100,
		"interval": "second",
		"burst":    100,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// 버스트 이내이므로 즉시 통과
	for i := range 5 {
		r, err := stage.Process(ctx, &Record{Data: map[string]any{"i": i}})
		if err != nil {
			t.Fatalf("Process failed on record %d: %v", i, err)
		}
		if r == nil {
			t.Fatalf("expected record %d to pass", i)
		}
	}

	input, output, errors := stage.Stats()
	if input != 5 || output != 5 || errors != 0 {
		t.Errorf("expected stats 5/5/0, got %d/%d/%d", input, output, errors)
	}
}

func TestThrottleStage_DropOnLimit(t *testing.T) {
	stage, err := NewThrottleStage("throttle-drop", map[string]any{
		"rate":          1,
		"interval":      "second",
		"burst":         1,
		"drop_on_limit": true,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// 첫 번째: 버스트 허용으로 통과
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{"i": 0}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected first record to pass (burst)")
	}

	// 즉시 두 번째: 버스트 소진 → 드롭
	r2, err := stage.Process(ctx, &Record{Data: map[string]any{"i": 1}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 != nil {
		t.Error("expected second record to be dropped")
	}
}

func TestThrottleStage_WaitMode(t *testing.T) {
	stage, err := NewThrottleStage("throttle-wait", map[string]any{
		"rate":          10,
		"interval":      "second",
		"burst":         1,
		"drop_on_limit": false,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	start := time.Now()

	// 버스트 1로 설정: 첫 번째는 즉시, 두 번째는 대기
	r1, err := stage.Process(ctx, &Record{Data: map[string]any{"i": 0}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r1 == nil {
		t.Fatal("expected first record to pass")
	}

	r2, err := stage.Process(ctx, &Record{Data: map[string]any{"i": 1}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r2 == nil {
		t.Fatal("expected second record to pass after waiting")
	}

	elapsed := time.Since(start)
	// rate=10/s → 100ms interval, should have waited ~100ms
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected some wait time, elapsed only %v", elapsed)
	}
}

func TestThrottleStage_ContextCancelled(t *testing.T) {
	stage, err := NewThrottleStage("throttle-cancel", map[string]any{
		"rate":          1,
		"interval":      "hour", // 매우 느린 rate
		"burst":         1,
		"drop_on_limit": false,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 첫 번째: 버스트로 통과
	_, _ = stage.Process(ctx, &Record{Data: map[string]any{}})

	// 컨텍스트 취소 후 두 번째 시도 → 에러
	cancel()
	_, err = stage.Process(ctx, &Record{Data: map[string]any{}})
	if err == nil {
		t.Error("expected error after context cancellation")
	}
}

func TestThrottleStage_MinuteInterval(t *testing.T) {
	stage, err := NewThrottleStage("throttle-minute", map[string]any{
		"rate":          60,
		"interval":      "minute",
		"burst":         10,
		"drop_on_limit": true,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	ctx := context.Background()

	// 60/minute = 1/second, burst=10이므로 10개까지는 즉시 통과
	passed := 0
	for i := range 15 {
		r, err := stage.Process(ctx, &Record{Data: map[string]any{"i": i}})
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}
		if r != nil {
			passed++
		}
	}

	if passed != 10 {
		t.Errorf("expected 10 records to pass (burst), got %d", passed)
	}
}

func TestThrottleStage_InvalidRate(t *testing.T) {
	_, err := NewThrottleStage("throttle-err", map[string]any{
		"interval": "second",
	})
	if err == nil {
		t.Fatal("expected error for missing rate")
	}
}

func TestThrottleStage_FallbackStrategy(t *testing.T) {
	// sliding_window는 token_bucket으로 fallback, 에러 없이 동작해야 함
	stage, err := NewThrottleStage("throttle-fallback", map[string]any{
		"rate":     100,
		"interval": "second",
		"strategy": "sliding_window",
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	r, err := stage.Process(context.Background(), &Record{Data: map[string]any{}})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if r == nil {
		t.Fatal("expected record to pass")
	}
}

// SetRate로 처리율을 올리면 이전 rate에서 드롭되던 레코드가 통과해야 한다.
func TestThrottleStage_SetRate(t *testing.T) {
	stage, err := NewThrottleStage("throttle-setrate", map[string]any{
		"rate":          1,
		"interval":      "second",
		"burst":         1,
		"drop_on_limit": true,
	})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}
	ctx := context.Background()

	// burst=1 소진
	if r, _ := stage.Process(ctx, &Record{Data: map[string]any{}}); r == nil {
		t.Fatal("first record should pass (burst)")
	}
	if r, _ := stage.Process(ctx, &Record{Data: map[string]any{}}); r != nil {
		t.Fatal("second record should drop at rate=1")
	}

	// 런타임에 rate/burst 상향 (1000/s → 토큰이 ~1ms마다 1개씩 충전)
	if err := stage.SetRate(1000, 1000); err != nil {
		t.Fatalf("SetRate failed: %v", err)
	}

	// 토큰 버킷이 상향된 rate로 재충전되므로, 잠시 후 다시 통과해야 한다.
	// (SetBurst는 상한만 올리고 토큰은 rate에 따라 시간이 지나며 충전됨 - 정상 동작)
	time.Sleep(20 * time.Millisecond)
	passed := 0
	for range 10 {
		if r, _ := stage.Process(ctx, &Record{Data: map[string]any{}}); r != nil {
			passed++
		}
	}
	if passed == 0 {
		t.Errorf("after SetRate(1000) + refill, expected records to pass again, got 0")
	}
}

func TestThrottleStage_SetRate_Invalid(t *testing.T) {
	stage, err := NewThrottleStage("throttle-setrate-invalid", map[string]any{"rate": 10})
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}
	if err := stage.SetRate(0, 0); err == nil {
		t.Error("expected error for rate <= 0")
	}
}
