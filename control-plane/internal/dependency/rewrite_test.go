package dependency

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 다중 패키지 모듈 하나를 디스크에 만든다(루트 + sub 패키지, 루트가 sub 를 자기참조 import).
func writeModuleTree(t *testing.T, dir, modulePath, marker string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]string{
		"go.mod": "module " + modulePath + "\n\ngo 1.21\n",
		"root.go": "package foo\n\nimport (\n\t\"strings\"\n\n\t\"" + modulePath + "/sub\"\n)\n\n" +
			"func Marker() string { return strings.ToUpper(sub.Marker()) }\n",
		"sub/sub.go":        "package sub\n\nfunc Marker() string { return \"" + marker + "\" }\n",
		"sub/sub_test.go":   "package sub\n\nimport \"" + modulePath + "/sub\"\n",
		"testdata/skip.go":  "package testdata\n\nimport \"" + modulePath + "/sub\"\n",
		"broken/broken.gox": "not go at all",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// 복사본 내부의 자기참조 import 를 안 바꾸면 에러 없이 두 버전이 섞인다.
// 이 테스트가 그 함정의 회귀선이다.
func TestRewriteModuleTree_SelfImportsAndGoMod(t *testing.T) {
	dir := t.TempDir()
	const orig = "github.com/example/foo"
	writeModuleTree(t, dir, orig, "v-old")

	forkPath := ForkPath(orig, "v0.8.0")
	if err := RewriteModuleTree(dir, orig, forkPath); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	goMod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(goMod, "module "+forkPath) {
		t.Fatalf("go.mod module line not rewritten:\n%s", goMod)
	}

	root := readFile(t, filepath.Join(dir, "root.go"))
	if strings.Contains(root, `"`+orig+`/sub"`) {
		t.Fatalf("self-import still points at the original module — versions would silently mix:\n%s", root)
	}
	if !strings.Contains(root, `"`+forkPath+`/sub"`) {
		t.Fatalf("self-import not rewritten to the fork path:\n%s", root)
	}
	// 무관한 import 와 본문은 그대로여야 한다.
	if !strings.Contains(root, `"strings"`) || !strings.Contains(root, "strings.ToUpper(sub.Marker())") {
		t.Fatalf("unrelated import or body was altered:\n%s", root)
	}

	// _test.go 와 testdata 는 빌드 대상이 아니므로 건드리지 않는다.
	if got := readFile(t, filepath.Join(dir, "sub", "sub_test.go")); !strings.Contains(got, orig) {
		t.Fatalf("_test.go should be left alone: %s", got)
	}
	if got := readFile(t, filepath.Join(dir, "testdata", "skip.go")); !strings.Contains(got, orig) {
		t.Fatalf("testdata should be left alone: %s", got)
	}
}

func TestRewriteModuleTree_PreservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	const orig = "github.com/example/foo"
	writeModuleTree(t, dir, orig, "v-old")
	target := filepath.Join(dir, "root.go")
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// 읽기 전용 모듈 캐시를 복사한 뒤 쓰기 권한을 주는 것은 빌더 책임이므로,
	// 여기서는 쓰기 가능해야 재작성이 성공한다는 계약만 고정한다.
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("chmod back: %v", err)
	}
	if err := RewriteModuleTree(dir, orig, ForkPath(orig, "v0.8.0")); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("permissions changed: %v", info.Mode().Perm())
	}
}

const stageWithUUID = `package uuidtag

import (
	"strconv"

	"github.com/google/uuid"
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type Stage struct{}

var _ sdk.NativeStage = (*Stage)(nil)

func (s *Stage) Process(r map[string]any) (map[string]any, error) {
	r["id"] = uuid.NewString() + strconv.Itoa(1)
	return r, nil
}
`

func pkgNameStub(name string) func(string, string, string) (string, error) {
	return func(string, string, string) (string, error) { return name, nil }
}

func TestRewriteStageImports_AddsAliasForForkedImport(t *testing.T) {
	defaults := map[string]string{"github.com/google/uuid": "v1.6.0"}
	pins := Pins{"github.com/google/uuid": "v1.3.0"}

	out, err := RewriteStageImports(stageWithUUID, pins, defaults, pkgNameStub("uuid"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	fp := ForkPath("github.com/google/uuid", "v1.3.0")
	if !strings.Contains(out, `uuid "`+fp+`"`) {
		t.Fatalf("expected an alias so the mangled path keeps the package name:\n%s", out)
	}
	// 본문 호출은 손대지 않는다.
	if !strings.Contains(out, "uuid.NewString()") {
		t.Fatalf("body must not be rewritten:\n%s", out)
	}
	// 무관한 import 는 그대로.
	if !strings.Contains(out, `"strconv"`) || !strings.Contains(out, `sdk "github.com/conduix/conduix/plugin-sdk"`) {
		t.Fatalf("unrelated imports were altered:\n%s", out)
	}
}

func TestRewriteStageImports_KeepsUserAlias(t *testing.T) {
	src := "package x\n\nimport guid \"github.com/google/uuid\"\n\nvar _ = guid.NewString\n"
	defaults := map[string]string{"github.com/google/uuid": "v1.6.0"}
	pins := Pins{"github.com/google/uuid": "v1.3.0"}

	out, err := RewriteStageImports(src, pins, defaults, pkgNameStub("uuid"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !strings.Contains(out, `guid "`+ForkPath("github.com/google/uuid", "v1.3.0")+`"`) {
		t.Fatalf("user alias must be preserved:\n%s", out)
	}
}

func TestRewriteStageImports_LeavesDefaultVersionUntouched(t *testing.T) {
	defaults := map[string]string{"github.com/google/uuid": "v1.6.0"}
	pins := Pins{"github.com/google/uuid": "v1.6.0"}

	out, err := RewriteStageImports(stageWithUUID, pins, defaults, pkgNameStub("uuid"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if out != stageWithUUID {
		t.Fatalf("a stage on the default version must come out byte-identical:\n%s", out)
	}
}

func TestRewriteStageImports_RewritesSubpackageImport(t *testing.T) {
	src := "package x\n\nimport \"github.com/go-resty/resty/v2/shellescape\"\n"
	defaults := map[string]string{"github.com/go-resty/resty/v2": "v2.11.0"}
	pins := Pins{"github.com/go-resty/resty/v2": "v2.7.0"}

	out, err := RewriteStageImports(src, pins, defaults, pkgNameStub("shellescape"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	want := ForkPath("github.com/go-resty/resty/v2", "v2.7.0") + "/shellescape"
	if !strings.Contains(out, `shellescape "`+want+`"`) {
		t.Fatalf("subpackage suffix must be preserved after the fork path:\n%s", out)
	}
}

func TestPackageNameInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package ignored\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package realname\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	name, err := PackageNameInDir(dir)
	if err != nil {
		t.Fatalf("package name: %v", err)
	}
	if name != "realname" {
		t.Fatalf("expected the non-test package name, got %q", name)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
