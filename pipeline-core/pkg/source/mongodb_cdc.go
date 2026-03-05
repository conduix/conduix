// Package source MongoDB Change Stream (CDC) 소스 구현
package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/secrets"
)

// MongoDBCDCSource MongoDB Change Stream 소스
type MongoDBCDCSource struct {
	name string

	// 연결 설정
	uri        string
	database   string
	collection string // 빈 문자열이면 database 전체 감시

	// Change Stream 옵션
	fullDocument         string // default, updateLookup, whenAvailable, required
	fullDocumentBefore   string // off, whenAvailable, required
	startAfter           interface{}
	startAtOperationTime *primitive.Timestamp
	batchSize            int32
	maxAwaitTime         time.Duration

	// Pipeline 필터 (MongoDB aggregation pipeline)
	pipeline []bson.D

	// 재연결 설정
	reconnectWait    time.Duration
	maxReconnect     int
	reconnectBackoff float64

	// 클라이언트
	client       *mongo.Client
	changeStream *mongo.ChangeStream

	// 상태
	resumeToken interface{}
	recordCount int64
	mu          sync.RWMutex
	closed      bool
}

// NewMongoDBCDCSource MongoDB CDC 소스 생성
func NewMongoDBCDCSource(cfg config.SourceV2) (*MongoDBCDCSource, error) {
	s := &MongoDBCDCSource{
		name:             "mongodb_cdc",
		fullDocument:     "updateLookup", // 기본값: 업데이트 시 전체 문서 조회
		batchSize:        1000,
		maxAwaitTime:     10 * time.Second,
		reconnectWait:    5 * time.Second,
		maxReconnect:     0, // 무한
		reconnectBackoff: 2.0,
	}

	// URI
	if cfg.MongoDBURI != "" {
		s.uri = secrets.ExpandEnvVars(cfg.MongoDBURI)
	}
	if s.uri == "" {
		s.uri = os.Getenv("MONGODB_URI")
	}
	if s.uri == "" {
		return nil, fmt.Errorf("mongodb uri is required")
	}

	// Database
	s.database = cfg.MongoDBDatabase
	if s.database == "" {
		return nil, fmt.Errorf("mongodb database is required")
	}

	// Collection (선택 - 없으면 database 전체 감시)
	s.collection = cfg.MongoDBCollection

	// Change Stream 옵션
	if cfg.MongoDBFullDocument != "" {
		s.fullDocument = cfg.MongoDBFullDocument
	}
	if cfg.MongoDBFullDocumentBefore != "" {
		s.fullDocumentBefore = cfg.MongoDBFullDocumentBefore
	}

	if cfg.MongoDBBatchSize > 0 {
		s.batchSize = int32(cfg.MongoDBBatchSize)
	}

	if cfg.MongoDBMaxAwaitTime != "" {
		if d, err := time.ParseDuration(cfg.MongoDBMaxAwaitTime); err == nil {
			s.maxAwaitTime = d
		}
	}

	// Resume token (재시작 시 사용)
	if cfg.MongoDBResumeAfter != "" {
		s.startAfter = cfg.MongoDBResumeAfter
	}

	// 재연결 설정
	if cfg.MongoDBReconnectWait > 0 {
		s.reconnectWait = time.Duration(cfg.MongoDBReconnectWait) * time.Millisecond
	}
	if cfg.MongoDBMaxReconnect > 0 {
		s.maxReconnect = cfg.MongoDBMaxReconnect
	}

	return s, nil
}

func (s *MongoDBCDCSource) Name() string {
	return s.name
}

func (s *MongoDBCDCSource) Open(ctx context.Context) error {
	// MongoDB 클라이언트 옵션
	clientOpts := options.Client().ApplyURI(s.uri)

	// 연결
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// 연결 확인
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	s.client = client

	log.Printf("[mongodb_cdc] Connected to MongoDB (db=%s, collection=%s)",
		s.database, s.collection)
	return nil
}

