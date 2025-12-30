package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/schema"
)

// BaseStage provides common stage functionality
type BaseStage struct {
	name        string
	typ         string
	config      map[string]any
	inputCount  int64
	outputCount int64
	errorCount  int64
	mu          sync.Mutex
}

func (s *BaseStage) Name() string { return s.name }
func (s *BaseStage) Type() string { return s.typ }
func (s *BaseStage) Close() error { return nil }

func (s *BaseStage) incrementInput()  { s.mu.Lock(); s.inputCount++; s.mu.Unlock() }
func (s *BaseStage) incrementOutput() { s.mu.Lock(); s.outputCount++; s.mu.Unlock() }
func (s *BaseStage) incrementError()  { s.mu.Lock(); s.errorCount++; s.mu.Unlock() }

func (s *BaseStage) Stats() (input, output, errors int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputCount, s.outputCount, s.errorCount
}

// PassthroughStage passes records through unchanged
type PassthroughStage struct {
	BaseStage
}

func NewPassthroughStage(name string, config map[string]any) *PassthroughStage {
	return &PassthroughStage{
		BaseStage: BaseStage{name: name, typ: "passthrough", config: config},
	}
}

func (s *PassthroughStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()
	s.incrementOutput()
	return record, nil
}

// FilterStage filters records based on conditions
type FilterStage struct {
	BaseStage
	condition string
	evaluator func(map[string]any) bool
}

func NewFilterStage(name string, config map[string]any) *FilterStage {
	condition := ""
	if c, ok := config["condition"].(string); ok {
		condition = c
	}

	s := &FilterStage{
		BaseStage: BaseStage{name: name, typ: "filter", config: config},
		condition: condition,
	}

	// Build evaluator based on condition
	s.evaluator = s.buildEvaluator(condition)
	return s
}

func (s *FilterStage) buildEvaluator(condition string) func(map[string]any) bool {
	// Simple condition parser
	// Supports: .field == "value", .field != "value", .field exists
	switch condition {
	case ".level == \"error\"":
		return func(data map[string]any) bool {
			if level, ok := data["level"].(string); ok {
				return level == "error"
			}
			return false
		}
	case ".level == \"warn\"":
		return func(data map[string]any) bool {
			if level, ok := data["level"].(string); ok {
				return level == "warn"
			}
			return false
		}
	case ".level != \"debug\"":
		return func(data map[string]any) bool {
			if level, ok := data["level"].(string); ok {
				return level != "debug"
			}
			return true
		}
	default:
		// No condition = pass all
		return func(data map[string]any) bool { return true }
	}
}

func (s *FilterStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	if s.evaluator(record.Data) {
		s.incrementOutput()
		return record, nil
	}

	// Filtered out
	return nil, nil
}

// RemapStage transforms record fields
type RemapStage struct {
	BaseStage
	mappings map[string]string
}

func NewRemapStage(name string, config map[string]any) *RemapStage {
	mappings := make(map[string]string)
	if m, ok := config["mappings"].(map[string]any); ok {
		for k, v := range m {
			if vs, ok := v.(string); ok {
				mappings[k] = vs
			}
		}
	}

	return &RemapStage{
		BaseStage: BaseStage{name: name, typ: "remap", config: config},
		mappings:  mappings,
	}
}

func (s *RemapStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Create new data map
	newData := make(map[string]any, len(record.Data))
	for k, v := range record.Data {
		newData[k] = v
	}

	// Add timestamp
	newData["processed_at"] = time.Now().Format(time.RFC3339)

	// Try to parse JSON message
	if msg, ok := record.Data["message"].(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(msg), &parsed); err == nil {
			for k, v := range parsed {
				newData[k] = v
			}
		}
	}

	// Apply mappings
	for newKey, oldKey := range s.mappings {
		if val, ok := record.Data[oldKey]; ok {
			newData[newKey] = val
		}
	}

	record.Data = newData
	s.incrementOutput()
	return record, nil
}

// SampleStage samples a percentage of records
type SampleStage struct {
	BaseStage
	rate float64
	rng  *rand.Rand
}

