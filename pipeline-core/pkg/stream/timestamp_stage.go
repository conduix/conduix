package stream

import (
	"context"
	"fmt"
	"time"
)

// TimestampStage handles timestamp processing: add, convert, format
type TimestampStage struct {
	BaseStage
	action       string // "add", "convert", "format"
	targetField  string
	sourceField  string
	timezone     *time.Location
	inputFormat  string
	outputFormat string
}

// NewTimestampStage creates a new timestamp stage
func NewTimestampStage(name string, config map[string]any) (*TimestampStage, error) {
	s := &TimestampStage{
		BaseStage: BaseStage{name: name, typ: "timestamp", config: config},
		action:    "add",
		timezone:  time.UTC,
	}

	if action, ok := config["action"].(string); ok {
		s.action = action
	}

	if tf, ok := config["target_field"].(string); ok {
		s.targetField = tf
	}
	if s.targetField == "" {
		return nil, fmt.Errorf("target_field is required for timestamp stage")
	}

	if sf, ok := config["source_field"].(string); ok {
		s.sourceField = sf
	}

	if tz, ok := config["timezone"].(string); ok && tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
		}
		s.timezone = loc
	}

	if inf, ok := config["input_format"].(string); ok {
		s.inputFormat = inf
	}

	if of, ok := config["output_format"].(string); ok {
		s.outputFormat = of
	}

	return s, nil
}

// Process applies the timestamp action to the record
func (s *TimestampStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	switch s.action {
	case "add":
		// 현재 시간을 target_field에 추가 (지정 타임존, RFC3339)
		now := time.Now().In(s.timezone)
		record.Data[s.targetField] = now.Format(time.RFC3339)

	case "convert":
		// source_field를 input_format으로 파싱 → target_field에 RFC3339으로 출력
		srcVal, ok := record.Data[s.sourceField]
		if !ok {
			s.incrementError()
			return nil, fmt.Errorf("source field %q not found", s.sourceField)
		}
		srcStr, ok := srcVal.(string)
		if !ok {
			s.incrementError()
			return nil, fmt.Errorf("source field %q is not a string", s.sourceField)
		}

		inputFmt := s.inputFormat
		if inputFmt == "" {
			inputFmt = time.RFC3339
		}

		parsed, err := time.Parse(inputFmt, srcStr)
		if err != nil {
			s.incrementError()
			return nil, fmt.Errorf("failed to parse %q with format %q: %w", srcStr, inputFmt, err)
		}

		record.Data[s.targetField] = parsed.In(s.timezone).Format(time.RFC3339)

	case "format":
		// source_field를 파싱 (RFC3339 우선) → output_format으로 출력
		srcVal, ok := record.Data[s.sourceField]
		if !ok {
			s.incrementError()
			return nil, fmt.Errorf("source field %q not found", s.sourceField)
		}
		srcStr, ok := srcVal.(string)
		if !ok {
			s.incrementError()
			return nil, fmt.Errorf("source field %q is not a string", s.sourceField)
		}

		// RFC3339 먼저 시도
		parsed, err := time.Parse(time.RFC3339, srcStr)
		if err != nil {
			// RFC3339Nano 시도
			parsed, err = time.Parse(time.RFC3339Nano, srcStr)
			if err != nil {
				s.incrementError()
				return nil, fmt.Errorf("failed to parse %q as RFC3339: %w", srcStr, err)
			}
		}

		outFmt := s.outputFormat
		if outFmt == "" {
			outFmt = "2006-01-02 15:04:05"
		}

		record.Data[s.targetField] = parsed.In(s.timezone).Format(outFmt)

	default:
		s.incrementError()
		return nil, fmt.Errorf("unknown timestamp action: %s", s.action)
	}

	s.incrementOutput()
	return record, nil
}
