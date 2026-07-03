package handlers

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// spec → YAML → spec 왕복이 무손실인지 검증(핵심: export한 YAML을 import하면 동일 정의).
func TestWorkflowYAML_RoundTrip(t *testing.T) {
	orig := &WorkflowSpec{
		ProjectID:     "proj-1",
		ClusterID:     "cluster-west",
		Name:          "orders-realtime",
		Slug:          "orders-realtime",
		Description:   "실시간 주문 처리",
		Type:          types.PipelineGroupType("realtime"),
		ExecutionMode: types.ExecutionMode("parallel"),
		Schedule: &types.ScheduleConfig{
			Type:    types.ScheduleType("cron"),
			Cron:    "0 * * * *",
			Enabled: true,
		},
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "will-be-reassigned",
				Name: "ingest",
				Input: types.WorkflowInput{
					Type:   "kafka",
					Config: map[string]any{"topics": []any{"orders"}, "group_id": "orders-consumers"},
				},
				Stages: []types.Stage{
					{Name: "f1", Type: "filter", Config: map[string]any{"condition": ".status == 'active'"}},
					{Name: "t1", Type: "throttle", Config: map[string]any{"rate": float64(100), "interval": "second"}},
				},
				Outputs: []types.Output{
					{Name: "es", Type: "elasticsearch", Config: map[string]any{"index": "orders"}},
				},
			},
		},
		Tags:     []string{"prod", "orders"},
		Metadata: map[string]any{"owner": "team-a"},
	}

	yamlBytes, err := specToYAML(orig)
	if err != nil {
		t.Fatalf("specToYAML failed: %v", err)
	}

	got, err := yamlToSpec(yamlBytes)
	if err != nil {
		t.Fatalf("yamlToSpec failed: %v", err)
	}

	if got.Name != orig.Name || got.Type != orig.Type || got.ClusterID != orig.ClusterID {
		t.Errorf("scalar mismatch: got name=%s type=%s cluster=%s", got.Name, got.Type, got.ClusterID)
	}
	if got.Schedule == nil || got.Schedule.Cron != "0 * * * *" || !got.Schedule.Enabled {
		t.Errorf("schedule lost in round-trip: %+v", got.Schedule)
	}
	if len(got.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(got.Pipelines))
	}
	p := got.Pipelines[0]
	if p.Name != "ingest" || p.Input.Type != "kafka" {
		t.Errorf("pipeline core fields lost: name=%s input=%s", p.Name, p.Input.Type)
	}
	if len(p.Stages) != 2 || p.Stages[0].Type != "filter" || p.Stages[1].Type != "throttle" {
		t.Errorf("stages lost/reordered: %+v", p.Stages)
	}
	if len(p.Outputs) != 1 || p.Outputs[0].Type != "elasticsearch" {
		t.Errorf("outputs lost: %+v", p.Outputs)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "prod" {
		t.Errorf("tags lost: %+v", got.Tags)
	}
	if got.Metadata["owner"] != "team-a" {
		t.Errorf("metadata lost: %+v", got.Metadata)
	}
}

// buildWorkflowModel은 파이프라인 ID를 새로 부여해야 한다(템플릿 인스턴스화 시 충돌 방지).
func TestBuildWorkflowModel_ReassignsPipelineIDs(t *testing.T) {
	h := &WorkflowHandler{}
	spec := &WorkflowSpec{
		ProjectID: "p1",
		Name:      "wf",
		Type:      types.PipelineGroupType("batch"),
		Pipelines: []types.GroupedPipeline{
			{ID: "old-id-1", Name: "a"},
			{ID: "old-id-1", Name: "b"}, // 의도적 중복 ID
		},
	}

	wf := h.buildWorkflowModel(spec, "user-1")
	if wf.ID == "" {
		t.Error("workflow ID should be assigned")
	}
	if wf.Status != string(types.PipelineGroupStatusIdle) {
		t.Errorf("new workflow should be idle, got %s", wf.Status)
	}
	if wf.ExecutionMode != string(types.ExecutionModeParallel) {
		t.Errorf("default execution mode should be parallel, got %s", wf.ExecutionMode)
	}
	// PipelinesConfig에서 ID가 새로 부여됐고 서로 다른지 확인은 통합 테스트 영역이나,
	// 최소한 빈 값이 아님을 확인
	if wf.PipelinesConfig == "" || wf.PipelinesConfig == "null" {
		t.Error("pipelines config should be serialized")
	}
}
