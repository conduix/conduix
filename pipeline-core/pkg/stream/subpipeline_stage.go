// Package stream Sub-pipeline Stage 구현
// 레코드에 대해 중첩 파이프라인을 실행하는 Stage
package stream

import (
	"context"
	"fmt"
	"sync"
)

// SubPipelineStage 중첩 파이프라인 실행 Stage
// 조건에 맞는 레코드에 대해 내부 파이프라인을 실행
type SubPipelineStage struct {
	BaseStage

	// 조건 (선택적 - 조건 없으면 모든 레코드 처리)
	condition string
	evaluator func(map[string]any) bool

	// 내부 파이프라인 Stages
	stages []Stage

	// 결과 병합 전략
	mergeStrategy string // replace, merge, nested

	// 실패 시 동작
	onError string // skip, fail, passthrough

	// 통계
	subPipelineExecutions int64
	subPipelineErrors     int64
	statsMu               sync.Mutex
}

// SubPipelineConfig Sub-pipeline 설정
type SubPipelineConfig struct {
	// 조건 (VRL 스타일)
	Condition string `yaml:"condition" json:"condition"`

	// 내부 파이프라인 Stage 설정 목록
	Stages []StageConfig `yaml:"stages" json:"stages"`

	// 병합 전략: replace (결과로 대체), merge (원본과 병합), nested (특정 필드에 결과 저장)
	MergeStrategy string `yaml:"merge_strategy" json:"merge_strategy"`

	// nested 전략 시 결과 저장 필드
	ResultField string `yaml:"result_field" json:"result_field"`

	// 에러 처리: skip (레코드 스킵), fail (에러 반환), passthrough (원본 통과)
	OnError string `yaml:"on_error" json:"on_error"`
}

// NewSubPipelineStage Sub-pipeline Stage 생성
func NewSubPipelineStage(name string, config map[string]any) (*SubPipelineStage, error) {
	s := &SubPipelineStage{
		BaseStage:     BaseStage{name: name, typ: "sub_pipeline", config: config},
		mergeStrategy: "replace",
		onError:       "passthrough",
	}

	// 조건 파싱
	if condition, ok := config["condition"].(string); ok && condition != "" {
		s.condition = condition
		eval, err := buildConditionEvaluator(name, condition)
		if err != nil {
			return nil, fmt.Errorf("invalid condition: %w", err)
		}
		s.evaluator = eval.evalFunc
	}

	// 병합 전략
	if strategy, ok := config["merge_strategy"].(string); ok {
		s.mergeStrategy = strategy
	}

	// 에러 처리
	if onError, ok := config["on_error"].(string); ok {
		s.onError = onError
	}

	// 내부 파이프라인 구성
	if pipelineCfg, ok := config["pipeline"].(map[string]any); ok {
		if stagesCfg, ok := pipelineCfg["stages"].([]any); ok {
			stages, err := buildStagesFromConfig(stagesCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to build sub-pipeline stages: %w", err)
			}
			s.stages = stages
		}
	} else if stagesCfg, ok := config["stages"].([]any); ok {
		// stages가 직접 설정된 경우
		stages, err := buildStagesFromConfig(stagesCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to build sub-pipeline stages: %w", err)
		}
		s.stages = stages
	}

	if len(s.stages) == 0 {
		return nil, fmt.Errorf("sub_pipeline requires at least one stage")
	}

	return s, nil
}

