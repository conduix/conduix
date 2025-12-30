package types

import "time"

// APIResponse 기본 API 응답
type APIResponse[T any] struct {
	Success   bool   `json:"success"`
	Data      T      `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
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
