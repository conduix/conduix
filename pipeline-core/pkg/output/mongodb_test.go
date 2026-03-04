// Package output MongoDB Output 테스트
package output

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func TestNewMongoDBOutput(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.OutputConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":        "mongodb://localhost:27017",
					"database":   "test_db",
					"collection": "test_collection",
				},
			},
			wantErr: false,
		},
		{
			name: "missing uri",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"database":   "test_db",
					"collection": "test_collection",
				},
			},
			wantErr: true,
		},
		{
			name: "missing database",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":        "mongodb://localhost:27017",
					"collection": "test_collection",
				},
			},
			wantErr: true,
		},
		{
			name: "missing collection",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":      "mongodb://localhost:27017",
					"database": "test_db",
				},
			},
			wantErr: true,
		},
		{
			name: "with template collection",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":        "mongodb://localhost:27017",
					"database":   "test_db",
					"collection": "events_{{ .year }}_{{ .month }}",
				},
			},
			wantErr: false,
		},
		{
			name: "with upsert config",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":        "mongodb://localhost:27017",
					"database":   "test_db",
					"collection": "test_collection",
					"upsert": map[string]interface{}{
						"enabled":    true,
						"key_fields": []interface{}{"_id", "event_id"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "with write concern",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":           "mongodb://localhost:27017",
					"database":      "test_db",
					"collection":    "test_collection",
					"write_concern": "majority",
					"ordered":       false,
				},
			},
			wantErr: false,
		},
		{
			name: "with batch config",
			cfg: config.OutputConfig{
				Type: "mongodb",
				Config: map[string]interface{}{
					"uri":            "mongodb://localhost:27017",
					"database":       "test_db",
					"collection":     "test_collection",
					"bulk_size":      500,
					"flush_interval": "10s",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := NewMongoDBOutput(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMongoDBOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && out == nil {
				t.Error("NewMongoDBOutput() returned nil output")
			}
		})
	}
}

func TestMongoDBOutput_SupportsBatch(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "mongodb",
		Config: map[string]interface{}{
			"uri":        "mongodb://localhost:27017",
			"database":   "test_db",
			"collection": "test_collection",
		},
	}

	out, err := NewMongoDBOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	if !out.SupportsBatch() {
		t.Error("MongoDB output should support batch")
	}

	batchCfg := out.BatchConfig()
	if !batchCfg.Enabled {
		t.Error("Batch should be enabled")
	}
	if batchCfg.Size != 1000 { // default
		t.Errorf("Expected default batch size 1000, got %d", batchCfg.Size)
	}
}

func TestMongoDBOutput_GetCollectionName(t *testing.T) {
	// 정적 Collection
	cfg1 := config.OutputConfig{
		Type: "mongodb",
		Config: map[string]interface{}{
			"uri":        "mongodb://localhost:27017",
			"database":   "test_db",
			"collection": "static_collection",
		},
	}

	out1, _ := NewMongoDBOutput(cfg1)
	record := source.Record{Data: map[string]interface{}{"name": "test"}}

	collName := out1.getCollectionName(record)
	if collName != "static_collection" {
		t.Errorf("Expected 'static_collection', got '%s'", collName)
	}

	// 템플릿 Collection
	cfg2 := config.OutputConfig{
		Type: "mongodb",
		Config: map[string]interface{}{
			"uri":        "mongodb://localhost:27017",
			"database":   "test_db",
			"collection": "events_{{ .year }}_{{ .month }}",
		},
	}

	out2, _ := NewMongoDBOutput(cfg2)
	collName2 := out2.getCollectionName(record)
	// 형식: events_YYYY_MM
	if len(collName2) < 12 || collName2[:7] != "events_" {
		t.Errorf("Expected collection to start with 'events_', got '%s'", collName2)
	}
}

func TestMongoDBOutput_UpsertConfig(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "mongodb",
		Config: map[string]interface{}{
			"uri":        "mongodb://localhost:27017",
			"database":   "test_db",
			"collection": "test_collection",
			"upsert": map[string]interface{}{
				"enabled":    true,
				"key_fields": []interface{}{"user_id", "event_id"},
			},
		},
	}

	out, err := NewMongoDBOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	if !out.upsertEnabled {
		t.Error("Upsert should be enabled")
	}

	if len(out.upsertKeys) != 2 {
		t.Errorf("Expected 2 upsert keys, got %d", len(out.upsertKeys))
	}

	if out.upsertKeys[0] != "user_id" || out.upsertKeys[1] != "event_id" {
		t.Errorf("Unexpected upsert keys: %v", out.upsertKeys)
	}
}

func TestMongoDBOutput_UpsertDefaultKey(t *testing.T) {
	cfg := config.OutputConfig{
		Type: "mongodb",
		Config: map[string]interface{}{
			"uri":        "mongodb://localhost:27017",
			"database":   "test_db",
			"collection": "test_collection",
			"upsert": map[string]interface{}{
				"enabled": true,
				// key_fields 미지정 -> 기본값 _id 사용
			},
		},
	}

	out, err := NewMongoDBOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}

	if !out.upsertEnabled {
		t.Error("Upsert should be enabled")
	}

	if len(out.upsertKeys) != 1 || out.upsertKeys[0] != "_id" {
		t.Errorf("Expected default upsert key ['_id'], got %v", out.upsertKeys)
	}
}

func TestMaskURI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "mongodb://user:password@localhost:27017",
			expected: "mongodb://user:***@localhost:27017",
		},
		{
			input:    "mongodb://localhost:27017",
			expected: "mongodb://localhost:27017",
		},
		{
			input:    "mongodb+srv://admin:secretpass@cluster.mongodb.net",
			expected: "mongodb+srv://admin:***@cluster.mongodb.net",
		},
	}

	for _, tt := range tests {
		result := maskURI(tt.input)
		if result != tt.expected {
			t.Errorf("maskURI(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestContainsTemplate(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"static_collection", false},
		{"events_{{ .year }}", true},
		{"{{ .type }}_events", true},
		{"events", false},
		{"{{}}", false}, // 너무 짧음
	}

	for _, tt := range tests {
		result := containsTemplate(tt.input)
		if result != tt.expected {
			t.Errorf("containsTemplate(%s) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestGetWriteConcern(t *testing.T) {
	// WriteConcern 함수가 nil을 반환하지 않는지 확인
	testCases := []string{"majority", "1", "w1", "journaled", "unknown"}

	for _, tc := range testCases {
		wc := getWriteConcern(tc)
		if wc == nil {
			t.Errorf("getWriteConcern(%s) returned nil", tc)
		}
	}
}
