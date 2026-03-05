package source

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	rateLimit  *config.RateLimitSourceConfig
	client     *http.Client

	// OAuth2 토큰 캐시
	tokenMu      sync.RWMutex
	accessToken  string
	refreshToken string // Refresh token (런타임에 갱신될 수 있음)
	tokenExpiry  time.Time

	// PKCE 상태
	pkceCodeVerifier string

	// Rate limiting
	rateLimitMu  sync.Mutex
	lastRequest  time.Time
	requestCount int
	windowStart  time.Time
}

// NewHTTPSource HTTP 소스 생성
func NewHTTPSource(cfg config.SourceV2) (*HTTPSource, error) {
	// HTTP 클라이언트 생성 (TLS 설정 포함)
	httpClient, err := buildHTTPClient(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	source := &HTTPSource{
		url:        cfg.URL,
		method:     cfg.Method,
		headers:    cfg.Headers,
		body:       cfg.Body,
		auth:       cfg.Auth,
		pagination: cfg.Pagination,
		rateLimit:  cfg.RateLimit,
		client:     httpClient,
	}

	// OAuth2 설정 초기화
	if cfg.Auth != nil && cfg.Auth.Type == "oauth2" {
		// Refresh token 초기화 (환경변수 지원)
		if cfg.Auth.RefreshToken != "" {
			source.refreshToken = expandEnvVars(cfg.Auth.RefreshToken)
		}

		// PKCE code verifier 초기화 또는 생성
		if cfg.Auth.UsePKCE {
			if cfg.Auth.PKCECodeVerifier != "" {
				source.pkceCodeVerifier = cfg.Auth.PKCECodeVerifier
			} else {
				// 자동 생성 (43-128자 사이의 랜덤 문자열)
				source.pkceCodeVerifier = generateCodeVerifier()
			}
		}
	}

	return source, nil
}

// buildHTTPClient TLS/mTLS 설정을 포함한 HTTP 클라이언트 생성
func buildHTTPClient(auth *config.AuthConfig) (*http.Client, error) {
	// 기본 타임아웃
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// mTLS 설정이 있는 경우
	if auth != nil && auth.Type == "mtls" && auth.TLS != nil && auth.TLS.Enabled {
		tlsConfig, err := buildHTTPTLSConfig(auth.TLS)
		if err != nil {
			return nil, err
		}

		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		fmt.Println("[http] mTLS enabled")
	}

	return client, nil
}

// buildHTTPTLSConfig HTTP 클라이언트용 TLS 설정 생성
func buildHTTPTLSConfig(cfg *config.TLSClientConfig) (*tls.Config, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.SkipVerify,
	}

	// 서버 이름 (SNI)
	if cfg.ServerName != "" {
		tlsConfig.ServerName = cfg.ServerName
	}

	// CA 인증서
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(expandEnvVars(cfg.CACert))
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// 클라이언트 인증서 (mTLS)
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(
			expandEnvVars(cfg.ClientCert),
			expandEnvVars(cfg.ClientKey),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
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
	response, err := s.doRequest(ctx, s.url)
	if err != nil {
		errs <- err
		return
	}

	// 배열 또는 객체 응답 처리
	items, obj := extractItems(response, "")
	if len(items) > 0 {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				records <- Record{
					Data: m,
					Metadata: Metadata{
						Source:    "http",
						Origin:    s.url,
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
				Source:    "http",
				Origin:    s.url,
				Timestamp: time.Now().UnixMilli(),
			},
		}
	}
}

func (s *HTTPSource) readWithPagination(ctx context.Context, records chan<- Record, errs chan<- error) {
	switch s.pagination.Type {
	case "offset", "page_increment", "page_with_count":
		s.readWithOffsetPagination(ctx, records, errs)
	case "next_url":
		s.readWithNextURLPagination(ctx, records, errs)
	case "next_offset":
		s.readWithNextOffsetPagination(ctx, records, errs)
	default:
		s.readWithNextURLPagination(ctx, records, errs)
	}
}

func (s *HTTPSource) readWithOffsetPagination(ctx context.Context, records chan<- Record, errs chan<- error) {
	maxPages := s.pagination.MaxPages
	if maxPages == 0 {
		maxPages = 100
	}

	perPage := s.pagination.PerPage
	if perPage == 0 {
		perPage = 10
	}

	// UI 호환: ParamName 우선, PageParam 폴백
	pageParam := s.pagination.ParamName
	if pageParam == "" {
		pageParam = s.pagination.PageParam
	}
	if pageParam == "" {
		pageParam = "page"
	}

	perPageParam := s.pagination.PerPageParam
	if perPageParam == "" {
		perPageParam = "perPage"
	}

	// UI 호환: StartValue 우선, StartPage 폴백
	startPage := s.pagination.StartValue
	if startPage == 0 {
		startPage = s.pagination.StartPage
	}
	if startPage == 0 {
		startPage = 1
	}

	baseURL, err := url.Parse(s.url)
	if err != nil {
		errs <- fmt.Errorf("invalid base URL: %w", err)
		return
	}

	for page := startPage; page < startPage+maxPages; page++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// URL에 페이지 파라미터 추가
		query := baseURL.Query()
		query.Set(pageParam, fmt.Sprintf("%d", page))
		query.Set(perPageParam, fmt.Sprintf("%d", perPage))
		baseURL.RawQuery = query.Encode()
		currentURL := baseURL.String()

		response, err := s.doRequest(ctx, currentURL)
		if err != nil {
			errs <- fmt.Errorf("page %d: %w", page, err)
			return
		}

		// 데이터 추출 (배열 또는 객체 응답 모두 지원)
		items, respObj := extractItems(response, s.pagination.DataField)

		// 데이터가 없으면 종료
		if len(items) == 0 {
			if respObj != nil {
				fmt.Printf("[HTTP] No items found on page %d (dataField=%s, response keys=%v)\n", page, s.pagination.DataField, getMapKeys(respObj))
			} else {
				fmt.Printf("[HTTP] No items found on page %d (response is not array or object)\n", page)
			}
			return
		}
		fmt.Printf("[HTTP] Page %d: found %d items\n", page, len(items))

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

		// totalCount 기반 종료 체크 (객체 응답인 경우만)
		if respObj != nil && s.pagination.TotalField != "" {
			if totalVal, ok := respObj[s.pagination.TotalField]; ok {
				var total int
				switch v := totalVal.(type) {
				case float64:
					total = int(v)
				case int:
					total = v
				}
				if total > 0 && page*perPage >= total {
					return
				}
			}
		}

		// 현재 페이지 데이터가 perPage보다 적으면 마지막 페이지
		if len(items) < perPage {
			return
		}
	}
}

