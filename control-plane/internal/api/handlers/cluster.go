package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ClusterHandler 클러스터 API 핸들러
type ClusterHandler struct {
	db           *database.DB
	redisService *services.RedisService
	logger       *slog.Logger
}

// NewClusterHandler 클러스터 핸들러 생성
func NewClusterHandler(db *database.DB, redisService *services.RedisService) *ClusterHandler {
	return &ClusterHandler{
		db:           db,
		redisService: redisService,
		logger:       slog.Default(),
	}
}

// CreateClusterRequest 클러스터 생성 요청
type CreateClusterRequest struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description,omitempty"`
	APIServerURL string `json:"api_server_url,omitempty"`
	Region       string `json:"region,omitempty"`
}

// UpdateClusterRequest 클러스터 수정 요청
type UpdateClusterRequest struct {
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	APIServerURL string `json:"api_server_url,omitempty"`
	Region       string `json:"region,omitempty"`
	Status       string `json:"status,omitempty"` // active, inactive
}

// ClusterResponse 클러스터 응답 (Agent 수 포함)
type ClusterResponse struct {
	models.Cluster
	AgentCount       int `json:"agent_count"`
	OnlineAgentCount int `json:"online_agent_count"`
}

// ListClusters GET /api/v1/clusters
// @Summary 클러스터 목록 조회
// @Tags clusters
// @Accept json
// @Produce json
// @Param status query string false "Status filter (active, inactive)"
// @Param search query string false "Search by name"
// @Success 200 {object} types.APIResponse[[]ClusterResponse]
// @Router /clusters [get]
func (h *ClusterHandler) ListClusters(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var clusters []models.Cluster
	query := h.db.Model(&models.Cluster{})

	// 필터링
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if search := c.Query("search"); search != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// 정렬
	orderBy := c.DefaultQuery("order_by", "created_at")
	orderDir := c.DefaultQuery("order_dir", "desc")
	query = query.Order(orderBy + " " + orderDir)

	if err := query.Find(&clusters).Error; err != nil {
		h.logger.Error("Failed to fetch clusters", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to fetch clusters")
		return
	}

	// Redis에서 실제 활성 Agent 하트비트 조회 (실시간 데이터)
	heartbeats, err := h.redisService.GetAllAgentHeartbeats()
	if err != nil {
		h.logger.Warn("Failed to fetch agent heartbeats, falling back to DB", "request_id", requestID, "error", err)
		heartbeats = nil
	}

	// 클러스터별 Agent 수 계산 (Redis 하트비트 기반)
	clusterAgentCounts := make(map[string]int)
	clusterOnlineAgentCounts := make(map[string]int)
	now := time.Now()

	for _, heartbeat := range heartbeats {
		clusterID := heartbeat.ClusterID
		if clusterID == "" {
			clusterID = "default" // 클러스터 미지정 Agent는 default로 간주
		}
		clusterAgentCounts[clusterID]++
		// 30초 이내 하트비트가 있으면 online
		if now.Sub(heartbeat.Timestamp) <= 30*time.Second {
			clusterOnlineAgentCounts[clusterID]++
		}
	}

	// 응답 생성
	responses := make([]ClusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		responses = append(responses, ClusterResponse{
			Cluster:          cluster,
			AgentCount:       clusterAgentCounts[cluster.ID],
			OnlineAgentCount: clusterOnlineAgentCounts[cluster.ID],
		})
	}

	middleware.SuccessResponse(c, responses)
}

// GetCluster GET /api/v1/clusters/:id
// @Summary 클러스터 상세 조회
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Success 200 {object} types.APIResponse[ClusterResponse]
// @Router /clusters/{id} [get]
func (h *ClusterHandler) GetCluster(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	// Redis에서 실제 활성 Agent 하트비트 조회 (실시간 데이터)
	var agentCount int
	var onlineAgentCount int
	now := time.Now()

	heartbeats, err := h.redisService.GetAllAgentHeartbeats()
	if err == nil && heartbeats != nil {
		for _, heartbeat := range heartbeats {
			hbClusterID := heartbeat.ClusterID
			if hbClusterID == "" {
				hbClusterID = "default"
			}
			if hbClusterID == cluster.ID {
				agentCount++
				if now.Sub(heartbeat.Timestamp) <= 30*time.Second {
					onlineAgentCount++
				}
			}
		}
	}

	response := ClusterResponse{
		Cluster:          cluster,
		AgentCount:       agentCount,
		OnlineAgentCount: onlineAgentCount,
	}

	middleware.SuccessResponse(c, response)
}

// CreateCluster POST /api/v1/clusters
// @Summary 클러스터 생성
// @Tags clusters
// @Accept json
// @Produce json
// @Param request body CreateClusterRequest true "Cluster Info"
// @Success 201 {object} types.APIResponse[models.Cluster]
// @Router /clusters [post]
func (h *ClusterHandler) CreateCluster(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req CreateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// 이름 중복 확인
	var existingCount int64
	h.db.Model(&models.Cluster{}).Where("name = ?", req.Name).Count(&existingCount)
	if existingCount > 0 {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeDuplicateResource, "Cluster with this name already exists")
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	cluster := &models.Cluster{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Description:  req.Description,
		APIServerURL: req.APIServerURL,
		Region:       req.Region,
		Status:       "active",
		CreatedBy:    userIDStr,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := h.db.Create(cluster).Error; err != nil {
		h.logger.Error("Failed to create cluster", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create cluster")
		return
	}

	h.logger.Info("Cluster created", "request_id", requestID, "cluster_id", cluster.ID)
	c.JSON(http.StatusCreated, types.APIResponse[models.Cluster]{
		Success: true,
		Data:    *cluster,
	})
}

// UpdateCluster PUT /api/v1/clusters/:id
// @Summary 클러스터 수정
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Param request body UpdateClusterRequest true "Cluster Info"
// @Success 200 {object} types.APIResponse[models.Cluster]
// @Router /clusters/{id} [put]
func (h *ClusterHandler) UpdateCluster(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	var req UpdateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// 이름 변경 시 중복 확인
	if req.Name != "" && req.Name != cluster.Name {
		var existingCount int64
		h.db.Model(&models.Cluster{}).Where("name = ? AND id != ?", req.Name, clusterID).Count(&existingCount)
		if existingCount > 0 {
			middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeDuplicateResource, "Cluster with this name already exists")
			return
		}
		cluster.Name = req.Name
	}

	if req.Description != "" {
		cluster.Description = req.Description
	}
	if req.APIServerURL != "" {
		cluster.APIServerURL = req.APIServerURL
	}
	if req.Region != "" {
		cluster.Region = req.Region
	}
	if req.Status != "" {
		cluster.Status = req.Status
	}

	cluster.UpdatedAt = time.Now()

	if err := h.db.Save(&cluster).Error; err != nil {
		h.logger.Error("Failed to update cluster", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update cluster")
		return
	}

	h.logger.Info("Cluster updated", "request_id", requestID, "cluster_id", clusterID)
	middleware.SuccessResponse(c, cluster)
}

// DeleteCluster DELETE /api/v1/clusters/:id
// @Summary 클러스터 삭제
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Success 200 {object} types.APIResponse[any]
// @Router /clusters/{id} [delete]
func (h *ClusterHandler) DeleteCluster(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	// 연결된 Agent 확인
	var agentCount int64
	h.db.Model(&models.Agent{}).Where("cluster_id = ?", clusterID).Count(&agentCount)
	if agentCount > 0 {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeResourceInUse, "Cluster has connected agents")
		return
	}

	// 연결된 Workflow 확인
	var workflowCount int64
	h.db.Model(&models.Workflow{}).Where("cluster_id = ?", clusterID).Count(&workflowCount)
	if workflowCount > 0 {
		middleware.ErrorResponseWithCode(c, http.StatusConflict, types.ErrCodeResourceInUse, "Cluster has associated workflows")
		return
	}

	// Soft delete
	if err := h.db.Delete(&cluster).Error; err != nil {
		h.logger.Error("Failed to delete cluster", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to delete cluster")
		return
	}

	h.logger.Info("Cluster deleted", "request_id", requestID, "cluster_id", clusterID)
	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Cluster deleted successfully",
	})
}

// GetClusterAgents GET /api/v1/clusters/:id/agents
// @Summary 클러스터별 Agent 조회
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Success 200 {object} types.APIResponse[[]AgentWithHeartbeat]
// @Router /clusters/{id}/agents [get]
func (h *ClusterHandler) GetClusterAgents(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	// 클러스터 존재 확인
	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	// Redis에서 해당 클러스터의 Agent 하트비트 조회
	heartbeats, err := h.redisService.GetAllAgentHeartbeats()
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeExternalService, "Failed to fetch agent heartbeats")
		return
	}

	now := time.Now()
	agentsWithHeartbeat := make([]AgentWithHeartbeat, 0)

	for agentID, heartbeat := range heartbeats {
		// 클러스터 ID 필터링
		if heartbeat.ClusterID != clusterID {
			continue
		}

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
				RegisteredAt:  heartbeat.Timestamp,
				ClusterID:     heartbeat.ClusterID,
			},
			CPUUsage:      heartbeat.CPUUsage,
			MemoryUsage:   heartbeat.MemoryUsage,
			DiskUsage:     heartbeat.DiskUsage,
			Pipelines:     heartbeat.Pipelines,
			PipelineStats: heartbeat.PipelineStats,
			RunningExecs:  heartbeat.RunningExecs,
		}

		agentsWithHeartbeat = append(agentsWithHeartbeat, agent)
	}

	middleware.SuccessResponse(c, agentsWithHeartbeat)
}

