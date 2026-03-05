// Package secrets 비밀 값 관리 (환경변수, Vault 등)
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Provider 비밀 값 제공자 인터페이스
type Provider interface {
	// Get 비밀 값 조회
	Get(ctx context.Context, key string) (string, error)
	// GetWithMetadata 메타데이터와 함께 비밀 값 조회
	GetWithMetadata(ctx context.Context, key string) (*Secret, error)
	// Name 제공자 이름
	Name() string
	// Close 리소스 정리
	Close() error
}

// Secret 비밀 값과 메타데이터
type Secret struct {
	Value     string
	Version   string
	CreatedAt time.Time
	ExpiresAt *time.Time
	Metadata  map[string]string
}

// Config 비밀 제공자 설정
type Config struct {
	// Provider 타입: env, vault, aws_secrets_manager, gcp_secret_manager
	Type string `yaml:"type" json:"type"`

	// Vault 설정
	VaultAddr      string `yaml:"vault_addr" json:"vault_addr"`
	VaultToken     string `yaml:"vault_token" json:"vault_token"`
	VaultPath      string `yaml:"vault_path" json:"vault_path"`           // KV 시크릿 경로 (예: secret/data/myapp)
	VaultNamespace string `yaml:"vault_namespace" json:"vault_namespace"` // Enterprise 네임스페이스

	// 캐시 설정
	CacheTTL     time.Duration `yaml:"cache_ttl" json:"cache_ttl"`
	CacheEnabled bool          `yaml:"cache_enabled" json:"cache_enabled"`

	// 재시도 설정
	MaxRetries int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// secretPattern ${provider:path} 형식 매칭
// 예: ${vault:secret/data/myapp/db_password}
// 예: ${env:DB_PASSWORD}
// 예: ${DB_PASSWORD} (환경변수 기본)
var secretPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Manager 비밀 관리자 (여러 Provider 통합)
type Manager struct {
	providers map[string]Provider
	cache     *secretCache
	cacheTTL  time.Duration
	mu        sync.RWMutex
}

// secretCache 비밀 캐시
type secretCache struct {
	data map[string]cachedSecret
	mu   sync.RWMutex
}

type cachedSecret struct {
	secret    *Secret
	expiresAt time.Time
}

// NewManager 비밀 관리자 생성
func NewManager(cfg *Config) (*Manager, error) {
	m := &Manager{
		providers: make(map[string]Provider),
		cache: &secretCache{
			data: make(map[string]cachedSecret),
		},
		cacheTTL: 5 * time.Minute,
	}

	if cfg != nil {
		if cfg.CacheTTL > 0 {
			m.cacheTTL = cfg.CacheTTL
		}
	}

	// 기본 환경변수 제공자 등록
	m.providers["env"] = &EnvProvider{}

	// Vault 제공자 등록 (설정이 있는 경우)
	if cfg != nil && cfg.Type == "vault" && cfg.VaultAddr != "" {
		vault, err := NewVaultProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Vault provider: %w", err)
		}
		m.providers["vault"] = vault
	}

	return m, nil
}

// RegisterProvider 제공자 등록
func (m *Manager) RegisterProvider(name string, provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[name] = provider
}

// Get 비밀 값 조회 (캐시 사용)
func (m *Manager) Get(ctx context.Context, providerName, key string) (string, error) {
	cacheKey := fmt.Sprintf("%s:%s", providerName, key)

	// 캐시 확인
	if secret := m.cache.get(cacheKey); secret != nil {
		return secret.Value, nil
	}

	// 제공자에서 조회
	m.mu.RLock()
	provider, ok := m.providers[providerName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("unknown provider: %s", providerName)
	}

	secret, err := provider.GetWithMetadata(ctx, key)
	if err != nil {
		return "", err
	}

	// 캐시에 저장
	m.cache.set(cacheKey, secret, m.cacheTTL)

	return secret.Value, nil
}

// Expand 문자열 내 비밀 참조 확장
// 지원 형식:
//   - ${vault:secret/data/myapp/db_password}
//   - ${env:DB_PASSWORD}
//   - ${DB_PASSWORD} (환경변수 기본)
func (m *Manager) Expand(ctx context.Context, s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}

	var lastErr error
	result := secretPattern.ReplaceAllStringFunc(s, func(match string) string {
		// ${...} 에서 내용 추출
		content := match[2 : len(match)-1]

		var providerName, key string
		if idx := strings.Index(content, ":"); idx > 0 {
			providerName = content[:idx]
			key = content[idx+1:]
		} else {
			// 기본값: 환경변수
			providerName = "env"
			key = content
		}

		value, err := m.Get(ctx, providerName, key)
		if err != nil {
			lastErr = err
			return match // 실패 시 원본 유지
		}
		return value
	})

	if lastErr != nil {
		return result, lastErr
	}
	return result, nil
}

// ExpandEnvVars 환경변수만 확장 (기존 호환)
func ExpandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.ExpandEnv(s)
}

// Close 모든 제공자 정리
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, p := range m.providers {
		if err := p.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing providers: %v", errs)
	}
	return nil
}

