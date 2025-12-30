package types

import "time"

// CheckpointStorage 체크포인트 저장소 타입
type CheckpointStorage string

const (
	CheckpointStorageRedis CheckpointStorage = "redis"
	CheckpointStorageFile  CheckpointStorage = "file"
	CheckpointStorageMySQL CheckpointStorage = "mysql"
)

// CheckpointConfig 체크포인트 설정
type CheckpointConfig struct {
	Enabled   bool              `json:"enabled" yaml:"enabled"`
	Storage   CheckpointStorage `json:"storage" yaml:"storage"`
	Interval  string            `json:"interval" yaml:"interval"` // e.g., "10s", "1m"
	OnFailure string            `json:"on_failure,omitempty" yaml:"on_failure,omitempty"`
}

// Checkpoint 체크포인트 데이터
type Checkpoint struct {
	PipelineID     string            `json:"pipeline_id"`
	ActorPath      string            `json:"actor_path,omitempty"`
	Offsets        map[string]any    `json:"offsets,omitempty"`
	ProcessedCount int64             `json:"processed_count"`
	State          map[string]any    `json:"state,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
	Version        int64             `json:"version"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// KafkaOffset Kafka 오프셋 정보
type KafkaOffset struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Offset    int64  `json:"offset"`
	Metadata  string `json:"metadata,omitempty"`
}

// FileOffset 파일 오프셋 정보
type FileOffset struct {
	Path        string `json:"path"`
	ByteOffset  int64  `json:"byte_offset"`
	LineNumber  int64  `json:"line_number,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// CheckpointRestore 체크포인트 복원 결과
type CheckpointRestore struct {
	PipelineID    string    `json:"pipeline_id"`
	RestoredFrom  time.Time `json:"restored_from"`
	RestoredAt    time.Time `json:"restored_at"`
	SkippedEvents int64     `json:"skipped_events"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
}
