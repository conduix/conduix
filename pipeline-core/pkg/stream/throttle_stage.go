package stream

import (
	"context"
	"fmt"
	"log"

	"golang.org/x/time/rate"
)

// ThrottleStage limits the processing rate of records
type ThrottleStage struct {
	BaseStage
	limiter     *rate.Limiter
	dropOnLimit bool
	strategy    string
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

	// rate.Limit 계산: events per second
	var limit rate.Limit
	switch interval {
	case "second":
		limit = rate.Limit(rateVal)
	case "minute":
		limit = rate.Limit(float64(rateVal) / 60.0)
	case "hour":
		limit = rate.Limit(float64(rateVal) / 3600.0)
	default:
		return nil, fmt.Errorf("unknown interval: %s (use second, minute, or hour)", interval)
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

	if s.dropOnLimit {
		// 초과 시 레코드 드롭
		if !s.limiter.Allow() {
			return nil, nil
		}
	} else {
		// 허용될 때까지 대기
		if err := s.limiter.Wait(ctx); err != nil {
			s.incrementError()
			return nil, fmt.Errorf("throttle wait interrupted: %w", err)
		}
	}

	s.incrementOutput()
	return record, nil
}
