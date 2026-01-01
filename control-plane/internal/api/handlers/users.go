package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/internal/api/middleware"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// UserHandler 사용자 관리 핸들러
type UserHandler struct {
	db *database.DB
}

// NewUserHandler 새 핸들러 생성
func NewUserHandler(db *database.DB) *UserHandler {
	return &UserHandler{db: db}
}

// UserListResponse 사용자 목록 응답
type UserListResponse struct {
	Users      []UserWithPermissions `json:"users"`
	Total      int64                 `json:"total"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"page_size"`
	TotalPages int                   `json:"total_pages"`
}

// UserWithPermissions 권한 포함 사용자 정보
type UserWithPermissions struct {
	models.User
	PermissionCount int `json:"permission_count"`
}

// ListUsers GET /api/v1/users
// 사용자 목록 조회 (관리자 전용)
func (h *UserHandler) ListUsers(c *gin.Context) {
	// 관리자 권한 확인
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	// 페이징 파라미터
	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if _, err := c.GetQuery("page"); err {
			page = parseIntDefault(p, 1)
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		pageSize = parseIntDefault(ps, 20)
		if pageSize > 100 {
			pageSize = 100
		}
	}

	// 검색/필터 파라미터
	search := c.Query("search")
	roleFilter := c.Query("role")

	// 쿼리 빌드
	query := h.db.Model(&models.User{})

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("email LIKE ? OR name LIKE ?", searchPattern, searchPattern)
	}
	if roleFilter != "" {
		query = query.Where("role = ?", roleFilter)
	}

	// 총 개수
	var total int64
	query.Count(&total)

	// 사용자 조회
	var users []models.User
	offset := (page - 1) * pageSize
	query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&users)

	// 권한 개수 집계
	usersWithPermissions := make([]UserWithPermissions, len(users))
	for i, user := range users {
		var permCount int64
		h.db.Model(&models.ResourcePermission{}).Where("user_id = ?", user.ID).Count(&permCount)
		usersWithPermissions[i] = UserWithPermissions{
			User:            user,
			PermissionCount: int(permCount),
		}
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, types.APIResponse[UserListResponse]{
		Success: true,
		Data: UserListResponse{
			Users:      usersWithPermissions,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
		},
	})
}

// GetUser GET /api/v1/users/:id
// 사용자 상세 조회 (관리자 전용)
func (h *UserHandler) GetUser(c *gin.Context) {
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	userID := c.Param("id")

	var user models.User
	if err := h.db.First(&user, "id = ?", userID).Error; err != nil {
		h.errorResponse(c, http.StatusNotFound, types.ErrCodeNotFound, "사용자를 찾을 수 없습니다", "User not found")
		return
	}

	// 권한 조회
	var permissions []models.ResourcePermission
	h.db.Where("user_id = ?", userID).Find(&permissions)

	c.JSON(http.StatusOK, types.APIResponse[map[string]any]{
		Success: true,
		Data: map[string]any{
			"user":        user,
			"permissions": permissions,
		},
	})
}

// UpdateUserRoleRequest 역할 수정 요청
type UpdateUserRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// UpdateUserRole PUT /api/v1/users/:id/role
// 사용자 역할 수정 (관리자 전용)
func (h *UserHandler) UpdateUserRole(c *gin.Context) {
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	currentUserID, _ := c.Get("user_id")
	targetUserID := c.Param("id")

	// 자기 자신의 역할은 수정 불가
	if currentUserID == targetUserID {
		h.errorResponse(c, http.StatusBadRequest, types.ErrCodeBadRequest, "자기 자신의 역할은 수정할 수 없습니다", "Cannot modify your own role")
		return
	}

	var req UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 유효한 역할인지 확인
	validRoles := []string{string(types.UserRoleAdmin), string(types.UserRoleOperator), string(types.UserRoleViewer)}
	isValid := false
	for _, r := range validRoles {
		if req.Role == r {
			isValid = true
			break
		}
	}
	if !isValid {
		h.errorResponse(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "유효하지 않은 역할입니다. (admin, operator, viewer)", "Invalid role. (admin, operator, viewer)")
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", targetUserID).Error; err != nil {
		h.errorResponse(c, http.StatusNotFound, types.ErrCodeNotFound, "사용자를 찾을 수 없습니다", "User not found")
		return
	}

	user.Role = req.Role
	if err := h.db.Save(&user).Error; err != nil {
		h.errorResponse(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "역할 수정에 실패했습니다", "Failed to update role")
		return
	}

	// 감사 로그
	h.logAudit(c, "user_role_update", "user", targetUserID, map[string]any{
		"new_role": req.Role,
	})

	c.JSON(http.StatusOK, types.APIResponse[models.User]{
		Success: true,
		Data:    user,
		Message: h.getErrorMessage(c, "역할이 수정되었습니다", "Role updated"),
	})
}

