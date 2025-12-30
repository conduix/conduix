package filter

import (
	"testing"
)

func TestEvaluatorWithCondition(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		data     map[string]any
		expected bool
	}{
		{
			name: "equal string",
			filter: &Filter{
				Root: NewCondition("status", OpEqual, "active"),
			},
			data:     map[string]any{"status": "active"},
			expected: true,
		},
		{
			name: "equal string - no match",
			filter: &Filter{
				Root: NewCondition("status", OpEqual, "active"),
			},
			data:     map[string]any{"status": "inactive"},
			expected: false,
		},
		{
			name: "not equal",
			filter: &Filter{
				Root: NewCondition("status", OpNotEqual, "inactive"),
			},
			data:     map[string]any{"status": "active"},
			expected: true,
		},
		{
			name: "greater than",
			filter: &Filter{
				Root: NewCondition("age", OpGreaterThan, 18),
			},
			data:     map[string]any{"age": 25},
			expected: true,
		},
		{
			name: "greater than - no match",
			filter: &Filter{
				Root: NewCondition("age", OpGreaterThan, 18),
			},
			data:     map[string]any{"age": 15},
			expected: false,
		},
		{
			name: "greater or equal",
			filter: &Filter{
				Root: NewCondition("age", OpGreaterOrEqual, 18),
			},
			data:     map[string]any{"age": 18},
			expected: true,
		},
		{
			name: "less than",
			filter: &Filter{
				Root: NewCondition("price", OpLessThan, 100),
			},
			data:     map[string]any{"price": 50},
			expected: true,
		},
		{
			name: "less or equal",
			filter: &Filter{
				Root: NewCondition("price", OpLessOrEqual, 100),
			},
			data:     map[string]any{"price": 100},
			expected: true,
		},
		{
			name: "contains",
			filter: &Filter{
				Root: NewCondition("message", OpContains, "error"),
			},
			data:     map[string]any{"message": "This is an error message"},
			expected: true,
		},
		{
			name: "starts with",
			filter: &Filter{
				Root: NewCondition("path", OpStartsWith, "/api"),
			},
			data:     map[string]any{"path": "/api/v1/users"},
			expected: true,
		},
		{
			name: "ends with",
			filter: &Filter{
				Root: NewCondition("file", OpEndsWith, ".json"),
			},
			data:     map[string]any{"file": "config.json"},
			expected: true,
		},
		{
			name: "regex",
			filter: &Filter{
				Root: NewCondition("email", OpRegex, "^[a-z]+@example\\.com$"),
			},
			data:     map[string]any{"email": "test@example.com"},
			expected: true,
		},
		{
			name: "exists",
			filter: &Filter{
				Root: NewCondition("optional", OpExists, nil),
			},
			data:     map[string]any{"optional": "value"},
			expected: true,
		},
		{
			name: "exists - no field",
			filter: &Filter{
				Root: NewCondition("optional", OpExists, nil),
			},
			data:     map[string]any{"other": "value"},
			expected: false,
		},
		{
			name: "not exists",
			filter: &Filter{
				Root: NewCondition("deleted", OpNotExists, nil),
			},
			data:     map[string]any{"other": "value"},
			expected: true,
		},
		{
			name: "is null",
			filter: &Filter{
				Root: NewCondition("value", OpIsNull, nil),
			},
			data:     map[string]any{"value": nil},
			expected: true,
		},
		{
			name: "is not null",
			filter: &Filter{
				Root: NewCondition("value", OpIsNotNull, nil),
			},
			data:     map[string]any{"value": "something"},
			expected: true,
		},
		{
			name: "in array",
			filter: &Filter{
				Root: NewCondition("status", OpIn, []any{"active", "pending"}),
			},
			data:     map[string]any{"status": "active"},
			expected: true,
		},
		{
			name: "not in array",
			filter: &Filter{
				Root: NewCondition("status", OpNotIn, []any{"deleted", "archived"}),
			},
			data:     map[string]any{"status": "active"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, err := NewEvaluator(tt.filter)
			if err != nil {
				t.Fatalf("failed to create evaluator: %v", err)
			}

			result, err := eval.Evaluate(tt.data)
			if err != nil {
				t.Fatalf("evaluation error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluatorWithGroup(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		data     map[string]any
		expected bool
	}{
		{
			name: "AND group - all match",
			filter: &Filter{
				Root: And(
					NewCondition("status", OpEqual, "active"),
					NewCondition("age", OpGreaterThan, 18),
				),
			},
			data:     map[string]any{"status": "active", "age": 25},
			expected: true,
		},
		{
			name: "AND group - one fails",
			filter: &Filter{
				Root: And(
					NewCondition("status", OpEqual, "active"),
					NewCondition("age", OpGreaterThan, 18),
				),
			},
			data:     map[string]any{"status": "active", "age": 15},
			expected: false,
		},
		{
			name: "OR group - one matches",
			filter: &Filter{
				Root: Or(
					NewCondition("status", OpEqual, "active"),
					NewCondition("status", OpEqual, "pending"),
				),
			},
			data:     map[string]any{"status": "pending"},
			expected: true,
		},
		{
			name: "OR group - none match",
			filter: &Filter{
				Root: Or(
					NewCondition("status", OpEqual, "active"),
					NewCondition("status", OpEqual, "pending"),
				),
			},
			data:     map[string]any{"status": "deleted"},
			expected: false,
		},
		{
			name: "nested groups",
			filter: &Filter{
				Root: And(
					NewCondition("type", OpEqual, "user"),
					Or(
						NewCondition("role", OpEqual, "admin"),
						NewCondition("role", OpEqual, "moderator"),
					),
				),
			},
			data:     map[string]any{"type": "user", "role": "admin"},
			expected: true,
		},
		{
			name: "nested groups - fail outer",
			filter: &Filter{
				Root: And(
					NewCondition("type", OpEqual, "user"),
					Or(
						NewCondition("role", OpEqual, "admin"),
						NewCondition("role", OpEqual, "moderator"),
					),
				),
			},
			data:     map[string]any{"type": "system", "role": "admin"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, err := NewEvaluator(tt.filter)
			if err != nil {
				t.Fatalf("failed to create evaluator: %v", err)
			}

			result, err := eval.Evaluate(tt.data)
			if err != nil {
				t.Fatalf("evaluation error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluatorWithNestedFields(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		data     map[string]any
		expected bool
	}{
		{
			name: "nested field access",
			filter: &Filter{
				Root: NewCondition("user.profile.name", OpEqual, "John"),
			},
			data: map[string]any{
				"user": map[string]any{
					"profile": map[string]any{
						"name": "John",
					},
				},
			},
			expected: true,
		},
		{
			name: "nested field - missing intermediate",
			filter: &Filter{
				Root: NewCondition("user.profile.name", OpEqual, "John"),
			},
			data: map[string]any{
				"user": map[string]any{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, err := NewEvaluator(tt.filter)
			if err != nil {
				t.Fatalf("failed to create evaluator: %v", err)
			}

			result, err := eval.Evaluate(tt.data)
			if err != nil {
				t.Fatalf("evaluation error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluatorWithExpression(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		data     map[string]any
		expected bool
	}{
		{
			name:     "simple equal",
			expr:     ".status == 'active'",
			data:     map[string]any{"status": "active"},
			expected: true,
		},
		{
			name:     "simple not equal",
			expr:     ".status != 'deleted'",
			data:     map[string]any{"status": "active"},
			expected: true,
		},
		{
			name:     "greater than",
			expr:     ".age > 18",
			data:     map[string]any{"age": 25},
			expected: true,
		},
		{
			name:     "regex match",
			expr:     ".email ~= '^[a-z]+@test.com$'",
			data:     map[string]any{"email": "user@test.com"},
			expected: true,
		},
		{
			name:     "exists",
			expr:     ".optional exists",
			data:     map[string]any{"optional": "value"},
			expected: true,
		},
		{
			name:     "AND expression",
			expr:     ".status == 'active' && .age > 18",
			data:     map[string]any{"status": "active", "age": 25},
			expected: true,
		},
		{
			name:     "OR expression",
			expr:     ".role == 'admin' || .role == 'moderator'",
			data:     map[string]any{"role": "moderator"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &Filter{Expression: tt.expr}
			eval, err := NewEvaluator(filter)
			if err != nil {
				t.Fatalf("failed to create evaluator: %v", err)
			}

			result, err := eval.Evaluate(tt.data)
			if err != nil {
				t.Fatalf("evaluation error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluatorNilFilter(t *testing.T) {
	_, err := NewEvaluator(nil)
	if err == nil {
		t.Error("expected error for nil filter")
	}
}

func TestEvaluatorEmptyFilter(t *testing.T) {
	filter := &Filter{}
	eval, err := NewEvaluator(filter)
	if err != nil {
		t.Fatalf("failed to create evaluator: %v", err)
	}

	result, err := eval.Evaluate(map[string]any{"any": "data"})
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}

	// Empty filter should pass all
	if !result {
		t.Error("empty filter should pass all data")
	}
}

func TestGetNestedValue(t *testing.T) {
	data := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"value": "deep",
			},
		},
		"simple": "top",
	}

	tests := []struct {
		name     string
		field    string
		expected any
		exists   bool
	}{
		{"simple field", "simple", "top", true},
		{"nested field", "level1.level2.value", "deep", true},
		{"missing field", "missing", nil, false},
		{"missing nested", "level1.missing", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, exists := getNestedValue(data, tt.field)
			if exists != tt.exists {
				t.Errorf("exists: expected %v, got %v", tt.exists, exists)
			}
			if exists && val != tt.expected {
				t.Errorf("value: expected %v, got %v", tt.expected, val)
			}
		})
	}
}

func TestCompareNumbers(t *testing.T) {
	tests := []struct {
		name     string
		a        any
		b        any
		expected int
	}{
		{"int equal", 10, 10, 0},
		{"int less", 5, 10, -1},
		{"int greater", 15, 10, 1},
		{"float equal", 10.5, 10.5, 0},
		{"mixed int float", 10, 10.0, 0},
		{"string numbers", "10", "5", 1},
		{"string compare", "abc", "def", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compareNumbers(tt.a, tt.b)
			if err != nil {
				t.Fatalf("comparison error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestInArray(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		arr      any
		expected bool
	}{
		{"string in any array", "a", []any{"a", "b", "c"}, true},
		{"string not in any array", "d", []any{"a", "b", "c"}, false},
		{"string in string array", "a", []string{"a", "b", "c"}, true},
		{"int in any array", 1, []any{1, 2, 3}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inArray(tt.value, tt.arr)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func BenchmarkEvaluator(b *testing.B) {
	filter := &Filter{
		Root: And(
			NewCondition("status", OpEqual, "active"),
			NewCondition("age", OpGreaterThan, 18),
			Or(
				NewCondition("role", OpEqual, "admin"),
				NewCondition("role", OpEqual, "moderator"),
			),
		),
	}

	eval, _ := NewEvaluator(filter)
	data := map[string]any{
		"status": "active",
		"age":    25,
		"role":   "admin",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eval.Evaluate(data)
	}
}