func (s *HTTPSource) readWithNextOffsetPagination(ctx context.Context, records chan<- Record, errs chan<- error) {
	maxPages := s.pagination.MaxPages
	if maxPages == 0 {
		maxPages = 100
	}

	offsetParam := s.pagination.OffsetParam
	if offsetParam == "" {
		offsetParam = "offset"
	}

	baseURL, err := url.Parse(s.url)
	if err != nil {
		errs <- fmt.Errorf("invalid base URL: %w", err)
		return
	}

	var currentOffset int64 = 0
	pageCount := 0

	for pageCount < maxPages {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pageCount++

		// URL에 offset 파라미터 추가
		query := baseURL.Query()
		query.Set(offsetParam, fmt.Sprintf("%d", currentOffset))
		baseURL.RawQuery = query.Encode()
		currentURL := baseURL.String()

		response, err := s.doRequest(ctx, currentURL)
		if err != nil {
			errs <- fmt.Errorf("offset %d: %w", currentOffset, err)
			return
		}

		// 데이터 추출 (배열 또는 객체 응답 모두 지원)
		items, respObj := extractItems(response, s.pagination.DataField)

		// 데이터가 없으면 종료
		if len(items) == 0 {
			return
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

		// 다음 offset 추출 (응답에서 가져오기, 객체 응답인 경우만)
		if respObj != nil && s.pagination.OffsetPath != "" {
			nextOffset := getNestedValue(respObj, s.pagination.OffsetPath)
			if nextOffset == nil {
				return // 다음 offset이 없으면 종료
			}
			switch v := nextOffset.(type) {
			case float64:
				currentOffset = int64(v)
			case int:
				currentOffset = int64(v)
			case int64:
				currentOffset = v
			default:
				return
			}
		} else {
			// 응답에 offset이 없으면 items 수만큼 증가
			currentOffset += int64(len(items))
		}
	}
}

func (s *HTTPSource) readWithNextURLPagination(ctx context.Context, records chan<- Record, errs chan<- error) {
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

		response, err := s.doRequest(ctx, currentURL)
		if err != nil {
			errs <- fmt.Errorf("page %d: %w", pageCount, err)
			return
		}

		// 데이터 추출 (배열 또는 객체 응답 모두 지원)
		items, respObj := extractItems(response, s.pagination.DataField)

		// 배열도 아니고 객체에서 데이터도 못 찾은 경우, 객체 자체를 단일 아이템으로 처리
		if len(items) == 0 && respObj != nil {
			items = []any{respObj}
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

		// 다음 페이지 URL 추출 (객체 응답인 경우만)
		currentURL = ""

		if respObj != nil {
			// URLPath 우선 (nested path 지원), NextField 폴백
			nextURLPath := s.pagination.URLPath
			if nextURLPath == "" {
				nextURLPath = s.pagination.NextField
			}

			if nextURLPath != "" {
				// 점으로 구분된 경로 지원 (예: links.next)
				if strings.Contains(nextURLPath, ".") {
					if nextURL := getNestedValue(respObj, nextURLPath); nextURL != nil {
						if urlStr, ok := nextURL.(string); ok && urlStr != "" {
							currentURL = urlStr
						}
					}
				} else {
					// 단순 필드명
					if nextURL, ok := respObj[nextURLPath]; ok {
						if urlStr, ok := nextURL.(string); ok && urlStr != "" {
							currentURL = urlStr
						}
					}
				}
			}
		}
	}
}

// waitForRateLimit 레이트 리밋 적용
func (s *HTTPSource) waitForRateLimit(ctx context.Context) error {
	if s.rateLimit == nil || !s.rateLimit.Enabled || s.rateLimit.Rate <= 0 {
		return nil
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	// interval을 duration으로 변환
	var windowDuration time.Duration
	switch s.rateLimit.Interval {
	case "second":
		windowDuration = time.Second
	case "minute":
		windowDuration = time.Minute
	case "hour":
		windowDuration = time.Hour
	default:
		windowDuration = time.Second
	}

	now := time.Now()

	// 윈도우가 지났으면 리셋
	if now.Sub(s.windowStart) >= windowDuration {
		s.windowStart = now
		s.requestCount = 0
	}

	// 요청 수가 rate를 초과하면 대기
	if s.requestCount >= s.rateLimit.Rate {
		waitTime := windowDuration - now.Sub(s.windowStart)
		if waitTime > 0 {
			fmt.Printf("[HTTP] Rate limit reached (%d/%d per %s), waiting %v\n",
				s.requestCount, s.rateLimit.Rate, s.rateLimit.Interval, waitTime)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
			}

			// 대기 후 윈도우 리셋
			s.windowStart = time.Now()
			s.requestCount = 0
		}
	}

	s.requestCount++
	s.lastRequest = time.Now()

	return nil
}

func (s *HTTPSource) doRequest(ctx context.Context, requestURL string) (any, error) {
	// Rate limiting 적용
	if err := s.waitForRateLimit(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait canceled: %w", err)
	}

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

	// any 타입으로 디코딩 (배열 또는 객체 모두 지원)
	var result any
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

	case "api_key":
		// API Key 인증: header 또는 query parameter로 전달
		apiKey := expandEnvVars(s.auth.APIKey)
		keyName := s.auth.APIKeyName
		if keyName == "" {
			keyName = "X-API-Key" // 기본 헤더 이름
		}

		keyIn := s.auth.APIKeyIn
		if keyIn == "" {
			keyIn = "header" // 기본: 헤더로 전달
		}

		switch keyIn {
		case "header":
			req.Header.Set(keyName, apiKey)
		case "query":
			// 쿼리 파라미터로 API Key 추가
			q := req.URL.Query()
			q.Set(keyName, apiKey)
			req.URL.RawQuery = q.Encode()
		}

	case "mtls":
		// mTLS는 HTTP 클라이언트 레벨에서 처리됨 (Transport에 TLS 설정)
		// 여기서는 추가 작업 불필요
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

	// Grant type 결정
	grantType := s.auth.GrantType
	if grantType == "" {
		// 기본값: refresh token이 있으면 refresh_token, 없으면 client_credentials
		if s.refreshToken != "" {
			grantType = "refresh_token"
		} else {
			grantType = "client_credentials"
		}
	}

	var tokenResp *oauth2TokenResponse
	var err error

	switch grantType {
	case "refresh_token":
		tokenResp, err = s.requestTokenWithRefreshToken(ctx)
		if err != nil {
			// Refresh token 실패 시 client_credentials로 폴백 (가능한 경우)
			if s.auth.ClientSecret != "" {
				fmt.Printf("[oauth2] Refresh token failed, falling back to client_credentials: %v\n", err)
				tokenResp, err = s.requestTokenWithClientCredentials(ctx)
			}
		}
	case "authorization_code":
		// Authorization code flow는 외부에서 code를 받아와야 함
		// 여기서는 PKCE code_verifier만 준비
		return "", fmt.Errorf("authorization_code flow requires external authorization; use refresh_token after initial auth")
	default:
		// client_credentials (기본)
		tokenResp, err = s.requestTokenWithClientCredentials(ctx)
	}

	if err != nil {
		return "", err
	}

	s.accessToken = tokenResp.AccessToken
	// 만료 시간에서 1분 여유 둠
	if tokenResp.ExpiresIn > 0 {
		s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		// ExpiresIn이 없으면 1시간 기본값
		s.tokenExpiry = time.Now().Add(59 * time.Minute)
	}

	// 새 refresh token이 발급되면 저장
	if tokenResp.RefreshToken != "" {
		s.refreshToken = tokenResp.RefreshToken
	}

	return s.accessToken, nil
}

// oauth2TokenResponse OAuth2 토큰 응답
type oauth2TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// requestTokenWithClientCredentials client_credentials grant로 토큰 요청
func (s *HTTPSource) requestTokenWithClientCredentials(ctx context.Context) (*oauth2TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", s.auth.ClientID)
	data.Set("client_secret", expandEnvVars(s.auth.ClientSecret))
	if len(s.auth.Scopes) > 0 {
		data.Set("scope", strings.Join(s.auth.Scopes, " "))
	}

	return s.doTokenRequest(ctx, data)
}

// requestTokenWithRefreshToken refresh_token grant로 토큰 갱신
func (s *HTTPSource) requestTokenWithRefreshToken(ctx context.Context) (*oauth2TokenResponse, error) {
	if s.refreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", s.refreshToken)
	data.Set("client_id", s.auth.ClientID)
	// client_secret은 선택 (public client의 경우 불필요)
	if s.auth.ClientSecret != "" {
		data.Set("client_secret", expandEnvVars(s.auth.ClientSecret))
	}
	if len(s.auth.Scopes) > 0 {
		data.Set("scope", strings.Join(s.auth.Scopes, " "))
	}

	return s.doTokenRequest(ctx, data)
}

// doTokenRequest 공통 토큰 요청 로직
func (s *HTTPSource) doTokenRequest(ctx context.Context, data url.Values) (*oauth2TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", s.auth.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// PKCE 관련 함수들

// generateCodeVerifier PKCE code_verifier 생성 (RFC 7636)
// 43-128자 사이의 unreserved characters
func generateCodeVerifier() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	const length = 64

	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// fallback to less random but still valid
		for i := range b {
			b[i] = charset[i%len(charset)]
		}
	} else {
		for i := range b {
			b[i] = charset[int(b[i])%len(charset)]
		}
	}
	return string(b)
}

