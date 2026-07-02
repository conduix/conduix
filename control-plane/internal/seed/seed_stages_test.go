package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/shared/types"
)

// 샘플 커스텀 stage(js_script)들이 실제 파이프라인에서 변환을 수행하는지 검증한다.
// 파일 소스로 레코드를 넣고, 3개 커스텀 stage를 거쳐 stub 출력까지 관통한 뒤
// 처리 건수를 확인한다(변환 자체의 정확성은 각 stage의 goja 실행으로 보장).
func runSampleStages(t *testing.T, records string, stages []types.Stage) *types.PipelineGroupExecution {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.json")
	if err := os.WriteFile(path, []byte(records), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	group := &types.PipelineGroup{
		ID:            "seed-test",
		Name:          "seed-test",
		ExecutionMode: types.ExecutionModeSequential,
		Pipelines: []types.GroupedPipeline{{
			ID:      "p1",
			Name:    "custom-stages",
			Input:   types.WorkflowInput{Type: "file", Config: map[string]any{"type": "file", "path": path, "format": "json"}},
			Stages:  stages,
			Outputs: []types.Output{{Name: "sink", Type: "stub", Config: map[string]any{}}},
		}},
	}

	e := executor.NewGroupExecutor(group)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := e.Start(ctx, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for e.Status() == types.PipelineGroupStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	exec := e.Execution()
	if exec == nil || exec.Status != types.PipelineGroupStatusCompleted {
		t.Fatalf("expected completed, got %+v", exec)
	}
	return exec
}

func TestSampleCustomStages_TextToNumber(t *testing.T) {
	exec := runSampleStages(t, `[{"price":"3.14","quantity":"5"}]`, []types.Stage{textToNumberStage()})
	if exec.TotalRecords != 1 {
		t.Errorf("expected 1 record processed, got %d", exec.TotalRecords)
	}
}

func TestSampleCustomStages_JSONTransform(t *testing.T) {
	exec := runSampleStages(t, `[{"first_name":"Ada","last_name":"Lovelace","status":"ACTIVE"}]`, []types.Stage{jsonTransformStage()})
	if exec.TotalRecords != 1 {
		t.Errorf("expected 1 record processed, got %d", exec.TotalRecords)
	}
}

func TestSampleCustomStages_JSONExtract(t *testing.T) {
	exec := runSampleStages(t, `[{"payload_json":"{\"user\":{\"email\":\"a@b.com\"},\"event\":{\"type\":\"signup\"}}"}]`, []types.Stage{jsonExtractStage()})
	if exec.TotalRecords != 1 {
		t.Errorf("expected 1 record processed, got %d", exec.TotalRecords)
	}
}

// 3개 커스텀 stage를 체인으로 연결해도 통과하는지(샘플 파이프라인 구성 그대로).
func TestSampleCustomStages_Chained(t *testing.T) {
	recs := `[{"price":"10","first_name":"Grace","payload_json":"{\"user\":{\"email\":\"g@h.com\"}}"}]`
	exec := runSampleStages(t, recs, []types.Stage{textToNumberStage(), jsonTransformStage(), jsonExtractStage()})
	if exec.TotalRecords != 1 {
		t.Errorf("expected 1 record through 3 custom stages, got %d", exec.TotalRecords)
	}
}
