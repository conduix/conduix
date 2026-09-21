package builder

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conduix/conduix/control-plane/internal/dependency"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func depsTestBuilder(t *testing.T, allowed []models.AllowedModule) *RunnerBuilder {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gdb.AutoMigrate(&models.AllowedModule{}, &models.Plugin{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for i := range allowed {
		if allowed[i].Status == "" {
			allowed[i].Status = "active"
		}
		if err := gdb.Create(&allowed[i]).Error; err != nil {
			t.Fatalf("seed module: %v", err)
		}
	}
	return &RunnerBuilder{db: gdb, logger: slog.Default()}
}

const uuidStageSource = `package uuidtag

import (
	"github.com/google/uuid"
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type Stage struct{}

var _ sdk.NativeStage = (*Stage)(nil)

func (s *Stage) Init(map[string]any) error { return nil }
func (s *Stage) Process(r map[string]any) (map[string]any, error) {
	r["id"] = uuid.NewString()
	return r, nil
}
func (s *Stage) Close() error { return nil }
`

// 레거시 stage(dep_versions 빈 값)는 빌드 시점 기본 버전으로 고정값이 만들어져야 한다.
// resolveDeps 는 순수 계산이라 Backfill 에만 담고, 저장은 Build 가 해시 계산 전에 한다.
func TestResolveDeps_BackfillsLegacyPinsWithoutWriting(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	plugins := []models.Plugin{{ID: "p1", Name: "uuid-tag", Type: "native", Status: "active", SourceCode: uuidStageSource}}
	if err := rb.db.Create(&plugins[0]).Error; err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	resolved, err := rb.resolveDeps(plugins)
	if err != nil {
		t.Fatalf("resolveDeps: %v", err)
	}
	if got := resolved.Pins["uuid_tag"]["github.com/google/uuid"]; got != "v1.6.0" {
		t.Fatalf("legacy stage should pin the default version, got %q", got)
	}
	if !strings.Contains(resolved.Backfill["p1"], "v1.6.0") {
		t.Fatalf("expected backfill entry, got %v", resolved.Backfill)
	}
	var stored models.Plugin
	if err := rb.db.First(&stored, "id = ?", "p1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.DepVersions != "" {
		t.Fatalf("resolveDeps itself must not write dep_versions (Build persists them), got %q", stored.DepVersions)
	}

	rb.saveBackfilledPins(resolved)
	if err := rb.db.First(&stored, "id = ?", "p1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(stored.DepVersions, "v1.6.0") {
		t.Fatalf("saveBackfilledPins should persist the pins, got %q", stored.DepVersions)
	}
}

// 저장된 고정값은 기본 버전이 올라가도 유지돼야 한다(기존 stage 불변).
func TestResolveDeps_KeepsStoredPinAgainstNewerDefault(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	plugins := []models.Plugin{
		{ID: "p1", Name: "old", SourceCode: uuidStageSource, DepVersions: `{"github.com/google/uuid":"v1.3.0"}`},
		{ID: "p2", Name: "new", SourceCode: uuidStageSource, DepVersions: `{"github.com/google/uuid":"v1.6.0"}`},
	}
	resolved, err := rb.resolveDeps(plugins)
	if err != nil {
		t.Fatalf("resolveDeps: %v", err)
	}
	if got := resolved.Pins["old"]["github.com/google/uuid"]; got != "v1.3.0" {
		t.Fatalf("stored pin must survive, got %q", got)
	}
	if len(resolved.Forks) != 1 || resolved.Forks[0].Version != "v1.3.0" {
		t.Fatalf("the non-default pin must become exactly one fork, got %+v", resolved.Forks)
	}
	if ids := resolved.Forks[0].PluginIDs; len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("fork must be attributed to p1, got %v", ids)
	}
}

// 불변식 2: 모든 stage 가 기본 버전이면 fork 가 없고 지문도 비어 해시가 예전과 같다.
func TestResolveDeps_AllDefaultsProduceNoForks(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	plugins := []models.Plugin{
		{ID: "p1", Name: "a", SourceCode: uuidStageSource, DepVersions: `{"github.com/google/uuid":"v1.6.0"}`},
		{ID: "p2", Name: "b", SourceCode: uuidStageSource},
	}
	resolved, err := rb.resolveDeps(plugins)
	if err != nil {
		t.Fatalf("resolveDeps: %v", err)
	}
	if len(resolved.Forks) != 0 {
		t.Fatalf("defaults only must not fork, got %+v", resolved.Forks)
	}
}

