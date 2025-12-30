package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"
)

// ElasticsearchProvisioner Elasticsearch 인덱스 생성 Provisioner
type ElasticsearchProvisioner struct {
	sinkType types.SinkType
}

// NewElasticsearchProvisioner Elasticsearch Provisioner 생성
func NewElasticsearchProvisioner() *ElasticsearchProvisioner {
	return &ElasticsearchProvisioner{
		sinkType: types.SinkTypeElastic,
	}
}

func (p *ElasticsearchProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *ElasticsearchProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{
		types.ProvisioningTypeIndexCreation,
		types.ProvisioningTypeExternal,
	}
}

// ElasticsearchProvisioningConfig Elasticsearch 프로비저닝 설정
type ElasticsearchProvisioningConfig struct {
	Addresses        []string       `json:"addresses"`          // ES 주소 목록
	Username         string         `json:"username"`           // 사용자명
	Password         string         `json:"password"`           // 비밀번호
	APIKey           string         `json:"api_key"`            // API Key (username/password 대신)
	IndexName        string         `json:"index_name"`         // 인덱스 이름
	NumberOfShards   int            `json:"number_of_shards"`   // 샤드 수
	NumberOfReplicas int            `json:"number_of_replicas"` // 레플리카 수
	Mappings         map[string]any `json:"mappings"`           // 매핑 설정
	Settings         map[string]any `json:"settings"`           // 인덱스 설정
	Aliases          []string       `json:"aliases"`            // 별칭
}

func (p *ElasticsearchProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 프로비저닝인 경우
	if req.Type == types.ProvisioningTypeExternal {
		return &types.ProvisioningResult{
			ID:         uuid.New().String(),
			RequestID:  req.ID,
			PipelineID: req.PipelineID,
			SinkType:   req.SinkType,
			Status:     types.ProvisioningStatusPending,
			Message:    "Waiting for external Elasticsearch index provisioning",
			Metadata: map[string]any{
				"external_url": req.ExternalURL,
				"callback_url": req.CallbackURL,
			},
		}, nil
	}

	// 설정 파싱
	config, err := p.parseConfig(req.Config)
	if err != nil {
		return nil, fmt.Errorf("invalid elasticsearch config: %w", err)
	}

	// 인덱스 생성
	if err := p.createIndex(ctx, config); err != nil {
		now := time.Now()
		return &types.ProvisioningResult{
			ID:          uuid.New().String(),
			RequestID:   req.ID,
			PipelineID:  req.PipelineID,
			SinkType:    req.SinkType,
			Status:      types.ProvisioningStatusFailed,
			Message:     "Failed to create Elasticsearch index",
			ErrorDetail: err.Error(),
			CompletedAt: &now,
		}, nil
	}

	now := time.Now()
	return &types.ProvisioningResult{
		ID:         uuid.New().String(),
		RequestID:  req.ID,
		PipelineID: req.PipelineID,
		SinkType:   req.SinkType,
		Status:     types.ProvisioningStatusCompleted,
		IndexName:  config.IndexName,
		Message:    fmt.Sprintf("Elasticsearch index '%s' created successfully", config.IndexName),
		Metadata: map[string]any{
			"addresses":          config.Addresses,
			"number_of_shards":   config.NumberOfShards,
			"number_of_replicas": config.NumberOfReplicas,
			"aliases":            config.Aliases,
		},
		CompletedAt: &now,
	}, nil
}

func (p *ElasticsearchProvisioner) parseConfig(config map[string]any) (*ElasticsearchProvisioningConfig, error) {
	cfg := &ElasticsearchProvisioningConfig{
		NumberOfShards:   1,
		NumberOfReplicas: 0,
	}

	// Addresses
	if addresses, ok := config["addresses"].([]any); ok {
		for _, addr := range addresses {
			if addrStr, ok := addr.(string); ok {
				cfg.Addresses = append(cfg.Addresses, addrStr)
			}
		}
	} else if addresses, ok := config["addresses"].([]string); ok {
		cfg.Addresses = addresses
	}
	if len(cfg.Addresses) == 0 {
		return nil, fmt.Errorf("addresses is required")
	}

	// Username
	if username, ok := config["username"].(string); ok {
		cfg.Username = username
	}

	// Password
	if password, ok := config["password"].(string); ok {
		cfg.Password = password
	}

	// APIKey
	if apiKey, ok := config["api_key"].(string); ok {
		cfg.APIKey = apiKey
	}

	// IndexName
	if indexName, ok := config["index_name"].(string); ok && indexName != "" {
		cfg.IndexName = indexName
	} else {
		return nil, fmt.Errorf("index_name is required")
	}

	// NumberOfShards
	if shards, ok := config["number_of_shards"].(float64); ok {
		cfg.NumberOfShards = int(shards)
	} else if shards, ok := config["number_of_shards"].(int); ok {
		cfg.NumberOfShards = shards
	}

	// NumberOfReplicas
	if replicas, ok := config["number_of_replicas"].(float64); ok {
		cfg.NumberOfReplicas = int(replicas)
	} else if replicas, ok := config["number_of_replicas"].(int); ok {
		cfg.NumberOfReplicas = replicas
	}

	// Mappings
	if mappings, ok := config["mappings"].(map[string]any); ok {
		cfg.Mappings = mappings
	}

	// Settings
	if settings, ok := config["settings"].(map[string]any); ok {
		cfg.Settings = settings
	}

	// Aliases
	if aliases, ok := config["aliases"].([]any); ok {
		for _, alias := range aliases {
			if aliasStr, ok := alias.(string); ok {
				cfg.Aliases = append(cfg.Aliases, aliasStr)
			}
		}
	}

	return cfg, nil
}

