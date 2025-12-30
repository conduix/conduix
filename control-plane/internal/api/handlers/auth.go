package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/conduix/conduix/control-plane/pkg/config"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// OAuth2ProviderInfo 프로바이더별 설정 정보
type OAuth2ProviderInfo struct {
	Config      *oauth2.Config
	UserInfoURL string
	ParseUser   func(data []byte) (*UserInfo, error)
	Name        string
}

// AuthHandler 인증 핸들러
type AuthHandler struct {
	db          *database.DB
	jwtSecret   []byte
	providers   map[types.OAuth2Provider]*OAuth2ProviderInfo
	usersConfig *config.UsersConfig
	frontendURL string
	mu          sync.RWMutex
}

// NewAuthHandler 새 핸들러 생성
func NewAuthHandler(db *database.DB, jwtSecret string, usersConfig *config.UsersConfig, frontendURL string) *AuthHandler {
	if usersConfig == nil {
		usersConfig = &config.UsersConfig{}
	}
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	return &AuthHandler{
		db:          db,
		jwtSecret:   []byte(jwtSecret),
		providers:   make(map[types.OAuth2Provider]*OAuth2ProviderInfo),
		usersConfig: usersConfig,
		frontendURL: frontendURL,
	}
}

// RegisterGitHubProvider GitHub OAuth2 프로바이더 등록
func (h *AuthHandler) RegisterGitHubProvider(clientID, clientSecret, redirectURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.providers[types.OAuth2ProviderGitHub] = &OAuth2ProviderInfo{
		Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"user:email", "read:user"},
			Endpoint:     github.Endpoint,
		},
		UserInfoURL: "https://api.github.com/user",
		ParseUser:   parseGitHubUser,
		Name:        "GitHub",
	}
}

// RegisterGoogleProvider Google OAuth2 프로바이더 등록
func (h *AuthHandler) RegisterGoogleProvider(clientID, clientSecret, redirectURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.providers[types.OAuth2ProviderGoogle] = &OAuth2ProviderInfo{
		Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "profile", "email"},
			Endpoint:     google.Endpoint,
		},
		UserInfoURL: "https://www.googleapis.com/oauth2/v2/userinfo",
		ParseUser:   parseGoogleUser,
		Name:        "Google",
	}
}

// RegisterCustomProvider 커스텀 OAuth2 프로바이더 등록
func (h *AuthHandler) RegisterCustomProvider(providerID types.OAuth2Provider, cfg *types.OAuth2ProviderConfig, parser func([]byte) (*UserInfo, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.providers[providerID] = &OAuth2ProviderInfo{
		Config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  cfg.AuthURL,
				TokenURL: cfg.TokenURL,
			},
		},
		UserInfoURL: cfg.UserInfoURL,
		ParseUser:   parser,
		Name:        cfg.Name,
	}
}

// GetProviders GET /api/v1/auth/providers
// 사용 가능한 OAuth2 프로바이더 목록 조회
func (h *AuthHandler) GetProviders(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	providers := make([]types.AvailableProvider, 0, len(h.providers))
	for id, info := range h.providers {
		providers = append(providers, types.AvailableProvider{
			ID:      id,
			Name:    info.Name,
			Enabled: true,
		})
	}

	c.JSON(http.StatusOK, types.APIResponse[[]types.AvailableProvider]{
		Success: true,
		Data:    providers,
	})
}

// Login POST /api/v1/auth/login
// 로그인 시작 - OAuth2 인증 URL 반환
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	providerID := types.OAuth2Provider(req.Provider)

	h.mu.RLock()
	provider, exists := h.providers[providerID]
	h.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   fmt.Sprintf("Provider '%s' not configured", req.Provider),
		})
		return
	}

	state := uuid.New().String()
	// state에 provider 정보 포함 (callback에서 사용)
	stateWithProvider := fmt.Sprintf("%s:%s", state, req.Provider)

	authURL := provider.Config.AuthCodeURL(stateWithProvider)

	c.JSON(http.StatusOK, types.APIResponse[map[string]string]{
		Success: true,
		Data: map[string]string{
			"auth_url": authURL,
			"state":    state,
			"provider": req.Provider,
		},
	})
}

