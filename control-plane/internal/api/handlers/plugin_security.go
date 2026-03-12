package handlers

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// blockedImports 보안상 금지된 Go import 패키지 목록
var blockedImports = []string{
	"os/exec",
	"syscall",
	"unsafe",
	"plugin",
	"debug/",
	"runtime/cgo",
	"C",
}

// SecurityCheckResult 보안 검사 결과
type SecurityCheckResult struct {
	Passed   bool     `json:"passed"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// CheckSourceSecurity Go 소스 코드의 보안 검사 (go/ast 기반)
// 금지된 import 사용 여부를 검사하고, var Stage 선언 존재 여부를 확인한다.
func CheckSourceSecurity(sourceCode string) *SecurityCheckResult {
	result := &SecurityCheckResult{Passed: true}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", sourceCode, parser.ImportsOnly)
	if err != nil {
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("Go syntax error: %v", err))
		return result
	}

	// 금지된 import 검사
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		for _, blocked := range blockedImports {
			if importPath == blocked || strings.HasPrefix(importPath, blocked) {
				result.Passed = false
				result.Errors = append(result.Errors, fmt.Sprintf("blocked import: %q is not allowed for security reasons", importPath))
			}
		}
	}

	// 전체 파싱 (var Stage 선언 확인)
	fFull, err := parser.ParseFile(fset, "main.go", sourceCode, 0)
	if err != nil {
		// import 검사는 통과했으나 전체 파싱 실패
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("Go syntax error: %v", err))
		return result
	}

	hasStageVar := false
	for _, decl := range fFull.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "Stage" {
					hasStageVar = true
				}
			}
		}
	}

	if !hasStageVar {
		result.Warnings = append(result.Warnings, "missing 'var Stage sdk.NativeStage = &YourStage{}' declaration — required for plugin registration")
	}

	return result
}
