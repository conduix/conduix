// Package plugin 플러그인 Stage 인터페이스 및 실행기
// 외부 사용자가 커스텀 Stage를 개발할 수 있는 SDK 제공
package plugin

import (
	"context"
	"encoding/json"
)

// Record 데이터 레코드 (Stage 입출력 단위)
type Record struct {
	Data     map[string]any `json:"data"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Stage 플러그인 Stage 인터페이스
// 모든 커스텀 Stage는 이 인터페이스를 구현해야 함
type Stage interface {
	// Init Stage 초기화 (설정 로드)
	Init(config json.RawMessage) error

	// Process 단일 레코드 처리
	Process(ctx context.Context, record *Record) ([]*Record, error)

	// ProcessBatch 배치 레코드 처리 (선택 구현, 기본값은 Process 반복)
	ProcessBatch(ctx context.Context, records []*Record) ([]*Record, error)

	// Close Stage 리소스 정리
	Close() error

	// Type Stage 타입 이름 반환
	Type() string
}

// BaseStage Stage 기본 구현체
// 커스텀 Stage 개발 시 임베딩하여 사용
type BaseStage struct {
	StageType string
}

// Type Stage 타입 반환
func (s *BaseStage) Type() string {
	return s.StageType
}

// Init 기본 초기화 (no-op)
func (s *BaseStage) Init(_ json.RawMessage) error {
	return nil
}

// Close 기본 정리 (no-op)
func (s *BaseStage) Close() error {
	return nil
}

// ProcessBatch 기본 배치 처리 (개별 Process 호출)
func (s *BaseStage) ProcessBatch(ctx context.Context, records []*Record) ([]*Record, error) {
	var results []*Record
	for _, r := range records {
		processed, err := s.Process(ctx, r)
		if err != nil {
			return nil, err
		}
		results = append(results, processed...)
	}
	return results, nil
}

// Process 기본 처리 (pass-through) - 반드시 오버라이드
func (s *BaseStage) Process(_ context.Context, record *Record) ([]*Record, error) {
	return []*Record{record}, nil
}

// StageFactory Stage 생성 팩토리 함수 타입
type StageFactory func() Stage

// StageMetadata Stage 메타데이터 (등록 시 사용)
type StageMetadata struct {
	Type         string          `json:"type"`
	DisplayName  string          `json:"display_name"`
	Category     string          `json:"category"`
	Description  string          `json:"description,omitempty"`
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"` // JSON Schema
	Icon         string          `json:"icon,omitempty"`
	Color        string          `json:"color,omitempty"`
}
