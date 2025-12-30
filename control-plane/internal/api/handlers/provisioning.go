package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// ProvisioningHandler 사전작업 API 핸들러
type ProvisioningHandler struct {
	db *database.DB
}

// NewProvisioningHandler 핸들러 생성
func NewProvisioningHandler(db *database.DB) *ProvisioningHandler {
	return &ProvisioningHandler{db: db}
}

// GetSinkRequirements GET /api/v1/provisioning/requirements
// 모든 저장소 타입의 요구사항 조회
func (h *ProvisioningHandler) GetSinkRequirements(c *gin.Context) {
	requirements := types.GetSinkRequirements()

	c.JSON(http.StatusOK, types.APIResponse[[]types.SinkRequirement]{
		Success: true,
		Data:    requirements,
	})
}

// GetSinkRequirement GET /api/v1/provisioning/requirements/:type
// 특정 저장소 타입의 요구사항 조회
func (h *ProvisioningHandler) GetSinkRequirement(c *gin.Context) {
	sinkType := types.SinkType(c.Param("type"))

	requirements := types.GetSinkRequirements()
	for _, req := range requirements {
		if req.Type == sinkType {
			c.JSON(http.StatusOK, types.APIResponse[types.SinkRequirement]{
				Success: true,
				Data:    req,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, types.APIResponse[any]{
		Success: false,
		Error:   "Sink type not found",
	})
}

// StartProvisioningRequest 사전작업 시작 요청
type StartProvisioningRequest struct {
	PipelineID  string                 `json:"pipeline_id" binding:"required"`
	SinkType    types.SinkType         `json:"sink_type" binding:"required"`
	SinkName    string                 `json:"sink_name" binding:"required"`
	Type        types.ProvisioningType `json:"type" binding:"required"`
	Config      map[string]any         `json:"config"`
	ExternalURL string                 `json:"external_url,omitempty"` // 외부 프로비저닝 페이지 URL
}

// StartProvisioning POST /api/v1/provisioning/start
// 사전작업 시작
func (h *ProvisioningHandler) StartProvisioning(c *gin.Context) {
	var req StartProvisioningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// 파이프라인 존재 확인
	var pipeline models.Pipeline
	if err := h.db.First(&pipeline, "id = ?", req.PipelineID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Pipeline not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	// Provisioning 레코드 생성
	provisioningID := uuid.New().String()
	callbackURL := c.Request.Host + "/api/v1/provisioning/callback/" + provisioningID

	provisioningReq := &types.ProvisioningRequest{
		ID:          provisioningID,
		PipelineID:  req.PipelineID,
		SinkType:    req.SinkType,
		SinkName:    req.SinkName,
		Type:        req.Type,
		Config:      req.Config,
		ExternalURL: req.ExternalURL,
		CallbackURL: callbackURL,
		RequestedBy: userIDStr,
		RequestedAt: time.Now(),
	}

	// DB에 Provisioning 요청 저장
	provisioningRecord := &models.ProvisioningRequest{
		ID:          provisioningReq.ID,
		PipelineID:  provisioningReq.PipelineID,
		SinkType:    string(provisioningReq.SinkType),
		SinkName:    provisioningReq.SinkName,
		Type:        string(provisioningReq.Type),
		ExternalURL: provisioningReq.ExternalURL,
		CallbackURL: callbackURL,
		Status:      string(types.ProvisioningStatusPending),
		RequestedBy: userIDStr,
		CreatedAt:   time.Now(),
	}

	if err := h.db.Create(provisioningRecord).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to create provisioning request",
		})
		return
	}

	// 응답 생성
	response := map[string]any{
		"id":           provisioningID,
		"pipeline_id":  req.PipelineID,
		"sink_type":    req.SinkType,
		"sink_name":    req.SinkName,
		"status":       types.ProvisioningStatusPending,
		"callback_url": callbackURL,
	}

	// 외부 프로비저닝인 경우 redirect URL 포함
	if req.Type == types.ProvisioningTypeExternal && req.ExternalURL != "" {
		redirectURL := req.ExternalURL +
			"?provisioning_id=" + provisioningID +
			"&pipeline_id=" + req.PipelineID +
			"&sink_name=" + req.SinkName +
			"&callback_url=" + callbackURL

		response["redirect_url"] = redirectURL
		response["message"] = "Please redirect to external setup page to complete provisioning"
	}

	c.JSON(http.StatusAccepted, types.APIResponse[map[string]any]{
		Success: true,
		Data:    response,
	})
}

// CompleteProvisioningRequest 사전작업 완료 콜백 요청
type CompleteProvisioningRequest struct {
	Status      types.ProvisioningStatus `json:"status" binding:"required"`
	TableName   string                   `json:"table_name,omitempty"`
	TopicName   string                   `json:"topic_name,omitempty"`
	IndexName   string                   `json:"index_name,omitempty"`
	BucketName  string                   `json:"bucket_name,omitempty"`
	FilePath    string                   `json:"file_path,omitempty"`
	APIEndpoint string                   `json:"api_endpoint,omitempty"`
	APIKey      string                   `json:"api_key,omitempty"`
	Metadata    map[string]any           `json:"metadata,omitempty"`
	Message     string                   `json:"message,omitempty"`
	ErrorDetail string                   `json:"error_detail,omitempty"`
	CompletedBy string                   `json:"completed_by,omitempty"`
}

// CompleteProvisioning POST /api/v1/provisioning/callback/:id
// 외부에서 사전작업 완료 후 콜백
func (h *ProvisioningHandler) CompleteProvisioning(c *gin.Context) {
	provisioningID := c.Param("id")

	var req CompleteProvisioningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Provisioning 요청 조회
	var provisioningRecord models.ProvisioningRequest
	if err := h.db.First(&provisioningRecord, "id = ?", provisioningID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Provisioning request not found",
		})
		return
	}

	// 이미 완료된 경우
	if provisioningRecord.Status == string(types.ProvisioningStatusCompleted) ||
		provisioningRecord.Status == string(types.ProvisioningStatusFailed) {
		c.JSON(http.StatusConflict, types.APIResponse[any]{
			Success: false,
			Error:   "Provisioning already completed",
		})
		return
	}

	// 결과 저장
	now := time.Now()
	resultRecord := &models.ProvisioningResult{
		ID:                uuid.New().String(),
		RequestID:         provisioningID,
		PipelineID:        provisioningRecord.PipelineID,
		SinkType:          provisioningRecord.SinkType,
		Status:            string(req.Status),
		ResultTableName:   req.TableName,
		ResultTopicName:   req.TopicName,
		ResultIndexName:   req.IndexName,
		ResultBucketName:  req.BucketName,
		ResultFilePath:    req.FilePath,
		ResultAPIEndpoint: req.APIEndpoint,
		ResultAPIKey:      req.APIKey,
		Message:           req.Message,
		ErrorDetail:       req.ErrorDetail,
		CompletedAt:       &now,
		CompletedBy:       req.CompletedBy,
		CreatedAt:         now,
	}

	if err := h.db.Create(resultRecord).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to save provisioning result",
		})
		return
	}

	// 요청 상태 업데이트
	provisioningRecord.Status = string(req.Status)
	provisioningRecord.UpdatedAt = now
	if err := h.db.Save(&provisioningRecord).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to update provisioning status",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Provisioning completed successfully",
		Data: map[string]any{
			"provisioning_id": provisioningID,
			"pipeline_id":     provisioningRecord.PipelineID,
			"status":          req.Status,
			"result_id":       resultRecord.ID,
		},
	})
}

