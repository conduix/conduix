package stream

import (
	"context"
	"fmt"
	"log"
	"sync"

	"golang.org/x/time/rate"
)

// ThrottleStage limits the processing rate of records
type ThrottleStage struct {
	BaseStage
	limiter     *rate.Limiter
	dropOnLimit bool
	strategy    string

	// rateMu는 런타임 rate 갱신(SetRate)과 Process의 limiter 접근을 보호한다.
	rateMu   sync.RWMutex
	interval string // second | minute | hour (SetRate에서 limit 재계산에 사용)
}

// intervalToLimit는 interval당 rate 값을 초당 rate.Limit으로 변환한다.
func intervalToLimit(ratePerInterval int, interval string) (rate.Limit, error) {
	switch interval {
	case "second":
		return rate.Limit(ratePerInterval), nil
	case "minute":
		return rate.Limit(float64(ratePerInterval) / 60.0), nil
	case "hour":
		return rate.Limit(float64(ratePerInterval) / 3600.0), nil
	default:
		return 0, fmt.Errorf("unknown interval: %s (use second, minute, or hour)", interval)
	}
}

// NewThrottleStage creates a new throttle stage using token bucket algorithm
func NewThrottleStage(name string, config map[string]any) (*ThrottleStage, error) {
	s := &ThrottleStage{
		BaseStage:   BaseStage{name: name, typ: "throttle", config: config},
		dropOnLimit: false,
		strategy:    "token_bucket",
	}

	// rate 파싱 (required)
	rateVal := 0
	switch r := config["rate"].(type) {
	case int:
		rateVal = r
	case float64:
		rateVal = int(r)
	}
	if rateVal <= 0 {
		return nil, fmt.Errorf("rate is required and must be > 0 for throttle stage")
	}

	// interval 파싱
	interval := "second"
	if i, ok := config["interval"].(string); ok {
		interval = i
	}
	s.interval = interval

	// rate.Limit 계산: events per second
	limit, err := intervalToLimit(rateVal, interval)
	if err != nil {
		return nil, err
	}

	// burst 파싱 (default: rate value)
	burst := rateVal
	switch b := config["burst"].(type) {
	case int:
		if b > 0 {
			burst = b
		}
	case float64:
		if int(b) > 0 {
			burst = int(b)
		}
	}

	s.limiter = rate.NewLimiter(limit, burst)

	// strategy 파싱
	if strategy, ok := config["strategy"].(string); ok {
		s.strategy = strategy
		if strategy != "token_bucket" {
			log.Printf("[throttle] strategy %q not implemented, falling back to token_bucket", strategy)
		}
	}

	// drop_on_limit 파싱
	if drop, ok := config["drop_on_limit"].(bool); ok {
		s.dropOnLimit = drop
	}

	return s, nil
}

// Process applies rate limiting to the record
func (s *ThrottleStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	s.rateMu.RLock()
	limiter := s.limiter
	s.rateMu.RUnlock()

	if s.dropOnLimit {
		// 초과 시 레코드 드롭
		if !limiter.Allow() {
			return nil, nil
		}
	} else {
		// 허용될 때까지 대기
		if err := limiter.Wait(ctx); err != nil {
			s.incrementError()
			return nil, fmt.Errorf("throttle wait interrupted: %w", err)
		}
	}

	s.incrementOutput()
	return record, nil
}

// SetRate는 실행 중 처리율(interval당 rate)과 burst를 런타임으로 갱신한다.
// interval은 생성 시 값을 재사용한다. burst<=0이면 rate 값을 burst로 사용한다.
// rate.Limiter의 SetLimit/SetBurst는 대기 중인 호출에도 즉시 반영된다.
func (s *ThrottleStage) SetRate(ratePerInterval, burst int) error {
	if ratePerInterval <= 0 {
		return fmt.Errorf("rate must be > 0")
	}
	limit, err := intervalToLimit(ratePerInterval, s.interval)
	if err != nil {
		return err
	}
	if burst <= 0 {
		burst = ratePerInterval
	}

	s.rateMu.Lock()
	s.limiter.SetLimit(limit)
	s.limiter.SetBurst(burst)
	s.rateMu.Unlock()

	log.Printf("[throttle:%s] rate updated: %d/%s (burst=%d)", s.name, ratePerInterval, s.interval, burst)
	return nil
}
