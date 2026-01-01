package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"
)

// RequestIDKey context key for request ID
const RequestIDKey = "request_id"

// GetRequestID extracts request ID from gin context
func GetRequestID(c *gin.Context) string {
	if id, exists := c.Get(RequestIDKey); exists {
		return id.(string)
	}
	return ""
}

// ErrorResponse sends error response with request_id (simple version)
func ErrorResponse(c *gin.Context, status int, message string) {
	ErrorResponseWithCode(c, status, types.ErrCodeInternalError, message)
}

// ErrorResponseWithCode sends error response with error code and request_id
func ErrorResponseWithCode(c *gin.Context, status int, code types.ErrorCode, message string) {
	c.JSON(status, types.APIResponse[any]{
		Success:   false,
		Error:     types.NewAPIError(code, message),
		RequestID: GetRequestID(c),
	})
}

// ErrorResponseWithDetails sends error response with error code, details and request_id
func ErrorResponseWithDetails(c *gin.Context, status int, code types.ErrorCode, message string, details map[string]string) {
	c.JSON(status, types.APIResponse[any]{
		Success:   false,
		Error:     types.NewAPIErrorWithDetails(code, message, details),
		RequestID: GetRequestID(c),
	})
}

// SuccessResponse sends success response with request_id
func SuccessResponse[T any](c *gin.Context, data T) {
	c.JSON(http.StatusOK, types.APIResponse[T]{
		Success:   true,
		Data:      data,
		RequestID: GetRequestID(c),
	})
}

// SuccessResponseWithMessage sends success response with message and request_id
func SuccessResponseWithMessage[T any](c *gin.Context, data T, message string) {
	c.JSON(http.StatusOK, types.APIResponse[T]{
		Success:   true,
		Data:      data,
		Message:   message,
		RequestID: GetRequestID(c),
	})
}

// AuthMiddleware JWT 인증 미들웨어
func AuthMiddleware(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			ErrorResponseWithCode(c, http.StatusUnauthorized, types.ErrCodeUnauthorized, "Missing authorization header")
			c.Abort()
			return
		}

		// Bearer 토큰 추출
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			ErrorResponseWithCode(c, http.StatusUnauthorized, types.ErrCodeUnauthorized, "Invalid authorization header format")
			c.Abort()
			return
		}

		tokenString := parts[1]

		// JWT 토큰 파싱
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			ErrorResponseWithCode(c, http.StatusUnauthorized, types.ErrCodeInvalidToken, "Invalid token")
			c.Abort()
			return
		}

		// 클레임에서 사용자 정보 추출
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			c.Set("user_id", claims["sub"])
			c.Set("user_email", claims["email"])
			c.Set("user_role", claims["role"])
		}

		c.Next()
	}
}

// RoleMiddleware 역할 기반 접근 제어 미들웨어
func RoleMiddleware(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			ErrorResponseWithCode(c, http.StatusForbidden, types.ErrCodeForbidden, "Access denied")
			c.Abort()
			return
		}

		roleStr := role.(string)
		for _, allowed := range allowedRoles {
			if roleStr == allowed {
				c.Next()
				return
			}
		}

		ErrorResponseWithCode(c, http.StatusForbidden, types.ErrCodeInsufficientPerms, "Insufficient permissions")
		c.Abort()
	}
}

// CORSMiddleware CORS 미들웨어
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RequestIDMiddleware 요청 ID 미들웨어
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set(RequestIDKey, requestID)
		c.Header("X-Request-ID", requestID)

		c.Next()
	}
}