// 지문은 고정값이 하나도 없을 때 빈 문자열이어야 한다 — 이 기능 도입만으로
// 기존 ready 버전이 전부 무효화돼 전면 재빌드가 일어나면 안 된다.
func TestPluginDepFingerprint_EmptyForLegacyOnly(t *testing.T) {
	legacy := []models.Plugin{{ID: "p1"}, {ID: "p2", DepVersions: ""}}
	if fp := PluginDepFingerprint(legacy); fp != "" {
		t.Fatalf("legacy-only plugin set must yield an empty fingerprint, got %q", fp)
	}
	hashes := map[string]string{"p1": "h1", "p2": "h2"}
	if CombinedSourceHash(hashes, "core", PluginDepFingerprint(legacy)) != CombinedSourceHash(hashes, "core", "") {
		t.Fatal("introducing dep_versions must not change the hash of a legacy-only plugin set")
	}
}

// stage 가 소스는 그대로 두고 버전만 올리면 재빌드돼야 한다.
func TestPluginDepFingerprint_ChangesWithPinnedVersion(t *testing.T) {
	before := []models.Plugin{{ID: "p1", DepVersions: `{"github.com/google/uuid":"v1.3.0"}`}}
	after := []models.Plugin{{ID: "p1", DepVersions: `{"github.com/google/uuid":"v1.6.0"}`}}
	hashes := map[string]string{"p1": "same-source-hash"}
	if CombinedSourceHash(hashes, "core", PluginDepFingerprint(before)) ==
		CombinedSourceHash(hashes, "core", PluginDepFingerprint(after)) {
		t.Fatal("a version bump with unchanged source must change the combined hash")
	}
}

// 해소 불가한 레거시 stage(예: 그 사이 retire 된 모듈을 import) 하나가 다른 stage 의
// 배포까지 막으면 안 된다. 고정 버전 없이 두고 진행해 해소 도입 전과 같은 동작으로 떨어진다.
func TestResolveDeps_UnresolvableLegacyStageDoesNotFailTheBuild(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	plugins := []models.Plugin{
		{ID: "p1", Name: "broken", SourceCode: "package x\nimport \"github.com/not/registered\"\n"},
		{ID: "p2", Name: "ok", SourceCode: uuidStageSource},
	}
	resolved, err := rb.resolveDeps(plugins)
	if err != nil {
		t.Fatalf("one unresolvable stage must not fail the whole build: %v", err)
	}
	if len(resolved.Pins["broken"]) != 0 {
		t.Fatalf("unresolvable stage must carry no pins, got %v", resolved.Pins["broken"])
	}
	if _, backfilled := resolved.Backfill["p1"]; backfilled {
		t.Fatal("unresolvable stage must not be backfilled")
	}
	if got := resolved.Pins["ok"]["github.com/google/uuid"]; got != "v1.6.0" {
		t.Fatalf("the healthy stage must still resolve, got %q", got)
	}
}

// stage 소스 재작성은 fork 디렉토리의 실제 package 절을 읽어 alias 를 붙인다.
// 디렉토리가 없으면(= fork 를 아직 안 만듦) 에러로 드러나야 한다 — 조용히 원본을 쓰면
// 그 stage 만 기본 버전으로 빌드돼 고정이 무시된다.
func TestStageSourceFor_RewritesUsingForkPackageName(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	work := t.TempDir()
	forkDir := filepath.Join(work, forkedDirRoot, dependency.ForkDirName("github.com/google/uuid", "v1.3.0"))
	if err := os.MkdirAll(forkDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(forkDir, "uuid.go"), []byte("package uuid\n\nfunc NewString() string { return \"\" }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	resolved := &resolvedDeps{
		Names:    []string{"tag"},
		Pins:     map[string]dependency.Pins{"tag": {"github.com/google/uuid": "v1.3.0"}},
		Defaults: map[string]string{"github.com/google/uuid": "v1.6.0"},
		Forks:    []dependency.Fork{{ModulePath: "github.com/google/uuid", Version: "v1.3.0"}},
	}
	out, err := rb.stageSourceFor(work, "tag", models.Plugin{Name: "tag", SourceCode: uuidStageSource}, resolved)
	if err != nil {
		t.Fatalf("stageSourceFor: %v", err)
	}
	if !strings.Contains(out, `uuid "`+dependency.ForkPath("github.com/google/uuid", "v1.3.0")+`"`) {
		t.Fatalf("expected the import rewritten with an alias:\n%s", out)
	}
	if !strings.Contains(out, "uuid.NewString()") {
		t.Fatalf("user code body must be untouched:\n%s", out)
	}
}

// fork 가 없으면 소스는 원본 그대로여야 한다(불변식 2).
func TestStageSourceFor_NoForksReturnsSourceUnchanged(t *testing.T) {
	rb := depsTestBuilder(t, nil)
	resolved := &resolvedDeps{
		Names:    []string{"tag"},
		Pins:     map[string]dependency.Pins{"tag": {"github.com/google/uuid": "v1.6.0"}},
		Defaults: map[string]string{"github.com/google/uuid": "v1.6.0"},
	}
	out, err := rb.stageSourceFor(t.TempDir(), "tag", models.Plugin{Name: "tag", SourceCode: uuidStageSource}, resolved)
	if err != nil {
		t.Fatalf("stageSourceFor: %v", err)
	}
	if out != uuidStageSource {
		t.Fatalf("source must be byte-identical when nothing is forked")
	}
}

// single_version_only 모듈을 비기본 버전으로 고정한 stage 가 있으면 빌드를 세운다 —
// 두 벌이 링크되면 init() 중복 등록으로 런타임 panic 이 나므로, 빌드 실패가 낫다.
func TestResolveDeps_RejectsForkOfSingleVersionOnlyModule(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0", SingleVersionOnly: true},
	})
	plugins := []models.Plugin{
		{ID: "p1", Name: "a", SourceCode: uuidStageSource, DepVersions: `{"github.com/google/uuid":"v1.3.0"}`},
	}
	_, err := rb.resolveDeps(plugins)
	if err == nil || !strings.Contains(err.Error(), "단일 버전") {
		t.Fatalf("expected a single_version_only rejection, got %v", err)
	}
}

