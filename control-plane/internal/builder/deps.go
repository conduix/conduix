package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conduix/conduix/control-plane/internal/dependency"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// resolvedDeps 는 이번 빌드에 참여하는 모든 stage 의 의존성 해소 결과다.
// 빌더는 이 구조체만 보고 go.mod 를 만든다 — 버전 결정 정책 자체는 dependency 패키지가 갖는다.
type resolvedDeps struct {
	Names    []string                   // sanitize 된 플러그인 이름(플러그인 목록 순서 보존)
	Pins     map[string]dependency.Pins // 이름 → 이 stage 가 쓸 모듈 버전
	Defaults map[string]string          // 모듈 → 레지스트리 기본 버전
	Forks    []dependency.Fork          // 기본과 다른 버전이라 복사·재작성이 필요한 모듈(W3)
	// Backfill 은 DepVersions 가 비어 있던 레거시 stage 의 새 고정값이다(plugin ID → JSON).
	// 빌드가 성공해야 저장한다 — 실패한 빌드의 버전을 stage 에 박아두지 않기 위함.
	Backfill map[string]string
}

// resolveDeps 는 각 stage 의 고정 버전을 확정한다.
// 레거시(DepVersions 비어 있음) stage 는 지금의 기본 버전으로 고정하되, 저장은 빌드 성공 후다.
func (rb *RunnerBuilder) resolveDeps(plugins []models.Plugin) (*resolvedDeps, error) {
	allowed, err := rb.activeAllowedModules()
	if err != nil {
		return nil, fmt.Errorf("query allowed modules: %w", err)
	}
	defaults := dependency.Defaults(allowed)

	out := &resolvedDeps{
		Names:    make([]string, 0, len(plugins)),
		Pins:     make(map[string]dependency.Pins, len(plugins)),
		Defaults: defaults,
		Backfill: map[string]string{},
	}
	idsByName := make(map[string]string, len(plugins))

	for _, p := range plugins {
		name := sanitizeName(p.Name)
		out.Names = append(out.Names, name)
		idsByName[name] = p.ID

		if pins := dependency.ParsePins(p.DepVersions); pins != nil {
			out.Pins[name] = pins
			continue
		}

		// 레거시 stage: 소스의 import 에서 기본 버전으로 고정값을 만든다.
		//
		// 해소가 실패해도(깨진 소스, retire 된 모듈을 아직 import 하는 stage) 빌드를
		// 세우지 않는다 — 이 stage 하나 때문에 다른 모든 stage 의 배포까지 막히기 때문이다.
		// 고정값 없이 두면 go mod tidy 가 메인 go.mod 의 require 로 해석하므로,
		// 해소 전과 같은 동작(그 stage 는 go build 단계에서 걸림)으로 떨어진다.
		imports, perr := dependency.ParseImports(p.SourceCode)
		if perr != nil {
			rb.logger.Warn("stage 소스 파싱 실패 — 고정 버전 없이 빌드 진행", "plugin", p.Name, "error", perr)
			continue
		}
		pins, rerr := dependency.ResolvePins(imports, nil, allowed)
		if rerr != nil {
			rb.logger.Warn("stage 의존성 해소 실패 — 고정 버전 없이 빌드 진행", "plugin", p.Name, "error", rerr)
			continue
		}
		out.Pins[name] = pins
		encoded, eerr := pins.Encode()
		if eerr != nil {
			rb.logger.Warn("고정 버전 직렬화 실패 — 백필 생략", "plugin", p.Name, "error", eerr)
			continue
		}
		if encoded != "" {
			out.Backfill[p.ID] = encoded
		}
	}

	out.Forks = dependency.CollectForks(idsByName, out.Names, out.Pins, defaults)

	// init() 으로 전역에 등록하는 모듈(database/sql 드라이버 등)은 두 벌이 링크되면
	// 중복 등록 panic 이 난다. 저장 시점에도 막지만, 저장 뒤에 모듈이
	// single_version_only 로 바뀐 경우는 여기서만 잡힌다.
	singleOnly := map[string]bool{}
	for _, m := range allowed {
		if m.SingleVersionOnly {
			singleOnly[m.ModulePath] = true
		}
	}
	for _, f := range out.Forks {
		if singleOnly[f.ModulePath] {
			return nil, fmt.Errorf("%s 는 단일 버전만 허용하는 모듈인데 stage %s 가 %s 로 고정했습니다 — 기본 버전(%s)으로 맞추세요",
				f.ModulePath, strings.Join(f.PluginIDs, ","), f.Version, defaults[f.ModulePath])
		}
	}
	return out, nil
}

