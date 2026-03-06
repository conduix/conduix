package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/pipeline-core/pkg/stream"
	"github.com/conduix/conduix/shared/types"
)

// StageSchemaResponse 개별 Stage 스키마 응답
type StageSchemaResponse struct {
	Type         string `json:"type"`
	DisplayName  string `json:"display_name"`
	PluginImage  string `json:"plugin_image,omitempty"`
	ConfigSchema any    `json:"config_schema"`
	UISchema     any    `json:"ui_schema,omitempty"`
}

// StageHandler Stage 스키마 관련 핸들러
type StageHandler struct {
	db     *database.DB
	logger *slog.Logger
}

// NewStageHandler 새 StageHandler 생성
func NewStageHandler(db *database.DB) *StageHandler {
	return &StageHandler{
		db:     db,
		logger: slog.Default(),
	}
}

// BuiltinStageInfo 빌트인 Stage 정보
type BuiltinStageInfo struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// PluginStageInfo 플러그인 Stage 정보
type PluginStageInfo struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	PluginName  string `json:"plugin_name"`
	PluginImage string `json:"plugin_image"`
}

// AllStagesResponse 모든 Stage 목록 응답
type AllStagesResponse struct {
	Builtin []BuiltinStageInfo `json:"builtin"`
	Plugins []PluginStageInfo  `json:"plugins"`
}

// ListAllStages GET /api/v1/stages
// @Summary 모든 Stage 목록 조회 (빌트인 + 플러그인)
// @Tags stages
// @Accept json
// @Produce json
// @Success 200 {object} types.APIResponse[AllStagesResponse]
// @Router /stages [get]
func (h *StageHandler) ListAllStages(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	// 빌트인 Stage 목록
	builtinSchemas := stream.StageRegistry.All()
	builtinStages := make([]BuiltinStageInfo, 0, len(builtinSchemas))
	for _, schema := range builtinSchemas {
		builtinStages = append(builtinStages, BuiltinStageInfo{
			Type:        schema.Type,
			DisplayName: schema.DisplayName,
			Category:    string(schema.Category),
			Description: schema.Description,
		})
	}

	// 플러그인 Stage 목록
	var pluginStages []PluginStageInfo
	var dbStages []models.PluginStage

	if h.db != nil {
		if err := h.db.Preload("Plugin").Where("1=1").Find(&dbStages).Error; err != nil {
			h.logger.Warn("Failed to fetch plugin stages", "request_id", requestID, "error", err)
		} else {
			for _, dbStage := range dbStages {
				pluginName := ""
				pluginImage := ""
				if dbStage.Plugin != nil {
					pluginName = dbStage.Plugin.Name
					pluginImage = dbStage.Plugin.Image
				}
				pluginStages = append(pluginStages, PluginStageInfo{
					Type:        dbStage.StageType,
					DisplayName: dbStage.DisplayName,
					Category:    dbStage.Category,
					Description: dbStage.Description,
					PluginName:  pluginName,
					PluginImage: pluginImage,
				})
			}
		}
	}

	if pluginStages == nil {
		pluginStages = []PluginStageInfo{}
	}

	response := AllStagesResponse{
		Builtin: builtinStages,
		Plugins: pluginStages,
	}

	middleware.SuccessResponse(c, response)
}

// GetAllSchemas 모든 Stage 스키마 조회 (빌트인만)
// GET /api/v1/stages/schemas
func (h *StageHandler) GetAllSchemas(c *gin.Context) {
	schemas := stream.StageRegistry.All()
	c.JSON(http.StatusOK, schemas)
}

// GetSchema 특정 Stage 스키마 조회 (빌트인만)
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

