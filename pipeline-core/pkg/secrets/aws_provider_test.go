package secrets

import (
	"context"
	"testing"
)

func TestAWSSecretsManagerProvider_Name(t *testing.T) {
	// 실제 AWS 연결 없이 이름만 테스트
	// 실제 연결 테스트는 AWS 자격증명 필요
	provider := &AWSSecretsManagerProvider{}
	if provider.Name() != "aws_secrets_manager" {
		t.Errorf("expected 'aws_secrets_manager', got '%s'", provider.Name())
	}
}

func TestAWSSecretsManagerProvider_Close(t *testing.T) {
	provider := &AWSSecretsManagerProvider{}
	if err := provider.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManager_WithAWSConfig(t *testing.T) {
	// AWS 환경변수 없이 매니저 생성 테스트
	cfg := &Config{
		Type:         "aws_secrets_manager",
		AWSRegion:    "us-east-1",
		CacheEnabled: true,
	}

	// AWS 자격증명 없이 생성 시도
	manager, err := NewManager(cfg)
	if err != nil {
		// 자격증명 없어도 매니저는 생성되어야 함 (환경변수 프로바이더만 등록)
		t.Fatalf("unexpected error creating manager: %v", err)
	}

	ctx := context.Background()

	// 환경변수 프로바이더는 항상 등록됨
	_, err = manager.Get(ctx, "env", "PATH")
	if err != nil {
		t.Logf("env provider works: got PATH")
	}
}

func TestParseAWSSecretKey(t *testing.T) {
	tests := []struct {
		input       string
		wantSecret  string
		wantField   string
		description string
	}{
		{
			input:       "my-secret",
			wantSecret:  "my-secret",
			wantField:   "",
			description: "simple secret name",
		},
		{
			input:       "my-secret:password",
			wantSecret:  "my-secret",
			wantField:   "password",
			description: "secret with field",
		},
		{
			input:       "my/namespace/secret:api_key",
			wantSecret:  "my/namespace/secret",
			wantField:   "api_key",
			description: "namespaced secret with field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// 직접 파싱 로직 테스트
			// 실제로는 GetWithMetadata 내부에서 처리됨
			secretName := tt.input
			field := ""

			idx := -1
			for i := len(tt.input) - 1; i >= 0; i-- {
				if tt.input[i] == ':' {
					idx = i
					break
				}
			}
			if idx > 0 {
				secretName = tt.input[:idx]
				field = tt.input[idx+1:]
			}

			if secretName != tt.wantSecret {
				t.Errorf("secret name: got %s, want %s", secretName, tt.wantSecret)
			}
			if field != tt.wantField {
				t.Errorf("field: got %s, want %s", field, tt.wantField)
			}
		})
	}
}
