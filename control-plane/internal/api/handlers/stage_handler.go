package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-core/pkg/stream"
	"github.com/conduix/conduix/shared/types"
)

// StageHandler Stage 스키마 관련 핸들러
type StageHandler struct{}

// NewStageHandler 새 StageHandler 생성
func NewStageHandler() *StageHandler {
	return &StageHandler{}
}

// GetAllSchemas 모든 Stage 스키마 조회
// GET /api/v1/stages/schemas
func (h *StageHandler) GetAllSchemas(c *gin.Context) {
	schemas := stream.StageRegistry.All()
	c.JSON(http.StatusOK, schemas)
}

// GetSchema 특정 Stage 스키마 조회
// GET /api/v1/stages/schemas/:type
func (h *StageHandler) GetSchema(c *gin.Context) {
	stageType := c.Param("type")

	schema, ok := stream.StageRegistry.Get(stageType)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Stage type not found: " + stageType,
		})
		return
	}

	c.JSON(http.StatusOK, schema)
}

// GetSchemasByCategory 카테고리별 Stage 스키마 조회
// GET /api/v1/stages/schemas/category/:category
func (h *StageHandler) GetSchemasByCategory(c *gin.Context) {
	categoryStr := c.Param("category")
	category := types.StageCategory(categoryStr)

	// 유효한 카테고리 확인
	validCategories := map[types.StageCategory]bool{
		types.CategoryTransform:  true,
		types.CategoryValidation: true,
		types.CategoryOutput:     true,
		types.CategoryControl:    true,
	}

	if !validCategories[category] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":            "Invalid category: " + categoryStr,
			"valid_categories": []string{"transform", "validation", "output", "control"},
		})
		return
	}

	schemas := stream.StageRegistry.AllByCategory(category)
	c.JSON(http.StatusOK, schemas)
}

// GetCategories 사용 가능한 카테고리 목록 조회
// GET /api/v1/stages/categories
func (h *StageHandler) GetCategories(c *gin.Context) {
	categories := []map[string]string{
		{
			"value":       string(types.CategoryTransform),
			"label":       "Transform",
			"description": "데이터 변환 (filter, remap, drop 등)",
		},
		{
			"value":       string(types.CategoryValidation),
			"label":       "Validation",
			"description": "데이터 검증 (validate, contract)",
		},
		{
			"value":       string(types.CategoryOutput),
			"label":       "Output",
			"description": "데이터 출력 (SQL, Elasticsearch, Kafka 등)",
		},
		{
			"value":       string(types.CategoryControl),
			"label":       "Control",
			"description": "흐름 제어 (throttle, route, sample)",
		},
	}
	c.JSON(http.StatusOK, categories)
}

// GetFieldTypes 사용 가능한 필드 타입 목록 조회
// GET /api/v1/stages/field-types
func (h *StageHandler) GetFieldTypes(c *gin.Context) {
	fieldTypes := []map[string]any{
		{"value": string(types.FieldTypeString), "label": "String", "description": "텍스트 입력"},
		{"value": string(types.FieldTypeNumber), "label": "Number", "description": "숫자 입력"},
		{"value": string(types.FieldTypeInteger), "label": "Integer", "description": "정수 입력"},
		{"value": string(types.FieldTypeBoolean), "label": "Boolean", "description": "체크박스/스위치"},
		{"value": string(types.FieldTypeEnum), "label": "Enum", "description": "드롭다운 선택"},
		{"value": string(types.FieldTypeArray), "label": "Array", "description": "배열 (태그 입력 등)"},
		{"value": string(types.FieldTypeObject), "label": "Object", "description": "중첩 객체"},
		{"value": string(types.FieldTypeJSON), "label": "JSON", "description": "JSON 에디터"},
		{"value": string(types.FieldTypeCode), "label": "Code", "description": "코드 에디터 (Monaco)"},
		{"value": string(types.FieldTypeKeyValue), "label": "Key-Value", "description": "키-값 쌍 에디터"},
		{"value": string(types.FieldTypeDuration), "label": "Duration", "description": "시간 입력 (예: 30s, 5m)"},
		{"value": string(types.FieldTypeSecret), "label": "Secret", "description": "비밀번호 입력"},
	}
	c.JSON(http.StatusOK, fieldTypes)
}
