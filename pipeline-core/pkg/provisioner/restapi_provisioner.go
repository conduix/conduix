package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"
)

// RestAPIProvisioner REST API 엔드포인트 설정 Provisioner
type RestAPIProvisioner struct {
	sinkType types.SinkType
}

// NewRestAPIProvisioner REST API Provisioner 생성
func NewRestAPIProvisioner() *RestAPIProvisioner {
	return &RestAPIProvisioner{
		sinkType: types.SinkTypeRestAPI,
	}
}

func (p *RestAPIProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *RestAPIProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{
		types.ProvisioningTypeAPISetup,
		types.ProvisioningTypeExternal,
	}
}

// RestAPIProvisioningConfig REST API 프로비저닝 설정
type RestAPIProvisioningConfig struct {
	BaseURL          string            `json:"base_url"`          // API 기본 URL
	Endpoint         string            `json:"endpoint"`          // 엔드포인트 경로
	Method           string            `json:"method"`            // HTTP 메서드 (POST, PUT 등)
	AuthType         string            `json:"auth_type"`         // 인증 타입: none, basic, bearer, api_key
	AuthConfig       map[string]string `json:"auth_config"`       // 인증 설정
	Headers          map[string]string `json:"headers"`           // 커스텀 헤더
	ValidateEndpoint string            `json:"validate_endpoint"` // 검증용 엔드포인트 (선택)
	Timeout          int               `json:"timeout"`           // 타임아웃 (초)
	RetryCount       int               `json:"retry_count"`       // 재시도 횟수
	RetryDelay       int               `json:"retry_delay"`       // 재시도 간격 (초)
}

func (p *RestAPIProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 프로비저닝인 경우
	if req.Type == types.ProvisioningTypeExternal {
		return &types.ProvisioningResult{
			ID:         uuid.New().String(),
			RequestID:  req.ID,
			PipelineID: req.PipelineID,
			SinkType:   req.SinkType,
			Status:     types.ProvisioningStatusPending,
			Message:    "Waiting for external REST API provisioning",
			Metadata: map[string]any{
				"external_url": req.ExternalURL,
				"callback_url": req.CallbackURL,
			},
		}, nil
	}

	// 설정 파싱
	config, err := p.parseConfig(req.Config)
	if err != nil {
		return nil, fmt.Errorf("invalid rest api config: %w", err)
	}

	// API 엔드포인트 검증
	if err := p.validateEndpoint(ctx, config); err != nil {
		now := time.Now()
		return &types.ProvisioningResult{
			ID:          uuid.New().String(),
			RequestID:   req.ID,
			PipelineID:  req.PipelineID,
			SinkType:    req.SinkType,
			Status:      types.ProvisioningStatusFailed,
			Message:     "Failed to validate REST API endpoint",
			ErrorDetail: err.Error(),
			CompletedAt: &now,
		}, nil
	}

	now := time.Now()
	apiEndpoint := config.BaseURL + config.Endpoint

	return &types.ProvisioningResult{
		ID:          uuid.New().String(),
		RequestID:   req.ID,
		PipelineID:  req.PipelineID,
		SinkType:    req.SinkType,
		Status:      types.ProvisioningStatusCompleted,
		APIEndpoint: apiEndpoint,
		Message:     fmt.Sprintf("REST API endpoint '%s' validated successfully", apiEndpoint),
		Metadata: map[string]any{
			"base_url":  config.BaseURL,
			"endpoint":  config.Endpoint,
			"method":    config.Method,
			"auth_type": config.AuthType,
		},
		CompletedAt: &now,
	}, nil
}

