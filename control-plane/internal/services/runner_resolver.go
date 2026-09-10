package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// DefaultRunnerImage 기본 pipeline-runner 이미지 (native plugin이 없는 경우)
const DefaultRunnerImage = "ghcr.io/conduix/pipeline-runner:latest"

// BuildRequiredReason 은 빌드가 필요해진 이유다.
// 이유마다 사용자가 확인할 곳이 달라서 메시지를 구분해야 한다 — stage 수정이면 그 stage 를,
// 코어 변경이면 배포 버전을, ready 버전 부재면 빌드 이력을 봐야 한다.
type BuildRequiredReason string

const (
	// BuildReasonPluginChanged stage 소스가 배포본과 다르다.
	BuildReasonPluginChanged BuildRequiredReason = "plugin_changed"
	// BuildReasonNoReadyVersion 쓸 수 있는 ready 버전이 없다(최초 빌드 전, 또는 전부 실패).
	BuildReasonNoReadyVersion BuildRequiredReason = "no_ready_version"
	// BuildReasonBinaryMissing ready 지만 바이너리가 비어 실행 불가하다.
	BuildReasonBinaryMissing BuildRequiredReason = "binary_missing"
	// BuildReasonCoreChanged stage 는 그대로인데 코어 모듈이 바뀌었다.
	BuildReasonCoreChanged BuildRequiredReason = "core_changed"
)

// BuildRequiredError native plugin의 빌드가 필요할 때 반환하는 에러
type BuildRequiredError struct {
	Reason             BuildRequiredReason `json:"reason"`
	PendingPlugins     []models.Plugin     `json:"pending_plugins"`
	LatestReadyVersion string              `json:"latest_ready_version,omitempty"`
	LatestReadySeq     int                 `json:"latest_ready_seq"` // 최신 ready runner의 revision seq
	LatestSeq          int                 `json:"latest_seq"`       // 현재 최신 revision seq
}

func (e *BuildRequiredError) Error() string {
	switch e.Reason {
	case BuildReasonNoReadyVersion:
		return "실행 가능한 runner 빌드가 없습니다. Stage 화면에서 빌드한 뒤 실행해주세요."
	case BuildReasonBinaryMissing:
		return fmt.Sprintf("runner %s 는 ready 지만 바이너리가 없어 실행할 수 없습니다. 다시 빌드해주세요.",
			e.LatestReadyVersion)
	case BuildReasonCoreChanged:
		// stage 소스는 그대로이므로 stage 이름을 나열하면 오히려 혼란스럽다.
		return fmt.Sprintf("conduix 코어가 업데이트되어 현재 runner(%s)가 낡았습니다. 다시 빌드한 뒤 실행해주세요.",
			e.LatestReadyVersion)
	default:
		names := make([]string, len(e.PendingPlugins))
		for i, p := range e.PendingPlugins {
			names[i] = p.Name
		}
		return fmt.Sprintf("stage [%s]가 seq #%d에서 수정되었습니다. 현재 runner는 seq #%d 기준 빌드입니다. 빌드 후 실행해주세요.",
			strings.Join(names, ", "), e.LatestSeq, e.LatestReadySeq)
	}
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
		Reason:             BuildReasonPluginChanged,
		PendingPlugins:     pendingPlugins,
		LatestReadyVersion: latestReady,
		LatestReadySeq:     latestReadySeq,
		LatestSeq:          latestSeq,
	}
}

