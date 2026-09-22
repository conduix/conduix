// Package dependency 는 "stage 가 쓸 모듈 버전" 정책의 단일 소유자다.
//
// 이 정책의 소비자는 셋이다 — runner 빌더, 인-에디터 테스트 빌드, LSP workspace.
// 셋이 각자 레지스트리에서 go.mod 를 만들면 자동완성·테스트·실제 빌드의 버전이
// 조용히 갈린다(ADR-0005). 여기 함수들만 쓰고, DB 접근은 호출부가 한다.
package dependency

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// Pins 는 stage 하나가 고정한 모듈 버전이다(module_path → version).
type Pins map[string]string

// MissingModulesError 는 소스가 레지스트리에 없는 외부 모듈을 import 할 때의 에러다.
// 문자열이 아니라 타입으로 돌려주는 이유: 핸들러가 import 목록을 구조화해 내려줘야
// UI 가 "추가하고 저장" 원클릭을 만들 수 있다. 문자열만 있으면 사용자가 탭을 옮겨 다시 타이핑한다.
type MissingModulesError struct {
	Imports []string // 정렬된 미등록 import 경로
}

func (e *MissingModulesError) Error() string {
	return fmt.Sprintf("허용되지 않은 외부 모듈 import: %s — 먼저 모듈 레지스트리에 추가하세요(POST /api/v1/modules)",
		strings.Join(e.Imports, ", "))
}

// InternalModulePrefixes 는 stage 가 레지스트리 등록 없이 import 할 수 있는 conduix 내부 모듈.
var InternalModulePrefixes = []string{
	"github.com/conduix/conduix/plugin-sdk",
	"github.com/conduix/conduix/pipeline-core",
	"github.com/conduix/conduix/shared",
}

// ParsePins 는 Plugin.DepVersions(JSON) 를 Pins 로 읽는다.
// 빈 값·깨진 JSON 은 레거시로 보고 nil 을 돌려준다 — 호출부가 기본 버전으로 채운다.
func ParsePins(depVersions string) Pins {
	if strings.TrimSpace(depVersions) == "" {
		return nil
	}
	var p Pins
	if err := json.Unmarshal([]byte(depVersions), &p); err != nil || len(p) == 0 {
		return nil
	}
	return p
}

