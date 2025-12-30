// Package stream provides local pipeline processing with direct function calls
// instead of actor message passing for better performance.
package stream

import (
	"context"
	"time"
)

// Record represents a single data record in the pipeline
type Record struct {
	Data      map[string]any
	Metadata  RecordMetadata
	Timestamp time.Time
}

// RecordMetadata contains metadata about the record
type RecordMetadata struct {
	Source    string
	Partition int
	Offset    int64
	Key       string
}

// Stage is the interface for pipeline stages.
// Stages are called directly (not via message passing) for performance.
// Stage is an abstraction unit: implementations can filter, transform, store, trigger, etc.
type Stage interface {
	// Name returns the stage name
	Name() string

	// Type returns the stage type (filter, remap, aggregate, elasticsearch, kafka, etc.)
	Type() string

	// Process processes a single record.
	// Returns nil to filter out the record.
	// Returns error only for unrecoverable errors.
	Process(ctx context.Context, record *Record) (*Record, error)

	// Close releases any resources
	Close() error
}

// Source is the interface for data sources.
// Sources produce records into a channel for efficient batch processing.
type Source interface {
	// Name returns the source name
	Name() string

	// Type returns the source type (kafka, file, http, etc.)
	Type() string

	// Start begins producing records to the output channel.
	// The source should close the channel when done or on context cancellation.
	Start(ctx context.Context, out chan<- *Record) error

	// Pause temporarily stops producing records
	Pause()

	// Resume continues producing records
	Resume()

	// Close releases any resources
	Close() error
}

// Sink is the interface for data sinks.
// Sinks receive records directly for efficient batch writing.
type Sink interface {
	// Name returns the sink name
	Name() string

	// Type returns the sink type (console, elasticsearch, s3, etc.)
	Type() string

	// Write writes a single record.
	// The sink should handle internal batching.
	Write(ctx context.Context, record *Record) error

	// Flush forces any buffered records to be written
	Flush(ctx context.Context) error

	// Close releases any resources, flushing first
	Close() error
}

// ProcessorStats holds statistics for stream processing
type ProcessorStats struct {
	SourceName     string
	InputCount     int64
	OutputCount    int64
	FilteredCount  int64
	ErrorCount     int64
	ProcessingTime time.Duration
	LastRecord     time.Time

	// Per-stage stats
	StageStats map[string]*StageStats
}

// StageStats holds per-stage statistics
type StageStats struct {
	Name          string
	InputCount    int64
	OutputCount   int64
	FilteredCount int64
	ErrorCount    int64
	AvgLatency    time.Duration
}

// SourceConfig is the common configuration for sources
type SourceConfig struct {
	Type       string         `yaml:"type" json:"type"`
	Name       string         `yaml:"name" json:"name"`
	BatchSize  int            `yaml:"batch_size" json:"batch_size"`
	BufferSize int            `yaml:"buffer_size" json:"buffer_size"`
	Config     map[string]any `yaml:"config" json:"config"`
}

// StageConfig is the common configuration for stages
type StageConfig struct {
	Type      string         `yaml:"type" json:"type"`
	Name      string         `yaml:"name" json:"name"`
	Condition string         `yaml:"condition,omitempty" json:"condition,omitempty"`
	Config    map[string]any `yaml:"config" json:"config"`
}

// SinkConfig is the common configuration for sinks
type SinkConfig struct {
	Type         string         `yaml:"type" json:"type"`
	Name         string         `yaml:"name" json:"name"`
	BatchSize    int            `yaml:"batch_size" json:"batch_size"`
	FlushTimeout time.Duration  `yaml:"flush_timeout" json:"flush_timeout"`
	Config       map[string]any `yaml:"config" json:"config"`
}
