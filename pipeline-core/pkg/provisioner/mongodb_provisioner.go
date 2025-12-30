package provisioner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/conduix/conduix/shared/types"
)

// MongoDBProvisioner MongoDB 컬렉션 생성 Provisioner
type MongoDBProvisioner struct {
	sinkType types.SinkType
}

// NewMongoDBProvisioner MongoDB Provisioner 생성
func NewMongoDBProvisioner() *MongoDBProvisioner {
	return &MongoDBProvisioner{
		sinkType: types.SinkTypeMongoDB,
	}
}

func (p *MongoDBProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *MongoDBProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{
		types.ProvisioningTypeTableCreation, // MongoDB에서는 Collection 생성
		types.ProvisioningTypeExternal,
	}
}

// MongoDBProvisioningConfig MongoDB 프로비저닝 설정
type MongoDBProvisioningConfig struct {
	URI            string         `json:"uri"`             // MongoDB 연결 URI
	Database       string         `json:"database"`        // 데이터베이스 이름
	CollectionName string         `json:"collection_name"` // 생성할 컬렉션 이름
	Capped         bool           `json:"capped"`          // Capped collection 여부
	SizeInBytes    int64          `json:"size_in_bytes"`   // Capped collection 크기
	MaxDocuments   int64          `json:"max_documents"`   // Capped collection 최대 문서 수
	Indexes        []MongoDBIndex `json:"indexes"`         // 생성할 인덱스
	Validator      bson.M         `json:"validator"`       // JSON Schema validator
}

// MongoDBIndex MongoDB 인덱스 정의
type MongoDBIndex struct {
	Keys   map[string]int `json:"keys"`   // 인덱스 키 (1: ASC, -1: DESC)
	Name   string         `json:"name"`   // 인덱스 이름
	Unique bool           `json:"unique"` // 유니크 인덱스 여부
	Sparse bool           `json:"sparse"` // Sparse 인덱스 여부
	TTL    int32          `json:"ttl"`    // TTL (초, 0이면 사용 안함)
}