// CreatePermissionRequest 권한 생성 요청
type CreatePermissionRequest struct {
	ResourceType string `json:"resource_type" binding:"required"` // provider, group, pipeline
	ResourceID   string `json:"resource_id" binding:"required"`
	UserID       string `json:"user_id" binding:"required"`
	Actions      string `json:"actions" binding:"required"` // read,write,execute,delete,admin
}

// CreatePermission POST /api/v1/permissions
// 리소스 권한 생성 (관리자 전용)
func (h *UserHandler) CreatePermission(c *gin.Context) {
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	var req CreatePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponseWithCode(c, http.StatusBadRequest, types.ErrCodeInvalidJSON, err.Error())
		return
	}

	// 유효한 리소스 타입 확인
	validTypes := []string{"provider", "group", "pipeline"}
	isValidType := false
	for _, t := range validTypes {
		if req.ResourceType == t {
			isValidType = true
			break
		}
	}
	if !isValidType {
		h.errorResponse(c, http.StatusBadRequest, types.ErrCodeValidationFailed, "유효하지 않은 리소스 타입입니다. (provider, group, pipeline)", "Invalid resource type. (provider, group, pipeline)")
		return
	}

	// 사용자 존재 확인
	var user models.User
	if err := h.db.First(&user, "id = ?", req.UserID).Error; err != nil {
		h.errorResponse(c, http.StatusBadRequest, types.ErrCodeNotFound, "사용자를 찾을 수 없습니다", "User not found")
		return
	}

	// 리소스 존재 확인
	if !h.checkResourceExists(req.ResourceType, req.ResourceID) {
		h.errorResponse(c, http.StatusBadRequest, types.ErrCodeNotFound, "리소스를 찾을 수 없습니다", "Resource not found")
		return
	}

	// 중복 확인
	var existing models.ResourcePermission
	if err := h.db.Where("resource_type = ? AND resource_id = ? AND user_id = ?",
		req.ResourceType, req.ResourceID, req.UserID).First(&existing).Error; err == nil {
		// 이미 존재하면 업데이트
		existing.Actions = req.Actions
		existing.UpdatedAt = time.Now()
		if err := h.db.Save(&existing).Error; err != nil {
			h.errorResponse(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "권한 수정에 실패했습니다", "Failed to update permission")
			return
		}
		c.JSON(http.StatusOK, types.APIResponse[models.ResourcePermission]{
			Success: true,
			Data:    existing,
			Message: h.getErrorMessage(c, "권한이 수정되었습니다", "Permission updated"),
		})
		return
	}

	// 새 권한 생성
	permission := models.ResourcePermission{
		ID:           uuid.New().String(),
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		UserID:       req.UserID,
		Actions:      req.Actions,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := h.db.Create(&permission).Error; err != nil {
		h.errorResponse(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "권한 생성에 실패했습니다", "Failed to create permission")
		return
	}

	// 감사 로그
	h.logAudit(c, "permission_create", req.ResourceType, req.ResourceID, map[string]any{
		"user_id": req.UserID,
		"actions": req.Actions,
	})

	c.JSON(http.StatusCreated, types.APIResponse[models.ResourcePermission]{
		Success: true,
		Data:    permission,
		Message: h.getErrorMessage(c, "권한이 생성되었습니다", "Permission created"),
	})
}

// DeletePermission DELETE /api/v1/permissions/:id
// 리소스 권한 삭제 (관리자 전용)
func (h *UserHandler) DeletePermission(c *gin.Context) {
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	permissionID := c.Param("id")

	var permission models.ResourcePermission
	if err := h.db.First(&permission, "id = ?", permissionID).Error; err != nil {
		h.errorResponse(c, http.StatusNotFound, types.ErrCodeNotFound, "권한을 찾을 수 없습니다", "Permission not found")
		return
	}

	if err := h.db.Delete(&permission).Error; err != nil {
		h.errorResponse(c, http.StatusInternalServerError, types.ErrCodeDatabaseError, "권한 삭제에 실패했습니다", "Failed to delete permission")
		return
	}

	// 감사 로그
	h.logAudit(c, "permission_delete", permission.ResourceType, permission.ResourceID, map[string]any{
		"user_id": permission.UserID,
	})

	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: h.getErrorMessage(c, "권한이 삭제되었습니다", "Permission deleted"),
	})
}

