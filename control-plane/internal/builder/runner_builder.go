// Package builder provides RunnerBuilder for building pipeline-batch-job images
// that include all native plugin stages compiled in-process.
package builder

import (
	"bytes"
	"compress/gzip"
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
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// RunnerBuilderConfig Runner 빌드 설정
type RunnerBuilderConfig struct {
	BuildTimeout time.Duration // 빌드 타임아웃 (default: 5분)
	GoProxy      string        // GOPROXY 설정
	ImagePrefix  string        // Docker 이미지 prefix (예: ghcr.io/conduix/pipeline-batch-job)
	Platform     string        // 빌드 플랫폼 (default: linux/arm64)
	DockerPush   bool          // Docker push 수행 여부
	SourceRoot   string        // 로컬 모듈(pipeline-core/shared/plugin-sdk) 소스 루트. go.mod replace 대상.
	CacheDir     string        // GOCACHE/GOPATH 영속 경로. tmpDir 밖에 두어 재빌드 간 컴파일·모듈 캐시 재사용.
}

// DefaultRunnerBuilderConfig 기본 설정.
// SourceRoot 는 CONDUIX_SOURCE_ROOT env(런타임 이미지의 /app)에서 읽는다 — 빌드 시 replace 가
// 가리킬 pipeline-core/shared/plugin-sdk 소스 위치. env 없으면 로컬 개발(리포 루트 기준) 폴백.
func DefaultRunnerBuilderConfig() *RunnerBuilderConfig {
	sourceRoot := os.Getenv("CONDUIX_SOURCE_ROOT")
	if sourceRoot == "" {
		sourceRoot = ".." // 로컬 개발 폴백(control-plane 디렉토리 기준 리포 루트)
	}
	cacheDir := os.Getenv("CONDUIX_BUILD_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "conduix-runner-cache")
	}
	// 첫 빌드는 모듈 전체 다운로드로 5분을 넘길 수 있다(콜드 캐시).
	// 타임아웃 kill 은 "go build: signal: killed" 로 나타나 OOM 과 구분이 어려우므로
	// 환경에 맞게 조절 가능해야 한다.
	buildTimeout := 5 * time.Minute
	if v := os.Getenv("CONDUIX_BUILD_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			buildTimeout = d
		}
	}
	return &RunnerBuilderConfig{
		BuildTimeout: buildTimeout,
		GoProxy:      "https://proxy.golang.org,direct",
		ImagePrefix:  "ghcr.io/conduix/pipeline-batch-job",
		Platform:     "linux/arm64",
		DockerPush:   false,
		SourceRoot:   sourceRoot,
		CacheDir:     cacheDir,
	}
}

// buildMu 는 Build() 를 프로세스 전역으로 직렬화한다.
// auto-build(plugin update)와 수동 build 가 각각 goroutine 으로 거의 동시에 Build() 를 호출하면,
// DB building-count 락(체크→레코드생성이 비원자적)을 둘 다 통과해 두 go build 가 같은 CacheDir 을
// 동시에 써 GOCACHE 교착(hang)이 난다. control-plane 은 단일 프로세스이므로 in-process mutex 로 충분.
var buildMu sync.Mutex

// RunnerBuilder 모든 native plugin을 포함하는 pipeline-batch-job 이미지를 빌드
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
	rb := &RunnerBuilder{
		config: cfg,
		db:     db,
		logger: slog.Default().With("component", "runner-builder"),
	}

	// 부팅 시 좀비 building 즉시 회수: 빌드는 HTTP 핸들러의 goroutine 으로 도는데 control-plane 이
	// 재시작되면 그 goroutine 은 죽지만 DB 의 building 레코드는 남는다. 새 프로세스에는 진행 중 빌드가
	// 없음이 확실하므로(방금 부팅) building 을 전부 failed 로 회수한다. 이게 없으면 Build() 의
	// self-heal(2*BuildTimeout 경과분만 회수)이 돌 때까지 새 빌드가 "already in progress"로 막힌다.
	//
	// db 가 nil 이거나 미연결(라우트 등록 테스트의 mock DB 등)이면 회수 쿼리를 건너뛴다 —
	// 생성자에서 DB 쓰기를 무조건 하면 mock DB(db.DB==nil)에서 nil pointer panic 이 난다.
	rb.reclaimStaleBuilding()

	return rb
}

