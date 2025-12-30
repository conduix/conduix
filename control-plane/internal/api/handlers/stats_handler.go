package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// StatsHandler 통계 API 핸들러
type StatsHandler struct {
	db *database.DB
}

// NewStatsHandler 새 통계 핸들러 생성
func NewStatsHandler(db *database.DB) *StatsHandler {
	return &StatsHandler{db: db}
}

// GetPipelineStats GET /api/v1/stats/pipelines/:id
// 파이프라인별 통계 조회
func (h *StatsHandler) GetPipelineStats(c *gin.Context) {
	pipelineID := c.Param("id")
	groupType := c.Query("type") // "batch" or "realtime"

	if groupType == "realtime" {
		h.getRealtimePipelineStats(c, pipelineID)
	} else {
		h.getBatchPipelineStats(c, pipelineID)
	}
}

// getBatchPipelineStats 배치 파이프라인 통계 조회
func (h *StatsHandler) getBatchPipelineStats(c *gin.Context, pipelineID string) {
	var stats []models.PipelineExecutionStats

	query := h.db.Where("pipeline_id = ?", pipelineID).Order("started_at DESC")

	// 시간 범위 필터
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("started_at >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("started_at <= ?", t)
		}
	}

	// 기본 제한
	limit := 50
	query = query.Limit(limit)

	if err := query.Find(&stats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch pipeline stats",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.PipelineExecutionStats]{
		Success: true,
		Data:    stats,
	})
}

// getRealtimePipelineStats 실시간 파이프라인 통계 조회
func (h *StatsHandler) getRealtimePipelineStats(c *gin.Context, pipelineID string) {
	var stats []models.PipelineHourlyStats

	query := h.db.Where("pipeline_id = ?", pipelineID).Order("bucket_hour DESC")

	// 시간 범위 필터
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("bucket_hour >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("bucket_hour <= ?", t)
		}
	}

	// 기본값: 최근 24시간
	if c.Query("from") == "" && c.Query("to") == "" {
		query = query.Where("bucket_hour >= ?", time.Now().Add(-24*time.Hour))
	}

	if err := query.Find(&stats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch pipeline stats",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.PipelineHourlyStats]{
		Success: true,
		Data:    stats,
	})
}

// GetWorkflowStats GET /api/v1/stats/workflows/:id
// 워크플로우 집계 통계 조회
func (h *StatsHandler) GetWorkflowStats(c *gin.Context) {
	workflowID := c.Param("id")

	// 워크플로우 정보 조회
	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		c.JSON(http.StatusNotFound, types.APIResponse[any]{
			Success: false,
			Error:   "Workflow not found",
		})
		return
	}

	if workflow.Type == "realtime" {
		h.getRealtimeWorkflowStats(c, workflowID, workflow.Name)
	} else {
		h.getBatchWorkflowStats(c, workflowID, workflow.Name)
	}
}

// getBatchWorkflowStats 배치 워크플로우 통계 조회
func (h *StatsHandler) getBatchWorkflowStats(c *gin.Context, workflowID, workflowName string) {
	// 시간 범위 파싱
	var fromTime, toTime time.Time
	if from := c.Query("from"); from != "" {
		fromTime, _ = time.Parse(time.RFC3339, from)
	} else {
		fromTime = time.Now().AddDate(0, 0, -7) // 기본: 최근 7일
	}
	if to := c.Query("to"); to != "" {
		toTime, _ = time.Parse(time.RFC3339, to)
	} else {
		toTime = time.Now()
	}

	// 집계 쿼리
	var result struct {
		TotalCollected        int64 `gorm:"column:total_collected"`
		TotalProcessed        int64 `gorm:"column:total_processed"`
		TotalCollectionErrors int64 `gorm:"column:total_collection_errors"`
		TotalProcessingErrors int64 `gorm:"column:total_processing_errors"`
	}

	h.db.Model(&models.PipelineExecutionStats{}).
		Where("workflow_id = ? AND started_at BETWEEN ? AND ?", workflowID, fromTime, toTime).
		Select(`
			COALESCE(SUM(records_collected), 0) as total_collected,
			COALESCE(SUM(records_processed), 0) as total_processed,
			COALESCE(SUM(collection_errors), 0) as total_collection_errors,
			COALESCE(SUM(processing_errors), 0) as total_processing_errors
		`).Scan(&result)

	// 파이프라인별 상세
	var pipelineStats []models.PipelineExecutionStats
	h.db.Where("workflow_id = ? AND started_at BETWEEN ? AND ?", workflowID, fromTime, toTime).
		Order("started_at DESC").
		Limit(100).
		Find(&pipelineStats)

	// PipelineStatistics 변환
	stats := make([]types.PipelineStatistics, 0, len(pipelineStats))
	for _, ps := range pipelineStats {
		stageCounts := make(map[string]int64)
		if ps.PerStageCounts != "" {
			_ = json.Unmarshal([]byte(ps.PerStageCounts), &stageCounts)
		}

		stats = append(stats, types.PipelineStatistics{
			PipelineID:       ps.PipelineID,
			PipelineName:     ps.PipelineName,
			RecordsCollected: ps.RecordsCollected,
			RecordsProcessed: ps.RecordsProcessed,
			PerStageCounts:   stageCounts,
			CollectionErrors: ps.CollectionErrors,
			ProcessingErrors: ps.ProcessingErrors,
			StartedAt:        ps.StartedAt,
			CompletedAt:      ps.CompletedAt,
			DurationMs:       ps.DurationMs,
		})
	}

	response := types.WorkflowStatistics{
		WorkflowID:            workflowID,
		WorkflowName:          workflowName,
		WorkflowType:          types.WorkflowTypeBatch,
		TotalRecordsCollected: result.TotalCollected,
		TotalRecordsProcessed: result.TotalProcessed,
		TotalCollectionErrors: result.TotalCollectionErrors,
		TotalProcessingErrors: result.TotalProcessingErrors,
		PipelineStats:         stats,
		Period:                types.StatsPeriodExecution,
		StartTime:             fromTime,
		EndTime:               toTime,
	}

	c.JSON(http.StatusOK, types.APIResponse[types.WorkflowStatistics]{
		Success: true,
		Data:    response,
	})
}

