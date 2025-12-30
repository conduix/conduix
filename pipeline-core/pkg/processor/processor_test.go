package processor

import (
	"context"
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func TestNoopProcessor(t *testing.T) {
	p := &NoopProcessor{name: "noop"}

	if p.Name() != "noop" {
		t.Errorf("expected name 'noop', got '%s'", p.Name())
	}

	record := source.Record{
		Data: map[string]any{
			"key": "value",
		},
	}

	result, err := p.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Data["key"] != "value" {
		t.Errorf("data should be unchanged")
	}
}

func TestTransformProcessor(t *testing.T) {
	tests := []struct {
		name      string
		transform string
		input     map[string]any
		expected  map[string]any
	}{
		{
			name:      "field reference",
			transform: "new_field = .old_field",
			input:     map[string]any{"old_field": "value"},
			expected:  map[string]any{"old_field": "value", "new_field": "value"},
		},
		{
			name:      "literal value with single quotes",
			transform: "status = 'active'",
			input:     map[string]any{},
			expected:  map[string]any{"status": "active"},
		},
		{
			name:      "literal value with double quotes",
			transform: `status = "active"`,
			input:     map[string]any{},
			expected:  map[string]any{"status": "active"},
		},
		{
			name: "multiple transforms",
			transform: `
				field1 = .source1
				field2 = 'literal'
			`,
			input:    map[string]any{"source1": "val1"},
			expected: map[string]any{"source1": "val1", "field1": "val1", "field2": "literal"},
		},
		{
			name:      "skip comments",
			transform: "# this is a comment\nfield = 'value'",
			input:     map[string]any{},
			expected:  map[string]any{"field": "value"},
		},
		{
			name:      "skip empty lines",
			transform: "\n\nfield = 'value'\n\n",
			input:     map[string]any{},
			expected:  map[string]any{"field": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &TransformProcessor{
				name:      "transform",
				transform: tt.transform,
			}

			record := source.Record{Data: tt.input}
			result, err := p.Process(context.Background(), record)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("result should not be nil")
			}

			for key, expectedVal := range tt.expected {
				if result.Data[key] != expectedVal {
					t.Errorf("field %s: expected %v, got %v", key, expectedVal, result.Data[key])
				}
			}
		})
	}
}

func TestFilterProcessor(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		input    map[string]any
		expected bool // true = pass, false = filtered out
	}{
		{
			name:     "equal match",
			filter:   ".status == 'active'",
			input:    map[string]any{"status": "active"},
			expected: true,
		},
		{
			name:     "equal no match",
			filter:   ".status == 'active'",
			input:    map[string]any{"status": "inactive"},
			expected: false,
		},
		{
			name:     "not equal match",
			filter:   ".status != 'deleted'",
			input:    map[string]any{"status": "active"},
			expected: true,
		},
		{
			name:     "not equal no match - field missing returns true",
			filter:   ".status != 'deleted'",
			input:    map[string]any{},
			expected: true,
		},
		{
			name:     "exists match",
			filter:   ".field exists",
			input:    map[string]any{"field": "value"},
			expected: true,
		},
		{
			name:     "exists no match",
			filter:   ".field exists",
			input:    map[string]any{},
			expected: false,
		},
		{
			name:     "regex match",
			filter:   ".email ~= '^test@'",
			input:    map[string]any{"email": "test@example.com"},
			expected: true,
		},
		{
			name:     "regex no match",
			filter:   ".email ~= '^test@'",
			input:    map[string]any{"email": "user@example.com"},
			expected: false,
		},
		{
			name:     "empty filter passes",
			filter:   "",
			input:    map[string]any{"any": "data"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &FilterProcessor{
				name:   "filter",
				filter: tt.filter,
			}

			record := source.Record{Data: tt.input}
			result, err := p.Process(context.Background(), record)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expected {
				if result == nil {
					t.Error("expected record to pass filter, but was filtered out")
				}
			} else {
				if result != nil {
					t.Error("expected record to be filtered out, but it passed")
				}
			}
		})
	}
}