// Callback GET /api/v1/auth/callback
// OAuth2 콜백 처리
func (h *AuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" {
		// OAuth 에러 응답 처리
		errorMsg := c.Query("error")
		errorDesc := c.Query("error_description")
		if errorMsg != "" {
			c.JSON(http.StatusBadRequest, types.APIResponse[any]{
				Success: false,
				Error:   fmt.Sprintf("%s: %s", errorMsg, errorDesc),
			})
			return
		}
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   "Missing authorization code",
		})
		return
	}

	// state에서 provider 추출 (format: uuid:provider)
	var providerID types.OAuth2Provider
	if len(state) > 37 && state[36] == ':' {
		providerID = types.OAuth2Provider(state[37:])
	} else {
		// 쿼리 파라미터에서 provider 확인
		providerID = types.OAuth2Provider(c.Query("provider"))
	}

	if providerID == "" {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   "Missing provider information",
		})
		return
	}

	h.mu.RLock()
	provider, exists := h.providers[providerID]
	h.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusBadRequest, types.APIResponse[any]{
			Success: false,
			Error:   fmt.Sprintf("Provider '%s' not configured", providerID),
		})
		return
	}

	// 토큰 교환
	token, err := provider.Config.Exchange(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   fmt.Sprintf("Failed to exchange token: %v", err),
		})
		return
	}

	// 사용자 정보 조회
	userInfo, err := h.fetchUserInfo(c.Request.Context(), provider, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   fmt.Sprintf("Failed to get user info: %v", err),
		})
		return
	}

	// 사용자 조회 또는 생성
	user, err := h.findOrCreateUser(userInfo, providerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// JWT 토큰 생성
	authToken, err := h.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// 프론트엔드 콜백 URL로 리다이렉트 (쿼리 파라미터에서 확인하거나 기본값 사용)
	frontendURL := c.Query("redirect_uri")
	if frontendURL == "" {
		frontendURL = h.frontendURL + "/login"
	}

	// 토큰을 쿼리 파라미터로 전달
	redirectURL := fmt.Sprintf("%s?token=%s", frontendURL, authToken.AccessToken)
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// fetchUserInfo OAuth2 프로바이더에서 사용자 정보 조회
func (h *AuthHandler) fetchUserInfo(ctx context.Context, provider *OAuth2ProviderInfo, token *oauth2.Token) (*UserInfo, error) {
	client := provider.Config.Client(ctx, token)

	resp, err := client.Get(provider.UserInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status %d", resp.StatusCode)
	}

	var data []byte
	data = make([]byte, 0)
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil {
			break
		}
	}

	return provider.ParseUser(data)
}

// UserInfo 사용자 정보
type UserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Login     string `json:"login"` // GitHub username
}

// parseGitHubUser GitHub API 응답 파싱
func parseGitHubUser(data []byte) (*UserInfo, error) {
	var ghUser struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.Unmarshal(data, &ghUser); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub user: %w", err)
	}

	name := ghUser.Name
	if name == "" {
		name = ghUser.Login
	}

	email := ghUser.Email
	if email == "" {
		// GitHub에서 이메일이 비공개일 경우
		email = fmt.Sprintf("%s@users.noreply.github.com", ghUser.Login)
	}

	return &UserInfo{
		ID:        fmt.Sprintf("%d", ghUser.ID),
		Email:     email,
		Name:      name,
		AvatarURL: ghUser.AvatarURL,
		Login:     ghUser.Login,
	}, nil
}

// parseGoogleUser Google API 응답 파싱
func parseGoogleUser(data []byte) (*UserInfo, error) {
	var googleUser struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}

	if err := json.Unmarshal(data, &googleUser); err != nil {
		return nil, fmt.Errorf("failed to parse Google user: %w", err)
	}

	return &UserInfo{
		ID:        googleUser.ID,
		Email:     googleUser.Email,
		Name:      googleUser.Name,
		AvatarURL: googleUser.Picture,
	}, nil
}

