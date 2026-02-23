// Package sink REST API 출력 구현
package sink

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

// RestAPISink REST API 출력
type RestAPISink struct {
	url          string
	method       string
	headers      map[string]string
	timeout      time.Duration
	retryCount   int
	retryDelay   time.Duration
	successCodes map[int]bool

	client *http.Client
	stats  SinkStats
}

// NewRestAPISink REST API 싱크 생성
func NewRestAPISink(cfg config.OutputConfig) (*RestAPISink, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("REST API sink requires url")
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

	return &RestAPISink{
		url:          cfg.URL,
		method:       method,
		headers:      cfg.Headers,
		timeout:      timeout,
		retryCount:   retryCount,
		retryDelay:   retryDelay,
		successCodes: successCodes,
	}, nil
}

// Name 싱크 이름 반환
func (s *RestAPISink) Name() string {
	return "rest_api"
}

// Open 싱크 열기
func (s *RestAPISink) Open(ctx context.Context) error {
	s.client = &http.Client{
		Timeout: s.timeout,
	}
	log.Printf("[rest_api] Sink opened (url=%s, method=%s, timeout=%v, retries=%d)",
		s.url, s.method, s.timeout, s.retryCount)
	return nil
}

// Write 레코드 쓰기
func (s *RestAPISink) Write(ctx context.Context, record source.Record) error {
	atomic.AddInt64(&s.stats.TotalRecords, 1)

	// JSON 직렬화
	payload, err := json.Marshal(record.Data)
	if err != nil {
		atomic.AddInt64(&s.stats.ErrorRecords, 1)
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// 재시도 로직
	var lastErr error
	for attempt := 0; attempt <= s.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.retryDelay):
			}
			log.Printf("[rest_api] Retrying request (attempt %d/%d)", attempt, s.retryCount)
		}

		err = s.sendRequest(ctx, payload)
		if err == nil {
			atomic.AddInt64(&s.stats.SuccessRecords, 1)
			s.stats.LastWriteTime = time.Now()
			return nil
		}
		lastErr = err
	}

	atomic.AddInt64(&s.stats.ErrorRecords, 1)
	return fmt.Errorf("failed after %d retries: %w", s.retryCount, lastErr)
}

// sendRequest HTTP 요청 전송
func (s *RestAPISink) sendRequest(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, s.method, s.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 기본 헤더
	req.Header.Set("Content-Type", "application/json")

	// 사용자 정의 헤더
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 응답 본문 읽기 (에러 메시지용)
	body, _ := io.ReadAll(resp.Body)

	// 성공 코드 확인
	if !s.successCodes[resp.StatusCode] {
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Flush 버퍼 플러시
func (s *RestAPISink) Flush(ctx context.Context) error {
	return nil
}

// Close 싱크 닫기
func (s *RestAPISink) Close() error {
	log.Printf("[rest_api] Sink closed. Total: %d, Success: %d, Errors: %d",
		s.stats.TotalRecords, s.stats.SuccessRecords, s.stats.ErrorRecords)
	return nil
}

// Stats 통계 반환
func (s *RestAPISink) Stats() SinkStats {
	return s.stats
}