func (p *MongoDBProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 프로비저닝인 경우
	if req.Type == types.ProvisioningTypeExternal {
		return &types.ProvisioningResult{
			ID:         uuid.New().String(),
			RequestID:  req.ID,
			PipelineID: req.PipelineID,
			SinkType:   req.SinkType,
			Status:     types.ProvisioningStatusPending,
			Message:    "Waiting for external MongoDB collection provisioning",
			Metadata: map[string]any{
				"external_url": req.ExternalURL,
				"callback_url": req.CallbackURL,
			},
		}, nil
	}

	// 설정 파싱
	config, err := p.parseConfig(req.Config)
	if err != nil {
		return nil, fmt.Errorf("invalid mongodb config: %w", err)
	}

	// 컬렉션 생성
	if err := p.createCollection(ctx, config); err != nil {
		now := time.Now()
		return &types.ProvisioningResult{
			ID:          uuid.New().String(),
			RequestID:   req.ID,
			PipelineID:  req.PipelineID,
			SinkType:    req.SinkType,
			Status:      types.ProvisioningStatusFailed,
			Message:     "Failed to create MongoDB collection",
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
		TableName:  config.CollectionName, // MongoDB에서는 컬렉션명
		Message:    fmt.Sprintf("MongoDB collection '%s' created successfully in database '%s'", config.CollectionName, config.Database),
		Metadata: map[string]any{
			"database":   config.Database,
			"collection": config.CollectionName,
			"capped":     config.Capped,
			"indexes":    len(config.Indexes),
		},
		CompletedAt: &now,
	}, nil
}

func (p *MongoDBProvisioner) parseConfig(config map[string]any) (*MongoDBProvisioningConfig, error) {
	cfg := &MongoDBProvisioningConfig{}

	// URI
	if uri, ok := config["uri"].(string); ok && uri != "" {
		cfg.URI = uri
	} else {
		return nil, fmt.Errorf("uri is required")
	}

	// Database
	if database, ok := config["database"].(string); ok && database != "" {
		cfg.Database = database
	} else {
		return nil, fmt.Errorf("database is required")
	}

	// CollectionName
	if collectionName, ok := config["collection_name"].(string); ok && collectionName != "" {
		cfg.CollectionName = collectionName
	} else {
		return nil, fmt.Errorf("collection_name is required")
	}

	// Capped
	if capped, ok := config["capped"].(bool); ok {
		cfg.Capped = capped
	}

	// SizeInBytes
	if size, ok := config["size_in_bytes"].(float64); ok {
		cfg.SizeInBytes = int64(size)
	} else if size, ok := config["size_in_bytes"].(int64); ok {
		cfg.SizeInBytes = size
	}

	// MaxDocuments
	if max, ok := config["max_documents"].(float64); ok {
		cfg.MaxDocuments = int64(max)
	} else if max, ok := config["max_documents"].(int64); ok {
		cfg.MaxDocuments = max
	}

	// Indexes
	if indexes, ok := config["indexes"].([]any); ok {
		for _, idx := range indexes {
			if idxMap, ok := idx.(map[string]any); ok {
				index := MongoDBIndex{
					Keys: make(map[string]int),
				}
				if keys, ok := idxMap["keys"].(map[string]any); ok {
					for k, v := range keys {
						if vi, ok := v.(float64); ok {
							index.Keys[k] = int(vi)
						} else if vi, ok := v.(int); ok {
							index.Keys[k] = vi
						}
					}
				}
				if name, ok := idxMap["name"].(string); ok {
					index.Name = name
				}
				if unique, ok := idxMap["unique"].(bool); ok {
					index.Unique = unique
				}
				if sparse, ok := idxMap["sparse"].(bool); ok {
					index.Sparse = sparse
				}
				if ttl, ok := idxMap["ttl"].(float64); ok {
					index.TTL = int32(ttl)
				}
				cfg.Indexes = append(cfg.Indexes, index)
			}
		}
	}

	return cfg, nil
}

func (p *MongoDBProvisioner) createCollection(ctx context.Context, config *MongoDBProvisioningConfig) error {
	// MongoDB 클라이언트 생성
	clientOpts := options.Client().ApplyURI(config.URI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	// 연결 테스트
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping mongodb: %w", err)
	}

	db := client.Database(config.Database)

	// 컬렉션 생성 옵션
	createOpts := options.CreateCollection()

	if config.Capped {
		createOpts.SetCapped(true)
		if config.SizeInBytes > 0 {
			createOpts.SetSizeInBytes(config.SizeInBytes)
		} else {
			createOpts.SetSizeInBytes(10 * 1024 * 1024) // 기본 10MB
		}
		if config.MaxDocuments > 0 {
			createOpts.SetMaxDocuments(config.MaxDocuments)
		}
	}

	// Validator가 있으면 설정
	if config.Validator != nil {
		createOpts.SetValidator(config.Validator)
	}

	// 컬렉션 생성
	err = db.CreateCollection(ctx, config.CollectionName, createOpts)
	if err != nil {
		// 이미 존재하는 경우 무시
		if !mongo.IsDuplicateKeyError(err) && err.Error() != "NamespaceExists" {
			return fmt.Errorf("failed to create collection: %w", err)
		}
	}

	// 인덱스 생성
	if len(config.Indexes) > 0 {
		collection := db.Collection(config.CollectionName)
		if err := p.createIndexes(ctx, collection, config.Indexes); err != nil {
			return fmt.Errorf("failed to create indexes: %w", err)
		}
	}

	return nil
}

func (p *MongoDBProvisioner) createIndexes(ctx context.Context, collection *mongo.Collection, indexes []MongoDBIndex) error {
	for _, idx := range indexes {
		keys := bson.D{}
		for k, v := range idx.Keys {
			keys = append(keys, bson.E{Key: k, Value: v})
		}

		indexOpts := options.Index()
		if idx.Name != "" {
			indexOpts.SetName(idx.Name)
		}
		if idx.Unique {
			indexOpts.SetUnique(true)
		}
		if idx.Sparse {
			indexOpts.SetSparse(true)
		}
		if idx.TTL > 0 {
			indexOpts.SetExpireAfterSeconds(idx.TTL)
		}

		indexModel := mongo.IndexModel{
			Keys:    keys,
			Options: indexOpts,
		}

		_, err := collection.Indexes().CreateOne(ctx, indexModel)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *MongoDBProvisioner) Validate(config map[string]any) error {
	_, err := p.parseConfig(config)
	return err
}

func (p *MongoDBProvisioner) RequiresExternalSetup() bool {
	return false // 자동 생성 가능
}

func (p *MongoDBProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return req.ExternalURL
}

// CheckCollectionExists 컬렉션 존재 여부 확인
func (p *MongoDBProvisioner) CheckCollectionExists(ctx context.Context, config *MongoDBProvisioningConfig) (bool, error) {
	clientOpts := options.Client().ApplyURI(config.URI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return false, err
	}
	defer func() { _ = client.Disconnect(ctx) }()

	db := client.Database(config.Database)
	collections, err := db.ListCollectionNames(ctx, bson.M{"name": config.CollectionName})
	if err != nil {
		return false, err
	}

	return len(collections) > 0, nil
}

// DropCollection 컬렉션 삭제 (롤백용)
func (p *MongoDBProvisioner) DropCollection(ctx context.Context, config *MongoDBProvisioningConfig) error {
	clientOpts := options.Client().ApplyURI(config.URI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return err
	}
	defer func() { _ = client.Disconnect(ctx) }()

	collection := client.Database(config.Database).Collection(config.CollectionName)
	return collection.Drop(ctx)
}
