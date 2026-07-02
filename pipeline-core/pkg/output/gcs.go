// Package output GCS (Google Cloud Storage) 데이터 출력 구현
package output

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	pipelineConfig "github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// GCSOutput GCS 데이터 출력
type GCSOutput struct {
	bucket       string
	pathTemplate *template.Template
	pathPrefix   string

	// 파일 포맷
	fileFormat  string // json, ndjson, csv
	compression string // none, gzip

	// 파티셔닝
	partitionBy []string

	// 파일 로테이션
	maxFileSize int64
	maxFileAge  time.Duration

	// 인증
	credentialsFile string // 서비스 계정 JSON 파일 경로
	projectID       string

	// GCS 클라이언트
	client *storage.Client

	// 버퍼
	buffer    []source.Record
	bufMu     sync.Mutex
	stats     OutputStats
	batchSize int

	// 백그라운드 플러시
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 현재 파일 상태
	fileStartTime time.Time
}

// GCSConfig GCS 설정
type GCSConfig struct {
	Bucket          string   `yaml:"bucket" json:"bucket"`
	ProjectID       string   `yaml:"project_id" json:"project_id"`
	PathTemplate    string   `yaml:"path_template" json:"path_template"`
	FileFormat      string   `yaml:"file_format" json:"file_format"`
	Compression     string   `yaml:"compression" json:"compression"`
	PartitionBy     []string `yaml:"partition_by" json:"partition_by"`
	MaxFileSize     string   `yaml:"max_file_size" json:"max_file_size"`
	MaxFileAge      string   `yaml:"max_file_age" json:"max_file_age"`
	BatchSize       int      `yaml:"batch_size" json:"batch_size"`
	CredentialsFile string   `yaml:"credentials_file" json:"credentials_file"`
}

// NewGCSOutput GCS 출력 생성
func NewGCSOutput(cfg pipelineConfig.OutputConfig) (*GCSOutput, error) {
	gcsCfg, err := parseGCSConfig(cfg)
	if err != nil {
		return nil, err
	}

	if gcsCfg.Bucket == "" {
		return nil, fmt.Errorf("gcs bucket is required")
	}

	// 기본값 설정
	if gcsCfg.FileFormat == "" {
		gcsCfg.FileFormat = "ndjson"
	}
	if gcsCfg.Compression == "" {
		gcsCfg.Compression = "none"
	}
	if gcsCfg.BatchSize <= 0 {
		gcsCfg.BatchSize = 1000
	}

	output := &GCSOutput{
		bucket:          gcsCfg.Bucket,
		projectID:       gcsCfg.ProjectID,
		fileFormat:      gcsCfg.FileFormat,
		compression:     gcsCfg.Compression,
		partitionBy:     gcsCfg.PartitionBy,
		credentialsFile: gcsCfg.CredentialsFile,
		batchSize:       gcsCfg.BatchSize,
		buffer:          make([]source.Record, 0, gcsCfg.BatchSize),
	}

	// Path 템플릿 파싱
	if gcsCfg.PathTemplate != "" {
		tmpl, err := template.New("path").Parse(gcsCfg.PathTemplate)
		if err != nil {
			return nil, fmt.Errorf("invalid path template: %w", err)
		}
		output.pathTemplate = tmpl
	} else {
		output.pathPrefix = "data/"
	}

	// 파일 크기 파싱
	if gcsCfg.MaxFileSize != "" {
		size, err := parseSize(gcsCfg.MaxFileSize)
		if err != nil {
			return nil, fmt.Errorf("invalid max_file_size: %w", err)
		}
		output.maxFileSize = size
	} else {
		output.maxFileSize = 128 * 1024 * 1024 // 기본 128MB
	}

	// 파일 수명 파싱
	if gcsCfg.MaxFileAge != "" {
		age, err := time.ParseDuration(gcsCfg.MaxFileAge)
		if err != nil {
			return nil, fmt.Errorf("invalid max_file_age: %w", err)
		}
		output.maxFileAge = age
	} else {
		output.maxFileAge = 1 * time.Hour // 기본 1시간
	}

	return output, nil
}

