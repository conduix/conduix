package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// reconcileTestHandler 는 Redis/Kafka 없이 reconcile 경로만 검증하는 최소 핸들러를 만든다.
func reconcileTestHandler(t *testing.T) (*WorkflowHandler, *database.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&models.Workflow{}, &models.WorkflowExecution{}, &models.Plugin{}, &models.RunnerVersion{}))
	db := &database.DB{DB: gdb}
	return &WorkflowHandler{
		db:             db,
		logger:         slog.Default(),
		runnerResolver: services.NewRunnerResolver(gdb),
	}, db
}

func reconcileRouter(h *WorkflowHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/internal/clusters/:id/running-executions", h.ReconcileClusterExecutions)
	return r
}

// running 인 단일 실행은 반환하고, partition sub(ParentExecutionID 有)와 다른 cluster·비-running 은 제외한다.
func TestReconcileClusterExecutions(t *testing.T) {
	h, db := reconcileTestHandler(t)

	wf := &models.Workflow{
		ID:              "wf-1",
		Name:            "rt",
		Type:            string(types.WorkflowTypeRealtime),
		PipelinesConfig: `[{"id":"p1","name":"rt","input":{"type":"kafka"},"stages":[],"outputs":[]}]`,
	}
	require.NoError(t, db.Create(wf).Error)

	snap := wf.PipelinesConfig
	now := time.Now()
	execs := []*models.WorkflowExecution{
		{ID: "e-single", WorkflowID: "wf-1", ClusterID: "c1", Status: string(types.PipelineGroupStatusRunning), PipelinesSnapshot: snap, StartedAt: now, CreatedAt: now},
		{ID: "e-sub", WorkflowID: "wf-1", ClusterID: "c1", ParentExecutionID: "e-parent", Status: string(types.PipelineGroupStatusRunning), PipelinesSnapshot: snap, StartedAt: now, CreatedAt: now},
		{ID: "e-othercluster", WorkflowID: "wf-1", ClusterID: "c2", Status: string(types.PipelineGroupStatusRunning), PipelinesSnapshot: snap, StartedAt: now, CreatedAt: now},
		{ID: "e-completed", WorkflowID: "wf-1", ClusterID: "c1", Status: string(types.PipelineGroupStatusCompleted), PipelinesSnapshot: snap, StartedAt: now, CreatedAt: now},
	}
	for _, e := range execs {
		require.NoError(t, db.Create(e).Error)
	}

	router := reconcileRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/internal/clusters/c1/running-executions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Success bool                              `json:"success"`
		Data    []*types.WorkflowExecutionCommand `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)

	// c1 의 running 단일 실행(e-single)만 나와야 한다. sub/다른cluster/completed 제외.
	require.Len(t, resp.Data, 1, "only the single running execution in c1 should be reconciled")
	cmd := resp.Data[0]
	require.Equal(t, "e-single", cmd.ExecutionID)
	require.Equal(t, "wf-1", cmd.WorkflowID)
	require.Equal(t, "c1", cmd.TargetClusterID)
	require.Equal(t, "reconcile", cmd.TriggeredBy)
	require.NotNil(t, cmd.WorkflowConfig)
	require.Len(t, cmd.WorkflowConfig.Pipelines, 1, "pipelines must be restored from snapshot")
}

// running execution 이 없으면 빈 배열을 반환한다(200, data:[]).
func TestReconcileClusterExecutions_EmptyWhenNoneRunning(t *testing.T) {
	h, _ := reconcileTestHandler(t)
	router := reconcileRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/internal/clusters/c1/running-executions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success bool                              `json:"success"`
		Data    []*types.WorkflowExecutionCommand `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Empty(t, resp.Data)
}
