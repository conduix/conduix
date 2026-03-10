package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// PartitionedHTTPSource 동적 파티션 기반 HTTP 소스
// 1. 파티션 디스커버리 URL에서 파티션 목록 조회
// 2. 각 파티션별로 URL 템플릿을 사용해 병렬 데이터 수집
type PartitionedHTTPSource struct {
	cfg       config.SourceV2
	partition *config.PartitionConfig
	auth      *config.AuthConfig
	client    *http.Client

	// OAuth2 토큰 캐시
	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// NewPartitionedHTTPSource 파티션 기반 HTTP 소스 생성
func NewPartitionedHTTPSource(cfg config.SourceV2) (*PartitionedHTTPSource, error) {
	if cfg.Partition == nil {
		return nil, fmt.Errorf("partition config is required for partitioned_http source")
	}

	// 파티션 설정 검증
	partition := cfg.Partition
	if partition.DiscoveryURL == "" && len(partition.StaticPartitions) == 0 {
		return nil, fmt.Errorf("either discovery_url or static_partitions is required")
	}
	if partition.URLTemplate == "" && cfg.URL == "" {
		return nil, fmt.Errorf("url_template or base url is required")
	}

	// 기본값 설정
	if partition.Parallelism <= 0 {
		partition.Parallelism = 4
	}
	if partition.DiscoveryMethod == "" {
		partition.DiscoveryMethod = "GET"
	}

	return &PartitionedHTTPSource{
		cfg:       cfg,
		partition: partition,
		auth:      cfg.Auth,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (s *PartitionedHTTPSource) Name() string {
	return "partitioned_http"
}

func (s *PartitionedHTTPSource) Open(ctx context.Context) error {
	// OAuth2인 경우 미리 토큰 획득
	if s.auth != nil && s.auth.Type == "oauth2" {
		if _, err := s.getOAuth2Token(ctx); err != nil {
			return fmt.Errorf("failed to get oauth2 token: %w", err)
		}
	}
	return nil
}

func (s *PartitionedHTTPSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 1. 파티션 목록 조회
		partitions, err := s.discoverPartitions(ctx)
		if err != nil {
			errs <- fmt.Errorf("partition discovery failed: %w", err)
			return
		}

		if len(partitions) == 0 {
			fmt.Println("[PartitionedHTTP] No partitions found")
			return
		}
		fmt.Printf("[PartitionedHTTP] Discovered %d partitions: %v\n", len(partitions), partitions)

		// 2. 파티션 병렬 처리
		s.fetchPartitions(ctx, partitions, records, errs)
	}()

	return records, errs
}

// discoverPartitions 파티션 목록 조회
func (s *PartitionedHTTPSource) discoverPartitions(ctx context.Context) ([]string, error) {
	// 정적 파티션이 설정된 경우 바로 반환
	if len(s.partition.StaticPartitions) > 0 {
		return s.partition.StaticPartitions, nil
	}

	// HTTP 요청으로 파티션 목록 조회
	auth := s.partition.DiscoveryAuth
	if auth == nil {
		auth = s.auth // 소스 auth 사용
	}

	response, err := s.doRequest(ctx, s.partition.DiscoveryURL, s.partition.DiscoveryMethod, nil, auth)
	if err != nil {
		return nil, fmt.Errorf("discovery request failed: %w", err)
	}

	// 응답에서 파티션 목록 추출
	return s.extractPartitions(response)
}

// extractPartitions 응답에서 파티션 목록 추출
// partition_id_path (새 방식) 또는 partition_list_path + partition_id_field (하위 호환) 지원
func (s *PartitionedHTTPSource) extractPartitions(response any) ([]string, error) {
	listPath := s.partition.GetPartitionListPath()
	idField := s.partition.GetPartitionIDField()

	var partitionList []any

	// 응답이 직접 배열인 경우
	if arr, ok := response.([]any); ok {
		partitionList = arr
	} else if obj, ok := response.(map[string]any); ok {
		// 객체에서 파티션 목록 경로로 추출
		value := getNestedValue(obj, listPath)
		if value == nil {
			return nil, fmt.Errorf("partition list not found at path: %s", listPath)
		}

		if arr, ok := value.([]any); ok {
			partitionList = arr
		} else {
			return nil, fmt.Errorf("partition list is not an array at path: %s", listPath)
		}
	} else {
		return nil, fmt.Errorf("unexpected response type for partition discovery")
	}

	// 파티션 ID 추출
	partitions := make([]string, 0, len(partitionList))
	for _, item := range partitionList {
		var partitionID string

		if idField != "" {
			// 객체에서 특정 필드 추출 (중첩 경로 지원)
			if obj, ok := item.(map[string]any); ok {
				if strings.Contains(idField, ".") {
					// 중첩 경로: getNestedValue 사용
					if id := getNestedValue(obj, idField); id != nil {
						partitionID = fmt.Sprintf("%v", id)
					}
				} else {
					if id, ok := obj[idField]; ok {
						partitionID = fmt.Sprintf("%v", id)
					}
				}
			}
		} else {
			// 배열 요소 자체가 파티션 ID
			partitionID = fmt.Sprintf("%v", item)
		}

		if partitionID != "" {
			partitions = append(partitions, partitionID)
		}
	}

	return partitions, nil
}

// fetchPartitions 파티션별 데이터 병렬 수집
func (s *PartitionedHTTPSource) fetchPartitions(ctx context.Context, partitions []string, records chan<- Record, errs chan<- error) {
	// 세마포어로 동시 실행 제한
	sem := make(chan struct{}, s.partition.Parallelism)
	var wg sync.WaitGroup

	for _, partition := range partitions {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(partitionID string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := s.fetchPartition(ctx, partitionID, records); err != nil {
				fmt.Printf("[PartitionedHTTP] Partition %s error: %v\n", partitionID, err)
				// 개별 파티션 실패는 계속 진행 (전체 실패 아님)
			}
		}(partition)
	}

	wg.Wait()
}

// fetchPartition 단일 파티션 데이터 수집
func (s *PartitionedHTTPSource) fetchPartition(ctx context.Context, partitionID string, records chan<- Record) error {
	// URL 템플릿에서 ${partition} 치환
	url := s.partition.URLTemplate
	if url == "" {
		url = s.cfg.URL
	}
	url = strings.ReplaceAll(url, "${partition}", partitionID)
	url = strings.ReplaceAll(url, "{partition}", partitionID) // 대체 문법 지원

	method := s.cfg.Method
	if method == "" {
		method = "GET"
	}

	fmt.Printf("[PartitionedHTTP] Fetching partition %s: %s\n", partitionID, url)

	response, err := s.doRequest(ctx, url, method, s.cfg.Headers, s.auth)
	if err != nil {
		return fmt.Errorf("partition %s request failed: %w", partitionID, err)
	}

	// 페이지네이션이 설정된 경우 페이지 순회
	if s.cfg.Pagination != nil {
		return s.fetchWithPagination(ctx, url, partitionID, records)
	}

	// 단일 요청 응답 처리
	return s.processResponse(response, partitionID, url, records)
}

// fetchWithPagination 페이지네이션 포함 수집
func (s *PartitionedHTTPSource) fetchWithPagination(ctx context.Context, baseURL, partitionID string, records chan<- Record) error {
	// 기존 HTTPSource의 페이지네이션 로직 재사용
	// 여기서는 간단한 next_url 방식만 구현 (필요시 확장)
	currentURL := baseURL
	pageCount := 0
	maxPages := s.cfg.Pagination.MaxPages
	if maxPages == 0 {
		maxPages = 100
	}

	for currentURL != "" && pageCount < maxPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pageCount++
		response, err := s.doRequest(ctx, currentURL, s.cfg.Method, s.cfg.Headers, s.auth)
		if err != nil {
			return fmt.Errorf("page %d: %w", pageCount, err)
		}

		if err := s.processResponse(response, partitionID, currentURL, records); err != nil {
			return err
		}

		// 다음 페이지 URL 추출
		currentURL = ""
		if obj, ok := response.(map[string]any); ok {
			urlPath := s.cfg.Pagination.URLPath
			if urlPath == "" {
				urlPath = s.cfg.Pagination.NextField
			}
			if urlPath != "" {
				if nextURL := getNestedValue(obj, urlPath); nextURL != nil {
					if urlStr, ok := nextURL.(string); ok && urlStr != "" {
						currentURL = urlStr
					}
				}
			}
		}
	}

	return nil
}

// processResponse 응답 데이터 처리
func (s *PartitionedHTTPSource) processResponse(response any, partitionID, url string, records chan<- Record) error {
	dataField := ""
	if s.cfg.Pagination != nil {
		dataField = s.cfg.Pagination.DataField
	}

	items, obj := extractItems(response, dataField)

	if len(items) > 0 {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				records <- Record{
					Data: m,
					Metadata: Metadata{
						Source:    "partitioned_http",
						Origin:    url,
						Offset:    partitionID, // 파티션 ID를 offset으로 사용
						Timestamp: time.Now().UnixMilli(),
					},
				}
			}
		}
	} else if obj != nil {
		// 단일 객체 응답
		records <- Record{
			Data: obj,
			Metadata: Metadata{
				Source:    "partitioned_http",
				Origin:    url,
				Offset:    partitionID,
				Timestamp: time.Now().UnixMilli(),
			},
		}
	}

	return nil
}