// ListPermissions GET /api/v1/permissions
// 리소스 권한 목록 조회 (관리자 전용)
func (h *UserHandler) ListPermissions(c *gin.Context) {
	role, _ := c.Get("user_role")
	if role != string(types.UserRoleAdmin) {
		h.errorResponse(c, http.StatusForbidden, types.ErrCodeForbidden, "관리자만 접근 가능합니다", "Admin access required")
		return
	}

	// 필터 파라미터
	resourceType := c.Query("resource_type")
	resourceID := c.Query("resource_id")
	userID := c.Query("user_id")

	query := h.db.Model(&models.ResourcePermission{}).Preload("User")

	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if resourceID != "" {
		query = query.Where("resource_id = ?", resourceID)
	}
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	var permissions []models.ResourcePermission
	query.Order("created_at DESC").Find(&permissions)

	c.JSON(http.StatusOK, types.APIResponse[[]models.ResourcePermission]{
		Success: true,
		Data:    permissions,
	})
}

// GetRoles GET /api/v1/roles
// 역할 목록 조회
func (h *UserHandler) GetRoles(c *gin.Context) {
	roles := []RoleInfo{
		{
			Role:        string(types.UserRoleAdmin),
			DisplayName: "관리자",
			Description: "모든 리소스에 대한 전체 권한",
			Permissions: []string{"read", "write", "execute", "delete", "admin", "user_management"},
		},
		{
			Role:        string(types.UserRoleOperator),
			DisplayName: "운영자",
			Description: "파이프라인 생성/수정 및 실행 권한",
			Permissions: []string{"read", "write", "execute"},
		},
		{
			Role:        string(types.UserRoleViewer),
			DisplayName: "뷰어",
			Description: "읽기 전용 접근",
			Permissions: []string{"read"},
		},
	}

	c.JSON(http.StatusOK, types.APIResponse[[]RoleInfo]{
		Success: true,
		Data:    roles,
	})
}

// UserSearchResult 사용자 검색 결과 (자동완성용)
type UserSearchResult struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// SearchUsers GET /api/v1/users/search
// 사용자 검색 (자동완성용, 모든 인증된 사용자 접근 가능)
func (h *UserHandler) SearchUsers(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusOK, types.APIResponse[[]UserSearchResult]{
			Success: true,
			Data:    []UserSearchResult{},
		})
		return
	}

	// 최대 10개까지만 반환
	limit := 10
	if l := c.Query("limit"); l != "" {
		limit = parseIntDefault(l, 10)
		if limit > 20 {
			limit = 20
		}
	}

	var users []models.User
	searchPattern := "%" + query + "%"
	h.db.Where("email LIKE ? OR name LIKE ?", searchPattern, searchPattern).
		Order("name ASC").
		Limit(limit).
		Find(&users)

	results := make([]UserSearchResult, len(users))
	for i, u := range users {
		results[i] = UserSearchResult{
			ID:        u.ID,
			Email:     u.Email,
			Name:      u.Name,
			AvatarURL: u.AvatarURL,
		}
	}

	c.JSON(http.StatusOK, types.APIResponse[[]UserSearchResult]{
		Success: true,
		Data:    results,
	})
}

// helper functions

// getErrorMessage returns localized error message based on Accept-Language header
func (h *UserHandler) getErrorMessage(c *gin.Context, ko, en string) string {
	lang := c.GetHeader("Accept-Language")
	if len(lang) >= 2 && lang[:2] == "ko" {
		return ko
	}
	return en
}

// errorResponse sends an error response with localized message
func (h *UserHandler) errorResponse(c *gin.Context, status int, code types.ErrorCode, ko, en string) {
	middleware.ErrorResponseWithCode(c, status, code, h.getErrorMessage(c, ko, en))
}

func (h *UserHandler) checkResourceExists(resourceType, resourceID string) bool {
	var count int64
	switch resourceType {
	case "project":
		h.db.Model(&models.Project{}).Where("id = ?", resourceID).Count(&count)
	case "workflow":
		h.db.Model(&models.Workflow{}).Where("id = ?", resourceID).Count(&count)
	case "pipeline":
		h.db.Model(&models.Pipeline{}).Where("id = ?", resourceID).Count(&count)
	default:
		return false
	}
	return count > 0
}

func (h *UserHandler) logAudit(c *gin.Context, action, resource, resourceID string, details map[string]any) {
	userID, _ := c.Get("user_id")

	detailsJSON := ""
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}

	audit := models.AuditLog{
		ID:         uuid.New().String(),
		UserID:     userID.(string),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    detailsJSON,
		IPAddress:  c.ClientIP(),
		CreatedAt:  time.Now(),
	}
	h.db.Create(&audit)
}

func parseIntDefault(s string, def int) int {
	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			return def
		}
	}
	if result == 0 {
		return def
	}
	return result
}
