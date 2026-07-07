package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// WithAssignedPartitions 지정 시 배정된 파티션만 실행되는지(분산 실행의 executor 필터).
func TestAssignedPartitions_RunsSubsetOnly(t *testing.T) {
	dir := t.TempDir()
	// 파티션별 파일 소스. p-a: 2건, p-b: 3건.
	pathA := filepath.Join(dir, "a.json")
	pathB := filepath.Join(dir, "b.json")
	if err := os.WriteFile(pathA, []byte(`[{"id":1},{"id":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(`[{"id":3},{"id":4},{"id":5}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	mkGroup := func() *types.PipelineGroup {
		return &types.PipelineGroup{
			ID: "pa", Name: "pa", ExecutionMode: types.ExecutionModeSequential,
			Pipelines: []types.GroupedPipeline{
				{
					ID: "p1", Name: "p",
					Input: types.WorkflowInput{
						Type:   "file",
						Name:   "src",
						Config: map[string]any{"type": "file", "format": "json"},
						Partitions: []types.PartitionConfig{
							{ID: "p-a", Enabled: true, Config: map[string]any{"path": pathA}},
							{ID: "p-b", Enabled: true, Config: map[string]any{"path": pathB}},
						},
					},
					Outputs: []types.Output{{Name: "sink", Type: "stub", Config: map[string]any{}}},
				},
			},
		}
	}

	run := func(assigned []string) int64 {
		opts := []GroupExecutorOption{}
		if assigned != nil {
			opts = append(opts, WithAssignedPartitions(assigned))
		}
		e := NewGroupExecutor(mkGroup(), opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := e.Start(ctx, "test"); err != nil {
			t.Fatalf("start: %v", err)
		}
		deadline := time.Now().Add(9 * time.Second)
		for e.Status() == types.PipelineGroupStatusRunning {
			if time.Now().After(deadline) {
				t.Fatal("did not complete")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return e.Execution().TotalRecords
	}

	// 전체(배정 없음): 5건.
	if got := run(nil); got != 5 {
		t.Errorf("all partitions: got %d, want 5", got)
	}
	// p-a 만 배정: 2건.
	if got := run([]string{"p-a"}); got != 2 {
		t.Errorf("only p-a: got %d, want 2", got)
	}
	// p-b 만 배정: 3건.
	if got := run([]string{"p-b"}); got != 3 {
		t.Errorf("only p-b: got %d, want 3", got)
	}
}
