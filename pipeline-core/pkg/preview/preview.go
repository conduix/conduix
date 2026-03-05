// Package preview 파이프라인 데이터 미리보기 기능
package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/stream"
)

// PreviewResult Stage 미리보기 결과
type PreviewResult struct {
	StageName     string          `json:"stage_name"`
	StageType     string          `json:"stage_type"`
	InputRecords  []PreviewRecord `json:"input_records"`
	OutputRecords []PreviewRecord `json:"output_records"`
	FilteredCount int             `json:"filtered_count"`
	ErrorCount    int             `json:"error_count"`
	ProcessTime   time.Duration   `json:"process_time_ns"`
	Diff          []RecordDiff    `json:"diff,omitempty"`
	Stats         PreviewStats    `json:"stats"`
}

// PreviewRecord 미리보기용 레코드
type PreviewRecord struct {
	Index    int            `json:"index"`
	Data     map[string]any `json:"data"`
	Metadata RecordMetadata `json:"metadata,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// RecordMetadata 레코드 메타데이터
type RecordMetadata struct {
	Source    string `json:"source,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// RecordDiff 레코드 변경 비교
type RecordDiff struct {
	Index          int         `json:"index"`
	Type           DiffType    `json:"type"` // added, removed, modified, unchanged
	AddedFields    []string    `json:"added_fields,omitempty"`
	RemovedFields  []string    `json:"removed_fields,omitempty"`
	ModifiedFields []FieldDiff `json:"modified_fields,omitempty"`
}

// FieldDiff 필드 변경 상세
type FieldDiff struct {
	Field    string `json:"field"`
	OldValue any    `json:"old_value"`
	NewValue any    `json:"new_value"`
}

// DiffType 변경 타입
type DiffType string

const (
	DiffTypeAdded     DiffType = "added"
	DiffTypeRemoved   DiffType = "removed"
	DiffTypeModified  DiffType = "modified"
	DiffTypeUnchanged DiffType = "unchanged"
)

// PreviewStats 미리보기 통계
type PreviewStats struct {
	TotalInput  int     `json:"total_input"`
	TotalOutput int     `json:"total_output"`
	PassRate    float64 `json:"pass_rate"`
	FilterRate  float64 `json:"filter_rate"`
	ErrorRate   float64 `json:"error_rate"`
	AvgFields   float64 `json:"avg_fields"`
}

// PreviewOptions 미리보기 옵션
type PreviewOptions struct {
	MaxRecords  int           `json:"max_records"`  // 최대 레코드 수 (기본: 10)
	IncludeDiff bool          `json:"include_diff"` // diff 포함 여부
	Timeout     time.Duration `json:"timeout"`      // 타임아웃 (기본: 30초)
	StageNames  []string      `json:"stage_names"`  // 특정 Stage만 미리보기 (빈 경우 전체)
}

// DefaultPreviewOptions 기본 옵션
func DefaultPreviewOptions() PreviewOptions {
	return PreviewOptions{
		MaxRecords:  10,
		IncludeDiff: true,
		Timeout:     30 * time.Second,
	}
}

// Previewer 파이프라인 미리보기 실행기
type Previewer struct {
	stages  []stream.Stage
	options PreviewOptions
	mu      sync.Mutex
}

// NewPreviewer 미리보기 실행기 생성
func NewPreviewer(stages []stream.Stage, opts PreviewOptions) *Previewer {
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = 10
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &Previewer{
		stages:  stages,
		options: opts,
	}
}

// Preview 샘플 데이터로 Stage 미리보기 실행
func (p *Previewer) Preview(ctx context.Context, sampleData []map[string]any) ([]PreviewResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, p.options.Timeout)
	defer cancel()

	// 샘플 데이터를 stream.Record로 변환
	records := make([]*stream.Record, 0, len(sampleData))
	for i, data := range sampleData {
		if i >= p.options.MaxRecords {
			break
		}
		records = append(records, &stream.Record{
			Data: data,
			Metadata: stream.RecordMetadata{
				Source: "preview",
			},
			Timestamp: time.Now(),
		})
	}

	results := make([]PreviewResult, 0, len(p.stages))

	// 각 Stage 순차 실행
	currentRecords := records
	for _, stage := range p.stages {
		// 특정 Stage만 미리보기하는 경우 필터링
		if len(p.options.StageNames) > 0 {
			found := false
			for _, name := range p.options.StageNames {
				if stage.Name() == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		result, outputRecords, err := p.previewStage(ctx, stage, currentRecords)
		if err != nil {
			return nil, fmt.Errorf("preview failed for stage %s: %w", stage.Name(), err)
		}

		results = append(results, result)
		currentRecords = outputRecords
	}

	return results, nil
}

// previewStage 단일 Stage 미리보기
func (p *Previewer) previewStage(ctx context.Context, stage stream.Stage, inputRecords []*stream.Record) (PreviewResult, []*stream.Record, error) {
	start := time.Now()

	result := PreviewResult{
		StageName:    stage.Name(),
		StageType:    stage.Type(),
		InputRecords: make([]PreviewRecord, 0, len(inputRecords)),
	}

	// 입력 레코드 저장
	for i, record := range inputRecords {
		result.InputRecords = append(result.InputRecords, PreviewRecord{
			Index: i,
			Data:  record.Data,
			Metadata: RecordMetadata{
				Source:    record.Metadata.Source,
				Timestamp: record.Timestamp.Format(time.RFC3339),
			},
		})
	}

	// Stage 처리
	outputRecords := make([]*stream.Record, 0, len(inputRecords))
	for _, record := range inputRecords {
		select {
		case <-ctx.Done():
			return result, nil, ctx.Err()
		default:
		}

		output, err := stage.Process(ctx, record)
		if err != nil {
			result.ErrorCount++
			result.OutputRecords = append(result.OutputRecords, PreviewRecord{
				Index: len(result.OutputRecords),
				Data:  record.Data,
				Error: err.Error(),
			})
			continue
		}

		if output == nil {
			// 필터링됨
			result.FilteredCount++
		} else {
			result.OutputRecords = append(result.OutputRecords, PreviewRecord{
				Index: len(result.OutputRecords),
				Data:  output.Data,
				Metadata: RecordMetadata{
					Source:    output.Metadata.Source,
					Timestamp: output.Timestamp.Format(time.RFC3339),
				},
			})
			outputRecords = append(outputRecords, output)
		}
	}

	result.ProcessTime = time.Since(start)

	// 통계 계산
	result.Stats = p.calculateStats(result)

	// Diff 계산
	if p.options.IncludeDiff && len(result.InputRecords) > 0 {
		result.Diff = p.calculateDiff(result.InputRecords, result.OutputRecords)
	}

	return result, outputRecords, nil
}

// calculateStats 통계 계산
func (p *Previewer) calculateStats(result PreviewResult) PreviewStats {
	stats := PreviewStats{
		TotalInput:  len(result.InputRecords),
		TotalOutput: len(result.OutputRecords),
	}

	if stats.TotalInput > 0 {
		stats.PassRate = float64(stats.TotalOutput) / float64(stats.TotalInput) * 100
		stats.FilterRate = float64(result.FilteredCount) / float64(stats.TotalInput) * 100
		stats.ErrorRate = float64(result.ErrorCount) / float64(stats.TotalInput) * 100
	}

	// 평균 필드 수 계산
	totalFields := 0
	for _, record := range result.OutputRecords {
		totalFields += len(record.Data)
	}
	if stats.TotalOutput > 0 {
		stats.AvgFields = float64(totalFields) / float64(stats.TotalOutput)
	}

	return stats
}

// calculateDiff 입력/출력 비교
func (p *Previewer) calculateDiff(inputs, outputs []PreviewRecord) []RecordDiff {
	diffs := make([]RecordDiff, 0)

	// 입력과 출력 레코드 매핑 (인덱스 기준)
	maxLen := len(inputs)
	if len(outputs) > maxLen {
		maxLen = len(outputs)
	}

	for i := 0; i < maxLen; i++ {
		var diff RecordDiff
		diff.Index = i

		if i >= len(inputs) {
			// 새로 추가된 레코드 (split 등)
			diff.Type = DiffTypeAdded
			for field := range outputs[i].Data {
				diff.AddedFields = append(diff.AddedFields, field)
			}
		} else if i >= len(outputs) {
			// 필터링된 레코드
			diff.Type = DiffTypeRemoved
			for field := range inputs[i].Data {
				diff.RemovedFields = append(diff.RemovedFields, field)
			}
		} else {
			// 비교
			inputData := inputs[i].Data
			outputData := outputs[i].Data
			diff = p.compareRecords(i, inputData, outputData)
		}

		diffs = append(diffs, diff)
	}

	return diffs
}

// compareRecords 두 레코드 비교
func (p *Previewer) compareRecords(index int, input, output map[string]any) RecordDiff {
	diff := RecordDiff{
		Index: index,
		Type:  DiffTypeUnchanged,
	}

	// 추가된 필드
	for field := range output {
		if _, exists := input[field]; !exists {
			diff.AddedFields = append(diff.AddedFields, field)
		}
	}

	// 삭제된 필드
	for field := range input {
		if _, exists := output[field]; !exists {
			diff.RemovedFields = append(diff.RemovedFields, field)
		}
	}

	// 변경된 필드
	for field, inputVal := range input {
		if outputVal, exists := output[field]; exists {
			if !p.valuesEqual(inputVal, outputVal) {
				diff.ModifiedFields = append(diff.ModifiedFields, FieldDiff{
					Field:    field,
					OldValue: inputVal,
					NewValue: outputVal,
				})
			}
		}
	}

	// 타입 결정
	if len(diff.AddedFields) > 0 || len(diff.RemovedFields) > 0 || len(diff.ModifiedFields) > 0 {
		diff.Type = DiffTypeModified
	}

	return diff
}

// valuesEqual 값 비교
func (p *Previewer) valuesEqual(a, b any) bool {
	// JSON 직렬화로 비교 (구조체, 맵 등 복잡한 타입 처리)
	aJSON, err1 := json.Marshal(a)
	bJSON, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return string(aJSON) == string(bJSON)
}

// PreviewSingleStage 단일 Stage 미리보기 (Stage 설정으로)
func PreviewSingleStage(ctx context.Context, stageConfig map[string]any, sampleData []map[string]any, opts PreviewOptions) (*PreviewResult, error) {
	// Stage 타입 확인
	stageType, ok := stageConfig["type"].(string)
	if !ok {
		return nil, fmt.Errorf("stage type is required")
	}

	// Stage 이름
	stageName, _ := stageConfig["name"].(string)
	if stageName == "" {
		stageName = stageType + "_preview"
	}

	// Stage 설정 구성
	cfg := stream.StageConfig{
		Type:   stageType,
		Name:   stageName,
		Config: stageConfig,
	}

	// Condition 추출
	if condition, ok := stageConfig["condition"].(string); ok {
		cfg.Condition = condition
	}

	// Stage 생성
	stage, err := stream.NewStage(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create stage: %w", err)
	}

	// 미리보기 실행
	previewer := NewPreviewer([]stream.Stage{stage}, opts)
	results, err := previewer.Preview(ctx, sampleData)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no preview results")
	}

	return &results[0], nil
}

// QuickPreview 빠른 미리보기 (기본 옵션 사용)
func QuickPreview(ctx context.Context, stages []stream.Stage, sampleData []map[string]any) ([]PreviewResult, error) {
	previewer := NewPreviewer(stages, DefaultPreviewOptions())
	return previewer.Preview(ctx, sampleData)
}
