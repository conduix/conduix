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

// CustomStageInfo 커스텀(플러그인) Stage 정보. HasSchema 로 GUI 가 폼/JSON폴백을 가른다.
type CustomStageInfo struct {
	Type        string `json:"type"` // = plugin.Name (등록 시 이 이름이 곧 stage type)
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	HasSchema   bool   `json:"has_schema"` // config_schema 유무 — 없으면 프론트가 JSON 폴백
}

// AllStagesResponse 모든 Stage 목록 응답
type AllStagesResponse struct {
	Builtin []BuiltinStageInfo `json:"builtin"`
	Custom  []CustomStageInfo  `json:"custom"`
}

// ListAllStages GET /api/v1/stages
// @Summary 모든 Stage 목록 조회 (빌트인)
// @Tags stages
// @Accept json
// @Produce json
// @Success 200 {object} types.APIResponse[AllStagesResponse]
// @Router /stages [get]
func (h *StageHandler) ListAllStages(c *gin.Context) {
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

	response := AllStagesResponse{
		Builtin: builtinStages,
		Custom:  h.listCustomStages(),
	}

	middleware.SuccessResponse(c, response)
}

// listCustomStages 는 활성 플러그인을 커스텀 stage 목록으로 변환한다.
// config_schema 가 있으면 DisplayName/Category 를 스키마에서 채우고, 없어도 목록엔 포함한다
// (타입·설명은 보여야 한다). DB 오류 시 빈 목록(빌트인 목록은 계속 반환되게).
func (h *StageHandler) listCustomStages() []CustomStageInfo {
	var plugins []models.Plugin
	if err := h.db.Where("status = ? AND type = ?", "active", "native").Find(&plugins).Error; err != nil {
		h.logger.Error("failed to list custom stages", "error", err)
		return []CustomStageInfo{}
	}
	out := make([]CustomStageInfo, 0, len(plugins))
	for _, p := range plugins {
		info := CustomStageInfo{
			Type:        p.Name,
			DisplayName: p.Name,
			Description: p.Description,
		}
		if schema, ok := parsePluginSchema(p.ConfigSchema); ok {
			info.HasSchema = true
			if schema.DisplayName != "" {
				info.DisplayName = schema.DisplayName
			}
			info.Category = string(schema.Category)
			if info.Description == "" {
				info.Description = schema.Description
			}
		}
		out = append(out, info)
	}
	return out
}

// parsePluginSchema config_schema 문자열을 types.StageSchema 로 파싱한다.
// 비었거나 파싱 실패면 (zero, false) — 등록 시 검증되지만 방어적으로 처리.
func parsePluginSchema(raw string) (types.StageSchema, bool) {
	if raw == "" {
		return types.StageSchema{}, false
	}
	var schema types.StageSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return types.StageSchema{}, false
	}
	return schema, true
}

// GetAllSchemas 모든 Stage 스키마 조회 (빌트인 + 커스텀)
// GET /api/v1/stages/schemas — raw types.StageSchema[] 반환(secret/ShowWhen 보존).
func (h *StageHandler) GetAllSchemas(c *gin.Context) {
	schemas := stream.StageRegistry.All()
	schemas = append(schemas, h.customSchemas()...)
	c.JSON(http.StatusOK, schemas)
}

// customSchemas config_schema 가 등록된 활성 플러그인의 raw StageSchema 목록.
// 스키마 없는 플러그인은 여기 포함되지 않는다(GUI 는 목록 API 의 has_schema 로 JSON 폴백).
func (h *StageHandler) customSchemas() []types.StageSchema {
	var plugins []models.Plugin
	if err := h.db.Where("status = ? AND type = ? AND config_schema IS NOT NULL AND config_schema != ''",
		"active", "native").Find(&plugins).Error; err != nil {
		h.logger.Error("failed to load custom schemas", "error", err)
		return nil
	}
	out := make([]types.StageSchema, 0, len(plugins))
	for _, p := range plugins {
		if schema, ok := parsePluginSchema(p.ConfigSchema); ok {
			if schema.Type == "" {
				schema.Type = p.Name // 스키마에 type 누락 시 plugin 이름으로 보정
			}
			out = append(out, schema)
		}
	}
	return out
}

// GetSchema 특정 Stage 스키마 조회 (빌트인 + 커스텀)
// GET /api/v1/stages/schemas/:type — raw types.StageSchema 반환.
func (h *StageHandler) GetSchema(c *gin.Context) {
	stageType := c.Param("type")

	if schema, ok := stream.StageRegistry.Get(stageType); ok {
		c.JSON(http.StatusOK, schema)
		return
	}

	// 커스텀 플러그인 조회
	var plugin models.Plugin
	if err := h.db.Where("name = ? AND status = ?", stageType, "active").First(&plugin).Error; err == nil {
		if schema, ok := parsePluginSchema(plugin.ConfigSchema); ok {
			if schema.Type == "" {
				schema.Type = plugin.Name
			}
			c.JSON(http.StatusOK, schema)
			return
		}
		// 스키마 미등록 커스텀: 최소 정보만(404 아님) → 프론트 JSON 폴백
		c.JSON(http.StatusOK, types.StageSchema{
			Type:        plugin.Name,
			DisplayName: plugin.Name,
			Description: plugin.Description,
		})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{
		"error": "Stage type not found: " + stageType,
	})
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

	// 2. 커스텀 플러그인에서 찾기. config_schema 있으면 JSON Schema 로 변환, 없으면
	//    빈 스키마(프론트 JSON 폴백). 어느 쪽이든 404 는 아니다.
	var plugin models.Plugin
	if err := h.db.Where("name = ? AND status = ?", stageType, "active").First(&plugin).Error; err == nil {
		resp := StageSchemaResponse{Type: plugin.Name, DisplayName: plugin.Name}
		if schema, ok := parsePluginSchema(plugin.ConfigSchema); ok {
			if schema.DisplayName != "" {
				resp.DisplayName = schema.DisplayName
			}
			resp.ConfigSchema = convertFieldsToJSONSchema(schema.Fields)
		}
		middleware.SuccessResponse(c, resp)
		return
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