// reclaimStaleBuilding 은 부팅 시 좀비 building 레코드를 failed 로 회수한다.
// db 미연결이면 안전하게 no-op(생성자가 mock DB 로도 안전하게 만들어지도록).
func (rb *RunnerBuilder) reclaimStaleBuilding() {
	if rb.db == nil {
		return
	}
	// 실제 커넥션이 없는 mock(*gorm.DB 는 있으나 ConnPool nil)에서도 패닉 없이 넘어가도록 방어.
	sqlDB, err := rb.db.DB()
	if err != nil || sqlDB == nil {
		return
	}
	if err := sqlDB.Ping(); err != nil {
		return
	}

	res := rb.db.Model(&models.RunnerVersion{}).
		Where("status = ?", "building").
		Updates(map[string]any{"status": "failed", "error": "reclaimed on control-plane startup (build goroutine did not survive restart)"})
	if res.Error != nil {
		rb.logger.Warn("startup: failed to reclaim stale building versions", "error", res.Error)
	} else if res.RowsAffected > 0 {
		rb.logger.Info("startup: reclaimed stale building runner versions", "count", res.RowsAffected)
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
	// 동시 Build 직렬화(race → GOCACHE 교착 방지). 대기하지 않고 즉시 거부해,
	// 동시 트리거(auto-build + 수동)의 두 번째 호출이 첫 빌드와 겹치지 않게 한다.
	if !buildMu.TryLock() {
		return nil, fmt.Errorf("another build is already in progress")
	}
	defer buildMu.Unlock()

	start := time.Now()
	var logBuf strings.Builder

	// 1. 좀비 building 정리 후, 살아있는 building 이 있으면 거부.
	// control-plane 이 재시작되면 빌드 프로세스는 죽지만 DB 의 building 레코드는 남아
	// 영구 락이 된다(another build is already in progress). BuildTimeout 을 크게 넘긴
	// building 은 좀비로 보고 failed 로 회수한다(부팅 정리를 대체하는 self-heal).
	staleCutoff := time.Now().Add(-2 * rb.config.BuildTimeout)
	rb.db.Model(&models.RunnerVersion{}).
		Where("status = ? AND created_at < ?", "building", staleCutoff).
		Updates(map[string]any{"status": "failed", "error": "stale build reclaimed (builder restarted or crashed)"})

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

	// 3. 소스 해시 스냅샷 + 결합 해시 계산.
	// 코어 모듈(pipeline-core/shared/plugin-sdk) 소스 해시도 포함해야 한다 —
	// 안 하면 플러그인 소스가 그대로일 때 코어 코드가 바뀌어도(예: 새 stage 헬퍼,
	// config 해소 로직) 옛 바이너리를 재사용해 stale 실행이 된다.
	pluginHashes := make(map[string]string)
	for _, p := range plugins {
		pluginHashes[p.ID] = p.SourceHash
	}
	coreHash := rb.coreSourceHash()
	combinedHash := CombinedSourceHash(pluginHashes, coreHash)

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
		// Save(전 컬럼) 금지: Create 가 채운 DB의 build_number(autoIncrement)를 Go 구조체는
		// 모르므로(0) Save 가 0 으로 덮어쓴다 — 두 번째 실패부터 unique(build_number) 중복으로
		// 저장이 실패해 status 가 building 좀비로 남는다. 변경 필드만 갱신한다.
		if err := rb.persistFailure(&version); err != nil {
			rb.logger.Error("failed to persist build failure", "version_id", version.ID, "error", err)
		}
		return &RunnerBuildResult{
			VersionID: version.ID,
			Status:    "failed",
			BuildLog:  logBuf.String(),
			Duration:  duration,
		}, buildErr
	}

	// binary(수십MB longblob)를 status/메타데이터와 한 Save 로 쓰면, 큰 쿼리가
	// max_allowed_packet 초과나 타임아웃으로 통째로 실패하고 status 가 building 에 갇힌다.
	// binary 를 먼저 저장하고, 실패하면 build 를 failed 로 확정한다(building 좀비 방지).
	if saveErr := rb.persistBinary(&version); saveErr != nil {
		version.Binary = nil
		version.BinarySize = 0
		version.Status = "failed"
		version.Error = fmt.Sprintf("build succeeded but binary persist failed: %v", saveErr)
		if err := rb.persistFailure(&version); err != nil {
			rb.logger.Error("failed to persist binary-save failure", "version_id", version.ID, "error", err)
		}
		return &RunnerBuildResult{
			VersionID: version.ID,
			Status:    "failed",
			BuildLog:  logBuf.String(),
			Duration:  duration,
		}, fmt.Errorf("persist binary: %w", saveErr)
	}

	// binary 는 persistBinary 가 이미 썼으므로, 여기선 binary 컬럼을 제외한 메타데이터만
	// 업데이트한다(Save 는 전 컬럼을 쓰므로 큰 binary 를 매번 재전송해 packet 초과를 재유발).
	version.Status = "ready"
	if err := rb.db.Model(&version).Omit("binary").Updates(map[string]any{
		"status":      "ready",
		"finished_at": version.FinishedAt,
		"duration_ms": version.DurationMs,
		"build_log":   version.BuildLog,
		"image_tag":   version.ImageTag,
	}).Error; err != nil {
		rb.logger.Error("failed to persist ready status", "version_id", version.ID, "error", err)
		return &RunnerBuildResult{VersionID: version.ID, Status: "failed", BuildLog: logBuf.String(), Duration: duration}, fmt.Errorf("persist ready status: %w", err)
	}

	// 7. 빌드 성공: DeployedHash 갱신 (빌드 중 수정 감지)
	staleDetected := rb.updateDeployedHashes(plugins, pluginHashes, version.ID)

	fmt.Fprintf(&logBuf, "  Build completed in %s\n", duration)

	// 8. 빌드 중 소스가 바뀌어(연속 저장) deployed_hash 갱신을 스킵한 plugin 이 있으면,
	//    그 최신 소스는 아직 빌드 안 된 상태다(lock 에 막혀 그때 트리거된 빌드는 거부됨).
	//    락 해제(이 함수 defer) 후 자동으로 한 번 더 빌드해 최신 해시로 수렴시킨다.
	//    → 연속 저장 시 BUILD_REQUIRED 에 영구히 갇히는 것을 방지.
	if staleDetected {
		rb.logger.Info("source changed during build — scheduling rebuild to converge", "version_id", version.ID)
		go func() {
			time.Sleep(500 * time.Millisecond) // 현재 빌드의 락 해제 대기
			if _, err := rb.Build(context.Background(), createdBy); err != nil {
				rb.logger.Warn("auto-rebuild after stale detection failed", "error", err)
			}
		}()
	}

	return &RunnerBuildResult{
		VersionID: version.ID,
		Status:    "ready",
		ImageTag:  version.ImageTag,
		BuildLog:  logBuf.String(),
		Duration:  duration,
	}, nil
}

