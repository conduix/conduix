package handlers

import (
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

	// 각 클러스터별 Agent 수 조회
	responses := make([]ClusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		var agentCount int64
		var onlineAgentCount int64

		h.db.Model(&models.Agent{}).Where("cluster_id = ?", cluster.ID).Count(&agentCount)
		h.db.Model(&models.Agent{}).Where("cluster_id = ? AND status = ?", cluster.ID, "online").Count(&onlineAgentCount)

		responses = append(responses, ClusterResponse{
			Cluster:          cluster,
			AgentCount:       int(agentCount),
			OnlineAgentCount: int(onlineAgentCount),
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

	// Agent 수 조회
	var agentCount int64
	var onlineAgentCount int64
	h.db.Model(&models.Agent{}).Where("cluster_id = ?", cluster.ID).Count(&agentCount)
	h.db.Model(&models.Agent{}).Where("cluster_id = ? AND status = ?", cluster.ID, "online").Count(&onlineAgentCount)

	response := ClusterResponse{
		Cluster:          cluster,
		AgentCount:       int(agentCount),
		OnlineAgentCount: int(onlineAgentCount),
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