// parseGCSConfig OutputConfig에서 GCS 설정 파싱
func parseGCSConfig(cfg pipelineConfig.OutputConfig) (*GCSConfig, error) {
	gcsCfg := &GCSConfig{}

	if bucket, ok := cfg.Config["bucket"].(string); ok {
		gcsCfg.Bucket = bucket
	}
	if projectID, ok := cfg.Config["project_id"].(string); ok {
		gcsCfg.ProjectID = projectID
	}
	if pathTemplate, ok := cfg.Config["path_template"].(string); ok {
		gcsCfg.PathTemplate = pathTemplate
	}
	if fileFormat, ok := cfg.Config["file_format"].(string); ok {
		gcsCfg.FileFormat = fileFormat
	}
	if compression, ok := cfg.Config["compression"].(string); ok {
		gcsCfg.Compression = compression
	}
	if partitionBy, ok := cfg.Config["partition_by"].([]interface{}); ok {
		for _, p := range partitionBy {
			if s, ok := p.(string); ok {
				gcsCfg.PartitionBy = append(gcsCfg.PartitionBy, s)
			}
		}
	}
	if maxFileSize, ok := cfg.Config["max_file_size"].(string); ok {
		gcsCfg.MaxFileSize = maxFileSize
	}
	if maxFileAge, ok := cfg.Config["max_file_age"].(string); ok {
		gcsCfg.MaxFileAge = maxFileAge
	}
	if batchSize, ok := cfg.Config["batch_size"].(int); ok {
		gcsCfg.BatchSize = batchSize
	}
	if batchSizeF, ok := cfg.Config["batch_size"].(float64); ok {
		gcsCfg.BatchSize = int(batchSizeF)
	}
	if credentialsFile, ok := cfg.Config["credentials_file"].(string); ok {
		gcsCfg.CredentialsFile = credentialsFile
	}

	return gcsCfg, nil
}

func (o *GCSOutput) Name() string {
	return "gcs"
}

func (o *GCSOutput) Open(ctx context.Context) error {
	var opts []option.ClientOption

	// 서비스 계정 인증
	if o.credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(o.credentialsFile))
	}
	// 그 외는 기본 자격 증명 사용 (GOOGLE_APPLICATION_CREDENTIALS 환경변수, GCE 메타데이터 등)

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create GCS client: %w", err)
	}
	o.client = client

	// 버킷 존재 확인
	bucket := o.client.Bucket(o.bucket)
	_, err = bucket.Attrs(ctx)
	if err != nil {
		return fmt.Errorf("failed to access bucket %s: %w", o.bucket, err)
	}

	// 백그라운드 플러시 시작
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go o.backgroundFlush()

	o.fileStartTime = time.Now()

	log.Printf("[gcs] Output opened (bucket=%s, format=%s, compression=%s)",
		o.bucket, o.fileFormat, o.compression)
	return nil
}

func (o *GCSOutput) Write(ctx context.Context, record source.Record) error {
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

// WriteBatch 배치 쓰기
func (o *GCSOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	return o.uploadRecords(ctx, records)
}

// SupportsBatch 배치 지원 여부
func (o *GCSOutput) SupportsBatch() bool {
	return true
}

// BatchConfig 배치 설정 반환
func (o *GCSOutput) BatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       true,
		Size:          o.batchSize,
		FlushInterval: o.maxFileAge,
	}
}

func (o *GCSOutput) Flush(ctx context.Context) error {
	o.bufMu.Lock()
	if len(o.buffer) == 0 {
		o.bufMu.Unlock()
		return nil
	}
	records := o.buffer
	o.buffer = make([]source.Record, 0, o.batchSize)
	o.bufMu.Unlock()

	return o.uploadRecords(ctx, records)
}

