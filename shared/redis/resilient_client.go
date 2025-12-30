package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConnectionState Redis 연결 상태
type ConnectionState int

const (
	StateConnected ConnectionState = iota
	StateDisconnected
	StateReconnecting
)

func (s ConnectionState) String() string {
	switch s {
	case StateConnected:
		return "connected"
	case StateDisconnected:
		return "disconnected"
	case StateReconnecting:
		return "reconnecting"
	default:
		return "unknown"
	}
}

// CircuitState Circuit Breaker 상태
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // 정상 동작
	CircuitOpen                         // 차단 (장애 상태)
	CircuitHalfOpen                     // 시험 연결
)

// Config ResilientClient 설정
type Config struct {
	Addr     string
	Password string
	DB       int

	// 재연결 설정
	MaxRetries        int           // 최대 재시도 횟수 (0 = 무한)
	InitialBackoff    time.Duration // 초기 백오프 시간
	MaxBackoff        time.Duration // 최대 백오프 시간
	BackoffMultiplier float64       // 백오프 증가 배수

	// Circuit Breaker 설정
	FailureThreshold int           // Circuit Open까지 실패 횟수
	SuccessThreshold int           // Circuit Close까지 성공 횟수
	OpenTimeout      time.Duration // Circuit Open 유지 시간

	// 폴백 설정
	EnableLocalCache  bool          // 로컬 캐시 활성화
	LocalCacheTTL     time.Duration // 로컬 캐시 TTL
	LocalCacheMaxSize int           // 로컬 캐시 최대 크기

	// 콜백
	OnStateChange func(old, new ConnectionState) // 상태 변경 콜백
	OnError       func(err error)                // 에러 콜백
}

// DefaultConfig 기본 설정
func DefaultConfig(addr string) *Config {
	return &Config{
		Addr:              addr,
		MaxRetries:        0, // 무한 재시도
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		FailureThreshold:  5,
		SuccessThreshold:  2,
		OpenTimeout:       30 * time.Second,
		EnableLocalCache:  true,
		LocalCacheTTL:     5 * time.Minute,
		LocalCacheMaxSize: 1000,
	}
}

// ResilientClient 장애 복구 기능이 있는 Redis 클라이언트
type ResilientClient struct {
	config *Config
	client *redis.Client
	ctx    context.Context
	cancel context.CancelFunc

	// 상태 관리
	connState    ConnectionState
	circuitState CircuitState
	stateMu      sync.RWMutex

	// Circuit Breaker
	failureCount int
	successCount int
	lastFailure  time.Time
	circuitMu    sync.Mutex

	// 로컬 캐시 (폴백용)
	localCache   map[string]cacheEntry
	cacheMu      sync.RWMutex
	cacheCleanup *time.Ticker

	// Pub/Sub 관리
	subscriptions map[string]*subscriptionInfo
	subMu         sync.RWMutex

	// 메트릭
	metrics *Metrics
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type subscriptionInfo struct {
	channel  string
	handler  func(msg string)
	pubsub   *redis.PubSub
	cancel   context.CancelFunc
	attempts int
}

// Metrics Redis 클라이언트 메트릭
type Metrics struct {
	mu                  sync.RWMutex
	TotalRequests       int64
	SuccessfulRequests  int64
	FailedRequests      int64
	CacheHits           int64
	CacheMisses         int64
	ReconnectAttempts   int64
	CircuitBreakerTrips int64
	LastError           error
	LastErrorTime       time.Time
	AverageLatencyMs    float64
	latencySum          int64
	latencyCount        int64
}

// NewResilientClient 새 ResilientClient 생성
func NewResilientClient(config *Config) (*ResilientClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	rc := &ResilientClient{
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		connState:     StateDisconnected,
		circuitState:  CircuitClosed,
		localCache:    make(map[string]cacheEntry),
		subscriptions: make(map[string]*subscriptionInfo),
		metrics:       &Metrics{},
	}

	// Redis 클라이언트 생성
	rc.client = redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	})

	// 초기 연결 시도
	if err := rc.connect(); err != nil {
		// 초기 연결 실패해도 클라이언트는 반환 (백그라운드 재연결)
		go rc.reconnectLoop()
	} else {
		rc.setConnectionState(StateConnected)
	}

	// 로컬 캐시 정리 루프
	if config.EnableLocalCache {
		rc.cacheCleanup = time.NewTicker(time.Minute)
		go rc.cleanupCacheLoop()
	}

	// 헬스체크 루프
	go rc.healthCheckLoop()

	return rc, nil
}

// connect Redis 연결
func (rc *ResilientClient) connect() error {
	ctx, cancel := context.WithTimeout(rc.ctx, 5*time.Second)
	defer cancel()

	if err := rc.client.Ping(ctx).Err(); err != nil {
		return err
	}
	return nil
}

