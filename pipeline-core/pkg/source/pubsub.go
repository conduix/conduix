// Package source Google Cloud Pub/Sub 데이터 소스
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// PubSubSource Google Cloud Pub/Sub 데이터 소스
type PubSubSource struct {
	projectID              string
	subscriptionID         string
	credentialsFile        string
	maxOutstandingMessages int
	maxOutstandingBytes    int
	maxExtension           time.Duration
	synchronous            bool
	numGoroutines          int

	client       *pubsub.Client
	subscription *pubsub.Subscription
	mu           sync.RWMutex

	// 체크포인트
	lastMessageID  string
	processedCount int64
	checkpointMu   sync.RWMutex
}

// NewPubSubSource Pub/Sub 소스 생성
func NewPubSubSource(cfg config.SourceV2) (*PubSubSource, error) {
	projectID := expandEnvVars(cfg.PubSubProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("pubsub project_id is required")
	}

	subscriptionID := expandEnvVars(cfg.PubSubSubscription)
	if subscriptionID == "" {
		return nil, fmt.Errorf("pubsub subscription is required")
	}

	maxOutstandingMessages := cfg.PubSubMaxOutstandingMessages
	if maxOutstandingMessages <= 0 {
		maxOutstandingMessages = 1000
	}

	maxOutstandingBytes := cfg.PubSubMaxOutstandingBytes
	if maxOutstandingBytes <= 0 {
		maxOutstandingBytes = 100 * 1024 * 1024 // 100MB
	}

	maxExtension := 10 * time.Minute // 기본값
	if cfg.PubSubMaxExtension != "" {
		d, err := time.ParseDuration(cfg.PubSubMaxExtension)
		if err == nil {
			maxExtension = d
		}
	}

	numGoroutines := cfg.PubSubNumGoroutines
	if numGoroutines <= 0 {
		numGoroutines = 10
	}

	return &PubSubSource{
		projectID:              projectID,
		subscriptionID:         subscriptionID,
		credentialsFile:        expandEnvVars(cfg.PubSubCredentialsFile),
		maxOutstandingMessages: maxOutstandingMessages,
		maxOutstandingBytes:    maxOutstandingBytes,
		maxExtension:           maxExtension,
		synchronous:            cfg.PubSubSynchronous,
		numGoroutines:          numGoroutines,
	}, nil
}

func (s *PubSubSource) Name() string {
	return "pubsub"
}

func (s *PubSubSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var opts []option.ClientOption

	// 서비스 계정 인증
	if s.credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(s.credentialsFile))
	}
	// 그 외는 기본 자격 증명 사용 (GOOGLE_APPLICATION_CREDENTIALS 환경변수, GCE 메타데이터 등)

	client, err := pubsub.NewClient(ctx, s.projectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}
	s.client = client

	// 구독 설정
	sub := client.Subscription(s.subscriptionID)

	// 구독 존재 확인
	exists, err := sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check subscription: %w", err)
	}
	if !exists {
		return fmt.Errorf("subscription %s does not exist", s.subscriptionID)
	}

	// 수신 설정
	sub.ReceiveSettings = pubsub.ReceiveSettings{
		MaxOutstandingMessages: s.maxOutstandingMessages,
		MaxOutstandingBytes:    s.maxOutstandingBytes,
		MaxExtension:           s.maxExtension,
		Synchronous:            s.synchronous,
		NumGoroutines:          s.numGoroutines,
	}

	s.subscription = sub

	slog.Default().Info("PubSub connected",
		"project", s.projectID, "subscription", s.subscriptionID,
		"max_outstanding_messages", s.maxOutstandingMessages)

	return nil
}

func (s *PubSubSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		s.mu.RLock()
		sub := s.subscription
		s.mu.RUnlock()

		if sub == nil {
			errs <- fmt.Errorf("Pub/Sub subscription not initialized")
			return
		}

		// 메시지 수신
		err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
			record, err := s.convertMessage(msg)
			if err != nil {
				slog.Default().Error("PubSub error converting message",
					"subscription", s.subscriptionID, "error", err)
				msg.Nack()
				return
			}

			// 체크포인트 업데이트
			s.updateCheckpoint(msg.ID)

			select {
			case records <- record:
				msg.Ack()
			case <-ctx.Done():
				msg.Nack()
				return
			}
		})

		if err != nil && err != context.Canceled {
			select {
			case errs <- fmt.Errorf("receive error: %w", err):
			default:
			}
		}
	}()

	return records, errs
}

