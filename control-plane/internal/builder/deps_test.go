package builder

import (
	"log/slog"
	"strings"
	"testing"

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
// 저장은 빌드 성공 후이므로 resolveDeps 단계에서는 Backfill 에만 담긴다.
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
		t.Fatalf("resolveDeps must not write dep_versions before the build succeeds, got %q", stored.DepVersions)
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
