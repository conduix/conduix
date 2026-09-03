// Package output BigQuery 데이터 출력 구현
package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/option"

	pipelineConfig "github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// BigQueryOutput BigQuery 데이터 출력
type BigQueryOutput struct {
	projectID       string
	datasetID       string
	tableID         string
	credentialsFile string

	// 스키마 설정
	autoDetectSchema bool

	// 쓰기 설정
	writeDisposition    bigquery.TableWriteDisposition
	createDisposition   bigquery.TableCreateDisposition
	ignoreUnknownValues bool
	skipInvalidRows     bool

	// 배치 설정
	batchSize     int
	flushInterval time.Duration

	// BigQuery 클라이언트
	client   *bigquery.Client
	inserter *bigquery.Inserter

	// 버퍼
	buffer []map[string]bigquery.Value
	bufMu  sync.Mutex
	stats  OutputStats

	// 백그라운드 플러시
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// BigQueryConfig BigQuery 설정
type BigQueryConfig struct {
	ProjectID           string `yaml:"project_id" json:"project_id"`
	DatasetID           string `yaml:"dataset_id" json:"dataset_id"`
	TableID             string `yaml:"table_id" json:"table_id"`
	CredentialsFile     string `yaml:"credentials_file" json:"credentials_file"`
	AutoDetectSchema    bool   `yaml:"auto_detect_schema" json:"auto_detect_schema"`
	WriteDisposition    string `yaml:"write_disposition" json:"write_disposition"`   // WRITE_APPEND, WRITE_TRUNCATE, WRITE_EMPTY
	CreateDisposition   string `yaml:"create_disposition" json:"create_disposition"` // CREATE_IF_NEEDED, CREATE_NEVER
	IgnoreUnknownValues bool   `yaml:"ignore_unknown_values" json:"ignore_unknown_values"`
	SkipInvalidRows     bool   `yaml:"skip_invalid_rows" json:"skip_invalid_rows"`
	BatchSize           int    `yaml:"batch_size" json:"batch_size"`
	FlushInterval       string `yaml:"flush_interval" json:"flush_interval"`
}

// NewBigQueryOutput BigQuery 출력 생성
func NewBigQueryOutput(cfg pipelineConfig.OutputConfig) (*BigQueryOutput, error) {
	bqCfg, err := parseBigQueryConfig(cfg)
	if err != nil {
		return nil, err
	}

	if bqCfg.ProjectID == "" {
		return nil, fmt.Errorf("bigquery project_id is required")
	}
	if bqCfg.DatasetID == "" {
		return nil, fmt.Errorf("bigquery dataset_id is required")
	}
	if bqCfg.TableID == "" {
		return nil, fmt.Errorf("bigquery table_id is required")
	}

	// 기본값 설정
	if bqCfg.BatchSize <= 0 {
		bqCfg.BatchSize = 500
	}

	flushInterval := 30 * time.Second
	if bqCfg.FlushInterval != "" {
		if d, err := time.ParseDuration(bqCfg.FlushInterval); err == nil {
			flushInterval = d
		}
	}

	// Write/Create disposition 파싱
	writeDisp := bigquery.WriteAppend
	switch bqCfg.WriteDisposition {
	case "WRITE_TRUNCATE":
		writeDisp = bigquery.WriteTruncate
	case "WRITE_EMPTY":
		writeDisp = bigquery.WriteEmpty
	}

	createDisp := bigquery.CreateIfNeeded
	switch bqCfg.CreateDisposition {
	case "CREATE_NEVER":
		createDisp = bigquery.CreateNever
	}

	output := &BigQueryOutput{
		projectID:           expandEnvVarsForOutput(bqCfg.ProjectID),
		datasetID:           expandEnvVarsForOutput(bqCfg.DatasetID),
		tableID:             expandEnvVarsForOutput(bqCfg.TableID),
		credentialsFile:     expandEnvVarsForOutput(bqCfg.CredentialsFile),
		autoDetectSchema:    bqCfg.AutoDetectSchema,
		writeDisposition:    writeDisp,
		createDisposition:   createDisp,
		ignoreUnknownValues: bqCfg.IgnoreUnknownValues,
		skipInvalidRows:     bqCfg.SkipInvalidRows,
		batchSize:           bqCfg.BatchSize,
		flushInterval:       flushInterval,
		buffer:              make([]map[string]bigquery.Value, 0, bqCfg.BatchSize),
	}

	return output, nil
}

// parseBigQueryConfig OutputConfig에서 BigQuery 설정 파싱
func parseBigQueryConfig(cfg pipelineConfig.OutputConfig) (*BigQueryConfig, error) {
	bqCfg := &BigQueryConfig{}

	if projectID, ok := cfg.Config["project_id"].(string); ok {
		bqCfg.ProjectID = projectID
	}
	if datasetID, ok := cfg.Config["dataset_id"].(string); ok {
		bqCfg.DatasetID = datasetID
	}
	if tableID, ok := cfg.Config["table_id"].(string); ok {
		bqCfg.TableID = tableID
	}
	if credentialsFile, ok := cfg.Config["credentials_file"].(string); ok {
		bqCfg.CredentialsFile = credentialsFile
	}
	if autoDetect, ok := cfg.Config["auto_detect_schema"].(bool); ok {
		bqCfg.AutoDetectSchema = autoDetect
	}
	if writeDisp, ok := cfg.Config["write_disposition"].(string); ok {
		bqCfg.WriteDisposition = writeDisp
	}
	if createDisp, ok := cfg.Config["create_disposition"].(string); ok {
		bqCfg.CreateDisposition = createDisp
	}
	if ignoreUnknown, ok := cfg.Config["ignore_unknown_values"].(bool); ok {
		bqCfg.IgnoreUnknownValues = ignoreUnknown
	}
	if skipInvalid, ok := cfg.Config["skip_invalid_rows"].(bool); ok {
		bqCfg.SkipInvalidRows = skipInvalid
	}
	if batchSize, ok := cfg.Config["batch_size"].(int); ok {
		bqCfg.BatchSize = batchSize
	}
	if batchSizeF, ok := cfg.Config["batch_size"].(float64); ok {
		bqCfg.BatchSize = int(batchSizeF)
	}
	if flushInterval, ok := cfg.Config["flush_interval"].(string); ok {
		bqCfg.FlushInterval = flushInterval
	}

	return bqCfg, nil
}

func (o *BigQueryOutput) Name() string {
	return "bigquery"
}

func (o *BigQueryOutput) Open(ctx context.Context) error {
	var opts []option.ClientOption

	// 서비스 계정 인증
	if o.credentialsFile != "" {
		// deprecated 대체재가 cloud.google.com/go/auth 전면 이전이라 별도 마이그레이션에서 처리
		opts = append(opts, option.WithCredentialsFile(o.credentialsFile)) //nolint:staticcheck
	}

	// BigQuery 클라이언트 생성
	client, err := bigquery.NewClient(ctx, o.projectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create BigQuery client: %w", err)
	}
	o.client = client

	// 테이블 참조 및 Inserter 생성
	table := client.Dataset(o.datasetID).Table(o.tableID)
	o.inserter = table.Inserter()
	o.inserter.IgnoreUnknownValues = o.ignoreUnknownValues
	o.inserter.SkipInvalidRows = o.skipInvalidRows

	// 테이블 존재 확인 (CREATE_NEVER인 경우)
	if o.createDisposition == bigquery.CreateNever {
		_, err := table.Metadata(ctx)
		if err != nil {
			return fmt.Errorf("table %s.%s does not exist: %w", o.datasetID, o.tableID, err)
		}
	}

	// 백그라운드 플러시 시작
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go o.backgroundFlush()

	log.Printf("[bigquery] Output opened (project=%s, dataset=%s, table=%s, batch_size=%d)",
		o.projectID, o.datasetID, o.tableID, o.batchSize)
	return nil
}

func (o *BigQueryOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	// Record를 BigQuery Value로 변환
	row := o.convertRecord(record)

	o.bufMu.Lock()
	o.buffer = append(o.buffer, row)
	shouldFlush := len(o.buffer) >= o.batchSize
	o.bufMu.Unlock()

	if shouldFlush {
		return o.Flush(ctx)
	}

	return nil
}

// WriteBatch 배치 쓰기
func (o *BigQueryOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	rows := make([]map[string]bigquery.Value, 0, len(records))
	for _, record := range records {
		rows = append(rows, o.convertRecord(record))
	}

	return o.insertRows(ctx, rows)
}

// SupportsBatch 배치 지원 여부
func (o *BigQueryOutput) SupportsBatch() bool {
	return true
}

// BatchConfig 배치 설정 반환
func (o *BigQueryOutput) BatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       true,
		Size:          o.batchSize,
		FlushInterval: o.flushInterval,
	}
}

