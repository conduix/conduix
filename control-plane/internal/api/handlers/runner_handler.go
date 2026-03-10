package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/internal/services"
	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// RunnerHandler Runner 빌드/버전 관리 핸들러
type RunnerHandler struct {
	db       *database.DB
	resolver *services.RunnerResolver
	builder  *builder.RunnerBuilder
}

// NewRunnerHandler RunnerHandler 생성
func NewRunnerHandler(db *database.DB) *RunnerHandler {
	return &RunnerHandler{
		db:       db,
		resolver: services.NewRunnerResolver(db.DB),
		builder:  builder.NewRunnerBuilder(db.DB, nil),
	}
}

// ListVersions GET /api/v1/runner/versions — RunnerVersion 목록 조회
func (h *RunnerHandler) ListVersions(c *gin.Context) {
	var versions []models.RunnerVersion
	query := h.db.Order("build_number DESC")

	// status 필터
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// limit
	limit := 20
	if err := query.Limit(limit).Find(&versions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    versions,
	})
}

// GetVersion GET /api/v1/runner/versions/:id — RunnerVersion 상세 조회
func (h *RunnerHandler) GetVersion(c *gin.Context) {
	id := c.Param("id")

	var version models.RunnerVersion
	if err := h.db.First(&version, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "runner version not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    version,
	})
}

// CheckStatus GET /api/v1/runner/status — 현재 Runner 배포 상태 확인
// 모든 native plugin의 SourceHash vs DeployedHash 비교
func (h *RunnerHandler) CheckStatus(c *gin.Context) {
	var plugins []models.Plugin
	if err := h.db.Where("type = ?", "native").Find(&plugins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type pluginStatus struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		SourceHash   string `json:"source_hash"`
		DeployedHash string `json:"deployed_hash"`
		NeedsBuild   bool   `json:"needs_build"`
	}

	statuses := make([]pluginStatus, 0, len(plugins))
	needsBuild := false
	for _, p := range plugins {
		nb := p.SourceHash != p.DeployedHash
		if nb {
			needsBuild = true
		}
		statuses = append(statuses, pluginStatus{
			ID:           p.ID,
			Name:         p.Name,
			SourceHash:   p.SourceHash,
			DeployedHash: p.DeployedHash,
			NeedsBuild:   nb,
		})
	}

	// 최신 ready 버전
	var latestReady *models.RunnerVersion
	var rv models.RunnerVersion
	if err := h.db.Where("status = ?", "ready").Order("build_number DESC").First(&rv).Error; err == nil {
		latestReady = &rv
	}

	c.JSON(http.StatusOK, gin.H{
		"success":              true,
		"needs_build":          needsBuild,
		"plugins":              statuses,
		"latest_ready_version": latestReady,
	})
}

// StartBuild POST /api/v1/runner/build — Runner 빌드 시작 (비동기)
func (h *RunnerHandler) StartBuild(c *gin.Context) {
	userID := c.GetString("user_id")

	// 비동기 빌드 시작 (HTTP context와 분리)
	go func() {
		result, err := h.builder.Build(context.Background(), userID)
		if err != nil {
			versionID := ""
			if result != nil {
				versionID = result.VersionID
			}
			slog.Error("runner build failed", "error", err, "version_id", versionID)
			return
		}
		slog.Info("runner build completed", "version_id", result.VersionID, "status", result.Status, "duration", result.Duration)
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "build started",
	})
}

// ResolveImage POST /api/v1/runner/resolve — 워크플로우 실행 시 Runner 이미지 결정
func (h *RunnerHandler) ResolveImage(c *gin.Context) {
	var req struct {
		WorkflowID string `json:"workflow_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var workflow models.Workflow
	if err := h.db.First(&workflow, "id = ?", req.WorkflowID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	image, err := h.resolver.ResolveRunnerImage(&workflow)
	if err != nil {
		if buildErr, ok := err.(*services.BuildRequiredError); ok {
			c.JSON(http.StatusConflict, gin.H{
				"error":                "runner_build_required",
				"message":              "커스텀 stage가 수정되었습니다. 빌드가 필요합니다.",
				"pending_plugins":      buildErr.PendingPlugins,
				"latest_ready_version": buildErr.LatestReadyVersion,
				"action":               "build",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"image":   image,
	})
}
