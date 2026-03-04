// Package output S3 데이터 출력 구현
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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	pipelineConfig "github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// S3Output S3 데이터 출력
type S3Output struct {
	bucket       string
	region       string
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
	accessKeyID     string
	secretAccessKey string
	endpoint        string // MinIO 등 커스텀 엔드포인트

	// S3 클라이언트
	client *s3.Client

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

// S3Config S3 설정
type S3Config struct {
	Bucket          string   `yaml:"bucket" json:"bucket"`
	Region          string   `yaml:"region" json:"region"`
	PathTemplate    string   `yaml:"path_template" json:"path_template"`
	FileFormat      string   `yaml:"file_format" json:"file_format"`
	Compression     string   `yaml:"compression" json:"compression"`
	PartitionBy     []string `yaml:"partition_by" json:"partition_by"`
	MaxFileSize     string   `yaml:"max_file_size" json:"max_file_size"`
	MaxFileAge      string   `yaml:"max_file_age" json:"max_file_age"`
	BatchSize       int      `yaml:"batch_size" json:"batch_size"`
	AccessKeyID     string   `yaml:"access_key_id" json:"access_key_id"`
	SecretAccessKey string   `yaml:"secret_access_key" json:"secret_access_key"`
	Endpoint        string   `yaml:"endpoint" json:"endpoint"`
}

// NewS3Output S3 출력 생성
func NewS3Output(cfg pipelineConfig.OutputConfig) (*S3Output, error) {
	s3Cfg, err := parseS3Config(cfg)
	if err != nil {
		return nil, err
	}

	if s3Cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}

	// 기본값 설정
	if s3Cfg.Region == "" {
		s3Cfg.Region = "us-east-1"
	}
	if s3Cfg.FileFormat == "" {
		s3Cfg.FileFormat = "ndjson"
	}
	if s3Cfg.Compression == "" {
		s3Cfg.Compression = "none"
	}
	if s3Cfg.BatchSize <= 0 {
		s3Cfg.BatchSize = 1000
	}

	output := &S3Output{
		bucket:          s3Cfg.Bucket,
		region:          s3Cfg.Region,
		fileFormat:      s3Cfg.FileFormat,
		compression:     s3Cfg.Compression,
		partitionBy:     s3Cfg.PartitionBy,
		accessKeyID:     s3Cfg.AccessKeyID,
		secretAccessKey: s3Cfg.SecretAccessKey,
		endpoint:        s3Cfg.Endpoint,
		batchSize:       s3Cfg.BatchSize,
		buffer:          make([]source.Record, 0, s3Cfg.BatchSize),
	}

	// Path 템플릿 파싱
	if s3Cfg.PathTemplate != "" {
		tmpl, err := template.New("path").Parse(s3Cfg.PathTemplate)
		if err != nil {
			return nil, fmt.Errorf("invalid path template: %w", err)
		}
		output.pathTemplate = tmpl
	} else {
		output.pathPrefix = "data/"
	}

	// 파일 크기 파싱
	if s3Cfg.MaxFileSize != "" {
		size, err := parseSize(s3Cfg.MaxFileSize)
		if err != nil {
			return nil, fmt.Errorf("invalid max_file_size: %w", err)
		}
		output.maxFileSize = size
	} else {
		output.maxFileSize = 128 * 1024 * 1024 // 기본 128MB
	}

	// 파일 수명 파싱
	if s3Cfg.MaxFileAge != "" {
		age, err := time.ParseDuration(s3Cfg.MaxFileAge)
		if err != nil {
			return nil, fmt.Errorf("invalid max_file_age: %w", err)
		}
		output.maxFileAge = age
	} else {
		output.maxFileAge = 1 * time.Hour // 기본 1시간
	}

	return output, nil
}

