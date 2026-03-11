package stream

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// CastStage converts field types (int, float, bool, string, date).
type CastStage struct {
	BaseStage
	casts       map[string]string // field -> target type
	dateFormat  string
	errorAction string // "null", "drop", "keep"
}

// NewCastStage creates a new CastStage from config.
// Config keys: casts (map[string]string), date_format (string), error_action (string)
func NewCastStage(name string, config map[string]any) *CastStage {
	casts := make(map[string]string)
	if c, ok := config["casts"].(map[string]any); ok {
		for k, v := range c {
			if s, ok := v.(string); ok {
				casts[k] = s
			}
		}
	}

	dateFormat := time.RFC3339
	if d, ok := config["date_format"].(string); ok && d != "" {
		dateFormat = d
	}

	errorAction := "null"
	if e, ok := config["error_action"].(string); ok {
		errorAction = e
	}

	return &CastStage{
		BaseStage:   BaseStage{name: name, typ: "cast", config: config},
		casts:       casts,
		dateFormat:  dateFormat,
		errorAction: errorAction,
	}
}

// Process converts each field to the specified type.
// On conversion error: "null" sets nil, "drop" drops the record, "keep" keeps original value.
func (s *CastStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	for field, targetType := range s.casts {
		val, ok := record.Data[field]
		if !ok {
			continue
		}

		converted, err := s.castValue(val, targetType)
		if err != nil {
			switch s.errorAction {
			case "drop":
				s.incrementError()
				return nil, nil
			case "keep":
				// Keep original value
				continue
			default: // "null"
				record.Data[field] = nil
				continue
			}
		}
		record.Data[field] = converted
	}

	s.incrementOutput()
	return record, nil
}

// castValue converts a value to the target type
func (s *CastStage) castValue(val any, targetType string) (any, error) {
	switch targetType {
	case "int":
		return s.castToInt(val)
	case "float":
		return s.castToFloat(val)
	case "bool":
		return s.castToBool(val)
	case "string":
		return fmt.Sprintf("%v", val), nil
	case "date":
		return s.castToDate(val)
	default:
		return nil, fmt.Errorf("unsupported cast type: %s", targetType)
	}
}

// castToInt converts value to int64
func (s *CastStage) castToInt(val any) (int64, error) {
	switch v := val.(type) {
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cannot cast %T to int", val)
	}
}

// castToFloat converts value to float64
func (s *CastStage) castToFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	case bool:
		if v {
			return 1.0, nil
		}
		return 0.0, nil
	default:
		return 0, fmt.Errorf("cannot cast %T to float", val)
	}
}

// castToBool converts value to bool
func (s *CastStage) castToBool(val any) (bool, error) {
	switch v := val.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(v)
	case float64:
		return v != 0, nil
	case int:
		return v != 0, nil
	case int64:
		return v != 0, nil
	default:
		return false, fmt.Errorf("cannot cast %T to bool", val)
	}
}

// castToDate parses a string value into time.Time using the configured date_format
func (s *CastStage) castToDate(val any) (time.Time, error) {
	str, ok := val.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("cannot cast %T to date, expected string", val)
	}
	return time.Parse(s.dateFormat, str)
}
