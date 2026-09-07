// Package checkpoint 체크포인트 관리자
// 스트리밍 모드에서 오프셋을 주기적으로 저장
package checkpoint

import (
	"context"
	"log/slog"
	"time"

	coreCheckpoint "github.com/conduix/conduix/pipeline-core/pkg/checkpoint"
)

// Manager 체크포인트 관리자
type Manager struct {
	client   *coreCheckpoint.Client
	interval time.Duration
	cancel   context.CancelFunc
}

// NewManager 체크포인트 관리자 생성
func NewManager(controlPlaneURL string, flushInterval time.Duration) *Manager {
	if flushInterval <= 0 {
		flushInterval = 30 * time.Second
	}
	return &Manager{
		client:   coreCheckpoint.NewClient(controlPlaneURL),
		interval: flushInterval,
	}
}

// Client 내부 체크포인트 클라이언트 반환
func (m *Manager) Client() *coreCheckpoint.Client {
	return m.client
}

// Start 주기적 체크포인트 저장 시작
func (m *Manager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)
	m.client.StartPeriodicFlush(ctx, m.interval)
	slog.Info("started periodic checkpoint flush", "interval", m.interval)
}

// Stop 체크포인트 저장 중지 및 마지막 flush
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	// 마지막 flush
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.client.FlushCheckpoints(ctx); err != nil {
		slog.Error("final checkpoint flush error", "error", err)
	}
}

// LoadForPipeline 파이프라인 체크포인트 로드
func (m *Manager) LoadForPipeline(ctx context.Context, pipelineID string) error {
	_, err := m.client.LoadCheckpoints(ctx, pipelineID)
	return err
}
