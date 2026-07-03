package executor

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// newTestExecutor는 PreStages 단위 테스트에 필요한 최소 GroupExecutor를 만든다.
func newTestExecutor() *GroupExecutor {
	return NewGroupExecutor(&types.PipelineGroup{})
}

func testStatsAndSample() (*StatsCollector, *SampleBuffer) {
	return NewStatsCollector("test", "test"), NewSampleBuffer(10)
}

func TestApplyPreStages_NoPreStages_Passthrough(t *testing.T) {
	e := newTestExecutor()
	stats, sample := testStatsAndSample()

	in := []map[string]any{
		{"id": 1},
		{"id": 2},
	}
	ows := OutputWithSink{Output: types.Output{Name: "es"}}

	out := e.applyPreStages(ows, in, stats, sample)
	if len(out) != 2 {
		t.Fatalf("expected 2 records passthrough, got %d", len(out))
	}
}

func TestApplyPreStages_FilterDropsRecords(t *testing.T) {
	e := newTestExecutor()
	stats, sample := testStatsAndSample()

	in := []map[string]any{
		{"status": "active"},
		{"status": "inactive"},
		{"status": "active"},
	}
	ows := OutputWithSink{
		Output: types.Output{Name: "es"},
		PreStages: []types.Stage{
			{Name: "only-active", Type: "filter", Config: map[string]any{"condition": ".status == 'active'"}},
		},
	}

	out := e.applyPreStages(ows, in, stats, sample)
	if len(out) != 2 {
		t.Fatalf("expected 2 active records after filter, got %d", len(out))
	}
	for _, r := range out {
		if r["status"] != "active" {
			t.Fatalf("filter leaked a non-active record: %v", r)
		}
	}
}

func TestApplyPreStages_RemapTransforms(t *testing.T) {
	e := newTestExecutor()
	stats, sample := testStatsAndSample()

	in := []map[string]any{
		{"created_at": "2026-07-02"},
	}
	ows := OutputWithSink{
		Output: types.Output{Name: "es"},
		PreStages: []types.Stage{
			{Name: "ts", Type: "remap", Config: map[string]any{
				"mapping": map[string]any{"@timestamp": ".created_at"},
			}},
		},
	}

	out := e.applyPreStages(ows, in, stats, sample)
	if len(out) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out))
	}
	if out[0]["@timestamp"] != "2026-07-02" {
		t.Fatalf("remap PreStage did not add @timestamp field: %v", out[0])
	}
}

// TestApplyPreStages_OutputIsolation는 한 Output의 PreStages 변환이
// 공유 입력 맵을 오염시켜 다른 Output에 새어나가지 않음을 검증한다.
// (배치 모드에서 transformed 슬라이스는 모든 Output이 공유하므로 중요.)
func TestApplyPreStages_OutputIsolation(t *testing.T) {
	e := newTestExecutor()
	stats, sample := testStatsAndSample()

	shared := []map[string]any{
		{"created_at": "2026-07-02"},
	}

	owsA := OutputWithSink{
		Output: types.Output{Name: "es"},
		PreStages: []types.Stage{
			{Name: "ts", Type: "remap", Config: map[string]any{
				"mapping": map[string]any{"@timestamp": ".created_at"},
			}},
		},
	}
	owsB := OutputWithSink{Output: types.Output{Name: "s3"}} // PreStages 없음

	outA := e.applyPreStages(owsA, shared, stats, sample)
	outB := e.applyPreStages(owsB, shared, stats, sample)

	if _, ok := outA[0]["@timestamp"]; !ok {
		t.Fatalf("output A should have @timestamp")
	}
	if _, ok := outB[0]["@timestamp"]; ok {
		t.Fatalf("output B (no PreStages) leaked @timestamp from output A: %v", outB[0])
	}
	if _, ok := shared[0]["@timestamp"]; ok {
		t.Fatalf("shared input map was mutated by output A's PreStage: %v", shared[0])
	}
}
