// Package builder provides RunnerBuilder for building pipeline-runner images
// that include all native plugin stages compiled in-process.
package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// RunnerBuilderConfig Runner 빌드 설정
type RunnerBuilderConfig struct {
	BuildTimeout time.Duration // 빌드 타임아웃 (default: 5분)
	GoProxy      string        // GOPROXY 설정
	ImagePrefix  string        // Docker 이미지 prefix (예: ghcr.io/conduix/pipeline-runner)
	Platform     string        // 빌드 플랫폼 (default: linux/arm64)
	DockerPush   bool          // Docker push 수행 여부
}

// DefaultRunnerBuilderConfig 기본 설정
func DefaultRunnerBuilderConfig() *RunnerBuilderConfig {
	return &RunnerBuilderConfig{
		BuildTimeout: 5 * time.Minute,
		GoProxy:      "https://proxy.golang.org,direct",
		ImagePrefix:  "ghcr.io/conduix/pipeline-runner",
		Platform:     "linux/arm64",
		DockerPush:   false,
	}
}

// RunnerBuilder 모든 native plugin을 포함하는 pipeline-runner 이미지를 빌드
type RunnerBuilder struct {
	config *RunnerBuilderConfig
	db     *gorm.DB
	logger *slog.Logger
}

// NewRunnerBuilder RunnerBuilder 생성
func NewRunnerBuilder(db *gorm.DB, cfg *RunnerBuilderConfig) *RunnerBuilder {
	if cfg == nil {
		cfg = DefaultRunnerBuilderConfig()
	}
	return &RunnerBuilder{
		config: cfg,
		db:     db,
		logger: slog.Default().With("component", "runner-builder"),
	}
}

// RunnerBuildResult 빌드 결과
type RunnerBuildResult struct {
	VersionID  string        `json:"version_id"`
	Status     string        `json:"status"` // ready, failed, skipped
	ImageTag   string        `json:"image_tag,omitempty"`
	BuildLog   string        `json:"build_log"`
	Duration   time.Duration `json:"duration"`
	SkipReason string        `json:"skip_reason,omitempty"`
}

// Build 모든 native plugin을 포함하는 Runner 이미지를 빌드
func (rb *RunnerBuilder) Build(ctx context.Context, createdBy string) (*RunnerBuildResult, error) {
	start := time.Now()
	var logBuf strings.Builder

	// 1. 이미 building 상태인 버전이 있으면 거부
	var buildingCount int64
	rb.db.Model(&models.RunnerVersion{}).Where("status = ?", "building").Count(&buildingCount)
	if buildingCount > 0 {
		return nil, fmt.Errorf("another build is already in progress")
	}

	// 2. 모든 native plugin 조회
	var plugins []models.Plugin
	if err := rb.db.Where("type = ? AND status = ?", "native", "active").Find(&plugins).Error; err != nil {
		return nil, fmt.Errorf("query native plugins: %w", err)
	}

	if len(plugins) == 0 {
		return nil, fmt.Errorf("no native plugins found")
	}

	// 3. 소스 해시 스냅샷 + 결합 해시 계산
	pluginHashes := make(map[string]string)
	for _, p := range plugins {
		pluginHashes[p.ID] = p.SourceHash
	}
	combinedHash := CombinedSourceHash(pluginHashes)

	// 4. 동일 해시 ready 버전이 있으면 빌드 스킵 + DeployedHash만 갱신
	var existing models.RunnerVersion
	if err := rb.db.Where("source_hash = ? AND status = ?", combinedHash, "ready").First(&existing).Error; err == nil {
		rb.updateDeployedHashes(plugins, pluginHashes, existing.ID)
		return &RunnerBuildResult{
			VersionID:  existing.ID,
			Status:     "skipped",
			ImageTag:   existing.ImageTag,
			Duration:   time.Since(start),
			SkipReason: "identical source hash found in existing ready version",
		}, nil
	}

	// 5. 새 RunnerVersion 레코드 생성
	pluginIDsJSON, _ := json.Marshal(extractPluginIDs(plugins))
	pluginHashesJSON, _ := json.Marshal(pluginHashes)

	version := models.RunnerVersion{
		ID:           fmt.Sprintf("rv-%s", uuid.New().String()[:8]),
		Status:       "building",
		SourceHash:   combinedHash,
		PluginIDs:    string(pluginIDsJSON),
		PluginHashes: string(pluginHashesJSON),
		CreatedBy:    createdBy,
		StartedAt:    timePtr(time.Now()),
	}

	if err := rb.db.Create(&version).Error; err != nil {
		return nil, fmt.Errorf("create runner version: %w", err)
	}

	fmt.Fprintf(&logBuf, "[%s] Runner build started: %s\n", time.Now().Format(time.RFC3339), version.ID)
	fmt.Fprintf(&logBuf, "  Plugins: %d, CombinedHash: %s\n", len(plugins), combinedHash[:12])

	// 6. 임시 디렉토리에 소스 배치 + 빌드
	buildErr := rb.buildInTempDir(ctx, &version, plugins, &logBuf)

	now := time.Now()
	duration := now.Sub(*version.StartedAt)
	version.FinishedAt = &now
	version.DurationMs = int(duration.Milliseconds())
	version.BuildLog = logBuf.String()

	if buildErr != nil {
		version.Status = "failed"
		version.Error = buildErr.Error()
		rb.db.Save(&version)
		return &RunnerBuildResult{
			VersionID: version.ID,
			Status:    "failed",
			BuildLog:  logBuf.String(),
			Duration:  duration,
		}, buildErr
	}

	version.Status = "ready"
	rb.db.Save(&version)

	// 7. 빌드 성공: DeployedHash 갱신 (빌드 중 수정 감지)
	rb.updateDeployedHashes(plugins, pluginHashes, version.ID)

	fmt.Fprintf(&logBuf, "  Build completed in %s\n", duration)

	return &RunnerBuildResult{
		VersionID: version.ID,
		Status:    "ready",
		ImageTag:  version.ImageTag,
		BuildLog:  logBuf.String(),
		Duration:  duration,
	}, nil
}

