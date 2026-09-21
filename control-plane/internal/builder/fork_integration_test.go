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

// init 자가점검이 중복 등록 panic 을 잡는지 확인한다.
// fork 두 벌이 같은 전역 이름을 등록하는 상황을 최소 재현으로 만들고,
// CONDUIX_INIT_CHECK 경로와 같은 방식(빌드 → 실행 → 종료코드)으로 검증한다.
func TestInitCheck_CatchesDuplicateRegistration(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	work := t.TempDir()
	rb := &RunnerBuilder{
		config: &RunnerBuilderConfig{CacheDir: filepath.Join(work, "cache"), GoProxy: "off", BuildTimeout: 5 * time.Minute},
		logger: slog.Default(),
	}

	// 두 패키지가 같은 드라이버 이름을 Register 한다 — fork 로 같은 모듈 두 벌이
	// 링크됐을 때 벌어지는 일의 최소 재현.
	files := map[string]string{
		"go.mod":  "module conduix-initcheck-probe\n\ngo 1.21\n",
		"a/a.go":  "package a\n\nimport (\n\t\"database/sql\"\n\t\"database/sql/driver\"\n)\n\ntype d struct{}\n\nfunc (d) Open(string) (driver.Conn, error) { return nil, nil }\n\nfunc init() { sql.Register(\"dupname\", d{}) }\n",
		"b/b.go":  "package b\n\nimport (\n\t\"database/sql\"\n\t\"database/sql/driver\"\n)\n\ntype d struct{}\n\nfunc (d) Open(string) (driver.Conn, error) { return nil, nil }\n\nfunc init() { sql.Register(\"dupname\", d{}) }\n",
		"main.go": "package main\n\nimport (\n\t\"os\"\n\n\t_ \"conduix-initcheck-probe/a\"\n\t_ \"conduix-initcheck-probe/b\"\n)\n\nfunc main() {\n\tif os.Getenv(\"CONDUIX_INIT_CHECK\") == \"1\" {\n\t\tos.Exit(0)\n\t}\n}\n",
	}
	for name, content := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := context.Background()
	if out, err := rb.runCommand(ctx, work, nil, "go", "build", "-o", "probe", "."); err != nil {
		t.Fatalf("build must succeed — the clash only shows at init time: %v\n%s", err, out)
	}
	out, err := rb.runCommand(ctx, work, []string{"CONDUIX_INIT_CHECK=1"}, filepath.Join(work, "probe"))
	if err == nil {
		t.Fatal("duplicate registration must panic before main() returns")
	}
	if line := firstPanicLine(out); !strings.Contains(line, "panic:") {
		t.Fatalf("expected a panic line in the output, got %q", line)
	}
}