// getRealtimeWorkflowStats 실시간 워크플로우 통계 조회
func (h *StatsHandler) getRealtimeWorkflowStats(c *gin.Context, workflowID, workflowName string) {
	// 시간 범위 파싱
	var fromTime, toTime time.Time
	if from := c.Query("from"); from != "" {
		fromTime, _ = time.Parse(time.RFC3339, from)
	} else {
		fromTime = time.Now().Add(-24 * time.Hour) // 기본: 최근 24시간
	}
	if to := c.Query("to"); to != "" {
		toTime, _ = time.Parse(time.RFC3339, to)
	} else {
		toTime = time.Now()
	}

	// 집계 쿼리
	var result struct {
		TotalCollected        int64 `gorm:"column:total_collected"`
		TotalProcessed        int64 `gorm:"column:total_processed"`
		TotalCollectionErrors int64 `gorm:"column:total_collection_errors"`
		TotalProcessingErrors int64 `gorm:"column:total_processing_errors"`
	}

	h.db.Model(&models.PipelineHourlyStats{}).
		Where("workflow_id = ? AND bucket_hour BETWEEN ? AND ?", workflowID, fromTime, toTime).
		Select(`
			COALESCE(SUM(records_collected), 0) as total_collected,
			COALESCE(SUM(records_processed), 0) as total_processed,
			COALESCE(SUM(collection_errors), 0) as total_collection_errors,
			COALESCE(SUM(processing_errors), 0) as total_processing_errors
		`).Scan(&result)

	// 시간별 상세
	var hourlyStats []models.PipelineHourlyStats
	h.db.Where("workflow_id = ? AND bucket_hour BETWEEN ? AND ?", workflowID, fromTime, toTime).
		Order("bucket_hour DESC").
		Find(&hourlyStats)

	// 파이프라인별 집계
	pipelineMap := make(map[string]*types.PipelineStatistics)
	for _, hs := range hourlyStats {
		if _, ok := pipelineMap[hs.PipelineID]; !ok {
			pipelineMap[hs.PipelineID] = &types.PipelineStatistics{
				PipelineID:     hs.PipelineID,
				PipelineName:   hs.PipelineName,
				PerStageCounts: make(map[string]int64),
				StartedAt:      fromTime,
			}
		}
		ps := pipelineMap[hs.PipelineID]
		ps.RecordsCollected += hs.RecordsCollected
		ps.RecordsProcessed += hs.RecordsProcessed
		ps.CollectionErrors += hs.CollectionErrors
		ps.ProcessingErrors += hs.ProcessingErrors

		// Stage counts 병합
		if hs.PerStageCounts != "" {
			var counts map[string]int64
			if err := json.Unmarshal([]byte(hs.PerStageCounts), &counts); err == nil {
				for k, v := range counts {
					ps.PerStageCounts[k] += v
				}
			}
		}
	}

	stats := make([]types.PipelineStatistics, 0, len(pipelineMap))
	for _, ps := range pipelineMap {
		now := time.Now()
		ps.CompletedAt = &now
		stats = append(stats, *ps)
	}

	response := types.WorkflowStatistics{
		WorkflowID:            workflowID,
		WorkflowName:          workflowName,
		WorkflowType:          types.WorkflowTypeRealtime,
		TotalRecordsCollected: result.TotalCollected,
		TotalRecordsProcessed: result.TotalProcessed,
		TotalCollectionErrors: result.TotalCollectionErrors,
		TotalProcessingErrors: result.TotalProcessingErrors,
		PipelineStats:         stats,
		Period:                types.StatsPeriodHourly,
		StartTime:             fromTime,
		EndTime:               toTime,
	}

	c.JSON(http.StatusOK, types.APIResponse[types.WorkflowStatistics]{
		Success: true,
		Data:    response,
	})
}

// GetExecutionStats GET /api/v1/stats/executions/:id
// 특정 실행의 상세 통계 조회
func (h *StatsHandler) GetExecutionStats(c *gin.Context) {
	executionID := c.Param("id")

	var stats []models.PipelineExecutionStats
	if err := h.db.Where("execution_id = ?", executionID).Find(&stats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, types.APIResponse[any]{
			Success: false,
			Error:   "Failed to fetch execution stats",
		})
		return
	}

	c.JSON(http.StatusOK, types.APIResponse[[]models.PipelineExecutionStats]{
		Success: true,
		Data:    stats,
	})
}
