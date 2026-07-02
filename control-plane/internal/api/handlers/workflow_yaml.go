package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// WorkflowSpec은 워크플로우의 이식 가능한(portable) 정의다.
// 서버 관리 필드(id, status, timestamps, created_by)를 제외하므로,
// export한 YAML에서 project_id만 바꾸거나 비워두면 그대로 "템플릿"으로 재사용된다.
// json 태그를 그대로 사용해 web-ui가 쓰는 구조와 1:1 정합을 유지한다(YAML은 JSON 경유 변환).
type WorkflowSpec struct {
	ProjectID     string                  `json:"project_id,omitempty"`
	ClusterID     string                  `json:"cluster_id,omitempty"`
	Name          string                  `json:"name"`
	Slug          string                  `json:"slug,omitempty"`
	Description   string                  `json:"description,omitempty"`
	Type          types.PipelineGroupType `json:"type"`
	ExecutionMode types.ExecutionMode     `json:"execution_mode,omitempty"`
	Schedule      *types.ScheduleConfig   `json:"schedule,omitempty"`
	Pipelines     []types.GroupedPipeline `json:"pipelines,omitempty"`
	FailurePolicy *types.FailurePolicy    `json:"failure_policy,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
	Tags          []string                `json:"tags,omitempty"`
}

// ExportWorkflowYAML GET /api/v1/workflows/:id/yaml
// 워크플로우를 이식 가능한 YAML로 내보낸다. 서버 관리 필드는 제외된다.
func (h *WorkflowHandler) ExportWorkflowYAML(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	workflowID := c.Param("id")

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Workflow not found")
		return
	}

	spec, err := workflowModelToSpec(&workflow)
	if err != nil {
		h.logger.Error("Failed to build workflow spec", "request_id", requestID, "workflow_id", workflowID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, "Failed to build spec")
		return
	}

	yamlBytes, err := specToYAML(spec)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeInternalError, "Failed to marshal YAML")
		return
	}

	c.Header("Content-Type", "application/x-yaml")
	c.Header("Content-Disposition", "attachment; filename=\""+workflow.Slug+".yaml\"")
	c.String(http.StatusOK, string(yamlBytes))
}

// ImportWorkflowYAML POST /api/v1/workflows/import
// YAML 본문으로 워크플로우를 생성한다. project_id는 본문 또는 쿼리 파라미터로 지정/override 가능.
// export한 YAML을 그대로(또는 project_id만 바꿔) import하면 복제/템플릿 인스턴스화가 된다.
func (h *WorkflowHandler) ImportWorkflowYAML(c *gin.Context) {
	requestID := middleware.GetRequestID(c)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeBadRequest, "Failed to read body")
		return
	}

	spec, err := yamlToSpec(body)
	if err != nil {
		h.logger.Warn("Invalid workflow YAML", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "Invalid YAML: "+err.Error())
		return
	}

	// project_id override (쿼리 파라미터 우선) — 템플릿을 다른 프로젝트로 인스턴스화할 때 사용
	if pid := c.Query("project_id"); pid != "" {
		spec.ProjectID = pid
	}
	if spec.ProjectID == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "project_id is required (in YAML or ?project_id=)")
		return
	}
	if spec.Name == "" {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "name is required")
		return
	}

	// Project 존재 확인
	var project models.Project
	if err := h.db.First(&project, "id = ?", spec.ProjectID).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "Project not found")
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := ""
	if s, ok := userID.(string); ok {
		userIDStr = s
	}

	workflow := h.buildWorkflowModel(spec, userIDStr)
	if err := h.db.Create(workflow).Error; err != nil {
		h.logger.Error("Failed to create workflow from YAML", "request_id", requestID, "error", err)
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "Failed to create workflow")
		return
	}

	h.logger.Info("Workflow imported from YAML", "request_id", requestID, "workflow_id", workflow.ID)
	middleware.SuccessResponse(c, *workflow)
	c.Status(http.StatusCreated)
}

// buildWorkflowModel은 WorkflowSpec에서 새 워크플로우 모델을 만든다.
// 파이프라인 ID 등 서버 관리 값을 새로 할당한다(템플릿 재사용 시 ID 충돌 방지).
func (h *WorkflowHandler) buildWorkflowModel(spec *WorkflowSpec, createdBy string) *models.Workflow {
	executionMode := spec.ExecutionMode
	if executionMode == "" {
		executionMode = types.ExecutionModeParallel
	}

	pipelines := spec.Pipelines
	if pipelines == nil {
		pipelines = []types.GroupedPipeline{}
	}
	// 파이프라인 ID는 항상 새로 부여한다 — 템플릿(id 있음/없음)을 인스턴스화할 때 고유성 보장.
	for i := range pipelines {
		pipelines[i].ID = uuid.New().String()
	}

	pipelinesJSON, _ := json.Marshal(pipelines)
	failurePolicyJSON, _ := json.Marshal(spec.FailurePolicy)
	metadataJSON, _ := json.Marshal(spec.Metadata)
	tagsJSON, _ := json.Marshal(spec.Tags)

	now := time.Now()
	workflow := &models.Workflow{
		ID:              uuid.New().String(),
		ProjectID:       spec.ProjectID,
		ClusterID:       spec.ClusterID,
		Name:            spec.Name,
		Slug:            spec.Slug,
		Description:     spec.Description,
		Type:            string(spec.Type),
		ExecutionMode:   string(executionMode),
		Status:          string(types.PipelineGroupStatusIdle),
		PipelinesConfig: string(pipelinesJSON),
		FailurePolicy:   string(failurePolicyJSON),
		Metadata:        string(metadataJSON),
		Tags:            string(tagsJSON),
		CreatedBy:       createdBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if spec.Schedule != nil {
		workflow.ScheduleType = string(spec.Schedule.Type)
		workflow.ScheduleCron = spec.Schedule.Cron
		workflow.ScheduleInterval = spec.Schedule.Interval
		workflow.ScheduleTimezone = spec.Schedule.Timezone
		workflow.ScheduleEnabled = spec.Schedule.Enabled
	}

	return workflow
}

// workflowModelToSpec은 저장된 워크플로우 모델을 이식 가능한 spec으로 변환한다.
func workflowModelToSpec(w *models.Workflow) (*WorkflowSpec, error) {
	spec := &WorkflowSpec{
		ProjectID:     w.ProjectID,
		ClusterID:     w.ClusterID,
		Name:          w.Name,
		Slug:          w.Slug,
		Description:   w.Description,
		Type:          types.PipelineGroupType(w.Type),
		ExecutionMode: types.ExecutionMode(w.ExecutionMode),
	}

	if w.PipelinesConfig != "" {
		if err := json.Unmarshal([]byte(w.PipelinesConfig), &spec.Pipelines); err != nil {
			return nil, err
		}
	}
	if w.FailurePolicy != "" {
		_ = json.Unmarshal([]byte(w.FailurePolicy), &spec.FailurePolicy)
	}
	if w.Metadata != "" {
		_ = json.Unmarshal([]byte(w.Metadata), &spec.Metadata)
	}
	if w.Tags != "" {
		_ = json.Unmarshal([]byte(w.Tags), &spec.Tags)
	}
	if w.ScheduleType != "" {
		spec.Schedule = &types.ScheduleConfig{
			Type:     types.ScheduleType(w.ScheduleType),
			Cron:     w.ScheduleCron,
			Interval: w.ScheduleInterval,
			Timezone: w.ScheduleTimezone,
			Enabled:  w.ScheduleEnabled,
		}
	}

	return spec, nil
}

// specToYAML은 spec을 YAML로 직렬화한다.
// 공유 타입에 yaml 태그가 없으므로 JSON(json 태그) → map → YAML 경유로 필드명을 보존한다.
func specToYAML(spec *WorkflowSpec) ([]byte, error) {
	jsonBytes, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, err
	}
	return yaml.Marshal(m)
}

// yamlToSpec은 YAML을 spec으로 역직렬화한다(JSON 경유로 json 태그 매핑 사용).
func yamlToSpec(data []byte) (*WorkflowSpec, error) {
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var spec WorkflowSpec
	if err := json.Unmarshal(jsonBytes, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
