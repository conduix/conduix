package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/contract"
	"github.com/conduix/conduix/shared/types"
)

// ContractStage Data Contract 검증 스테이지
// ECL 패러다임의 Contextualize 단계 담당
type ContractStage struct {
	BaseStage
	evaluator *contract.Evaluator
	contract  *types.DataContract
	action    types.ViolationAction
	dlqSink   Sink
	tagField  string // 위반 태그를 추가할 필드명

	// Circuit Breaker
	circuitBreaker *contract.CircuitBreaker
	circuitOpen    int32 // atomic flag

	// 메트릭스
	metrics          *types.ContractMetrics
	metricsMu        sync.RWMutex
	violationsByRule map[string]*int64
}

// ContractStageConfig ContractStage 설정
type ContractStageConfig struct {
	Contract       *types.DataContract            `json:"contract" yaml:"contract"`
	Action         types.ViolationAction          `json:"action" yaml:"action"`       // drop, quarantine, tag, error
	DLQSink        Sink                           `json:"-" yaml:"-"`                 // quarantine 모드용 DLQ 싱크
	TagField       string                         `json:"tag_field" yaml:"tag_field"` // tag 모드용 필드명
	CircuitBreaker *contract.CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
}

// NewContractStage 새 ContractStage 생성
func NewContractStage(name string, cfg *ContractStageConfig) (*ContractStage, error) {
	if cfg.Contract == nil {
		return nil, fmt.Errorf("contract is required")
	}

	evaluator, err := contract.NewEvaluator(cfg.Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluator: %w", err)
	}

	// 기본 action 설정
	action := cfg.Action
	if action == "" {
		action = types.ActionDrop
	}

	// quarantine 모드인데 DLQ가 없으면 경고
	if action == types.ActionQuarantine && cfg.DLQSink == nil {
		action = types.ActionDrop // 폴백
	}

	tagField := cfg.TagField
	if tagField == "" {
		tagField = "_contract_violations"
	}

	// 규칙별 위반 카운터 초기화
	violationsByRule := make(map[string]*int64)
	for _, rule := range cfg.Contract.Rules {
		counter := int64(0)
		violationsByRule[rule.Name] = &counter
	}
	// 스키마 위반 카운터
	schemaCounter := int64(0)
	violationsByRule["_schema"] = &schemaCounter

	// Circuit Breaker 설정
	var circuitBreaker *contract.CircuitBreaker
	cbConfig := cfg.CircuitBreaker
	if cbConfig == nil {
		cbConfig = contract.DefaultCircuitBreakerConfig()
	}

	// 상태 변경 콜백 설정
	cbConfig.OnStateChange = func(from, to contract.CircuitState) {
		log.Printf("[contract:%s] Circuit Breaker state changed: %s -> %s", name, from, to)
	}
	cbConfig.OnThresholdReached = func(stats contract.CircuitBreakerStats) {
		log.Printf("[contract:%s] Circuit Breaker threshold reached! consecutive_failures=%d, failure_rate=%.2f%%",
			name, stats.ConsecutiveFailures, stats.WindowFailureRate*100)
	}
	circuitBreaker = contract.NewCircuitBreaker(cbConfig)

	stage := &ContractStage{
		BaseStage: BaseStage{name: name, typ: "contract", config: map[string]any{
			"contract": cfg.Contract.Name,
			"action":   string(action),
		}},
		evaluator:        evaluator,
		contract:         cfg.Contract,
		action:           action,
		dlqSink:          cfg.DLQSink,
		tagField:         tagField,
		circuitBreaker:   circuitBreaker,
		violationsByRule: violationsByRule,
		metrics: &types.ContractMetrics{
			ContractID:       cfg.Contract.Name,
			ViolationsByRule: make(map[string]int64),
		},
	}

	return stage, nil
}

