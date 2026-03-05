// Package source 데이터 소스 구현
package source

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// SourceCheckpoint 소스 체크포인트 정보
type SourceCheckpoint struct {
	PartitionKey string    // 파티션 키 (예: ns/pod/container, topic-partition)
	OffsetValue  string    // 오프셋 값 (타임스탬프 또는 숫자)
	OffsetType   string    // offset 타입: timestamp, numeric, string
	RecordCount  int64     // 처리된 레코드 수
	UpdatedAt    time.Time // 마지막 업데이트 시간
}

// CheckpointableSource 체크포인트를 지원하는 소스 인터페이스
type CheckpointableSource interface {
	Source

	// GetSourceCheckpoints 현재 모든 체크포인트 반환
	GetSourceCheckpoints() []*SourceCheckpoint

	// SetSourceCheckpoints 체크포인트 설정 (재시작 시 복원용)
	SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error

	// SourceType 소스 타입 반환 (kubernetes, kafka, cdc, sql_event)
	SourceType() string
}

// NewSource 소스 설정으로 Source 생성
func NewSource(cfg config.SourceV2) (Source, error) {
	switch cfg.Type {
	case "file":
		return NewFileSource(cfg)
	case "sql":
		// partition 설정이 있으면 PartitionedSQLSource 사용
		if cfg.Partition != nil {
			return NewPartitionedSQLSource(cfg)
		}
		return NewSQLSource(cfg)
	case "http", "rest_api":
		// partition 설정이 있으면 PartitionedHTTPSource 사용
		if cfg.Partition != nil {
			return NewPartitionedHTTPSource(cfg)
		}
		return NewHTTPSource(cfg)
	case "partitioned_http":
		return NewPartitionedHTTPSource(cfg)
	case "partitioned_sql":
		return NewPartitionedSQLSource(cfg)
	case "kafka":
		return NewKafkaSource(cfg)
	case "sql_event":
		return NewSQLEventSource(cfg)
	case "cdc":
		return NewCDCSource(cfg)
	case "kubernetes", "k8s_logs":
		return NewKubernetesSource(cfg)
	case "websocket":
		return NewWebSocketSource(cfg)
	case "mqtt":
		return NewMQTTSource(cfg)
	case "sse":
		return NewSSESource(cfg)
	case "sqs":
		return NewSQSSource(cfg)
	case "rabbitmq", "amqp":
		return NewRabbitMQSource(cfg)
	case "pubsub", "gcp_pubsub":
		return NewPubSubSource(cfg)
	case "mongodb_cdc":
		return NewMongoDBCDCSource(cfg)
	case "redis_stream":
		return NewRedisStreamSource(cfg)
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

// ParseTimestamp 다양한 포맷의 타임스탬프 문자열을 파싱
// 정밀도가 높은 순서로 시도: nanoseconds -> milliseconds -> seconds
func ParseTimestamp(value string) (time.Time, error) {
	// 1. RFC3339Nano (나노초 정밀도)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}

	// 2. RFC3339 with milliseconds (밀리초 정밀도)
	if t, err := time.Parse("2006-01-02T15:04:05.000Z07:00", value); err == nil {
		return t, nil
	}

	// 3. RFC3339 (초 정밀도)
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	// 4. Unix timestamp (숫자 형태)
	// 나노초 (19자리), 밀리초 (13자리), 초 (10자리)
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		switch {
		case ts > 1e18: // 나노초 (19자리)
			return time.Unix(0, ts), nil
		case ts > 1e12: // 밀리초 (13자리)
			return time.UnixMilli(ts), nil
		default: // 초 (10자리)
			return time.Unix(ts, 0), nil
		}
	}

	// 5. Unix timestamp with decimal (예: 1234567890.123456789)
	if strings.Contains(value, ".") {
		parts := strings.SplitN(value, ".", 2)
		if len(parts) == 2 {
			sec, err1 := strconv.ParseInt(parts[0], 10, 64)
			if err1 == nil {
				// 소수점 이하를 나노초로 변환
				fracStr := parts[1]
				// 9자리로 패딩
				for len(fracStr) < 9 {
					fracStr += "0"
				}
				if len(fracStr) > 9 {
					fracStr = fracStr[:9]
				}
				nsec, err2 := strconv.ParseInt(fracStr, 10, 64)
				if err2 == nil {
					return time.Unix(sec, nsec), nil
				}
			}
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format: %s", value)
}

// ParseNumeric 다양한 포맷의 숫자 문자열을 int64로 파싱
// 정수, 실수(소수점 버림), 지수 표기법 지원
func ParseNumeric(value string) (int64, error) {
	// 1. 정수 파싱 시도
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n, nil
	}

	// 2. 실수 파싱 시도 (소수점 버림)
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return int64(f), nil
	}

	// 3. 지수 표기법 (예: 1.5e6)
	if strings.ContainsAny(value, "eE") {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return int64(f), nil
		}
	}

	return 0, fmt.Errorf("unsupported numeric format: %s", value)
}
