package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

const defaultMaxDataTypeDepth = 10

// getMaxDataTypeDepth 환경변수에서 최대 depth 제한 조회
func getMaxDataTypeDepth() int {
	if envVal := os.Getenv("MAX_DATATYPE_DEPTH"); envVal != "" {
		if depth, err := strconv.Atoi(envVal); err == nil && depth > 0 {
			return depth
		}
	}
	return defaultMaxDataTypeDepth
}

// DataTypeHandler 데이터 유형 핸들러
type DataTypeHandler struct {
	db *database.DB
}

// NewDataTypeHandler 새 핸들러 생성
func NewDataTypeHandler(db *database.DB) *DataTypeHandler {
	return &DataTypeHandler{db: db}
}

// getDataTypeDepth 특정 데이터타입의 depth 계산 (부모 체인 따라가며)
func (h *DataTypeHandler) getDataTypeDepth(dataTypeID string) int {
	depth := 0
	currentID := dataTypeID
	for currentID != "" {
		var dt models.DataType
		if err := h.db.First(&dt, "id = ?", currentID).Error; err != nil {
			break
		}
		depth++
		if dt.ParentID != nil {
			currentID = *dt.ParentID
		} else {
			break
		}
	}
	return depth
}

// ListDataTypes 데이터 유형 목록 조회
// @Summary 데이터 유형 목록 조회
// @Tags DataTypes
// @Produce json
// @Param project_id query string false "프로젝트 ID 필터"
// @Param category query string false "카테고리 필터"
// @Success 200 {object} types.APIResponse{data=[]models.DataType}
// @Router /data-types [get]
func (h *DataTypeHandler) ListDataTypes(c *gin.Context) {
	projectID := c.Query("project_id")
	category := c.Query("category")

	var dataTypes []models.DataType
	query := h.db.Preload("Preworks").Preload("Project").Preload("Parent")

	// 프로젝트 ID 필터 (필수)
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}

	if category != "" {
		query = query.Where("category = ?", category)
	}

	if err := query.Order("name ASC").Find(&dataTypes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형 목록 조회 실패", "Failed to load data types"),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.DataType]{
		Success: true,
		Data:    dataTypes,
	})
}

// GetDataType 데이터 유형 상세 조회
// @Summary 데이터 유형 상세 조회
// @Tags DataTypes
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Success 200 {object} types.APIResponse{data=models.DataType}
// @Router /data-types/{id} [get]
func (h *DataTypeHandler) GetDataType(c *gin.Context) {
	id := c.Param("id")

	var dataType models.DataType
	if err := h.db.Preload("Preworks").Preload("Parent").First(&dataType, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형을 찾을 수 없습니다", "Data type not found"),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.DataType]{
		Success: true,
		Data:    dataType,
	})
}

// CreateDataTypeRequest 데이터 유형 생성 요청
type CreateDataTypeRequest struct {
	ProjectID      string                 `json:"project_id" binding:"required"` // 프로젝트 ID (필수)
	ParentID       *string                `json:"parent_id,omitempty"`           // 부모 데이터타입 ID (종속관계)
	Name           string                 `json:"name" binding:"required"`
	DisplayName    string                 `json:"display_name" binding:"required"`
	Description    string                 `json:"description,omitempty"`
	Category       string                 `json:"category,omitempty"`
	DeleteStrategy *types.DeleteStrategy  `json:"delete_strategy,omitempty"`
	IDFields       []string               `json:"id_fields,omitempty"` // 복합키 필드 (예: ["board_id", "post_id"])
	Schema         *types.DataTypeSchema  `json:"schema,omitempty"`
	Storage        *types.DataTypeStorage `json:"storage,omitempty"`
	Preworks       []PreworkRequest       `json:"preworks,omitempty"`
}

