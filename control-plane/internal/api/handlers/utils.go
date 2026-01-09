package handlers

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// UtilsHandler 유틸리티 핸들러
type UtilsHandler struct {
	httpClient *http.Client
}

// NewUtilsHandler 유틸리티 핸들러 생성
func NewUtilsHandler() *UtilsHandler {
	return &UtilsHandler{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// ConnectionTestResponse 공통 연결 테스트 응답
type ConnectionTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Latency string `json:"latency,omitempty"`
	Details any    `json:"details,omitempty"`
}

// ==================== SQL ====================

// TestDBConnectionRequest DB 연결 테스트 요청
type TestDBConnectionRequest struct {
	ConnectionString string `json:"connection_string" binding:"required"`
}

// TestDBConnection DB 연결 테스트
func (h *UtilsHandler) TestDBConnection(c *gin.Context) {
	var req TestDBConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.ConnectionString == "" {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Connection string is required",
		})
		return
	}

	driver := detectDBDriver(req.ConnectionString)
	if driver == "" {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Unsupported database. Supported: postgres://, mysql://",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()

	db, err := sql.Open(driver, req.ConnectionString)
	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Failed to open connection: " + err.Error(),
		})
		return
	}
	defer db.Close()

	err = db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: true,
		Message: "Connection successful",
		Latency: latency.String(),
	})
}

func detectDBDriver(connStr string) string {
	if len(connStr) >= 8 && connStr[:8] == "postgres" {
		return "postgres"
	}
	if len(connStr) >= 5 && connStr[:5] == "mysql" {
		return "mysql"
	}
	if len(connStr) > 0 && connStr[0] != '/' {
		for _, c := range connStr {
			if c == '@' {
				return "mysql"
			}
		}
	}
	return ""
}

// ==================== Elasticsearch ====================

// TestElasticsearchRequest Elasticsearch 연결 테스트 요청
type TestElasticsearchRequest struct {
	Addresses []string `json:"addresses" binding:"required"`
	Username  string   `json:"username,omitempty"`
	Password  string   `json:"password,omitempty"`
	Index     string   `json:"index,omitempty"`
}

// TestElasticsearch Elasticsearch 연결 테스트
func (h *UtilsHandler) TestElasticsearch(c *gin.Context) {
	var req TestElasticsearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if len(req.Addresses) == 0 {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "At least one address is required",
		})
		return
	}

	start := time.Now()

	// 첫 번째 주소로 클러스터 health 확인
	addr := req.Addresses[0]
	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}

	healthURL := strings.TrimSuffix(addr, "/") + "/_cluster/health"

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "GET", healthURL, nil)
	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Failed to create request: " + err.Error(),
		})
		return
	}

	if req.Username != "" && req.Password != "" {
		httpReq.SetBasicAuth(req.Username, req.Password)
	}

	resp, err := h.httpClient.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Authentication failed (401 Unauthorized)",
			Latency: latency.String(),
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   fmt.Sprintf("Unexpected status: %d", resp.StatusCode),
			Latency: latency.String(),
		})
		return
	}

	// 클러스터 정보 파싱
	body, _ := io.ReadAll(resp.Body)
	var health map[string]any
	_ = json.Unmarshal(body, &health)

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: true,
		Message: fmt.Sprintf("Cluster '%s' is %s", health["cluster_name"], health["status"]),
		Latency: latency.String(),
		Details: map[string]any{
			"cluster_name": health["cluster_name"],
			"status":       health["status"],
			"nodes":        health["number_of_nodes"],
		},
	})
}

// ==================== Kafka ====================

// TestKafkaRequest Kafka 연결 테스트 요청
type TestKafkaRequest struct {
	Brokers []string `json:"brokers" binding:"required"`
	Topic   string   `json:"topic,omitempty"`
}

// TestKafka Kafka 브로커 연결 테스트
func (h *UtilsHandler) TestKafka(c *gin.Context) {
	var req TestKafkaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if len(req.Brokers) == 0 {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "At least one broker is required",
		})
		return
	}

	start := time.Now()

	// TCP 연결 테스트 (각 브로커에 대해)
	var connectedBrokers []string
	var failedBrokers []string

	for _, broker := range req.Brokers {
		conn, err := net.DialTimeout("tcp", broker, 5*time.Second)
		if err != nil {
			failedBrokers = append(failedBrokers, broker)
			continue
		}
		conn.Close()
		connectedBrokers = append(connectedBrokers, broker)
	}

	latency := time.Since(start)

	if len(connectedBrokers) == 0 {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "All brokers are unreachable",
			Latency: latency.String(),
			Details: map[string]any{
				"failed_brokers": failedBrokers,
			},
		})
		return
	}

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: true,
		Message: fmt.Sprintf("%d/%d brokers reachable", len(connectedBrokers), len(req.Brokers)),
		Latency: latency.String(),
		Details: map[string]any{
			"connected_brokers": connectedBrokers,
			"failed_brokers":    failedBrokers,
		},
	})
}