// generateCodeChallenge PKCE code_challenge 생성
func generateCodeChallenge(verifier string, method string) string {
	if method == "plain" {
		return verifier
	}
	// S256 (기본)
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// GetPKCEAuthURL PKCE Authorization URL 생성 (외부에서 사용자 인증용)
func (s *HTTPSource) GetPKCEAuthURL(state string) (string, error) {
	if s.auth == nil || !s.auth.UsePKCE {
		return "", fmt.Errorf("PKCE not enabled")
	}
	if s.auth.AuthURL == "" {
		return "", fmt.Errorf("auth_url is required for PKCE")
	}

	challengeMethod := s.auth.PKCEChallengeMethod
	if challengeMethod == "" {
		challengeMethod = "S256"
	}

	codeChallenge := generateCodeChallenge(s.pkceCodeVerifier, challengeMethod)

	u, err := url.Parse(s.auth.AuthURL)
	if err != nil {
		return "", fmt.Errorf("invalid auth_url: %w", err)
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", s.auth.ClientID)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", challengeMethod)
	if s.auth.RedirectURL != "" {
		q.Set("redirect_uri", s.auth.RedirectURL)
	}
	if len(s.auth.Scopes) > 0 {
		q.Set("scope", strings.Join(s.auth.Scopes, " "))
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// ExchangeAuthorizationCode PKCE authorization code를 토큰으로 교환
func (s *HTTPSource) ExchangeAuthorizationCode(ctx context.Context, code string) error {
	if s.auth == nil || !s.auth.UsePKCE {
		return fmt.Errorf("PKCE not enabled")
	}

	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", s.auth.ClientID)
	data.Set("code_verifier", s.pkceCodeVerifier)
	if s.auth.RedirectURL != "" {
		data.Set("redirect_uri", s.auth.RedirectURL)
	}
	// Public client는 client_secret 불필요
	if s.auth.ClientSecret != "" {
		data.Set("client_secret", expandEnvVars(s.auth.ClientSecret))
	}

	tokenResp, err := s.doTokenRequest(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	s.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		s.tokenExpiry = time.Now().Add(59 * time.Minute)
	}

	if tokenResp.RefreshToken != "" {
		s.refreshToken = tokenResp.RefreshToken
	}

	return nil
}

// GetRefreshToken 현재 refresh token 반환 (저장용)
func (s *HTTPSource) GetRefreshToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.refreshToken
}

// SetRefreshToken refresh token 설정 (복원용)
func (s *HTTPSource) SetRefreshToken(token string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.refreshToken = token
}

func (s *HTTPSource) Close() error {
	return nil
}

// getMapKeys map의 키 목록 반환 (디버깅용)
func getMapKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// extractItems 응답에서 데이터 배열 추출
// - 응답이 배열이면 직접 반환
// - 응답이 객체면 dataField에서 추출 (기본값: "data")
func extractItems(response any, dataField string) ([]any, map[string]any) {
	// 응답이 직접 배열인 경우
	if arr, ok := response.([]any); ok {
		return arr, nil
	}

	// 응답이 객체인 경우
	if obj, ok := response.(map[string]any); ok {
		fieldName := dataField
		if fieldName == "" {
			fieldName = "data"
		}

		if dataValue, ok := obj[fieldName]; ok {
			if arr, ok := dataValue.([]any); ok {
				return arr, obj
			}
		}
		return nil, obj
	}

	return nil, nil
}

// getNestedValue JSON 객체에서 점으로 구분된 경로의 값을 가져옴
// 예: "meta.pagination.next_offset" -> data["meta"]["pagination"]["next_offset"]
func getNestedValue(data map[string]any, path string) any {
	parts := strings.Split(path, ".")
	current := any(data)

	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
			if current == nil {
				return nil
			}
		} else {
			return nil
		}
	}

	return current
}