// UpdateDataTypeRequest 데이터 유형 수정 요청 (ProjectID 불필요)
type UpdateDataTypeRequest struct {
	ParentID       *string                `json:"parent_id,omitempty"`
	Name           string                 `json:"name,omitempty"`
	DisplayName    string                 `json:"display_name,omitempty"`
	Description    string                 `json:"description,omitempty"`
	Category       string                 `json:"category,omitempty"`
	DeleteStrategy *types.DeleteStrategy  `json:"delete_strategy,omitempty"`
	IDFields       []string               `json:"id_fields,omitempty"`
	Schema         *types.DataTypeSchema  `json:"schema,omitempty"`
	Storage        *types.DataTypeStorage `json:"storage,omitempty"`
}

// PreworkRequest 사전작업 요청
type PreworkRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description string         `json:"description,omitempty"`
	Type        string         `json:"type" binding:"required"`  // sql, http, elasticsearch, s3, script
	Phase       string         `json:"phase" binding:"required"` // data_type, pipeline, manual
	Order       int            `json:"order"`
	Config      map[string]any `json:"config" binding:"required"`
}

// getErrorMessage 언어 설정에 따른 에러 메시지 반환
func getErrorMessage(c *gin.Context, ko, en string) string {
	lang := c.GetHeader("Accept-Language")
	if lang == "" {
		return ko
	}
	// "en", "en-US", "en-GB" 등 영어 로케일 확인
	if len(lang) >= 2 && lang[:2] == "en" {
		return en
	}
	return ko
}

// CreateDataType 데이터 유형 생성
// @Summary 데이터 유형 생성
// @Tags DataTypes
// @Accept json
// @Produce json
// @Param body body CreateDataTypeRequest true "데이터 유형 정보"
// @Success 201 {object} types.APIResponse{data=models.DataType}
// @Router /data-types [post]
func (h *DataTypeHandler) CreateDataType(c *gin.Context) {
	var req CreateDataTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "잘못된 요청: "+err.Error(), "Invalid request: "+err.Error()),
		})
		return
	}

	// 프로젝트 존재 여부 확인 (ID 또는 Alias)
	var project models.Project
	if err := h.db.Where("id = ? OR alias = ?", req.ProjectID, req.ProjectID).First(&project).Error; err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "프로젝트를 찾을 수 없습니다", "Project not found"),
		})
		return
	}

	// 부모 데이터타입 존재 여부 확인 (종속관계)
	if req.ParentID != nil && *req.ParentID != "" {
		var parentDataType models.DataType
		if err := h.db.First(&parentDataType, "id = ? AND project_id = ?", *req.ParentID, project.ID).Error; err != nil {
			c.JSON(http.StatusBadRequest, types.APIResponse[any]{
				Success: false,
				Error:   getErrorMessage(c, "부모 데이터타입을 찾을 수 없습니다", "Parent data type not found"),
			})
			return
		}

		// depth 제한 검사 (새 데이터타입의 depth = 부모 depth + 1)
		maxDepth := getMaxDataTypeDepth()
		parentDepth := h.getDataTypeDepth(*req.ParentID)
		if parentDepth+1 > maxDepth {
			c.JSON(http.StatusBadRequest, types.APIResponse[any]{
				Success: false,
				Error:   getErrorMessage(c, "최대 계층 깊이를 초과했습니다 (최대: "+strconv.Itoa(maxDepth)+")", "Maximum hierarchy depth exceeded (max: "+strconv.Itoa(maxDepth)+")"),
			})
			return
		}
	}

	// JSON 직렬화
	deleteStrategyJSON := ""
	if req.DeleteStrategy != nil {
		if b, err := json.Marshal(req.DeleteStrategy); err == nil {
			deleteStrategyJSON = string(b)
		}
	}

	idFieldsJSON := ""
	if len(req.IDFields) > 0 {
		if b, err := json.Marshal(req.IDFields); err == nil {
			idFieldsJSON = string(b)
		}
	}

	schemaJSON := ""
	if req.Schema != nil {
		if b, err := json.Marshal(req.Schema); err == nil {
			schemaJSON = string(b)
		}
	}

	storageJSON := ""
	if req.Storage != nil {
		if b, err := json.Marshal(req.Storage); err == nil {
			storageJSON = string(b)
		}
	}

	// 사용자 ID 추출
	userID := ""
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			userID = u.ID
		}
	}

	dataType := models.DataType{
		ID:             uuid.New().String(),
		ProjectID:      project.ID, // alias가 아닌 실제 프로젝트 ID 사용
		ParentID:       req.ParentID,
		Name:           req.Name,
		DisplayName:    req.DisplayName,
		Description:    req.Description,
		Category:       req.Category,
		DeleteStrategy: deleteStrategyJSON,
		IDFields:       idFieldsJSON,
		Schema:         schemaJSON,
		Storage:        storageJSON,
		CreatedBy:      userID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 트랜잭션 시작
	tx := h.db.Begin()

	if err := tx.Create(&dataType).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형 생성 실패: "+err.Error(), "Failed to create data type: "+err.Error()),
		})
		return
	}

	// 사전작업 생성
	for i, pw := range req.Preworks {
		configJSON, _ := json.Marshal(pw.Config)

		prework := models.DataTypePrework{
			ID:          uuid.New().String(),
			DataTypeID:  dataType.ID,
			Name:        pw.Name,
			Description: pw.Description,
			Type:        pw.Type,
			Phase:       pw.Phase,
			Order:       pw.Order,
			Config:      string(configJSON),
			Status:      "pending",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		// Order가 0이면 순서대로 설정
		if prework.Order == 0 {
			prework.Order = i + 1
		}

		if err := tx.Create(&prework).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
				Success: false,
				Error:   getErrorMessage(c, "사전작업 생성 실패: "+err.Error(), "Failed to create prework: "+err.Error()),
			})
			return
		}
	}

	tx.Commit()

	// 결과 조회
	h.db.Preload("Preworks").Preload("Parent").First(&dataType, "id = ?", dataType.ID)

	c.JSON(http.StatusCreated, types.APIResponse[models.DataType]{
		Success: true,
		Data:    dataType,
	})
}

