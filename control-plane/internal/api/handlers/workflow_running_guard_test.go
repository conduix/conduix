package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// BUG#5 회귀: running workflow 는 pipeline 추가/삭제를 409 로 거부해야 한다(UpdateWorkflow 와 일관).
// 예전엔 AddPipeline/RemovePipeline 이 가드 없이 201/200 으로 running workflow DB 를 조용히 변경했다.
func TestPipelineChangeRejectedWhenRunning(t *testing.T) {
	h, db := reconcileTestHandler(t)
	require.NoError(t, db.Create(&models.Workflow{
		ID:              "wf-run",
		Name:            "running-wf",
		Type:            string(types.WorkflowTypeRealtime),
		Status:          string(types.PipelineGroupStatusRunning),
		PipelinesConfig: `[{"id":"p1","name":"a"}]`,
		CreatedAt:       time.Now(),
	}).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/workflows/:id/pipelines", h.AddPipelineToWorkflow)
	r.DELETE("/api/v1/workflows/:id/pipelines/:pipelineId", h.RemovePipelineFromWorkflow)

	// AddPipeline → 409
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-run/pipelines",
		strings.NewReader(`{"id":"p2","name":"b","input":{"type":"kafka"},"stages":[],"outputs":[]}`))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	r.ServeHTTP(addW, addReq)
	require.Equal(t, http.StatusConflict, addW.Code, "AddPipeline on running workflow must be 409")

	// RemovePipeline → 409
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/workflows/wf-run/pipelines/p1", nil)
	delW := httptest.NewRecorder()
	r.ServeHTTP(delW, delReq)
	require.Equal(t, http.StatusConflict, delW.Code, "RemovePipeline on running workflow must be 409")

	// DB config 는 변경되지 않아야 한다(원래 p1 하나 그대로).
	var wf models.Workflow
	require.NoError(t, db.First(&wf, "id = ?", "wf-run").Error)
	require.Equal(t, `[{"id":"p1","name":"a"}]`, wf.PipelinesConfig, "running workflow config must be unchanged")
}

// idle workflow 는 정상적으로 pipeline 추가가 된다(가드가 과하게 막지 않는지).
func TestPipelineAddAllowedWhenIdle(t *testing.T) {
	h, db := reconcileTestHandler(t)
	require.NoError(t, db.Create(&models.Workflow{
		ID:              "wf-idle",
		Name:            "idle-wf",
		Type:            string(types.WorkflowTypeBatch),
		Status:          string(types.PipelineGroupStatusIdle),
		PipelinesConfig: `[]`,
		CreatedAt:       time.Now(),
	}).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/workflows/:id/pipelines", h.AddPipelineToWorkflow)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-idle/pipelines",
		strings.NewReader(`{"id":"p1","name":"a","input":{"type":"sql"},"stages":[],"outputs":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "AddPipeline on idle workflow must succeed")
}