// setConnectionState 연결 상태 변경
func (rc *ResilientClient) setConnectionState(state ConnectionState) {
	rc.stateMu.Lock()
	oldState := rc.connState
	rc.connState = state
	rc.stateMu.Unlock()

	if rc.config.OnStateChange != nil && oldState != state {
		rc.config.OnStateChange(oldState, state)
	}
}

// GetConnectionState 현재 연결 상태 조회
func (rc *ResilientClient) GetConnectionState() ConnectionState {
	rc.stateMu.RLock()
	defer rc.stateMu.RUnlock()
	return rc.connState
}

// reconnectLoop 재연결 루프
func (rc *ResilientClient) reconnectLoop() {
	rc.setConnectionState(StateReconnecting)

	backoff := rc.config.InitialBackoff
	attempts := 0

	for {
		select {
		case <-rc.ctx.Done():
			return
		default:
		}

		attempts++
		rc.metrics.mu.Lock()
		rc.metrics.ReconnectAttempts++
		rc.metrics.mu.Unlock()

		if err := rc.connect(); err != nil {
			if rc.config.OnError != nil {
				rc.config.OnError(fmt.Errorf("reconnect attempt %d failed: %w", attempts, err))
			}

			// 최대 재시도 횟수 체크
			if rc.config.MaxRetries > 0 && attempts >= rc.config.MaxRetries {
				rc.setConnectionState(StateDisconnected)
				return
			}

			// Exponential backoff
			time.Sleep(backoff)
			backoff = time.Duration(float64(backoff) * rc.config.BackoffMultiplier)
			if backoff > rc.config.MaxBackoff {
				backoff = rc.config.MaxBackoff
			}
			continue
		}

		// 연결 성공
		rc.setConnectionState(StateConnected)
		rc.resetCircuitBreaker()

		// 기존 Pub/Sub 재구독
		rc.resubscribeAll()

		return
	}
}

// healthCheckLoop 헬스체크 루프
func (rc *ResilientClient) healthCheckLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rc.ctx.Done():
			return
		case <-ticker.C:
			if rc.GetConnectionState() == StateConnected {
				if err := rc.connect(); err != nil {
					rc.recordFailure(err)
					if rc.GetConnectionState() == StateConnected {
						rc.setConnectionState(StateDisconnected)
						go rc.reconnectLoop()
					}
				}
			}
		}
	}
}

// cleanupCacheLoop 로컬 캐시 정리 루프
func (rc *ResilientClient) cleanupCacheLoop() {
	for {
		select {
		case <-rc.ctx.Done():
			return
		case <-rc.cacheCleanup.C:
			rc.cleanupExpiredCache()
		}
	}
}

func (rc *ResilientClient) cleanupExpiredCache() {
	now := time.Now()
	rc.cacheMu.Lock()
	defer rc.cacheMu.Unlock()

	for key, entry := range rc.localCache {
		if now.After(entry.expiresAt) {
			delete(rc.localCache, key)
		}
	}
}

// Circuit Breaker 메서드들

func (rc *ResilientClient) recordFailure(err error) {
	rc.circuitMu.Lock()
	defer rc.circuitMu.Unlock()

	rc.failureCount++
	rc.successCount = 0
	rc.lastFailure = time.Now()

	rc.metrics.mu.Lock()
	rc.metrics.LastError = err
	rc.metrics.LastErrorTime = time.Now()
	rc.metrics.FailedRequests++
	rc.metrics.mu.Unlock()

	if rc.circuitState == CircuitClosed && rc.failureCount >= rc.config.FailureThreshold {
		rc.circuitState = CircuitOpen
		rc.metrics.mu.Lock()
		rc.metrics.CircuitBreakerTrips++
		rc.metrics.mu.Unlock()

		if rc.config.OnError != nil {
			rc.config.OnError(fmt.Errorf("circuit breaker opened after %d failures", rc.failureCount))
		}
	}
}

func (rc *ResilientClient) recordSuccess() {
	rc.circuitMu.Lock()
	defer rc.circuitMu.Unlock()

	rc.metrics.mu.Lock()
	rc.metrics.SuccessfulRequests++
	rc.metrics.mu.Unlock()

	switch rc.circuitState {
	case CircuitHalfOpen:
		rc.successCount++
		if rc.successCount >= rc.config.SuccessThreshold {
			rc.circuitState = CircuitClosed
			rc.failureCount = 0
			rc.successCount = 0
		}
	case CircuitClosed:
		rc.failureCount = 0
	}
}

