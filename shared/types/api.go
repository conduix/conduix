package types

import "time"

// ErrorCode API 에러 코드
type ErrorCode string

// 공통 에러 코드
const (
	// 인증/인가 에러 (AUTH_*)
	ErrCodeUnauthorized       ErrorCode = "AUTH_UNAUTHORIZED"
	ErrCodeInvalidToken       ErrorCode = "AUTH_INVALID_TOKEN"
	ErrCodeTokenExpired       ErrorCode = "AUTH_TOKEN_EXPIRED"
	ErrCodeForbidden          ErrorCode = "AUTH_FORBIDDEN"
	ErrCodeInsufficientPerms  ErrorCode = "AUTH_INSUFFICIENT_PERMISSIONS"

	// 요청 에러 (REQUEST_*)
	ErrCodeBadRequest         ErrorCode = "REQUEST_BAD_REQUEST"
	ErrCodeValidationFailed   ErrorCode = "REQUEST_VALIDATION_FAILED"
	ErrCodeInvalidJSON        ErrorCode = "REQUEST_INVALID_JSON"
	ErrCodeMissingField       ErrorCode = "REQUEST_MISSING_FIELD"

	// 리소스 에러 (RESOURCE_*)
	ErrCodeNotFound           ErrorCode = "RESOURCE_NOT_FOUND"
	ErrCodeAlreadyExists      ErrorCode = "RESOURCE_ALREADY_EXISTS"
	ErrCodeConflict           ErrorCode = "RESOURCE_CONFLICT"

	// 서버 에러 (SERVER_*)
	ErrCodeInternalError      ErrorCode = "SERVER_INTERNAL_ERROR"
	ErrCodeDatabaseError      ErrorCode = "SERVER_DATABASE_ERROR"
	ErrCodeExternalService    ErrorCode = "SERVER_EXTERNAL_SERVICE_ERROR"

	// 비즈니스 로직 에러 (BUSINESS_*)
	ErrCodeWorkflowRunning    ErrorCode = "BUSINESS_WORKFLOW_RUNNING"
	ErrCodeWorkflowNotRunning ErrorCode = "BUSINESS_WORKFLOW_NOT_RUNNING"
	ErrCodeHasChildren        ErrorCode = "BUSINESS_HAS_CHILDREN"
	ErrCodeInvalidState       ErrorCode = "BUSINESS_INVALID_STATE"
)

// APIError 구조화된 에러
type APIError struct {
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// Error implements error interface
func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError 새 API 에러 생성
func NewAPIError(code ErrorCode, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// NewAPIErrorWithDetails 상세 정보 포함 API 에러 생성
func NewAPIErrorWithDetails(code ErrorCode, message string, details map[string]string) *APIError {
	return &APIError{Code: code, Message: message, Details: details}
}

// APIResponse 기본 API 응답
type APIResponse[T any] struct {
	Success   bool      `json:"success"`
	Data      T         `json:"data,omitempty"`
	Error     *APIError `json:"error,omitempty"`
	Message   string    `json:"message,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
}

// PaginatedResponse 페이지네이션 응답
type PaginatedResponse[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
}

// PaginationParams 페이지네이션 파라미터
type PaginationParams struct {
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"page_size" form:"page_size"`
	SortBy   string `json:"sort_by,omitempty" form:"sort_by"`
	SortDir  string `json:"sort_dir,omitempty" form:"sort_dir"`
}

// CreatePipelineRequest 파이프라인 생성 요청
type CreatePipelineRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description,omitempty"`
	ConfigYAML  string `json:"config_yaml" binding:"required"`
}

// UpdatePipelineRequest 파이프라인 수정 요청
type UpdatePipelineRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	ConfigYAML  string `json:"config_yaml,omitempty"`
}

// PipelineControlRequest 파이프라인 제어 요청
type PipelineControlRequest struct {
	Action string `json:"action" binding:"required,oneof=start stop pause resume"`
}

// CreateScheduleRequest 스케줄 생성 요청
type CreateScheduleRequest struct {
	PipelineID     string `json:"pipeline_id" binding:"required"`
	CronExpression string `json:"cron_expression" binding:"required"`
	Enabled        bool   `json:"enabled"`
}

// UpdateScheduleRequest 스케줄 수정 요청
type UpdateScheduleRequest struct {
	CronExpression string `json:"cron_expression,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
}

// HealthStatus 헬스 상태
type HealthStatus struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Uptime    string            `json:"uptime"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// ErrorResponse 에러 응답
type ErrorResponse struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}
