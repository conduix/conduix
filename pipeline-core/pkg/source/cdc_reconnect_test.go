package source

import (
	"context"
	"testing"
	"time"
)

// 연결 불가 상태에서 runCanalWithReconnect 가 재연결을 시도하되,
// ctx 취소 시 즉시(백오프 대기 중이라도) 탈출해야 한다(무한 루프/행 방지).
func TestCDC_Reconnect_ExitsOnCtxCancel(t *testing.T) {
	s := &CDCSource{
		driver:   "mysql",
		host:     "127.0.0.1",
		port:     1,
		serverID: 101, // 연결 불가 포트 → runCanalOnce 항상 실패 → 재연결 루프 진입
		running:  true,
		stopCh:   make(chan struct{}),
		errorCh:  make(chan error, 10),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.runWithReconnect(ctx); close(done) }()

	// 잠깐 돌게 둔 뒤 취소 → 곧 탈출해야 함.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// 정상: ctx 취소로 탈출
	case <-time.After(3 * time.Second):
		t.Fatal("runCanalWithReconnect did not exit after ctx cancel (would hang)")
	}
}

// Stop(running=false + stopCh close) 시에도 탈출해야 한다.
func TestCDC_Reconnect_ExitsOnStop(t *testing.T) {
	s := &CDCSource{
		driver:   "mysql",
		host:     "127.0.0.1",
		port:     1,
		serverID: 101,
		running:  true,
		stopCh:   make(chan struct{}),
		errorCh:  make(chan error, 10),
	}

	done := make(chan struct{})
	go func() { s.runWithReconnect(context.Background()); close(done) }()

	time.Sleep(300 * time.Millisecond)
	s.mu.Lock()
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runCanalWithReconnect did not exit after Stop")
	}
}
