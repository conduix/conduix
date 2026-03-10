package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// RevisionService stage 변경 히스토리 관리 서비스
type RevisionService struct {
	db *gorm.DB
}

// NewRevisionService RevisionService 생성
func NewRevisionService(db *gorm.DB) *RevisionService {
	return &RevisionService{db: db}
}

// CreateRevisionParams revision 생성 파라미터
type CreateRevisionParams struct {
	PluginID   string
	PluginName string
	Action     string // "create" | "update" | "delete"
	SourceCode string // 원본 소스 (압축 전)
	GoMod      string // go.mod (압축 전, optional)
	SourceHash string
	OldSource  string // 이전 소스 (diff 계산용, optional)
	Message    string // 사용자 메모
	CreatedBy  string
}

// CreateRevision stage 변경 revision 생성
// 소스를 zstd 압축하여 저장하고 글로벌 seq를 부여
func (s *RevisionService) CreateRevision(params *CreateRevisionParams) (*models.StageRevision, error) {
	// 소스 압축
	var sourceData []byte
	if params.SourceCode != "" {
		var err error
		sourceData, err = models.CompressZstd([]byte(params.SourceCode))
		if err != nil {
			return nil, fmt.Errorf("failed to compress source: %w", err)
		}
	}

	// go.mod 압축
	var goModData []byte
	if params.GoMod != "" {
		var err error
		goModData, err = models.CompressZstd([]byte(params.GoMod))
		if err != nil {
			return nil, fmt.Errorf("failed to compress go.mod: %w", err)
		}
	}

	// diff summary 계산
	diffSummary := computeDiffSummary(params.OldSource, params.SourceCode, params.Action)

	revision := &models.StageRevision{
		ID:          uuid.New().String(),
		PluginID:    params.PluginID,
		PluginName:  params.PluginName,
		Action:      params.Action,
		SourceData:  sourceData,
		GoModData:   goModData,
		SourceHash:  params.SourceHash,
		DiffSummary: diffSummary,
		Message:     params.Message,
		CreatedBy:   params.CreatedBy,
		CreatedAt:   time.Now(),
	}

	if err := s.db.Create(revision).Error; err != nil {
		return nil, fmt.Errorf("failed to create revision: %w", err)
	}

	return revision, nil
}

// ListRevisions plugin의 revision 히스토리 조회
func (s *RevisionService) ListRevisions(pluginID string, limit int) ([]models.StageRevision, error) {
	if limit <= 0 {
		limit = 50
	}

	var revisions []models.StageRevision
	err := s.db.Where("plugin_id = ?", pluginID).
		Order("seq DESC").
		Limit(limit).
		Find(&revisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list revisions: %w", err)
	}

	return revisions, nil
}

// GetRevision revision 상세 조회 (소스 압축 해제 포함)
func (s *RevisionService) GetRevision(id string) (*models.StageRevision, string, string, error) {
	var revision models.StageRevision
	if err := s.db.First(&revision, "id = ?", id).Error; err != nil {
		return nil, "", "", fmt.Errorf("revision not found: %w", err)
	}

	// 소스 압축 해제
	var sourceCode string
	if len(revision.SourceData) > 0 {
		data, err := models.DecompressZstd(revision.SourceData)
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to decompress source: %w", err)
		}
		sourceCode = string(data)
	}

	// go.mod 압축 해제
	var goMod string
	if len(revision.GoModData) > 0 {
		data, err := models.DecompressZstd(revision.GoModData)
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to decompress go.mod: %w", err)
		}
		goMod = string(data)
	}

	return &revision, sourceCode, goMod, nil
}

// GetLatestSeq 현재 최신 글로벌 seq 번호 반환
func (s *RevisionService) GetLatestSeq() (int, error) {
	var revision models.StageRevision
	err := s.db.Order("seq DESC").First(&revision).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get latest seq: %w", err)
	}
	return revision.Seq, nil
}

// GetRevisionsBySeqRange seq 범위로 revision 조회
func (s *RevisionService) GetRevisionsBySeqRange(fromSeq, toSeq int) ([]models.StageRevision, error) {
	var revisions []models.StageRevision
	err := s.db.Where("seq > ? AND seq <= ?", fromSeq, toSeq).
		Order("seq ASC").
		Find(&revisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get revisions by seq range: %w", err)
	}
	return revisions, nil
}

// computeDiffSummary 변경 요약 계산
func computeDiffSummary(oldSource, newSource, action string) string {
	switch action {
	case "create":
		lines := countLines(newSource)
		return fmt.Sprintf("+%d lines (new)", lines)
	case "delete":
		lines := countLines(oldSource)
		return fmt.Sprintf("-%d lines (deleted)", lines)
	case "update":
		if oldSource == "" {
			lines := countLines(newSource)
			return fmt.Sprintf("+%d lines", lines)
		}
		oldLines := strings.Split(oldSource, "\n")
		newLines := strings.Split(newSource, "\n")
		added := 0
		removed := 0
		// 간단한 라인 기반 diff (정확한 diff가 아닌 근사치)
		oldSet := make(map[string]int)
		for _, line := range oldLines {
			oldSet[line]++
		}
		newSet := make(map[string]int)
		for _, line := range newLines {
			newSet[line]++
		}
		for line, count := range newSet {
			if oldCount, ok := oldSet[line]; ok {
				if count > oldCount {
					added += count - oldCount
				}
			} else {
				added += count
			}
		}
		for line, count := range oldSet {
			if newCount, ok := newSet[line]; ok {
				if count > newCount {
					removed += count - newCount
				}
			} else {
				removed += count
			}
		}
		return fmt.Sprintf("+%d -%d lines", added, removed)
	default:
		return ""
	}
}

// countLines 텍스트의 줄 수 계산
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