// buildStagesFromConfig 설정에서 Stage 목록 생성
func buildStagesFromConfig(stagesCfg []any) ([]Stage, error) {
	var stages []Stage

	for i, stageCfgAny := range stagesCfg {
		stageCfg, ok := stageCfgAny.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid stage config at index %d", i)
		}

		stageType, ok := stageCfg["type"].(string)
		if !ok {
			return nil, fmt.Errorf("stage type required at index %d", i)
		}

		stageName := fmt.Sprintf("sub_stage_%d", i)
		if name, ok := stageCfg["name"].(string); ok {
			stageName = name
		}

		// config 필드 추출 또는 전체 사용
		var stageConfig map[string]any
		if cfg, ok := stageCfg["config"].(map[string]any); ok {
			stageConfig = cfg
		} else {
			stageConfig = stageCfg
		}

		cfg := StageConfig{
			Type:   stageType,
			Name:   stageName,
			Config: stageConfig,
		}

		stage, err := NewStage(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create stage %s: %w", stageName, err)
		}

		stages = append(stages, stage)
	}

	return stages, nil
}

func (s *SubPipelineStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// 조건 확인
	if s.evaluator != nil && !s.evaluator(record.Data) {
		// 조건 불충족 - 원본 통과
		s.incrementOutput()
		return record, nil
	}

	// 서브 파이프라인 실행
	s.statsMu.Lock()
	s.subPipelineExecutions++
	s.statsMu.Unlock()

	result, err := s.executeSubPipeline(ctx, record)
	if err != nil {
		s.statsMu.Lock()
		s.subPipelineErrors++
		s.statsMu.Unlock()

		switch s.onError {
		case "skip":
			// 레코드 스킵 (필터링)
			return nil, nil
		case "fail":
			// 에러 반환
			s.incrementError()
			return nil, fmt.Errorf("sub-pipeline error: %w", err)
		default:
			// passthrough - 원본 통과
			s.incrementOutput()
			return record, nil
		}
	}

	// 결과가 nil이면 (필터링된 경우) nil 반환
	if result == nil {
		return nil, nil
	}

	// 결과 병합
	finalRecord := s.mergeResult(record, result)
	s.incrementOutput()
	return finalRecord, nil
}

// executeSubPipeline 서브 파이프라인 실행
func (s *SubPipelineStage) executeSubPipeline(ctx context.Context, record *Record) (*Record, error) {
	// 레코드 복사 (원본 보존)
	current := &Record{
		Data:      copyMap(record.Data),
		Metadata:  record.Metadata,
		Timestamp: record.Timestamp,
	}

	// 각 Stage 순차 실행
	for _, stage := range s.stages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := stage.Process(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("stage %s failed: %w", stage.Name(), err)
		}

		// 필터링된 경우
		if result == nil {
			return nil, nil
		}

		current = result
	}

	return current, nil
}

// mergeResult 결과 병합
func (s *SubPipelineStage) mergeResult(original, result *Record) *Record {
	switch s.mergeStrategy {
	case "replace":
		// 결과로 완전 대체
		return result

	case "merge":
		// 원본에 결과 병합 (결과가 우선)
		merged := &Record{
			Data:      copyMap(original.Data),
			Metadata:  original.Metadata,
			Timestamp: original.Timestamp,
		}
		for k, v := range result.Data {
			merged.Data[k] = v
		}
		return merged

	case "nested":
		// 결과를 특정 필드에 저장
		resultField := "_sub_result"
		if field, ok := s.config["result_field"].(string); ok && field != "" {
			resultField = field
		}

		nested := &Record{
			Data:      copyMap(original.Data),
			Metadata:  original.Metadata,
			Timestamp: original.Timestamp,
		}
		nested.Data[resultField] = result.Data
		return nested

	default:
		return result
	}
}

func (s *SubPipelineStage) Close() error {
	var errs []error
	for _, stage := range s.stages {
		if err := stage.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing sub-pipeline stages: %v", errs)
	}
	return nil
}

// Stats 서브 파이프라인 통계 반환
func (s *SubPipelineStage) SubPipelineStats() (executions, errors int64) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.subPipelineExecutions, s.subPipelineErrors
}

// copyMap map 깊은 복사
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = copyMap(val)
		case []any:
			result[k] = copySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// copySlice slice 깊은 복사
func copySlice(s []any) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = copyMap(val)
		case []any:
			result[i] = copySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}