// GetStageSchema GET /api/v1/stages/:type/schema
// @Summary 특정 Stage의 JSON Schema 조회 (빌트인 + 플러그인)
// @Tags stages
// @Accept json
// @Produce json
// @Param type path string true "Stage Type"
// @Success 200 {object} types.APIResponse[StageSchemaResponse]
// @Router /stages/{type}/schema [get]
func (h *StageHandler) GetStageSchema(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	stageType := c.Param("type")

	// 1. 빌트인 Stage에서 찾기
	if schema, ok := stream.StageRegistry.Get(stageType); ok {
		// 빌트인 Stage의 Fields를 JSON Schema 형태로 변환
		configSchema := convertFieldsToJSONSchema(schema.Fields)

		response := StageSchemaResponse{
			Type:         schema.Type,
			DisplayName:  schema.DisplayName,
			ConfigSchema: configSchema,
		}
		middleware.SuccessResponse(c, response)
		return
	}

	// 2. 플러그인 Stage에서 찾기
	if h.db != nil {
		var pluginStage models.PluginStage
		if err := h.db.Preload("Plugin").First(&pluginStage, "stage_type = ?", stageType).Error; err == nil {
			var configSchema any
			var uiSchema any

			if pluginStage.ConfigSchema != "" {
				if err := json.Unmarshal([]byte(pluginStage.ConfigSchema), &configSchema); err != nil {
					h.logger.Warn("Failed to parse config schema", "request_id", requestID, "error", err)
				}
			}
			if pluginStage.UISchema != "" {
				if err := json.Unmarshal([]byte(pluginStage.UISchema), &uiSchema); err != nil {
					h.logger.Warn("Failed to parse ui schema", "request_id", requestID, "error", err)
				}
			}

			pluginImage := ""
			if pluginStage.Plugin != nil {
				pluginImage = pluginStage.Plugin.Image
			}

			response := StageSchemaResponse{
				Type:         pluginStage.StageType,
				DisplayName:  pluginStage.DisplayName,
				PluginImage:  pluginImage,
				ConfigSchema: configSchema,
				UISchema:     uiSchema,
			}
			middleware.SuccessResponse(c, response)
			return
		}
	}

	// 찾지 못함
	middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Stage type not found: "+stageType)
}

// convertFieldsToJSONSchema 빌트인 Stage 필드를 JSON Schema로 변환
func convertFieldsToJSONSchema(fields []types.StageFieldSchema) map[string]any {
	properties := make(map[string]any)
	required := []string{}

	for _, field := range fields {
		prop := map[string]any{
			"title": field.DisplayName,
		}

		if field.Description != "" {
			prop["description"] = field.Description
		}
		if field.Default != nil {
			prop["default"] = field.Default
		}

		// 타입 매핑
		switch field.Type {
		case types.FieldTypeString, types.FieldTypeCode, types.FieldTypeSecret:
			prop["type"] = "string"
		case types.FieldTypeNumber:
			prop["type"] = "number"
		case types.FieldTypeInteger:
			prop["type"] = "integer"
		case types.FieldTypeBoolean:
			prop["type"] = "boolean"
		case types.FieldTypeArray:
			prop["type"] = "array"
			if field.ItemSchema != nil {
				prop["items"] = convertFieldToJSONSchemaItem(*field.ItemSchema)
			} else {
				prop["items"] = map[string]any{"type": "string"}
			}
		case types.FieldTypeObject, types.FieldTypeJSON:
			prop["type"] = "object"
			if len(field.Fields) > 0 {
				prop["properties"] = convertFieldsToJSONSchema(field.Fields)["properties"]
			}
		case types.FieldTypeEnum:
			prop["type"] = "string"
			if len(field.Options) > 0 {
				enumValues := make([]string, 0, len(field.Options))
				for _, opt := range field.Options {
					enumValues = append(enumValues, opt.Value)
				}
				prop["enum"] = enumValues
			}
		case types.FieldTypeKeyValue:
			prop["type"] = "object"
			prop["additionalProperties"] = map[string]any{"type": "string"}
		case types.FieldTypeDuration:
			prop["type"] = "string"
			prop["pattern"] = "^[0-9]+(ns|us|ms|s|m|h)$"
		default:
			prop["type"] = "string"
		}

		properties[field.Name] = prop

		if field.Required {
			required = append(required, field.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// convertFieldToJSONSchemaItem 배열 아이템 필드를 JSON Schema로 변환
func convertFieldToJSONSchemaItem(field types.StageFieldSchema) map[string]any {
	item := map[string]any{}

	switch field.Type {
	case types.FieldTypeString:
		item["type"] = "string"
	case types.FieldTypeNumber:
		item["type"] = "number"
	case types.FieldTypeInteger:
		item["type"] = "integer"
	case types.FieldTypeBoolean:
		item["type"] = "boolean"
	case types.FieldTypeObject:
		item["type"] = "object"
	default:
		item["type"] = "string"
	}

	return item
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
