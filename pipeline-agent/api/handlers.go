package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-agent/internal/agent"
	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/shared/types"
)

// Handler API 핸들러
type Handler struct {
	agent *agent.Agent
}

// NewHandler 새 핸들러 생성
func NewHandler(a *agent.Agent) *Handler {
	return &Handler{agent: a}
}

// RegisterRoutes 라우트 등록
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// 헬스체크
		api.GET("/health", h.Health)

		// 에이전트 정보
		api.GET("/agent", h.GetAgentInfo)

		// 파이프라인 관리
		api.GET("/pipelines", h.ListPipelines)
		api.POST("/pipelines/:id/start", h.StartPipeline)
		api.POST("/pipelines/:id/stop", h.StopPipeline)
		api.POST("/pipelines/:id/pause", h.PausePipeline)
		api.POST("/pipelines/:id/resume", h.ResumePipeline)
		api.GET("/pipelines/:id/status", h.GetPipelineStatus)
	}
}

// Health 헬스체크
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, types.HealthStatus{
		Status: "healthy",
	})
}

// GetAgentInfo 에이전트 정보 조회
func (h *Handler) GetAgentInfo(c *gin.Context) {
	info := h.agent.GetStatus()
	c.JSON(http.StatusOK, types.APIResponse[*types.Agent]{
		Success: true,
		Data:    info,
	})
}

// ListPipelines 파이프라인 목록 조회
func (h *Handler) ListPipelines(c *gin.Context) {
	pipelines := h.agent.ListPipelines()

	items := make([]PipelineInfo, 0, len(pipelines))
	for _, p := range pipelines {
		items = append(items, PipelineInfo{
			ID:        p.ID,
			Name:      p.Config.Name,
			Status:    p.Status,
			StartTime: p.StartTime.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	c.JSON(http.StatusOK, types.APIResponse[[]PipelineInfo]{
		Success: true,
		Data:    items,
	})
}

// PipelineInfo 파이프라인 정보
type PipelineInfo struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Status    types.PipelineStatus `json:"status"`
	StartTime string               `json:"start_time"`
}

// StartPipelineRequest 파이프라인 시작 요청
type StartPipelineRequest struct {
	ConfigYAML string `json:"config_yaml" binding:"required"`
}

// StartPipeline 파이프라인 시작
func (h *Handler) StartPipeline(c *gin.Context) {
	pipelineID := c.Param("id")

	var req StartPipelineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	cfg, err := config.Parse([]byte(req.ConfigYAML))
	if err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   "Invalid config: " + err.Error(),
		})
		return
	}

	if err := h.agent.StartPipeline(pipelineID, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline started",
	})
}

// StopPipeline 파이프라인 중지
func (h *Handler) StopPipeline(c *gin.Context) {
	pipelineID := c.Param("id")

	if err := h.agent.StopPipeline(pipelineID); err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline stopped",
	})
}

// PausePipeline 파이프라인 일시중지
func (h *Handler) PausePipeline(c *gin.Context) {
	pipelineID := c.Param("id")

	if err := h.agent.PausePipeline(pipelineID); err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline paused",
	})
}

// ResumePipeline 파이프라인 재개
func (h *Handler) ResumePipeline(c *gin.Context) {
	pipelineID := c.Param("id")

	if err := h.agent.ResumePipeline(pipelineID); err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline resumed",
	})
}

// GetPipelineStatus 파이프라인 상태 조회
func (h *Handler) GetPipelineStatus(c *gin.Context) {
	pipelineID := c.Param("id")

	instance, err := h.agent.GetPipelineStatus(pipelineID)
	if err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	info := PipelineInfo{
		ID:        instance.ID,
		Name:      instance.Config.Name,
		Status:    instance.Status,
		StartTime: instance.StartTime.Format("2006-01-02T15:04:05Z07:00"),
	}

	c.JSON(http.StatusOK, types.APIResponse[PipelineInfo]{
		Success: true,
		Data:    info,
	})
}
