package filter

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOperators(t *testing.T) {
	tests := []struct {
		op       Operator
		expected string
	}{
		{OpEqual, "eq"},
		{OpNotEqual, "neq"},
		{OpGreaterThan, "gt"},
		{OpGreaterOrEqual, "gte"},
		{OpLessThan, "lt"},
		{OpLessOrEqual, "lte"},
		{OpContains, "contains"},
		{OpStartsWith, "startswith"},
		{OpEndsWith, "endswith"},
		{OpRegex, "regex"},
		{OpExists, "exists"},
		{OpNotExists, "notexists"},
		{OpIn, "in"},
		{OpNotIn, "notin"},
		{OpIsNull, "null"},
		{OpIsNotNull, "notnull"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.op) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.op)
			}
		})
	}
}

func TestLogicalOperators(t *testing.T) {
	if string(LogicalAnd) != "and" {
		t.Errorf("expected 'and', got %s", LogicalAnd)
	}
	if string(LogicalOr) != "or" {
		t.Errorf("expected 'or', got %s", LogicalOr)
	}
}

func TestNewCondition(t *testing.T) {
	node := NewCondition("field", OpEqual, "value")

	if node.Type != "condition" {
		t.Errorf("expected type 'condition', got %s", node.Type)
	}
	if node.Condition == nil {
		t.Fatal("condition should not be nil")
	}
	if node.Condition.Field != "field" {
		t.Errorf("expected field 'field', got %s", node.Condition.Field)
	}
	if node.Condition.Op != OpEqual {
		t.Errorf("expected op 'eq', got %s", node.Condition.Op)
	}
	if node.Condition.Value != "value" {
		t.Errorf("expected value 'value', got %v", node.Condition.Value)
	}
}

func TestNewGroup(t *testing.T) {
	cond1 := NewCondition("field1", OpEqual, "value1")
	cond2 := NewCondition("field2", OpEqual, "value2")

	group := NewGroup(LogicalAnd, cond1, cond2)

	if group.Type != "group" {
		t.Errorf("expected type 'group', got %s", group.Type)
	}
	if group.Group == nil {
		t.Fatal("group should not be nil")
	}
	if group.Group.Operator != LogicalAnd {
		t.Errorf("expected operator 'and', got %s", group.Group.Operator)
	}
	if len(group.Group.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(group.Group.Conditions))
	}
}

func TestAndHelper(t *testing.T) {
	group := And(
		NewCondition("a", OpEqual, "1"),
		NewCondition("b", OpEqual, "2"),
	)

	if group.Group.Operator != LogicalAnd {
		t.Errorf("expected 'and' operator")
	}
}

func TestOrHelper(t *testing.T) {
	group := Or(
		NewCondition("a", OpEqual, "1"),
		NewCondition("b", OpEqual, "2"),
	)

	if group.Group.Operator != LogicalOr {
		t.Errorf("expected 'or' operator")
	}
}

