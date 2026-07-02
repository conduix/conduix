package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-agent/internal/agent"
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