// Encode 는 Pins 를 Plugin.DepVersions 에 저장할 JSON 으로 만든다. 빈 pins 는 빈 문자열.
func (p Pins) Encode() (string, error) {
	if len(p) == 0 {
		return "", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Defaults 는 허용 모듈의 기본 버전 맵을 만든다(module_path → 기본 version).
func Defaults(allowed []models.AllowedModule) map[string]string {
	d := make(map[string]string, len(allowed))
	for _, m := range allowed {
		d[m.ModulePath] = m.Version
	}
	return d
}

// ResolvePins 는 stage 소스의 import 목록에서 이 stage 가 쓸 모듈 버전을 확정한다.
//
// 규칙:
//   - 표준 라이브러리·conduix 내부 모듈은 무시한다(레지스트리 불필요).
//   - 외부 import 는 허용 모듈 중 하나에 속해야 한다. 아니면 에러(D5 검증).
//   - 이미 고정된 버전(existing)이 있으면 유지한다 — 기본 버전을 올려도 기존 stage 는 안 깨진다.
//   - 없으면 기본 버전으로 새로 고정한다.
//   - SingleVersionOnly 모듈을 기본과 다르게 고정하려 하면 에러(init 전역 등록 모듈은 fork 불가).
//
// 반환된 Pins 는 이 소스가 실제로 import 하는 모듈만 담는다 — 더 이상 쓰지 않는 모듈의
// 고정값은 자연히 떨어져 나간다.
func ResolvePins(imports []string, existing Pins, allowed []models.AllowedModule) (Pins, error) {
	byPath := make(map[string]models.AllowedModule, len(allowed))
	paths := make([]string, 0, len(allowed))
	for _, m := range allowed {
		byPath[m.ModulePath] = m
		paths = append(paths, m.ModulePath)
	}

	pins := Pins{}
	var disallowed []string
	for _, imp := range imports {
		if IsStdlib(imp) || HasAnyPrefix(imp, InternalModulePrefixes) {
			continue
		}
		modPath, ok := OwningModule(imp, paths)
		if !ok {
			disallowed = append(disallowed, imp)
			continue
		}
		if _, done := pins[modPath]; done {
			continue
		}
		mod := byPath[modPath]
		version := mod.Version
		if v, ok := existing[modPath]; ok && v != "" {
			version = v
		}
		if mod.SingleVersionOnly && version != mod.Version {
			return nil, fmt.Errorf("%s 는 단일 버전만 허용하는 모듈입니다(single_version_only) — 고정 %s, 기본 %s. 기본 버전으로 맞추세요",
				modPath, version, mod.Version)
		}
		pins[modPath] = version
	}

	if len(disallowed) > 0 {
		sort.Strings(disallowed)
		return nil, &MissingModulesError{Imports: disallowed}
	}
	return pins, nil
}

// ParseImports 는 Go 소스의 import 경로 목록을 추출한다.
func ParseImports(sourceCode string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "stage.go", sourceCode, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		paths = append(paths, strings.Trim(imp.Path.Value, `"`))
	}
	return paths, nil
}

// IsStdlib 는 표준 라이브러리 import 인지 판별한다.
// 표준 라이브러리 경로는 첫 세그먼트에 점(.)이 없다(도메인이 아님). 예: "fmt", "encoding/json".
func IsStdlib(importPath string) bool {
	first := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		first = importPath[:i]
	}
	return !strings.Contains(first, ".")
}

// OwningModule 은 import 경로를 소유한 허용 모듈 경로를 찾는다.
// 여러 모듈이 커버하면 가장 긴(= 가장 구체적인) 경로를 고른다.
func OwningModule(importPath string, modulePaths []string) (string, bool) {
	best := ""
	for _, m := range modulePaths {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			if len(m) > len(best) {
				best = m
			}
		}
	}
	return best, best != ""
}

// HasAnyPrefix 는 s 가 prefixes 중 하나와 같거나 그 하위 경로인지 본다.
func HasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if s == p || strings.HasPrefix(s, p+"/") {
			return true
		}
	}
	return false
}

// SortedModules 는 pins 의 모듈 경로를 정렬해 돌려준다(go.mod 출력 결정성).
func (p Pins) SortedModules() []string {
	out := make([]string, 0, len(p))
	for m := range p {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Fingerprint 는 stage 들의 고정 버전을 재빌드 판정 해시에 섞을 문자열로 만든다.
//
// 빌더와 리졸버가 **같은 입력(DB 의 dep_versions)** 으로 같은 값을 얻어야 하므로
// 레지스트리 기본값을 참조하지 않는다 — 기본값을 끼우면 리졸버가 그것을 따로 조회하게 되고,
// 두 곳의 조회 시점이 어긋나면 빌더는 재빌드하는데 리졸버는 옛 버전을 유효하다고 판정한다.
//
// 아무 stage 도 버전을 고정하지 않았으면(전부 레거시) 빈 문자열이라 기존 해시와 값이 같다 —
// 이 변경만으로 전면 재빌드가 일어나지 않게 하는 장치다(ADR-0005 불변식 2).
func Fingerprint(depVersionsByPluginID map[string]string) string {
	ids := make([]string, 0, len(depVersionsByPluginID))
	for id, dv := range depVersionsByPluginID {
		if pins := ParsePins(dv); pins != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		pins := ParsePins(depVersionsByPluginID[id])
		fmt.Fprintf(&b, "%s:", id)
		for _, mod := range pins.SortedModules() {
			fmt.Fprintf(&b, "%s@%s,", mod, pins[mod])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
