// Package secrets AWS Secrets Manager 제공자
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// AWSConfig AWS Secrets Manager 설정
type AWSConfig struct {
	Region          string `yaml:"region" json:"region"`
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key" json:"secret_access_key"`
	SessionToken    string `yaml:"session_token" json:"session_token"`
	Endpoint        string `yaml:"endpoint" json:"endpoint"` // LocalStack 등 커스텀 엔드포인트

	// 재시도 설정
	MaxRetries int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// AWSSecretsManagerProvider AWS Secrets Manager 제공자
type AWSSecretsManagerProvider struct {
	client     *secretsmanager.Client
	maxRetries int
	retryDelay time.Duration
}

// NewAWSSecretsManagerProvider AWS Secrets Manager 제공자 생성
func NewAWSSecretsManagerProvider(cfg *AWSConfig) (*AWSSecretsManagerProvider, error) {
	if cfg == nil {
		cfg = &AWSConfig{}
	}

	// Region 결정
	region := cfg.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1" // 기본값
	}

	// AWS SDK 설정 옵션
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(region))

	// 명시적 자격증명 사용
	accessKeyID := cfg.AccessKeyID
	if accessKeyID == "" {
		accessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	secretAccessKey := cfg.SecretAccessKey
	if secretAccessKey == "" {
		secretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}

	if accessKeyID != "" && secretAccessKey != "" {
		sessionToken := cfg.SessionToken
		if sessionToken == "" {
			sessionToken = os.Getenv("AWS_SESSION_TOKEN")
		}
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, sessionToken),
		))
	}
	// 그렇지 않으면 기본 자격증명 체인 사용 (IAM Role, ~/.aws/credentials 등)

	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Secrets Manager 클라이언트 생성
	var smOpts []func(*secretsmanager.Options)
	if cfg.Endpoint != "" {
		smOpts = append(smOpts, func(o *secretsmanager.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	client := secretsmanager.NewFromConfig(awsCfg, smOpts...)

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	return &AWSSecretsManagerProvider{
		client:     client,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}, nil
}

func (p *AWSSecretsManagerProvider) Name() string {
	return "aws_secrets_manager"
}

func (p *AWSSecretsManagerProvider) Get(ctx context.Context, key string) (string, error) {
	secret, err := p.GetWithMetadata(ctx, key)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

func (p *AWSSecretsManagerProvider) GetWithMetadata(ctx context.Context, key string) (*Secret, error) {
	// key 파싱: secret_name 또는 secret_name:field
	secretName := key
	field := ""
	if idx := strings.LastIndex(key, ":"); idx > 0 {
		secretName = key[:idx]
		field = key[idx+1:]
	}

	var lastErr error
	for i := 0; i < p.maxRetries; i++ {
		secretValue, versionID, err := p.getSecretValue(ctx, secretName)
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
			Version: versionID,
			Metadata: map[string]string{
				"source":      "aws_secrets_manager",
				"secret_name": secretName,
			},
		}, nil
	}

	return nil, fmt.Errorf("failed to get secret %s after %d retries: %w", key, p.maxRetries, lastErr)
}

func (p *AWSSecretsManagerProvider) getSecretValue(ctx context.Context, secretName string) (string, string, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	output, err := p.client.GetSecretValue(ctx, input)
	if err != nil {
		return "", "", fmt.Errorf("failed to get secret value: %w", err)
	}

	var secretValue string
	if output.SecretString != nil {
		secretValue = *output.SecretString
	} else if output.SecretBinary != nil {
		secretValue = string(output.SecretBinary)
	}

	var versionID string
	if output.VersionId != nil {
		versionID = *output.VersionId
	}

	return secretValue, versionID, nil
}

func (p *AWSSecretsManagerProvider) Close() error {
	// AWS SDK v2는 명시적 종료 불필요
	return nil
}
