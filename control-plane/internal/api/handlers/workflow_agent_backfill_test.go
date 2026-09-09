package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

func setupExecutionDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.WorkflowExecution{}))
	return &database.DB{DB: db}
}

// realtime(streaming) 은 종료 결과 콜백이 영구히 발생하지 않으므로, 모니터링 조회가
// agent_id 를 남기는 유일한 경로다. 비어 있을 때 채우는 것이 핵심 동작이다.
func TestBackfillExecutionAgentID_FillsWhenEmpty(t *testing.T) {
	db := setupExecutionDB(t)
	h := &WorkflowHandler{db: db}

	exec := &models.WorkflowExecution{ID: "exec-1", WorkflowID: "wf-1", Status: "running"}
	require.NoError(t, db.Create(exec).Error)

	h.backfillExecutionAgentID(exec, "agent-a")

	assert.Equal(t, "agent-a", exec.AgentID, "메모리 상 구조체도 갱신돼야 응답에 반영된다")

	var reloaded models.WorkflowExecution
	require.NoError(t, db.First(&reloaded, "id = ?", "exec-1").Error)
	assert.Equal(t, "agent-a", reloaded.AgentID)
}

// 결과 콜백이 이미 기록한 값을 조회 시점 값으로 덮으면 이력이 지워진다.
func TestBackfillExecutionAgentID_KeepsExisting(t *testing.T) {
	db := setupExecutionDB(t)
	h := &WorkflowHandler{db: db}

	exec := &models.WorkflowExecution{ID: "exec-2", WorkflowID: "wf-1", Status: "running", AgentID: "agent-original"}
	require.NoError(t, db.Create(exec).Error)

	h.backfillExecutionAgentID(exec, "agent-other")

	var reloaded models.WorkflowExecution
	require.NoError(t, db.First(&reloaded, "id = ?", "exec-2").Error)
	assert.Equal(t, "agent-original", reloaded.AgentID)
}

// 위임 pod 이 AGENT_ID 를 못 받은 경우(구 이미지 등) 빈 문자열로 덮어쓰지 않아야 한다.
func TestBackfillExecutionAgentID_IgnoresEmptyAgentID(t *testing.T) {
	db := setupExecutionDB(t)
	h := &WorkflowHandler{db: db}

	exec := &models.WorkflowExecution{ID: "exec-3", WorkflowID: "wf-1", Status: "running"}
	require.NoError(t, db.Create(exec).Error)

	h.backfillExecutionAgentID(exec, "")

	assert.Empty(t, exec.AgentID)

	var reloaded models.WorkflowExecution
	require.NoError(t, db.First(&reloaded, "id = ?", "exec-3").Error)
	assert.Empty(t, reloaded.AgentID)
}
