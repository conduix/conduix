package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 인프라 없이(파일 소스 + stub 출력) 워크플로우 전 구간을 실제 실행하는 통합 테스트.
// source → 공통 Stage(filter) → output(stub) → 통계까지 실제 코드 경로를 관통한다.
func TestE2E_FileSource_Filter_StubOutput(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	// 5건 중 status=active 3건만 filter 통과해야 한다.
	content := `[
	  {"id": 1, "status": "active"},
	  {"id": 2, "status": "inactive"},
	  {"id": 3, "status": "active"},
	  {"id": 4, "status": "inactive"},
	  {"id": 5, "status": "active"}
	]`
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	group := &types.PipelineGroup{
		ID:            "e2e-wf",
		Name:          "e2e",
		ExecutionMode: types.ExecutionModeSequential,
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "p1",
				Name: "ingest",
				Input: types.WorkflowInput{
					Type:   "file",
					Config: map[string]any{"type": "file", "path": inputPath, "format": "json"},
				},
				Stages: []types.Stage{
					{Name: "only-active", Type: "filter", Config: map[string]any{"condition": ".status == 'active'"}},
				},
				Outputs: []types.Output{
					{Name: "sink", Type: "stub", Config: map[string]any{}},
				},
			},
		},
	}

	e := NewGroupExecutor(group)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := e.Start(ctx, "test"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 실행 완료 대기
	deadline := time.Now().Add(8 * time.Second)
	for e.Status() == types.PipelineGroupStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("execution did not complete in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	exec := e.Execution()
	if exec == nil {
		t.Fatal("no execution result")
	}
	if exec.Status != types.PipelineGroupStatusCompleted {
		t.Fatalf("expected completed, got %s (err=%s)", exec.Status, exec.ErrorMessage)
	}
	// filter로 active 3건만 출력됐어야 한다.
	if exec.TotalRecords != 3 {
		t.Errorf("expected 3 records written (active only), got %d", exec.TotalRecords)
	}
}

// filter가 전부 통과하는 경우 5건 모두 출력.
func TestE2E_FileSource_NoFilter(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	content := `[{"id":1},{"id":2},{"id":3}]`
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	group := &types.PipelineGroup{
		ID:            "e2e-wf2",
		Name:          "e2e2",
		ExecutionMode: types.ExecutionModeSequential,
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "p1",
				Name: "passthrough",
				Input: types.WorkflowInput{
					Type:   "file",
					Config: map[string]any{"type": "file", "path": inputPath, "format": "json"},
				},
				Outputs: []types.Output{
					{Name: "sink", Type: "stub", Config: map[string]any{}},
				},
			},
		},
	}

	e := NewGroupExecutor(group)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := e.Start(ctx, "test"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for e.Status() == types.PipelineGroupStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("execution did not complete in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	exec := e.Execution()
	if exec == nil || exec.Status != types.PipelineGroupStatusCompleted {
		t.Fatalf("expected completed execution, got %+v", exec)
	}
	if exec.TotalRecords != 3 {
		t.Errorf("expected 3 records, got %d", exec.TotalRecords)
	}
}
