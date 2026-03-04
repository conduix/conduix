// Package output MongoDB 데이터 출력 구현
package output

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// MongoDBOutput MongoDB 데이터 출력
type MongoDBOutput struct {
	uri                string
	database           string
	collectionName     string
	collectionTemplate *template.Template

	// Upsert 설정
	upsertEnabled bool
	upsertKeys    []string

	// Write 설정
	writeConcern string
	ordered      bool

	// 배치 설정
	batchSize     int
	flushInterval time.Duration

	// MongoDB 클라이언트
	client     *mongo.Client
	collection *mongo.Collection

	// 버퍼
	buffer []source.Record
	bufMu  sync.Mutex
	stats  OutputStats

	// 백그라운드 플러시
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// MongoDBConfig MongoDB 설정
type MongoDBConfig struct {
	URI           string        `yaml:"uri" json:"uri"`
	Database      string        `yaml:"database" json:"database"`
	Collection    string        `yaml:"collection" json:"collection"` // 정적 또는 템플릿
	WriteConcern  string        `yaml:"write_concern,omitempty" json:"write_concern,omitempty"`
	Ordered       bool          `yaml:"ordered,omitempty" json:"ordered,omitempty"`
	BulkSize      int           `yaml:"bulk_size,omitempty" json:"bulk_size,omitempty"`
	FlushInterval string        `yaml:"flush_interval,omitempty" json:"flush_interval,omitempty"`
	Upsert        *UpsertConfig `yaml:"upsert,omitempty" json:"upsert,omitempty"`
}

// UpsertConfig Upsert 설정
type UpsertConfig struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	KeyFields []string `yaml:"key_fields" json:"key_fields"`
}

// NewMongoDBOutput MongoDB 출력 생성
func NewMongoDBOutput(cfg config.OutputConfig) (*MongoDBOutput, error) {
	mongoCfg, err := parseMongoDBConfig(cfg)
	if err != nil {
		return nil, err
	}

	if mongoCfg.URI == "" {
		return nil, fmt.Errorf("mongodb uri is required")
	}
	if mongoCfg.Database == "" {
		return nil, fmt.Errorf("mongodb database is required")
	}
	if mongoCfg.Collection == "" {
		return nil, fmt.Errorf("mongodb collection is required")
	}

	// 배치 설정 기본값
	batchSize := mongoCfg.BulkSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	flushInterval := 5 * time.Second
	if mongoCfg.FlushInterval != "" {
		if d, err := time.ParseDuration(mongoCfg.FlushInterval); err == nil {
			flushInterval = d
		}
	}

	output := &MongoDBOutput{
		uri:            mongoCfg.URI,
		database:       mongoCfg.Database,
		collectionName: mongoCfg.Collection,
		writeConcern:   mongoCfg.WriteConcern,
		ordered:        mongoCfg.Ordered,
		batchSize:      batchSize,
		flushInterval:  flushInterval,
		buffer:         make([]source.Record, 0, batchSize),
	}

	// Collection 템플릿 파싱
	if containsTemplate(mongoCfg.Collection) {
		tmpl, err := template.New("collection").Parse(mongoCfg.Collection)
		if err != nil {
			return nil, fmt.Errorf("invalid collection template: %w", err)
		}
		output.collectionTemplate = tmpl
	}

	// Upsert 설정
	if mongoCfg.Upsert != nil && mongoCfg.Upsert.Enabled {
		output.upsertEnabled = true
		output.upsertKeys = mongoCfg.Upsert.KeyFields
		if len(output.upsertKeys) == 0 {
			output.upsertKeys = []string{"_id"}
		}
	}

	return output, nil
}

// parseMongoDBConfig OutputConfig에서 MongoDB 설정 파싱
func parseMongoDBConfig(cfg config.OutputConfig) (*MongoDBConfig, error) {
	mongoCfg := &MongoDBConfig{}

	if uri, ok := cfg.Config["uri"].(string); ok {
		mongoCfg.URI = uri
	}
	if database, ok := cfg.Config["database"].(string); ok {
		mongoCfg.Database = database
	}
	if collection, ok := cfg.Config["collection"].(string); ok {
		mongoCfg.Collection = collection
	}
	if writeConcern, ok := cfg.Config["write_concern"].(string); ok {
		mongoCfg.WriteConcern = writeConcern
	}
	if ordered, ok := cfg.Config["ordered"].(bool); ok {
		mongoCfg.Ordered = ordered
	}
	if bulkSize, ok := cfg.Config["bulk_size"].(int); ok {
		mongoCfg.BulkSize = bulkSize
	}
	if bulkSizeF, ok := cfg.Config["bulk_size"].(float64); ok {
		mongoCfg.BulkSize = int(bulkSizeF)
	}
	if flushInterval, ok := cfg.Config["flush_interval"].(string); ok {
		mongoCfg.FlushInterval = flushInterval
	}

	// Upsert 파싱
	if upsertMap, ok := cfg.Config["upsert"].(map[string]interface{}); ok {
		mongoCfg.Upsert = &UpsertConfig{}
		if enabled, ok := upsertMap["enabled"].(bool); ok {
			mongoCfg.Upsert.Enabled = enabled
		}
		if keyFields, ok := upsertMap["key_fields"].([]interface{}); ok {
			for _, k := range keyFields {
				if s, ok := k.(string); ok {
					mongoCfg.Upsert.KeyFields = append(mongoCfg.Upsert.KeyFields, s)
				}
			}
		}
	}

	return mongoCfg, nil
}