// saveBackfilledPins 는 레거시 stage 의 고정 버전을 빌드 성공 후 저장한다.
func (rb *RunnerBuilder) saveBackfilledPins(resolved *resolvedDeps) {
	for id, encoded := range resolved.Backfill {
		if err := rb.db.Model(&models.Plugin{}).Where("id = ?", id).
			Update("dep_versions", encoded).Error; err != nil {
			rb.logger.Warn("dep_versions 백필 저장 실패 — 다음 빌드에서 재시도", "plugin_id", id, "error", err)
		}
	}
}

// forkedDirRoot 는 fork 복사본들이 놓이는 배치잡 모듈 하위 디렉토리다.
const forkedDirRoot = "forked"

// materializeForks 는 기본과 다른 버전으로 고정된 모듈들을 batchJobDir/forked/<dir> 로
// 복사하고 자기참조 import 를 fork 경로로 재작성한다.
//
// 재작성을 생략하면 복사본 내부 패키지들이 여전히 원래 경로를 가리켜 기본 버전으로 해석되고,
// 한 stage 안에 두 버전이 에러 없이 섞인다(CONFLICT.md §5 실험 4). 복사와 재작성은
// 항상 짝이다.
func (rb *RunnerBuilder) materializeForks(ctx context.Context, batchJobDir string, resolved *resolvedDeps, logBuf *strings.Builder) error {
	if len(resolved.Forks) == 0 {
		return nil
	}

	src := rb.moduleSource
	if src == nil {
		src = &dependency.GoModDownloadSource{Run: rb.runCommand, WorkDir: batchJobDir}
	}
	for _, f := range resolved.Forks {
		cacheDir, err := src.Dir(ctx, f.ModulePath, f.Version)
		if err != nil {
			return fmt.Errorf("fork %s@%s 소스 확보 실패: %w", f.ModulePath, f.Version, err)
		}
		dst := filepath.Join(batchJobDir, forkedDirRoot, f.DirName)
		if err := copyDir(cacheDir, dst); err != nil {
			return fmt.Errorf("fork %s@%s 복사 실패: %w", f.ModulePath, f.Version, err)
		}
		// 모듈 캐시는 읽기 전용(0444)이라 복사본도 그대로면 재작성이 실패한다.
		if err := makeTreeWritable(dst); err != nil {
			return fmt.Errorf("fork %s@%s 권한 변경 실패: %w", f.ModulePath, f.Version, err)
		}
		if err := dependency.RewriteModuleTree(dst, f.ModulePath, f.ForkPath); err != nil {
			return fmt.Errorf("fork %s@%s 내부 import 재작성 실패: %w", f.ModulePath, f.Version, err)
		}
		fmt.Fprintf(logBuf, "  Forked module: %s@%s → %s/%s (stages: %s)\n",
			f.ModulePath, f.Version, forkedDirRoot, f.DirName, strings.Join(f.PluginIDs, ","))
	}
	return nil
}

// stageSourceFor 는 플러그인 소스에서 비기본 고정 모듈의 import 를 fork 경로로 바꾼 결과다.
// 사용자 코드 본문은 건드리지 않는다 — import 줄만 바뀌고 alias 로 패키지 이름을 유지한다.
func (rb *RunnerBuilder) stageSourceFor(batchJobDir, name string, p models.Plugin, resolved *resolvedDeps) (string, error) {
	pins := resolved.Pins[name]
	if len(pins) == 0 || len(resolved.Forks) == 0 {
		return p.SourceCode, nil
	}
	pkgNameOf := func(modulePath, version, importPath string) (string, error) {
		dir := filepath.Join(batchJobDir, forkedDirRoot, dependency.ForkDirName(modulePath, version))
		if sub := strings.TrimPrefix(importPath, modulePath); sub != "" {
			dir = filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(sub, "/")))
		}
		return dependency.PackageNameInDir(dir)
	}
	out, err := dependency.RewriteStageImports(p.SourceCode, pins, resolved.Defaults, pkgNameOf)
	if err != nil {
		return "", fmt.Errorf("plugin %s import 재작성 실패: %w", p.Name, err)
	}
	return out, nil
}

// makeTreeWritable 은 복사된 모듈 캐시 트리에 쓰기 권한을 준다(캐시 원본은 0444/0555).
func makeTreeWritable(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		return os.Chmod(path, mode)
	})
}
