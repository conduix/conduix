package stream

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// dedupeEntry tracks a seen key with its timestamp
type dedupeEntry struct {
	seenAt    time.Time
	recordTS  time.Time // keep_latest 전략용 (레코드의 타임스탬프)
}

// DedupeStage removes duplicate records based on key fields
type DedupeStage struct {
	BaseStage
	keyFields      []string
	strategy       string // "keep_first", "keep_last", "keep_latest"
	window         time.Duration
	timestampField string

	mu   sync.Mutex
	seen map[string]dedupeEntry
}

// NewDedupeStage creates a new dedupe stage
func NewDedupeStage(name string, config map[string]any) (*DedupeStage, error) {
	s := &DedupeStage{
		BaseStage: BaseStage{name: name, typ: "dedupe", config: config},
		strategy:  "keep_first",
		seen:      make(map[string]dedupeEntry),
	}

	// key_fields 파싱
	if f, ok := config["key_fields"].([]any); ok {
		for _, v := range f {
			if str, ok := v.(string); ok {
				s.keyFields = append(s.keyFields, str)
			}
		}
	}
	if len(s.keyFields) == 0 {
		return nil, fmt.Errorf("key_fields is required for dedupe stage")
	}

	if strategy, ok := config["strategy"].(string); ok {
		s.strategy = strategy
	}

	if w, ok := config["window"].(string); ok && w != "" {
		d, err := time.ParseDuration(w)
		if err != nil {
			return nil, fmt.Errorf("invalid window duration %q: %w", w, err)
		}
		s.window = d
	}

	if tf, ok := config["timestamp_field"].(string); ok {
		s.timestampField = tf
	}

	return s, nil
}

// buildKey creates a composite key from key_fields values
func (s *DedupeStage) buildKey(data map[string]any) string {
	parts := make([]string, 0, len(s.keyFields))
	for _, field := range s.keyFields {
		parts = append(parts, fmt.Sprintf("%v", data[field]))
	}
	return strings.Join(parts, "|")
}

// cleanExpired removes entries older than the window duration (lazy cleanup)
func (s *DedupeStage) cleanExpired(now time.Time) {
	if s.window <= 0 {
		return
	}
	cutoff := now.Add(-s.window)
	for key, entry := range s.seen {
		if entry.seenAt.Before(cutoff) {
			delete(s.seen, key)
		}
	}
}

// parseRecordTimestamp extracts timestamp from record for keep_latest strategy
func (s *DedupeStage) parseRecordTimestamp(data map[string]any) time.Time {
	if s.timestampField == "" {
		return time.Time{}
	}
	val, ok := data[s.timestampField]
	if !ok {
		return time.Time{}
	}

	switch v := val.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	case time.Time:
		return v
	}
	return time.Time{}
}

// Process checks for duplicates and applies the configured strategy
func (s *DedupeStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	key := s.buildKey(record.Data)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Lazy cleanup of expired entries
	s.cleanExpired(now)

	existing, exists := s.seen[key]

	// 윈도우 밖이면 "미존재"와 동일하게 처리
	if exists && s.window > 0 && now.Sub(existing.seenAt) > s.window {
		exists = false
	}

	switch s.strategy {
	case "keep_first":
		if exists {
			// 중복 → 드롭
			return nil, nil
		}
		s.seen[key] = dedupeEntry{seenAt: now}
		s.incrementOutput()
		return record, nil

	case "keep_last":
		// 항상 최신 레코드로 업데이트, 이전에 있었으면 이전 것을 드롭한 것으로 간주
		s.seen[key] = dedupeEntry{seenAt: now}
		s.incrementOutput()
		return record, nil

	case "keep_latest":
		recordTS := s.parseRecordTimestamp(record.Data)
		if exists && !recordTS.IsZero() && !existing.recordTS.IsZero() {
			if recordTS.Before(existing.recordTS) || recordTS.Equal(existing.recordTS) {
				// 현재 레코드가 기존보다 이전 또는 같음 → 드롭
				return nil, nil
			}
		}
		s.seen[key] = dedupeEntry{seenAt: now, recordTS: recordTS}
		s.incrementOutput()
		return record, nil

	default:
		s.incrementError()
		return nil, fmt.Errorf("unknown dedupe strategy: %s", s.strategy)
	}
}

// Close cleans up resources
func (s *DedupeStage) Close() error {
	s.mu.Lock()
	s.seen = make(map[string]dedupeEntry)
	s.mu.Unlock()
	return nil
}
