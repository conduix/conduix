package source

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// HTTPSource HTTP 데이터 소스
type HTTPSource struct {
	url        string
	method     string
	headers    map[string]string
	body       string
	auth       *config.AuthConfig
	pagination *config.PaginationConfig
	client     *http.Client

	// OAuth2 토큰 캐시
	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// NewHTTPSource HTTP 소스 생성
func NewHTTPSource(cfg config.SourceV2) (*HTTPSource, error) {
	return &HTTPSource{
		url:        cfg.URL,
		method:     cfg.Method,
		headers:    cfg.Headers,
		body:       cfg.Body,
		auth:       cfg.Auth,
		pagination: cfg.Pagination,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (s *HTTPSource) Name() string {
	return "http"
}

func (s *HTTPSource) Open(ctx context.Context) error {
	// OAuth2인 경우 미리 토큰 획득
	if s.auth != nil && s.auth.Type == "oauth2" {
		if _, err := s.getOAuth2Token(ctx); err != nil {
			return fmt.Errorf("failed to get oauth2 token: %w", err)
		}
	}
	return nil
}

func (s *HTTPSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		if s.pagination != nil {
			s.readWithPagination(ctx, records, errs)
		} else {
			s.readSingle(ctx, records, errs)
		}
	}()

	return records, errs
}

func (s *HTTPSource) readSingle(ctx context.Context, records chan<- Record, errs chan<- error) {
	data, err := s.doRequest(ctx, s.url)
	if err != nil {
		errs <- err
		return
	}

	s.emitRecords(data, s.url, records)
}

func (s *HTTPSource) readWithPagination(ctx context.Context, records chan<- Record, errs chan<- error) {
	currentURL := s.url
	pageCount := 0
	maxPages := s.pagination.MaxPages
	if maxPages == 0 {
		maxPages = 100 // 기본 최대 페이지
	}

	for currentURL != "" && pageCount < maxPages {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pageCount++

		data, err := s.doRequest(ctx, currentURL)
		if err != nil {
			errs <- fmt.Errorf("page %d: %w", pageCount, err)
			return
		}

		// 데이터 추출
		var items []any
		if s.pagination.DataField != "" {
			if dataField, ok := data[s.pagination.DataField]; ok {
				if arr, ok := dataField.([]any); ok {
					items = arr
				}
			}
		} else {
			// 전체 응답이 배열인 경우
			items = []any{data}
		}

		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				records <- Record{
					Data: m,
					Metadata: Metadata{
						Source:    "http",
						Origin:    currentURL,
						Timestamp: time.Now().UnixMilli(),
					},
				}
			}
		}

		// 다음 페이지 URL 추출
		currentURL = ""
		if s.pagination.NextField != "" {
			if nextURL, ok := data[s.pagination.NextField]; ok {
				if urlStr, ok := nextURL.(string); ok && urlStr != "" {
					currentURL = urlStr
				}
			}
		}
	}
}

func (s *HTTPSource) doRequest(ctx context.Context, requestURL string) (map[string]any, error) {
	var bodyReader io.Reader
	if s.body != "" {
		bodyReader = bytes.NewReader([]byte(s.body))
	}

	req, err := http.NewRequestWithContext(ctx, s.method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 기본 헤더
	req.Header.Set("Accept", "application/json")
	if s.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 커스텀 헤더
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	// 인증 설정
	if err := s.setAuth(ctx, req); err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

func (s *HTTPSource) setAuth(ctx context.Context, req *http.Request) error {
	if s.auth == nil {
		return nil
	}

	switch s.auth.Type {
	case "basic":
		auth := base64.StdEncoding.EncodeToString(
			[]byte(s.auth.Username + ":" + s.auth.Password))
		req.Header.Set("Authorization", "Basic "+auth)

	case "bearer":
		req.Header.Set("Authorization", "Bearer "+s.auth.Token)

	case "oauth2":
		token, err := s.getOAuth2Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

func (s *HTTPSource) getOAuth2Token(ctx context.Context) (string, error) {
	// 캐시된 토큰 확인
	s.tokenMu.RLock()
	if s.accessToken != "" && time.Now().Before(s.tokenExpiry) {
		token := s.accessToken
		s.tokenMu.RUnlock()
		return token, nil
	}
	s.tokenMu.RUnlock()

	// 새 토큰 요청
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	// 다시 확인 (다른 고루틴이 갱신했을 수 있음)
	if s.accessToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.accessToken, nil
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", s.auth.ClientID)
	data.Set("client_secret", s.auth.ClientSecret)
	if len(s.auth.Scopes) > 0 {
		data.Set("scope", strings.Join(s.auth.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.auth.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	s.accessToken = tokenResp.AccessToken
	// 만료 시간에서 1분 여유 둠
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return s.accessToken, nil
}

func (s *HTTPSource) emitRecords(data map[string]any, origin string, records chan<- Record) {
	// 응답이 배열인 경우
	if arr, ok := data["data"].([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				records <- Record{
					Data: m,
					Metadata: Metadata{
						Source:    "http",
						Origin:    origin,
						Timestamp: time.Now().UnixMilli(),
					},
				}
			}
		}
		return
	}

	// 단일 객체
	records <- Record{
		Data: data,
		Metadata: Metadata{
			Source:    "http",
			Origin:    origin,
			Timestamp: time.Now().UnixMilli(),
		},
	}
}

func (s *HTTPSource) Close() error {
	return nil
}
