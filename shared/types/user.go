package types

import "time"

// UserRole 사용자 역할
type UserRole string

const (
	UserRoleAdmin    UserRole = "admin"
	UserRoleOperator UserRole = "operator"
	UserRoleViewer   UserRole = "viewer"
)

// AuthProvider 인증 제공자
type AuthProvider string

const (
	AuthProviderOAuth2 AuthProvider = "oauth2"
	AuthProviderOIDC   AuthProvider = "oidc"
	AuthProviderLocal  AuthProvider = "local"
)

// OAuth2Provider OAuth2 프로바이더 유형
type OAuth2Provider string

const (
	OAuth2ProviderGitHub OAuth2Provider = "github"
	OAuth2ProviderGoogle OAuth2Provider = "google"
	OAuth2ProviderGitLab OAuth2Provider = "gitlab"
)

// OAuth2ProviderConfig OAuth2 프로바이더 설정
type OAuth2ProviderConfig struct {
	Name         string   `json:"name"` // 표시 이름
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"-"` // JSON 직렬화 제외
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	UserInfoURL  string   `json:"user_info_url"`
	Scopes       []string `json:"scopes"`
	RedirectURL  string   `json:"redirect_url"`
	Enabled      bool     `json:"enabled"`
}

// AvailableProvider 클라이언트에 노출되는 프로바이더 정보
type AvailableProvider struct {
	ID      OAuth2Provider `json:"id"`
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
}

// User 사용자 정의
type User struct {
	ID         string       `json:"id"`
	Email      string       `json:"email"`
	Name       string       `json:"name,omitempty"`
	Provider   AuthProvider `json:"provider"`
	ProviderID string       `json:"provider_id,omitempty"`
	Role       UserRole     `json:"role"`
	AvatarURL  string       `json:"avatar_url,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	LastLogin  *time.Time   `json:"last_login,omitempty"`
}

// AuthToken 인증 토큰
type AuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// AuthSession 인증 세션
type AuthSession struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      UserRole  `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginRequest 로그인 요청
type LoginRequest struct {
	Provider    AuthProvider `json:"provider"`
	RedirectURL string       `json:"redirect_url,omitempty"`
}

// LoginCallback 로그인 콜백
type LoginCallback struct {
	Provider AuthProvider `json:"provider"`
	Code     string       `json:"code"`
	State    string       `json:"state"`
}