// UpdateDataType 데이터 유형 수정
// @Summary 데이터 유형 수정
// @Tags DataTypes
// @Accept json
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Param body body CreateDataTypeRequest true "데이터 유형 정보"
// @Success 200 {object} types.APIResponse{data=models.DataType}
// @Router /data-types/{id} [put]
func (h *DataTypeHandler) UpdateDataType(c *gin.Context) {
	id := c.Param("id")

	var dataType models.DataType
	if err := h.db.First(&dataType, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형을 찾을 수 없습니다", "Data type not found"),
		})
		return
	}

	var req UpdateDataTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "잘못된 요청: "+err.Error(), "Invalid request: "+err.Error()),
		})
		return
	}

	// 부모 변경 시 depth 제한 검사
	if req.ParentID != nil && *req.ParentID != "" {
		maxDepth := getMaxDataTypeDepth()
		parentDepth := h.getDataTypeDepth(*req.ParentID)
		if parentDepth+1 > maxDepth {
			c.JSON(http.StatusBadRequest, types.APIResponse[any]{
				Success: false,
				Error:   getErrorMessage(c, "최대 계층 깊이를 초과했습니다 (최대: "+strconv.Itoa(maxDepth)+")", "Maximum hierarchy depth exceeded (max: "+strconv.Itoa(maxDepth)+")"),
			})
			return
		}
	}

	// 업데이트 (값이 있는 필드만)
	updates := map[string]any{
		"updated_at": time.Now(),
	}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.DisplayName != "" {
		updates["display_name"] = req.DisplayName
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Category != "" {
		updates["category"] = req.Category
	}
	// parent_id는 null로 설정할 수 있으므로 항상 업데이트
	updates["parent_id"] = req.ParentID

	if req.DeleteStrategy != nil {
		if b, err := json.Marshal(req.DeleteStrategy); err == nil {
			updates["delete_strategy"] = string(b)
		}
	}

	if len(req.IDFields) > 0 {
		if b, err := json.Marshal(req.IDFields); err == nil {
			updates["id_fields"] = string(b)
		}
	}

	if req.Schema != nil {
		if b, err := json.Marshal(req.Schema); err == nil {
			updates["schema"] = string(b)
		}
	}

	if req.Storage != nil {
		if b, err := json.Marshal(req.Storage); err == nil {
			updates["storage"] = string(b)
		}
	}

	if err := h.db.Model(&dataType).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형 수정 실패", "Failed to update data type"),
		})
		return
	}

	// 결과 조회
	h.db.Preload("Preworks").Preload("Parent").First(&dataType, "id = ?", id)

	c.JSON(http.StatusOK, types.APIResponse[models.DataType]{
		Success: true,
		Data:    dataType,
	})
}