// ScaleAgentsRequest Agent 스케일링 요청
type ScaleAgentsRequest struct {
	DesiredAgents int `json:"desired_agents" binding:"required,min=0,max=100"`
}

// UpdateAgentConfigRequest Agent 배포 설정 업데이트 요청
type UpdateAgentConfigRequest struct {
	DesiredAgents int                       `json:"desired_agents,omitempty"`
	AgentConfig   *types.ClusterAgentConfig `json:"agent_config,omitempty"`
}

// ScaleAgents POST /api/v1/clusters/:id/scale
// @Summary 클러스터 Agent 스케일링
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Param request body ScaleAgentsRequest true "Scale Request"
// @Success 200 {object} types.APIResponse[models.Cluster]
// @Router /clusters/{id}/scale [post]
func (h *ClusterHandler) ScaleAgents(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	var req ScaleAgentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// DesiredAgents 업데이트
	cluster.DesiredAgents = req.DesiredAgents
	cluster.UpdatedAt = time.Now()

	if err := h.db.Save(&cluster).Error; err != nil {
		h.logger.Error("Failed to update cluster", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to scale agents")
		return
	}

	// control-plane은 worker replica를 직접 조정하지 않는다(K8s 크레덴셜 미보유).
	// DesiredAgents는 "의도"로만 기록되고, 실제 replica는 각 cluster의 배포 차트
	// (Helm/ArgoCD)가 이 값을 반영해 맞춘다. worker는 배포 시점에 CLUSTER_ID로
	// 자기 그룹을 선언하고 control-plane에 접속한다(agent 방식).
	h.logger.Info("Cluster desired agents updated", "request_id", requestID, "cluster_id", clusterID, "desired_agents", req.DesiredAgents)
	middleware.SuccessResponse(c, cluster)
}

// UpdateAgentConfig PUT /api/v1/clusters/:id/agent-config
// @Summary 클러스터 Agent 배포 설정 업데이트
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Param request body UpdateAgentConfigRequest true "Agent Config"
// @Success 200 {object} types.APIResponse[models.Cluster]
// @Router /clusters/{id}/agent-config [put]
func (h *ClusterHandler) UpdateAgentConfig(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	var req UpdateAgentConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	// DesiredAgents 업데이트
	if req.DesiredAgents > 0 {
		cluster.DesiredAgents = req.DesiredAgents
	}

	// AgentConfig JSON으로 저장
	if req.AgentConfig != nil {
		configJSON, err := json.Marshal(req.AgentConfig)
		if err != nil {
			h.logger.Error("Failed to marshal agent config", "request_id", requestID, "error", err)
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, "Failed to process agent config")
			return
		}
		cluster.AgentConfig = string(configJSON)
	}

	cluster.UpdatedAt = time.Now()

	if err := h.db.Save(&cluster).Error; err != nil {
		h.logger.Error("Failed to update cluster", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to update agent config")
		return
	}

	// DesiredAgents/AgentConfig는 "의도"로만 기록된다. 실제 worker replica·배포 스펙은
	// 각 cluster의 배포 차트(Helm/ArgoCD)가 반영한다. control-plane은 K8s를 직접 만지지 않는다.
	h.logger.Info("Cluster agent config updated", "request_id", requestID, "cluster_id", clusterID)
	middleware.SuccessResponse(c, cluster)
}

// GetAgentConfig GET /api/v1/clusters/:id/agent-config
// @Summary 클러스터 Agent 배포 설정 조회
// @Tags clusters
// @Accept json
// @Produce json
// @Param id path string true "Cluster ID"
// @Success 200 {object} types.APIResponse[types.ClusterAgentConfig]
// @Router /clusters/{id}/agent-config [get]
func (h *ClusterHandler) GetAgentConfig(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	clusterID := c.Param("id")

	var cluster models.Cluster
	if err := h.db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		h.logger.Warn("Cluster not found", "request_id", requestID, "cluster_id", clusterID)
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Cluster not found")
		return
	}

	var config types.ClusterAgentConfig
	if cluster.AgentConfig != "" {
		if err := json.Unmarshal([]byte(cluster.AgentConfig), &config); err != nil {
			h.logger.Warn("Failed to parse agent config", "request_id", requestID, "error", err)
		}
	}

	// 응답에 DesiredAgents도 포함
	response := struct {
		DesiredAgents int                      `json:"desired_agents"`
		AgentConfig   types.ClusterAgentConfig `json:"agent_config"`
	}{
		DesiredAgents: cluster.DesiredAgents,
		AgentConfig:   config,
	}

	middleware.SuccessResponse(c, response)
}
