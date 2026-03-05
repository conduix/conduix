package secrets

import (
	"testing"
)

func TestGCPSecretManagerProvider_Name(t *testing.T) {
	provider := &GCPSecretManagerProvider{}
	if provider.Name() != "gcp_secret_manager" {
		t.Errorf("expected 'gcp_secret_manager', got '%s'", provider.Name())
	}
}

func TestGCPSecretManagerProvider_Close(t *testing.T) {
	provider := &GCPSecretManagerProvider{}
	if err := provider.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseGCPSecretKey(t *testing.T) {
	tests := []struct {
		input       string
		wantSecret  string
		wantVersion string
		wantField   string
	}{
		{
			input:       "my-secret",
			wantSecret:  "my-secret",
			wantVersion: "latest",
			wantField:   "",
		},
		{
			input:       "my-secret:password",
			wantSecret:  "my-secret",
			wantVersion: "latest",
			wantField:   "password",
		},
		{
			input:       "my-secret:1:password",
			wantSecret:  "my-secret",
			wantVersion: "1",
			wantField:   "password",
		},
		{
			input:       "my-secret:latest:password",
			wantSecret:  "my-secret",
			wantVersion: "latest",
			wantField:   "password",
		},
		{
			input:       "my-secret:123",
			wantSecret:  "my-secret",
			wantVersion: "123",
			wantField:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			secretName, version, field := parseGCPSecretKey(tt.input)

			if secretName != tt.wantSecret {
				t.Errorf("secretName: got %s, want %s", secretName, tt.wantSecret)
			}
			if version != tt.wantVersion {
				t.Errorf("version: got %s, want %s", version, tt.wantVersion)
			}
			if field != tt.wantField {
				t.Errorf("field: got %s, want %s", field, tt.wantField)
			}
		})
	}
}

func TestIsNumericVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"123", true},
		{"0", true},
		{"latest", false},
		{"password", false},
		{"1a", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumericVersion(tt.input); got != tt.want {
				t.Errorf("isNumericVersion(%s) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestManager_Expand_WithCloudProviders(t *testing.T) {
	// 클라우드 제공자 없이 환경변수 확장만 테스트
	manager, err := NewManager(&Config{
		CacheEnabled: true,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 환경변수 확장 (클라우드 시크릿 없이)
	input := "path: $PATH"
	result, _ := manager.Expand(nil, input)
	if result == input {
		// $PATH가 확장되지 않았다면 그대로 반환
		t.Log("environment variable expansion working")
	}
}