// NewContractStageFromConfig config map에서 ContractStage 생성
func NewContractStageFromConfig(name string, config map[string]any) (*ContractStage, error) {
	// contract 설정 파싱
	contractData, ok := config["contract"]
	if !ok {
		return nil, fmt.Errorf("contract configuration is required")
	}

	// JSON으로 변환 후 파싱
	contractJSON, err := json.Marshal(contractData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal contract: %w", err)
	}

	var dataContract types.DataContract
	if err := json.Unmarshal(contractJSON, &dataContract); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract: %w", err)
	}

	// action 설정
	action := types.ViolationAction("drop")
	if actionStr, ok := config["action"].(string); ok {
		action = types.ViolationAction(actionStr)
	}
	if actionStr, ok := config["on_violation"].(string); ok {
		action = types.ViolationAction(actionStr)
	}

	// tag_field 설정
	tagField := "_contract_violations"
	if tf, ok := config["tag_field"].(string); ok {
		tagField = tf
	}

	// circuit_breaker 설정 파싱
	var cbConfig *contract.CircuitBreakerConfig
	if cbData, ok := config["circuit_breaker"].(map[string]any); ok {
		cbConfig = &contract.CircuitBreakerConfig{}
		if v, ok := cbData["consecutive_failures"].(float64); ok {
			cbConfig.ConsecutiveFailures = int(v)
		}
		if v, ok := cbData["failure_rate_threshold"].(float64); ok {
			cbConfig.FailureRateThreshold = v
		}
		if v, ok := cbData["window_size"].(float64); ok {
			cbConfig.WindowSize = int(v)
		}
		if v, ok := cbData["open_timeout"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				cbConfig.OpenTimeout = d
			}
		}
		if v, ok := cbData["half_open_requests"].(float64); ok {
			cbConfig.HalfOpenRequests = int(v)
		}
	}

	cfg := &ContractStageConfig{
		Contract:       &dataContract,
		Action:         action,
		TagField:       tagField,
		CircuitBreaker: cbConfig,
	}

	return NewContractStage(name, cfg)
}

// Process 레코드 처리 (계약 검증)
func (s *ContractStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()
	atomic.AddInt64(&s.metrics.TotalRecords, 1)

	// Circuit Breaker 확인
	if !s.circuitBreaker.Allow() {
		// Circuit Open 상태 - 레코드 통과 (검증 스킵)
		if atomic.CompareAndSwapInt32(&s.circuitOpen, 0, 1) {
			log.Printf("[contract:%s] Circuit OPEN - skipping validation, passing records through", s.name)
		}
		s.incrementOutput()
		return record, nil
	}

	// Circuit이 다시 닫혔으면 로그
	if atomic.CompareAndSwapInt32(&s.circuitOpen, 1, 0) {
		log.Printf("[contract:%s] Circuit CLOSED - resuming validation", s.name)
	}

	// 계약 검증
	result := s.evaluator.Validate(record.Data)

	// 경고 처리 (항상 카운트)
	if len(result.Warnings) > 0 {
		atomic.AddInt64(&s.metrics.WarningRecords, 1)
	}

	// 유효한 경우
	if result.Valid {
		atomic.AddInt64(&s.metrics.ValidRecords, 1)
		s.circuitBreaker.RecordSuccess()
		s.incrementOutput()
		return record, nil
	}

	// 위반 발생
	atomic.AddInt64(&s.metrics.InvalidRecords, 1)
	s.circuitBreaker.RecordFailure()
	s.updateViolationMetrics(result.Violations)

	switch s.action {
	case types.ActionDrop:
		// 레코드 삭제 (nil 반환)
		return nil, nil

	case types.ActionQuarantine:
		// DLQ로 전송
		if s.dlqSink != nil {
			dlqRecord := s.createDLQRecord(record, result.Violations)
			if err := s.writeToDLQ(ctx, dlqRecord); err != nil {
				s.incrementError()
				// DLQ 쓰기 실패해도 레코드는 드롭
			}
		}
		return nil, nil

	case types.ActionTag:
		// 위반 태그 추가 후 통과
		record.Data[s.tagField] = s.formatViolations(result.Violations)
		s.incrementOutput()
		return record, nil

	case types.ActionError:
		// 에러 반환 (파이프라인 중단)
		s.incrementError()
		return nil, fmt.Errorf("contract violation: %s", s.formatViolations(result.Violations))

	default:
		// 기본: 드롭
		return nil, nil
	}
}