func TestFilterValidation(t *testing.T) {
	tests := []struct {
		name    string
		filter  Filter
		wantErr bool
	}{
		{
			name: "valid expression",
			filter: Filter{
				Expression: ".status == 'active'",
			},
			wantErr: false,
		},
		{
			name: "valid root",
			filter: Filter{
				Root: NewCondition("status", OpEqual, "active"),
			},
			wantErr: false,
		},
		{
			name: "both expression and root",
			filter: Filter{
				Expression: ".status == 'active'",
				Root:       NewCondition("status", OpEqual, "active"),
			},
			wantErr: true,
		},
		{
			name:    "empty filter",
			filter:  Filter{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConditionValidation(t *testing.T) {
	tests := []struct {
		name    string
		cond    Condition
		wantErr bool
	}{
		{
			name:    "valid condition",
			cond:    Condition{Field: "status", Op: OpEqual, Value: "active"},
			wantErr: false,
		},
		{
			name:    "empty field",
			cond:    Condition{Field: "", Op: OpEqual, Value: "active"},
			wantErr: true,
		},
		{
			name:    "empty operator",
			cond:    Condition{Field: "status", Op: "", Value: "active"},
			wantErr: true,
		},
		{
			name:    "missing value for eq",
			cond:    Condition{Field: "status", Op: OpEqual},
			wantErr: true,
		},
		{
			name:    "exists without value",
			cond:    Condition{Field: "status", Op: OpExists},
			wantErr: false,
		},
		{
			name:    "not exists without value",
			cond:    Condition{Field: "status", Op: OpNotExists},
			wantErr: false,
		},
		{
			name:    "is null without value",
			cond:    Condition{Field: "status", Op: OpIsNull},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cond.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConditionGroupValidation(t *testing.T) {
	tests := []struct {
		name    string
		group   ConditionGroup
		wantErr bool
	}{
		{
			name: "valid and group",
			group: ConditionGroup{
				Operator: LogicalAnd,
				Conditions: []FilterNode{
					*NewCondition("a", OpEqual, "1"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid or group",
			group: ConditionGroup{
				Operator: LogicalOr,
				Conditions: []FilterNode{
					*NewCondition("a", OpEqual, "1"),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid operator",
			group: ConditionGroup{
				Operator: "xor",
				Conditions: []FilterNode{
					*NewCondition("a", OpEqual, "1"),
				},
			},
			wantErr: true,
		},
		{
			name: "empty conditions",
			group: ConditionGroup{
				Operator:   LogicalAnd,
				Conditions: []FilterNode{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.group.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterNodeValidation(t *testing.T) {
	tests := []struct {
		name    string
		node    FilterNode
		wantErr bool
	}{
		{
			name:    "valid condition node",
			node:    *NewCondition("field", OpEqual, "value"),
			wantErr: false,
		},
		{
			name: "condition type but nil condition",
			node: FilterNode{
				Type:      "condition",
				Condition: nil,
			},
			wantErr: true,
		},
		{
			name: "group type but nil group",
			node: FilterNode{
				Type:  "group",
				Group: nil,
			},
			wantErr: true,
		},
		{
			name: "unknown type",
			node: FilterNode{
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.node.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterConfigYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		isStruct bool
		expr     string
	}{
		{
			name:     "string expression",
			yaml:     `".status == 'active'"`,
			isStruct: false,
			expr:     ".status == 'active'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fc FilterConfig
			err := yaml.Unmarshal([]byte(tt.yaml), &fc)
			if err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}

			if fc.IsStructured() != tt.isStruct {
				t.Errorf("IsStructured() = %v, want %v", fc.IsStructured(), tt.isStruct)
			}
			if fc.GetExpression() != tt.expr {
				t.Errorf("GetExpression() = %s, want %s", fc.GetExpression(), tt.expr)
			}
		})
	}
}

func TestFilterConfigJSON(t *testing.T) {
	// Test string expression
	jsonStr := `".status == 'active'"`
	var fc1 FilterConfig
	if err := json.Unmarshal([]byte(jsonStr), &fc1); err != nil {
		t.Fatalf("unmarshal string error: %v", err)
	}
	if fc1.IsStructured() {
		t.Error("string should not be structured")
	}
	if fc1.GetExpression() != ".status == 'active'" {
		t.Errorf("expression mismatch: %s", fc1.GetExpression())
	}

	// Test structured filter
	jsonObj := `{"expression": ".status == 'active'"}`
	var fc2 FilterConfig
	if err := json.Unmarshal([]byte(jsonObj), &fc2); err != nil {
		t.Fatalf("unmarshal object error: %v", err)
	}
	if fc2.GetExpression() != ".status == 'active'" {
		t.Errorf("expression mismatch: %s", fc2.GetExpression())
	}
}

func TestFilterConfigMarshalYAML(t *testing.T) {
	// Expression only should marshal as string
	fc := FilterConfig{}
	fc.filter = &Filter{Expression: ".status == 'active'"}

	data, err := yaml.Marshal(fc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Should be a simple string
	expected := ".status == 'active'\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

func TestFilterConfigMarshalJSON(t *testing.T) {
	fc := FilterConfig{}
	fc.filter = &Filter{
		Root: NewCondition("status", OpEqual, "active"),
	}

	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Should contain the root
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded["root"] == nil {
		t.Error("root should be present in JSON")
	}
}
