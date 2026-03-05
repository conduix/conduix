// Package contract Circuit Breaker 구현
// 연속 위반 감지 시 파이프라인 보호
package contract

import (
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState Circuit Breaker 상태
type CircuitState int32

const (
	CircuitClosed   CircuitState = iota // 정상 동작
	CircuitOpen                         // 차단 (위반 임계치 초과)
	CircuitHalfOpen                     // 테스트 중 (일부 허용)
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig Circuit Breaker 설정
type CircuitBreakerConfig struct {
	// 연속 위반 임계치 (이 값 초과 시 Circuit Open)
	ConsecutiveFailures int `json:"consecutive_failures" yaml:"consecutive_failures"`

	// Sliding Window 기반 위반율 임계치 (0.0 ~ 1.0)
	FailureRateThreshold float64 `json:"failure_rate_threshold" yaml:"failure_rate_threshold"`

	// Sliding Window 크기 (최근 N건 기준)
	WindowSize int `json:"window_size" yaml:"window_size"`

	// Circuit Open 후 Half-Open 전환까지 대기 시간
	OpenTimeout time.Duration `json:"open_timeout" yaml:"open_timeout"`

	// Half-Open 상태에서 테스트할 요청 수
	HalfOpenRequests int `json:"half_open_requests" yaml:"half_open_requests"`

	// 알림 콜백 (Circuit 상태 변경 시)
	OnStateChange func(from, to CircuitState) `json:"-" yaml:"-"`

	// 알림 콜백 (위반 임계치 도달 시)
	OnThresholdReached func(stats CircuitBreakerStats) `json:"-" yaml:"-"`
}

// DefaultCircuitBreakerConfig 기본 설정
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		ConsecutiveFailures:  50,  // 연속 50건 위반 시
		FailureRateThreshold: 0.8, // 또는 80% 위반율 시
		WindowSize:           100, // 최근 100건 기준
		OpenTimeout:          30 * time.Second,
		HalfOpenRequests:     5,
	}
}

// CircuitBreakerStats Circuit Breaker 통계
type CircuitBreakerStats struct {
	State               CircuitState `json:"state"`
	ConsecutiveFailures int64        `json:"consecutive_failures"`
	TotalRequests       int64        `json:"total_requests"`
	TotalFailures       int64        `json:"total_failures"`
	WindowFailureRate   float64      `json:"window_failure_rate"`
	LastStateChange     time.Time    `json:"last_state_change"`
	OpenedAt            time.Time    `json:"opened_at,omitempty"`
	LastFailure         time.Time    `json:"last_failure,omitempty"`
}

// CircuitBreaker Sliding Window 기반 Circuit Breaker
type CircuitBreaker struct {
	config *CircuitBreakerConfig

	state            int32 // CircuitState (atomic)
	consecutiveFails int64 // 연속 실패 카운트 (atomic)
	totalRequests    int64
	totalFailures    int64

	// Sliding Window
	window    []bool // true: success, false: failure
	windowIdx int
	windowMu  sync.Mutex

	// 상태 전환 시간
	lastStateChange time.Time
	openedAt        time.Time
	lastFailure     time.Time

	// Half-Open 테스트
	halfOpenCount int32 // atomic

	mu sync.RWMutex
}

// NewCircuitBreaker 새 Circuit Breaker 생성
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	windowSize := config.WindowSize
	if windowSize <= 0 {
		windowSize = 100
	}

	// Window를 성공(true)으로 초기화 - 시작 시 Open 방지
	window := make([]bool, windowSize)
	for i := range window {
		window[i] = true
	}

	return &CircuitBreaker{
		config:          config,
		state:           int32(CircuitClosed),
		window:          window,
		lastStateChange: time.Now(),
	}
}

// Allow 요청 허용 여부 확인
// Circuit Open 상태면 false 반환
func (cb *CircuitBreaker) Allow() bool {
	state := CircuitState(atomic.LoadInt32(&cb.state))

	switch state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Open Timeout 경과 시 Half-Open으로 전환
		cb.mu.RLock()
		openedAt := cb.openedAt
		cb.mu.RUnlock()

		if time.Since(openedAt) >= cb.config.OpenTimeout {
			cb.transitionTo(CircuitHalfOpen)
			return true
		}
		return false

	case CircuitHalfOpen:
		// Half-Open 상태에서는 제한된 요청만 허용
		count := atomic.AddInt32(&cb.halfOpenCount, 1)
		return int(count) <= cb.config.HalfOpenRequests
	}

	return false
}

// RecordSuccess 성공 기록
func (cb *CircuitBreaker) RecordSuccess() {
	atomic.AddInt64(&cb.totalRequests, 1)
	atomic.StoreInt64(&cb.consecutiveFails, 0)
	cb.recordToWindow(true)

	state := CircuitState(atomic.LoadInt32(&cb.state))
	if state == CircuitHalfOpen {
		// Half-Open에서 성공 시 Closed로 전환
		count := atomic.LoadInt32(&cb.halfOpenCount)
		if int(count) >= cb.config.HalfOpenRequests {
			cb.transitionTo(CircuitClosed)
		}
	}
}

