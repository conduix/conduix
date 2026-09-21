package dependency

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RewriteModuleTree 는 복사된 모듈 트리의 자기참조 import 를 fork 경로로 바꾼다.
//
// 이 재작성을 생략하면 에러 없이 버전이 섞인다: 복사본 안의 패키지들이 서로를 여전히
// 원래 경로로 import 하므로, go 가 그 경로를 기본 버전 모듈로 해석해 한 stage 안에서
// 두 버전이 동시에 링크된다(CONFLICT.md §5 실험 4). 조용히 잘못되는 종류의 실패라
// fork 를 만들 때 반드시 함께 해야 한다.
//
// go.mod 의 module 줄도 함께 바꿔 복사본이 자기 이름을 fork 경로로 갖게 한다.
func RewriteModuleTree(dir, oldPath, newPath string) error {
	if err := rewriteGoModModuleLine(filepath.Join(dir, "go.mod"), newPath); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "testdata" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out, cerr := rewriteImportsInFile(path, string(src), func(imp string) (string, bool) {
			if imp == oldPath {
				return newPath, true
			}
			if strings.HasPrefix(imp, oldPath+"/") {
				return newPath + imp[len(oldPath):], true
			}
			return "", false
		})
		if cerr != nil {
			// 파싱 불가한 파일(build tag 조합용 조각 등)은 건너뛴다 — 재작성 대상이 아니면
			// 그대로 두는 편이 낫고, 실제로 컴파일되는 파일이면 go build 가 잡는다.
			return nil
		}
		if out == string(src) {
			return nil
		}
		info, serr := os.Stat(path)
		if serr != nil {
			return serr
		}
		return os.WriteFile(path, []byte(out), info.Mode().Perm())
	})
}

// RewriteStageImports 는 stage 소스에서 비기본 버전으로 고정된 모듈의 import 를 fork 경로로 바꾼다.
//
// fork 경로의 마지막 요소는 mangle 된 이름이라 패키지 이름을 유추할 수 없다. 그래서
// 사용자가 alias 를 안 붙였으면 복사본의 실제 package 절을 읽어 alias 로 달아준다 —
// 사용자 코드 본문(uuid.NewString() 같은 호출)은 손대지 않는다.
//
// pkgNameOf 는 (modulePath, version) 의 fork 디렉토리에서 import 된 패키지의 이름을 돌려준다.
func RewriteStageImports(src string, pins Pins, defaults map[string]string, pkgNameOf func(modulePath, version, importPath string) (string, error)) (string, error) {
	var resolveErr error
	out, err := rewriteImportsInFileWithAlias(src, func(imp, alias string) (string, string, bool) {
		mod, ok := OwningModule(imp, pinsModulePaths(pins))
		if !ok {
			return "", "", false
		}
		v := pins[mod]
		if def, isDefault := defaults[mod]; !isDefault || def == v {
			return "", "", false
		}
		newImp := ForkPath(mod, v) + imp[len(mod):]
		if alias != "" {
			return newImp, alias, true
		}
		name, perr := pkgNameOf(mod, v, imp)
		if perr != nil {
			resolveErr = fmt.Errorf("fork 패키지 이름 확인 실패(%s): %w", imp, perr)
			return "", "", false
		}
		return newImp, name, true
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return out, err
}

func pinsModulePaths(pins Pins) []string {
	return pins.SortedModules()
}

// rewriteImportsInFile 은 import 경로만 치환한다(alias 는 건드리지 않음).
func rewriteImportsInFile(filename, src string, mapPath func(string) (string, bool)) (string, error) {
	return rewriteImports(filename, src, func(imp, _ string) (string, string, bool) {
		np, ok := mapPath(imp)
		return np, "", ok
	}, false)
}

// rewriteImportsInFileWithAlias 는 경로와 alias 를 함께 치환한다.
func rewriteImportsInFileWithAlias(src string, mapSpec func(imp, alias string) (string, string, bool)) (string, error) {
	return rewriteImports("stage.go", src, mapSpec, true)
}

// rewriteImports 는 go/parser 로 import 위치를 찾아 바이트 단위로 치환한다.
// AST 를 다시 출력(printer)하면 파일 전체 포맷이 바뀌므로, 위치 기반 치환으로
// 사용자 코드의 원본 서식을 그대로 보존한다.
func rewriteImports(filename, src string, mapSpec func(imp, alias string) (string, string, bool), withAlias bool) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly)
	if err != nil {
		return "", err
	}

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, spec := range f.Imports {
		imp, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil {
			continue
		}
		alias := ""
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		newImp, newAlias, ok := mapSpec(imp, alias)
		if !ok {
			continue
		}
		start := fset.Position(spec.Pos()).Offset
		end := fset.Position(spec.End()).Offset
		text := strconv.Quote(newImp)
		if withAlias && newAlias != "" {
			text = newAlias + " " + text
		} else if alias != "" {
			text = alias + " " + text
		}
		edits = append(edits, edit{start: start, end: end, text: text})
	}
	if len(edits) == 0 {
		return src, nil
	}

	// 뒤에서부터 적용해야 앞선 오프셋이 밀리지 않는다.
	out := src
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = out[:e.start] + e.text + out[e.end:]
	}
	return out, nil
}

// rewriteGoModModuleLine 은 go.mod 의 module 줄을 새 경로로 바꾼다.
// go.mod 가 없으면(과거 GOPATH 시절 모듈) 아무것도 하지 않는다 — 빌더가 새로 써준다.
func rewriteGoModModuleLine(goModPath, newPath string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") || trimmed == "module" {
			lines[i] = "module " + newPath
			break
		}
	}
	return os.WriteFile(goModPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// PackageNameInDir 은 디렉토리의 첫 컴파일 대상 .go 파일에서 package 절을 읽는다.
func PackageNameInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if perr != nil {
			continue
		}
		if f.Name != nil && f.Name.Name != "" {
			return f.Name.Name, nil
		}
	}
	return "", fmt.Errorf("%s 에서 package 절을 찾지 못했습니다", dir)
}
