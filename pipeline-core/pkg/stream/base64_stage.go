package stream

import (
	"context"
	"encoding/base64"
	"fmt"
)

// Base64Stage encodes or decodes specified fields using Base64.
// Config:
//   - fields: []string — target field names
//   - action: string — "encode" (default) or "decode"
type Base64Stage struct {
	BaseStage
	fields []string
	encode bool // true=encode, false=decode
}

// NewBase64Stage creates a Base64Stage from config.
func NewBase64Stage(name string, config map[string]any) (*Base64Stage, error) {
	s := &Base64Stage{
		BaseStage: BaseStage{name: name, typ: "base64", config: config},
		encode:    true,
	}

	if f, ok := config["fields"].([]any); ok {
		for _, v := range f {
			if str, ok := v.(string); ok {
				s.fields = append(s.fields, str)
			}
		}
	}
	if len(s.fields) == 0 {
		return nil, fmt.Errorf("base64 stage %q: 'fields' is required", name)
	}

	if action, ok := config["action"].(string); ok && action == "decode" {
		s.encode = false
	}

	return s, nil
}

// Process encodes or decodes the specified fields.
func (s *Base64Stage) Process(_ context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	for _, field := range s.fields {
		val, ok := record.Data[field]
		if !ok {
			continue
		}
		str, ok := val.(string)
		if !ok {
			continue
		}

		if s.encode {
			record.Data[field] = base64.StdEncoding.EncodeToString([]byte(str))
		} else {
			decoded, err := base64.StdEncoding.DecodeString(str)
			if err != nil {
				s.incrementError()
				continue
			}
			record.Data[field] = string(decoded)
		}
	}

	s.incrementOutput()
	return record, nil
}
