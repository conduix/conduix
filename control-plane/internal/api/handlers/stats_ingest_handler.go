package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

// StatsIngestHandler 는 실행 pod 가 보내는 시간 버킷 통계를 받아 upsert 한다.
// realtime 은 종료 결과 콜백이 없으므로(무한 실행) 이 경로가 유일한 통계 수집 수단이다.
type StatsIngestHandler struct {
	db     *database.DB
	logger *slog.Logger
}

func NewStatsIngestHandler(db *database.DB) *StatsIngestHandler {
	return &StatsIngestHandler{db: db, logger: slog.Default()}
}

// IngestHourlyStats POST /api/v1/internal/stats/hourly
// 실행 pod(pipeline-runner)가 시간 경계마다 버킷을 보낸다. 인증 없는 internal 경로다 —
// runner 는 클러스터 내부에서만 호출하고, 결과 콜백(/internal/job-result)과 같은 신뢰 모델이다.
func (h *StatsIngestHandler) IngestHourlyStats(c *gin.Context) {
	var bucket types.HourlyStatsBucket
	if err := c.ShouldBindJSON(&bucket); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_PAYLOAD", "message": err.Error()},
		})
		return
	}
	if bucket.PipelineID == "" || bucket.BucketHour.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_PAYLOAD", "message": "pipeline_id and bucket_hour are required"},
		})
		return
	}

	if err := h.upsertBucket(&bucket); err != nil {
		h.logger.Error("failed to upsert hourly stats",
			"pipeline_id", bucket.PipelineID, "bucket_hour", bucket.BucketHour, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "UPSERT_FAILED", "message": "failed to store stats"},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// upsertBucket 은 (pipeline_id, bucket_hour) 유니크 키로 누적한다.
// 같은 버킷을 여러 pod(파티션 분산)이 보내므로 덮어쓰기가 아니라 더하기여야 한다 —
// 덮어쓰면 마지막 pod 의 수치만 남아 분산 실행의 합계가 사라진다.
func (h *StatsIngestHandler) upsertBucket(b *types.HourlyStatsBucket) error {
	perStage := "{}"
	if len(b.PerStageCounts) > 0 {
		if raw, err := json.Marshal(b.PerStageCounts); err == nil {
			perStage = string(raw)
		}
	}

	row := models.PipelineHourlyStats{
		ID:               uuid.New().String(),
		PipelineID:       b.PipelineID,
		PipelineName:     b.PipelineName,
		WorkflowID:       b.WorkflowID,
		BucketHour:       b.BucketHour.Truncate(time.Hour),
		RecordsCollected: b.RecordsCollected,
		RecordsProcessed: b.RecordsProcessed,
		PerStageCounts:   perStage,
		CollectionErrors: b.CollectionErrors,
		ProcessingErrors: b.ProcessingErrors,
		SampleCount:      b.SampleCount,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	return h.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "pipeline_id"}, {Name: "bucket_hour"}},
		DoUpdates: clause.Assignments(map[string]any{
			"records_collected": gorm.Expr("records_collected + ?", b.RecordsCollected),
			"records_processed": gorm.Expr("records_processed + ?", b.RecordsProcessed),
			"collection_errors": gorm.Expr("collection_errors + ?", b.CollectionErrors),
			"processing_errors": gorm.Expr("processing_errors + ?", b.ProcessingErrors),
			"sample_count":      gorm.Expr("sample_count + ?", b.SampleCount),
			"per_stage_counts":  perStage,
			"updated_at":        time.Now(),
		}),
	}).Create(&row).Error
}
