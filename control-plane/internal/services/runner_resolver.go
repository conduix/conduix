package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// DefaultRunnerImage 기본 pipeline-runner 이미지 (native plugin이 없는 경우)
const DefaultRunnerImage = "ghcr.io/conduix/pipeline-runner:latest"

// BuildRequiredError native plugin의 빌드가 필요할 때 반환하는 에러
type BuildRequiredError struct {
	PendingPlugins     []models.Plugin `json:"pending_plugins"`
	LatestReadyVersion string          `json:"latest_ready_version,omitempty"`
}

func (e *BuildRequiredError) Error() string {
	names := make([]string, len(e.PendingPlugins))
	for i, p := range e.PendingPlugins {
		names[i] = p.Name
	}
	return fmt.Sprintf("runner build required for plugins: %s", strings.Join(names, ", "))
}

// RunnerResolver 워크플로우 실행 시 Runner 이미지를 결정하는 서비스
type RunnerResolver struct {
	db *gorm.DB
}

// NewRunnerResolver RunnerResolver 생성
func NewRunnerResolver(db *gorm.DB) *RunnerResolver {
	return &RunnerResolver{db: db}
}

// ResolveRunnerImage 워크플로우의 파이프라인 설정을 분석하여 Runner 이미지를 결정
// - native plugin이 없으면 → 기본 runner 이미지
// - native plugin이 있고 모두 배포 완료 → 최신 ready RunnerVersion 이미지
// - native plugin이 있고 빌드 필요 → BuildRequiredError 반환
func (r *RunnerResolver) ResolveRunnerImage(workflow *models.Workflow) (string, error) {
	nativePlugins, err := r.findNativePluginsInWorkflow(workflow)
	if err != nil {
		return "", fmt.Errorf("failed to find native plugins: %w", err)
	}

	if len(nativePlugins) == 0 {
		return DefaultRunnerImage, nil
	}

	// 모든 native plugin이 배포 완료 상태인지 확인
	var pendingList []models.Plugin
	for _, p := range nativePlugins {
		if p.SourceHash != p.DeployedHash {
			pendingList = append(pendingList, p)
		}
	}

	if len(pendingList) > 0 {
		latestReady := r.getLatestReadyVersionID()
		return "", &BuildRequiredError{
			PendingPlugins:     pendingList,
			LatestReadyVersion: latestReady,
		}
	}

	// 최신 ready RunnerVersion의 이미지 사용
	latestReady, err := r.getLatestReadyVersion()
	if err != nil {
		return "", fmt.Errorf("no ready runner version found: %w", err)
	}
	return latestReady.ImageTag, nil
}

// findNativePluginsInWorkflow 워크플로우의 파이프라인 설정에서 native plugin stage를 찾아 해당 Plugin 모델을 반환
func (r *RunnerResolver) findNativePluginsInWorkflow(workflow *models.Workflow) ([]models.Plugin, error) {
	if workflow.PipelinesConfig == "" {
		return nil, nil
	}

	// PipelinesConfig에서 stage type 추출
	stageTypes := extractStageTypes(workflow.PipelinesConfig)
	if len(stageTypes) == 0 {
		return nil, nil
	}

	// PluginStage 테이블에서 해당 stage type의 native plugin 조회
	var pluginStages []models.PluginStage
	if err := r.db.Where("stage_type IN ?", stageTypes).Find(&pluginStages).Error; err != nil {
		return nil, err
	}

	if len(pluginStages) == 0 {
		return nil, nil
	}

	// plugin ID 수집
	pluginIDs := make([]string, 0, len(pluginStages))
	seen := make(map[string]bool)
	for _, ps := range pluginStages {
		if !seen[ps.PluginID] {
			pluginIDs = append(pluginIDs, ps.PluginID)
			seen[ps.PluginID] = true
		}
	}

	// native 타입 플러그인만 조회
	var plugins []models.Plugin
	if err := r.db.Where("id IN ? AND type = ?", pluginIDs, "native").Find(&plugins).Error; err != nil {
		return nil, err
	}

	return plugins, nil
}

// extractStageTypes PipelinesConfig JSON에서 모든 stage type을 추출
func extractStageTypes(pipelinesConfig string) []string {
	// PipelinesConfig는 JSON array of pipeline objects
	var pipelines []struct {
		Stages []struct {
			Type string `json:"type"`
		} `json:"stages"`
		Outputs []struct {
			PreStages []struct {
				Type string `json:"type"`
			} `json:"pre_stages"`
		} `json:"outputs"`
	}

	if err := json.Unmarshal([]byte(pipelinesConfig), &pipelines); err != nil {
		return nil
	}

	typeSet := make(map[string]bool)
	for _, p := range pipelines {
		for _, s := range p.Stages {
			if s.Type != "" {
				typeSet[s.Type] = true
			}
		}
		for _, o := range p.Outputs {
			for _, ps := range o.PreStages {
				if ps.Type != "" {
					typeSet[ps.Type] = true
				}
			}
		}
	}

	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	return types
}

// getLatestReadyVersion 최신 ready 상태의 RunnerVersion 반환
func (r *RunnerResolver) getLatestReadyVersion() (*models.RunnerVersion, error) {
	var version models.RunnerVersion
	err := r.db.Where("status = ?", "ready").
		Order("build_number DESC").
		First(&version).Error
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// getLatestReadyVersionID 최신 ready RunnerVersion의 ID 반환 (에러 메시지용)
func (r *RunnerResolver) getLatestReadyVersionID() string {
	var version models.RunnerVersion
	err := r.db.Where("status = ?", "ready").
		Order("build_number DESC").
		First(&version).Error
	if err != nil {
		return ""
	}
	return version.ID
}