// buildInTempDir 임시 디렉토리에 pipeline-batch-job 모듈을 복사하고, 그 안에
// 플러그인 소스 + cmd/runner/registry_custom.go 를 주입해 실제 runner(./cmd/runner)를 빌드한다.
// 스텁 main 이 아니라 실제 배치 실행 로직에 native stage 를 compile-in 하는 것이 핵심이다 —
// registry_custom.go 의 init() 이 stream 전역 레지스트리에 stage 를 등록하면 executor 가 해석한다.
func (rb *RunnerBuilder) buildInTempDir(ctx context.Context, version *models.RunnerVersion, plugins []models.Plugin, logBuf *strings.Builder) error {
	tmpDir, err := os.MkdirTemp("", "conduix-runner-build-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// 영속 캐시 디렉토리(재빌드 간 컴파일·모듈 캐시 재사용). tmpDir 과 달리 삭제하지 않는다.
	if err := os.MkdirAll(rb.config.CacheDir, 0o755); err != nil {
		return fmt.Errorf("create build cache dir: %w", err)
	}

	// pipeline-batch-job 모듈을 tmpDir 로 복사. replace 가 ../pipeline-core 등 상대경로라
	// 형제 모듈(pipeline-core/shared/plugin-sdk)도 sourceRoot 에서 함께 복사해야 한다.
	batchJobDir := filepath.Join(tmpDir, "pipeline-batch-job")
	for _, mod := range []string{"pipeline-batch-job", "pipeline-core", "shared", "plugin-sdk"} {
		src := filepath.Join(rb.config.SourceRoot, mod)
		if err := copyDir(src, filepath.Join(tmpDir, mod)); err != nil {
			return fmt.Errorf("copy module %s from source root: %w", mod, err)
		}
	}
	logBuf.WriteString("  pipeline-batch-job module + sibling modules copied\n")

	// 허용 모듈 레지스트리 조회 — 의존성 버전의 단일 진실원천.
	// plugin go.mod 와 batch-job go.mod 양쪽이 이 버전을 쓰므로, 여러 stage 를 합쳐도
	// 같은 모듈이 항상 같은 버전 → 병합 충돌이 발생하지 않는다.
	allowedModules, err := rb.activeAllowedModules()
	if err != nil {
		return fmt.Errorf("query allowed modules: %w", err)
	}
	fmt.Fprintf(logBuf, "  Allowed modules: %d\n", len(allowedModules))

	// 플러그인 소스 배치 (batch-job 모듈 하위 plugins/)
	for _, p := range plugins {
		pluginDir := filepath.Join(batchJobDir, "plugins", sanitizeName(p.Name))
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			return fmt.Errorf("create plugin dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "stage.go"), []byte(p.SourceCode), 0o644); err != nil {
			return fmt.Errorf("write plugin source: %w", err)
		}
		// plugin go.mod 는 사용자 자유입력(p.GoMod)이 아니라 레지스트리에서 생성한다.
		// require 에 허용 모듈 전체를 넣어도, 실제 import 안 하는 건 go mod tidy 가 정리한다.
		goMod := generatePluginGoMod(sanitizeName(p.Name), allowedModules)
		if err := os.WriteFile(filepath.Join(pluginDir, "go.mod"), []byte(goMod), 0o644); err != nil {
			return fmt.Errorf("write plugin go.mod: %w", err)
		}
		fmt.Fprintf(logBuf, "  Plugin source placed: %s\n", p.Name)
	}

	// cmd/runner 패키지(package main)에 registry_custom.go 주입 — init() 으로 native stage 등록.
	registryCode := GenerateRegistryCustom(plugins)
	if err := os.WriteFile(filepath.Join(batchJobDir, "cmd", "runner", "registry_custom.go"), []byte(registryCode), 0o644); err != nil {
		return fmt.Errorf("write registry_custom.go: %w", err)
	}
	logBuf.WriteString("  cmd/runner/registry_custom.go generated\n")

	// batch-job go.mod 에 플러그인 require/replace + 허용 모듈 require 추가.
	// 허용 모듈을 메인 모듈에도 require 해야 plugin(로컬 replace)이 쓰는 외부 의존성을
	// go mod tidy 가 단일 버전으로 해석한다.
	if err := rb.appendPluginRequires(batchJobDir, plugins, allowedModules); err != nil {
		return fmt.Errorf("append plugin requires to go.mod: %w", err)
	}
	logBuf.WriteString("  go.mod extended with plugin + allowed-module require\n")

	// go mod tidy (batch-job 모듈 디렉토리에서)
	buildCtx, cancel := context.WithTimeout(ctx, rb.config.BuildTimeout)
	defer cancel()

	logBuf.WriteString("  Running go mod tidy...\n")
	tidyOut, err := rb.runCommand(buildCtx, batchJobDir, nil, "go", "mod", "tidy")
	logBuf.WriteString(tidyOut)
	if err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	// go build ./cmd/runner
	goos, goarch := parsePlatform(rb.config.Platform)
	logBuf.WriteString("  Running go build ./cmd/runner...\n")
	buildOut, err := rb.runCommand(buildCtx, batchJobDir, []string{
		"GOOS=" + goos,
		"GOARCH=" + goarch,
	}, "go", "build", "-ldflags=-s -w", "-trimpath", "-o", "pipeline-batch-job", "./cmd/runner")
	logBuf.WriteString(buildOut)
	if err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	logBuf.WriteString("  Go build successful\n")

	// 빌드 바이너리를 gzip 압축해 RunnerVersion 에 저장 — 레지스트리 push 없이 Job initContainer 가
	// 받아 실행하는 경로(선택지 2). tmpDir 은 곧 삭제되므로 여기서 읽어 둔다.
	binPath := filepath.Join(batchJobDir, "pipeline-batch-job")
	rawBin, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("read built binary: %w", err)
	}
	gzBin, err := gzipBytes(rawBin)
	if err != nil {
		return fmt.Errorf("gzip binary: %w", err)
	}
	version.Binary = gzBin
	version.BinarySize = len(rawBin)
	fmt.Fprintf(logBuf, "  Binary stored (raw=%d bytes, gz=%d bytes)\n", len(rawBin), len(gzBin))

	// 이미지 태그 설정
	imageTag := fmt.Sprintf("%s:%s", rb.config.ImagePrefix, version.ID)
	version.ImageTag = imageTag

	// Docker build (optional). 바이너리가 batchJobDir 에 있으므로 빌드 컨텍스트도 그곳.
	if rb.config.DockerPush {
		dockerfile := generateDockerfile()
		if err := os.WriteFile(filepath.Join(batchJobDir, "Dockerfile.runner"), []byte(dockerfile), 0o644); err != nil {
			return fmt.Errorf("write Dockerfile: %w", err)
		}

		logBuf.WriteString("  Running docker build...\n")
		dockerOut, err := rb.runCommand(buildCtx, batchJobDir, nil, "docker", "build", "-f", "Dockerfile.runner", "-t", imageTag, ".")
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

// updateDeployedHashes 빌드 성공 시 각 Plugin의 DeployedHash를 갱신.
// 빌드 중 소스가 수정된 경우 (현재 SourceHash != 빌드 시점 해시) 해당 plugin은 갱신하지 않는다.
// 반환값: 빌드 중 소스 변경으로 갱신을 스킵한 plugin 이 하나라도 있으면 true(재빌드 필요).
func (rb *RunnerBuilder) updateDeployedHashes(plugins []models.Plugin, buildTimeHashes map[string]string, versionID string) bool {
	staleDetected := false
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
		} else {
			staleDetected = true
		}
	}
	return staleDetected
}