func (h *AuthHandler) findOrCreateUser(info *UserInfo, providerID types.OAuth2Provider) (*models.User, error) {
	var user models.User

	// 설정 파일에서 역할 확인
	configRole := h.usersConfig.GetRole(info.Email)

	result := h.db.Where("email = ?", info.Email).First(&user)
	if result.Error == nil {
		// 기존 사용자 - 마지막 로그인 업데이트
		now := time.Now()
		user.LastLogin = &now
		user.AvatarURL = info.AvatarURL // 아바타 업데이트

		// 설정 파일에 admin/operator로 등록된 경우 역할 업데이트
		// (설정 파일의 역할이 더 높은 권한인 경우에만 업데이트)
		if shouldUpgradeRole(user.Role, configRole) {
			user.Role = configRole
			fmt.Printf("[Auth] User %s role upgraded to %s (from config)\n", info.Email, configRole)
		}

		h.db.Save(&user)
		return &user, nil
	}

	// 새 사용자 생성 - 설정에 따른 역할 할당
	user = models.User{
		ID:         uuid.New().String(),
		Email:      info.Email,
		Name:       info.Name,
		Provider:   string(providerID),
		ProviderID: info.ID,
		Role:       configRole,
		AvatarURL:  info.AvatarURL,
		CreatedAt:  time.Now(),
	}

	if err := h.db.Create(&user).Error; err != nil {
		return nil, err
	}

	fmt.Printf("[Auth] New user %s created with role %s\n", info.Email, configRole)
	return &user, nil
}

// shouldUpgradeRole 설정 파일의 역할이 현재 역할보다 높은지 확인
func shouldUpgradeRole(currentRole, configRole string) bool {
	// 설정 파일에 명시적으로 admin 또는 operator로 지정된 경우
	// viewer -> operator, viewer -> admin, operator -> admin 업그레이드 허용
	roleWeight := map[string]int{
		"viewer":   1,
		"operator": 2,
		"admin":    3,
	}

	currentWeight := roleWeight[currentRole]
	configWeight := roleWeight[configRole]

	// 설정 파일의 역할이 더 높은 경우에만 업그레이드
	return configWeight > currentWeight
}

func (h *AuthHandler) generateToken(user *models.User) (*types.AuthToken, error) {
	expiresAt := time.Now().Add(24 * time.Hour)

	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"role":  user.Role,
		"exp":   expiresAt.Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(h.jwtSecret)
	if err != nil {
		return nil, err
	}

	return &types.AuthToken{
		AccessToken: tokenString,
		TokenType:   "Bearer",
		ExpiresIn:   int(24 * time.Hour.Seconds()),
		ExpiresAt:   expiresAt,
	}, nil
}

// GetCurrentUser GET /api/v1/auth/me
// 현재 사용자 조회
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, types.APIResponse[any]{
			Success: false,
			Error:   "Not authenticated",
		})
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "User not found",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[models.User]{
		Success: true,
		Data:    user,
	})
}

// Logout POST /api/v1/auth/logout
// 로그아웃
func (h *AuthHandler) Logout(c *gin.Context) {
	// TODO: 세션/토큰 무효화 (Redis 블랙리스트 등)
	c.JSON(http.StatusOK, types.APIResponse[any]{
		Success: true,
		Message: "Logged out",
	})
}

// UserProfileResponse 사용자 프로필 응답
type UserProfileResponse struct {
	User        models.User                 `json:"user"`
	Permissions []models.ResourcePermission `json:"permissions"`
	Pipelines   []PipelineAccess            `json:"pipelines"`
	Workflows   []WorkflowAccess            `json:"workflows"`
	RoleInfo    RoleInfo                    `json:"role_info"`
}