func NewSampleStage(name string, config map[string]any) *SampleStage {
	rate := 1.0
	if r, ok := config["rate"].(float64); ok {
		rate = r
	}

	return &SampleStage{
		BaseStage: BaseStage{name: name, typ: "sample", config: config},
		rate:      rate,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *SampleStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	if s.rng.Float64() < s.rate {
		s.incrementOutput()
		return record, nil
	}

	// Sampled out
	return nil, nil
}

// EnrichStage enriches records with additional data
type EnrichStage struct {
	BaseStage
	staticFields map[string]any
}

func NewEnrichStage(name string, config map[string]any) *EnrichStage {
	staticFields := make(map[string]any)
	if fields, ok := config["fields"].(map[string]any); ok {
		staticFields = fields
	}

	return &EnrichStage{
		BaseStage:    BaseStage{name: name, typ: "enrich", config: config},
		staticFields: staticFields,
	}
}

func (s *EnrichStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Add enrichment fields
	for k, v := range s.staticFields {
		record.Data[k] = v
	}

	if lookupTable, ok := s.config["lookup_table"].(string); ok {
		record.Data["enriched_from"] = lookupTable
	}

	s.incrementOutput()
	return record, nil
}

// AggregateStage aggregates records over time windows
type AggregateStage struct {
	BaseStage
	windowSize time.Duration
	groupBy    []string
	aggregates map[string]string // field -> aggregation type (sum, count, avg, etc.)

	// Window state
	windowMu    sync.Mutex //nolint:unused
	windowStart time.Time
	buckets     map[string]*aggregateBucket
}

type aggregateBucket struct {
	count  int64   //nolint:unused
	sum    float64 //nolint:unused
	min    float64 //nolint:unused
	max    float64 //nolint:unused
	values []any   //nolint:unused
}

func NewAggregateStage(name string, config map[string]any) *AggregateStage {
	windowSize := 60 * time.Second
	if ws, ok := config["window"].(string); ok {
		if d, err := time.ParseDuration(ws); err == nil {
			windowSize = d
		}
	}

	var groupBy []string
	if gb, ok := config["group_by"].([]any); ok {
		for _, g := range gb {
			if gs, ok := g.(string); ok {
				groupBy = append(groupBy, gs)
			}
		}
	}

	aggregates := make(map[string]string)
	if agg, ok := config["aggregates"].(map[string]any); ok {
		for k, v := range agg {
			if vs, ok := v.(string); ok {
				aggregates[k] = vs
			}
		}
	}

	return &AggregateStage{
		BaseStage:   BaseStage{name: name, typ: "aggregate", config: config},
		windowSize:  windowSize,
		groupBy:     groupBy,
		aggregates:  aggregates,
		windowStart: time.Now(),
		buckets:     make(map[string]*aggregateBucket),
	}
}

func (s *AggregateStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// For now, pass through (full aggregation requires windowing logic)
	s.incrementOutput()
	return record, nil
}

// ValidationStage validates records against a schema
// Used for input validation (after Reader)
type ValidationStage struct {
	BaseStage
	schema     *schema.DataSchema
	dropOnFail bool // true면 검증 실패 시 레코드 드롭, false면 에러 반환
}

// NewValidationStage creates a new validation stage
func NewValidationStage(name string, config map[string]any) (*ValidationStage, error) {
	s := &ValidationStage{
		BaseStage:  BaseStage{name: name, typ: "validate", config: config},
		dropOnFail: false,
	}

	// drop_on_fail 설정
	if drop, ok := config["drop_on_fail"].(bool); ok {
		s.dropOnFail = drop
	}

	// 스키마 설정 파싱
	if schemaConfig, ok := config["schema"].(map[string]any); ok {
		sch, err := schema.NewDataSchemaFromConfig(schemaConfig)
		if err != nil {
			return nil, fmt.Errorf("invalid schema config: %w", err)
		}
		s.schema = sch
	} else {
		return nil, fmt.Errorf("schema configuration is required for validation stage")
	}

	return s, nil
}

// Process validates the record against the schema
func (s *ValidationStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	if s.schema == nil {
		s.incrementOutput()
		return record, nil
	}

	if err := s.schema.Validate(record.Data); err != nil {
		s.incrementError()
		if s.dropOnFail {
			// 드롭 모드: 검증 실패 레코드 무시
			return nil, nil
		}
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	s.incrementOutput()
	return record, nil
}

// SetSchema sets the schema for validation (for programmatic use)
func (s *ValidationStage) SetSchema(sch *schema.DataSchema) {
	s.schema = sch
}

// NewStage creates a stage from configuration
func NewStage(cfg StageConfig) (Stage, error) {
	switch cfg.Type {
	case "passthrough":
		return NewPassthroughStage(cfg.Name, cfg.Config), nil
	case "filter":
		return NewFilterStage(cfg.Name, cfg.Config), nil
	case "remap":
		return NewRemapStage(cfg.Name, cfg.Config), nil
	case "sample":
		return NewSampleStage(cfg.Name, cfg.Config), nil
	case "enrich":
		return NewEnrichStage(cfg.Name, cfg.Config), nil
	case "aggregate":
		return NewAggregateStage(cfg.Name, cfg.Config), nil
	case "validate":
		return NewValidationStage(cfg.Name, cfg.Config)
	default:
		return nil, fmt.Errorf("unknown stage type: %s", cfg.Type)
	}
}
