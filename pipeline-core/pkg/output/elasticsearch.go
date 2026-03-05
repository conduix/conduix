// Package output Elasticsearch 데이터 출력 구현
package output

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// ElasticsearchOutput Elasticsearch 데이터 출력
type ElasticsearchOutput struct {
	addresses     []string
	indexTemplate *template.Template
	indexName     string // 정적 인덱스명 (템플릿이 아닌 경우)
	idField       string
	pipeline      string

	// 인증
	authType string
	username string
	password string
	apiKey   string
	bearer   string

	// TLS
	tlsEnabled  bool
	tlsInsecure bool

	// 배치 설정
	batchSize     int
	flushInterval time.Duration

	// Retry 설정
	maxRetries     int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration

	// Index Template 설정
	indexTemplateDef     *IndexTemplateDefinition
	indexTemplateCreated bool

	// HTTP 클라이언트
	client *http.Client

	// 버퍼
	buffer []source.Record
	bufMu  sync.Mutex
	stats  OutputStats

	// 백그라운드 플러시
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ElasticsearchConfig Elasticsearch 설정
type ElasticsearchConfig struct {
	Addresses     []string                 `yaml:"addresses" json:"addresses"`
	Index         string                   `yaml:"index" json:"index"` // 정적 또는 템플릿 (예: "events-{{ .date }}")
	IDField       string                   `yaml:"id_field" json:"id_field"`
	Pipeline      string                   `yaml:"pipeline,omitempty" json:"pipeline,omitempty"`
	BulkSize      int                      `yaml:"bulk_size,omitempty" json:"bulk_size,omitempty"`
	FlushInterval string                   `yaml:"flush_interval,omitempty" json:"flush_interval,omitempty"`
	Auth          *ElasticsearchAuth       `yaml:"auth,omitempty" json:"auth,omitempty"`
	TLS           *ElasticsearchTLS        `yaml:"tls,omitempty" json:"tls,omitempty"`
	Retry         *ElasticsearchRetry      `yaml:"retry,omitempty" json:"retry,omitempty"`
	IndexTemplate *IndexTemplateDefinition `yaml:"index_template,omitempty" json:"index_template,omitempty"`
}

// ElasticsearchAuth 인증 설정
type ElasticsearchAuth struct {
	Type     string `yaml:"type" json:"type"` // basic, api_key, bearer
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	APIKey   string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Bearer   string `yaml:"bearer,omitempty" json:"bearer,omitempty"`
}

// ElasticsearchTLS TLS 설정
type ElasticsearchTLS struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
	CACert             string `yaml:"ca_cert,omitempty" json:"ca_cert,omitempty"`
}

// ElasticsearchRetry Retry 설정
type ElasticsearchRetry struct {
	MaxRetries int    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"` // 최대 재시도 횟수 (기본값: 3)
	BaseDelay  string `yaml:"base_delay,omitempty" json:"base_delay,omitempty"`   // 초기 대기 시간 (기본값: 1s)
	MaxDelay   string `yaml:"max_delay,omitempty" json:"max_delay,omitempty"`     // 최대 대기 시간 (기본값: 30s)
	RetryOn    []int  `yaml:"retry_on,omitempty" json:"retry_on,omitempty"`       // 재시도할 HTTP 상태 코드
}

// IndexTemplateDefinition Index Template 정의
type IndexTemplateDefinition struct {
	Name             string                 `yaml:"name" json:"name"`                             // 템플릿 이름
	IndexPatterns    []string               `yaml:"index_patterns" json:"index_patterns"`         // 인덱스 패턴 (예: ["events-*"])
	Priority         int                    `yaml:"priority,omitempty" json:"priority,omitempty"` // 우선순위
	NumberOfShards   int                    `yaml:"number_of_shards,omitempty" json:"number_of_shards,omitempty"`
	NumberOfReplicas int                    `yaml:"number_of_replicas,omitempty" json:"number_of_replicas,omitempty"`
	Mappings         map[string]interface{} `yaml:"mappings,omitempty" json:"mappings,omitempty"` // 필드 매핑
	Settings         map[string]interface{} `yaml:"settings,omitempty" json:"settings,omitempty"` // 추가 설정
}