// DeleteDataType 데이터 유형 삭제
// @Summary 데이터 유형 삭제
// @Tags DataTypes
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Success 200 {object} types.APIResponse
// @Router /data-types/{id} [delete]
func (h *DataTypeHandler) DeleteDataType(c *gin.Context) {
	id := c.Param("id")

	// 참조하는 파이프라인이 있는지 확인
	var pipelineCount int64
	h.db.Model(&models.Pipeline{}).Where("data_type_id = ?", id).Count(&pipelineCount)
	if pipelineCount > 0 {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "이 데이터 유형을 사용하는 파이프라인이 있어 삭제할 수 없습니다", "Cannot delete: pipelines are using this data type"),
		})
		return
	}

	tx := h.db.Begin()

	// 사전작업 삭제 (hard delete)
	if err := tx.Unscoped().Where("data_type_id = ?", id).Delete(&models.DataTypePrework{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "사전작업 삭제 실패", "Failed to delete prework"),
		})
		return
	}

	// 데이터 유형 삭제 (hard delete - 이름 재사용 가능하도록)
	if err := tx.Unscoped().Delete(&models.DataType{}, "id = ?", id).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형 삭제 실패", "Failed to delete data type"),
		})
		return
	}

	tx.Commit()

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
	})
}

// ExecutePrework 사전작업 실행
// @Summary 사전작업 실행
// @Tags DataTypes
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Param preworkId path string true "사전작업 ID"
// @Success 200 {object} types.APIResponse{data=models.DataTypePrework}
// @Router /data-types/{id}/preworks/{preworkId}/execute [post]
func (h *DataTypeHandler) ExecutePrework(c *gin.Context) {
	dataTypeID := c.Param("id")
	preworkID := c.Param("preworkId")

	var prework models.DataTypePrework
	if err := h.db.First(&prework, "id = ? AND data_type_id = ?", preworkID, dataTypeID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "사전작업을 찾을 수 없습니다", "Prework not found"),
		})
		return
	}

	// 실행 중 상태로 변경
	now := time.Now()
	h.db.Model(&prework).Updates(map[string]any{
		"status":     "running",
		"updated_at": now,
	})

	// 사용자 ID 추출
	userID := ""
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			userID = u.ID
		}
	}

	// TODO: 실제 사전작업 실행 로직 (타입별로 분기)
	// 현재는 성공으로 표시
	h.db.Model(&prework).Updates(map[string]any{
		"status":      "completed",
		"executed_at": now,
		"executed_by": userID,
		"updated_at":  now,
	})

	h.db.First(&prework, "id = ?", preworkID)

	c.JSON(http.StatusOK, types.APIResponse[models.DataTypePrework]{
		Success: true,
		Data:    prework,
	})
}

