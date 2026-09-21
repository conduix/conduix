//go:build integration

package builder

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conduix/conduix/control-plane/internal/dependency"
)

// fixtureSource 는 testdata 의 모듈 두 벌을 버전별로 돌려주는 ModuleSource 대역.
// 네트워크(go mod download) 없이 fork 경로 전체를 검증하기 위한 이음매다.
type fixtureSource struct{ root string }

func (s *fixtureSource) Dir(_ context.Context, modulePath, version string) (string, error) {
	name := map[string]string{"v1.0.0": "foo_v1", "v2.0.0": "foo_v2"}[version]
	return filepath.Join(s.root, "testdata", name), nil
}

// CONFLICT.md §5 실험 2~5 의 자동화: 같은 모듈의 두 버전을 fork 로 함께 링크하고
// 실제로 go build + 실행해 두 stage 가 각기 다른 버전을 보는지 확인한다.
//
// 자기참조 import 재작성을 빼면 이 테스트는 "빌드는 성공하는데 두 값이 같아지는"
// 형태로 실패한다 — 조용히 섞이는 그 함정이 정확히 여기서 잡힌다.
func TestForkedModules_TwoVersionsCoexistInOneBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	const modulePath = "github.com/example/foo"
	forkOld := dependency.Fork{
		ModulePath: modulePath, Version: "v1.0.0",
		ForkPath: dependency.ForkPath(modulePath, "v1.0.0"),
		DirName:  dependency.ForkDirName(modulePath, "v1.0.0"),
	}
	forkNew := dependency.Fork{
		ModulePath: modulePath, Version: "v2.0.0",
		ForkPath: dependency.ForkPath(modulePath, "v2.0.0"),
		DirName:  dependency.ForkDirName(modulePath, "v2.0.0"),
	}

	work := t.TempDir()
	rb := &RunnerBuilder{
		config:       &RunnerBuilderConfig{CacheDir: filepath.Join(work, "cache"), GoProxy: "off", BuildTimeout: 5 * time.Minute},
		logger:       slog.Default(),
		moduleSource: &fixtureSource{root: cwd},
	}
	resolved := &resolvedDeps{Forks: []dependency.Fork{forkOld, forkNew}}

	var logBuf strings.Builder
	if err := rb.materializeForks(context.Background(), work, resolved, &logBuf); err != nil {
		t.Fatalf("materializeForks: %v", err)
	}

	// 두 fork 를 함께 import 하는 main — 실제 "한 바이너리에 두 버전" 상황.
	main := "package main\n\nimport (\n\t\"fmt\"\n\n\tvold \"" + forkOld.ForkPath + "\"\n\tvnew \"" + forkNew.ForkPath + "\"\n)\n\n" +
		"func main() { fmt.Printf(\"%s|%s\\n\", vold.Marker(), vnew.Marker()) }\n"
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	goMod := "module conduix-fork-integration\n\ngo 1.21\n\nrequire (\n" +
		"\t" + forkOld.ForkPath + " v0.0.0\n" +
		"\t" + forkNew.ForkPath + " v0.0.0\n)\n\nreplace (\n" +
		"\t" + forkOld.ForkPath + " => ./" + forkedDirRoot + "/" + forkOld.DirName + "\n" +
		"\t" + forkNew.ForkPath + " => ./" + forkedDirRoot + "/" + forkNew.DirName + "\n)\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	ctx := context.Background()
	if out, err := rb.runCommand(ctx, work, nil, "go", "build", "-o", "forkprobe", "."); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	out, err := rb.runCommand(ctx, work, nil, filepath.Join(work, "forkprobe"))
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}

	got := strings.TrimSpace(out)
	if got != "old|new" {
		t.Fatalf("the two forks must resolve to their own versions, got %q — "+
			"identical halves mean the copies' self-imports still point at one module", got)
	}
}