// NewElasticsearchOutput Elasticsearch 출력 생성
func NewElasticsearchOutput(cfg config.OutputConfig) (*ElasticsearchOutput, error) {
	// 설정 파싱
	esCfg, err := parseElasticsearchConfig(cfg)
	if err != nil {
		return nil, err
	}

	if len(esCfg.Addresses) == 0 {
		return nil, fmt.Errorf("elasticsearch addresses are required")
	}
	if esCfg.Index == "" {
		return nil, fmt.Errorf("elasticsearch index is required")
	}

	// 배치 설정 기본값
	batchSize := esCfg.BulkSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	flushInterval := 5 * time.Second
	if esCfg.FlushInterval != "" {
		if d, err := time.ParseDuration(esCfg.FlushInterval); err == nil {
			flushInterval = d
		}
	}

	// Retry 설정 기본값
	maxRetries := 3
	retryBaseDelay := 1 * time.Second
	retryMaxDelay := 30 * time.Second
	if esCfg.Retry != nil {
		if esCfg.Retry.MaxRetries > 0 {
			maxRetries = esCfg.Retry.MaxRetries
		}
		if esCfg.Retry.BaseDelay != "" {
			if d, err := time.ParseDuration(esCfg.Retry.BaseDelay); err == nil {
				retryBaseDelay = d
			}
		}
		if esCfg.Retry.MaxDelay != "" {
			if d, err := time.ParseDuration(esCfg.Retry.MaxDelay); err == nil {
				retryMaxDelay = d
			}
		}
	}

	output := &ElasticsearchOutput{
		addresses:        esCfg.Addresses,
		idField:          esCfg.IDField,
		pipeline:         esCfg.Pipeline,
		batchSize:        batchSize,
		flushInterval:    flushInterval,
		maxRetries:       maxRetries,
		retryBaseDelay:   retryBaseDelay,
		retryMaxDelay:    retryMaxDelay,
		indexTemplateDef: esCfg.IndexTemplate,
		buffer:           make([]source.Record, 0, batchSize),
	}

	// 인덱스 템플릿 파싱
	if strings.Contains(esCfg.Index, "{{") {
		tmpl, err := template.New("index").Parse(esCfg.Index)
		if err != nil {
			return nil, fmt.Errorf("invalid index template: %w", err)
		}
		output.indexTemplate = tmpl
	} else {
		output.indexName = esCfg.Index
	}

	// 인증 설정
	if esCfg.Auth != nil {
		output.authType = esCfg.Auth.Type
		output.username = esCfg.Auth.Username
		output.password = esCfg.Auth.Password
		output.apiKey = esCfg.Auth.APIKey
		output.bearer = esCfg.Auth.Bearer
	}

	// TLS 설정
	if esCfg.TLS != nil {
		output.tlsEnabled = esCfg.TLS.Enabled
		output.tlsInsecure = esCfg.TLS.InsecureSkipVerify
	}

	return output, nil
}

