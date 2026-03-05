// Package source Redis Stream 소스 구현
package source

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/secrets"
)

// RedisStreamSource Redis Stream 소스
type RedisStreamSource struct {
	name string

	// 연결 설정
	address  string
	password string
	db       int
	username string

	// TLS 설정
	tlsEnabled    bool
	tlsSkipVerify bool

	// Stream 설정
	stream          string
	group           string
	consumer        string
	count           int64
	block           time.Duration
	noAck           bool
	startID         string // > (새 메시지만), 0 (처음부터), $ (현재 이후)
	claimMinIdle    time.Duration
	autoCreateGroup bool

	// 재연결 설정
	reconnectWait time.Duration
	maxReconnect  int

	// 클라이언트
	client *redis.Client

	// 상태
	lastID      string
	recordCount int64
	mu          sync.RWMutex
	closed      bool
}

// NewRedisStreamSource Redis Stream 소스 생성
func NewRedisStreamSource(cfg config.SourceV2) (*RedisStreamSource, error) {
	s := &RedisStreamSource{
		name:            "redis_stream",
		db:              0,
		count:           100,
		block:           5 * time.Second,
		startID:         ">", // 새 메시지만 (Consumer Group 사용 시)
		claimMinIdle:    30 * time.Second,
		autoCreateGroup: true,
		reconnectWait:   5 * time.Second,
		maxReconnect:    0, // 무한
	}

	// Address
	if cfg.RedisAddress != "" {
		s.address = secrets.ExpandEnvVars(cfg.RedisAddress)
	}
	if s.address == "" {
		s.address = os.Getenv("REDIS_ADDR")
	}
	if s.address == "" {
		s.address = "localhost:6379"
	}

	// Password
	if cfg.RedisPassword != "" {
		s.password = secrets.ExpandEnvVars(cfg.RedisPassword)
	}
	if s.password == "" {
		s.password = os.Getenv("REDIS_PASSWORD")
	}

	// Username (Redis 6+)
	s.username = cfg.RedisUsername

	// Database
	s.db = cfg.RedisDB

	// Stream 설정
	s.stream = cfg.RedisStream
	if s.stream == "" {
		return nil, fmt.Errorf("redis stream name is required")
	}

	// Consumer Group 설정
	s.group = cfg.RedisGroup
	s.consumer = cfg.RedisConsumer
	if s.group != "" && s.consumer == "" {
		// Consumer Group 있으면 consumer 이름 필요
		hostname, _ := os.Hostname()
		s.consumer = fmt.Sprintf("consumer-%s-%d", hostname, time.Now().UnixNano())
	}

	// Count
	if cfg.RedisCount > 0 {
		s.count = int64(cfg.RedisCount)
	}

	// Block
	if cfg.RedisBlock != "" {
		if d, err := time.ParseDuration(cfg.RedisBlock); err == nil {
			s.block = d
		}
	}

	// NoAck (auto-ack)
	s.noAck = cfg.RedisNoAck

	// Start ID
	if cfg.RedisStartID != "" {
		s.startID = cfg.RedisStartID
	}

	// Claim Min Idle (pending 메시지 claim 시간)
	if cfg.RedisClaimMinIdle != "" {
		if d, err := time.ParseDuration(cfg.RedisClaimMinIdle); err == nil {
			s.claimMinIdle = d
		}
	}

	// Auto Create Group
	s.autoCreateGroup = cfg.RedisAutoCreateGroup

	// 재연결 설정
	if cfg.RedisReconnectWait > 0 {
		s.reconnectWait = time.Duration(cfg.RedisReconnectWait) * time.Millisecond
	}
	if cfg.RedisMaxReconnect > 0 {
		s.maxReconnect = cfg.RedisMaxReconnect
	}

	// TLS 설정
	s.tlsEnabled = cfg.RedisTLSEnabled
	s.tlsSkipVerify = cfg.RedisTLSSkipVerify

	return s, nil
}

func (s *RedisStreamSource) Name() string {
	return s.name
}

func (s *RedisStreamSource) Open(ctx context.Context) error {
	opts := &redis.Options{
		Addr:     s.address,
		Password: s.password,
		DB:       s.db,
		Username: s.username,
	}

	// TLS 설정
	if s.tlsEnabled {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: s.tlsSkipVerify,
		}
	}

	s.client = redis.NewClient(opts)

	// 연결 확인
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Consumer Group 생성 (필요한 경우)
	if s.group != "" && s.autoCreateGroup {
		// XGROUP CREATE stream group $ MKSTREAM
		// 이미 존재하면 에러 무시
		startID := "$" // 현재 이후부터
		if s.startID == "0" || s.startID == "0-0" {
			startID = "0" // 처음부터
		}

		err := s.client.XGroupCreateMkStream(ctx, s.stream, s.group, startID).Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			log.Printf("[redis_stream] Warning: failed to create consumer group: %v", err)
		}
	}

	log.Printf("[redis_stream] Connected to Redis (stream=%s, group=%s, consumer=%s)",
		s.stream, s.group, s.consumer)
	return nil
}

