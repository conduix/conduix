package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-core/pkg/preview"
	"github.com/conduix/conduix/pipeline-core/pkg/stream"
)

// PreviewHandler 미리보기 핸들러
type PreviewHandler struct{}

// NewPreviewHandler 미리보기 핸들러 생성
func NewPreviewHandler() *PreviewHandler {
	return &PreviewHandler{}
}

// PreviewRequest 미리보기 요청
type PreviewRequest struct {
	// Stage 설정
	Stage StagePreviewConfig `json:"stage"`
	// 샘플 데이터
	SampleData []map[string]any `json:"sample_data"`
	// 옵션
	Options PreviewOptionsRequest `json:"options,omitempty"`
}

// StagePreviewConfig Stage 설정
type StagePreviewConfig struct {
	Type      string         `json:"type"`
	Name      string         `json:"name,omitempty"`
	Condition string         `json:"condition,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
}

// PreviewOptionsRequest 미리보기 옵션 요청
type PreviewOptionsRequest struct {
	MaxRecords  int  `json:"max_records,omitempty"`
	IncludeDiff bool `json:"include_diff,omitempty"`
	TimeoutSec  int  `json:"timeout_sec,omitempty"`
}

// PipelinePreviewRequest 파이프라인 전체 미리보기 요청
type PipelinePreviewRequest struct {
	// Stage 설정 목록
	Stages []StagePreviewConfig `json:"stages"`
	// 샘플 데이터
	SampleData []map[string]any `json:"sample_data"`
	// 옵션
	Options PreviewOptionsRequest `json:"options,omitempty"`
	// 특정 Stage만 미리보기
	StageNames []string `json:"stage_names,omitempty"`
}

// PreviewErrorResponse 미리보기 에러 응답
type PreviewErrorResponse struct {
	Error string `json:"error"`
}

// PreviewStage 단일 Stage 미리보기
// @Summary Stage 미리보기
// @Description 샘플 데이터로 단일 Stage의 입력/출력을 미리보기
// @Tags Preview
// @Accept json
// @Produce json
// @Param request body PreviewRequest true "미리보기 요청"
// @Success 200 {object} preview.PreviewResult
// @Failure 400 {object} PreviewErrorResponse
// @Router /api/v1/preview/stage [post]
func (h *PreviewHandler) PreviewStage(c *gin.Context) {
	var req PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: err.Error()})
		return
	}

	// Stage 타입 필수
	if req.Stage.Type == "" {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: "stage type is required"})
		return
	}

	// 샘플 데이터 필수
	if len(req.SampleData) == 0 {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: "sample_data is required"})
		return
	}

	// Stage 설정 구성
	stageConfig := make(map[string]any)
	stageConfig["type"] = req.Stage.Type
	if req.Stage.Name != "" {
		stageConfig["name"] = req.Stage.Name
	}
	if req.Stage.Condition != "" {
		stageConfig["condition"] = req.Stage.Condition
	}
	for k, v := range req.Stage.Config {
		stageConfig[k] = v
	}

	// 옵션 변환
	opts := preview.DefaultPreviewOptions()
	if req.Options.MaxRecords > 0 {
		opts.MaxRecords = req.Options.MaxRecords
	}
	if req.Options.TimeoutSec > 0 {
		opts.Timeout = time.Duration(req.Options.TimeoutSec) * time.Second
	}
	opts.IncludeDiff = req.Options.IncludeDiff

	// 미리보기 실행
	result, err := preview.PreviewSingleStage(c.Request.Context(), stageConfig, req.SampleData, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, PreviewErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// PreviewPipeline 파이프라인 전체 미리보기
// @Summary 파이프라인 미리보기
// @Description 샘플 데이터로 파이프라인의 모든 Stage 입력/출력을 미리보기
// @Tags Preview
// @Accept json
// @Produce json
// @Param request body PipelinePreviewRequest true "미리보기 요청"
// @Success 200 {array} preview.PreviewResult
// @Failure 400 {object} PreviewErrorResponse
// @Router /api/v1/preview/pipeline [post]
func (h *PreviewHandler) PreviewPipeline(c *gin.Context) {
	var req PipelinePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: err.Error()})
		return
	}

	// Stage 목록 필수
	if len(req.Stages) == 0 {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: "stages is required"})
		return
	}

	// 샘플 데이터 필수
	if len(req.SampleData) == 0 {
		c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: "sample_data is required"})
		return
	}

	// Stage 목록 생성
	stages := make([]stream.Stage, 0, len(req.Stages))
	for _, stageCfg := range req.Stages {
		if stageCfg.Type == "" {
			c.JSON(http.StatusBadRequest, PreviewErrorResponse{Error: "stage type is required"})
			return
		}

		name := stageCfg.Name
		if name == "" {
			name = stageCfg.Type
		}

		cfg := stream.StageConfig{
			Type:      stageCfg.Type,
			Name:      name,
			Condition: stageCfg.Condition,
			Config:    stageCfg.Config,
		}

		stage, err := stream.NewStage(cfg)
		if err != nil {
			c.JSON(http.StatusBadRequest, PreviewErrorResponse{
				Error: "failed to create stage " + name + ": " + err.Error(),
			})
			return
		}
		stages = append(stages, stage)
	}

	// 옵션 변환
	opts := preview.DefaultPreviewOptions()
	if req.Options.MaxRecords > 0 {
		opts.MaxRecords = req.Options.MaxRecords
	}
	if req.Options.TimeoutSec > 0 {
		opts.Timeout = time.Duration(req.Options.TimeoutSec) * time.Second
	}
	opts.IncludeDiff = req.Options.IncludeDiff
	opts.StageNames = req.StageNames

	// 미리보기 실행
	previewer := preview.NewPreviewer(stages, opts)
	results, err := previewer.Preview(c.Request.Context(), req.SampleData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, PreviewErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, results)
}