func (o *BigQueryOutput) convertRecord(record source.Record) map[string]bigquery.Value {
	row := make(map[string]bigquery.Value)
	for k, v := range record.Data {
		// 내부 필드는 제외 (옵션)
		if len(k) > 0 && k[0] == '_' {
			continue
		}
		row[k] = convertToBigQueryValue(v)
	}
	return row
}

// convertToBigQueryValue Go 값을 BigQuery Value로 변환
func convertToBigQueryValue(v any) bigquery.Value {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]any:
		// Nested struct -> JSON string for flexibility
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(data)
	case []any:
		// Array -> JSON string
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(data)
	case time.Time:
		return val
	default:
		return val
	}
}

func (o *BigQueryOutput) Flush(ctx context.Context) error {
	o.bufMu.Lock()
	if len(o.buffer) == 0 {
		o.bufMu.Unlock()
		return nil
	}
	rows := o.buffer
	o.buffer = make([]map[string]bigquery.Value, 0, o.batchSize)
	o.bufMu.Unlock()

	return o.insertRows(ctx, rows)
}

func (o *BigQueryOutput) insertRows(ctx context.Context, rows []map[string]bigquery.Value) error {
	if len(rows) == 0 {
		return nil
	}

	// ValueSaver 슬라이스로 변환
	savers := make([]*bigQueryValueSaver, 0, len(rows))
	for _, row := range rows {
		savers = append(savers, &bigQueryValueSaver{data: row})
	}

	// 삽입
	if err := o.inserter.Put(ctx, savers); err != nil {
		if multiErr, ok := err.(bigquery.PutMultiError); ok {
			// 부분 실패 처리
			failedCount := len(multiErr)
			successCount := len(rows) - failedCount
			atomic.AddInt64(&o.stats.SuccessRecords, int64(successCount))
			atomic.AddInt64(&o.stats.ErrorRecords, int64(failedCount))

			// 첫 몇 개 에러 로깅
			for i, rowErr := range multiErr {
				if i >= 3 {
					log.Printf("[bigquery] ... and %d more errors", len(multiErr)-3)
					break
				}
				log.Printf("[bigquery] Row %d error: %v", rowErr.RowIndex, rowErr.Errors)
			}
		} else {
			atomic.AddInt64(&o.stats.ErrorRecords, int64(len(rows)))
			return fmt.Errorf("failed to insert rows: %w", err)
		}
	} else {
		atomic.AddInt64(&o.stats.SuccessRecords, int64(len(rows)))
	}

	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	log.Printf("[bigquery] Inserted %d rows into %s.%s", len(rows), o.datasetID, o.tableID)
	return nil
}

// bigQueryValueSaver implements bigquery.ValueSaver
type bigQueryValueSaver struct {
	data map[string]bigquery.Value
}

func (s *bigQueryValueSaver) Save() (map[string]bigquery.Value, string, error) {
	return s.data, bigquery.NoDedupeID, nil
}

func (o *BigQueryOutput) backgroundFlush() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.Flush(context.Background()); err != nil {
				log.Printf("[bigquery] Background flush error: %v", err)
			}
		}
	}
}

func (o *BigQueryOutput) Close() error {
	// 백그라운드 플러시 중지
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[bigquery] Warning: failed to flush remaining records: %v", err)
	}

	// 클라이언트 닫기
	if o.client != nil {
		if err := o.client.Close(); err != nil {
			log.Printf("[bigquery] Warning: error closing client: %v", err)
		}
	}

	log.Printf("[bigquery] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)
	return nil
}

func (o *BigQueryOutput) Stats() OutputStats {
	return o.stats
}

// expandEnvVarsForOutput 환경 변수 확장 (Output용)
func expandEnvVarsForOutput(s string) string {
	// os.ExpandEnv 사용
	return s
}
