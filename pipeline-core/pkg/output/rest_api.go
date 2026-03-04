// Package output REST API 출력 구현
package output

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// RestAPIOutput REST API 출력
type RestAPIOutput struct {
	url          string
	method       string
	headers      map[string]string
	timeout      time.Duration
	retryCount   int
	retryDelay   time.Duration
	successCodes map[int]bool

	// 배치 설정
	batchConfig BatchConfig

	client *http.Client
	stats  OutputStats
}

// NewRestAPIOutput REST API 출력 생성
func NewRestAPIOutput(cfg config.OutputConfig) (*RestAPIOutput, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("REST API output requires url")
	}

	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	// 타임아웃 파싱
	timeout := 30 * time.Second
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout format: %w", err)
		}
		timeout = parsed
	}

	// 재시도 설정
	retryCount := cfg.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}

	retryDelay := time.Second
	if cfg.RetryDelay != "" {
		parsed, err := time.ParseDuration(cfg.RetryDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid retry_delay format: %w", err)
		}
		retryDelay = parsed
	}

	// 성공 코드 설정
	successCodes := make(map[int]bool)
	if len(cfg.SuccessCodes) > 0 {
		for _, code := range cfg.SuccessCodes {
			successCodes[code] = true
		}
	} else {
		// 기본 성공 코드
		successCodes[200] = true
		successCodes[201] = true
		successCodes[202] = true
		successCodes[204] = true
	}

	// 배치 설정
	batchConfig := DefaultBatchConfig()
	if cfg.BatchEnabled {
		batchConfig.Enabled = true
		if cfg.BatchSizeHTTP > 0 {
			batchConfig.Size = cfg.BatchSizeHTTP
		}
		if cfg.BatchDelimiter == "\n" || cfg.BatchDelimiter == "ndjson" {
			batchConfig.Format = "ndjson"
		} else {
			batchConfig.Format = "array"
		}
	}

	return &RestAPIOutput{
		url:          cfg.URL,
		method:       method,
		headers:      cfg.Headers,
		timeout:      timeout,
		retryCount:   retryCount,
		retryDelay:   retryDelay,
		successCodes: successCodes,
		batchConfig:  batchConfig,
	}, nil
}

// Name 출력 이름 반환
func (o *RestAPIOutput) Name() string {
	return "rest_api"
}

// Open 출력 열기
func (o *RestAPIOutput) Open(ctx context.Context) error {
	o.client = &http.Client{
		Timeout: o.timeout,
	}
	log.Printf("[rest_api] Output opened (url=%s, method=%s, timeout=%v, retries=%d)",
		o.url, o.method, o.timeout, o.retryCount)
	return nil
}

// Write 레코드 쓰기
func (o *RestAPIOutput) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&o.stats.TotalRecords, 1)

	// JSON 직렬화
	payload, err := json.Marshal(record.Data)
	if err != nil {
		atomic.AddInt64(&o.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// 재시도 로직
	var lastErr error
	for attempt := 0; attempt <= o.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(o.retryDelay):
			}
			log.Printf("[rest_api] Retrying request (attempt %d/%d)", attempt, o.retryCount)
		}

		err = o.sendRequest(ctx, payload)
		if err == nil {
			atomic.AddInt64(&o.stats.SuccessRecords, 1)
			o.stats.LastWriteTime = time.Now()
			return nil
		}
		lastErr = err
	}

	atomic.AddInt64(&o.stats.ErrorRecords, 1)
	return fmt.Errorf("failed after %d retries: %w", o.retryCount, lastErr)
}

// sendRequest HTTP 요청 전송
func (o *RestAPIOutput) sendRequest(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, o.method, o.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 기본 헤더
	req.Header.Set("Content-Type", "application/json")

	// 사용자 정의 헤더
	for key, value := range o.headers {
		req.Header.Set(key, value)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 응답 본문 읽기 (에러 메시지용)
	body, _ := io.ReadAll(resp.Body)

	// 성공 코드 확인
	if !o.successCodes[resp.StatusCode] {
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Flush 버퍼 플러시
func (o *RestAPIOutput) Flush(ctx context.Context) error {
	return nil
}

// Close 출력 닫기
func (o *RestAPIOutput) Close() error {
	log.Printf("[rest_api] Output closed. Total: %d, Success: %d, Errors: %d",
		o.stats.TotalRecords, o.stats.SuccessRecords, o.stats.ErrorRecords)
	return nil
}

// Stats 통계 반환
func (o *RestAPIOutput) Stats() OutputStats {
	return o.stats
}

// SupportsBatch 배치 쓰기 지원 여부
func (o *RestAPIOutput) SupportsBatch() bool {
	return o.batchConfig.Enabled
}

// BatchConfig 배치 설정 반환
func (o *RestAPIOutput) BatchConfig() BatchConfig {
	return o.batchConfig
}

// WriteBatch 여러 레코드를 한 번에 전송
func (o *RestAPIOutput) WriteBatch(ctx context.Context, records []source.Record) error {
	if len(records) == 0 {
		return nil
	}

	atomic.AddInt64(&o.stats.TotalRecords, int64(len(records)))

	// 페이로드 생성
	var payload []byte
	var err error

	if o.batchConfig.Format == "ndjson" {
		// NDJSON 포맷 (Newline Delimited JSON)
		var lines [][]byte
		for _, record := range records {
			line, marshalErr := json.Marshal(record.Data)
			if marshalErr != nil {
				atomic.AddInt64(&o.stats.ErrorRecords, 1)
				continue
			}
			lines = append(lines, line)
		}
		payload = bytes.Join(lines, []byte("\n"))
	} else {
		// JSON Array 포맷
		dataArray := make([]any, 0, len(records))
		for _, record := range records {
			dataArray = append(dataArray, record.Data)
		}
		payload, err = json.Marshal(dataArray)
		if err != nil {
			atomic.AddInt64(&o.stats.ErrorRecords, int64(len(records)))
			return fmt.Errorf("failed to marshal batch: %w", err)
		}
	}

	// 재시도 로직
	var lastErr error
	for attempt := 0; attempt <= o.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(o.retryDelay):
			}
			log.Printf("[rest_api] Retrying batch request (attempt %d/%d, records=%d)",
				attempt, o.retryCount, len(records))
		}

		err = o.sendRequest(ctx, payload)
		if err == nil {
			atomic.AddInt64(&o.stats.SuccessRecords, int64(len(records)))
			atomic.AddInt64(&o.stats.BatchCount, 1)
			o.stats.LastWriteTime = time.Now()
			log.Printf("[rest_api] Batch sent successfully (records=%d, format=%s)",
				len(records), o.batchConfig.Format)
			return nil
		}
		lastErr = err
	}

	atomic.AddInt64(&o.stats.ErrorRecords, int64(len(records)))
	return fmt.Errorf("batch failed after %d retries: %w", o.retryCount, lastErr)
}