// parseElasticsearchConfig OutputConfig에서 Elasticsearch 설정 파싱
func parseElasticsearchConfig(cfg config.OutputConfig) (*ElasticsearchConfig, error) {
	esCfg := &ElasticsearchConfig{}

	// 직접 필드 매핑 (OutputConfig 확장 시)
	if addresses, ok := cfg.Config["addresses"].([]interface{}); ok {
		for _, addr := range addresses {
			if s, ok := addr.(string); ok {
				esCfg.Addresses = append(esCfg.Addresses, s)
			}
		}
	}
	if index, ok := cfg.Config["index"].(string); ok {
		esCfg.Index = index
	}
	if idField, ok := cfg.Config["id_field"].(string); ok {
		esCfg.IDField = idField
	}
	if pipeline, ok := cfg.Config["pipeline"].(string); ok {
		esCfg.Pipeline = pipeline
	}
	if bulkSize, ok := cfg.Config["bulk_size"].(int); ok {
		esCfg.BulkSize = bulkSize
	}
	if bulkSizeF, ok := cfg.Config["bulk_size"].(float64); ok {
		esCfg.BulkSize = int(bulkSizeF)
	}
	if flushInterval, ok := cfg.Config["flush_interval"].(string); ok {
		esCfg.FlushInterval = flushInterval
	}

	// Auth 파싱
	if authMap, ok := cfg.Config["auth"].(map[string]interface{}); ok {
		esCfg.Auth = &ElasticsearchAuth{}
		if t, ok := authMap["type"].(string); ok {
			esCfg.Auth.Type = t
		}
		if u, ok := authMap["username"].(string); ok {
			esCfg.Auth.Username = u
		}
		if p, ok := authMap["password"].(string); ok {
			esCfg.Auth.Password = p
		}
		if k, ok := authMap["api_key"].(string); ok {
			esCfg.Auth.APIKey = k
		}
		if b, ok := authMap["bearer"].(string); ok {
			esCfg.Auth.Bearer = b
		}
	}

	// TLS 파싱
	if tlsMap, ok := cfg.Config["tls"].(map[string]interface{}); ok {
		esCfg.TLS = &ElasticsearchTLS{}
		if e, ok := tlsMap["enabled"].(bool); ok {
			esCfg.TLS.Enabled = e
		}
		if i, ok := tlsMap["insecure_skip_verify"].(bool); ok {
			esCfg.TLS.InsecureSkipVerify = i
		}
		if c, ok := tlsMap["ca_cert"].(string); ok {
			esCfg.TLS.CACert = c
		}
	}

	// Retry 파싱
	if retryMap, ok := cfg.Config["retry"].(map[string]interface{}); ok {
		esCfg.Retry = &ElasticsearchRetry{}
		if m, ok := retryMap["max_retries"].(int); ok {
			esCfg.Retry.MaxRetries = m
		}
		if m, ok := retryMap["max_retries"].(float64); ok {
			esCfg.Retry.MaxRetries = int(m)
		}
		if b, ok := retryMap["base_delay"].(string); ok {
			esCfg.Retry.BaseDelay = b
		}
		if m, ok := retryMap["max_delay"].(string); ok {
			esCfg.Retry.MaxDelay = m
		}
	}

	// Index Template 파싱
	if tmplMap, ok := cfg.Config["index_template"].(map[string]interface{}); ok {
		esCfg.IndexTemplate = &IndexTemplateDefinition{}
		if n, ok := tmplMap["name"].(string); ok {
			esCfg.IndexTemplate.Name = n
		}
		if patterns, ok := tmplMap["index_patterns"].([]interface{}); ok {
			for _, p := range patterns {
				if s, ok := p.(string); ok {
					esCfg.IndexTemplate.IndexPatterns = append(esCfg.IndexTemplate.IndexPatterns, s)
				}
			}
		}
		if p, ok := tmplMap["priority"].(int); ok {
			esCfg.IndexTemplate.Priority = p
		}
		if p, ok := tmplMap["priority"].(float64); ok {
			esCfg.IndexTemplate.Priority = int(p)
		}
		if n, ok := tmplMap["number_of_shards"].(int); ok {
			esCfg.IndexTemplate.NumberOfShards = n
		}
		if n, ok := tmplMap["number_of_shards"].(float64); ok {
			esCfg.IndexTemplate.NumberOfShards = int(n)
		}
		if n, ok := tmplMap["number_of_replicas"].(int); ok {
			esCfg.IndexTemplate.NumberOfReplicas = n
		}
		if n, ok := tmplMap["number_of_replicas"].(float64); ok {
			esCfg.IndexTemplate.NumberOfReplicas = int(n)
		}
		if m, ok := tmplMap["mappings"].(map[string]interface{}); ok {
			esCfg.IndexTemplate.Mappings = m
		}
		if s, ok := tmplMap["settings"].(map[string]interface{}); ok {
			esCfg.IndexTemplate.Settings = s
		}
	}

	return esCfg, nil
}

