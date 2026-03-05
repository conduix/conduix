// Package secrets GCP Secret Manager 제공자
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
)

// GCPConfig GCP Secret Manager 설정
type GCPConfig struct {
	ProjectID       string `yaml:"project_id" json:"project_id"`
	CredentialsFile string `yaml:"credentials_file" json:"credentials_file"`
	CredentialsJSON string `yaml:"credentials_json" json:"credentials_json"` // 인라인 JSON

	// 재시도 설정
	MaxRetries int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// GCPSecretManagerProvider GCP Secret Manager 제공자
type GCPSecretManagerProvider struct {
	client     *secretmanager.Client
	projectID  string
	maxRetries int
	retryDelay time.Duration
}

// NewGCPSecretManagerProvider GCP Secret Manager 제공자 생성
func NewGCPSecretManagerProvider(cfg *GCPConfig) (*GCPSecretManagerProvider, error) {
	if cfg == nil {
		cfg = &GCPConfig{}
	}

	// Project ID 결정
	projectID := cfg.ProjectID
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		projectID = os.Getenv("GCLOUD_PROJECT")
	}
	if projectID == "" {
		projectID = os.Getenv("GCP_PROJECT")
	}
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID not configured")
	}

	// 클라이언트 옵션
	var opts []option.ClientOption

	// 인증 설정
	if cfg.CredentialsJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.CredentialsJSON)))
	} else if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	} else if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		opts = append(opts, option.WithCredentialsFile(credFile))
	}
	// 그렇지 않으면 Application Default Credentials 사용

	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP Secret Manager client: %w", err)
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	return &GCPSecretManagerProvider{
		client:     client,
		projectID:  projectID,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}, nil
}

func (p *GCPSecretManagerProvider) Name() string {
	return "gcp_secret_manager"
}

func (p *GCPSecretManagerProvider) Get(ctx context.Context, key string) (string, error) {
	secret, err := p.GetWithMetadata(ctx, key)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

func (p *GCPSecretManagerProvider) GetWithMetadata(ctx context.Context, key string) (*Secret, error) {
	// key 파싱: secret_name 또는 secret_name:field 또는 secret_name:version:field
	secretName, version, field := parseGCPSecretKey(key)

	var lastErr error
	for i := 0; i < p.maxRetries; i++ {
		secretValue, versionName, err := p.accessSecretVersion(ctx, secretName, version)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.retryDelay * time.Duration(i+1)):
				continue
			}
		}

		// 필드 추출 (JSON 형식인 경우)
		var value string
		if field != "" {
			var data map[string]any
			if err := json.Unmarshal([]byte(secretValue), &data); err != nil {
				return nil, fmt.Errorf("secret %s is not valid JSON, cannot extract field %s", secretName, field)
			}
			val, ok := data[field]
			if !ok {
				return nil, fmt.Errorf("field %s not found in secret %s", field, secretName)
			}
			value = fmt.Sprintf("%v", val)
		} else {
			value = secretValue
		}

		return &Secret{
			Value:   value,
			Version: versionName,
			Metadata: map[string]string{
				"source":      "gcp_secret_manager",
				"secret_name": secretName,
				"project_id":  p.projectID,
			},
		}, nil
	}

	return nil, fmt.Errorf("failed to get secret %s after %d retries: %w", key, p.maxRetries, lastErr)
}

// parseGCPSecretKey 키를 secret_name, version, field로 파싱
// 형식:
//   - my-secret → (my-secret, latest, "")
//   - my-secret:password → (my-secret, latest, password)
//   - my-secret:1:password → (my-secret, 1, password)
//   - my-secret:latest:password → (my-secret, latest, password)
func parseGCPSecretKey(key string) (secretName, version, field string) {
	parts := strings.Split(key, ":")
	secretName = parts[0]
	version = "latest"

	switch len(parts) {
	case 1:
		// secret_name
		return secretName, version, ""
	case 2:
		// secret_name:field 또는 secret_name:version
		// 숫자이거나 "latest"면 버전으로 처리
		if parts[1] == "latest" || isNumericVersion(parts[1]) {
			return secretName, parts[1], ""
		}
		return secretName, version, parts[1]
	case 3:
		// secret_name:version:field
		return secretName, parts[1], parts[2]
	default:
		// 그 이상이면 마지막을 field로
		return secretName, parts[1], strings.Join(parts[2:], ":")
	}
}

func isNumericVersion(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func (p *GCPSecretManagerProvider) accessSecretVersion(ctx context.Context, secretName, version string) (string, string, error) {
	// 전체 리소스 이름 구성
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/%s", p.projectID, secretName, version)

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	}

	resp, err := p.client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("failed to access secret version: %w", err)
	}

	return string(resp.Payload.Data), resp.Name, nil
}

func (p *GCPSecretManagerProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
