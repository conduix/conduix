package stream

import (
	"context"
	"fmt"
)

// DropStage removes specified fields from record data
type DropStage struct {
	BaseStage
	fields []string
}

// NewDropStage creates a new drop stage from config
func NewDropStage(name string, config map[string]any) (*DropStage, error) {
	s := &DropStage{
		BaseStage: BaseStage{name: name, typ: "drop", config: config},
	}

	// Parse fields (comes as []any from JSON/YAML)
	if f, ok := config["fields"].([]any); ok {
		for _, v := range f {
			if str, ok := v.(string); ok {
				s.fields = append(s.fields, str)
			}
		}
	}
	if len(s.fields) == 0 {
		return nil, fmt.Errorf("drop stage requires at least one field")
	}

	return s, nil
}

// Process removes the specified fields from the record
func (s *DropStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	for _, field := range s.fields {
		delete(record.Data, field)
	}

	s.incrementOutput()
	return record, nil
}