func (s *PubSubSource) convertMessage(msg *pubsub.Message) (Record, error) {
	var data map[string]any

	// JSON 파싱 시도
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		// JSON이 아닌 경우 raw data로 처리
		data = map[string]any{
			"data": string(msg.Data),
		}
	}

	// 메시지 ID 추가
	data["_message_id"] = msg.ID

	// 발행 시간 추가
	data["_publish_time"] = msg.PublishTime.Format(time.RFC3339)

	// Ordering Key 추가 (있는 경우)
	if msg.OrderingKey != "" {
		data["_ordering_key"] = msg.OrderingKey
	}

	// 속성 추가
	if len(msg.Attributes) > 0 {
		data["_attributes"] = msg.Attributes
	}

	// DeliveryAttempt 추가 (Dead Letter 관련)
	if msg.DeliveryAttempt != nil {
		data["_delivery_attempt"] = *msg.DeliveryAttempt
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "pubsub",
			Origin:    fmt.Sprintf("projects/%s/subscriptions/%s", s.projectID, s.subscriptionID),
			Offset:    msg.ID,
			Timestamp: msg.PublishTime.UnixMilli(),
		},
	}, nil
}

func (s *PubSubSource) updateCheckpoint(messageID string) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.lastMessageID = messageID
	atomic.AddInt64(&s.processedCount, 1)
}

// SourceType 소스 타입 반환
func (s *PubSubSource) SourceType() string {
	return "pubsub"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *PubSubSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	return []*SourceCheckpoint{
		{
			PartitionKey: fmt.Sprintf("projects/%s/subscriptions/%s", s.projectID, s.subscriptionID),
			OffsetValue:  s.lastMessageID,
			OffsetType:   "string",
			RecordCount:  atomic.LoadInt64(&s.processedCount),
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정
func (s *PubSubSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	// Pub/Sub은 메시지 기반 ack를 사용하므로 체크포인트 복원이 의미 없음
	// ack되지 않은 메시지는 acknowledgement deadline 후 다시 수신 가능
	slog.Default().Info("PubSub checkpoint restoration not supported (ack deadline based)",
		"subscription", s.subscriptionID)
	return nil
}

func (s *PubSubSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			slog.Default().Warn("PubSub error closing client", "subscription", s.subscriptionID, "error", err)
		}
		s.client = nil
	}

	s.checkpointMu.RLock()
	lastID := s.lastMessageID
	count := atomic.LoadInt64(&s.processedCount)
	s.checkpointMu.RUnlock()

	slog.Default().Info("PubSub closed",
		"subscription", s.subscriptionID, "last_message_id", lastID, "processed", count)
	return nil
}

// SeekToSnapshot 스냅샷으로 이동 (메시지 재처리)
func (s *PubSubSource) SeekToSnapshot(ctx context.Context, snapshotName string) error {
	s.mu.RLock()
	sub := s.subscription
	s.mu.RUnlock()

	if sub == nil {
		return fmt.Errorf("Pub/Sub subscription not initialized")
	}

	snapshot := s.client.Snapshot(snapshotName)
	return sub.SeekToSnapshot(ctx, snapshot)
}

// SeekToTime 특정 시간으로 이동 (메시지 재처리)
func (s *PubSubSource) SeekToTime(ctx context.Context, t time.Time) error {
	s.mu.RLock()
	sub := s.subscription
	s.mu.RUnlock()

	if sub == nil {
		return fmt.Errorf("Pub/Sub subscription not initialized")
	}

	return sub.SeekToTime(ctx, t)
}

// CreateSnapshot 현재 위치의 스냅샷 생성
func (s *PubSubSource) CreateSnapshot(ctx context.Context, snapshotName string) (*pubsub.SnapshotConfig, error) {
	s.mu.RLock()
	sub := s.subscription
	s.mu.RUnlock()

	if sub == nil {
		return nil, fmt.Errorf("Pub/Sub subscription not initialized")
	}

	// pubsub v1.49+ API: subscription.CreateSnapshot
	return sub.CreateSnapshot(ctx, snapshotName)
}