// RecordFailure 실패 기록
func (cb *CircuitBreaker) RecordFailure() {
	atomic.AddInt64(&cb.totalRequests, 1)
	atomic.AddInt64(&cb.totalFailures, 1)
	consecutiveFails := atomic.AddInt64(&cb.consecutiveFails, 1)
	cb.recordToWindow(false)

	cb.mu.Lock()
	cb.lastFailure = time.Now()
	cb.mu.Unlock()

	state := CircuitState(atomic.LoadInt32(&cb.state))

	// Half-Open에서 실패 시 즉시 Open으로
	if state == CircuitHalfOpen {
		cb.transitionTo(CircuitOpen)
		return
	}

	// Closed 상태에서 임계치 확인
	if state == CircuitClosed {
		shouldOpen := false

		// 연속 실패 임계치 확인
		if cb.config.ConsecutiveFailures > 0 &&
			int(consecutiveFails) >= cb.config.ConsecutiveFailures {
			shouldOpen = true
		}

		// Window 기반 위반율 확인
		if cb.config.FailureRateThreshold > 0 {
			failureRate := cb.calculateWindowFailureRate()
			if failureRate >= cb.config.FailureRateThreshold {
				shouldOpen = true
			}
		}

		if shouldOpen {
			cb.transitionTo(CircuitOpen)

			// 알림 콜백
			if cb.config.OnThresholdReached != nil {
				cb.config.OnThresholdReached(cb.Stats())
			}
		}
	}
}

// recordToWindow Sliding Window에 결과 기록
func (cb *CircuitBreaker) recordToWindow(success bool) {
	cb.windowMu.Lock()
	defer cb.windowMu.Unlock()

	cb.window[cb.windowIdx] = success
	cb.windowIdx = (cb.windowIdx + 1) % len(cb.window)
}

// calculateWindowFailureRate Window 내 실패율 계산
func (cb *CircuitBreaker) calculateWindowFailureRate() float64 {
	cb.windowMu.Lock()
	defer cb.windowMu.Unlock()

	total := len(cb.window)
	failures := 0
	for _, success := range cb.window {
		if !success {
			failures++
		}
	}

	// 아직 Window가 채워지지 않았으면 낮은 비율 반환
	requests := atomic.LoadInt64(&cb.totalRequests)
	if requests < int64(total) {
		return float64(failures) / float64(requests)
	}

	return float64(failures) / float64(total)
}

// transitionTo 상태 전환
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	oldState := CircuitState(atomic.SwapInt32(&cb.state, int32(newState)))
	if oldState == newState {
		return
	}

	cb.mu.Lock()
	cb.lastStateChange = time.Now()
	if newState == CircuitOpen {
		cb.openedAt = time.Now()
	}
	if newState == CircuitHalfOpen {
		atomic.StoreInt32(&cb.halfOpenCount, 0)
	}
	if newState == CircuitClosed {
		atomic.StoreInt64(&cb.consecutiveFails, 0)
	}
	cb.mu.Unlock()

	// 상태 변경 알림
	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(oldState, newState)
	}
}

// State 현재 상태 반환
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(atomic.LoadInt32(&cb.state))
}

// Stats 통계 반환
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:               CircuitState(atomic.LoadInt32(&cb.state)),
		ConsecutiveFailures: atomic.LoadInt64(&cb.consecutiveFails),
		TotalRequests:       atomic.LoadInt64(&cb.totalRequests),
		TotalFailures:       atomic.LoadInt64(&cb.totalFailures),
		WindowFailureRate:   cb.calculateWindowFailureRateUnsafe(),
		LastStateChange:     cb.lastStateChange,
		OpenedAt:            cb.openedAt,
		LastFailure:         cb.lastFailure,
	}
}

// calculateWindowFailureRateUnsafe 락 없이 실패율 계산 (내부용)
func (cb *CircuitBreaker) calculateWindowFailureRateUnsafe() float64 {
	cb.windowMu.Lock()
	defer cb.windowMu.Unlock()

	total := len(cb.window)
	failures := 0
	for _, success := range cb.window {
		if !success {
			failures++
		}
	}

	requests := atomic.LoadInt64(&cb.totalRequests)
	if requests < int64(total) {
		if requests == 0 {
			return 0
		}
		return float64(failures) / float64(requests)
	}

	return float64(failures) / float64(total)
}

// Reset Circuit Breaker 초기화
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, int32(CircuitClosed))
	atomic.StoreInt64(&cb.consecutiveFails, 0)
	atomic.StoreInt64(&cb.totalRequests, 0)
	atomic.StoreInt64(&cb.totalFailures, 0)
	atomic.StoreInt32(&cb.halfOpenCount, 0)

	cb.windowMu.Lock()
	for i := range cb.window {
		cb.window[i] = true // 초기값: 성공
	}
	cb.windowIdx = 0
	cb.windowMu.Unlock()

	cb.lastStateChange = time.Now()
	cb.openedAt = time.Time{}
	cb.lastFailure = time.Time{}
}

// IsOpen Circuit이 열려있는지 확인
func (cb *CircuitBreaker) IsOpen() bool {
	return CircuitState(atomic.LoadInt32(&cb.state)) == CircuitOpen
}
