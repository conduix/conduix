package dependency

import (
	"errors"
	"strings"
	"testing"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func allowedFixture() []models.AllowedModule {
	return []models.AllowedModule{
		{ModulePath: "github.com/google/uuid", Version: "v1.6.0", Status: "active"},
		{ModulePath: "github.com/go-resty/resty/v2", Version: "v2.11.0", Status: "active"},
		{ModulePath: "github.com/lib/pq", Version: "v1.10.9", Status: "active", SingleVersionOnly: true},
	}
}

func TestResolvePins_DefaultsForNewImports(t *testing.T) {
	src := `package pluginstage
import (
	"fmt"
	"github.com/google/uuid"
	sdk "github.com/conduix/conduix/plugin-sdk"
)`
	imports, err := ParseImports(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pins, err := ResolvePins(imports, nil, allowedFixture())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(pins) != 1 || pins["github.com/google/uuid"] != "v1.6.0" {
		t.Fatalf("expected only uuid pinned at default, got %v", pins)
	}
}

func TestResolvePins_KeepsExistingPin(t *testing.T) {
	imports := []string{"github.com/google/uuid"}
	existing := Pins{"github.com/google/uuid": "v1.3.0"}
	pins, err := ResolvePins(imports, existing, allowedFixture())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pins["github.com/google/uuid"] != "v1.3.0" {
		t.Fatalf("existing pin must survive a default bump, got %v", pins)
	}
}

func TestResolvePins_DropsUnusedPin(t *testing.T) {
	existing := Pins{"github.com/go-resty/resty/v2": "v2.7.0"}
	pins, err := ResolvePins([]string{"github.com/google/uuid"}, existing, allowedFixture())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := pins["github.com/go-resty/resty/v2"]; ok {
		t.Fatalf("pin for a module no longer imported must be dropped, got %v", pins)
	}
}

func TestResolvePins_RejectsUnregisteredImport(t *testing.T) {
	_, err := ResolvePins([]string{"github.com/unknown/thing"}, nil, allowedFixture())
	if err == nil || !strings.Contains(err.Error(), "허용되지 않은 외부 모듈") {
		t.Fatalf("expected D5 rejection, got %v", err)
	}
}

func TestResolvePins_RejectsNonDefaultOnSingleVersionOnly(t *testing.T) {
	existing := Pins{"github.com/lib/pq": "v1.10.0"}
	_, err := ResolvePins([]string{"github.com/lib/pq"}, existing, allowedFixture())
	if err == nil || !strings.Contains(err.Error(), "single_version_only") {
		t.Fatalf("expected single_version_only rejection, got %v", err)
	}
}

func TestResolvePins_SubpackageMapsToLongestModule(t *testing.T) {
	allowed := append(allowedFixture(), models.AllowedModule{ModulePath: "github.com/google/uuid/extra", Version: "v0.2.0"})
	pins, err := ResolvePins([]string{"github.com/google/uuid/extra/deep"}, nil, allowed)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pins["github.com/google/uuid/extra"] != "v0.2.0" || len(pins) != 1 {
		t.Fatalf("subpackage must map to the longest matching module, got %v", pins)
	}
}

// 불변식 2 의 회귀선: 모든 stage 가 기본 버전이면 plugin go.mod 는 기존 생성기 출력과 같아야 한다.
func TestPluginGoMod_DefaultsMatchLegacyLayout(t *testing.T) {
	defaults := Defaults(allowedFixture())
	pins := Pins{"github.com/google/uuid": "v1.6.0"}
	got := PluginGoMod("uuidtag", pins, defaults)
	want := "module github.com/conduix/plugins/uuidtag\n\ngo 1.27\n" +
		"\nrequire github.com/conduix/conduix/plugin-sdk v0.0.0\n" +
		"\nrequire (\n\tgithub.com/google/uuid v1.6.0\n)\n" +
		"\nreplace github.com/conduix/conduix/plugin-sdk => ../../../plugin-sdk\n"
	if got != want {
		t.Fatalf("plugin go.mod layout drifted.\n got: %q\nwant: %q", got, want)
	}
}

func TestPluginGoMod_ForkedVersionUsesForkPath(t *testing.T) {
	defaults := Defaults(allowedFixture())
	pins := Pins{"github.com/google/uuid": "v1.3.0"}
	got := PluginGoMod("uuidtag", pins, defaults)
	if !strings.Contains(got, ForkPath("github.com/google/uuid", "v1.3.0")+" v0.0.0") {
		t.Fatalf("non-default pin must require the fork path, got:\n%s", got)
	}
	if strings.Contains(got, "github.com/google/uuid v1.3.0") {
		t.Fatalf("non-default pin must not require the original path, got:\n%s", got)
	}
}

func TestMainRequireBlock_DefaultsMatchLegacyLayout(t *testing.T) {
	defaults := Defaults(allowedFixture())
	names := []string{"crm_enrichment", "score_classifier"}
	pins := map[string]Pins{
		"crm_enrichment":   {"github.com/google/uuid": "v1.6.0"},
		"score_classifier": {"github.com/google/uuid": "v1.6.0"},
	}
	got := MainRequireBlock(names, pins, defaults, nil)
	want := "\nrequire (\n" +
		"\tgithub.com/conduix/plugins/crm_enrichment v0.0.0\n" +
		"\tgithub.com/conduix/plugins/score_classifier v0.0.0\n" +
		"\tgithub.com/google/uuid v1.6.0\n" +
		")\n\nreplace (\n" +
		"\tgithub.com/conduix/plugins/crm_enrichment => ./plugins/crm_enrichment\n" +
		"\tgithub.com/conduix/plugins/score_classifier => ./plugins/score_classifier\n" +
		")\n"
	if got != want {
		t.Fatalf("main require block drifted.\n got: %q\nwant: %q", got, want)
	}
}

func TestMainRequireBlock_WithForks(t *testing.T) {
	defaults := Defaults(allowedFixture())
	names := []string{"a", "b"}
	pins := map[string]Pins{
		"a": {"github.com/google/uuid": "v1.3.0"},
		"b": {"github.com/google/uuid": "v1.6.0"},
	}
	forks := CollectForks(map[string]string{"a": "p-a", "b": "p-b"}, names, pins, defaults)
	if len(forks) != 1 || forks[0].Version != "v1.3.0" || len(forks[0].PluginIDs) != 1 || forks[0].PluginIDs[0] != "p-a" {
		t.Fatalf("expected one fork owned by p-a, got %+v", forks)
	}
	got := MainRequireBlock(names, pins, defaults, forks)
	fp := ForkPath("github.com/google/uuid", "v1.3.0")
	for _, want := range []string{
		"\t" + fp + " v0.0.0\n",
		"\tgithub.com/google/uuid v1.6.0\n",
		"\t" + fp + " => ./forked/" + forks[0].DirName + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestForkPath_Injective(t *testing.T) {
	cases := [][2]string{
		{"github.com/go-resty/resty", "v0.8.0"},
		{"github.com/go-resty/resty/v2", "v2.7.0"},
		{"github.com/go-resty/resty_v2", "v2.7.0"},
		{"github.com/a/b", "v1.0.0+incompatible"},
		{"github.com/a/b", "v1.0.0-rc.1"},
		{"github.com/a", "b__v__v1.0.0"},
		{"github.com/a__v__b", "v1.0.0"},
	}
	seen := map[string][2]string{}
	for _, c := range cases {
		name := ForkDirName(c[0], c[1])
		if prev, dup := seen[name]; dup {
			t.Fatalf("fork dir collision: %v and %v both map to %q", prev, c, name)
		}
		seen[name] = c
	}
}

func TestTestGoMod_UsesPinnedVersions(t *testing.T) {
	got := TestGoMod(Pins{"github.com/google/uuid": "v1.3.0"}, "/app/plugin-sdk")
	want := "module conduix-plugin-test\n\ngo 1.26\n\n" +
		"require github.com/conduix/conduix/plugin-sdk v0.0.0\n" +
		"\nrequire (\n\tgithub.com/google/uuid v1.3.0\n)\n" +
		"\nreplace github.com/conduix/conduix/plugin-sdk => /app/plugin-sdk\n"
	if got != want {
		t.Fatalf("test go.mod drifted.\n got: %q\nwant: %q", got, want)
	}
}

func TestWorkspaceGoMod_UsesPinnedVersions(t *testing.T) {
	got := WorkspaceGoMod(Pins{"github.com/google/uuid": "v1.3.0"}, "/src/plugin-sdk")
	if !strings.Contains(got, "module conduix-plugin-workspace") ||
		!strings.Contains(got, "\tgithub.com/google/uuid v1.3.0\n") ||
		!strings.Contains(got, "replace github.com/conduix/conduix/plugin-sdk => /src/plugin-sdk") {
		t.Fatalf("workspace go.mod unexpected:\n%s", got)
	}
}

func TestPinsRoundTrip(t *testing.T) {
	p := Pins{"github.com/google/uuid": "v1.3.0"}
	enc, err := p.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got := ParsePins(enc); got["github.com/google/uuid"] != "v1.3.0" {
		t.Fatalf("round trip lost the pin: %v", got)
	}
	if ParsePins("") != nil || ParsePins("not-json") != nil {
		t.Fatal("legacy/broken dep_versions must resolve to nil pins")
	}
}

// 아래 셋은 handlers/stage_import_validation_test.go 에서 옮겨온 회귀 테스트다.
func TestParseImports(t *testing.T) {
	src := `package uuidtag
import (
	"strconv"
	"github.com/google/uuid"
	sdk "github.com/conduix/conduix/plugin-sdk"
)
func x() {}`
	paths, err := ParseImports(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]bool{"strconv": true, "github.com/google/uuid": true, "github.com/conduix/conduix/plugin-sdk": true}
	if len(paths) != len(want) {
		t.Fatalf("got %v", paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected import %q", p)
		}
	}
}

func TestIsStdlib(t *testing.T) {
	cases := map[string]bool{
		"fmt":                        true,
		"encoding/json":              true,
		"strconv":                    true,
		"github.com/google/uuid":     false,
		"golang.org/x/sync/errgroup": false,
	}
	for imp, want := range cases {
		if got := IsStdlib(imp); got != want {
			t.Errorf("IsStdlib(%q)=%v want %v", imp, got, want)
		}
	}
}

func TestOwningModule(t *testing.T) {
	allowed := []string{"github.com/google/uuid", "github.com/shopspring/decimal"}
	if m, ok := OwningModule("github.com/google/uuid", allowed); !ok || m != "github.com/google/uuid" {
		t.Error("exact match should be covered")
	}
	if m, ok := OwningModule("github.com/google/uuid/subpkg", allowed); !ok || m != "github.com/google/uuid" {
		t.Error("subpackage should map to its module")
	}
	if _, ok := OwningModule("github.com/evil/pkg", allowed); ok {
		t.Error("unlisted module must not be covered")
	}
	// prefix 유사(다른 모듈)는 커버 안 됨.
	if _, ok := OwningModule("github.com/google/uuidx", allowed); ok {
		t.Error("uuidx must not match uuid")
	}
}

// 미등록 import 는 문자열이 아니라 타입 있는 에러여야 핸들러가 import 목록을 구조화해 내려줄 수 있다.
func TestResolvePins_MissingModulesErrorIsTyped(t *testing.T) {
	_, err := ResolvePins([]string{"fmt", "github.com/not/registered/sub", "github.com/other/x"}, nil, nil)
	var missing *MissingModulesError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingModulesError, got %T: %v", err, err)
	}
	if len(missing.Imports) != 2 || missing.Imports[0] != "github.com/not/registered/sub" || missing.Imports[1] != "github.com/other/x" {
		t.Fatalf("imports must be the sorted unregistered set, got %v", missing.Imports)
	}
	if !strings.Contains(err.Error(), "github.com/not/registered/sub") {
		t.Fatalf("message must still name the imports, got %q", err.Error())
	}
}