// doRequest HTTP 요청 실행
func (s *PartitionedHTTPSource) doRequest(ctx context.Context, url, method string, headers map[string]string, auth *config.AuthConfig) (any, error) {
	var bodyReader io.Reader
	if s.cfg.Body != "" {
		bodyReader = bytes.NewReader([]byte(s.cfg.Body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 기본 헤더
	req.Header.Set("Accept", "application/json")
	if s.cfg.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 커스텀 헤더
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 인증 설정
	if err := s.setAuth(ctx, req, auth); err != nil {
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

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// setAuth 인증 설정
func (s *PartitionedHTTPSource) setAuth(ctx context.Context, req *http.Request, auth *config.AuthConfig) error {
	if auth == nil {
		return nil
	}

	switch auth.Type {
	case "basic":
		req.SetBasicAuth(auth.Username, auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "oauth2":
		token, err := s.getOAuth2Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

// getOAuth2Token OAuth2 토큰 획득 (캐시 사용)
func (s *PartitionedHTTPSource) getOAuth2Token(ctx context.Context) (string, error) {
	s.tokenMu.RLock()
	if s.accessToken != "" && time.Now().Before(s.tokenExpiry) {
		token := s.accessToken
		s.tokenMu.RUnlock()
		return token, nil
	}
	s.tokenMu.RUnlock()

	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	// 다시 확인
	if s.accessToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.accessToken, nil
	}

	// 토큰 요청 (HTTPSource와 동일한 로직)
	auth := s.auth
	if auth == nil || auth.Type != "oauth2" {
		return "", fmt.Errorf("oauth2 auth config not found")
	}

	data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		auth.ClientID, auth.ClientSecret)
	if len(auth.Scopes) > 0 {
		data += "&scope=" + strings.Join(auth.Scopes, " ")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", auth.TokenURL, strings.NewReader(data))
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
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return s.accessToken, nil
}

func (s *PartitionedHTTPSource) Close() error {
	return nil
}
