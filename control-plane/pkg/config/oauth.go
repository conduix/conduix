package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// OAuthProviderConfig OAuth 프로바이더 설정
type OAuthProviderConfig struct {
	Name        string            `yaml:"name" json:"name"`
	AuthURL     string            `yaml:"auth_url" json:"auth_url"`
	TokenURL    string            `yaml:"token_url" json:"token_url"`
	UserInfoURL string            `yaml:"user_info_url" json:"user_info_url"`
	Scopes      []string          `yaml:"scopes" json:"scopes"`
	UserMapping UserMappingConfig `yaml:"user_mapping" json:"user_mapping"`
	// 환경변수에서 로드되는 값들
	ClientID     string `yaml:"-" json:"-"`
	ClientSecret string `yaml:"-" json:"-"`
	RedirectURL  string `yaml:"-" json:"-"`
}

// UserMappingConfig 사용자 정보 매핑 설정
// JSON path 형식으로 응답에서 값을 추출 (예: "response.email", "data.user.name")
type UserMappingConfig struct {
	ID     string `yaml:"id" json:"id"`         // 사용자 고유 ID 경로
	Email  string `yaml:"email" json:"email"`   // 이메일 경로
	Name   string `yaml:"name" json:"name"`     // 이름 경로
	Avatar string `yaml:"avatar" json:"avatar"` // 아바타 URL 경로
}

// OAuthConfig 전체 OAuth 설정
type OAuthConfig struct {
	Providers map[string]*OAuthProviderConfig `yaml:"providers" json:"providers"`
}

// LoadOAuthConfig YAML 파일에서 OAuth 설정 로드
func LoadOAuthConfig(path string) (*OAuthConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read oauth config file: %w", err)
	}

	var config OAuthConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse oauth config: %w", err)
	}

	return &config, nil
}

// LoadOAuthConfigWithEnv OAuth 설정 로드 + 환경변수에서 credentials 주입
func LoadOAuthConfigWithEnv(path string, defaultRedirectURL string) (*OAuthConfig, error) {
	config, err := LoadOAuthConfig(path)
	if err != nil {
		return nil, err
	}

	// 각 프로바이더에 환경변수에서 credentials 주입
	for id, provider := range config.Providers {
		upperID := strings.ToUpper(id)
		provider.ClientID = os.Getenv(fmt.Sprintf("%s_CLIENT_ID", upperID))
		provider.ClientSecret = os.Getenv(fmt.Sprintf("%s_CLIENT_SECRET", upperID))
		provider.RedirectURL = os.Getenv(fmt.Sprintf("%s_REDIRECT_URL", upperID))
		if provider.RedirectURL == "" {
			provider.RedirectURL = defaultRedirectURL
		}
	}

	return config, nil
}

// GetEnabledProviders Client ID가 설정된 (활성화된) 프로바이더만 반환
func (c *OAuthConfig) GetEnabledProviders() map[string]*OAuthProviderConfig {
	enabled := make(map[string]*OAuthProviderConfig)
	for id, provider := range c.Providers {
		if provider.ClientID != "" && provider.ClientSecret != "" {
			enabled[id] = provider
		}
	}
	return enabled
}

// GetDefaultOAuthConfig 기본 OAuth 프로바이더 설정 반환 (설정 파일 없을 때 사용)
func GetDefaultOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		Providers: map[string]*OAuthProviderConfig{
			"github": {
				Name:        "GitHub",
				AuthURL:     "https://github.com/login/oauth/authorize",
				TokenURL:    "https://github.com/login/oauth/access_token",
				UserInfoURL: "https://api.github.com/user",
				Scopes:      []string{"user:email", "read:user"},
				UserMapping: UserMappingConfig{
					ID:     "id",
					Email:  "email",
					Name:   "name",
					Avatar: "avatar_url",
				},
			},
			"google": {
				Name:        "Google",
				AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL:    "https://oauth2.googleapis.com/token",
				UserInfoURL: "https://www.googleapis.com/oauth2/v2/userinfo",
				Scopes:      []string{"openid", "profile", "email"},
				UserMapping: UserMappingConfig{
					ID:     "id",
					Email:  "email",
					Name:   "name",
					Avatar: "picture",
				},
			},
			"naver": {
				Name:        "Naver",
				AuthURL:     "https://nid.naver.com/oauth2.0/authorize",
				TokenURL:    "https://nid.naver.com/oauth2.0/token",
				UserInfoURL: "https://openapi.naver.com/v1/nid/me",
				Scopes:      []string{},
				UserMapping: UserMappingConfig{
					ID:     "response.id",
					Email:  "response.email",
					Name:   "response.name",
					Avatar: "response.profile_image",
				},
			},
		},
	}
}
