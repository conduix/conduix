package executor

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// 정상 샘플 + 정상 stage → pass. 잘못된 stage 설정 → fail + 이슈 리포트.
func TestValidatePipelines(t *testing.T) {
	// filter + remap 파이프라인.
	group := &types.PipelineGroup{
		ID:   "vwf",
		Name: "v",
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "p1",
				Name: "p",
				Stages: []types.Stage{
					{Name: "only-active", Type: "filter", Config: map[string]any{"condition": ".status == 'active'"}},
				},
				Outputs: []types.Output{{Name: "sink", Type: "stub", Config: map[string]any{}}},
			},
		},
	}
	e := NewGroupExecutor(group)

	// 정상 샘플: filter 통과/필터 모두 에러 아님 → pass.
	rep := e.ValidatePipelines(map[string][]map[string]any{
		"p1": {
			{"id": 1, "status": "active"},
			{"id": 2, "status": "inactive"},
		},
	})
	if !rep.Pass {
		t.Errorf("정상 파이프라인인데 fail: %+v", rep.Issues)
	}

	// 잘못된 stage: schema_validate 에 malformed JSON Schema → applyStage 에러 유발.
	badGroup := &types.PipelineGroup{
		ID: "bad", Name: "bad",
		Pipelines: []types.GroupedPipeline{
			{
				ID: "p1", Name: "p",
				Stages: []types.Stage{
					// 잘못된 스키마 JSON → validator 생성 실패 → 에러.
					{Name: "bad-schema", Type: "schema_validate", Config: map[string]any{"json_schema": "{not valid json"}},
				},
				Outputs: []types.Output{{Name: "sink", Type: "stub", Config: map[string]any{}}},
			},
		},
	}
	be := NewGroupExecutor(badGroup)
	rep2 := be.ValidatePipelines(map[string][]map[string]any{
		"p1": {{"id": 1}},
	})
	if rep2.Pass {
		t.Error("잘못된 remap mapping 인데 pass — validation 이 못 잡음")
	}
	if len(rep2.Issues) == 0 {
		t.Error("fail 인데 이슈 리포트 없음")
	} else {
		t.Logf("검출된 이슈: %s", rep2.Issues[0].Message)
	}
}
