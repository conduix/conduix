package handlers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/conduix/conduix/control-plane/internal/dependency"
)

// stageCompileTimeout 은 단일 stage 임시 빌드의 상한이다.
const stageCompileTimeout = 60 * time.Second

// stageCompileResult 는 임시 빌드 결과다. BinaryPath 는 Cleanup 전까지만 유효하다.
type stageCompileResult struct {
	BinaryPath string
	Output     string
	// Cleanup 은 임시 디렉토리를 지운다. 성공·실패 모두 반드시 호출한다.
	Cleanup func()
}

// compileStage 는 stage 소스 하나를 지정한 의존성 버전으로 임시 모듈에 배치해 컴파일한다.
//
// 에디터 테스트(TestNativePlugin)와 버전 올리기(UpgradeDeps)가 같은 절차를 쓴다 —
// "이 소스가 이 버전들로 컴파일되는가" 라는 하나의 질문이라 절차가 갈리면 두 기능의
// 판정이 어긋난다. 단일 stage 빌드라 fork 는 필요 없다(고정 버전을 원래 경로로 require).
func compileStage(ctx context.Context, sourceCode string, pins dependency.Pins) (*stageCompileResult, error) {
	tmpDir, err := os.MkdirTemp("", "conduix-test-*")
	if err != nil {
		return nil, fmt.Errorf("임시 디렉토리 생성 실패: %w", err)
	}
	res := &stageCompileResult{Cleanup: func() { _ = os.RemoveAll(tmpDir) }}

	// 사용자 소스는 하위 패키지 pluginstage/stage.go 로 둔다(실제 RunnerBuilder 와 동일 계약:
	// 사용자 소스는 자기 package + type Stage struct, 러너는 그것을 import 해 &Stage{} 로 생성).
	// runner main 은 루트에 package main 으로 둔다. 이렇게 분리해야 'found packages main and X'
	// package clash 없이 빌드된다(BUG#6).
	stageDir := filepath.Join(tmpDir, "pluginstage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		res.Cleanup()
		return nil, fmt.Errorf("stage 패키지 디렉토리 생성 실패: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "stage.go"), []byte(sourceCode), 0o600); err != nil {
		res.Cleanup()
		return nil, fmt.Errorf("소스 파일 작성 실패: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "runner_main.go"), []byte(testRunnerMain), 0o600); err != nil {
		res.Cleanup()
		return nil, fmt.Errorf("러너 main 작성 실패: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(dependency.TestGoMod(pins, sdkPathFromEnv())), 0o600); err != nil {
		res.Cleanup()
		return nil, fmt.Errorf("go.mod 작성 실패: %w", err)
	}

	// GOCACHE/GOPATH/HOME 을 쓰기가능 경로로 지정 — control-plane 이 비-root(HOME=/)면
	// go 가 /.cache 에 쓰려다 permission denied 로 실패한다(RunnerBuilder 와 동일 이슈).
	// runner 빌드와 CacheDir 를 공유해 이미 받은 모듈(예: uuid)을 재다운로드하지 않는다.
	cacheDir := os.Getenv("CONDUIX_BUILD_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "conduix-runner-cache")
	}
	buildEnv := append(os.Environ(),
		"CGO_ENABLED=0",
		"GOCACHE="+filepath.Join(cacheDir, "gocache"),
		"GOPATH="+filepath.Join(cacheDir, "gopath"),
		"GOMODCACHE="+filepath.Join(cacheDir, "gopath", "pkg", "mod"),
		"HOME="+tmpDir,
	)

	// 외부 모듈(레지스트리) require 해석을 위해 build 전에 go mod tidy(실패해도 build 에서 재시도).
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	tidyCmd.Env = buildEnv
	_, _ = tidyCmd.CombinedOutput()

	binPath := filepath.Join(tmpDir, "plugin-test")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	buildCmd.Env = buildEnv

	out, buildErr := buildCmd.CombinedOutput()
	res.BinaryPath = binPath
	res.Output = string(out)
	if buildErr != nil {
		return res, fmt.Errorf("컴파일 실패: %w", buildErr)
	}
	return res, nil
}

// sdkPathFromEnv 는 plugin-sdk 로컬 소스 경로다(런타임 이미지 기준 기본값).
func sdkPathFromEnv() string {
	if p := os.Getenv("CONDUIX_SDK_PATH"); p != "" {
		return p
	}
	return "/app/plugin-sdk"
}
