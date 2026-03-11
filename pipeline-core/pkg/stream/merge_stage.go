package stream

import (
	"context"
	"fmt"
	"strings"
)

// MergeStage merges multiple fields into a single field.
// Supports delimiter-based joining or template-based formatting.
type MergeStage struct {
	BaseStage
	sourceFields []string
	targetField  string
	delimiter    string
	template     string
}

// NewMergeStage creates a new MergeStage from config.
// Config keys: source_fields ([]string), target_field (string), delimiter (string), template (string)
func NewMergeStage(name string, config map[string]any) *MergeStage {
	var sourceFields []string
	if f, ok := config["source_fields"].([]any); ok {
		for _, v := range f {
			if s, ok := v.(string); ok {
				sourceFields = append(sourceFields, s)
			}
		}
	}

	targetField := ""
	if t, ok := config["target_field"].(string); ok {
		targetField = t
	}

	delimiter := " "
	if d, ok := config["delimiter"].(string); ok {
		delimiter = d
	}

	template := ""
	if t, ok := config["template"].(string); ok {
		template = t
	}

	return &MergeStage{
		BaseStage:    BaseStage{name: name, typ: "merge", config: config},
		sourceFields: sourceFields,
		targetField:  targetField,
		delimiter:    delimiter,
		template:     template,
	}
}

// Process merges source fields into target field.
// If template is set, replaces {{fieldName}} placeholders with field values.
// Otherwise, joins source field values with delimiter.
func (s *MergeStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	if s.targetField == "" {
		s.incrementError()
		return nil, fmt.Errorf("merge stage: target_field is required")
	}

	var result string
	if s.template != "" {
		// Template mode: replace {{fieldName}} with actual values
		result = s.template
		for key, val := range record.Data {
			placeholder := "{{" + key + "}}"
			result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
		}
	} else {
		// Delimiter mode: join source_fields values
		var parts []string
		for _, field := range s.sourceFields {
			if val, ok := record.Data[field]; ok {
				parts = append(parts, fmt.Sprintf("%v", val))
			}
		}
		result = strings.Join(parts, s.delimiter)
	}

	record.Data[s.targetField] = result
	s.incrementOutput()
	return record, nil
}
