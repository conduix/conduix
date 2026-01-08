package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// AgentHandler 에이전트 핸들러
type AgentHandler struct {
	db    *database.DB
	redis *services.RedisService
}

// NewAgentHandler 에이전트 핸들러 생성
func NewAgentHandler(db *database.DB, redis *services.RedisService) *AgentHandler {
	return &AgentHandler{
		db:    db,
		redis: redis,
	}
}

// AgentWithHeartbeat 에이전트 + 하트비트 정보
type AgentWithHeartbeat struct {
	models.Agent
	CPUUsage      float64                      `json:"cpu_usage"`
	MemoryUsage   float64                      `json:"memory_usage"`
	DiskUsage     float64                      `json:"disk_usage"`
	Pipelines     []string                     `json:"pipelines"`
	PipelineStats []types.PipelineStatShort    `json:"pipeline_stats,omitempty"`
	RunningExecs  []types.RunningExecutionInfo `json:"running_execs,omitempty"`
	Uptime        string                       `json:"uptime,omitempty"`
}

// ListAgents 에이전트 목록 조회
// @Summary 에이전트 목록 조회
// @Tags agents
// @Accept json
// @Produce json
// @Success 200 {object} gin.H{success=bool,data=[]AgentWithHeartbeat}
// @Router /agents [get]
func (h *AgentHandler) ListAgents(c *gin.Context) {
	var agents []models.Agent

	result := h.db.Order("registered_at DESC").Find(&agents)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to fetch agents",
		})
		return
	}

	// 각 에이전트의 하트비트 정보 조회
	agentsWithHeartbeat := make([]AgentWithHeartbeat, len(agents))
	now := time.Now()

	for i, agent := range agents {
		agentsWithHeartbeat[i] = AgentWithHeartbeat{
			Agent: agent,
		}

		// Redis에서 하트비트 정보 조회
		heartbeat, err := h.redis.GetAgentHeartbeat(agent.ID)
		if err == nil && heartbeat != nil {
			agentsWithHeartbeat[i].CPUUsage = heartbeat.CPUUsage
			agentsWithHeartbeat[i].MemoryUsage = heartbeat.MemoryUsage
			agentsWithHeartbeat[i].DiskUsage = heartbeat.DiskUsage
			agentsWithHeartbeat[i].Pipelines = heartbeat.Pipelines
			agentsWithHeartbeat[i].PipelineStats = heartbeat.PipelineStats
			agentsWithHeartbeat[i].RunningExecs = heartbeat.RunningExecs
		}

		// Uptime 계산
		if agent.RegisteredAt.Before(now) {
			agentsWithHeartbeat[i].Uptime = formatDuration(now.Sub(agent.RegisteredAt))
		}

		// Status 업데이트 (하트비트 기반)
		if agent.LastHeartbeat != nil {
			if now.Sub(*agent.LastHeartbeat) > 30*time.Second {
				agentsWithHeartbeat[i].Status = "offline"
			} else {
				agentsWithHeartbeat[i].Status = "online"
			}
		} else {
			agentsWithHeartbeat[i].Status = "unknown"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    agentsWithHeartbeat,
	})
}

// GetAgent 에이전트 상세 조회
// @Summary 에이전트 상세 조회
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} gin.H{success=bool,data=AgentWithHeartbeat}
// @Router /agents/{id} [get]
func (h *AgentHandler) GetAgent(c *gin.Context) {
	id := c.Param("id")

	var agent models.Agent
	result := h.db.First(&agent, "id = ?", id)
	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Agent not found",
		})
		return
	}

	agentWithHeartbeat := AgentWithHeartbeat{
		Agent: agent,
	}

	// Redis에서 하트비트 정보 조회
	heartbeat, err := h.redis.GetAgentHeartbeat(agent.ID)
	if err == nil && heartbeat != nil {
		agentWithHeartbeat.CPUUsage = heartbeat.CPUUsage
		agentWithHeartbeat.MemoryUsage = heartbeat.MemoryUsage
		agentWithHeartbeat.DiskUsage = heartbeat.DiskUsage
		agentWithHeartbeat.Pipelines = heartbeat.Pipelines
		agentWithHeartbeat.PipelineStats = heartbeat.PipelineStats
		agentWithHeartbeat.RunningExecs = heartbeat.RunningExecs
	}

	// Uptime 계산
	now := time.Now()
	if agent.RegisteredAt.Before(now) {
		agentWithHeartbeat.Uptime = formatDuration(now.Sub(agent.RegisteredAt))
	}

	// Status 업데이트
	if agent.LastHeartbeat != nil {
		if now.Sub(*agent.LastHeartbeat) > 30*time.Second {
			agentWithHeartbeat.Status = "offline"
		} else {
			agentWithHeartbeat.Status = "online"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    agentWithHeartbeat,
	})
}

// formatDuration 시간 포맷팅
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return formatInt(days) + "d " + formatInt(hours) + "h"
	}
	if hours > 0 {
		return formatInt(hours) + "h " + formatInt(minutes) + "m"
	}
	return formatInt(minutes) + "m"
}

func formatInt(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
