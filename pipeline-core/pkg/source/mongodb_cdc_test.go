package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewMongoDBCDCSource_RequiresURI(t *testing.T) {
	cfg := config.SourceV2{
		Type:            "mongodb_cdc",
		MongoDBDatabase: "test",
	}

	_, err := NewMongoDBCDCSource(cfg)
	if err == nil {
		t.Error("expected error for missing URI")
	}
}

func TestNewMongoDBCDCSource_RequiresDatabase(t *testing.T) {
	cfg := config.SourceV2{
		Type:       "mongodb_cdc",
		MongoDBURI: "mongodb://localhost:27017",
	}

	_, err := NewMongoDBCDCSource(cfg)
	if err == nil {
		t.Error("expected error for missing database")
	}
}

func TestNewMongoDBCDCSource_ValidConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:                 "mongodb_cdc",
		MongoDBURI:           "mongodb://localhost:27017",
		MongoDBDatabase:      "test",
		MongoDBCollection:    "events",
		MongoDBFullDocument:  "updateLookup",
		MongoDBBatchSize:     500,
		MongoDBMaxAwaitTime:  "30s",
		MongoDBReconnectWait: 5000,
		MongoDBMaxReconnect:  10,
	}

	src, err := NewMongoDBCDCSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if src.Name() != "mongodb_cdc" {
		t.Errorf("expected name 'mongodb_cdc', got '%s'", src.Name())
	}

	if src.SourceType() != "mongodb_cdc" {
		t.Errorf("expected source type 'mongodb_cdc', got '%s'", src.SourceType())
	}

	// 체크포인트 테스트 (초기 상태)
	checkpoints := src.GetSourceCheckpoints()
	if checkpoints != nil {
		t.Error("expected nil checkpoints for new source")
	}
}

func TestMongoDBCDCSource_FormatObjectID(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{"abc123", "abc123"},
		{123, "123"},
	}

	for _, tt := range tests {
		result := formatObjectID(tt.input)
		if result != tt.expected {
			t.Errorf("formatObjectID(%v) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}