// buildInTempDir 임시 디렉토리에 소스를 배치하고 빌드
func (rb *RunnerBuilder) buildInTempDir(ctx context.Context, version *models.RunnerVersion, plugins []models.Plugin, logBuf *strings.Builder) error {
	tmpDir, err := os.MkdirTemp("", "conduix-runner-build-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// 플러그인 소스 배치
	for _, p := range plugins {
		pluginDir := filepath.Join(tmpDir, "plugins", sanitizeName(p.Name))
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			return fmt.Errorf("create plugin dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "stage.go"), []byte(p.SourceCode), 0o644); err != nil {
			return fmt.Errorf("write plugin source: %w", err)
		}
		// plugin 개별 go.mod
		goMod := p.GoMod
		if goMod == "" {
			goMod = fmt.Sprintf("module github.com/conduix/plugins/%s\n\ngo 1.26\n", sanitizeName(p.Name))
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "go.mod"), []byte(goMod), 0o644); err != nil {
			return fmt.Errorf("write plugin go.mod: %w", err)
		}
		fmt.Fprintf(logBuf, "  Plugin source placed: %s\n", p.Name)
	}

	// registry_custom.go 자동 생성
	registryCode := GenerateRegistryCustom(plugins)
	if err := os.WriteFile(filepath.Join(tmpDir, "registry_custom.go"), []byte(registryCode), 0o644); err != nil {
		return fmt.Errorf("write registry_custom.go: %w", err)
	}
	logBuf.WriteString("  registry_custom.go generated\n")

	// main.go (최소 runner entrypoint)
	mainCode := generateRunnerMain()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainCode), 0o644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// go.mod with local replace directives
	goMod := GenerateRunnerGoMod(plugins)
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}
	logBuf.WriteString("  go.mod generated with local replace directives\n")

	// go mod tidy
	buildCtx, cancel := context.WithTimeout(ctx, rb.config.BuildTimeout)
	defer cancel()

	logBuf.WriteString("  Running go mod tidy...\n")
	tidyOut, err := rb.runCommand(buildCtx, tmpDir, nil, "go", "mod", "tidy")
	logBuf.WriteString(tidyOut)
	if err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	// go build
	goos, goarch := parsePlatform(rb.config.Platform)
	logBuf.WriteString("  Running go build...\n")
	buildOut, err := rb.runCommand(buildCtx, tmpDir, []string{
		"GOOS=" + goos,
		"GOARCH=" + goarch,
	}, "go", "build", "-ldflags=-s -w", "-trimpath", "-o", "pipeline-runner", ".")
	logBuf.WriteString(buildOut)
	if err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	logBuf.WriteString("  Go build successful\n")

	// 이미지 태그 설정
	imageTag := fmt.Sprintf("%s:%s", rb.config.ImagePrefix, version.ID)
	version.ImageTag = imageTag

	// Docker build (optional)
	if rb.config.DockerPush {
		dockerfile := generateDockerfile()
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
			return fmt.Errorf("write Dockerfile: %w", err)
		}

		logBuf.WriteString("  Running docker build...\n")
		dockerOut, err := rb.runCommand(buildCtx, tmpDir, nil, "docker", "build", "-t", imageTag, ".")
		logBuf.WriteString(dockerOut)
		if err != nil {
			return fmt.Errorf("docker build: %w", err)
		}

		logBuf.WriteString("  Running docker push...\n")
		pushOut, err := rb.runCommand(buildCtx, tmpDir, nil, "docker", "push", imageTag)
		logBuf.WriteString(pushOut)
		if err != nil {
			return fmt.Errorf("docker push: %w", err)
		}
		logBuf.WriteString("  Docker push successful\n")
	}

	return nil
}

