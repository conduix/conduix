package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// UsersConfig 사용자 설정
type UsersConfig struct {
	AdminEmails    []string `yaml:"admin_emails"`
	OperatorEmails []string `yaml:"operator_emails"`
}

// LoadUsersConfig 사용자 설정 파일 로드
func LoadUsersConfig(path string) (*UsersConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 파일이 없으면 빈 설정 반환
			return &UsersConfig{}, nil
		}
		return nil, err
	}

	var cfg UsersConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 이메일 정규화 (소문자, 공백 제거)
	for i, email := range cfg.AdminEmails {
		cfg.AdminEmails[i] = strings.TrimSpace(strings.ToLower(email))
	}
	for i, email := range cfg.OperatorEmails {
		cfg.OperatorEmails[i] = strings.TrimSpace(strings.ToLower(email))
	}

	return &cfg, nil
}

// LoadUsersConfigFromEnv 환경변수에서 사용자 설정 로드
// CONDUIX_ADMIN_EMAILS=email1,email2 형식
func LoadUsersConfigFromEnv() *UsersConfig {
	cfg := &UsersConfig{}

	if adminEmails := os.Getenv("CONDUIX_ADMIN_EMAILS"); adminEmails != "" {
		for _, email := range strings.Split(adminEmails, ",") {
			email = strings.TrimSpace(strings.ToLower(email))
			if email != "" {
				cfg.AdminEmails = append(cfg.AdminEmails, email)
			}
		}
	}

	if operatorEmails := os.Getenv("CONDUIX_OPERATOR_EMAILS"); operatorEmails != "" {
		for _, email := range strings.Split(operatorEmails, ",") {
			email = strings.TrimSpace(strings.ToLower(email))
			if email != "" {
				cfg.OperatorEmails = append(cfg.OperatorEmails, email)
			}
		}
	}

	return cfg
}

// Merge 두 설정 병합 (other가 우선)
func (c *UsersConfig) Merge(other *UsersConfig) {
	if other == nil {
		return
	}
	c.AdminEmails = append(c.AdminEmails, other.AdminEmails...)
	c.OperatorEmails = append(c.OperatorEmails, other.OperatorEmails...)
}

// IsAdmin 이메일이 관리자인지 확인
func (c *UsersConfig) IsAdmin(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	for _, adminEmail := range c.AdminEmails {
		if adminEmail == email {
			return true
		}
	}
	return false
}

// IsOperator 이메일이 운영자인지 확인
func (c *UsersConfig) IsOperator(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	for _, opEmail := range c.OperatorEmails {
		if opEmail == email {
			return true
		}
	}
	return false
}

// GetRole 이메일에 해당하는 역할 반환
func (c *UsersConfig) GetRole(email string) string {
	if c.IsAdmin(email) {
		return "admin"
	}
	if c.IsOperator(email) {
		return "operator"
	}
	return "viewer"
}
