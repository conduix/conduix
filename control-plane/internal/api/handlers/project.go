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

// ProjectHandler 프로젝트 핸들러
type ProjectHandler struct {
	db *database.DB
}

// NewProjectHandler 새 핸들러 생성
func NewProjectHandler(db *database.DB) *ProjectHandler {
	return &ProjectHandler{db: db}
}

// ProjectResponse 프로젝트 응답
type ProjectResponse struct {
	models.Project
	WorkflowCount int `json:"workflow_count"`
}

// ProjectListResponse 프로젝트 목록 응답
type ProjectListResponse struct {
	Projects   []ProjectResponse `json:"projects"`
	Total      int64             `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// ListProjects GET /api/v1/projects
// 프로젝트 목록 조회
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	// 페이징 파라미터
	page := parseIntDefault(c.Query("page"), 1)
	pageSize := parseIntDefault(c.Query("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}

	// 필터 파라미터
	search := c.Query("search")
	status := c.Query("status")

	// 쿼리 빌드
	query := h.db.Model(&models.Project{})

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("name LIKE ? OR alias LIKE ? OR description LIKE ?",
			searchPattern, searchPattern, searchPattern)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// 총 개수
	var total int64
	query.Count(&total)

	// 프로젝트 조회
	var projects []models.Project
	offset := (page - 1) * pageSize
	query.Preload("Owner").Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&projects)

	// 워크플로우 개수 집계
	projectResponses := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		var workflowCount int64
		h.db.Model(&models.Workflow{}).Where("project_id = ?", p.ID).Count(&workflowCount)
		projectResponses[i] = ProjectResponse{
			Project:       p,
			WorkflowCount: int(workflowCount),
		}
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, types.APIResponse[ProjectListResponse]{
		Success: true,
		Data: ProjectListResponse{
			Projects:   projectResponses,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
		},
	})
}

// GetProject GET /api/v1/projects/:id
// 프로젝트 상세 조회 (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) GetProject(c *gin.Context) {
	idOrAlias := c.Param("id")

	var project models.Project
	// ID 또는 Alias로 조회 (담당자 목록 포함)
	if err := h.db.Preload("Owner").Preload("Owners.User").Preload("Workflows").
		Where("id = ? OR alias = ?", idOrAlias, idOrAlias).
		First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.Project]{
		Success: true,
		Data:    project,
	})
}

// CreateProjectRequest 프로젝트 생성 요청
type CreateProjectRequest struct {
	Name        string   `json:"name" binding:"required"`  // 프로젝트명
	Alias       string   `json:"alias" binding:"required"` // URL 경로용 별칭
	Description string   `json:"description"`
	OwnerID     string   `json:"owner_id"`  // 기본 담당자 (deprecated, use owner_ids)
	OwnerIDs    []string `json:"owner_ids"` // 담당자 목록
	Metadata    string   `json:"metadata"`
	Tags        string   `json:"tags"`
}

// CreateProject POST /api/v1/projects
// 프로젝트 생성
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr := userID.(string)

	// 중복 프로젝트명 확인
	var existing models.Project
	if err := h.db.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeAlreadyExists, "이미 존재하는 프로젝트명입니다")
		return
	}

	// 중복 Alias 확인
	if err := h.db.Where("alias = ?", req.Alias).First(&existing).Error; err == nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeAlreadyExists, "이미 존재하는 Alias입니다")
		return
	}

	// 기본 담당자 설정 (owner_ids가 없으면 owner_id 사용, 둘 다 없으면 현재 사용자)
	primaryOwnerID := req.OwnerID
	if len(req.OwnerIDs) > 0 {
		primaryOwnerID = req.OwnerIDs[0]
	}
	if primaryOwnerID == "" {
		primaryOwnerID = userIDStr
	}

	project := models.Project{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Alias:       req.Alias,
		Description: req.Description,
		Status:      "active",
		OwnerID:     primaryOwnerID,
		Metadata:    req.Metadata,
		Tags:        req.Tags,
		CreatedBy:   userIDStr,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// 트랜잭션으로 프로젝트와 담당자 동시 생성
	tx := h.db.Begin()

	if err := tx.Create(&project).Error; err != nil {
		tx.Rollback()
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "프로젝트 생성에 실패했습니다")
		return
	}

	// 담당자 목록 생성
	ownerIDs := req.OwnerIDs
	if len(ownerIDs) == 0 && primaryOwnerID != "" {
		ownerIDs = []string{primaryOwnerID}
	}

	for i, ownerID := range ownerIDs {
		role := "maintainer"
		if i == 0 {
			role = "owner"
		}
		projectOwner := models.ProjectOwner{
			ID:        uuid.New().String(),
			ProjectID: project.ID,
			UserID:    ownerID,
			Role:      role,
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&projectOwner).Error; err != nil {
			tx.Rollback()
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "담당자 등록에 실패했습니다")
			return
		}
	}

	tx.Commit()

	// 담당자 정보 조회하여 반환
	h.db.Preload("Owners.User").First(&project, "id = ?", project.ID)

	c.JSON(http.StatusCreated, types.APIResponse[models.Project]{
		Success: true,
		Data:    project,
		Message: "프로젝트가 생성되었습니다",
	})
}

// UpdateProjectRequest 프로젝트 수정 요청
type UpdateProjectRequest struct {
	Name        string   `json:"name"`  // 프로젝트명
	Alias       string   `json:"alias"` // URL 경로용 별칭
	Description string   `json:"description"`
	Status      string   `json:"status"`
	OwnerID     string   `json:"owner_id"`  // deprecated, use owner_ids
	OwnerIDs    []string `json:"owner_ids"` // 담당자 목록
	Metadata    string   `json:"metadata"`
	Tags        string   `json:"tags"`
}

// UpdateProject PUT /api/v1/projects/:id
// 프로젝트 수정 (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	idOrAlias := c.Param("id")

	var project models.Project
	if err := h.db.Where("id = ? OR alias = ?", idOrAlias, idOrAlias).First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 프로젝트명 변경 시 중복 확인
	if req.Name != "" && req.Name != project.Name {
		var existing models.Project
		if err := h.db.Where("name = ? AND id != ?", req.Name, project.ID).First(&existing).Error; err == nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeAlreadyExists, "이미 존재하는 프로젝트명입니다")
			return
		}
		project.Name = req.Name
	}

	// Alias 변경 시 중복 확인
	if req.Alias != "" && req.Alias != project.Alias {
		var existing models.Project
		if err := h.db.Where("alias = ? AND id != ?", req.Alias, project.ID).First(&existing).Error; err == nil {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeAlreadyExists, "이미 존재하는 Alias입니다")
			return
		}
		project.Alias = req.Alias
	}

	if req.Description != "" {
		project.Description = req.Description
	}
	if req.Status != "" {
		// 유효한 상태인지 확인
		validStatuses := []string{"active", "inactive", "archived"}
		isValid := false
		for _, s := range validStatuses {
			if req.Status == s {
				isValid = true
				break
			}
		}
		if !isValid {
			middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "유효하지 않은 상태입니다. (active, inactive, archived)")
			return
		}
		project.Status = req.Status
	}
	if req.OwnerID != "" {
		project.OwnerID = req.OwnerID
	}
	if req.Metadata != "" {
		project.Metadata = req.Metadata
	}
	if req.Tags != "" {
		project.Tags = req.Tags
	}

	project.UpdatedAt = time.Now()

	// 트랜잭션 시작
	tx := h.db.Begin()

	if err := tx.Save(&project).Error; err != nil {
		tx.Rollback()
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "프로젝트 수정에 실패했습니다")
		return
	}

	// 담당자 목록 업데이트 (owner_ids가 있는 경우에만)
	if len(req.OwnerIDs) > 0 {
		// 기존 담당자 삭제
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.ProjectOwner{}).Error; err != nil {
			tx.Rollback()
			middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "담당자 수정에 실패했습니다")
			return
		}

		// 새 담당자 추가
		for i, ownerID := range req.OwnerIDs {
			role := "maintainer"
			if i == 0 {
				role = "owner"
			}
			projectOwner := models.ProjectOwner{
				ID:        uuid.New().String(),
				ProjectID: project.ID,
				UserID:    ownerID,
				Role:      role,
				CreatedAt: time.Now(),
			}
			if err := tx.Create(&projectOwner).Error; err != nil {
				tx.Rollback()
				middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "담당자 등록에 실패했습니다")
				return
			}
		}

		// 첫 번째 담당자를 기본 담당자로 설정
		project.OwnerID = req.OwnerIDs[0]
		tx.Model(&project).Update("owner_id", project.OwnerID)
	}

	tx.Commit()

	// 담당자 정보 조회하여 반환
	h.db.Preload("Owners.User").Preload("Owner").First(&project, "id = ?", project.ID)

	c.JSON(http.StatusOK, types.APIResponse[models.Project]{
		Success: true,
		Data:    project,
		Message: "프로젝트가 수정되었습니다",
	})
}

// DeleteProject DELETE /api/v1/projects/:id
// 프로젝트 삭제 (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	idOrAlias := c.Param("id")

	var project models.Project
	if err := h.db.Where("id = ? OR alias = ?", idOrAlias, idOrAlias).First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	// 하위 워크플로우가 있는지 확인
	var workflowCount int64
	h.db.Model(&models.Workflow{}).Where("project_id = ?", project.ID).Count(&workflowCount)
	if workflowCount > 0 {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeHasChildren, "하위 워크플로우가 존재합니다. 먼저 워크플로우를 삭제하거나 다른 프로젝트로 이동하세요.")
		return
	}

	// 소프트 삭제
	if err := h.db.Delete(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "프로젝트 삭제에 실패했습니다")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "프로젝트가 삭제되었습니다",
	})
}

// GetProjectWorkflows GET /api/v1/projects/:id/workflows
// 프로젝트의 워크플로우 목록 조회 (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) GetProjectWorkflows(c *gin.Context) {
	idOrAlias := c.Param("id")

	// 프로젝트 존재 확인
	var project models.Project
	if err := h.db.Where("id = ? OR alias = ?", idOrAlias, idOrAlias).First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	var workflows []models.Workflow
	h.db.Where("project_id = ?", project.ID).Order("created_at DESC").Find(&workflows)

	c.JSON(http.StatusOK, types.APIResponse[[]models.Workflow]{
		Success: true,
		Data:    workflows,
	})
}

// GetProjectHierarchy GET /api/v1/projects/:id/hierarchy
// 프로젝트 계층 구조 조회 (프로젝트 > 워크플로우 > 파이프라인) (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) GetProjectHierarchy(c *gin.Context) {
	idOrAlias := c.Param("id")

	var project models.Project
	if err := h.db.Preload("Owner").Where("id = ? OR alias = ?", idOrAlias, idOrAlias).First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	// 워크플로우 조회
	var workflows []models.Workflow
	h.db.Where("project_id = ?", project.ID).Find(&workflows)

	// 계층 구조 구성
	type WorkflowWithPipelines struct {
		models.Workflow
		Pipelines []map[string]any `json:"pipelines"`
	}

	type Hierarchy struct {
		Project   models.Project          `json:"project"`
		Workflows []WorkflowWithPipelines `json:"workflows"`
	}

	workflowsWithPipelines := make([]WorkflowWithPipelines, len(workflows))
	for i, w := range workflows {
		// 워크플로우의 파이프라인 설정에서 파이프라인 정보 추출
		workflowsWithPipelines[i] = WorkflowWithPipelines{
			Workflow:  w,
			Pipelines: []map[string]any{}, // PipelinesConfig JSON 파싱 필요
		}
	}

	hierarchy := Hierarchy{
		Project:   project,
		Workflows: workflowsWithPipelines,
	}

	c.JSON(http.StatusOK, types.APIResponse[Hierarchy]{
		Success: true,
		Data:    hierarchy,
	})
}

// GetProjectDataTypes GET /api/v1/projects/{id}/data-types
// 프로젝트의 데이터 유형 목록 조회 (ID 또는 Alias로 조회 가능)
func (h *ProjectHandler) GetProjectDataTypes(c *gin.Context) {
	idOrAlias := c.Param("id")

	// 프로젝트 존재 여부 확인 (ID 또는 Alias)
	var project models.Project
	if err := h.db.Where("id = ? OR alias = ?", idOrAlias, idOrAlias).First(&project).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusNotFound, types.ErrCodeNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}

	// 카테고리 필터
	category := c.Query("category")

	var dataTypes []models.DataType
	query := h.db.Where("project_id = ?", project.ID).Preload("Preworks").Preload("Parent")

	if category != "" {
		query = query.Where("category = ?", category)
	}

	if err := query.Order("name ASC").Find(&dataTypes).Error; err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "데이터 유형 목록 조회 실패")
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.DataType]{
		Success: true,
		Data:    dataTypes,
	})
}