// updateDeployedHashes 빌드 성공 시 각 Plugin의 DeployedHash를 갱신
// 빌드 중 소스가 수정된 경우 (현재 SourceHash != 빌드 시점 해시) 해당 plugin은 갱신하지 않음
func (rb *RunnerBuilder) updateDeployedHashes(plugins []models.Plugin, buildTimeHashes map[string]string, versionID string) {
	for _, p := range plugins {
		buildHash, ok := buildTimeHashes[p.ID]
		if !ok {
			continue
		}
		// 현재 DB의 최신 SourceHash 조회
		var current models.Plugin
		if err := rb.db.First(&current, "id = ?", p.ID).Error; err != nil {
			continue
		}
		// 빌드 시점 해시와 현재 SourceHash가 같을 때만 갱신
		if current.SourceHash == buildHash {
			rb.db.Model(&current).Updates(map[string]any{
				"deployed_hash":     buildHash,
				"runner_version_id": versionID,
			})
		}
	}
}

// runCommand 명령 실행
func (rb *RunnerBuilder) runCommand(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOPROXY="+rb.config.GoProxy,
	)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Env, extraEnv...)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// CombinedSourceHash 모든 native plugin의 SourceHash를 결합하여 단일 해시 생성
// 정렬된 plugin ID 순서로 해시를 결합하여 결정적(deterministic) 결과 보장
func CombinedSourceHash(pluginHashes map[string]string) string {
	ids := make([]string, 0, len(pluginHashes))
	for id := range pluginHashes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	h := sha256.New()
	for _, id := range ids {
		_, _ = fmt.Fprintf(h, "%s:%s\n", id, pluginHashes[id])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// GenerateRegistryCustom registry_custom.go 코드 생성
// 모든 native plugin을 import하고 RegisterCustomStage로 등록하는 init() 함수 생성
func GenerateRegistryCustom(plugins []models.Plugin) string {
	var buf strings.Builder
	buf.WriteString("// Code generated by RunnerBuilder. DO NOT EDIT.\n")
	buf.WriteString("package main\n\n")
	buf.WriteString("import (\n")
	buf.WriteString("\t\"github.com/conduix/conduix/pipeline-core/pkg/stream\"\n")

	// 각 plugin import
	for _, p := range plugins {
		alias := fmt.Sprintf("plugin_%s", sanitizeName(p.Name))
		modPath := fmt.Sprintf("github.com/conduix/plugins/%s", sanitizeName(p.Name))
		fmt.Fprintf(&buf, "\t%s %q\n", alias, modPath)
	}
	buf.WriteString(")\n\n")

	buf.WriteString("func init() {\n")

	// PluginStage 정보를 사용하여 등록
	// 각 plugin의 stages를 등록
	for _, p := range plugins {
		alias := fmt.Sprintf("plugin_%s", sanitizeName(p.Name))
		// plugin의 Export 구조체 이름은 관례적으로 Stage
		stageType := sanitizeName(p.Name)
		fmt.Fprintf(&buf, "\tstream.RegisterCustomStage(%q, func(name string, config map[string]any) (stream.Stage, error) {\n", stageType)
		fmt.Fprintf(&buf, "\t\ts := &%s.Stage{}\n", alias)
		fmt.Fprintf(&buf, "\t\treturn stream.NewNativeStageAdapter(name, s, config)\n")
		buf.WriteString("\t})\n")
	}

	buf.WriteString("}\n")
	return buf.String()
}

// GenerateRunnerGoMod Runner 빌드용 go.mod 생성
func GenerateRunnerGoMod(plugins []models.Plugin) string {
	var buf strings.Builder
	buf.WriteString("module conduix-runner\n\ngo 1.26\n\nrequire (\n")
	buf.WriteString("\tgithub.com/conduix/conduix/pipeline-core v0.0.0\n")
	buf.WriteString("\tgithub.com/conduix/conduix/shared v0.0.0\n")
	buf.WriteString("\tgithub.com/conduix/conduix/plugin-sdk v0.0.0\n")

	for _, p := range plugins {
		modPath := fmt.Sprintf("github.com/conduix/plugins/%s", sanitizeName(p.Name))
		fmt.Fprintf(&buf, "\t%s v0.0.0\n", modPath)
	}
	buf.WriteString(")\n\nreplace (\n")
	buf.WriteString("\tgithub.com/conduix/conduix/pipeline-core => ../pipeline-core\n")
	buf.WriteString("\tgithub.com/conduix/conduix/shared => ../shared\n")
	buf.WriteString("\tgithub.com/conduix/conduix/plugin-sdk => ../plugin-sdk\n")

	for _, p := range plugins {
		name := sanitizeName(p.Name)
		modPath := fmt.Sprintf("github.com/conduix/plugins/%s", name)
		localPath := fmt.Sprintf("./plugins/%s", name)
		fmt.Fprintf(&buf, "\t%s => %s\n", modPath, localPath)
	}
	buf.WriteString(")\n")
	return buf.String()
}

// generateRunnerMain Runner 엔트리포인트 main.go 생성
func generateRunnerMain() string {
	return `// Code generated by RunnerBuilder. DO NOT EDIT.
package main

import (
	"fmt"
	"os"

	"github.com/conduix/conduix/pipeline-core/pkg/stream"
)

func main() {
	stages := stream.GetRegisteredCustomStages()
	fmt.Printf("Conduix Pipeline Runner — %d custom stages loaded\n", len(stages))
	for _, s := range stages {
		fmt.Printf("  - %s\n", s)
	}

	// TODO: 실제 runner 실행 로직 (Agent로부터 파이프라인 설정 수신 후 실행)
	fmt.Println("Runner ready. Waiting for pipeline execution...")

	// 환경변수로 모드 결정
	if os.Getenv("CONDUIX_RUNNER_MODE") == "list" {
		os.Exit(0)
	}

	// 실행 대기 (실제 구현은 Phase 4+)
	select {}
}
`
}

// generateDockerfile Runner Docker 이미지용 Dockerfile
func generateDockerfile() string {
	return `FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY pipeline-runner /usr/local/bin/pipeline-runner
RUN chmod +x /usr/local/bin/pipeline-runner
ENTRYPOINT ["/usr/local/bin/pipeline-runner"]
`
}

// extractPluginIDs Plugin 목록에서 ID만 추출
func extractPluginIDs(plugins []models.Plugin) []string {
	ids := make([]string, len(plugins))
	for i, p := range plugins {
		ids[i] = p.ID
	}
	return ids
}

// sanitizeName 플러그인 이름을 Go 패키지명으로 사용 가능하게 변환
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ToLower(name)
	return name
}

// parsePlatform "linux/arm64" → ("linux", "arm64")
func parsePlatform(platform string) (string, string) {
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "linux", "arm64"
}

func timePtr(t time.Time) *time.Time {
	return &t
}
