package executor

import (
	"fmt"

	"github.com/conduix/conduix/shared/types"
)

// ValidationIssue 는 dry-run validation 에서 발견된 문제 1건.
type ValidationIssue struct {
	PipelineID string `json:"pipeline_id"`
	Stage      string `json:"stage"`            // 실패한 stage/prestage 이름
	Output     string `json:"output,omitempty"` // prestage 면 소속 output
	SampleIdx  int    `json:"sample_index"`     // 몇 번째 샘플에서
	Message    string `json:"message"`
}

// ValidationReport 는 샘플 기반 dry-run 결과. Pass=true 여야 (DDL 후) 파이프라인 재개 가능.
type ValidationReport struct {
	Pass   bool              `json:"pass"`
	Issues []ValidationIssue `json:"issues,omitempty"`
}

// ValidatePipelines 는 소스 샘플 N건을 실제 적재 없이 stage 파이프라인에 태워(dry-run),
// 현재 파이프라인 설정이 그 데이터에서 동작 가능한지 검증한다(R3 재개 게이트).
// DDL(스키마 변경) 후, 운영자가 설정을 고치고 이 검증을 통과해야 재개하도록 쓴다.
// samples: pipelineID -> 샘플 레코드들(소스에서 앞부분 몇 건). 싱크 write 는 하지 않는다.
func (e *GroupExecutor) ValidatePipelines(samples map[string][]map[string]any) *ValidationReport {
	report := &ValidationReport{Pass: true}

	for _, pipeline := range e.group.Pipelines {
		recs := samples[pipeline.ID]
		if len(recs) == 0 {
			continue // 샘플 없으면 검증 대상 아님
		}
		for i, sample := range recs {
			issues := e.validateSample(pipeline, sample, i)
			if len(issues) > 0 {
				report.Pass = false
				report.Issues = append(report.Issues, issues...)
			}
		}
	}
	return report
}

// validateSample 은 샘플 1건을 공통 Stage → Output별 PreStages 순으로 dry-run 한다.
func (e *GroupExecutor) validateSample(pipeline types.GroupedPipeline, sample map[string]any, idx int) []ValidationIssue {
	var issues []ValidationIssue

	// 공통 Stage. 필터링(nil 반환)은 정상이므로 통과로 본다(에러만 문제).
	data := sample
	for _, stage := range pipeline.Stages {
		if isOutputType(stage.Type) {
			continue
		}
		out, err := e.applyStage(data, stage)
		if err != nil {
			issues = append(issues, ValidationIssue{
				PipelineID: pipeline.ID, Stage: stage.Name, SampleIdx: idx,
				Message: fmt.Sprintf("stage %q(%s) failed: %v", stage.Name, stage.Type, err),
			})
			return issues // 이 샘플은 공통 stage 에서 막힘 → output 검증 무의미
		}
		if out == nil {
			return issues // 필터됨(정상)
		}
		data = out
	}

	// Output별 PreStages.
	for _, out := range pipeline.Outputs {
		od := data
		for _, pre := range out.PreStages {
			transformed, err := e.applyStage(od, pre)
			if err != nil {
				issues = append(issues, ValidationIssue{
					PipelineID: pipeline.ID, Stage: pre.Name, Output: out.Name, SampleIdx: idx,
					Message: fmt.Sprintf("output %q prestage %q(%s) failed: %v", out.Name, pre.Name, pre.Type, err),
				})
				break
			}
			if transformed == nil {
				break // 필터됨(정상)
			}
			od = transformed
		}
	}
	return issues
}
