// Package dedup 이벤트 중복 제거 및 Upsert 로직
package dedup

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// EventType 이벤트 타입
type EventType string

const (
	EventCreate EventType = "CREATE"
	EventUpdate EventType = "UPDATE"
	EventDelete EventType = "DELETE"
)

// DedupService 중복 제거 서비스 인터페이스
type DedupService interface {
	// IsDuplicate 이벤트 ID가 이미 처리되었는지 확인
	IsDuplicate(ctx context.Context, eventID string) (bool, error)

	// MarkProcessed 이벤트 ID를 처리됨으로 표시
	MarkProcessed(ctx context.Context, eventID string) error

	// EntityExists 엔티티가 존재하는지 확인 (Upsert용)
	EntityExists(ctx context.Context, entityID string) (bool, error)

	// SetEntityExists 엔티티 존재 표시
	SetEntityExists(ctx context.Context, entityID string) error

	// DeleteEntity 엔티티 삭제 표시
	DeleteEntity(ctx context.Context, entityID string) error

	// Close 서비스 종료
	Close() error
}

// MemoryDedupService 메모리 기반 중복 제거 (개발/테스트용)
type MemoryDedupService struct {
	processedIDs map[string]time.Time
	entities     map[string]bool
	ttl          time.Duration
	mu           sync.RWMutex
	cleanupStop  chan struct{}
}

// NewMemoryDedupService 메모리 기반 서비스 생성
func NewMemoryDedupService(ttl time.Duration) *MemoryDedupService {
	s := &MemoryDedupService{
		processedIDs: make(map[string]time.Time),
		entities:     make(map[string]bool),
		ttl:          ttl,
		cleanupStop:  make(chan struct{}),
	}

	// 백그라운드 정리
	go s.cleanupLoop()

	return s
}

func (s *MemoryDedupService) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupStop:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *MemoryDedupService) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, ts := range s.processedIDs {
		if now.Sub(ts) > s.ttl {
			delete(s.processedIDs, id)
		}
	}
}

func (s *MemoryDedupService) IsDuplicate(ctx context.Context, eventID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.processedIDs[eventID]
	return exists, nil
}

func (s *MemoryDedupService) MarkProcessed(ctx context.Context, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.processedIDs[eventID] = time.Now()
	return nil
}

func (s *MemoryDedupService) EntityExists(ctx context.Context, entityID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.entities[entityID], nil
}

func (s *MemoryDedupService) SetEntityExists(ctx context.Context, entityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entities[entityID] = true
	return nil
}

func (s *MemoryDedupService) DeleteEntity(ctx context.Context, entityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entities, entityID)
	return nil
}

func (s *MemoryDedupService) Close() error {
	close(s.cleanupStop)
	return nil
}

// RedisDedupService Redis 기반 중복 제거 (프로덕션용)
type RedisDedupService struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisDedupService Redis 기반 서비스 생성.
// 연결 실패 시 에러를 반환하여 조용히 중복을 흘려보내지 않도록 한다.
func NewRedisDedupService(addr, prefix string, ttl time.Duration) (*RedisDedupService, error) {
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis dedup: failed to connect to %s: %w", addr, err)
	}

	return &RedisDedupService{
		client: client,
		prefix: prefix,
		ttl:    ttl,
	}, nil
}

func (s *RedisDedupService) eventKey(eventID string) string {
	return fmt.Sprintf("%s:event:%s", s.prefix, eventID)
}

func (s *RedisDedupService) entityKey(entityID string) string {
	return fmt.Sprintf("%s:entity:%s", s.prefix, entityID)
}

func (s *RedisDedupService) IsDuplicate(ctx context.Context, eventID string) (bool, error) {
	n, err := s.client.Exists(ctx, s.eventKey(eventID)).Result()
	if err != nil {
		return false, fmt.Errorf("redis dedup IsDuplicate: %w", err)
	}
	return n > 0, nil
}

func (s *RedisDedupService) MarkProcessed(ctx context.Context, eventID string) error {
	if err := s.client.Set(ctx, s.eventKey(eventID), "1", s.ttl).Err(); err != nil {
		return fmt.Errorf("redis dedup MarkProcessed: %w", err)
	}
	return nil
}

func (s *RedisDedupService) EntityExists(ctx context.Context, entityID string) (bool, error) {
	n, err := s.client.Exists(ctx, s.entityKey(entityID)).Result()
	if err != nil {
		return false, fmt.Errorf("redis dedup EntityExists: %w", err)
	}
	return n > 0, nil
}

// SetEntityExists 엔티티 존재를 영구 저장한다(Upsert 판별용이므로 TTL 없음).
func (s *RedisDedupService) SetEntityExists(ctx context.Context, entityID string) error {
	if err := s.client.Set(ctx, s.entityKey(entityID), "1", 0).Err(); err != nil {
		return fmt.Errorf("redis dedup SetEntityExists: %w", err)
	}
	return nil
}

func (s *RedisDedupService) DeleteEntity(ctx context.Context, entityID string) error {
	if err := s.client.Del(ctx, s.entityKey(entityID)).Err(); err != nil {
		return fmt.Errorf("redis dedup DeleteEntity: %w", err)
	}
	return nil
}

func (s *RedisDedupService) Close() error {
	return s.client.Close()
}

// NewDedupService 설정에 따라 적절한 서비스 생성
func NewDedupService(storage, redisAddr string, ttl time.Duration) (DedupService, error) {
	switch storage {
	case "memory", "":
		return NewMemoryDedupService(ttl), nil
	case "redis":
		return NewRedisDedupService(redisAddr, "dedup", ttl)
	default:
		return nil, fmt.Errorf("unsupported dedup storage: %s", storage)
	}
}