func (p *RestAPIProvisioner) parseConfig(config map[string]any) (*RestAPIProvisioningConfig, error) {
	cfg := &RestAPIProvisioningConfig{
		Method:     "POST",
		AuthType:   "none",
		Timeout:    30,
		RetryCount: 3,
		RetryDelay: 1,
		Headers:    make(map[string]string),
		AuthConfig: make(map[string]string),
	}

	// BaseURL
	if baseURL, ok := config["base_url"].(string); ok && baseURL != "" {
		cfg.BaseURL = baseURL
	} else {
		return nil, fmt.Errorf("base_url is required")
	}

	// Endpoint
	if endpoint, ok := config["endpoint"].(string); ok && endpoint != "" {
		cfg.Endpoint = endpoint
	} else {
		return nil, fmt.Errorf("endpoint is required")
	}

	// Method
	if method, ok := config["method"].(string); ok && method != "" {
		cfg.Method = method
	}

	// AuthType
	if authType, ok := config["auth_type"].(string); ok {
		cfg.AuthType = authType
	}

	// AuthConfig
	if authConfig, ok := config["auth_config"].(map[string]any); ok {
		for k, v := range authConfig {
			if vs, ok := v.(string); ok {
				cfg.AuthConfig[k] = vs
			}
		}
	}

	// Headers
	if headers, ok := config["headers"].(map[string]any); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				cfg.Headers[k] = vs
			}
		}
	}

	// ValidateEndpoint
	if validateEndpoint, ok := config["validate_endpoint"].(string); ok {
		cfg.ValidateEndpoint = validateEndpoint
	}

	// Timeout
	if timeout, ok := config["timeout"].(float64); ok {
		cfg.Timeout = int(timeout)
	} else if timeout, ok := config["timeout"].(int); ok {
		cfg.Timeout = timeout
	}

	// RetryCount
	if retryCount, ok := config["retry_count"].(float64); ok {
		cfg.RetryCount = int(retryCount)
	} else if retryCount, ok := config["retry_count"].(int); ok {
		cfg.RetryCount = retryCount
	}

	// RetryDelay
	if retryDelay, ok := config["retry_delay"].(float64); ok {
		cfg.RetryDelay = int(retryDelay)
	} else if retryDelay, ok := config["retry_delay"].(int); ok {
		cfg.RetryDelay = retryDelay
	}

	return cfg, nil
}

func (p *RestAPIProvisioner) createHTTPClient(config *RestAPIProvisioningConfig) *http.Client {
	return &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}
}

func (p *RestAPIProvisioner) createRequest(ctx context.Context, method, url string, body []byte, config *RestAPIProvisioningConfig) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	// 기본 헤더
	req.Header.Set("Content-Type", "application/json")

	// 커스텀 헤더 설정
	for k, v := range config.Headers {
		req.Header.Set(k, v)
	}

	// 인증 설정
	switch config.AuthType {
	case "basic":
		username := config.AuthConfig["username"]
		password := config.AuthConfig["password"]
		req.SetBasicAuth(username, password)
	case "bearer":
		token := config.AuthConfig["token"]
		req.Header.Set("Authorization", "Bearer "+token)
	case "api_key":
		headerName := config.AuthConfig["header_name"]
		if headerName == "" {
			headerName = "X-API-Key"
		}
		apiKey := config.AuthConfig["api_key"]
		req.Header.Set(headerName, apiKey)
	}

	return req, nil
}

func (p *RestAPIProvisioner) validateEndpoint(ctx context.Context, config *RestAPIProvisioningConfig) error {
	client := p.createHTTPClient(config)

	// 검증용 엔드포인트가 있으면 해당 엔드포인트로 검증
	validateURL := config.BaseURL
	if config.ValidateEndpoint != "" {
		validateURL += config.ValidateEndpoint
	}

	var lastErr error
	for i := 0; i <= config.RetryCount; i++ {
		if i > 0 {
			time.Sleep(time.Duration(config.RetryDelay) * time.Second)
		}

		req, err := p.createRequest(ctx, "GET", validateURL, nil, config)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to connect to API: %w", err)
			continue
		}
		resp.Body.Close()

		// 2xx 또는 404 (엔드포인트 특성상 GET이 없을 수 있음)는 성공으로 처리
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if resp.StatusCode == 404 && config.ValidateEndpoint == "" {
			// 기본 URL만 체크한 경우 404도 서버가 살아있다는 의미
			return nil
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return fmt.Errorf("authentication failed: %s", resp.Status)
		}

		lastErr = fmt.Errorf("unexpected status code: %s", resp.Status)
	}

	return lastErr
}

func (p *RestAPIProvisioner) Validate(config map[string]any) error {
	_, err := p.parseConfig(config)
	return err
}

func (p *RestAPIProvisioner) RequiresExternalSetup() bool {
	return false // 자동 검증 가능
}

func (p *RestAPIProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return req.ExternalURL
}

// TestEndpoint 엔드포인트 테스트 (실제 데이터 전송 테스트)
func (p *RestAPIProvisioner) TestEndpoint(ctx context.Context, config *RestAPIProvisioningConfig, testPayload map[string]any) error {
	client := p.createHTTPClient(config)

	url := config.BaseURL + config.Endpoint

	body, err := json.Marshal(testPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal test payload: %w", err)
	}

	req, err := p.createRequest(ctx, config.Method, url, body, config)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send test request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("test request failed: %s - %s", resp.Status, string(bodyBytes))
	}

	return nil
}

// GetEndpointInfo 엔드포인트 정보 조회
func (p *RestAPIProvisioner) GetEndpointInfo(config *RestAPIProvisioningConfig) map[string]any {
	return map[string]any{
		"full_url":  config.BaseURL + config.Endpoint,
		"method":    config.Method,
		"auth_type": config.AuthType,
		"timeout":   config.Timeout,
	}
}