func (o *ElasticsearchOutput) Name() string {
	return "elasticsearch"
}

func (o *ElasticsearchOutput) Open(ctx context.Context) error {
	// HTTP 클라이언트 생성
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if o.tlsEnabled || o.tlsInsecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: o.tlsInsecure,
		}
	}

	o.client = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// 연결 테스트
	if err := o.ping(ctx); err != nil {
		return fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}

	// Index Template 자동 생성
	if o.indexTemplateDef != nil && !o.indexTemplateCreated {
		if err := o.ensureIndexTemplate(ctx); err != nil {
			log.Printf("[elasticsearch] Warning: failed to create index template: %v", err)
		} else {
			o.indexTemplateCreated = true
		}
	}

	// 백그라운드 플러시 시작
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go o.backgroundFlush()

	log.Printf("[elasticsearch] Output opened (addresses=%v, batch_size=%d, flush_interval=%s)",
		o.addresses, o.batchSize, o.flushInterval)
	return nil
}

func (o *ElasticsearchOutput) ping(ctx context.Context) error {
	for _, addr := range o.addresses {
		req, err := http.NewRequestWithContext(ctx, "GET", addr, nil)
		if err != nil {
			continue
		}
		o.setAuth(req)

		resp, err := o.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
	}
	return fmt.Errorf("all elasticsearch addresses unreachable")
}

func (o *ElasticsearchOutput) setAuth(req *http.Request) {
	switch o.authType {
	case "basic":
		req.SetBasicAuth(o.username, o.password)
	case "api_key":
		req.Header.Set("Authorization", "ApiKey "+o.apiKey)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+o.bearer)
	}
}

func (o *ElasticsearchOutput) Write(ctx context.Context, record source.Record) error {
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
func (o *ElasticsearchOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	return o.bulkIndex(ctx, records)
}

// SupportsBatch 배치 지원 여부
func (o *ElasticsearchOutput) SupportsBatch() bool {
	return true
}

// BatchConfig 배치 설정 반환
func (o *ElasticsearchOutput) BatchConfig() BatchConfig {
	return BatchConfig{
		Enabled:       true,
		Size:          o.batchSize,
		FlushInterval: o.flushInterval,
		Format:        "ndjson",
	}
}

func (o *ElasticsearchOutput) Flush(ctx context.Context) error {
	o.bufMu.Lock()
	if len(o.buffer) == 0 {
		o.bufMu.Unlock()
		return nil
	}
	records := o.buffer
	o.buffer = make([]source.Record, 0, o.batchSize)
	o.bufMu.Unlock()

	return o.bulkIndex(ctx, records)
}

func (o *ElasticsearchOutput) bulkIndex(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	// Bulk API 요청 본문 생성 (NDJSON)
	var buf bytes.Buffer
	for _, record := range records {
		// 인덱스명 결정
		indexName := o.getIndexName(record)

		// 메타 라인 (action)
		meta := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
			},
		}

		// Document ID 설정 (있는 경우)
		if o.idField != "" {
			if id, ok := record.Data[o.idField]; ok {
				meta["index"].(map[string]interface{})["_id"] = id
			}
		}

		// Pipeline 설정 (있는 경우)
		if o.pipeline != "" {
			meta["index"].(map[string]interface{})["pipeline"] = o.pipeline
		}

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			log.Printf("[elasticsearch] Failed to marshal meta: %v", err)
			continue
		}
		buf.Write(metaJSON)
		buf.WriteByte('\n')

		// 데이터 라인
		dataJSON, err := json.Marshal(record.Data)
		if err != nil {
			log.Printf("[elasticsearch] Failed to marshal data: %v", err)
			continue
		}
		buf.Write(dataJSON)
		buf.WriteByte('\n')
	}

	// Bulk API 호출
	if err := o.doBulkRequest(ctx, buf.Bytes()); err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, int64(len(records)))
		return err
	}

	atomic.AddInt64(&o.stats.SuccessRecords, int64(len(records)))
	atomic.AddInt64(&o.stats.BatchCount, 1)
	o.stats.LastWriteTime = time.Now()

	log.Printf("[elasticsearch] Bulk indexed %d documents", len(records))
	return nil
}