// fork 가 없으면 자가점검을 건너뛴다 — 링크 구성이 이전과 같아 새로 터질 것이 없고,
// 모든 빌드에 실행을 얹으면 불변식 2(기본 버전만일 때 이전과 동일)가 깨진다.
func TestRunInitCheck_SkippedWithoutForks(t *testing.T) {
	rb := &RunnerBuilder{config: &RunnerBuilderConfig{Platform: "linux/arm64"}, logger: slog.Default()}
	var logBuf strings.Builder
	// batchJobDir 가 존재하지 않아도 통과해야 한다 — 아무 명령도 실행하지 않는다는 뜻.
	if err := rb.runInitCheck(context.Background(), "/nonexistent", &resolvedDeps{}, &logBuf); err != nil {
		t.Fatalf("no forks must mean no init check: %v", err)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("nothing should be logged when skipped, got %q", logBuf.String())
	}
}

func TestRunInitCheck_RespectsOffSwitch(t *testing.T) {
	rb := &RunnerBuilder{
		config: &RunnerBuilderConfig{Platform: "linux/arm64", InitCheck: InitCheckOff},
		logger: slog.Default(),
	}
	var logBuf strings.Builder
	resolved := &resolvedDeps{Forks: []dependency.Fork{{ModulePath: "m", Version: "v1"}}}
	if err := rb.runInitCheck(context.Background(), "/nonexistent", resolved, &logBuf); err != nil {
		t.Fatalf("InitCheckOff must skip the check: %v", err)
	}
}

func TestFirstPanicLine(t *testing.T) {
	out := "some log\npanic: sql: Register called twice for driver pq\n\ngoroutine 1:\n"
	if got := firstPanicLine(out); got != "panic: sql: Register called twice for driver pq" {
		t.Fatalf("got %q", got)
	}
	if got := firstPanicLine("only one line\n\n"); got != "only one line" {
		t.Fatalf("fallback should be the last non-empty line, got %q", got)
	}
}

func TestForkSummary_NamesModulesAndStages(t *testing.T) {
	got := forkSummary([]dependency.Fork{
		{ModulePath: "github.com/lib/pq", Version: "v1.10.0", PluginIDs: []string{"p1", "p2"}},
	})
	if !strings.Contains(got, "github.com/lib/pq@v1.10.0") || !strings.Contains(got, "p1,p2") {
		t.Fatalf("the message must name the module and the stages behind it, got %q", got)
	}
}

// 백필을 저장한 뒤 메모리 목록에도 반영해야, 빌더가 만든 해시와 리졸버가 DB 로 다시 계산한
// 해시가 같다. 어긋나면 성공 직후 core_changed 로 재빌드가 걸린다(리뷰 3번).
func TestApplyBackfill_FingerprintMatchesStoredPins(t *testing.T) {
	rb := depsTestBuilder(t, []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0"},
	})
	plugins := []models.Plugin{{ID: "p1", Name: "uuid-tag", Type: "native", Status: "active", SourceCode: uuidStageSource}}
	if err := rb.db.Create(&plugins[0]).Error; err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	resolved, err := rb.resolveDeps(plugins)
	if err != nil {
		t.Fatalf("resolveDeps: %v", err)
	}

	if PluginDepFingerprint(plugins) != "" {
		t.Fatalf("레거시 목록의 지문은 비어 있어야 한다")
	}
	rb.saveBackfilledPins(resolved)
	applyBackfill(plugins, resolved)

	var stored []models.Plugin
	if err := rb.db.Find(&stored).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	inMemory := PluginDepFingerprint(plugins)
	fromDB := PluginDepFingerprint(stored)
	if inMemory == "" || inMemory != fromDB {
		t.Fatalf("fingerprint mismatch: memory=%q db=%q", inMemory, fromDB)
	}
	if DepsFingerprintHash(inMemory) == "" || DepsFingerprintHash("") != "" {
		t.Fatalf("DepsFingerprintHash: 비어 있지 않은 지문은 해시, 빈 지문은 빈 문자열이어야 한다")
	}
}
