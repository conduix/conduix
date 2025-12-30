// Package source 데이터 소스 구현
package source

import (
	"context"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// Record 데이터 레코드
type Record struct {
	Data     map[string]any // 실제 데이터
	Metadata Metadata       // 메타데이터
}

// Metadata 레코드 메타데이터
type Metadata struct {
	Source    string // 소스 타입 (file, sql, http, kafka)
	Origin    string // 원본 위치 (파일 경로, URL 등)
	Offset    string // 오프셋 (Kafka 등)
	Timestamp int64  // 수신 시간
}

// Source 데이터 소스 인터페이스
type Source interface {
	// Open 소스 연결
	Open(ctx context.Context) error

	// Read 데이터 읽기 (채널로 반환)
	Read(ctx context.Context) (<-chan Record, <-chan error)

	// Close 소스 닫기
	Close() error

	// Name 소스 이름
	Name() string
}

// NewSource 소스 설정으로 Source 생성
func NewSource(cfg config.SourceV2) (Source, error) {
	switch cfg.Type {
	case "file":
		return NewFileSource(cfg)
	case "sql":
		return NewSQLSource(cfg)
	case "http", "rest_api":
		return NewHTTPSource(cfg)
	case "kafka":
		return NewKafkaSource(cfg)
	case "sql_event":
		return NewSQLEventSource(cfg)
	case "cdc":
		return NewCDCSource(cfg)
	default:
		return nil, &UnsupportedSourceError{Type: cfg.Type}
	}
}

// UnsupportedSourceError 지원하지 않는 소스 타입 에러
type UnsupportedSourceError struct {
	Type string
}

func (e *UnsupportedSourceError) Error() string {
	return "unsupported source type: " + e.Type
}