// runCommand 명령 실행.
// GOCACHE/GOPATH 를 tmpDir 밖의 영속 CacheDir 하위로 둔다 — tmpDir 은 매 빌드 삭제돼
// 여기 두면 컴파일·모듈 캐시가 버려져 121MB 바이너리를 매번 처음부터 빌드하게 된다.
// HOME 은 dir(tmp)로 격리한다. control-plane 이 비-root(HOME=/)일 때 go 가 /.cache 에
// 쓰려다 permission denied 로 실패하는 걸 CacheDir/HOME 지정으로 우회한다.
// (동시 빌드는 상위 락으로 1개만 허용되므로 캐시 경합 없음.)
//
// 프로세스 그룹 kill: `go build` 는 compile/link 자식을 여럿 fork 하는데, 기본
// exec.CommandContext 의 취소는 직접 자식(go)만 SIGKILL 하고 손자(compile)는 고아로
// 남아 계속 돈다 → CacheDir 오염 + 상위 buildMu 점유로 새 빌드 무한 거부. Setpgid 로
// 새 프로세스 그룹을 만들고 Cancel 을 그룹 전체(-pgid) kill 로 재정의해 전 자손을 함께 종료한다.
func (rb *RunnerBuilder) runCommand(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOPROXY="+rb.config.GoProxy,
		"GOCACHE="+filepath.Join(rb.config.CacheDir, "gocache"),
		"GOPATH="+filepath.Join(rb.config.CacheDir, "gopath"),
		"HOME="+dir,
	)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Env, extraEnv...)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// 음수 pid = 프로세스 그룹 전체에 시그널(go + 모든 compile/link 자손).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// SIGKILL 후에도 파이프가 안 닫히면 최대 2초 뒤 Wait 강제 종료(고아 잔류 방지).
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// persistBinary 는 gzip 바이너리(수십MB longblob)만 별도 UPDATE 로 저장한다.
// status/메타데이터와 분리해, 큰 write 가 실패해도 상위에서 build 를 failed 로 확정할 수 있게 한다.
// persistFailure 실패 상태를 변경 필드만으로 기록한다.
// Save(전 컬럼 UPDATE)는 autoIncrement 인 build_number 를 0 으로 덮어
// unique 충돌 → 저장 실패 → building 좀비를 만들므로 쓰지 않는다.
func (rb *RunnerBuilder) persistFailure(version *models.RunnerVersion) error {
	return rb.db.Model(version).Omit("binary").Updates(map[string]any{
		"status":      version.Status,
		"error":       version.Error,
		"finished_at": version.FinishedAt,
		"duration_ms": version.DurationMs,
		"build_log":   version.BuildLog,
		"binary_size": version.BinarySize,
	}).Error
}