// GetProvisioningStatus GET /api/v1/provisioning/status/:id
// 사전작업 상태 조회
func (h *ProvisioningHandler) GetProvisioningStatus(c *gin.Context) {
	provisioningID := c.Param("id")

	var provisioningRecord models.ProvisioningRequest
	if err := h.db.First(&provisioningRecord, "id = ?", provisioningID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Provisioning request not found",
		})
		return
	}

	// 결과 조회 (있는 경우)
	var result *models.ProvisioningResult
	if provisioningRecord.Status == string(types.ProvisioningStatusCompleted) ||
		provisioningRecord.Status == string(types.ProvisioningStatusFailed) {
		var resultRecord models.ProvisioningResult
		if err := h.db.First(&resultRecord, "request_id = ?", provisioningID).Error; err == nil {
			result = &resultRecord
		}
	}

	response := map[string]any{
		"id":          provisioningRecord.ID,
		"pipeline_id": provisioningRecord.PipelineID,
		"sink_type":   provisioningRecord.SinkType,
		"sink_name":   provisioningRecord.SinkName,
		"status":      provisioningRecord.Status,
		"created_at":  provisioningRecord.CreatedAt,
	}

	if result != nil {
		response["result"] = map[string]any{
			"table_name":   result.ResultTableName,
			"topic_name":   result.ResultTopicName,
			"index_name":   result.ResultIndexName,
			"bucket_name":  result.ResultBucketName,
			"file_path":    result.ResultFilePath,
			"api_endpoint": result.ResultAPIEndpoint,
			"message":      result.Message,
			"completed_at": result.CompletedAt,
			"completed_by": result.CompletedBy,
		}
	}

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data:    response,
	})
}

// ListPipelineProvisioning GET /api/v1/pipelines/:id/provisioning
// 파이프라인의 모든 사전작업 목록 조회
func (h *ProvisioningHandler) ListPipelineProvisioning(c *gin.Context) {
	pipelineID := c.Param("id")

	var records []models.ProvisioningRequest
	if err := h.db.Where("pipeline_id = ?", pipelineID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch provisioning records",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.ProvisioningRequest]{
		Success: true,
		Data:    records,
	})
}