// ==================== MongoDB ====================

// TestMongoDBRequest MongoDB 연결 테스트 요청
type TestMongoDBRequest struct {
	URI        string `json:"uri" binding:"required"`
	Database   string `json:"database,omitempty"`
	Collection string `json:"collection,omitempty"`
}

// TestMongoDB MongoDB 연결 테스트
func (h *UtilsHandler) TestMongoDB(c *gin.Context) {
	var req TestMongoDBRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.URI == "" {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "URI is required",
		})
		return
	}

	start := time.Now()

	// MongoDB URI에서 호스트 추출하여 TCP 연결 테스트
	host := extractMongoHost(req.URI)
	if host == "" {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid MongoDB URI format",
		})
		return
	}

	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}
	conn.Close()

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: true,
		Message: fmt.Sprintf("MongoDB reachable at %s", host),
		Latency: latency.String(),
	})
}

func extractMongoHost(uri string) string {
	// mongodb://user:pass@host:port/db -> host:port
	uri = strings.TrimPrefix(uri, "mongodb://")
	uri = strings.TrimPrefix(uri, "mongodb+srv://")

	// @ 이후 부분 추출
	if idx := strings.Index(uri, "@"); idx != -1 {
		uri = uri[idx+1:]
	}

	// / 이전 부분 (호스트) 추출
	if idx := strings.Index(uri, "/"); idx != -1 {
		uri = uri[:idx]
	}

	// ? 이전 부분 추출
	if idx := strings.Index(uri, "?"); idx != -1 {
		uri = uri[:idx]
	}

	// 포트 없으면 기본 포트 추가
	if !strings.Contains(uri, ":") {
		uri += ":27017"
	}

	return uri
}

// ==================== S3 ====================

// TestS3Request S3 연결 테스트 요청
type TestS3Request struct {
	Bucket          string `json:"bucket" binding:"required"`
	Region          string `json:"region" binding:"required"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"` // For MinIO or custom S3
}

// TestS3 S3 버킷 연결 테스트
func (h *UtilsHandler) TestS3(c *gin.Context) {
	var req TestS3Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.Bucket == "" || req.Region == "" {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Bucket and region are required",
		})
		return
	}

	start := time.Now()

	// S3 엔드포인트 URL 구성
	var s3URL string
	if req.Endpoint != "" {
		s3URL = strings.TrimSuffix(req.Endpoint, "/") + "/" + req.Bucket
	} else {
		s3URL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", req.Bucket, req.Region)
	}

	// HEAD 요청으로 버킷 존재 확인
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "HEAD", s3URL, nil)
	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Failed to create request: " + err.Error(),
		})
		return
	}

	resp, err := h.httpClient.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}
	defer resp.Body.Close()

	// 200, 403(권한없음), 301(리전 다름) 등은 버킷이 존재함을 의미
	if resp.StatusCode == 200 || resp.StatusCode == 403 || resp.StatusCode == 301 {
		msg := "Bucket exists"
		switch resp.StatusCode {
		case 403:
			msg = "Bucket exists (access denied - check credentials)"
		case 301:
			msg = "Bucket exists (wrong region)"
		}
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: resp.StatusCode == 200,
			Message: msg,
			Latency: latency.String(),
			Details: map[string]any{
				"status_code": resp.StatusCode,
				"endpoint":    s3URL,
			},
		})
		return
	}

	if resp.StatusCode == 404 {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Bucket not found",
			Latency: latency.String(),
		})
		return
	}

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: false,
		Error:   fmt.Sprintf("Unexpected status: %d", resp.StatusCode),
		Latency: latency.String(),
	})
}

// ==================== REST API ====================

// TestRESTAPIRequest REST API 연결 테스트 요청
type TestRESTAPIRequest struct {
	URL     string            `json:"url" binding:"required"`
	Method  string            `json:"method,omitempty"` // GET, POST, etc. (default: GET)
	Headers map[string]string `json:"headers,omitempty"`
}

// TestRESTAPI REST API 엔드포인트 연결 테스트
func (h *UtilsHandler) TestRESTAPI(c *gin.Context) {
	var req TestRESTAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.URL == "" {
		c.JSON(http.StatusBadRequest, ConnectionTestResponse{
			Success: false,
			Error:   "URL is required",
		})
		return
	}

	method := req.Method
	if method == "" {
		method = "GET"
	}

	start := time.Now()

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), method, req.URL, nil)
	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Failed to create request: " + err.Error(),
		})
		return
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := h.httpClient.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, ConnectionTestResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}
	defer resp.Body.Close()

	// 2xx, 3xx는 성공으로 처리
	success := resp.StatusCode >= 200 && resp.StatusCode < 400

	c.JSON(http.StatusOK, ConnectionTestResponse{
		Success: success,
		Message: fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		Latency: latency.String(),
		Details: map[string]any{
			"status_code":    resp.StatusCode,
			"content_type":   resp.Header.Get("Content-Type"),
			"content_length": resp.ContentLength,
		},
	})
}