// SetDLQSink DLQ 싱크 설정
func (s *ContractStage) SetDLQSink(sink Sink) {
	s.dlqSink = sink
	if s.action == types.ActionDrop && sink != nil {
		s.action = types.ActionQuarantine
	}
}

// GetMetrics 메트릭스 조회
func (s *ContractStage) GetMetrics() types.ContractMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	metrics := types.ContractMetrics{
		ContractID:       s.metrics.ContractID,
		TotalRecords:     atomic.LoadInt64(&s.metrics.TotalRecords),
		ValidRecords:     atomic.LoadInt64(&s.metrics.ValidRecords),
		InvalidRecords:   atomic.LoadInt64(&s.metrics.InvalidRecords),
		WarningRecords:   atomic.LoadInt64(&s.metrics.WarningRecords),
		ViolationsByRule: make(map[string]int64),
		LastUpdated:      time.Now(),
	}

	for ruleName, counter := range s.violationsByRule {
		metrics.ViolationsByRule[ruleName] = atomic.LoadInt64(counter)
	}

	return metrics
}

// GetCircuitBreakerStats Circuit Breaker 통계 조회
func (s *ContractStage) GetCircuitBreakerStats() contract.CircuitBreakerStats {
	return s.circuitBreaker.Stats()
}

// IsCircuitOpen Circuit이 열려있는지 확인
func (s *ContractStage) IsCircuitOpen() bool {
	return s.circuitBreaker.IsOpen()
}

// ResetCircuitBreaker Circuit Breaker 초기화
func (s *ContractStage) ResetCircuitBreaker() {
	s.circuitBreaker.Reset()
	atomic.StoreInt32(&s.circuitOpen, 0)
	log.Printf("[contract:%s] Circuit Breaker reset", s.name)
}

// updateViolationMetrics 위반 메트릭스 업데이트
func (s *ContractStage) updateViolationMetrics(violations []types.ContractViolation) {
	for _, v := range violations {
		if counter, ok := s.violationsByRule[v.RuleName]; ok {
			atomic.AddInt64(counter, 1)
		} else if counter, ok := s.violationsByRule["_schema"]; ok {
			atomic.AddInt64(counter, 1)
		}
	}
}

// createDLQRecord DLQ 레코드 생성
func (s *ContractStage) createDLQRecord(record *Record, violations []types.ContractViolation) *types.DLQRecord {
	return &types.DLQRecord{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:    time.Now(),
		Source:       record.Metadata.Source,
		ContractID:   s.contract.Name,
		Violations:   violations,
		OriginalData: record.Data,
		Metadata: map[string]string{
			"partition": fmt.Sprintf("%d", record.Metadata.Partition),
			"offset":    fmt.Sprintf("%d", record.Metadata.Offset),
		},
	}
}

// writeToDLQ DLQ에 쓰기
func (s *ContractStage) writeToDLQ(ctx context.Context, dlqRecord *types.DLQRecord) error {
	if s.dlqSink == nil {
		return fmt.Errorf("DLQ sink not configured")
	}

	// DLQ 레코드를 Record로 변환
	dlqData := map[string]any{
		"id":            dlqRecord.ID,
		"timestamp":     dlqRecord.Timestamp,
		"source":        dlqRecord.Source,
		"contract_id":   dlqRecord.ContractID,
		"violations":    dlqRecord.Violations,
		"original_data": dlqRecord.OriginalData,
		"metadata":      dlqRecord.Metadata,
	}

	record := &Record{
		Data:      dlqData,
		Timestamp: time.Now(),
		Metadata: RecordMetadata{
			Source: "dlq",
		},
	}

	return s.dlqSink.Write(ctx, record)
}

// formatViolations 위반 정보를 문자열로 포맷
func (s *ContractStage) formatViolations(violations []types.ContractViolation) string {
	if len(violations) == 0 {
		return ""
	}

	messages := make([]string, 0, len(violations))
	for _, v := range violations {
		messages = append(messages, fmt.Sprintf("[%s] %s", v.RuleName, v.Message))
	}

	result, _ := json.Marshal(messages)
	return string(result)
}

// Close 리소스 정리
func (s *ContractStage) Close() error {
	if s.dlqSink != nil {
		return s.dlqSink.Close()
	}
	return nil
}
