package handlers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// JobConfig 가 Update 요청 구조체에 없어서 API 로는 수정이 불가한데도 success:true 를
// 반환했다 — 요청이 조용히 무시되어 DB 를 직접 고쳐야 했다(실측).
func TestUpdateWorkflowRequest_AcceptsJobConfig(t *testing.T) {
	body := []byte(`{"job_config":{"cpu":"150m","memory":"512Mi","cpu_limit":"800m"}}`)

	var req UpdateWorkflowRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.NotNil(t, req.JobConfig, "job_config 가 구조체에 없으면 요청이 조용히 버려진다")
	assert.Equal(t, "150m", req.JobConfig.CPU)
	assert.Equal(t, "512Mi", req.JobConfig.Memory)
	assert.Equal(t, "800m", req.JobConfig.CPULimit)
}

// 생성 시에도 지정할 수 있어야 한다 — 만든 뒤 수정하는 두 단계를 강요하지 않는다.
func TestCreateWorkflowRequest_AcceptsJobConfig(t *testing.T) {
	body := []byte(`{"project_id":"p1","name":"w","type":"batch","job_config":{"cpu":"200m"}}`)

	var req CreateWorkflowRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.NotNil(t, req.JobConfig)
	assert.Equal(t, "200m", req.JobConfig.CPU)
}

// 미지정은 빈 문자열로 남아야 한다 — agent 가 DefaultJobConfig 를 쓰는 경로를 막지 않는다.
func TestBuildWorkflowModel_OmitsJobConfigWhenNil(t *testing.T) {
	h := &WorkflowHandler{}
	w := h.buildWorkflowModel(&WorkflowSpec{
		ProjectID: "p1", Name: "w", Type: types.WorkflowTypeBatch,
	}, "tester")

	assert.Empty(t, w.JobConfig, "미지정이면 빈 문자열이어야 agent 기본값이 적용된다")
}

// 지정하면 JSON 으로 저장된다.
func TestBuildWorkflowModel_SerializesJobConfig(t *testing.T) {
	h := &WorkflowHandler{}
	w := h.buildWorkflowModel(&WorkflowSpec{
		ProjectID: "p1", Name: "w", Type: types.WorkflowTypeBatch,
		JobConfig: &types.JobConfig{CPU: "150m", Memory: "512Mi"},
	}, "tester")

	require.NotEmpty(t, w.JobConfig)
	var got types.JobConfig
	require.NoError(t, json.Unmarshal([]byte(w.JobConfig), &got))
	assert.Equal(t, "150m", got.CPU)
	assert.Equal(t, "512Mi", got.Memory)
}

// export→import 왕복에서 리소스 스펙이 유실되면, 노드 여유가 다른 환경으로 옮길 때
// 스케줄이 실패한다(실측: 2코어 노드에서 기본 500m 요청이 Insufficient cpu 로 Pending).
func TestWorkflowModelToSpec_IncludesJobConfig(t *testing.T) {
	w := &models.Workflow{
		ProjectID: "p1", Name: "w", Type: string(types.WorkflowTypeBatch),
		JobConfig: `{"cpu":"150m","memory":"512Mi"}`,
	}

	spec, err := workflowModelToSpec(w)
	require.NoError(t, err)
	require.NotNil(t, spec.JobConfig, "export 에 빠지면 import 때 값이 사라진다")
	assert.Equal(t, "150m", spec.JobConfig.CPU)
}

// JobConfig 가 없는 기존 워크플로우도 export 가 깨지지 않아야 한다.
func TestWorkflowModelToSpec_HandlesEmptyJobConfig(t *testing.T) {
	w := &models.Workflow{ProjectID: "p1", Name: "w", Type: string(types.WorkflowTypeBatch)}

	spec, err := workflowModelToSpec(w)
	require.NoError(t, err)
	assert.Nil(t, spec.JobConfig)
}