// ResolveRunnerVersion 은 실행에 쓸 RunnerVersion 을 결정한다(바이너리 주입 경로용).
// 반환: versionID(native 이고 ready 바이너리 있을 때만 non-empty), image(base/기본 이미지), usesNative(native plugin 사용 여부), err.
// - native 없음 → ("", DefaultRunnerImage, false, nil)
// - native 있고 ready(바이너리 보유) → (v.ID, v.ImageTag, true, nil)
// - 변경분 있어 빌드 필요 → ("", "", true, *BuildRequiredError)
// - native 있으나 ready 바이너리 없음 → BuildRequiredError(바이너리 없는 ready 는 실행 불가 — 함정 회피)
func (r *RunnerResolver) ResolveRunnerVersion(workflow *models.Workflow) (string, string, bool, error) {
	// 빌드는 워크플로 단위가 아니라 "활성 native plugin 전체" 를 한 바이너리로 만든다
	// (runner_builder.go). 따라서 pending/코어변경 판정도 같은 집합으로 해야 한다 —
	// 워크플로가 참조하는 플러그인만 보면 CombinedSourceHash 가 빌더와 달라져
	// 항상 coreChanged 로 오판한다.
	nativePlugins, err := r.findAllActiveNativePlugins()
	if err != nil {
		return "", "", false, fmt.Errorf("failed to find native plugins: %w", err)
	}
	// native plugin 이 없어도 최신 ready 바이너리로 실행한다.
	// 예전에는 여기서 DefaultRunnerImage 로 조기 반환했는데, 그러면 이미지에 구워진
	// 낡은 바이너리로 돌아 코어(pipeline-core/shared/plugin-sdk) 수정이 영영 반영되지
	// 않았다(실측: initContainer 미주입 → 이미지 ENTRYPOINT 실행). 실행 이력에도
	// runner_version_id 가 남지 않아 무엇으로 돌았는지 추적할 수 없었다.
	// nativePlugins 는 이제 "무엇이 pending 인지" 판정에만 쓰인다.

	var pendingPlugins []models.Plugin
	for _, p := range nativePlugins {
		if p.SourceHash != p.DeployedHash {
			pendingPlugins = append(pendingPlugins, p)
		}
	}

	buildRequired := func(reason BuildRequiredReason) error {
		return &BuildRequiredError{
			Reason:             reason,
			PendingPlugins:     pendingPlugins,
			LatestReadyVersion: r.getLatestReadyVersionID(),
			LatestReadySeq:     r.getLatestReadyVersionSeq(),
			LatestSeq:          r.getLatestRevisionSeq(),
		}
	}

	if len(pendingPlugins) > 0 {
		return "", "", true, buildRequired(BuildReasonPluginChanged)
	}

	latestReady, err := r.getLatestReadyVersion()
	if err != nil {
		return "", "", true, buildRequired(BuildReasonNoReadyVersion)
	}
	// ready 지만 바이너리가 없는 버전은 실행 불가 → 빌드 필요로 유도(함정 #3 회피).
	// Binary 를 읽지 않았으므로 BinarySize 로 판정한다(빌더가 저장 시 함께 기록한다).
	if latestReady.BinarySize == 0 {
		return "", "", true, buildRequired(BuildReasonBinaryMissing)
	}
	// plugin 해시가 같아도 코어(pipeline-runner/pipeline-core/shared/plugin-sdk)가 바뀌면
	// 그 버전의 바이너리는 낡았다. 빌더는 이걸 combinedHash 로 감지해 재빌드하는데,
	// 리졸버가 안 보면 옛 바이너리로 계속 실행돼 코어 수정이 영구히 반영되지 않는다.
	if r.coreChangedSince(latestReady, nativePlugins) {
		return "", "", true, buildRequired(BuildReasonCoreChanged)
	}
	return latestReady.ID, latestReady.ImageTag, true, nil
}

// coreChangedSince 는 해당 버전이 빌드된 뒤 코어 소스가 바뀌었는지 본다.
// 판정은 빌더와 같은 CombinedSourceHash 로 한다 — 두 곳이 다른 식으로 계산하면
// 빌더는 재빌드하는데 리졸버는 옛 버전을 유효하다고 해서 서로 어긋난다.
// SourceRoot 를 못 읽는 환경(소스 미포함 이미지)에서는 코어 해시가 빈 문자열이 되므로
// 판정을 건너뛴다 — 재빌드를 강요해 실행을 막는 쪽보다 현행 유지가 안전하다.
func (r *RunnerResolver) coreChangedSince(version *models.RunnerVersion, plugins []models.Plugin) bool {
	coreHash := builder.CoreSourceHash(builder.SourceRootFromEnv(), nil)
	if coreHash == "" {
		return false
	}

	pluginHashes := make(map[string]string, len(plugins))
	for _, p := range plugins {
		pluginHashes[p.ID] = p.SourceHash
	}
	return builder.CombinedSourceHash(pluginHashes, coreHash) != version.SourceHash
}

// findNativePluginsInWorkflow 워크플로우의 파이프라인 설정에서 native plugin stage를 찾아 해당 Plugin 모델을 반환
// findAllActiveNativePlugins 는 빌드 대상과 같은 집합을 돌려준다.
// 빌더(runner_builder.go)가 type=native AND status=active 전체를 컴파일하므로,
// 리졸버의 해시 판정도 같은 기준이어야 한다.
func (r *RunnerResolver) findAllActiveNativePlugins() ([]models.Plugin, error) {
	var plugins []models.Plugin
	if err := r.db.Where("type = ? AND status = ?", "native", "active").Find(&plugins).Error; err != nil {
		return nil, err
	}
	return plugins, nil
}

func (r *RunnerResolver) findNativePluginsInWorkflow(workflow *models.Workflow) ([]models.Plugin, error) {
	if workflow.PipelinesConfig == "" {
		return nil, nil
	}

	// PipelinesConfig에서 stage type 추출
	stageTypes := extractStageTypes(workflow.PipelinesConfig)
	if len(stageTypes) == 0 {
		return nil, nil
	}

	// stage type = plugin name으로 직접 매칭하여 native plugin 조회
	var plugins []models.Plugin
	if err := r.db.Where("name IN ? AND type = ?", stageTypes, "native").Find(&plugins).Error; err != nil {
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
	// Binary(32MB급)를 읽지 않는다 — 여기서는 존재 여부만 필요하고, 그것은 BinarySize 로
	// 판정한다. 워크플로우 시작마다 32MB 를 읽어 len() 만 확인하던 낭비를 없앤다.
	err := r.db.Select(models.RunnerVersionMetaColumns()).
		Where("status = ?", "ready").
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

// getPendingStageTypes 변경된 plugin들의 stage type(= plugin name) 목록을 map으로 반환
func (r *RunnerResolver) getPendingStageTypes(pendingPlugins []models.Plugin) map[string]bool {
	result := make(map[string]bool)
	for _, p := range pendingPlugins {
		result[p.Name] = true
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
