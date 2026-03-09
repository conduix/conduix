package handlers

import (
	"fmt"
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
	ClusterName   string                       `json:"cluster_name,omitempty"`
}

// ListAgents 에이전트 목록 조회 (Redis 하트비트 기반)
// @Summary 에이전트 목록 조회
// @Tags agents
// @Accept json
// @Produce json
// @Param cluster_id query string false "Filter by cluster ID"
// @Success 200 {object} gin.H{success=bool,data=[]AgentWithHeartbeat}
// @Router /agents [get]
func (h *AgentHandler) ListAgents(c *gin.Context) {
	// 클러스터 필터
	filterClusterID := c.Query("cluster_id")

	// Redis에서 모든 에이전트 하트비트 조회
	heartbeats, err := h.redis.GetAllAgentHeartbeats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to fetch agents: " + err.Error(),
		})
		return
	}

	// 클러스터 이름 캐시 (DB 조회 최소화)
	clusterNames := make(map[string]string)

	// DB에서 agent 등록 시간 조회 (uptime 계산용)
	agentIDs := make([]string, 0, len(heartbeats))
	for id := range heartbeats {
		agentIDs = append(agentIDs, id)
	}
	registeredAtMap := make(map[string]time.Time)
	if len(agentIDs) > 0 {
		var dbAgents []models.Agent
		h.db.Select("id, registered_at").Where("id IN ?", agentIDs).Find(&dbAgents)
		for _, a := range dbAgents {
			registeredAtMap[a.ID] = a.RegisteredAt
		}
	}

	now := time.Now()
	agentsWithHeartbeat := make([]AgentWithHeartbeat, 0, len(heartbeats))

	for agentID, heartbeat := range heartbeats {
		// 클러스터 필터링
		if filterClusterID != "" && heartbeat.ClusterID != filterClusterID {
			continue
		}

		// 하트비트가 30초 이내인 에이전트만 표시 (online)
		// 30초 초과 에이전트는 하트비트 키가 TTL로 자동 만료됨
		status := "online"
		if now.Sub(heartbeat.Timestamp) > 30*time.Second {
			status = "offline"
		}

		agent := AgentWithHeartbeat{
			Agent: models.Agent{
				ID:            agentID,
				Hostname:      heartbeat.Hostname,
				Status:        status,
				LastHeartbeat: &heartbeat.Timestamp,
				RegisteredAt:  registeredAtMap[agentID], // DB에서 조회한 실제 등록 시간
				ClusterID:     heartbeat.ClusterID,
			},
			CPUUsage:      heartbeat.CPUUsage,
			MemoryUsage:   heartbeat.MemoryUsage,
			DiskUsage:     heartbeat.DiskUsage,
			Pipelines:     heartbeat.Pipelines,
			PipelineStats: heartbeat.PipelineStats,
			RunningExecs:  heartbeat.RunningExecs,
		}

		// 클러스터 이름 조회
		if heartbeat.ClusterID != "" {
			if clusterName, ok := clusterNames[heartbeat.ClusterID]; ok {
				agent.ClusterName = clusterName
			} else {
				var cluster models.Cluster
				if err := h.db.First(&cluster, "id = ?", heartbeat.ClusterID).Error; err == nil {
					clusterNames[heartbeat.ClusterID] = cluster.Name
					agent.ClusterName = cluster.Name
				}
			}
		}

		// Uptime 계산 (DB 등록 시간 기준)
		if regAt, ok := registeredAtMap[agentID]; ok && regAt.Before(now) {
			agent.Uptime = formatDuration(now.Sub(regAt))
		}

		agentsWithHeartbeat = append(agentsWithHeartbeat, agent)
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
	return fmt.Sprintf("%02d", value)
}

