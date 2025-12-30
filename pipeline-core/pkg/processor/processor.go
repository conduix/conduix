// Package processor 데이터 처리 단계 구현
package processor

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// Processor 처리 단계 인터페이스
type Processor interface {
	Process(ctx context.Context, record source.Record) (*source.Record, error)
	Name() string
}

// NewProcessor 설정에서 프로세서 생성
func NewProcessor(step config.StepV2) (Processor, error) {
	processors := []Processor{}

	// Transform (Bloblang-like)
	if step.Transform != "" {
		processors = append(processors, &TransformProcessor{
			name:      step.Name,
			transform: step.Transform,
		})
	}

	// Filter
	if !step.Filter.IsEmpty() {
		processors = append(processors, &FilterProcessor{
			name:   step.Name,
			filter: step.Filter.GetExpression(),
		})
	}

	// Sample
	if step.Sample > 0 {
		processors = append(processors, &SampleProcessor{
			name: step.Name,
			rate: step.Sample,
		})
	}

	// Select fields
	if len(step.Select) > 0 {
		processors = append(processors, &SelectProcessor{
			name:   step.Name,
			fields: step.Select,
		})
	}

	// Exclude fields
	if len(step.Exclude) > 0 {
		processors = append(processors, &ExcludeProcessor{
			name:   step.Name,
			fields: step.Exclude,
		})
	}

	if len(processors) == 0 {
		return &NoopProcessor{name: step.Name}, nil
	}

	if len(processors) == 1 {
		return processors[0], nil
	}

	return &ChainProcessor{
		name:       step.Name,
		processors: processors,
	}, nil
}

// ChainProcessor 여러 프로세서 체인
type ChainProcessor struct {
	name       string
	processors []Processor
}

func (p *ChainProcessor) Name() string {
	return p.name
}

func (p *ChainProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	current := &record
	for _, proc := range p.processors {
		result, err := proc.Process(ctx, *current)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil // 필터링됨
		}
		current = result
	}
	return current, nil
}

// NoopProcessor 아무 처리도 하지 않는 프로세서
type NoopProcessor struct {
	name string
}

func (p *NoopProcessor) Name() string {
	return p.name
}

func (p *NoopProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	return &record, nil
}

// TransformProcessor 변환 프로세서 (간단한 표현식 지원)
type TransformProcessor struct {
	name      string
	transform string
}

func (p *TransformProcessor) Name() string {
	return p.name
}

func (p *TransformProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	// 간단한 필드 매핑 지원
	// 형식: "new_field = .old_field" 또는 "new_field = 'literal'"
	lines := strings.Split(p.transform, "\n")
	result := make(map[string]any)

	// 기존 데이터 복사
	for k, v := range record.Data {
		result[k] = v
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		field := strings.TrimSpace(parts[0])
		expr := strings.TrimSpace(parts[1])

		// 필드 참조: .fieldname
		if strings.HasPrefix(expr, ".") {
			srcField := strings.TrimPrefix(expr, ".")
			if val, ok := record.Data[srcField]; ok {
				result[field] = val
			}
		} else if strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'") {
			// 리터럴 값
			result[field] = strings.Trim(expr, "'")
		} else if strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"") {
			result[field] = strings.Trim(expr, "\"")
		}
	}

	return &source.Record{
		Data:     result,
		Metadata: record.Metadata,
	}, nil
}

// FilterProcessor 필터 프로세서
type FilterProcessor struct {
	name   string
	filter string
}

func (p *FilterProcessor) Name() string {
	return p.name
}

func (p *FilterProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	// 간단한 필터 표현식 지원
	// 형식: ".field == 'value'" 또는 ".field != 'value'" 또는 ".field exists"
	filter := strings.TrimSpace(p.filter)

	// exists 체크
	if strings.HasSuffix(filter, " exists") {
		field := strings.TrimSuffix(filter, " exists")
		field = strings.TrimPrefix(strings.TrimSpace(field), ".")
		if _, ok := record.Data[field]; ok {
			return &record, nil
		}
		return nil, nil
	}

	// == 비교
	if strings.Contains(filter, "==") {
		parts := strings.SplitN(filter, "==", 2)
		field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

		if val, ok := record.Data[field]; ok {
			if fmt.Sprintf("%v", val) == value {
				return &record, nil
			}
		}
		return nil, nil
	}

	// != 비교
	if strings.Contains(filter, "!=") {
		parts := strings.SplitN(filter, "!=", 2)
		field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

		if val, ok := record.Data[field]; ok {
			if fmt.Sprintf("%v", val) != value {
				return &record, nil
			}
		}
		return &record, nil
	}

	// ~= 정규식
	if strings.Contains(filter, "~=") {
		parts := strings.SplitN(filter, "~=", 2)
		field := strings.TrimPrefix(strings.TrimSpace(parts[0]), ".")
		pattern := strings.Trim(strings.TrimSpace(parts[1]), "'\"")

		if val, ok := record.Data[field]; ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid regex pattern: %w", err)
			}
			if re.MatchString(fmt.Sprintf("%v", val)) {
				return &record, nil
			}
		}
		return nil, nil
	}

	// 기본: 통과
	return &record, nil
}

// SampleProcessor 샘플링 프로세서
type SampleProcessor struct {
	name string
	rate float64
}

func (p *SampleProcessor) Name() string {
	return p.name
}

func (p *SampleProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	if rand.Float64() < p.rate {
		return &record, nil
	}
	return nil, nil
}

// SelectProcessor 필드 선택 프로세서
type SelectProcessor struct {
	name   string
	fields []string
}

func (p *SelectProcessor) Name() string {
	return p.name
}

func (p *SelectProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	result := make(map[string]any)
	for _, field := range p.fields {
		if val, ok := record.Data[field]; ok {
			result[field] = val
		}
	}
	return &source.Record{
		Data:     result,
		Metadata: record.Metadata,
	}, nil
}

// ExcludeProcessor 필드 제외 프로세서
type ExcludeProcessor struct {
	name   string
	fields []string
}

func (p *ExcludeProcessor) Name() string {
	return p.name
}

func (p *ExcludeProcessor) Process(ctx context.Context, record source.Record) (*source.Record, error) {
	result := make(map[string]any)
	excludeSet := make(map[string]bool)
	for _, f := range p.fields {
		excludeSet[f] = true
	}

	for k, v := range record.Data {
		if !excludeSet[k] {
			result[k] = v
		}
	}
	return &source.Record{
		Data:     result,
		Metadata: record.Metadata,
	}, nil
}
