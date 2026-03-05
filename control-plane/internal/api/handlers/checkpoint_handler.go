package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// CheckpointHandler 체크포인트 핸들러
type CheckpointHandler struct {
	db *database.DB
}

// NewCheckpointHandler 새 핸들러 생성
func NewCheckpointHandler(db *database.DB) *CheckpointHandler {
	return &CheckpointHandler{db: db}
}

// CheckpointResponse 체크포인트 응답
type CheckpointResponse struct {
	ID           string    `json:"id"`
	WorkflowID   string    `json:"workflow_id"`
	PipelineID   string    `json:"pipeline_id"`
	PipelineName string    `json:"pipeline_name"`
	InputType    string    `json:"input_type"`  // Input 타입 (하위호환성: source_type도 지원)
	SourceType   string    `json:"source_type"` // Deprecated: input_type을 사용하세요
	PartitionKey string    `json:"partition_key"`
	OffsetValue  string    `json:"offset_value"`
	OffsetType   string    `json:"offset_type"`
	RecordCount  int64     `json:"record_count"`
	Metadata     string    `json:"metadata,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ResetCheckpointRequest 체크포인트 리셋 요청
type ResetCheckpointRequest struct {
	// 리셋 모드: "specific_time", "beginning", "specific_offset"
	Mode string `json:"mode" binding:"required,oneof=specific_time beginning specific_offset"`

	// specific_time 모드: RFC3339 형식 타임스탬프
	Timestamp string `json:"timestamp,omitempty"`

	// specific_offset 모드: 오프셋 값
	OffsetValue string `json:"offset_value,omitempty"`

	// 특정 파티션만 리셋 (비어있으면 전체)
	PartitionKeys []string `json:"partition_keys,omitempty"`
}

// ListCheckpoints 파이프라인의 체크포인트 목록 조회
// GET /api/v1/pipelines/:id/checkpoints
func (h *CheckpointHandler) ListCheckpoints(c *gin.Context) {
	pipelineID := c.Param("id")

	var checkpoints []models.InputCheckpoint
	result := h.db.Where("pipeline_id = ?", pipelineID).
		Order("partition_key ASC").
		Find(&checkpoints)

	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.InputCheckpoint]{
		Success: true,
		Data:    checkpoints,
	})
}

// GetCheckpoint 특정 체크포인트 조회
// GET /api/v1/pipelines/:id/checkpoints/:partitionKey
func (h *CheckpointHandler) GetCheckpoint(c *gin.Context) {
	pipelineID := c.Param("id")
	partitionKey := c.Param("partitionKey")

	var checkpoint models.InputCheckpoint
	result := h.db.Where("pipeline_id = ? AND partition_key = ?", pipelineID, partitionKey).First(&checkpoint)

	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Checkpoint not found")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.InputCheckpoint]{
		Success: true,
		Data:    checkpoint,
	})
}

// ResetCheckpoints 체크포인트 리셋
// PUT /api/v1/pipelines/:id/checkpoints/reset
func (h *CheckpointHandler) ResetCheckpoints(c *gin.Context) {
	pipelineID := c.Param("id")

	var req ResetCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 파이프라인 존재 확인 및 워크플로우 ID 조회
	var workflowID string
	// pipelines_config에서 파이프라인 찾기
	var workflows []models.Workflow
	h.db.Find(&workflows)

	for _, w := range workflows {
		if containsPipelineID(w.PipelinesConfig, pipelineID) {
			workflowID = w.ID
			break
		}
	}

	// 기존 체크포인트 조회
	var checkpoints []models.InputCheckpoint
	query := h.db.Where("pipeline_id = ?", pipelineID)

	// 특정 파티션만 리셋하는 경우
	if len(req.PartitionKeys) > 0 {
		query = query.Where("partition_key IN ?", req.PartitionKeys)
	}

	if err := query.Find(&checkpoints).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
		return
	}

	if len(checkpoints) == 0 {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "No checkpoints found")
		return
	}

	// 리셋 모드에 따라 처리
	switch req.Mode {
	case "beginning":
		// 체크포인트 삭제 (처음부터 시작)
		if err := query.Delete(&models.InputCheckpoint{}).Error; err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
			return
		}

	case "specific_time":
		// 특정 시간으로 리셋
		if req.Timestamp == "" {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "timestamp is required for specific_time mode")
			return
		}

		// 타임스탬프 형식 검증
		if _, err := time.Parse(time.RFC3339, req.Timestamp); err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "invalid timestamp format, use RFC3339")
			return
		}

		for _, cp := range checkpoints {
			if cp.OffsetType == "timestamp" {
				cp.OffsetValue = req.Timestamp
				cp.UpdatedAt = time.Now()
				h.db.Save(&cp)
			}
		}

	case "specific_offset":
		// 특정 오프셋으로 리셋
		if req.OffsetValue == "" {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "offset_value is required for specific_offset mode")
			return
		}

		for _, cp := range checkpoints {
			cp.OffsetValue = req.OffsetValue
			cp.UpdatedAt = time.Now()
			h.db.Save(&cp)
		}
	}

	// 업데이트된 체크포인트 반환
	var updatedCheckpoints []models.InputCheckpoint
	h.db.Where("pipeline_id = ?", pipelineID).Find(&updatedCheckpoints)

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"message":     "Checkpoints reset successfully",
			"mode":        req.Mode,
			"workflow_id": workflowID,
			"affected":    len(checkpoints),
			"checkpoints": updatedCheckpoints,
		},
	})
}

// DeleteCheckpoints 체크포인트 삭제
// DELETE /api/v1/pipelines/:id/checkpoints
func (h *CheckpointHandler) DeleteCheckpoints(c *gin.Context) {
	pipelineID := c.Param("id")

	// 특정 파티션만 삭제하는 경우
	partitionKey := c.Query("partition_key")

	query := h.db.Where("pipeline_id = ?", pipelineID)
	if partitionKey != "" {
		query = query.Where("partition_key = ?", partitionKey)
	}

	result := query.Delete(&models.InputCheckpoint{})
	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"message": "Checkpoints deleted successfully",
			"deleted": result.RowsAffected,
		},
	})
}

// UpdateCheckpoint Agent에서 체크포인트 업데이트 (내부 API)
// POST /api/v1/pipelines/:id/checkpoints
func (h *CheckpointHandler) UpdateCheckpoint(c *gin.Context) {
	pipelineID := c.Param("id")

	var req struct {
		WorkflowID   string `json:"workflow_id" binding:"required"`
		PipelineName string `json:"pipeline_name"`
		InputType    string `json:"input_type"`  // 새 필드명
		SourceType   string `json:"source_type"` // 하위호환성
		PartitionKey string `json:"partition_key" binding:"required"`
		OffsetValue  string `json:"offset_value" binding:"required"`
		OffsetType   string `json:"offset_type" binding:"required"`
		RecordCount  int64  `json:"record_count"`
		Metadata     string `json:"metadata,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 하위 호환성: input_type이 없으면 source_type 사용
	inputType := req.InputType
	if inputType == "" {
		inputType = req.SourceType
	}
	if inputType == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, "input_type or source_type is required")
		return
	}

	// Upsert: 있으면 업데이트, 없으면 생성
	var checkpoint models.InputCheckpoint
	result := h.db.Where("pipeline_id = ? AND partition_key = ?", pipelineID, req.PartitionKey).First(&checkpoint)

	if result.Error != nil {
		// 새로 생성
		checkpoint = models.InputCheckpoint{
			ID:           uuid.New().String(),
			WorkflowID:   req.WorkflowID,
			PipelineID:   pipelineID,
			PipelineName: req.PipelineName,
			InputType:    inputType,
			PartitionKey: req.PartitionKey,
			OffsetValue:  req.OffsetValue,
			OffsetType:   req.OffsetType,
			RecordCount:  req.RecordCount,
			Metadata:     req.Metadata,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := h.db.Create(&checkpoint).Error; err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
			return
		}
	} else {
		// 업데이트
		checkpoint.OffsetValue = req.OffsetValue
		checkpoint.RecordCount = req.RecordCount
		checkpoint.Metadata = req.Metadata
		checkpoint.UpdatedAt = time.Now()
		if err := h.db.Save(&checkpoint).Error; err != nil {
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, err.Error())
			return
		}
	}

	c.JSON(http.StatusOK, types.APIResponse[models.InputCheckpoint]{
		Success: true,
		Data:    checkpoint,
	})
}

// GetCheckpointsByWorkflow 워크플로우의 모든 체크포인트 조회
// GET /api/v1/workflows/:id/checkpoints
func (h *CheckpointHandler) GetCheckpointsByWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	var checkpoints []models.InputCheckpoint
	result := h.db.Where("workflow_id = ?", workflowID).
		Order("pipeline_name ASC, partition_key ASC").
		Find(&checkpoints)

	if result.Error != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, result.Error.Error())
		return
	}

	// 파이프라인별로 그룹화
	grouped := make(map[string][]models.InputCheckpoint)
	for _, cp := range checkpoints {
		key := cp.PipelineID
		if cp.PipelineName != "" {
			key = cp.PipelineName
		}
		grouped[key] = append(grouped[key], cp)
	}

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"checkpoints": checkpoints,
			"grouped":     grouped,
			"total":       len(checkpoints),
		},
	})
}

// containsPipelineID PipelinesConfig JSON에서 파이프라인 ID 확인
func containsPipelineID(config string, pipelineID string) bool {
	// 간단히 문자열 포함 여부 확인
	return len(config) > 0 && (config == pipelineID ||
		len(pipelineID) > 0 && (config[0:1] == "[" || config[0:1] == "{"))
}
