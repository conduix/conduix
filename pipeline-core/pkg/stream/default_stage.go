package stream

import (
	"context"
	"fmt"
)

// DefaultStage sets default values for null or empty fields
type DefaultStage struct {
	BaseStage
	defaults map[string]any
	onlyNull bool
}

// NewDefaultStage creates a new default stage from config
func NewDefaultStage(name string, config map[string]any) (*DefaultStage, error) {
	s := &DefaultStage{
		BaseStage: BaseStage{name: name, typ: "default", config: config},
		onlyNull:  true, // default: only replace nil values
	}

	// Parse defaults map
	if d, ok := config["defaults"].(map[string]any); ok {
		s.defaults = d
	}
	if len(s.defaults) == 0 {
		return nil, fmt.Errorf("default stage requires at least one default value")
	}

	// Parse only_null (default true)
	if on, ok := config["only_null"].(bool); ok {
		s.onlyNull = on
	}

	return s, nil
}

// Process sets default values for null or empty fields in the record
func (s *DefaultStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	for field, defaultVal := range s.defaults {
		val, exists := record.Data[field]

		if !exists || val == nil {
			// Field missing or nil -> set default
			record.Data[field] = defaultVal
			continue
		}

		// If only_null is false, also replace empty strings
		if !s.onlyNull {
			if strVal, ok := val.(string); ok && strVal == "" {
				record.Data[field] = defaultVal
			}
		}
	}

	s.incrementOutput()
	return record, nil
}
