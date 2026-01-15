package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// PipelineLinkHandler handles pipeline link API
type PipelineLinkHandler struct {
	db          *database.DB
	linkService *services.PipelineLinkService
	logger      *slog.Logger
}

// NewPipelineLinkHandler creates a new handler
func NewPipelineLinkHandler(db *database.DB, linkService *services.PipelineLinkService) *PipelineLinkHandler {
	return &PipelineLinkHandler{
		db:          db,
		linkService: linkService,
		logger:      slog.Default(),
	}
}

// CreateLinkRequest is the request body for creating a link
type CreateLinkRequest struct {
	WorkflowID       string `json:"workflow_id" binding:"required"`
	ParentPipelineID string `json:"parent_pipeline_id" binding:"required"`
	ChildPipelineID  string `json:"child_pipeline_id" binding:"required"`
}

// CreateLink POST /api/v1/pipeline-links
// Links parent and child pipelines and creates a Kafka Topic
func (h *PipelineLinkHandler) CreateLink(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var req CreateLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.(string)
	}

	// Create link (also creates Kafka Topic)
	link, err := h.linkService.CreateLink(c.Request.Context(), req.WorkflowID, req.ParentPipelineID, req.ChildPipelineID, userIDStr)
	if err != nil {
		h.logger.Error("Failed to create pipeline link",
			"request_id", requestID,
			"workflow_id", req.WorkflowID,
			"parent", req.ParentPipelineID,
			"child", req.ChildPipelineID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	h.logger.Info("Pipeline link created",
		"request_id", requestID,
		"link_id", link.ID,
		"parent", req.ParentPipelineID,
		"child", req.ChildPipelineID,
		"topic", link.KafkaTopic)

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    link,
	})
}

// DeleteLink DELETE /api/v1/pipeline-links/:parent_id/:child_id
// Breaks pipeline link and deletes Kafka Topic
func (h *PipelineLinkHandler) DeleteLink(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	parentID := c.Param("parent_id")
	childID := c.Param("child_id")

	if parentID == "" || childID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "parent_id and child_id are required")
		return
	}

	// Delete link (also deletes Kafka Topic)
	if err := h.linkService.DeleteLink(c.Request.Context(), parentID, childID); err != nil {
		h.logger.Error("Failed to delete pipeline link",
			"request_id", requestID,
			"parent", parentID,
			"child", childID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	h.logger.Info("Pipeline link deleted",
		"request_id", requestID,
		"parent", parentID,
		"child", childID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pipeline link deleted successfully",
	})
}

// GetLinksByParent GET /api/v1/pipeline-links/parent/:parent_id
// Gets all links where the given pipeline is a parent
func (h *PipelineLinkHandler) GetLinksByParent(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	parentID := c.Param("parent_id")

	if parentID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "parent_id is required")
		return
	}

	links, err := h.linkService.GetLinksByParent(c.Request.Context(), parentID)
	if err != nil {
		h.logger.Error("Failed to get links by parent",
			"request_id", requestID,
			"parent_id", parentID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    links,
	})
}

// GetLinksByChild GET /api/v1/pipeline-links/child/:child_id
// Gets all links where the given pipeline is a child
func (h *PipelineLinkHandler) GetLinksByChild(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	childID := c.Param("child_id")

	if childID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "child_id is required")
		return
	}

	links, err := h.linkService.GetLinksByChild(c.Request.Context(), childID)
	if err != nil {
		h.logger.Error("Failed to get links by child",
			"request_id", requestID,
			"child_id", childID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    links,
	})
}

// GetLinksByWorkflow GET /api/v1/pipeline-links/workflow/:workflow_id
// Gets all links within a workflow
func (h *PipelineLinkHandler) GetLinksByWorkflow(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("workflow_id")

	if workflowID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "workflow_id is required")
		return
	}

	links, err := h.linkService.GetLinksByWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		h.logger.Error("Failed to get links by workflow",
			"request_id", requestID,
			"workflow_id", workflowID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    links,
	})
}

// GetLink GET /api/v1/pipeline-links/:parent_id/:child_id
// Gets a single link
func (h *PipelineLinkHandler) GetLink(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	parentID := c.Param("parent_id")
	childID := c.Param("child_id")

	if parentID == "" || childID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "parent_id and child_id are required")
		return
	}

	link, err := h.linkService.GetLink(c.Request.Context(), parentID, childID)
	if err != nil {
		h.logger.Error("Failed to get link",
			"request_id", requestID,
			"parent", parentID,
			"child", childID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	if link == nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Link not found")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    link,
	})
}

// ListAllLinks GET /api/v1/pipeline-links
// Lists all links
func (h *PipelineLinkHandler) ListAllLinks(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	var links []models.PipelineLink
	if err := h.db.Find(&links).Error; err != nil {
		h.logger.Error("Failed to list all links",
			"request_id", requestID,
			"error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    links,
	})
}