// containsTemplate 문자열이 템플릿 변수를 포함하는지 확인
func containsTemplate(s string) bool {
	return len(s) > 4 && (contains(s, "{{") && contains(s, "}}"))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (o *MongoDBOutput) Name() string {
	return "mongodb"
}

func (o *MongoDBOutput) Open(ctx context.Context) error {
	// 클라이언트 옵션 설정
	clientOpts := options.Client().ApplyURI(o.uri)

	// Write Concern 설정
	if o.writeConcern != "" {
		wc := getWriteConcern(o.writeConcern)
		clientOpts.SetWriteConcern(wc)
	}

	// 연결
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}

	// 연결 테스트
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping mongodb: %w", err)
	}

	o.client = client

	// 정적 Collection인 경우 미리 참조
	if o.collectionTemplate == nil {
		o.collection = client.Database(o.database).Collection(o.collectionName)
	}

	// 백그라운드 플러시 시작
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go o.backgroundFlush()

	log.Printf("[mongodb] Output opened (uri=%s, database=%s, collection=%s, batch_size=%d)",
		maskURI(o.uri), o.database, o.collectionName, o.batchSize)
	return nil
}

// getWriteConcern WriteConcern 문자열을 WriteConcern 객체로 변환
func getWriteConcern(wc string) *writeconcern.WriteConcern {
	switch wc {
	case "majority":
		return writeconcern.Majority()
	case "1", "w1":
		return writeconcern.W1()
	case "journaled":
		return writeconcern.Journaled()
	default:
		return writeconcern.Majority()
	}
}

// maskURI URI에서 비밀번호 마스킹
func maskURI(uri string) string {
	// 간단한 마스킹: mongodb://user:pass@host -> mongodb://user:***@host
	// mongodb+srv:// 도 지원

	// @ 위치 찾기
	atIdx := -1
	for i := 0; i < len(uri); i++ {
		if uri[i] == '@' {
			atIdx = i
			break
		}
	}
	if atIdx == -1 {
		return uri // @ 없으면 마스킹 불필요
	}

	// "://" 이후의 첫 번째 ':' 찾기 (user:pass 구분자)
	schemeEnd := -1
	for i := 0; i < len(uri)-2; i++ {
		if uri[i] == ':' && uri[i+1] == '/' && uri[i+2] == '/' {
			schemeEnd = i + 3
			break
		}
	}
	if schemeEnd == -1 {
		return uri
	}

	// schemeEnd 이후, @ 이전에서 ':' 찾기
	for i := schemeEnd; i < atIdx; i++ {
		if uri[i] == ':' {
			return uri[:i+1] + "***" + uri[atIdx:]
		}
	}
	return uri
}

func (o *MongoDBOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	o.bufMu.Lock()
	o.buffer = append(o.buffer, record)
	shouldFlush := len(o.buffer) >= o.batchSize
	o.bufMu.Unlock()

	if shouldFlush {
		return o.Flush(ctx)
	}

	return nil
}

// WriteBatch 배치 쓰기 (BatchOutput 인터페이스)
func (o *MongoDBOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	return o.bulkWrite(ctx, records)
}

// SupportsBatch 배치 지원 여부
func (o *MongoDBOutput) SupportsBatch() bool {
	return true
}

// BatchConfig 배치 설정 반환
func (o *MongoDBOutput) BatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       true,
		Size:          o.batchSize,
		FlushInterval: o.flushInterval,
	}
}

func (o *MongoDBOutput) Flush(ctx context.Context) error {
	o.bufMu.Lock()
	if len(o.buffer) == 0 {
		o.bufMu.Unlock()
		return nil
	}
	records := o.buffer
	o.buffer = make([]source.Record, 0, o.batchSize)
	o.bufMu.Unlock()

	return o.bulkWrite(ctx, records)
}