func (s *RedisStreamSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	recordCh := make(chan Record, 1000)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		reconnectCount := 0

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 메시지 읽기
			messages, err := s.readMessages(ctx)
			if err != nil {
				s.mu.RLock()
				closed := s.closed
				s.mu.RUnlock()

				if closed {
					return
				}

				if ctx.Err() != nil {
					return
				}

				reconnectCount++
				if s.maxReconnect > 0 && reconnectCount > s.maxReconnect {
					errCh <- fmt.Errorf("max reconnect attempts exceeded: %w", err)
					return
				}

				log.Printf("[redis_stream] Read error, retrying: %v", err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(s.reconnectWait):
					continue
				}
			}

			reconnectCount = 0

			// 메시지 처리
			for _, msg := range messages {
				s.mu.Lock()
				s.lastID = msg.ID
				s.recordCount++
				s.mu.Unlock()

				record := s.messageToRecord(msg)

				select {
				case recordCh <- record:
				case <-ctx.Done():
					return
				}

				// Ack (Consumer Group 사용 시, noAck가 아닌 경우)
				if s.group != "" && !s.noAck {
					if err := s.client.XAck(ctx, s.stream, s.group, msg.ID).Err(); err != nil {
						log.Printf("[redis_stream] Failed to ack message %s: %v", msg.ID, err)
					}
				}
			}
		}
	}()

	return recordCh, errCh
}

func (s *RedisStreamSource) readMessages(ctx context.Context) ([]redis.XMessage, error) {
	if s.group != "" {
		// Consumer Group 사용 - XREADGROUP
		streams, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.group,
			Consumer: s.consumer,
			Streams:  []string{s.stream, s.startID},
			Count:    s.count,
			Block:    s.block,
			NoAck:    s.noAck,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				return nil, nil
			}
			return nil, err
		}

		if len(streams) > 0 && len(streams[0].Messages) > 0 {
			return streams[0].Messages, nil
		}

		// Pending 메시지 claim 시도
		pending, err := s.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: s.stream,
			Group:  s.group,
			Start:  "-",
			End:    "+",
			Count:  s.count,
		}).Result()

		if err != nil || len(pending) == 0 {
			return nil, nil
		}

		// 오래된 pending 메시지 claim
		var claimIDs []string
		for _, p := range pending {
			if p.Idle >= s.claimMinIdle {
				claimIDs = append(claimIDs, p.ID)
			}
		}

		if len(claimIDs) > 0 {
			claimed, err := s.client.XClaim(ctx, &redis.XClaimArgs{
				Stream:   s.stream,
				Group:    s.group,
				Consumer: s.consumer,
				MinIdle:  s.claimMinIdle,
				Messages: claimIDs,
			}).Result()

			if err != nil {
				return nil, err
			}
			return claimed, nil
		}

		return nil, nil
	}

	// Consumer Group 없이 - XREAD
	s.mu.RLock()
	startID := s.lastID
	s.mu.RUnlock()

	if startID == "" {
		startID = s.startID
		if startID == ">" {
			startID = "$" // XREAD에서는 $가 최신
		}
	}

	streams, err := s.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{s.stream, startID},
		Count:   s.count,
		Block:   s.block,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	if len(streams) > 0 {
		return streams[0].Messages, nil
	}

	return nil, nil
}

func (s *RedisStreamSource) messageToRecord(msg redis.XMessage) Record {
	data := make(map[string]any)

	// Stream ID
	data["_id"] = msg.ID

	// ID 분해 (timestamp-sequence)
	parts := strings.SplitN(msg.ID, "-", 2)
	if len(parts) == 2 {
		data["_timestamp_ms"] = parts[0]
		data["_sequence"] = parts[1]
	}

	// Values
	for k, v := range msg.Values {
		data[k] = v
	}

	// 메타데이터
	data["_stream"] = s.stream
	if s.group != "" {
		data["_group"] = s.group
		data["_consumer"] = s.consumer
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "redis_stream",
			Origin:    s.stream,
			Offset:    msg.ID,
			Timestamp: time.Now().UnixNano(),
		},
	}
}

func (s *RedisStreamSource) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			log.Printf("[redis_stream] Error closing client: %v", err)
		}
	}

	log.Printf("[redis_stream] Source closed")
	return nil
}

// CheckpointableSource 인터페이스 구현

func (s *RedisStreamSource) SourceType() string {
	return "redis_stream"
}

func (s *RedisStreamSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastID == "" {
		return nil
	}

	return []*SourceCheckpoint{
		{
			PartitionKey: s.stream,
			OffsetValue:  s.lastID,
			OffsetType:   "string",
			RecordCount:  s.recordCount,
			UpdatedAt:    time.Now(),
		},
	}
}

func (s *RedisStreamSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cp := range checkpoints {
		if cp.PartitionKey == s.stream {
			s.lastID = cp.OffsetValue
			s.recordCount = cp.RecordCount
			break
		}
	}

	return nil
}
