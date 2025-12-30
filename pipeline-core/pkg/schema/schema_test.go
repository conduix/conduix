package schema

import (
	"testing"
)

func TestDataSchemaValidate(t *testing.T) {
	tests := []struct {
		name      string
		schema    DataSchema
		data      map[string]any
		wantError bool
	}{
		{
			name: "valid data - all fields present",
			schema: DataSchema{
				Name: "test",
				Fields: []FieldSchema{
					{Name: "name", Type: FieldTypeString, Required: true},
					{Name: "age", Type: FieldTypeInteger, Required: true},
				},
			},
			data: map[string]any{
				"name": "John",
				"age":  float64(30), // JSON unmarshals numbers as float64
			},
			wantError: false,
		},
		{
			name: "missing required field",
			schema: DataSchema{
				Name: "test",
				Fields: []FieldSchema{
					{Name: "name", Type: FieldTypeString, Required: true},
					{Name: "email", Type: FieldTypeString, Required: true},
				},
			},
			data: map[string]any{
				"name": "John",
			},
			wantError: true,
		},
		{
			name: "wrong type",
			schema: DataSchema{
				Name: "test",
				Fields: []FieldSchema{
					{Name: "age", Type: FieldTypeInteger, Required: true},
				},
			},
			data: map[string]any{
				"age": "thirty",
			},
			wantError: true,
		},
		{
			name: "optional field missing is ok",
			schema: DataSchema{
				Name: "test",
				Fields: []FieldSchema{
					{Name: "name", Type: FieldTypeString, Required: true},
					{Name: "nickname", Type: FieldTypeString, Required: false},
				},
			},
			data: map[string]any{
				"name": "John",
			},
			wantError: false,
		},
		{
			name: "strict mode - undefined field",
			schema: DataSchema{
				Name:   "test",
				Strict: true,
				Fields: []FieldSchema{
					{Name: "name", Type: FieldTypeString, Required: true},
				},
			},
			data: map[string]any{
				"name":  "John",
				"extra": "field",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFieldSchemaStringValidation(t *testing.T) {
	minLen := 3
	maxLen := 10
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{
				Name:      "username",
				Type:      FieldTypeString,
				Required:  true,
				MinLength: &minLen,
				MaxLength: &maxLen,
				Pattern:   "^[a-z]+$",
			},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name:      "valid string",
			data:      map[string]any{"username": "john"},
			wantError: false,
		},
		{
			name:      "too short",
			data:      map[string]any{"username": "ab"},
			wantError: true,
		},
		{
			name:      "too long",
			data:      map[string]any{"username": "verylongusername"},
			wantError: true,
		},
		{
			name:      "pattern mismatch",
			data:      map[string]any{"username": "John123"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFieldSchemaNumberValidation(t *testing.T) {
	min := float64(0)
	max := float64(100)
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{
				Name:     "score",
				Type:     FieldTypeNumber,
				Required: true,
				Min:      &min,
				Max:      &max,
			},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name:      "valid number",
			data:      map[string]any{"score": float64(50)},
			wantError: false,
		},
		{
			name:      "below minimum",
			data:      map[string]any{"score": float64(-5)},
			wantError: true,
		},
		{
			name:      "above maximum",
			data:      map[string]any{"score": float64(150)},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFieldSchemaEnumValidation(t *testing.T) {
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{
				Name:     "status",
				Type:     FieldTypeString,
				Required: true,
				Enum:     []any{"pending", "active", "completed"},
			},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name:      "valid enum value",
			data:      map[string]any{"status": "active"},
			wantError: false,
		},
		{
			name:      "invalid enum value",
			data:      map[string]any{"status": "invalid"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFieldSchemaArrayValidation(t *testing.T) {
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{
				Name:     "tags",
				Type:     FieldTypeArray,
				Required: true,
				Items: &FieldSchema{
					Type: FieldTypeString,
				},
			},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name:      "valid array",
			data:      map[string]any{"tags": []any{"go", "rust", "python"}},
			wantError: false,
		},
		{
			name:      "invalid array item type",
			data:      map[string]any{"tags": []any{"go", 123, "python"}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFieldSchemaObjectValidation(t *testing.T) {
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{
				Name:     "address",
				Type:     FieldTypeObject,
				Required: true,
				Properties: []FieldSchema{
					{Name: "city", Type: FieldTypeString, Required: true},
					{Name: "zip", Type: FieldTypeString, Required: true},
				},
			},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name: "valid object",
			data: map[string]any{
				"address": map[string]any{
					"city": "Seoul",
					"zip":  "12345",
				},
			},
			wantError: false,
		},
		{
			name: "missing required property",
			data: map[string]any{
				"address": map[string]any{
					"city": "Seoul",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestNestedFieldValidation(t *testing.T) {
	schema := DataSchema{
		Name: "test",
		Fields: []FieldSchema{
			{Name: "user.name", Type: FieldTypeString, Required: true},
			{Name: "user.age", Type: FieldTypeInteger, Required: true},
		},
	}

	tests := []struct {
		name      string
		data      map[string]any
		wantError bool
	}{
		{
			name: "valid nested fields",
			data: map[string]any{
				"user": map[string]any{
					"name": "John",
					"age":  float64(30),
				},
			},
			wantError: false,
		},
		{
			name: "missing nested field",
			data: map[string]any{
				"user": map[string]any{
					"name": "John",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestNewDataSchemaFromConfig(t *testing.T) {
	config := map[string]any{
		"name":        "user_schema",
		"description": "User data schema",
		"strict":      true,
		"fields": []any{
			map[string]any{
				"name":       "email",
				"type":       "string",
				"required":   true,
				"pattern":    "^[a-z]+@[a-z]+\\.[a-z]+$",
				"min_length": float64(5),
			},
			map[string]any{
				"name":     "age",
				"type":     "integer",
				"required": false,
				"min":      float64(0),
				"max":      float64(150),
			},
		},
	}

	schema, err := NewDataSchemaFromConfig(config)
	if err != nil {
		t.Fatalf("NewDataSchemaFromConfig() error = %v", err)
	}

	if schema.Name != "user_schema" {
		t.Errorf("Name = %v, want user_schema", schema.Name)
	}
	if !schema.Strict {
		t.Error("Strict should be true")
	}
	if len(schema.Fields) != 2 {
		t.Errorf("Fields count = %v, want 2", len(schema.Fields))
	}

	// Test validation with the created schema
	validData := map[string]any{
		"email": "test@example.com",
		"age":   float64(25),
	}
	if err := schema.Validate(validData); err != nil {
		t.Errorf("Validate() with valid data should not error: %v", err)
	}

	invalidData := map[string]any{
		"email": "invalid-email",
		"age":   float64(25),
	}
	if err := schema.Validate(invalidData); err == nil {
		t.Error("Validate() with invalid email should error")
	}
}