// AddPrework 사전작업 추가
// @Summary 사전작업 추가
// @Tags DataTypes
// @Accept json
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Param body body PreworkRequest true "사전작업 정보"
// @Success 201 {object} types.APIResponse{data=models.DataTypePrework}
// @Router /data-types/{id}/preworks [post]
func (h *DataTypeHandler) AddPrework(c *gin.Context) {
	dataTypeID := c.Param("id")

	// 데이터 유형 존재 확인
	var dataType models.DataType
	if err := h.db.First(&dataType, "id = ?", dataTypeID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "데이터 유형을 찾을 수 없습니다", "Data type not found"),
		})
		return
	}

	var req PreworkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "잘못된 요청: "+err.Error(), "Invalid request: "+err.Error()),
		})
		return
	}

	configJSON, _ := json.Marshal(req.Config)

	prework := models.DataTypePrework{
		ID:          uuid.New().String(),
		DataTypeID:  dataTypeID,
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Phase:       req.Phase,
		Order:       req.Order,
		Config:      string(configJSON),
		Status:      "pending",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.db.Create(&prework).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "사전작업 생성 실패", "Failed to create prework"),
		})
		return
	}

	c.JSON(http.StatusCreated, types.APIResponse[models.DataTypePrework]{
		Success: true,
		Data:    prework,
	})
}

// DeletePrework 사전작업 삭제
// @Summary 사전작업 삭제
// @Tags DataTypes
// @Produce json
// @Param id path string true "데이터 유형 ID"
// @Param preworkId path string true "사전작업 ID"
// @Success 200 {object} types.APIResponse
// @Router /data-types/{id}/preworks/{preworkId} [delete]
func (h *DataTypeHandler) DeletePrework(c *gin.Context) {
	dataTypeID := c.Param("id")
	preworkID := c.Param("preworkId")

	if err := h.db.Delete(&models.DataTypePrework{}, "id = ? AND data_type_id = ?", preworkID, dataTypeID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "사전작업 삭제 실패", "Failed to delete prework"),
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
	})
}

// ListDeleteStrategyPresets 삭제 전략 프리셋 목록
// @Summary 삭제 전략 프리셋 목록 조회
// @Tags DataTypes
// @Produce json
// @Success 200 {object} types.APIResponse{data=[]models.DeleteStrategyPreset}
// @Router /delete-strategy-presets [get]
func (h *DataTypeHandler) ListDeleteStrategyPresets(c *gin.Context) {
	var presets []models.DeleteStrategyPreset
	if err := h.db.Order("is_default DESC, name ASC").Find(&presets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   getErrorMessage(c, "프리셋 목록 조회 실패", "Failed to load presets"),
		})
		return
	}

	// 시스템 프리셋이 없으면 기본값 삽입
	if len(presets) == 0 {
		h.initDefaultPresets()
		h.db.Find(&presets)
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.DeleteStrategyPreset]{
		Success: true,
		Data:    presets,
	})
}

// initDefaultPresets 기본 프리셋 초기화
func (h *DataTypeHandler) initDefaultPresets() {
	for _, preset := range types.DefaultDeleteStrategyPresets {
		strategyJSON, _ := json.Marshal(preset.Strategy)

		dbPreset := models.DeleteStrategyPreset{
			ID:          preset.ID,
			Name:        preset.ID,
			DisplayName: preset.Name,
			Description: preset.Description,
			Strategy:    string(strategyJSON),
			IsDefault:   preset.IsDefault,
			IsSystem:    true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		h.db.FirstOrCreate(&dbPreset, "id = ?", preset.ID)
	}
}

// GetCategories 데이터 유형 카테고리 목록
// @Summary 데이터 유형 카테고리 목록 조회
// @Tags DataTypes
// @Produce json
// @Success 200 {object} types.APIResponse{data=[]string}
// @Router /data-types/categories [get]
func (h *DataTypeHandler) GetCategories(c *gin.Context) {
	categories := []map[string]string{
		{"id": "master", "name": "마스터 데이터", "description": "사용자, 상품 등 기준 정보"},
		{"id": "transaction", "name": "거래 데이터", "description": "주문, 결제 등 트랜잭션"},
		{"id": "log", "name": "로그 데이터", "description": "이벤트 로그, 접근 로그 등"},
		{"id": "metric", "name": "메트릭 데이터", "description": "모니터링, 통계 데이터"},
		{"id": "reference", "name": "참조 데이터", "description": "코드, 설정 등 참조 정보"},
	}

	c.JSON(http.StatusOK, types.APIResponse[[]map[string]string]{
		Success: true,
		Data:    categories,
	})
}