func (o *ElasticsearchOutput) getIndexName(record source.Record) string {
	if o.indexTemplate != nil {
		// 템플릿 데이터 준비
		data := make(map[string]interface{})
		for k, v := range record.Data {
			data[k] = v
		}

		// 날짜 관련 변수 추가
		now := time.Now()
		data["date"] = now.Format("2006.01.02")
		data["year"] = now.Format("2006")
		data["month"] = now.Format("01")
		data["day"] = now.Format("02")
		data["hour"] = now.Format("15")

		var buf bytes.Buffer
		if err := o.indexTemplate.Execute(&buf, data); err != nil {
			log.Printf("[elasticsearch] Failed to execute index template: %v", err)
			return o.indexName
		}
		return buf.String()
	}
	return o.indexName
}

func (o *ElasticsearchOutput) doBulkRequest(ctx context.Context, body []byte) error {
	var lastErr error
	delay := o.retryBaseDelay

	for attempt := 0; attempt <= o.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[elasticsearch] Retry attempt %d/%d after %v", attempt, o.maxRetries, delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			// Exponential backoff with jitter
			delay = time.Duration(float64(delay) * 2)
			if delay > o.retryMaxDelay {
				delay = o.retryMaxDelay
			}
		}

		err := o.doSingleBulkRequest(ctx, body)
		if err == nil {
			return nil
		}

		lastErr = err

		// 재시도 가능한 에러인지 확인
		if !o.isRetryableError(err) {
			return err
		}
	}

	return fmt.Errorf("bulk request failed after %d retries: %w", o.maxRetries, lastErr)
}

// isRetryableError 재시도 가능한 에러인지 확인
func (o *ElasticsearchOutput) isRetryableError(err error) bool {
	errStr := err.Error()
	// 네트워크 에러, 타임아웃, 5xx 에러는 재시도
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "status 429") || // Too Many Requests
		strings.Contains(errStr, "status 500") ||
		strings.Contains(errStr, "status 502") ||
		strings.Contains(errStr, "status 503") ||
		strings.Contains(errStr, "status 504") {
		return true
	}
	return false
}

func (o *ElasticsearchOutput) doSingleBulkRequest(ctx context.Context, body []byte) error {
	// 랜덤 주소 선택 (간단한 로드밸런싱)
	addr := o.addresses[0]
	if len(o.addresses) > 1 {
		addr = o.addresses[time.Now().UnixNano()%int64(len(o.addresses))]
	}

	url := strings.TrimSuffix(addr, "/") + "/_bulk"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create bulk request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-ndjson")
	o.setAuth(req)

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("bulk request failed: %w", err)
	}
	defer resp.Body.Close()

	// 응답 확인
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("bulk request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 에러 항목 확인
	var bulkResp struct {
		Errors bool `json:"errors"`
		Items  []struct {
			Index struct {
				ID     string `json:"_id"`
				Status int    `json:"status"`
				Error  *struct {
					Type   string `json:"type"`
					Reason string `json:"reason"`
				} `json:"error,omitempty"`
			} `json:"index"`
		} `json:"items"`
	}

	if err := json.Unmarshal(respBody, &bulkResp); err != nil {
		// 파싱 실패해도 HTTP 성공이면 OK
		return nil
	}

	if bulkResp.Errors {
		// 에러 항목 카운트
		errorCount := 0
		for _, item := range bulkResp.Items {
			if item.Index.Error != nil {
				errorCount++
				log.Printf("[elasticsearch] Bulk item error: %s - %s",
					item.Index.Error.Type, item.Index.Error.Reason)
			}
		}
		if errorCount > 0 {
			log.Printf("[elasticsearch] Bulk request had %d errors out of %d items",
				errorCount, len(bulkResp.Items))
		}
	}

	return nil
}