func (s *MongoDBCDCSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	recordCh := make(chan Record, 1000)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		reconnectCount := 0
		currentBackoff := s.reconnectWait

		for {
			// 컨텍스트 취소 확인
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Change Stream 시작
			if err := s.startChangeStream(ctx); err != nil {
				s.mu.RLock()
				closed := s.closed
				s.mu.RUnlock()

				if closed {
					return
				}

				reconnectCount++
				if s.maxReconnect > 0 && reconnectCount > s.maxReconnect {
					errCh <- fmt.Errorf("max reconnect attempts exceeded: %w", err)
					return
				}

				log.Printf("[mongodb_cdc] Change stream error, reconnecting in %v: %v", currentBackoff, err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(currentBackoff):
					currentBackoff = time.Duration(float64(currentBackoff) * s.reconnectBackoff)
					if currentBackoff > 5*time.Minute {
						currentBackoff = 5 * time.Minute
					}
					continue
				}
			}

			// 이벤트 수신
			for s.changeStream.Next(ctx) {
				reconnectCount = 0
				currentBackoff = s.reconnectWait

				var event bson.M
				if err := s.changeStream.Decode(&event); err != nil {
					log.Printf("[mongodb_cdc] Failed to decode event: %v", err)
					continue
				}

				// Resume token 저장
				s.mu.Lock()
				s.resumeToken = s.changeStream.ResumeToken()
				s.recordCount++
				s.mu.Unlock()

				// 레코드 변환
				record := s.eventToRecord(event)

				select {
				case recordCh <- record:
				case <-ctx.Done():
					return
				}
			}

			// Change Stream 에러 확인
			if err := s.changeStream.Err(); err != nil {
				s.mu.RLock()
				closed := s.closed
				s.mu.RUnlock()

				if closed {
					return
				}

				log.Printf("[mongodb_cdc] Change stream error: %v", err)
				// 재연결 시도
			}
		}
	}()

	return recordCh, errCh
}

func (s *MongoDBCDCSource) startChangeStream(ctx context.Context) error {
	// Change Stream 옵션
	opts := options.ChangeStream()

	// Full document 옵션
	switch s.fullDocument {
	case "updateLookup":
		opts.SetFullDocument(options.UpdateLookup)
	case "whenAvailable":
		opts.SetFullDocument(options.WhenAvailable)
	case "required":
		opts.SetFullDocument(options.Required)
	default:
		opts.SetFullDocument(options.Default)
	}

	// Full document before change (MongoDB 6.0+)
	switch s.fullDocumentBefore {
	case "whenAvailable":
		opts.SetFullDocumentBeforeChange(options.WhenAvailable)
	case "required":
		opts.SetFullDocumentBeforeChange(options.Required)
	default:
		opts.SetFullDocumentBeforeChange(options.Off)
	}

	if s.batchSize > 0 {
		opts.SetBatchSize(s.batchSize)
	}
	if s.maxAwaitTime > 0 {
		opts.SetMaxAwaitTime(s.maxAwaitTime)
	}

	// Resume 설정 (저장된 토큰이 있으면 사용)
	s.mu.RLock()
	resumeToken := s.resumeToken
	s.mu.RUnlock()

	if resumeToken != nil {
		opts.SetResumeAfter(resumeToken)
	} else if s.startAfter != nil {
		opts.SetStartAfter(s.startAfter)
	} else if s.startAtOperationTime != nil {
		opts.SetStartAtOperationTime(s.startAtOperationTime)
	}

	// Pipeline
	pipeline := mongo.Pipeline{}
	if len(s.pipeline) > 0 {
		pipeline = s.pipeline
	}

	// Change Stream 시작
	var changeStream *mongo.ChangeStream
	var err error

	if s.collection != "" {
		// 특정 Collection 감시
		col := s.client.Database(s.database).Collection(s.collection)
		changeStream, err = col.Watch(ctx, pipeline, opts)
	} else {
		// Database 전체 감시
		db := s.client.Database(s.database)
		changeStream, err = db.Watch(ctx, pipeline, opts)
	}

	if err != nil {
		return fmt.Errorf("failed to start change stream: %w", err)
	}

	s.changeStream = changeStream
	log.Printf("[mongodb_cdc] Change stream started")
	return nil
}

