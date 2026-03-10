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
	LatestReadySeq     int             `json:"latest_ready_seq"`  // 최신 ready runner의 revision seq
	LatestSeq          int             `json:"latest_seq"`        // 현재 최신 revision seq
}

func (e *BuildRequiredError) Error() string {
	names := make([]string, len(e.PendingPlugins))
	for i, p := range e.PendingPlugins {
		names[i] = p.Name
	}
	return fmt.Sprintf("stage [%s]가 seq #%d에서 수정되었습니다. 현재 runner는 seq #%d 기준 빌드입니다. 빌드 후 실행해주세요.",
		strings.Join(names, ", "), e.LatestSeq, e.LatestReadySeq)
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
// 실행 정책:
// - native plugin이 없으면 → 기본 runner 이미지
// - 변경된 stage를 사용하지 않는 pipeline → 최신 ready runner로 실행 가능
// - 변경된 stage를 사용하는 pipeline → 빌드 필요 메시지 반환
func (r *RunnerResolver) ResolveRunnerImage(workflow *models.Workflow) (string, error) {
	nativePlugins, err := r.findNativePluginsInWorkflow(workflow)
	if err != nil {
		return "", fmt.Errorf("failed to find native plugins: %w", err)
	}

	if len(nativePlugins) == 0 {
		return DefaultRunnerImage, nil
	}

	// 변경된(빌드 필요한) native plugin 목록
	var pendingPlugins []models.Plugin
	for _, p := range nativePlugins {
		if p.SourceHash != p.DeployedHash {
			pendingPlugins = append(pendingPlugins, p)
		}
	}

	// 변경 없으면 최신 ready 이미지 사용
	if len(pendingPlugins) == 0 {
		latestReady, err := r.getLatestReadyVersion()
		if err != nil {
			return "", fmt.Errorf("no ready runner version found: %w", err)
		}
		return latestReady.ImageTag, nil
	}

	// 변경된 plugin의 stage type 수집
	pendingStageTypes := r.getPendingStageTypes(pendingPlugins)

	// 워크플로우의 pipeline별로 변경된 stage 사용 여부 확인
	pipelineStageTypes := extractStageTypesPerPipeline(workflow.PipelinesConfig)
	usesModifiedStage := false
	for _, stageTypes := range pipelineStageTypes {
		for _, st := range stageTypes {
			if pendingStageTypes[st] {
				usesModifiedStage = true
				break
			}
		}
		if usesModifiedStage {
			break
		}
	}

	// 변경된 stage를 사용하지 않는 pipeline만 있으면 → 최신 ready runner로 실행 가능
	if !usesModifiedStage {
		latestReady, err := r.getLatestReadyVersion()
		if err != nil {
			return DefaultRunnerImage, nil
		}
		return latestReady.ImageTag, nil
	}

	// 변경된 stage를 사용하는 pipeline이 있으면 → 빌드 필요
	latestReady := r.getLatestReadyVersionID()
	latestReadySeq := r.getLatestReadyVersionSeq()
	latestSeq := r.getLatestRevisionSeq()

	return "", &BuildRequiredError{
		PendingPlugins:     pendingPlugins,
		LatestReadyVersion: latestReady,
		LatestReadySeq:     latestReadySeq,
		LatestSeq:          latestSeq,
	}
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

// getLatestReadyVersionSeq 최신 ready RunnerVersion의 revision seq 반환
func (r *RunnerResolver) getLatestReadyVersionSeq() int {
	var version models.RunnerVersion
	err := r.db.Where("status = ?", "ready").
		Order("build_number DESC").
		First(&version).Error
	if err != nil {
		return 0
	}
	return version.RevisionSeq
}

// getLatestRevisionSeq 현재 최신 글로벌 revision seq 반환
func (r *RunnerResolver) getLatestRevisionSeq() int {
	var revision models.StageRevision
	err := r.db.Order("seq DESC").First(&revision).Error
	if err != nil {
		return 0
	}
	return revision.Seq
}

// getPendingStageTypes 변경된 plugin들의 stage type 목록을 map으로 반환
func (r *RunnerResolver) getPendingStageTypes(pendingPlugins []models.Plugin) map[string]bool {
	pluginIDs := make([]string, len(pendingPlugins))
	for i, p := range pendingPlugins {
		pluginIDs[i] = p.ID
	}

	var pluginStages []models.PluginStage
	r.db.Where("plugin_id IN ?", pluginIDs).Find(&pluginStages)

	result := make(map[string]bool)
	for _, ps := range pluginStages {
		result[ps.StageType] = true
	}
	return result
}

// extractStageTypesPerPipeline PipelinesConfig에서 pipeline별 stage type 목록을 추출
func extractStageTypesPerPipeline(pipelinesConfig string) [][]string {
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

	result := make([][]string, 0, len(pipelines))
	for _, p := range pipelines {
		var types []string
		for _, s := range p.Stages {
			if s.Type != "" {
				types = append(types, s.Type)
			}
		}
		for _, o := range p.Outputs {
			for _, ps := range o.PreStages {
				if ps.Type != "" {
					types = append(types, ps.Type)
				}
			}
		}
		result = append(result, types)
	}
	return result
}