// RegisterAgentRequest 에이전트 등록 요청
type RegisterAgentRequest struct {
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname"`
	IPAddress string   `json:"ip_address,omitempty"`
	Version   string   `json:"version,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	ClusterID string   `json:"cluster_id,omitempty"`
}

// RegisterAgent 에이전트 등록 (인증 불필요 - 클러스터 내부 통신)
// @Summary 에이전트 등록
// @Tags agents
// @Accept json
// @Produce json
// @Param request body RegisterAgentRequest true "Agent Info"
// @Success 200 {object} gin.H{success=bool}
// @Router /agents/register [post]
func (h *AgentHandler) RegisterAgent(c *gin.Context) {
	var req RegisterAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.ID == "" || req.Hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "ID and Hostname are required",
		})
		return
	}

	now := time.Now()
	labelsJSON := ""
	if len(req.Labels) > 0 {
		// Simple JSON array encoding
		labelsJSON = "["
		for i, l := range req.Labels {
			if i > 0 {
				labelsJSON += ","
			}
			labelsJSON += `"` + l + `"`
		}
		labelsJSON += "]"
	}

	// ClusterID가 있으면 해당 Cluster가 존재하는지 확인하고, 없으면 자동 생성
	if req.ClusterID != "" {
		var cluster models.Cluster
		if err := h.db.First(&cluster, "id = ?", req.ClusterID).Error; err != nil {
			// Cluster가 없으면 자동 생성
			cluster = models.Cluster{
				ID:          req.ClusterID,
				Name:        req.ClusterID, // 기본 이름은 ID와 동일
				Description: "Auto-created cluster for agent: " + req.Hostname,
				Status:      "active",
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if createErr := h.db.Create(&cluster).Error; createErr != nil {
				// 동시 생성으로 인한 중복 에러는 무시 (이미 존재하는 경우)
				// MySQL의 경우 duplicate key error, 이 경우 계속 진행
				_ = createErr
			}
		}
	}

	agent := models.Agent{
		ID:            req.ID,
		Hostname:      req.Hostname,
		IPAddress:     req.IPAddress,
		Status:        "online",
		LastHeartbeat: &now,
		RegisteredAt:  now,
		Version:       req.Version,
		Labels:        labelsJSON,
		ClusterID:     req.ClusterID,
	}

	// Upsert: 존재하면 업데이트, 없으면 생성
	result := h.db.Where("id = ?", req.ID).FirstOrCreate(&agent)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to register agent: " + result.Error.Error(),
		})
		return
	}

	// 이미 존재하는 경우 업데이트
	if result.RowsAffected == 0 {
		h.db.Model(&agent).Updates(map[string]any{
			"hostname":       req.Hostname,
			"ip_address":     req.IPAddress,
			"status":         "online",
			"last_heartbeat": now,
			"version":        req.Version,
			"labels":         labelsJSON,
			"cluster_id":     req.ClusterID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Agent registered successfully",
	})
}

// HeartbeatRequest 하트비트 요청
type HeartbeatRequest struct {
	CPUUsage      float64                      `json:"cpu_usage"`
	MemoryUsage   float64                      `json:"memory_usage"`
	DiskUsage     float64                      `json:"disk_usage"`
	Pipelines     []string                     `json:"pipelines"`
	PipelineStats []types.PipelineStatShort    `json:"pipeline_stats,omitempty"`
	RunningExecs  []types.RunningExecutionInfo `json:"running_execs,omitempty"`
}

// Heartbeat 에이전트 하트비트 수신 (인증 불필요 - 클러스터 내부 통신)
// @Summary 에이전트 하트비트
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body HeartbeatRequest true "Heartbeat Info"
// @Success 200 {object} gin.H{success=bool}
// @Router /agents/{id}/heartbeat [post]
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	id := c.Param("id")

	var req HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request: " + err.Error(),
		})
		return
	}

	now := time.Now()

	// DB에서 에이전트 업데이트
	result := h.db.Model(&models.Agent{}).Where("id = ?", id).Updates(map[string]any{
		"status":         "online",
		"last_heartbeat": now,
	})

	if result.RowsAffected == 0 {
		// 에이전트가 없으면 자동 등록
		hostname, _ := c.GetQuery("hostname")
		if hostname == "" {
			hostname = id
		}
		agent := models.Agent{
			ID:            id,
			Hostname:      hostname,
			Status:        "online",
			LastHeartbeat: &now,
			RegisteredAt:  now,
		}
		h.db.Create(&agent)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}
