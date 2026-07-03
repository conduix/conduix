package executor

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// applyStage가 내장하지 않은 타입(cast)을 stream 레지스트리로 위임해 실제 변환하는지 검증.
// 브리지 이전에는 default 케이스로 통과(no-op)되어 "25"가 그대로 남았다.
func TestApplyStage_BridgesToStreamRegistry_Cast(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	defer e.closeStreamStages()

	stage := types.Stage{
		Name: "cast-age",
		Type: "cast",
		Config: map[string]any{
			"casts": map[string]any{"age": "int"},
		},
	}

	out, err := e.applyStage(map[string]any{"age": "25"}, stage)
	if err != nil {
		t.Fatalf("applyStage failed: %v", err)
	}
	if out["age"] != int64(25) {
		t.Fatalf("cast stage did not run via bridge: expected int64(25), got %v (%T)", out["age"], out["age"])
	}
}

// throttle(drop_on_limit)도 워크플로우 경로에서 실제 동작하는지 검증.
func TestApplyStage_BridgesToStreamRegistry_Throttle(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	defer e.closeStreamStages()

	stage := types.Stage{
		Name: "throttle-1",
		Type: "throttle",
		Config: map[string]any{
			"rate":          1,
			"interval":      "second",
			"burst":         1,
			"drop_on_limit": true,
		},
	}

	// 첫 레코드: burst 허용으로 통과
	out1, err := e.applyStage(map[string]any{"i": 1}, stage)
	if err != nil {
		t.Fatalf("applyStage failed: %v", err)
	}
	if out1 == nil {
		t.Fatal("first record should pass through throttle burst")
	}

	// 즉시 두 번째: burst 소진 → 드롭(nil). 브리지 이전엔 항상 통과했다.
	out2, err := e.applyStage(map[string]any{"i": 2}, stage)
	if err != nil {
		t.Fatalf("applyStage failed: %v", err)
	}
	if out2 != nil {
		t.Fatal("second record should be dropped by throttle (bridge not working)")
	}
}

// 같은 stage에 대해 인스턴스가 재사용(캐시)되어 상태가 유지되는지 검증.
// throttle이 매 호출 새로 생성되면 매번 burst가 리셋되어 드롭이 발생하지 않는다.
func TestApplyStage_StreamStageInstanceReused(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	defer e.closeStreamStages()

	stage := types.Stage{
		Name:   "throttle-reuse",
		Type:   "throttle",
		Config: map[string]any{"rate": 1, "interval": "second", "burst": 1, "drop_on_limit": true},
	}

	dropped := 0
	for range 5 {
		out, err := e.applyStage(map[string]any{}, stage)
		if err != nil {
			t.Fatalf("applyStage failed: %v", err)
		}
		if out == nil {
			dropped++
		}
	}
	// 인스턴스가 재사용되면 burst=1 이후 나머지는 드롭되어야 한다.
	if dropped == 0 {
		t.Fatal("no records dropped — stage instance not reused (recreated each call)")
	}
}

// 알 수 없는 타입은 안전하게 통과(하위호환)해야 한다.
func TestApplyStage_UnknownTypePassesThrough(t *testing.T) {
	e := NewGroupExecutor(&types.PipelineGroup{})
	defer e.closeStreamStages()

	stage := types.Stage{Name: "x", Type: "totally_unknown_type_xyz", Config: map[string]any{}}
	in := map[string]any{"keep": "me"}
	out, err := e.applyStage(in, stage)
	if err != nil {
		t.Fatalf("unknown type should not error, got: %v", err)
	}
	if out["keep"] != "me" {
		t.Fatalf("unknown type should pass data through unchanged, got %v", out)
	}
}