// parseS3Config OutputConfig에서 S3 설정 파싱
func parseS3Config(cfg pipelineConfig.OutputConfig) (*S3Config, error) {
	s3Cfg := &S3Config{}

	if bucket, ok := cfg.Config["bucket"].(string); ok {
		s3Cfg.Bucket = bucket
	}
	if region, ok := cfg.Config["region"].(string); ok {
		s3Cfg.Region = region
	}
	if pathTemplate, ok := cfg.Config["path_template"].(string); ok {
		s3Cfg.PathTemplate = pathTemplate
	}
	if fileFormat, ok := cfg.Config["file_format"].(string); ok {
		s3Cfg.FileFormat = fileFormat
	}
	if compression, ok := cfg.Config["compression"].(string); ok {
		s3Cfg.Compression = compression
	}
	if partitionBy, ok := cfg.Config["partition_by"].([]interface{}); ok {
		for _, p := range partitionBy {
			if s, ok := p.(string); ok {
				s3Cfg.PartitionBy = append(s3Cfg.PartitionBy, s)
			}
		}
	}
	if maxFileSize, ok := cfg.Config["max_file_size"].(string); ok {
		s3Cfg.MaxFileSize = maxFileSize
	}
	if maxFileAge, ok := cfg.Config["max_file_age"].(string); ok {
		s3Cfg.MaxFileAge = maxFileAge
	}
	if batchSize, ok := cfg.Config["batch_size"].(int); ok {
		s3Cfg.BatchSize = batchSize
	}
	if batchSizeF, ok := cfg.Config["batch_size"].(float64); ok {
		s3Cfg.BatchSize = int(batchSizeF)
	}
	if accessKeyID, ok := cfg.Config["access_key_id"].(string); ok {
		s3Cfg.AccessKeyID = accessKeyID
	}
	if secretAccessKey, ok := cfg.Config["secret_access_key"].(string); ok {
		s3Cfg.SecretAccessKey = secretAccessKey
	}
	if endpoint, ok := cfg.Config["endpoint"].(string); ok {
		s3Cfg.Endpoint = endpoint
	}

	return s3Cfg, nil
}

// parseSize 크기 문자열 파싱 (예: "128MB", "1GB")
func parseSize(s string) (int64, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid size format")
	}

	var multiplier int64
	unit := s[len(s)-2:]
	numStr := s[:len(s)-2]

	switch unit {
	case "KB", "kb":
		multiplier = 1024
	case "MB", "mb":
		multiplier = 1024 * 1024
	case "GB", "gb":
		multiplier = 1024 * 1024 * 1024
	default:
		// 단일 문자 단위 확인
		unit = s[len(s)-1:]
		numStr = s[:len(s)-1]
		switch unit {
		case "K", "k":
			multiplier = 1024
		case "M", "m":
			multiplier = 1024 * 1024
		case "G", "g":
			multiplier = 1024 * 1024 * 1024
		case "B", "b":
			multiplier = 1
		default:
			return 0, fmt.Errorf("unknown size unit: %s", unit)
		}
	}

	var num int64
	_, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil {
		return 0, err
	}

	return num * multiplier, nil
}

func (o *S3Output) Name() string {
	return "s3"
}

func (o *S3Output) Open(ctx context.Context) error {
	// AWS 설정 로드
	var awsCfg aws.Config
	var err error

	if o.accessKeyID != "" && o.secretAccessKey != "" {
		// 명시적 자격 증명 사용
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(o.region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				o.accessKeyID,
				o.secretAccessKey,
				"",
			)),
		)
	} else {
		// 기본 자격 증명 체인 사용 (환경변수, IAM Role 등)
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(o.region),
		)
	}
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// S3 클라이언트 생성
	s3Options := func(o *s3.Options) {
		if o.Region == "" {
			o.Region = "us-east-1"
		}
	}

	// 커스텀 엔드포인트 (MinIO 등)
	if o.endpoint != "" {
		s3Options = func(opts *s3.Options) {
			opts.BaseEndpoint = aws.String(o.endpoint)
			opts.UsePathStyle = true // MinIO 호환성
		}
	}

	o.client = s3.NewFromConfig(awsCfg, s3Options)

	// 버킷 존재 확인
	_, err = o.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(o.bucket),
	})
	if err != nil {
		return fmt.Errorf("failed to access bucket %s: %w", o.bucket, err)
	}

	// 백그라운드 플러시 시작
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go o.backgroundFlush()

	o.fileStartTime = time.Now()

	log.Printf("[s3] Output opened (bucket=%s, region=%s, format=%s, compression=%s)",
		o.bucket, o.region, o.fileFormat, o.compression)
	return nil
}

func (o *S3Output) Write(ctx context.Context, record source.Record) error {
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
func (o *S3Output) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	return o.uploadRecords(ctx, records)
}

// SupportsBatch 배치 지원 여부
func (o *S3Output) SupportsBatch() bool {
	return true
}

// BatchConfig 배치 설정 반환
func (o *S3Output) BatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       true,
		Size:          o.batchSize,
		FlushInterval: o.maxFileAge,
	}
}