func (rb *RunnerBuilder) persistBinary(version *models.RunnerVersion) error {
	if len(version.Binary) == 0 {
		return fmt.Errorf("empty binary")
	}
	return rb.db.Model(version).Updates(map[string]any{
		"binary":      version.Binary,
		"binary_size": version.BinarySize,
	}).Error
}

// gzipBytes 는 바이너리를 gzip 압축한다(RunnerVersion.Binary 저장용).
func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CombinedSourceHash 모든 native plugin의 SourceHash를 결합하여 단일 해시 생성
// 정렬된 plugin ID 순서로 해시를 결합하여 결정적(deterministic) 결과 보장
// CombinedSourceHash 플러그인 소스 해시들 + 코어 모듈 해시를 하나로 결합한다.
// coreHash 는 pipeline-core/shared/plugin-sdk 소스 스냅샷 해시(빈 문자열 허용 — 하위호환).
func CombinedSourceHash(pluginHashes map[string]string, coreHash string) string {
	ids := make([]string, 0, len(pluginHashes))
	for id := range pluginHashes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	h := sha256.New()
	for _, id := range ids {
		_, _ = fmt.Fprintf(h, "%s:%s\n", id, pluginHashes[id])
	}
	if coreHash != "" {
		_, _ = fmt.Fprintf(h, "core:%s\n", coreHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// coreSourceHash pipeline-core/shared/plugin-sdk 의 .go 소스 내용을 해시한다.
// 코어 코드가 바뀌면(플러그인 소스는 그대로여도) combinedHash 가 달라져 재빌드된다.
// 실패 시 빈 문자열(해시 미포함) — 재사용 스킵 판정이 관대해질 뿐 안전에는 무해.
func (rb *RunnerBuilder) coreSourceHash() string {
	h := sha256.New()
	for _, mod := range []string{"pipeline-core", "shared", "plugin-sdk"} {
		root := filepath.Join(rb.config.SourceRoot, mod)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(rb.config.SourceRoot, path)
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			_, _ = fmt.Fprintf(h, "%s:%x\n", rel, sha256.Sum256(data))
			return nil
		})
		if err != nil {
			rb.logger.Warn("core source hash walk failed — 코어 변경 감지 없이 진행", "module", mod, "error", err)
			return ""
		}
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

	// 각 plugin의 Stage를 등록 (plugin name = stage type)
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

// activeAllowedModules 는 status=active 인 허용 모듈을 조회한다(의존성 버전 단일 원천).
func (rb *RunnerBuilder) activeAllowedModules() ([]models.AllowedModule, error) {
	var mods []models.AllowedModule
	if err := rb.db.Where("status = ?", "active").Order("module_path asc").Find(&mods).Error; err != nil {
		return nil, err
	}
	return mods, nil
}

// generatePluginGoMod 는 plugin 개별 go.mod 를 레지스트리 기반으로 생성한다.
// 허용 모듈 전체를 require 에 넣어도, 실제 import 하지 않는 모듈은 go mod tidy 가 제거한다.
// 사용자 자유입력(p.GoMod)을 쓰지 않으므로 stage 마다 버전이 갈릴 수 없다.
func generatePluginGoMod(name string, allowed []models.AllowedModule) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "module github.com/conduix/plugins/%s\n\ngo 1.27\n", name)
	buf.WriteString("\nrequire github.com/conduix/conduix/plugin-sdk v0.0.0\n")
	if len(allowed) > 0 {
		buf.WriteString("\nrequire (\n")
		for _, m := range allowed {
			fmt.Fprintf(&buf, "\t%s %s\n", m.ModulePath, m.Version)
		}
		buf.WriteString(")\n")
	}
	// plugin-sdk 는 로컬 모듈이라 replace 필요(batch-job 의 replace 와 동일 상대경로 기준).
	buf.WriteString("\nreplace github.com/conduix/conduix/plugin-sdk => ../../../plugin-sdk\n")
	return buf.String()
}

// pluginRequireBlock go.mod 에 추가할 플러그인 require/replace + 허용 모듈 require 텍스트.
// batch-job go.mod 는 이미 pipeline-core/shared/plugin-sdk replace(../..)를 가지므로
// 로컬 모듈 replace 는 플러그인만 다룬다. 허용 모듈은 require 만 추가(외부 모듈).
func pluginRequireBlock(plugins []models.Plugin, allowed []models.AllowedModule) string {
	var buf strings.Builder
	buf.WriteString("\nrequire (\n")
	for _, p := range plugins {
		fmt.Fprintf(&buf, "\tgithub.com/conduix/plugins/%s v0.0.0\n", sanitizeName(p.Name))
	}
	for _, m := range allowed {
		fmt.Fprintf(&buf, "\t%s %s\n", m.ModulePath, m.Version)
	}
	buf.WriteString(")\n\nreplace (\n")
	for _, p := range plugins {
		name := sanitizeName(p.Name)
		fmt.Fprintf(&buf, "\tgithub.com/conduix/plugins/%s => ./plugins/%s\n", name, name)
	}
	buf.WriteString(")\n")
	return buf.String()
}

// appendPluginRequires batch-job go.mod 끝에 플러그인 + 허용 모듈 require/replace 블록을 덧붙인다.
func (rb *RunnerBuilder) appendPluginRequires(batchJobDir string, plugins []models.Plugin, allowed []models.AllowedModule) error {
	goModPath := filepath.Join(batchJobDir, "go.mod")
	existing, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read batch-job go.mod: %w", err)
	}
	combined := string(existing) + pluginRequireBlock(plugins, allowed)
	return os.WriteFile(goModPath, []byte(combined), 0o644)
}

// copyDir src 디렉토리를 dst 로 재귀 복사한다(심볼릭 링크·.git 은 제외).
// 빌드에 불필요한 대용량 디렉토리(.git, .gocache, .gopath)는 건너뛴다.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if rel != "." && (base == ".git" || base == ".gocache" || base == ".gopath") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, info.Mode().Perm())
	})
}

// generateDockerfile Runner Docker 이미지용 Dockerfile(DockerPush 옵션 전용).
func generateDockerfile() string {
	return `FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY pipeline-batch-job /usr/local/bin/pipeline-batch-job
RUN chmod +x /usr/local/bin/pipeline-batch-job
ENTRYPOINT ["/usr/local/bin/pipeline-batch-job"]
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
