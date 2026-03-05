package secrets

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEnvProvider_Get(t *testing.T) {
	provider := &EnvProvider{}

	// 테스트용 환경변수 설정
	if err := os.Setenv("TEST_SECRET_KEY", "test_secret_value"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SECRET_KEY") }()

	ctx := context.Background()

	// 정상 조회
	value, err := provider.Get(ctx, "TEST_SECRET_KEY")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if value != "test_secret_value" {
		t.Errorf("expected 'test_secret_value', got '%s'", value)
	}

	// 존재하지 않는 키
	_, err = provider.Get(ctx, "NON_EXISTENT_KEY_12345")
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestEnvProvider_GetWithMetadata(t *testing.T) {
	provider := &EnvProvider{}

	if err := os.Setenv("TEST_META_KEY", "meta_value"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_META_KEY") }()

	ctx := context.Background()
	secret, err := provider.GetWithMetadata(ctx, "TEST_META_KEY")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if secret.Value != "meta_value" {
		t.Errorf("expected 'meta_value', got '%s'", secret.Value)
	}
	if secret.Metadata["source"] != "env" {
		t.Errorf("expected source 'env', got '%s'", secret.Metadata["source"])
	}
}

func TestManager_Expand(t *testing.T) {
	manager, err := NewManager(&Config{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 테스트용 환경변수 설정
	if err := os.Setenv("DB_HOST", "localhost"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("DB_PORT", "5432"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv("DB_HOST") }()
	defer func() { _ = os.Unsetenv("DB_PORT") }()

	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no expansion needed",
			input:    "plain string",
			expected: "plain string",
		},
		{
			name:     "single env var",
			input:    "host: ${DB_HOST}",
			expected: "host: localhost",
		},
		{
			name:     "multiple env vars",
			input:    "jdbc:postgresql://${DB_HOST}:${DB_PORT}/mydb",
			expected: "jdbc:postgresql://localhost:5432/mydb",
		},
		{
			name:     "explicit env provider",
			input:    "host: ${env:DB_HOST}",
			expected: "host: localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.Expand(ctx, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestExpandEnvVars(t *testing.T) {
	if err := os.Setenv("EXPAND_TEST", "expanded_value"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv("EXPAND_TEST") }()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "plain text",
			expected: "plain text",
		},
		{
			input:    "value: ${EXPAND_TEST}",
			expected: "value: expanded_value",
		},
		{
			input:    "${EXPAND_TEST}_suffix",
			expected: "expanded_value_suffix",
		},
	}

	for _, tt := range tests {
		result := ExpandEnvVars(tt.input)
		if result != tt.expected {
			t.Errorf("input '%s': expected '%s', got '%s'", tt.input, tt.expected, result)
		}
	}
}

func TestSecretCache(t *testing.T) {
	cache := &secretCache{
		data: make(map[string]cachedSecret),
	}

	// 캐시에 값 저장
	secret := &Secret{
		Value:    "cached_value",
		Metadata: map[string]string{"test": "true"},
	}
	cache.set("test_key", secret, 100*time.Millisecond)

	// 캐시에서 값 조회
	cached := cache.get("test_key")
	if cached == nil {
		t.Fatal("expected cached value, got nil")
	}
	if cached.Value != "cached_value" {
		t.Errorf("expected 'cached_value', got '%s'", cached.Value)
	}

	// TTL 만료 후 조회
	time.Sleep(150 * time.Millisecond)
	expired := cache.get("test_key")
	if expired != nil {
		t.Error("expected nil after TTL expiration")
	}
}

func TestManager_RegisterProvider(t *testing.T) {
	manager, err := NewManager(nil)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 커스텀 제공자 등록
	customProvider := &EnvProvider{}
	manager.RegisterProvider("custom", customProvider)

	// 등록된 제공자 확인
	if err := os.Setenv("CUSTOM_KEY", "custom_value"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() { _ = os.Unsetenv("CUSTOM_KEY") }()

	ctx := context.Background()
	value, err := manager.Get(ctx, "custom", "CUSTOM_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "custom_value" {
		t.Errorf("expected 'custom_value', got '%s'", value)
	}
}

func TestManager_UnknownProvider(t *testing.T) {
	manager, err := NewManager(nil)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.Get(ctx, "unknown_provider", "key")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}