// secretCache 메서드들
func (c *secretCache) get(key string) *Secret {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.data[key]
	if !ok {
		return nil
	}

	if time.Now().After(cached.expiresAt) {
		return nil
	}

	return cached.secret
}

func (c *secretCache) set(key string, secret *Secret, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = cachedSecret{
		secret:    secret,
		expiresAt: time.Now().Add(ttl),
	}
}

// EnvProvider 환경변수 제공자
type EnvProvider struct{}

func (p *EnvProvider) Name() string {
	return "env"
}

func (p *EnvProvider) Get(ctx context.Context, key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable %s not set", key)
	}
	return value, nil
}

func (p *EnvProvider) GetWithMetadata(ctx context.Context, key string) (*Secret, error) {
	value, err := p.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Secret{
		Value:    value,
		Metadata: map[string]string{"source": "env"},
	}, nil
}

func (p *EnvProvider) Close() error {
	return nil
}

// VaultProvider HashiCorp Vault 제공자
type VaultProvider struct {
	addr       string
	token      string
	namespace  string
	basePath   string
	httpClient *http.Client
	maxRetries int
	retryDelay time.Duration
}

// NewVaultProvider Vault 제공자 생성
func NewVaultProvider(cfg *Config) (*VaultProvider, error) {
	addr := cfg.VaultAddr
	if addr == "" {
		addr = os.Getenv("VAULT_ADDR")
	}
	if addr == "" {
		return nil, fmt.Errorf("vault address not configured")
	}

	token := cfg.VaultToken
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("vault token not configured")
	}

	namespace := cfg.VaultNamespace
	if namespace == "" {
		namespace = os.Getenv("VAULT_NAMESPACE")
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	return &VaultProvider{
		addr:      addr,
		token:     token,
		namespace: namespace,
		basePath:  cfg.VaultPath,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}, nil
}

func (p *VaultProvider) Name() string {
	return "vault"
}

func (p *VaultProvider) Get(ctx context.Context, key string) (string, error) {
	secret, err := p.GetWithMetadata(ctx, key)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

func (p *VaultProvider) GetWithMetadata(ctx context.Context, key string) (*Secret, error) {
	// 경로 파싱: secret/data/myapp/db_password 또는 key만 전달
	path := key
	field := ""

	// 마지막 세그먼트를 필드로 사용 (슬래시가 없으면 basePath 사용)
	if !strings.Contains(key, "/") && p.basePath != "" {
		path = p.basePath
		field = key
	} else if idx := strings.LastIndex(key, "/"); idx >= 0 {
		// 경로에서 필드 추출 시도 (예: secret/data/myapp/db_password)
		possiblePath := key[:idx]
		possibleField := key[idx+1:]
		path = possiblePath
		field = possibleField
	}

	var lastErr error
	for i := 0; i < p.maxRetries; i++ {
		data, version, err := p.readSecret(ctx, path)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.retryDelay * time.Duration(i+1)):
				continue
			}
		}

		// 특정 필드 추출
		var value string
		if field != "" {
			val, ok := data[field]
			if !ok {
				return nil, fmt.Errorf("field %s not found in secret %s", field, path)
			}
			value = fmt.Sprintf("%v", val)
		} else {
			// 전체 데이터를 JSON으로 반환
			jsonData, err := json.Marshal(data)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal secret data: %w", err)
			}
			value = string(jsonData)
		}

		return &Secret{
			Value:    value,
			Version:  version,
			Metadata: map[string]string{"source": "vault", "path": path},
		}, nil
	}

	return nil, fmt.Errorf("failed to read secret after %d retries: %w", p.maxRetries, lastErr)
}

func (p *VaultProvider) readSecret(ctx context.Context, path string) (map[string]any, string, error) {
	// KV v2 API 경로 구성
	url := fmt.Sprintf("%s/v1/%s", strings.TrimRight(p.addr, "/"), path)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("X-Vault-Token", p.token)
	if p.namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.namespace)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode vault response: %w", err)
	}

	version := fmt.Sprintf("%d", result.Data.Metadata.Version)
	return result.Data.Data, version, nil
}

func (p *VaultProvider) Close() error {
	return nil
}

// Global 기본 매니저 (편의를 위한 싱글톤)
var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
	defaultManagerErr  error
)

// GetDefaultManager 기본 비밀 관리자 반환
func GetDefaultManager() (*Manager, error) {
	defaultManagerOnce.Do(func() {
		cfg := &Config{
			CacheEnabled: true,
			CacheTTL:     5 * time.Minute,
		}

		// 환경변수에서 Vault 설정 확인
		if vaultAddr := os.Getenv("VAULT_ADDR"); vaultAddr != "" {
			cfg.Type = "vault"
			cfg.VaultAddr = vaultAddr
		}

		defaultManager, defaultManagerErr = NewManager(cfg)
	})
	return defaultManager, defaultManagerErr
}

// Expand 기본 매니저를 사용하여 문자열 확장 (편의 함수)
func Expand(ctx context.Context, s string) (string, error) {
	manager, err := GetDefaultManager()
	if err != nil {
		// Vault 연결 실패 시 환경변수만 사용
		return ExpandEnvVars(s), nil
	}
	return manager.Expand(ctx, s)
}
