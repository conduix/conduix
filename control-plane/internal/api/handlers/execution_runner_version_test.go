package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// native stage 워크플로우는 GHCR 이미지가 아니라 runner_versions.Binary 로 실행된다.
// 실행 레코드에 버전이 남지 않으면 "어떤 코드가 돌고 있는지" 확인할 방법이 없다.
func TestWorkflowExecution_RunnerVersionIDPersists(t *testing.T) {
	db := setupExecutionDB(t)

	exec := &models.WorkflowExecution{
		ID:              "exec-rv",
		WorkflowID:      "wf-1",
		Status:          "running",
		RunnerVersionID: "rv-8a80887a",
	}
	require.NoError(t, db.Create(exec).Error)

	var got models.WorkflowExecution
	require.NoError(t, db.First(&got, "id = ?", "exec-rv").Error)
	assert.Equal(t, "rv-8a80887a", got.RunnerVersionID)
}

// native stage 를 안 쓰는 워크플로우는 비어 있어야 한다 — 빈 값이 '구버전' 으로 오탐되면 안 된다.
func TestWorkflowExecution_RunnerVersionIDEmptyForNonNative(t *testing.T) {
	db := setupExecutionDB(t)

	exec := &models.WorkflowExecution{ID: "exec-plain", WorkflowID: "wf-1", Status: "running"}
	require.NoError(t, db.Create(exec).Error)

	var got models.WorkflowExecution
	require.NoError(t, db.First(&got, "id = ?", "exec-plain").Error)
	assert.Empty(t, got.RunnerVersionID)
}

// 버전으로 실행을 조회할 수 있어야 한다 — "이 버전으로 도는 실행이 무엇인가" 를 답하려면 필요하다.
func TestWorkflowExecution_QueryByRunnerVersion(t *testing.T) {
	db := setupExecutionDB(t)

	require.NoError(t, db.Create(&models.WorkflowExecution{
		ID: "e1", WorkflowID: "wf-1", Status: "running", RunnerVersionID: "rv-old",
	}).Error)
	require.NoError(t, db.Create(&models.WorkflowExecution{
		ID: "e2", WorkflowID: "wf-1", Status: "running", RunnerVersionID: "rv-new",
	}).Error)

	var stale []models.WorkflowExecution
	require.NoError(t, db.Where("runner_version_id = ?", "rv-old").Find(&stale).Error)
	require.Len(t, stale, 1)
	assert.Equal(t, "e1", stale[0].ID)
}
