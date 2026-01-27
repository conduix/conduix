package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-agent/internal/agent"
	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/shared/types"
)

// Version 버전 정보 (빌드 시 설정)
var Version = "dev"

// StartTime 시작 시간
var StartTime = time.Now()

// Handler API 핸들러
type Handler struct {
	agent *agent.Agent
}

// NewHandler 새 핸들러 생성
func NewHandler(a *agent.Agent) *Handler {
	return &Handler{agent: a}
}

// IndexInfo 인덱스 페이지 정보
type IndexInfo struct {
	Service      string            `json:"service"`
	Version      string            `json:"version"`
	Uptime       string            `json:"uptime"`
	UptimeSec    float64           `json:"uptime_seconds"`
	StartTime    string            `json:"start_time"`
	GoVersion    string            `json:"go_version"`
	NumGoroutine int               `json:"num_goroutine"`
	MemoryMB     float64           `json:"memory_mb"`
	AgentID      string            `json:"agent_id,omitempty"`
	Endpoints    map[string]string `json:"endpoints"`
}

// RegisterRoutes 라우트 등록
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// Index (서비스 정보)
	r.GET("/", h.Index)

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

		// 모니터링
		api.GET("/monitoring", h.GetAllMonitoring)
		api.GET("/monitoring/:executionId", h.GetExecutionMonitoring)
	}
}

// Index 서비스 정보 페이지
func (h *Handler) Index(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(StartTime)

	agentStatus := h.agent.GetStatus()
	agentID := ""
	if agentStatus != nil {
		agentID = agentStatus.ID
	}

	info := IndexInfo{
		Service:      "Conduix Pipeline Agent",
		Version:      Version,
		Uptime:       uptime.Round(time.Second).String(),
		UptimeSec:    uptime.Seconds(),
		StartTime:    StartTime.Format(time.RFC3339),
		GoVersion:    runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		MemoryMB:     float64(m.Alloc) / 1024 / 1024,
		AgentID:      agentID,
		Endpoints: map[string]string{
			"health":     "/api/v1/health",
			"agent":      "/api/v1/agent",
			"pipelines":  "/api/v1/pipelines",
			"monitoring": "/api/v1/monitoring",
		},
	}

	c.JSON(http.StatusOK, info)
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
			Error:   types.NewAPIError(types.ErrCodeBadRequest, err.Error()),
		})
		return
	}

	cfg, err := config.Parse([]byte(req.ConfigYAML))
	if err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   types.NewAPIError(types.ErrCodeValidationFailed, "Invalid config: "+err.Error()),
		})
		return
	}

	if err := h.agent.StartPipeline(pipelineID, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   types.NewAPIError(types.ErrCodeInternalError, err.Error()),
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
			Error:   types.NewAPIError(types.ErrCodeInternalError, err.Error()),
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
			Error:   types.NewAPIError(types.ErrCodeInternalError, err.Error()),
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
			Error:   types.NewAPIError(types.ErrCodeInternalError, err.Error()),
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
			Error:   types.NewAPIError(types.ErrCodeNotFound, err.Error()),
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

// GetAllMonitoring 모든 실행의 모니터링 정보 조회
func (h *Handler) GetAllMonitoring(c *gin.Context) {
	monitoring := h.agent.GetAllExecutionMonitoring()
	c.JSON(http.StatusOK, types.APIResponse[[]*types.ExecutionMonitoringInfo]{
		Success: true,
		Data:    monitoring,
	})
}

// GetExecutionMonitoring 특정 실행의 모니터링 정보 조회
func (h *Handler) GetExecutionMonitoring(c *gin.Context) {
	executionID := c.Param("executionId")

	monitoring := h.agent.GetExecutionMonitoring(executionID)
	if monitoring == nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   types.NewAPIError(types.ErrCodeNotFound, "Execution not found or not running"),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[*types.ExecutionMonitoringInfo]{
		Success: true,
		Data:    monitoring,
	})
}