// ensureIndexTemplate Index Template 생성 (존재하지 않는 경우)
func (o *ElasticsearchOutput) ensureIndexTemplate(ctx context.Context) error {
	if o.indexTemplateDef == nil || o.indexTemplateDef.Name == "" {
		return nil
	}

	addr := o.addresses[0]
	templateURL := strings.TrimSuffix(addr, "/") + "/_index_template/" + o.indexTemplateDef.Name

	// 템플릿 존재 여부 확인
	checkReq, err := http.NewRequestWithContext(ctx, "HEAD", templateURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create check request: %w", err)
	}
	o.setAuth(checkReq)

	checkResp, err := o.client.Do(checkReq)
	if err != nil {
		return fmt.Errorf("failed to check template: %w", err)
	}
	checkResp.Body.Close()

	// 이미 존재하면 스킵
	if checkResp.StatusCode == 200 {
		log.Printf("[elasticsearch] Index template '%s' already exists", o.indexTemplateDef.Name)
		return nil
	}

	// 템플릿 생성
	templateBody := o.buildIndexTemplateBody()
	bodyJSON, err := json.Marshal(templateBody)
	if err != nil {
		return fmt.Errorf("failed to marshal template body: %w", err)
	}

	createReq, err := http.NewRequestWithContext(ctx, "PUT", templateURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create put request: %w", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	o.setAuth(createReq)

	createResp, err := o.client.Do(createReq)
	if err != nil {
		return fmt.Errorf("failed to create template: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("failed to create template with status %d: %s", createResp.StatusCode, string(respBody))
	}

	log.Printf("[elasticsearch] Index template '%s' created successfully", o.indexTemplateDef.Name)
	return nil
}

// buildIndexTemplateBody Index Template 본문 생성
func (o *ElasticsearchOutput) buildIndexTemplateBody() map[string]interface{} {
	def := o.indexTemplateDef

	template := map[string]interface{}{
		"index_patterns": def.IndexPatterns,
	}

	if def.Priority > 0 {
		template["priority"] = def.Priority
	}

	// Settings
	settings := make(map[string]interface{})
	if def.NumberOfShards > 0 {
		settings["number_of_shards"] = def.NumberOfShards
	}
	if def.NumberOfReplicas >= 0 {
		settings["number_of_replicas"] = def.NumberOfReplicas
	}
	// 사용자 정의 설정 추가
	for k, v := range def.Settings {
		settings[k] = v
	}

	templateSettings := map[string]interface{}{}
	if len(settings) > 0 {
		templateSettings["settings"] = settings
	}
	if len(def.Mappings) > 0 {
		templateSettings["mappings"] = def.Mappings
	}

	if len(templateSettings) > 0 {
		template["template"] = templateSettings
	}

	return template
}

func (o *ElasticsearchOutput) backgroundFlush() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.Flush(context.Background()); err != nil {
				log.Printf("[elasticsearch] Background flush error: %v", err)
			}
		}
	}
}

func (o *ElasticsearchOutput) Close() error {
	// 백그라운드 플러시 중지
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// 남은 버퍼 플러시
	if err := o.Flush(context.Background()); err != nil {
		log.Printf("[elasticsearch] Warning: failed to flush remaining records: %v", err)
	}

	log.Printf("[elasticsearch] Output closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords, o.stats.BatchCount)
	return nil
}

func (o *ElasticsearchOutput) Stats() OutputStats {
	return o.stats
}
