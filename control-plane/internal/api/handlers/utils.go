package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// UtilsHandler 유틸리티 핸들러
type UtilsHandler struct{}

// NewUtilsHandler 유틸리티 핸들러 생성
func NewUtilsHandler() *UtilsHandler {
	return &UtilsHandler{}
}

// TestDBConnectionRequest DB 연결 테스트 요청
type TestDBConnectionRequest struct {
	ConnectionString string `json:"connection_string" binding:"required"`
}

// TestDBConnectionResponse DB 연결 테스트 응답
type TestDBConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// TestDBConnection DB 연결 테스트
// @Summary DB 연결 테스트
// @Tags utils
// @Accept json
// @Produce json
// @Param request body TestDBConnectionRequest true "Connection Info"
// @Success 200 {object} TestDBConnectionResponse
// @Router /utils/test-db-connection [post]
func (h *UtilsHandler) TestDBConnection(c *gin.Context) {
	var req TestDBConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, TestDBConnectionResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.ConnectionString == "" {
		c.JSON(http.StatusBadRequest, TestDBConnectionResponse{
			Success: false,
			Error:   "Connection string is required",
		})
		return
	}

	// 드라이버 판별
	driver := detectDBDriver(req.ConnectionString)
	if driver == "" {
		c.JSON(http.StatusBadRequest, TestDBConnectionResponse{
			Success: false,
			Error:   "Unsupported database. Supported: postgres://, mysql://",
		})
		return
	}

	// 연결 테스트 (타임아웃 5초)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()

	db, err := sql.Open(driver, req.ConnectionString)
	if err != nil {
		c.JSON(http.StatusOK, TestDBConnectionResponse{
			Success: false,
			Error:   "Failed to open connection: " + err.Error(),
		})
		return
	}
	defer db.Close()

	// Ping으로 실제 연결 확인
	err = db.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusOK, TestDBConnectionResponse{
			Success: false,
			Error:   "Connection failed: " + err.Error(),
			Latency: latency.String(),
		})
		return
	}

	c.JSON(http.StatusOK, TestDBConnectionResponse{
		Success: true,
		Message: "Connection successful",
		Latency: latency.String(),
	})
}

// detectDBDriver 연결 문자열에서 드라이버 판별
func detectDBDriver(connStr string) string {
	if len(connStr) >= 8 && connStr[:8] == "postgres" {
		return "postgres"
	}
	if len(connStr) >= 5 && connStr[:5] == "mysql" {
		return "mysql"
	}
	// MySQL DSN 형식 (user:pass@tcp(host:port)/db)
	if len(connStr) > 0 && connStr[0] != '/' {
		for _, c := range connStr {
			if c == '@' {
				return "mysql"
			}
			if c == ':' && connStr[0] != '/' {
				continue
			}
		}
	}
	return ""
}