func (o *GCSOutput) uploadRecords(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	// 파티션별 레코드 그룹화
	partitions := o.partitionRecords(records)

	var totalErrors int64

	for partitionKey, partitionRecords := range partitions {
		// 파일 경로 생성
		objectName := o.generateObjectName(partitionKey, partitionRecords[0])

		// 데이터 직렬화
		data, err := o.serializeRecords(partitionRecords)
		if err != nil {
			log.Printf("[gcs] Serialization error: %v", err)
			totalErrors += int64(len(partitionRecords))
			continue
		}

		// 압축
		if o.compression == "gzip" {
			data, err = o.compressGzip(data)
			if err != nil {
				log.Printf("[gcs] Compression error: %v", err)
				totalErrors += int64(len(partitionRecords))
				continue
			}
			objectName += ".gz"
		}

		// GCS 업로드
		bucket := o.client.Bucket(o.bucket)
		obj := bucket.Object(objectName)
		writer := obj.NewWriter(ctx)
		writer.ContentType = o.getContentType()

		if _, err := writer.Write(data); err != nil {
			log.Printf("[gcs] Write error for %s: %v", objectName, err)
			totalErrors += int64(len(partitionRecords))
			_ = writer.Close()
			continue
		}

		if err := writer.Close(); err != nil {
			log.Printf("[gcs] Close error for %s: %v", objectName, err)
			totalErrors += int64(len(partitionRecords))
			continue
		}

		log.Printf("[gcs] Uploaded %d records to gs://%s/%s (%d bytes)",
			len(partitionRecords), o.bucket, objectName, len(data))
	}

	successCount := int64(len(records)) - totalErrors
	atomic.AddInt64(&o.stats.SuccessRecords, successCount)
	atomic.AddInt64(&o.stats.ErrorRecords, totalErrors)
	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	return nil
}

// partitionRecords 파티션 키별로 레코드 그룹화
func (o *GCSOutput) partitionRecords(records []source.Record) map[string][]source.Record {
	if len(o.partitionBy) == 0 {
		return map[string][]source.Record{"": records}
	}

	partitions := make(map[string][]source.Record)

	for _, record := range records {
		key := o.getPartitionKey(record)
		partitions[key] = append(partitions[key], record)
	}

	return partitions
}

// getPartitionKey 레코드에서 파티션 키 생성
func (o *GCSOutput) getPartitionKey(record source.Record) string {
	if len(o.partitionBy) == 0 {
		return ""
	}

	var parts []string
	for _, field := range o.partitionBy {
		val := ""
		if v, ok := record.Data[field]; ok {
			val = fmt.Sprintf("%v", v)
		}
		parts = append(parts, fmt.Sprintf("%s=%s", field, val))
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "/"
		}
		result += part
	}
	return result
}

// generateObjectName GCS 객체 이름 생성
func (o *GCSOutput) generateObjectName(partitionKey string, record source.Record) string {
	now := time.Now()

	// 템플릿 데이터
	data := make(map[string]interface{})
	for k, v := range record.Data {
		data[k] = v
	}
	data["year"] = now.Format("2006")
	data["month"] = now.Format("01")
	data["day"] = now.Format("02")
	data["hour"] = now.Format("15")
	data["minute"] = now.Format("04")
	data["timestamp"] = now.Unix()

	var basePath string
	if o.pathTemplate != nil {
		var buf bytes.Buffer
		if err := o.pathTemplate.Execute(&buf, data); err != nil {
			log.Printf("[gcs] Failed to execute path template: %v", err)
			basePath = o.pathPrefix
		} else {
			basePath = buf.String()
		}
	} else {
		basePath = o.pathPrefix
	}

	// 파티션 경로 추가
	if partitionKey != "" {
		basePath = basePath + partitionKey + "/"
	}

	// 파일명 생성
	filename := fmt.Sprintf("%s_%d.%s",
		now.Format("20060102_150405"),
		now.UnixNano()%1000000,
		o.fileFormat)

	return basePath + filename
}