func (s *MongoDBCDCSource) eventToRecord(event bson.M) Record {
	data := make(map[string]any)

	// 기본 필드
	if opType, ok := event["operationType"].(string); ok {
		data["_op"] = opType
	}

	// Document Key
	if docKey, ok := event["documentKey"].(bson.M); ok {
		if id, ok := docKey["_id"]; ok {
			data["_id"] = formatObjectID(id)
		}
		data["_document_key"] = formatBsonM(docKey)
	}

	// Full Document
	if fullDoc, ok := event["fullDocument"].(bson.M); ok {
		for k, v := range fullDoc {
			data[k] = formatBsonValue(v)
		}
	}

	// Full Document Before Change (for update/replace/delete)
	if beforeDoc, ok := event["fullDocumentBeforeChange"].(bson.M); ok {
		data["_before"] = formatBsonM(beforeDoc)
	}

	// Update Description (for update operations)
	if updateDesc, ok := event["updateDescription"].(bson.M); ok {
		data["_update_description"] = formatBsonM(updateDesc)
	}

	// Namespace
	if ns, ok := event["ns"].(bson.M); ok {
		if db, ok := ns["db"].(string); ok {
			data["_database"] = db
		}
		if coll, ok := ns["coll"].(string); ok {
			data["_collection"] = coll
		}
	}

	// Cluster Time
	if clusterTime, ok := event["clusterTime"].(primitive.Timestamp); ok {
		data["_cluster_time"] = clusterTime.T
	}

	// Wall Time
	if wallTime, ok := event["wallTime"].(primitive.DateTime); ok {
		data["_wall_time"] = wallTime.Time().Format(time.RFC3339Nano)
	}

	// Transaction Number
	if txnNumber, ok := event["txnNumber"]; ok {
		data["_txn_number"] = txnNumber
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "mongodb_cdc",
			Origin:    fmt.Sprintf("%s.%s", s.database, s.collection),
			Timestamp: time.Now().UnixNano(),
		},
	}
}

// formatObjectID ObjectID를 문자열로 변환
func formatObjectID(v interface{}) string {
	switch id := v.(type) {
	case primitive.ObjectID:
		return id.Hex()
	case string:
		return id
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatBsonValue BSON 값을 Go 기본 타입으로 변환
func formatBsonValue(v interface{}) interface{} {
	switch val := v.(type) {
	case primitive.ObjectID:
		return val.Hex()
	case primitive.DateTime:
		return val.Time().Format(time.RFC3339Nano)
	case primitive.Timestamp:
		return val.T
	case bson.M:
		return formatBsonM(val)
	case bson.A:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = formatBsonValue(item)
		}
		return result
	case primitive.Binary:
		return val.Data
	case primitive.Decimal128:
		return val.String()
	default:
		return v
	}
}

// formatBsonM bson.M을 map[string]any로 변환
func formatBsonM(m bson.M) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		result[k] = formatBsonValue(v)
	}
	return result
}

func (s *MongoDBCDCSource) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	if s.changeStream != nil {
		if err := s.changeStream.Close(context.Background()); err != nil {
			log.Printf("[mongodb_cdc] Error closing change stream: %v", err)
		}
	}

	if s.client != nil {
		if err := s.client.Disconnect(context.Background()); err != nil {
			log.Printf("[mongodb_cdc] Error disconnecting client: %v", err)
		}
	}

	log.Printf("[mongodb_cdc] Source closed")
	return nil
}

// CheckpointableSource 인터페이스 구현

func (s *MongoDBCDCSource) SourceType() string {
	return "mongodb_cdc"
}

func (s *MongoDBCDCSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.resumeToken == nil {
		return nil
	}

	// Resume token을 문자열로 변환
	tokenBytes, err := bson.Marshal(s.resumeToken)
	if err != nil {
		return nil
	}

	return []*SourceCheckpoint{
		{
			PartitionKey: fmt.Sprintf("%s.%s", s.database, s.collection),
			OffsetValue:  string(tokenBytes),
			OffsetType:   "string",
			RecordCount:  s.recordCount,
			UpdatedAt:    time.Now(),
		},
	}
}

func (s *MongoDBCDCSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cp := range checkpoints {
		if cp.PartitionKey == fmt.Sprintf("%s.%s", s.database, s.collection) {
			// Resume token 복원
			var token interface{}
			if err := bson.Unmarshal([]byte(cp.OffsetValue), &token); err == nil {
				s.resumeToken = token
			}
			s.recordCount = cp.RecordCount
			break
		}
	}

	return nil
}
