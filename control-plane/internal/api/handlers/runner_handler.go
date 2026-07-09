package handlers

import (
	"context"
	"fmt"
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
	db              *database.DB
	resolver        *services.RunnerResolver
	builder         *builder.RunnerBuilder
	revisionService *services.RevisionService
}

// NewRunnerHandler RunnerHandler 생성
func NewRunnerHandler(db *database.DB) *RunnerHandler {
	return &RunnerHandler{
		db:              db,
		resolver:        services.NewRunnerResolver(db.DB),
		builder:         builder.NewRunnerBuilder(db.DB, nil),
		revisionService: services.NewRevisionService(db.DB),
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

	// 현재 최신 revision seq 가져오기
	latestSeq, _ := h.revisionService.GetLatestSeq()

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

		// revision_seq, trigger 업데이트
		h.db.Model(&models.RunnerVersion{}).Where("id = ?", result.VersionID).Updates(map[string]any{
			"revision_seq": latestSeq,
			"trigger":      "manual",
		})

		slog.Info("runner build completed", "version_id", result.VersionID, "status", result.Status, "duration", result.Duration, "revision_seq", latestSeq)
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
				"message":              buildErr.Error(),
				"pending_plugins":      buildErr.PendingPlugins,
				"latest_ready_version": buildErr.LatestReadyVersion,
				"latest_ready_seq":     buildErr.LatestReadySeq,
				"latest_seq":           buildErr.LatestSeq,
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

// RebuildVersion POST /api/v1/runner/rebuild/:id — 기존 버전 기반 재빌드
func (h *RunnerHandler) RebuildVersion(c *gin.Context) {
	parentID := c.Param("id")
	userID := c.GetString("user_id")

	// 원본 버전 확인
	var parentVersion models.RunnerVersion
	if err := h.db.First(&parentVersion, "id = ?", parentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "runner version not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 현재 최신 revision seq 가져오기
	latestSeq, _ := h.revisionService.GetLatestSeq()

	// 비동기 재빌드 시작
	go func() {
		result, err := h.builder.Build(context.Background(), userID)
		if err != nil {
			versionID := ""
			if result != nil {
				versionID = result.VersionID
			}
			slog.Error("runner rebuild failed", "error", err, "parent_id", parentID, "version_id", versionID)
			return
		}

		// revision_seq, trigger, parent_id 업데이트
		h.db.Model(&models.RunnerVersion{}).Where("id = ?", result.VersionID).Updates(map[string]any{
			"revision_seq": latestSeq,
			"trigger":      "rebuild",
			"parent_id":    parentID,
		})

		slog.Info("runner rebuild completed",
			"version_id", result.VersionID,
			"parent_id", parentID,
			"revision_seq", latestSeq,
		)
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"success":   true,
		"message":   fmt.Sprintf("rebuild started (parent: %s)", parentID),
		"parent_id": parentID,
	})
}

// DownloadBinary GET /api/v1/runner/versions/:id/binary — gzip 압축된 runner 바이너리 스트리밍.
// batch Job initContainer 가 인증 없이 받아 압축 해제 후 실행한다(레지스트리 push 없는 native stage 전달).
func (h *RunnerHandler) DownloadBinary(c *gin.Context) {
	id := c.Param("id")
	var v models.RunnerVersion
	if err := h.db.Select("id", "status", "binary").First(&v, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "runner version not found"})
		return
	}
	if v.Status != "ready" || len(v.Binary) == 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": fmt.Sprintf("runner version %s not ready or has no binary (status=%s)", id, v.Status)})
		return
	}
	c.Data(http.StatusOK, "application/gzip", v.Binary)
}