// serializeRecords 레코드를 지정된 포맷으로 직렬화
func (o *GCSOutput) serializeRecords(records []source.Record) ([]byte, error) {
	switch o.fileFormat {
	case "json":
		return o.serializeJSON(records)
	case "ndjson", "jsonl":
		return o.serializeNDJSON(records)
	case "csv":
		return o.serializeCSV(records)
	default:
		return o.serializeNDJSON(records)
	}
}

// serializeJSON JSON 배열로 직렬화
func (o *GCSOutput) serializeJSON(records []source.Record) ([]byte, error) {
	data := make([]map[string]interface{}, len(records))
	for i, r := range records {
		data[i] = r.Data
	}
	return json.Marshal(data)
}

// serializeNDJSON NDJSON (줄바꿈 구분 JSON)으로 직렬화
func (o *GCSOutput) serializeNDJSON(records []source.Record) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)

	for _, r := range records {
		if err := encoder.Encode(r.Data); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// serializeCSV CSV로 직렬화
func (o *GCSOutput) serializeCSV(records []source.Record) ([]byte, error) {
	if len(records) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// 헤더 추출 (첫 번째 레코드 기준)
	var headers []string
	for k := range records[0].Data {
		headers = append(headers, k)
	}

	// 헤더 쓰기
	if err := writer.Write(headers); err != nil {
		return nil, err
	}

	// 데이터 쓰기
	for _, r := range records {
		row := make([]string, len(headers))
		for i, h := range headers {
			if v, ok := r.Data[h]; ok {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	return buf.Bytes(), writer.Error()
}

// compressGzip Gzip 압축
func (o *GCSOutput) compressGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)

	if _, err := gzWriter.Write(data); err != nil {
		return nil, err
	}
	if err := gzWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// getContentType Content-Type 반환
func (o *GCSOutput) getContentType() string {
	switch o.fileFormat {
	case "json":
		return "application/json"
	case "ndjson", "jsonl":
		return "application/x-ndjson"
	case "csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

func (o *GCSOutput) backgroundFlush() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.maxFileAge)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.Flush(context.Background()); err != nil {
				log.Printf("[gcs] Background flush error: %v", err)
			}
		}
	}
}

func (o *GCSOutput) Close() error {
	// 백그라운드 플러시 중지
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[gcs] Warning: failed to flush remaining records: %v", err)
	}

	// GCS 클라이언트 닫기
	if o.client != nil {
		if err := o.client.Close(); err != nil {
			log.Printf("[gcs] Warning: failed to close client: %v", err)
		}
	}

	log.Printf("[gcs] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)
	return nil
}

func (o *GCSOutput) Stats() OutputStats {
	return o.stats
}

// SetClient GCS 클라이언트 설정 (테스트용)
func (o *GCSOutput) SetClient(client *storage.Client) {
	o.client = client
}

// GetGCSClient GCS 클라이언트 반환 (테스트용)
func (o *GCSOutput) GetGCSClient() *storage.Client {
	return o.client
}

// GenerateObjectNameForTest 테스트용 객체명 생성 (exported)
func (o *GCSOutput) GenerateObjectNameForTest(partitionKey string, record source.Record) string {
	return o.generateObjectName(partitionKey, record)
}

// SerializeRecordsForTest 테스트용 직렬화 (exported)
func (o *GCSOutput) SerializeRecordsForTest(records []source.Record) ([]byte, error) {
	return o.serializeRecords(records)
}

// CompressGzipForTest 테스트용 압축 (exported)
func (o *GCSOutput) CompressGzipForTest(data []byte) ([]byte, error) {
	return o.compressGzip(data)
}

// DecompressGzipGCS Gzip 압축 해제 (테스트용)
func DecompressGzipGCS(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	return io.ReadAll(reader)
}
