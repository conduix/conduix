package stream

import (
	"context"
	"fmt"
	"regexp"
)

// SplitStage splits a field into multiple fields using a regex pattern with capture groups.
type SplitStage struct {
	BaseStage
	sourceField  string
	pattern      *regexp.Regexp
	targetFields []string
	keepOriginal bool
}

// NewSplitStage creates a new SplitStage from config.
// Config keys: source_field (string), pattern (string), target_fields ([]string), keep_original (bool)
func NewSplitStage(name string, config map[string]any) (*SplitStage, error) {
	sourceField := ""
	if s, ok := config["source_field"].(string); ok {
		sourceField = s
	}

	patternStr := ""
	if p, ok := config["pattern"].(string); ok {
		patternStr = p
	}

	compiled, err := regexp.Compile(patternStr)
	if err != nil {
		return nil, fmt.Errorf("split stage: invalid regex pattern %q: %w", patternStr, err)
	}

	var targetFields []string
	if f, ok := config["target_fields"].([]any); ok {
		for _, v := range f {
			if s, ok := v.(string); ok {
				targetFields = append(targetFields, s)
			}
		}
	}

	keepOriginal := false
	if k, ok := config["keep_original"].(bool); ok {
		keepOriginal = k
	}

	return &SplitStage{
		BaseStage:    BaseStage{name: name, typ: "split", config: config},
		sourceField:  sourceField,
		pattern:      compiled,
		targetFields: targetFields,
		keepOriginal: keepOriginal,
	}, nil
}

// Process applies regex on source field, assigns captured groups to target fields.
// If keep_original is false, the source field is deleted.
func (s *SplitStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	val, ok := record.Data[s.sourceField]
	if !ok {
		s.incrementError()
		return nil, fmt.Errorf("split stage: source field %q not found", s.sourceField)
	}

	strVal := fmt.Sprintf("%v", val)
	matches := s.pattern.FindStringSubmatch(strVal)

	if matches == nil {
		// No match — pass through without modification
		s.incrementOutput()
		return record, nil
	}

	// matches[0] is the full match; matches[1:] are capture groups
	groups := matches[1:]
	for i, field := range s.targetFields {
		if i < len(groups) {
			record.Data[field] = groups[i]
		}
	}

	if !s.keepOriginal {
		delete(record.Data, s.sourceField)
	}

	s.incrementOutput()
	return record, nil
}
