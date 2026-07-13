package handlers

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// conduixInternalModulePrefixes 는 stage 가 자유롭게 import 할 수 있는 내부 모듈(레지스트리 불필요).
var conduixInternalModulePrefixes = []string{
	"github.com/conduix/conduix/plugin-sdk",
	"github.com/conduix/conduix/pipeline-core",
	"github.com/conduix/conduix/shared",
}

// validateStageImports 는 native stage 소스가 import 하는 외부 패키지가 전부
// 허용됐는지(표준 라이브러리 + conduix 내부 모듈 + allowed_modules) 검증한다.
// 허용 안 된 외부 import 가 있으면 그 목록을 담은 에러를 반환한다(D5).
func validateStageImports(db *database.DB, sourceCode string) error {
	imports, err := parseImportPaths(sourceCode)
	if err != nil {
		return fmt.Errorf("소스 파싱 실패: %w", err)
	}

	// 허용 모듈 조회(active 만).
	var allowed []models.AllowedModule
	if err := db.Where("status = ?", "active").Find(&allowed).Error; err != nil {
		return fmt.Errorf("허용 모듈 조회 실패: %w", err)
	}
	allowedPaths := make([]string, len(allowed))
	for i, m := range allowed {
		allowedPaths[i] = m.ModulePath
	}

	var disallowed []string
	for _, imp := range imports {
		if isStdlibImport(imp) || hasAnyPrefix(imp, conduixInternalModulePrefixes) {
			continue
		}
		if !isCoveredByAllowedModule(imp, allowedPaths) {
			disallowed = append(disallowed, imp)
		}
	}
	if len(disallowed) > 0 {
		sort.Strings(disallowed)
		return fmt.Errorf("허용되지 않은 외부 모듈 import: %s — 먼저 모듈 레지스트리에 추가하세요(POST /api/v1/modules)", strings.Join(disallowed, ", "))
	}
	return nil
}

// parseImportPaths 는 Go 소스의 import 경로 목록을 추출한다.
func parseImportPaths(sourceCode string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "stage.go", sourceCode, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		paths = append(paths, p)
	}
	return paths, nil
}

// isStdlibImport 는 표준 라이브러리 import 인지 판별한다.
// 표준 라이브러리 경로는 첫 세그먼트에 점(.)이 없다(도메인이 아님). 예: "fmt", "encoding/json".
func isStdlibImport(importPath string) bool {
	first := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		first = importPath[:i]
	}
	return !strings.Contains(first, ".")
}

// isCoveredByAllowedModule 은 import 경로가 허용 모듈 중 하나에 속하는지(모듈 경로 == import 이거나
// 그 하위 패키지) 판별한다. 예: allowed "github.com/foo/bar" 는 "github.com/foo/bar/baz" 도 커버.
func isCoveredByAllowedModule(importPath string, allowedPaths []string) bool {
	for _, m := range allowedPaths {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if s == p || strings.HasPrefix(s, p+"/") {
			return true
		}
	}
	return false
}

// testRunnerMain 은 인-에디터 테스트 빌드에 주입하는 실행 러너(package main).
// 실제 RunnerBuilder 와 동일 계약을 쓴다: 사용자 소스를 별도 subpackage(pluginstage)로 두고
// import 해 `pluginstage.Stage{}`(구조체) 를 생성한다. 실제 빌드는 registry_custom.go 가
// `plugin_<name>.Stage{}` 를 쓰므로(runner_builder.GenerateRegistryCustom), 테스트도 struct Stage
// 를 요구해야 "에디터 테스트 통과=실제 빌드 통과" 가 성립한다.
// (구 방식은 사용자 소스를 package main 으로 같은 디렉토리에 둬 package clash + var Stage 요구로
// 실제 빌드 계약과 어긋났다 — BUG#6.)
const testRunnerMain = `package main

import (
	"encoding/json"
	"os"

	sdk "github.com/conduix/conduix/plugin-sdk"
	pluginstage "conduix-plugin-test/pluginstage"
)

func main() {
	var stage sdk.NativeStage = &pluginstage.Stage{}
	var in struct {
		Config     map[string]any   ` + "`json:\"config\"`" + `
		SampleData []map[string]any ` + "`json:\"sample_data\"`" + `
	}
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "decode input: " + err.Error()})
		return
	}
	if err := stage.Init(in.Config); err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "init: " + err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(in.SampleData))
	for _, rec := range in.SampleData {
		r, err := stage.Process(rec)
		if err != nil {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"error": "process: " + err.Error()})
			return
		}
		if r != nil {
			out = append(out, r)
		}
	}
	_ = stage.Close()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"records": out})
}
`

// buildTestGoMod 는 인-에디터 테스트 빌드용 go.mod 를 레지스트리 기반으로 생성한다.
// TestNativePlugin(임시 빌드)이 실제 runner 빌드와 같은 의존성 버전을 쓰게 해,
// "에디터 테스트는 실패/성공인데 실제 빌드는 반대" 인 불일치를 없앤다.
// plugin-sdk 는 CONDUIX_SDK_PATH(런타임 이미지의 로컬 소스)로 replace 한다.
func buildTestGoMod(db *database.DB) string {
	var b strings.Builder
	b.WriteString("module conduix-plugin-test\n\ngo 1.26\n\n")
	b.WriteString("require github.com/conduix/conduix/plugin-sdk v0.0.0\n")

	var allowed []models.AllowedModule
	if err := db.Where("status = ?", "active").Order("module_path asc").Find(&allowed).Error; err == nil && len(allowed) > 0 {
		b.WriteString("\nrequire (\n")
		for _, m := range allowed {
			fmt.Fprintf(&b, "\t%s %s\n", m.ModulePath, m.Version)
		}
		b.WriteString(")\n")
	}

	sdkPath := os.Getenv("CONDUIX_SDK_PATH")
	if sdkPath == "" {
		sdkPath = "/app/plugin-sdk"
	}
	fmt.Fprintf(&b, "\nreplace github.com/conduix/conduix/plugin-sdk => %s\n", sdkPath)
	return b.String()
}
