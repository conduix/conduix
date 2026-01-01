package handlers

import (
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

// PipelineHandler 파이프라인 핸들러
type PipelineHandler struct {
	db           *database.DB
	redisService *services.RedisService
}

// NewPipelineHandler 새 핸들러 생성
func NewPipelineHandler(db *database.DB, redisService *services.RedisService) *PipelineHandler {
	return &PipelineHandler{
		db:           db,
		redisService: redisService,
	}
}

// List 파이프라인 목록 조회
func (h *PipelineHandler) List(c *gin.Context) {
	var pipelines []models.Pipeline

	page := 1
	pageSize := 20

	var total int64
	h.db.Model(&models.Pipeline{}).Count(&total)

	result := h.db.Offset((page - 1) * pageSize).Limit(pageSize).Find(&pipelines)
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[types.PaginatedResponse[models.Pipeline]]{
		Success: true,
		Data: types.PaginatedResponse[models.Pipeline]{
			Items:      pipelines,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: int((total + int64(pageSize) - 1) / int64(pageSize)),
		},
	})
}

// Get 파이프라인 상세 조회
func (h *PipelineHandler) Get(c *gin.Context) {
	id := c.Param("id")

	var pipeline models.Pipeline
	result := h.db.First(&pipeline, "id = ?", id)
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.Pipeline]{
		Success: true,
		Data:    pipeline,
	})
}

// Create 파이프라인 생성
func (h *PipelineHandler) Create(c *gin.Context) {
	var req types.CreatePipelineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 사용자 ID 가져오기 (인증 미들웨어에서 설정)
	userID, _ := c.Get("user_id")

	pipeline := models.Pipeline{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		ConfigYAML:  req.ConfigYAML,
		CreatedBy:   userID.(string),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	result := h.db.Create(&pipeline)
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	c.JSON(http.StatusCreated, types.APIResponse[models.Pipeline]{
		Success: true,
		Data:    pipeline,
	})
}

// Update 파이프라인 수정
func (h *PipelineHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var pipeline models.Pipeline
	if err := h.db.First(&pipeline, "id = ?", id).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	var req types.UpdatePipelineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	if req.Name != "" {
		pipeline.Name = req.Name
	}
	if req.Description != "" {
		pipeline.Description = req.Description
	}
	if req.ConfigYAML != "" {
		pipeline.ConfigYAML = req.ConfigYAML
	}
	pipeline.UpdatedAt = time.Now()

	if err := h.db.Save(&pipeline).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.Pipeline]{
		Success: true,
		Data:    pipeline,
	})
}

// Delete 파이프라인 삭제
func (h *PipelineHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	result := h.db.Delete(&models.Pipeline{}, "id = ?", id)
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	if result.RowsAffected == 0 {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Pipeline not found")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Pipeline deleted",
	})
}

// NOTE: 개별 파이프라인 실행 제어(Start/Stop/Pause/Resume)는 지원하지 않음
// 파이프라인 실행 제어는 PipelineGroup 단위로만 가능
// /api/v1/groups/:id/start, stop, pause, resume 사용

// GetStatus 파이프라인 상태 조회
func (h *PipelineHandler) GetStatus(c *gin.Context) {
	id := c.Param("id")

	var run models.PipelineRun
	result := h.db.Where("pipeline_id = ?", id).Order("created_at DESC").First(&run)
	if result.Error != nil {
		c.JSON(http.StatusOK, types.APIResponse[any]{
			Success: true,
			Data: map[string]string{
				"status": "stopped",
			},
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.PipelineRun]{
		Success: true,
		Data:    run,
	})
}

// GetHistory 파이프라인 히스토리 조회
func (h *PipelineHandler) GetHistory(c *gin.Context) {
	id := c.Param("id")

	var runs []models.PipelineRun
	result := h.db.Where("pipeline_id = ?", id).Order("created_at DESC").Limit(50).Find(&runs)
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.PipelineRun]{
		Success: true,
		Data:    runs,
	})
}

// GetMetrics 파이프라인 메트릭 조회
func (h *PipelineHandler) GetMetrics(c *gin.Context) {
	id := c.Param("id")

	// Redis에서 실시간 메트릭 조회
	var metrics *types.PipelineMetrics
	var err error

	if h.redisService != nil && h.redisService.IsHealthy() {
		metrics, err = h.redisService.GetPipelineMetrics(id)
		if err != nil {
			// Redis 조회 실패 시 기본 메트릭 반환
			metrics = &types.PipelineMetrics{
				PipelineID:  id,
				LastUpdated: time.Now(),
			}
		}
	} else {
		// Redis 불가 시 기본 메트릭 반환
		metrics = &types.PipelineMetrics{
			PipelineID:  id,
			LastUpdated: time.Now(),
		}

		// DB에서 최근 run 정보로 메트릭 보완
		var run models.PipelineRun
		if err := h.db.Where("pipeline_id = ?", id).Order("created_at DESC").First(&run).Error; err == nil {
			metrics.EventsIn = run.ProcessedCount
			metrics.ErrorsTotal = run.ErrorCount
		}
	}

	c.JSON(http.StatusOK, types.APIResponse[types.PipelineMetrics]{
		Success: true,
		Data:    *metrics,
	})
}
