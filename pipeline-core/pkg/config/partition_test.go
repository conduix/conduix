package config

import "testing"

func TestParseListPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"partitions.[*].url", "partitions"},
		{"data.items.[*].id", "data.items"},
		{"data.items", "data.items"},
		{"[*].name", ""},
		{"partitions", "partitions"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseListPath(tt.input)
			if result != tt.expected {
				t.Errorf("parseListPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseIDField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"partitions.[*].url", "url"},
		{"data.items.[*].id", "id"},
		{"data.items.[*].nested.id", "nested.id"},
		{"data.items", ""},
		{"[*].name", "name"},
		{"[*]", ""},
		{"partitions", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseIDField(tt.input)
			if result != tt.expected {
				t.Errorf("parseIDField(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPartitionConfig_GetPartitionListPath(t *testing.T) {
	tests := []struct {
		name     string
		config   PartitionConfig
		expected string
	}{
		{
			name:     "new partition_id_path field",
			config:   PartitionConfig{PartitionIDPath: "items.[*].id"},
			expected: "items",
		},
		{
			name:     "legacy partition_list_path field",
			config:   PartitionConfig{PartitionListPath: "data.partitions"},
			expected: "data.partitions",
		},
		{
			name:     "default when empty",
			config:   PartitionConfig{},
			expected: "partitions",
		},
		{
			name:     "new field takes priority over legacy",
			config:   PartitionConfig{PartitionIDPath: "items.[*].url", PartitionListPath: "old_path"},
			expected: "items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetPartitionListPath()
			if result != tt.expected {
				t.Errorf("GetPartitionListPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPartitionConfig_GetPartitionIDField(t *testing.T) {
	tests := []struct {
		name     string
		config   PartitionConfig
		expected string
	}{
		{
			name:     "new partition_id_path with field",
			config:   PartitionConfig{PartitionIDPath: "items.[*].url"},
			expected: "url",
		},
		{
			name:     "new partition_id_path without field",
			config:   PartitionConfig{PartitionIDPath: "items"},
			expected: "",
		},
		{
			name:     "legacy partition_id_field",
			config:   PartitionConfig{PartitionIDField: "id"},
			expected: "id",
		},
		{
			name:     "empty config",
			config:   PartitionConfig{},
			expected: "",
		},
		{
			name:     "new field takes priority over legacy",
			config:   PartitionConfig{PartitionIDPath: "items.[*].url", PartitionIDField: "old_field"},
			expected: "url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetPartitionIDField()
			if result != tt.expected {
				t.Errorf("GetPartitionIDField() = %q, want %q", result, tt.expected)
			}
		})
	}
}