// PipelineAccess 파이프라인 접근 정보
type PipelineAccess struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// WorkflowAccess 워크플로우 접근 정보
type WorkflowAccess struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Actions []string `json:"actions"`
}

// RoleInfo 역할 정보
type RoleInfo struct {
	Role        string   `json:"role"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// GetUserProfile GET /api/v1/auth/profile
// 사용자 프로필 상세 조회 (권한 및 접근 가능한 리소스 포함)
func (h *AuthHandler) GetUserProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, types.APIResponse[any]{
			Success: false,
			Error:   "Not authenticated",
		})
		return
	}

	// 사용자 조회
	var user models.User
	if err := h.db.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "User not found",
		})
		return
	}

	// 사용자 권한 조회
	var permissions []models.ResourcePermission
	h.db.Where("user_id = ?", userID).Find(&permissions)

	// 역할 정보
	roleInfo := getRoleInfo(user.Role)

	// 접근 가능한 파이프라인/워크플로우 조회
	var pipelines []PipelineAccess
	var workflows []WorkflowAccess

	if user.Role == string(types.UserRoleAdmin) {
		// 관리자는 모든 파이프라인/워크플로우 접근 가능
		var allPipelines []models.Pipeline
		h.db.Select("id, name").Find(&allPipelines)
		for _, p := range allPipelines {
			pipelines = append(pipelines, PipelineAccess{
				ID:      p.ID,
				Name:    p.Name,
				Actions: []string{"read", "write", "execute", "delete", "admin"},
			})
		}

		var allWorkflows []models.Workflow
		h.db.Select("id, name, type").Find(&allWorkflows)
		for _, w := range allWorkflows {
			workflows = append(workflows, WorkflowAccess{
				ID:      w.ID,
				Name:    w.Name,
				Type:    w.Type,
				Actions: []string{"read", "write", "execute", "delete", "admin"},
			})
		}
	} else {
		// 일반 사용자는 권한이 있는 리소스만 조회
		for _, perm := range permissions {
			actions := splitActions(perm.Actions)
			switch perm.ResourceType {
			case "pipeline":
				var pipeline models.Pipeline
				if h.db.Select("id, name").First(&pipeline, "id = ?", perm.ResourceID).Error == nil {
					pipelines = append(pipelines, PipelineAccess{
						ID:      pipeline.ID,
						Name:    pipeline.Name,
						Actions: actions,
					})
				}
			case "workflow":
				var workflow models.Workflow
				if h.db.Select("id, name, type").First(&workflow, "id = ?", perm.ResourceID).Error == nil {
					workflows = append(workflows, WorkflowAccess{
						ID:      workflow.ID,
						Name:    workflow.Name,
						Type:    workflow.Type,
						Actions: actions,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, types.APIResponse[UserProfileResponse]{
		Success: true,
		Data: UserProfileResponse{
			User:        user,
			Permissions: permissions,
			Pipelines:   pipelines,
			Workflows:   workflows,
			RoleInfo:    roleInfo,
		},
	})
}

func getRoleInfo(role string) RoleInfo {
	switch role {
	case string(types.UserRoleAdmin):
		return RoleInfo{
			Role:        role,
			DisplayName: "관리자",
			Description: "모든 리소스에 대한 전체 권한",
			Permissions: []string{"read", "write", "execute", "delete", "admin", "user_management"},
		}
	case string(types.UserRoleOperator):
		return RoleInfo{
			Role:        role,
			DisplayName: "운영자",
			Description: "파이프라인 생성/수정 및 실행 권한",
			Permissions: []string{"read", "write", "execute"},
		}
	default:
		return RoleInfo{
			Role:        role,
			DisplayName: "뷰어",
			Description: "읽기 전용 접근",
			Permissions: []string{"read"},
		}
	}
}

func splitActions(actions string) []string {
	if actions == "" {
		return []string{}
	}
	result := []string{}
	for _, a := range strings.Split(actions, ",") {
		result = append(result, strings.TrimSpace(a))
	}
	return result
}
