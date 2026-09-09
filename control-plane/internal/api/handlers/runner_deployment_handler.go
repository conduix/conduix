package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// RunnerDeploymentRow 은 실행 중인 워크플로우 하나가 어느 노드에서 어떤 runner 버전으로
// 도는지를 나타낸다.
//
// 이 정보가 없으면 "빌드는 최신인데 노드는 옛 바이너리로 돈다" 는 상태를 알 수 없다.
// 실측: 코어 수정 후 이미지·agent·CP 를 다 재배포했는데도 pod 이 옛 rv- 로 돌았고,
// kubectl 로 initContainer 명령을 뜯어야 확인 가능했다.
type RunnerDeploymentRow struct {
	ExecutionID     string `json:"execution_id"`
	WorkflowID      string `json:"workflow_id"`
	WorkflowName    string `json:"workflow_name"`
	WorkflowType    string `json:"workflow_type"`
	AgentID         string `json:"agent_id,omitempty"`  // 실행 노드. 아직 보고 전이면 빈다.
	RunnerVersionID string `json:"runner_version_id"`   // 이 실행이 쓰는 바이너리 버전
	RunnerBuildNo   int    `json:"runner_build_number"` // 사람이 읽는 순번
	Stale           bool   `json:"stale"`               // 최신 ready 와 다르면 true
	Deploying       bool   `json:"deploying"`           // 실행은 시작됐으나 노드 미확정
	StartedAt       string `json:"started_at,omitempty"`
}

// RunnerDeploymentStatus 는 배포 현황 응답이다.
type RunnerDeploymentStatus struct {
	LatestReadyVersion string                `json:"latest_ready_version,omitempty"`
	LatestReadyBuildNo int                   `json:"latest_ready_build_number,omitempty"`
	BuildingVersion    string                `json:"building_version,omitempty"`
	Deployments        []RunnerDeploymentRow `json:"deployments"`
	StaleCount         int                   `json:"stale_count"`
	DeployingCount     int                   `json:"deploying_count"`
}

// GetRunnerDeployments GET /api/v1/runner/deployments
// 실행 중인 워크플로우별로 "어느 노드에서 어떤 runner 버전이 도는지" 와 그것이 최신인지를 준다.
func (h *RunnerHandler) GetRunnerDeployments(c *gin.Context) {
	var latestReady models.RunnerVersion
	hasReady := h.db.Select(models.RunnerVersionMetaColumns()).
		Where("status = ?", "ready").
		Order("build_number DESC").First(&latestReady).Error == nil

	var building models.RunnerVersion
	hasBuilding := h.db.Select(models.RunnerVersionMetaColumns()).
		Where("status IN ?", []string{"pending", "building"}).
		Order("build_number DESC").First(&building).Error == nil

	// 실행 중인 것만 본다. 끝난 실행의 버전은 배포 현황이 아니라 이력이다.
	var execs []models.WorkflowExecution
	if err := h.db.Where("status = ?", string(types.PipelineGroupStatusRunning)).
		Order("started_at DESC").Find(&execs).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError,
			types.ErrCodeInternalError, "failed to query running executions")
		return
	}

	// 워크플로우 이름·타입을 한 번에 읽는다(실행마다 조회하면 N+1).
	names := map[string]models.Workflow{}
	if len(execs) > 0 {
		ids := make([]string, 0, len(execs))
		for i := range execs {
			ids = append(ids, execs[i].WorkflowID)
		}
		var wfs []models.Workflow
		h.db.Where("id IN ?", ids).Find(&wfs)
		for i := range wfs {
			names[wfs[i].ID] = wfs[i]
		}
	}

	// runner_version_id 별 build_number 를 한 번에 읽는다.
	buildNos := map[string]int{}
	{
		rvIDs := make([]string, 0, len(execs))
		for i := range execs {
			if execs[i].RunnerVersionID != "" {
				rvIDs = append(rvIDs, execs[i].RunnerVersionID)
			}
		}
		if len(rvIDs) > 0 {
			var vs []models.RunnerVersion
			h.db.Select("id", "build_number").Where("id IN ?", rvIDs).Find(&vs)
			for i := range vs {
				buildNos[vs[i].ID] = vs[i].BuildNumber
			}
		}
	}

	resp := RunnerDeploymentStatus{Deployments: make([]RunnerDeploymentRow, 0, len(execs))}
	if hasReady {
		resp.LatestReadyVersion = latestReady.ID
		resp.LatestReadyBuildNo = latestReady.BuildNumber
	}
	if hasBuilding {
		resp.BuildingVersion = building.ID
	}

	for i := range execs {
		e := &execs[i]
		wf := names[e.WorkflowID]

		row := RunnerDeploymentRow{
			ExecutionID:     e.ID,
			WorkflowID:      e.WorkflowID,
			WorkflowName:    wf.Name,
			WorkflowType:    wf.Type,
			AgentID:         e.AgentID,
			RunnerVersionID: e.RunnerVersionID,
			RunnerBuildNo:   buildNos[e.RunnerVersionID],
		}
		if e.StartedAt.IsZero() {
			row.StartedAt = ""
		} else {
			row.StartedAt = e.StartedAt.Format("2006-01-02T15:04:05Z07:00")
		}

		// agent_id 는 모니터링 조회 시점에 백필된다. 아직 없으면 노드 확정 전 = 배포 중이다.
		// native stage 를 안 쓰는 실행은 runner_version_id 가 비므로 stale 판정 대상이 아니다.
		row.Deploying = e.AgentID == ""
		row.Stale = e.RunnerVersionID != "" && hasReady && e.RunnerVersionID != latestReady.ID

		if row.Stale {
			resp.StaleCount++
		}
		if row.Deploying {
			resp.DeployingCount++
		}
		resp.Deployments = append(resp.Deployments, row)
	}

	c.JSON(http.StatusOK, types.APIResponse[RunnerDeploymentStatus]{
		Success: true,
		Data:    resp,
	})
}