func (rc *ResilientClient) resetCircuitBreaker() {
	rc.circuitMu.Lock()
	defer rc.circuitMu.Unlock()

	rc.circuitState = CircuitClosed
	rc.failureCount = 0
	rc.successCount = 0
}

func (rc *ResilientClient) canExecute() bool {
	rc.circuitMu.Lock()
	defer rc.circuitMu.Unlock()

	switch rc.circuitState {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(rc.lastFailure) > rc.config.OpenTimeout {
			rc.circuitState = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// Public API

// Set 값 저장
func (rc *ResilientClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	rc.metrics.mu.Lock()
	rc.metrics.TotalRequests++
	rc.metrics.mu.Unlock()

	// 직렬화
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	// 로컬 캐시 저장 (항상)
	if rc.config.EnableLocalCache {
		rc.setLocalCache(key, string(data), expiration)
	}

	// Circuit Breaker 체크
	if !rc.canExecute() {
		return fmt.Errorf("circuit breaker is open")
	}

	// 연결 상태 체크
	if rc.GetConnectionState() != StateConnected {
		return fmt.Errorf("redis not connected (state: %s)", rc.GetConnectionState())
	}

	// Redis 저장
	start := time.Now()
	err = rc.client.Set(ctx, key, data, expiration).Err()
	rc.recordLatency(time.Since(start))

	if err != nil {
		rc.recordFailure(err)
		// 로컬 캐시에는 저장되었으므로 부분 성공
		return fmt.Errorf("redis set failed (cached locally): %w", err)
	}

	rc.recordSuccess()
	return nil
}

// Get 값 조회
func (rc *ResilientClient) Get(ctx context.Context, key string) (string, error) {
	rc.metrics.mu.Lock()
	rc.metrics.TotalRequests++
	rc.metrics.mu.Unlock()

	// Circuit Breaker 체크 및 연결 상태 체크
	if rc.canExecute() && rc.GetConnectionState() == StateConnected {
		start := time.Now()
		result, err := rc.client.Get(ctx, key).Result()
		rc.recordLatency(time.Since(start))

		if err == nil {
			rc.recordSuccess()
			// 로컬 캐시 업데이트
			if rc.config.EnableLocalCache {
				rc.setLocalCache(key, result, rc.config.LocalCacheTTL)
			}
			return result, nil
		}

		if err != redis.Nil {
			rc.recordFailure(err)
		}
	}

	// 로컬 캐시 폴백
	if rc.config.EnableLocalCache {
		if value, ok := rc.getLocalCache(key); ok {
			rc.metrics.mu.Lock()
			rc.metrics.CacheHits++
			rc.metrics.mu.Unlock()
			return value, nil
		}
		rc.metrics.mu.Lock()
		rc.metrics.CacheMisses++
		rc.metrics.mu.Unlock()
	}

	return "", fmt.Errorf("key not found (redis unavailable, cache miss)")
}

// Del 키 삭제
func (rc *ResilientClient) Del(ctx context.Context, keys ...string) error {
	// 로컬 캐시에서 삭제
	if rc.config.EnableLocalCache {
		rc.cacheMu.Lock()
		for _, key := range keys {
			delete(rc.localCache, key)
		}
		rc.cacheMu.Unlock()
	}

	if !rc.canExecute() || rc.GetConnectionState() != StateConnected {
		return fmt.Errorf("redis not available")
	}

	err := rc.client.Del(ctx, keys...).Err()
	if err != nil {
		rc.recordFailure(err)
		return err
	}

	rc.recordSuccess()
	return nil
}

// Publish 메시지 발행
func (rc *ResilientClient) Publish(ctx context.Context, channel string, message interface{}) error {
	rc.metrics.mu.Lock()
	rc.metrics.TotalRequests++
	rc.metrics.mu.Unlock()

	if !rc.canExecute() || rc.GetConnectionState() != StateConnected {
		return fmt.Errorf("redis not available for publish")
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = rc.client.Publish(ctx, channel, data).Err()
	if err != nil {
		rc.recordFailure(err)
		return err
	}

	rc.recordSuccess()
	return nil
}

// Subscribe 채널 구독 (자동 재구독 지원)
func (rc *ResilientClient) Subscribe(ctx context.Context, channel string, handler func(msg string)) error {
	rc.subMu.Lock()
	defer rc.subMu.Unlock()

	// 이미 구독 중인지 확인
	if _, exists := rc.subscriptions[channel]; exists {
		return fmt.Errorf("already subscribed to channel: %s", channel)
	}

	subCtx, cancel := context.WithCancel(ctx)
	info := &subscriptionInfo{
		channel: channel,
		handler: handler,
		cancel:  cancel,
	}
	rc.subscriptions[channel] = info

	go rc.subscribeLoop(subCtx, info)

	return nil
}

func (rc *ResilientClient) subscribeLoop(ctx context.Context, info *subscriptionInfo) {
	backoff := rc.config.InitialBackoff

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.ctx.Done():
			return
		default:
		}

		// 연결 대기
		for rc.GetConnectionState() != StateConnected {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}

		// 구독 시작
		pubsub := rc.client.Subscribe(ctx, info.channel)
		info.pubsub = pubsub
		info.attempts++

		ch := pubsub.Channel()

		// 메시지 수신 루프
	msgLoop:
		for {
			select {
			case <-ctx.Done():
				pubsub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					// 채널 닫힘 - 재연결 필요
					break msgLoop
				}
				if msg != nil {
					info.handler(msg.Payload)
					backoff = rc.config.InitialBackoff // 성공 시 백오프 리셋
				}
			}
		}

		pubsub.Close()

		// 재연결 대기
		if rc.config.OnError != nil {
			rc.config.OnError(fmt.Errorf("subscription to %s lost, reconnecting...", info.channel))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = time.Duration(float64(backoff) * rc.config.BackoffMultiplier)
			if backoff > rc.config.MaxBackoff {
				backoff = rc.config.MaxBackoff
			}
		}
	}
}

// Unsubscribe 구독 취소
func (rc *ResilientClient) Unsubscribe(channel string) error {
	rc.subMu.Lock()
	defer rc.subMu.Unlock()

	info, exists := rc.subscriptions[channel]
	if !exists {
		return fmt.Errorf("not subscribed to channel: %s", channel)
	}

	info.cancel()
	if info.pubsub != nil {
		info.pubsub.Close()
	}
	delete(rc.subscriptions, channel)

	return nil
}

// resubscribeAll 모든 채널 재구독
func (rc *ResilientClient) resubscribeAll() {
	rc.subMu.RLock()
	defer rc.subMu.RUnlock()

	for channel, info := range rc.subscriptions {
		if info.pubsub != nil {
			info.pubsub.Close()
		}
		// subscribeLoop이 자동으로 재연결함
		fmt.Printf("Resubscribing to channel: %s\n", channel)
	}
}

// 로컬 캐시 헬퍼

func (rc *ResilientClient) setLocalCache(key, value string, ttl time.Duration) {
	rc.cacheMu.Lock()
	defer rc.cacheMu.Unlock()

	// 최대 크기 체크
	if len(rc.localCache) >= rc.config.LocalCacheMaxSize {
		// 가장 오래된 항목 제거 (간단한 구현)
		for k := range rc.localCache {
			delete(rc.localCache, k)
			break
		}
	}

	expiresAt := time.Now().Add(ttl)
	if ttl == 0 {
		expiresAt = time.Now().Add(rc.config.LocalCacheTTL)
	}

	rc.localCache[key] = cacheEntry{
		value:     value,
		expiresAt: expiresAt,
	}
}

func (rc *ResilientClient) getLocalCache(key string) (string, bool) {
	rc.cacheMu.RLock()
	defer rc.cacheMu.RUnlock()

	entry, exists := rc.localCache[key]
	if !exists {
		return "", false
	}

	if time.Now().After(entry.expiresAt) {
		return "", false
	}

	return entry.value, true
}

func (rc *ResilientClient) recordLatency(d time.Duration) {
	rc.metrics.mu.Lock()
	defer rc.metrics.mu.Unlock()

	rc.metrics.latencySum += d.Milliseconds()
	rc.metrics.latencyCount++
	if rc.metrics.latencyCount > 0 {
		rc.metrics.AverageLatencyMs = float64(rc.metrics.latencySum) / float64(rc.metrics.latencyCount)
	}
}

// GetMetrics 메트릭 조회
func (rc *ResilientClient) GetMetrics() Metrics {
	rc.metrics.mu.RLock()
	defer rc.metrics.mu.RUnlock()
	return *rc.metrics
}

// Close 클라이언트 종료
func (rc *ResilientClient) Close() error {
	rc.cancel()

	if rc.cacheCleanup != nil {
		rc.cacheCleanup.Stop()
	}

	// 모든 구독 취소
	rc.subMu.Lock()
	for _, info := range rc.subscriptions {
		info.cancel()
		if info.pubsub != nil {
			info.pubsub.Close()
		}
	}
	rc.subscriptions = make(map[string]*subscriptionInfo)
	rc.subMu.Unlock()

	return rc.client.Close()
}

// IsHealthy 연결 상태 체크
func (rc *ResilientClient) IsHealthy() bool {
	return rc.GetConnectionState() == StateConnected && rc.circuitState != CircuitOpen
}