func TestSampleProcessor(t *testing.T) {
	p := &SampleProcessor{
		name: "sample",
		rate: 0.5, // 50% sample rate
	}

	record := source.Record{Data: map[string]any{"key": "value"}}

	passed := 0
	iterations := 10000

	for i := 0; i < iterations; i++ {
		result, err := p.Process(context.Background(), record)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			passed++
		}
	}

	// With 50% rate, expect roughly 5000 (allow 10% variance)
	expectedMin := int(float64(iterations) * 0.4)
	expectedMax := int(float64(iterations) * 0.6)

	if passed < expectedMin || passed > expectedMax {
		t.Errorf("expected %d-%d passed, got %d", expectedMin, expectedMax, passed)
	}
}

func TestSelectProcessor(t *testing.T) {
	p := &SelectProcessor{
		name:   "select",
		fields: []string{"field1", "field3"},
	}

	record := source.Record{
		Data: map[string]any{
			"field1": "value1",
			"field2": "value2",
			"field3": "value3",
		},
	}

	result, err := p.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	if len(result.Data) != 2 {
		t.Errorf("expected 2 fields, got %d", len(result.Data))
	}
	if result.Data["field1"] != "value1" {
		t.Error("field1 should be present")
	}
	if result.Data["field3"] != "value3" {
		t.Error("field3 should be present")
	}
	if _, ok := result.Data["field2"]; ok {
		t.Error("field2 should not be present")
	}
}

func TestExcludeProcessor(t *testing.T) {
	p := &ExcludeProcessor{
		name:   "exclude",
		fields: []string{"password", "secret"},
	}

	record := source.Record{
		Data: map[string]any{
			"name":     "test",
			"password": "secret123",
			"secret":   "api-key",
			"public":   "data",
		},
	}

	result, err := p.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	if len(result.Data) != 2 {
		t.Errorf("expected 2 fields, got %d", len(result.Data))
	}
	if _, ok := result.Data["password"]; ok {
		t.Error("password should be excluded")
	}
	if _, ok := result.Data["secret"]; ok {
		t.Error("secret should be excluded")
	}
	if result.Data["name"] != "test" {
		t.Error("name should be present")
	}
	if result.Data["public"] != "data" {
		t.Error("public should be present")
	}
}

func TestChainProcessor(t *testing.T) {
	chain := &ChainProcessor{
		name: "chain",
		processors: []Processor{
			&TransformProcessor{name: "t1", transform: "new_field = 'added'"},
			&SelectProcessor{name: "s1", fields: []string{"new_field"}},
		},
	}

	record := source.Record{
		Data: map[string]any{
			"original": "value",
		},
	}

	result, err := chain.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	// Only new_field should remain (original was not selected)
	if len(result.Data) != 1 {
		t.Errorf("expected 1 field, got %d", len(result.Data))
	}
	if result.Data["new_field"] != "added" {
		t.Error("new_field should be 'added'")
	}
}

func TestChainProcessorWithFilter(t *testing.T) {
	chain := &ChainProcessor{
		name: "chain",
		processors: []Processor{
			&FilterProcessor{name: "f1", filter: ".status == 'active'"},
			&TransformProcessor{name: "t1", transform: "processed = 'true'"},
		},
	}

	// Active record should pass
	activeRecord := source.Record{Data: map[string]any{"status": "active"}}
	result, err := chain.Process(context.Background(), activeRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("active record should pass filter")
	}
	if result != nil && result.Data["processed"] != "true" {
		t.Error("transform should be applied")
	}

	// Inactive record should be filtered
	inactiveRecord := source.Record{Data: map[string]any{"status": "inactive"}}
	result, err = chain.Process(context.Background(), inactiveRecord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("inactive record should be filtered out")
	}
}

func BenchmarkTransformProcessor(b *testing.B) {
	p := &TransformProcessor{
		name:      "transform",
		transform: "new_field = .old_field\nstatus = 'processed'",
	}

	record := source.Record{
		Data: map[string]any{"old_field": "value"},
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = p.Process(ctx, record)
	}
}

func BenchmarkFilterProcessor(b *testing.B) {
	p := &FilterProcessor{
		name:   "filter",
		filter: ".status == 'active'",
	}

	record := source.Record{
		Data: map[string]any{"status": "active"},
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = p.Process(ctx, record)
	}
}
