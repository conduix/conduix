package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	runnerBinOnce sync.Once
	runnerBinPath string
	runnerBinErr  error
)

// buildRunnerOnce 는 러너 바이너리를 테스트당 한 번만 빌드한다(전체 링크라 수십 초 걸린다).
func buildRunnerOnce(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	runnerBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "runner-init-check-*")
		if err != nil {
			runnerBinErr = err
			return
		}
		runnerBinPath = filepath.Join(dir, "runner-under-test")
		if out, berr := exec.Command("go", "build", "-o", runnerBinPath, ".").CombinedOutput(); berr != nil {
			runnerBinErr = berr
			t.Logf("build output:\n%s", out)
		}
	})
	if runnerBinErr != nil {
		t.Fatalf("build runner: %v", runnerBinErr)
	}
	return runnerBinPath
}

// CONDUIX_INIT_CHECK=1 이면 설정 없이 즉시 정상 종료해야 한다.
// 빌더가 이 종료 코드로 init() 중복 등록 여부를 판정하므로, 설정 누락 같은 다른 이유로
// 0 이 아닌 코드가 나오면 멀쩡한 빌드가 실패한다.
func TestMain_InitCheckExitsZeroWithoutConfig(t *testing.T) {
	cmd := exec.Command(buildRunnerOnce(t))
	// 설정 환경변수를 하나도 주지 않는다 — 점검 경로는 설정을 읽기 전에 끝나야 한다.
	cmd.Env = append(os.Environ(), "CONDUIX_INIT_CHECK=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init check must exit 0 without any config: %v\n%s", err, out)
	}
}

// 점검 플래그가 없으면 평소대로 설정을 읽고, 설정이 없으면 실패해야 한다.
// (플래그 검사가 설정 로드를 통째로 건너뛰게 만들면 안 된다.)
func TestMain_WithoutInitCheckStillRequiresConfig(t *testing.T) {
	cmd := exec.Command(buildRunnerOnce(t))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	if err := cmd.Run(); err == nil {
		t.Fatal("without CONDUIX_INIT_CHECK the runner must still fail on missing config")
	}
}