func (p *ElasticsearchProvisioner) createClient(config *ElasticsearchProvisioningConfig) (*elasticsearch.Client, error) {
	esCfg := elasticsearch.Config{
		Addresses: config.Addresses,
	}

	if config.APIKey != "" {
		esCfg.APIKey = config.APIKey
	} else if config.Username != "" && config.Password != "" {
		esCfg.Username = config.Username
		esCfg.Password = config.Password
	}

	return elasticsearch.NewClient(esCfg)
}

func (p *ElasticsearchProvisioner) createIndex(ctx context.Context, config *ElasticsearchProvisioningConfig) error {
	client, err := p.createClient(config)
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// 인덱스 설정 구성
	indexBody := map[string]any{
		"settings": map[string]any{
			"number_of_shards":   config.NumberOfShards,
			"number_of_replicas": config.NumberOfReplicas,
		},
	}

	// 추가 설정 병합
	if config.Settings != nil {
		settings := indexBody["settings"].(map[string]any)
		for k, v := range config.Settings {
			settings[k] = v
		}
	}

	// 매핑 설정
	if config.Mappings != nil {
		indexBody["mappings"] = config.Mappings
	} else {
		// 기본 매핑
		indexBody["mappings"] = p.getDefaultMappings()
	}

	// 별칭 설정
	if len(config.Aliases) > 0 {
		aliases := make(map[string]any)
		for _, alias := range config.Aliases {
			aliases[alias] = map[string]any{}
		}
		indexBody["aliases"] = aliases
	}

	// JSON 변환
	body, err := json.Marshal(indexBody)
	if err != nil {
		return fmt.Errorf("failed to marshal index body: %w", err)
	}

	// 인덱스 생성 요청
	res, err := client.Indices.Create(
		config.IndexName,
		client.Indices.Create.WithContext(ctx),
		client.Indices.Create.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		var errResp map[string]any
		if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
			return fmt.Errorf("failed to create index: %s", res.Status())
		}
		// 이미 존재하는 경우 무시
		if errType, ok := errResp["error"].(map[string]any)["type"].(string); ok {
			if errType == "resource_already_exists_exception" {
				return nil
			}
		}
		return fmt.Errorf("failed to create index: %v", errResp)
	}

	return nil
}

func (p *ElasticsearchProvisioner) getDefaultMappings() map[string]any {
	return map[string]any{
		"properties": map[string]any{
			"id": map[string]any{
				"type": "keyword",
			},
			"data": map[string]any{
				"type":    "object",
				"enabled": true,
			},
			"source": map[string]any{
				"type": "keyword",
			},
			"pipeline_id": map[string]any{
				"type": "keyword",
			},
			"created_at": map[string]any{
				"type": "date",
			},
			"@timestamp": map[string]any{
				"type": "date",
			},
		},
	}
}

func (p *ElasticsearchProvisioner) Validate(config map[string]any) error {
	_, err := p.parseConfig(config)
	return err
}

func (p *ElasticsearchProvisioner) RequiresExternalSetup() bool {
	return false // 자동 생성 가능
}

func (p *ElasticsearchProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return req.ExternalURL
}

// CheckIndexExists 인덱스 존재 여부 확인
func (p *ElasticsearchProvisioner) CheckIndexExists(ctx context.Context, config *ElasticsearchProvisioningConfig) (bool, error) {
	client, err := p.createClient(config)
	if err != nil {
		return false, err
	}

	res, err := client.Indices.Exists(
		[]string{config.IndexName},
		client.Indices.Exists.WithContext(ctx),
	)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	return res.StatusCode == 200, nil
}

// DeleteIndex 인덱스 삭제 (롤백용)
func (p *ElasticsearchProvisioner) DeleteIndex(ctx context.Context, config *ElasticsearchProvisioningConfig) error {
	client, err := p.createClient(config)
	if err != nil {
		return err
	}

	res, err := client.Indices.Delete(
		[]string{config.IndexName},
		client.Indices.Delete.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("failed to delete index: %s", res.Status())
	}

	return nil
}

// UpdateMapping 매핑 업데이트
func (p *ElasticsearchProvisioner) UpdateMapping(ctx context.Context, config *ElasticsearchProvisioningConfig, mappings map[string]any) error {
	client, err := p.createClient(config)
	if err != nil {
		return err
	}

	body, err := json.Marshal(mappings)
	if err != nil {
		return err
	}

	res, err := client.Indices.PutMapping(
		[]string{config.IndexName},
		bytes.NewReader(body),
		client.Indices.PutMapping.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("failed to update mapping: %s", res.Status())
	}

	return nil
}