func (o *MongoDBOutput) bulkWrite(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	// Collection별 레코드 그룹화 (동적 Collection 지원)
	collectionRecords := make(map[string][]source.Record)

	for _, record := range records {
		collName := o.getCollectionName(record)
		collectionRecords[collName] = append(collectionRecords[collName], record)
	}

	var totalErrors int64

	for collName, recs := range collectionRecords {
		coll := o.client.Database(o.database).Collection(collName)

		if o.upsertEnabled {
			// Upsert 모드: BulkWrite 사용
			if err := o.doBulkUpsert(ctx, coll, recs); err != nil {
				log.Printf("[mongodb] Bulk upsert error for collection %s: %v", collName, err)
				totalErrors += int64(len(recs))
			}
		} else {
			// Insert 모드: InsertMany 사용
			if err := o.doInsertMany(ctx, coll, recs); err != nil {
				log.Printf("[mongodb] Insert many error for collection %s: %v", collName, err)
				totalErrors += int64(len(recs))
			}
		}
	}

	successCount := int64(len(records)) - totalErrors
	atomic.AddInt64(&o.stats.SuccessRecords, successCount)
	atomic.AddInt64(&o.stats.ErrorRecords, totalErrors)
	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	log.Printf("[mongodb] Bulk wrote %d documents (success: %d, errors: %d)",
		len(records), successCount, totalErrors)
	return nil
}

// doInsertMany InsertMany 실행
func (o *MongoDBOutput) doInsertMany(ctx context.Context, coll *mongo.Collection, records []source.Record) error {
	docs := make([]interface{}, len(records))
	for i, record := range records {
		docs[i] = record.Data
	}

	opts := options.InsertMany().SetOrdered(o.ordered)
	_, err := coll.InsertMany(ctx, docs, opts)
	if err != nil {
		// 부분 성공 처리 (ordered=false인 경우)
		if bulkErr, ok := err.(mongo.BulkWriteException); ok {
			log.Printf("[mongodb] Partial insert: %d errors out of %d documents",
				len(bulkErr.WriteErrors), len(records))
			return nil // 부분 성공은 에러로 취급하지 않음
		}
		return err
	}
	return nil
}

// doBulkUpsert BulkWrite로 Upsert 실행
func (o *MongoDBOutput) doBulkUpsert(ctx context.Context, coll *mongo.Collection, records []source.Record) error {
	models := make([]mongo.WriteModel, len(records))

	for i, record := range records {
		// Filter 생성 (upsert key 기반)
		filter := bson.M{}
		for _, key := range o.upsertKeys {
			if val, ok := record.Data[key]; ok {
				filter[key] = val
			}
		}

		// Update document
		update := bson.M{"$set": record.Data}

		model := mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true)

		models[i] = model
	}

	opts := options.BulkWrite().SetOrdered(o.ordered)
	result, err := coll.BulkWrite(ctx, models, opts)
	if err != nil {
		if bulkErr, ok := err.(mongo.BulkWriteException); ok {
			log.Printf("[mongodb] Partial upsert: %d errors", len(bulkErr.WriteErrors))
			return nil
		}
		return err
	}

	log.Printf("[mongodb] Upsert result: inserted=%d, modified=%d, upserted=%d",
		result.InsertedCount, result.ModifiedCount, result.UpsertedCount)
	return nil
}

// getCollectionName 레코드에서 Collection 이름 결정
func (o *MongoDBOutput) getCollectionName(record source.Record) string {
	if o.collectionTemplate != nil {
		data := make(map[string]interface{})
		for k, v := range record.Data {
			data[k] = v
		}

		// 날짜 변수 추가
		now := time.Now()
		data["date"] = now.Format("2006-01-02")
		data["year"] = now.Format("2006")
		data["month"] = now.Format("01")
		data["day"] = now.Format("02")

		var buf bytes.Buffer
		if err := o.collectionTemplate.Execute(&buf, data); err != nil {
			log.Printf("[mongodb] Failed to execute collection template: %v", err)
			return o.collectionName
		}
		return buf.String()
	}
	return o.collectionName
}

func (o *MongoDBOutput) backgroundFlush() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.Flush(context.Background()); err != nil {
				log.Printf("[mongodb] Background flush error: %v", err)
			}
		}
	}
}

func (o *MongoDBOutput) Close() error {
	// 백그라운드 플러시 중지
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[mongodb] Warning: failed to flush remaining records: %v", err)
	}

	// MongoDB 연결 종료
	if o.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := o.client.Disconnect(ctx); err != nil {
			log.Printf("[mongodb] Warning: failed to disconnect: %v", err)
		}
	}

	log.Printf("[mongodb] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)
	return nil
}

func (o *MongoDBOutput) Stats() OutputStats {
	return o.stats
}