func (o *S3Output) Flush(ctx context.Context) error {
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

func (o *S3Output) uploadRecords(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	// 파티션별 레코드 그룹화
	partitions := o.partitionRecords(records)

	var totalErrors int64

	for partitionKey, partitionRecords := range partitions {
		// 파일 경로 생성
		key := o.generateKey(partitionKey, partitionRecords[0])

		// 데이터 직렬화
		data, err := o.serializeRecords(partitionRecords)
		if err != nil {
			log.Printf("[s3] Serialization error: %v", err)
			totalErrors += int64(len(partitionRecords))
			continue
		}

		// 압축
		if o.compression == "gzip" {
			data, err = o.compressGzip(data)
			if err != nil {
				log.Printf("[s3] Compression error: %v", err)
				totalErrors += int64(len(partitionRecords))
				continue
			}
			key += ".gz"
		}

		// S3 업로드
		_, err = o.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(o.bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(data),
			ContentType: aws.String(o.getContentType()),
		})
		if err != nil {
			log.Printf("[s3] Upload error for %s: %v", key, err)
			totalErrors += int64(len(partitionRecords))
			continue
		}

		log.Printf("[s3] Uploaded %d records to s3://%s/%s (%d bytes)",
			len(partitionRecords), o.bucket, key, len(data))
	}

	successCount := int64(len(records)) - totalErrors
	atomic.AddInt64(&o.stats.SuccessRecords, successCount)
	atomic.AddInt64(&o.stats.ErrorRecords, totalErrors)
	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	return nil
}

// partitionRecords 파티션 키별로 레코드 그룹화
func (o *S3Output) partitionRecords(records []source.Record) map[string][]source.Record {
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
func (o *S3Output) getPartitionKey(record source.Record) string {
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

// generateKey S3 객체 키 생성
func (o *S3Output) generateKey(partitionKey string, record source.Record) string {
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
			log.Printf("[s3] Failed to execute path template: %v", err)
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
func (o *S3Output) serializeRecords(records []source.Record) ([]byte, error) {
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
func (o *S3Output) serializeJSON(records []source.Record) ([]byte, error) {
	data := make([]map[string]interface{}, len(records))
	for i, r := range records {
		data[i] = r.Data
	}
	return json.Marshal(data)
}

// serializeNDJSON NDJSON (줄바꿈 구분 JSON)으로 직렬화
func (o *S3Output) serializeNDJSON(records []source.Record) ([]byte, error) {
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
func (o *S3Output) serializeCSV(records []source.Record) ([]byte, error) {
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
func (o *S3Output) compressGzip(data []byte) ([]byte, error) {
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
func (o *S3Output) getContentType() string {
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

func (o *S3Output) backgroundFlush() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.maxFileAge)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.Flush(context.Background()); err != nil {
				log.Printf("[s3] Background flush error: %v", err)
			}
		}
	}
}

func (o *S3Output) Close() error {
	// 백그라운드 플러시 중지
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[s3] Warning: failed to flush remaining records: %v", err)
	}

	log.Printf("[s3] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)
	return nil
}

func (o *S3Output) Stats() OutputStats {
	return o.stats
}

// S3OutputOption S3 출력 옵션 (테스트용)
type S3OutputOption func(*S3Output)

// WithS3Client S3 클라이언트 설정 (테스트용)
func WithS3Client(client *s3.Client) S3OutputOption {
	return func(o *S3Output) {
		o.client = client
	}
}

// SetClient S3 클라이언트 설정 (테스트용)
func (o *S3Output) SetClient(client *s3.Client) {
	o.client = client
}

// GetClient S3 클라이언트 반환 (테스트용)
func (o *S3Output) GetClient() *s3.Client {
	return o.client
}

// GenerateKeyForTest 테스트용 키 생성 (exported)
func (o *S3Output) GenerateKeyForTest(partitionKey string, record source.Record) string {
	return o.generateKey(partitionKey, record)
}

// SerializeRecordsForTest 테스트용 직렬화 (exported)
func (o *S3Output) SerializeRecordsForTest(records []source.Record) ([]byte, error) {
	return o.serializeRecords(records)
}

// CompressGzipForTest 테스트용 압축 (exported)
func (o *S3Output) CompressGzipForTest(data []byte) ([]byte, error) {
	return o.compressGzip(data)
}

// DecompressGzip Gzip 압축 해제 (테스트용)
func DecompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	return io.ReadAll(reader)
}
