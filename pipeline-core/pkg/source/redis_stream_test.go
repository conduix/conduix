package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestNewRedisStreamSource_RequiresStream(t *testing.T) {
	cfg := config.SourceV2{
		Type:         "redis_stream",
		RedisAddress: "localhost:6379",
	}

	_, err := NewRedisStreamSource(cfg)
	if err == nil {
		t.Error("expected error for missing stream")
	}
}

func TestNewRedisStreamSource_DefaultAddress(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "redis_stream",
		RedisStream: "test-stream",
	}

	src, err := NewRedisStreamSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 기본 주소 확인
	if src.address != "localhost:6379" {
		t.Errorf("expected default address 'localhost:6379', got '%s'", src.address)
	}
}

func TestNewRedisStreamSource_ValidConfig(t *testing.T) {
	cfg := config.SourceV2{
		Type:                 "redis_stream",
		RedisAddress:         "localhost:6379",
		RedisStream:          "events",
		RedisGroup:           "my-group",
		RedisConsumer:        "consumer-1",
		RedisCount:           50,
		RedisBlock:           "10s",
		RedisNoAck:           false,
		RedisStartID:         "0",
		RedisClaimMinIdle:    "1m",
		RedisAutoCreateGroup: true,
	}

	src, err := NewRedisStreamSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if src.Name() != "redis_stream" {
		t.Errorf("expected name 'redis_stream', got '%s'", src.Name())
	}

	if src.SourceType() != "redis_stream" {
		t.Errorf("expected source type 'redis_stream', got '%s'", src.SourceType())
	}

	if src.stream != "events" {
		t.Errorf("expected stream 'events', got '%s'", src.stream)
	}

	if src.group != "my-group" {
		t.Errorf("expected group 'my-group', got '%s'", src.group)
	}

	if src.consumer != "consumer-1" {
		t.Errorf("expected consumer 'consumer-1', got '%s'", src.consumer)
	}

	if src.count != 50 {
		t.Errorf("expected count 50, got %d", src.count)
	}
}

func TestRedisStreamSource_AutoGenerateConsumer(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "redis_stream",
		RedisStream: "test-stream",
		RedisGroup:  "my-group",
		// Consumer 지정 안함
	}

	src, err := NewRedisStreamSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consumer가 자동 생성되어야 함
	if src.consumer == "" {
		t.Error("expected auto-generated consumer name")
	}
}

func TestRedisStreamSource_Checkpoints(t *testing.T) {
	cfg := config.SourceV2{
		Type:        "redis_stream",
		RedisStream: "test-stream",
	}

	src, err := NewRedisStreamSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 초기 상태 - 체크포인트 없음
	checkpoints := src.GetSourceCheckpoints()
	if checkpoints != nil {
		t.Error("expected nil checkpoints for new source")
	}

	// 체크포인트 설정
	err = src.SetSourceCheckpoints([]*SourceCheckpoint{
		{
			PartitionKey: "test-stream",
			OffsetValue:  "1609459200000-0",
			OffsetType:   "string",
			RecordCount:  100,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error setting checkpoints: %v", err)
	}

	if src.lastID != "1609459200000-0" {
		t.Errorf("expected lastID '1609459200000-0', got '%s'", src.lastID)
	}
	if src.recordCount != 100 {
		t.Errorf("expected recordCount 100, got %d", src.recordCount)
	}
}
