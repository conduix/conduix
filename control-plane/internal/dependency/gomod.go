package dependency

import (
	"fmt"
	"sort"
	"strings"
)

// Fork 는 기본과 다른 버전이라 복사·재작성해 링크할 모듈 하나다.
type Fork struct {
	ModulePath string   `json:"module_path"`
	Version    string   `json:"version"`
	ForkPath   string   `json:"fork_path"`
	DirName    string   `json:"dir_name"`
	PluginIDs  []string `json:"plugin_ids,omitempty"`
}

// requireLines 는 pins 를 go.mod require 항목 줄로 만든다.
// 기본과 다른 버전은 fork 가상 경로를 v0.0.0 으로 require 한다(실체는 replace 로 로컬 복사본).
func requireLines(pins Pins, defaults map[string]string) []string {
	lines := make([]string, 0, len(pins))
	for _, mod := range pins.SortedModules() {
		v := pins[mod]
		if def, ok := defaults[mod]; ok && def != v {
			lines = append(lines, fmt.Sprintf("%s v0.0.0", ForkPath(mod, v)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", mod, v))
	}
	sort.Strings(lines)
	return lines
}

// PluginGoMod 는 plugin 개별 go.mod 를 생성한다.
// 사용자 자유입력(p.GoMod)을 쓰지 않으므로 stage 마다 버전이 제멋대로 갈릴 수 없다 —
// 갈리는 경우는 오직 이 stage 가 명시적으로 고정한 비기본 버전(fork)뿐이다.
func PluginGoMod(name string, pins Pins, defaults map[string]string) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "module github.com/conduix/plugins/%s\n\ngo 1.27\n", name)
	buf.WriteString("\nrequire github.com/conduix/conduix/plugin-sdk v0.0.0\n")
	if lines := requireLines(pins, defaults); len(lines) > 0 {
		buf.WriteString("\nrequire (\n")
		for _, l := range lines {
			fmt.Fprintf(&buf, "\t%s\n", l)
		}
		buf.WriteString(")\n")
	}
	// plugin-sdk 는 로컬 모듈이라 replace 필요(batch-job 의 replace 와 동일 상대경로 기준).
	buf.WriteString("\nreplace github.com/conduix/conduix/plugin-sdk => ../../../plugin-sdk\n")
	return buf.String()
}

// MainRequireBlock 은 batch-job(pipeline-runner) go.mod 끝에 덧붙일 블록이다.
// 플러그인 로컬 모듈 require/replace + 각 stage 가 고정한 외부 모듈 require 를 낸다.
// fork 가 있으면 가상 경로를 로컬 복사본(./forked/<dir>)으로 replace 한다.
//
// pluginNames 는 sanitize 된 플러그인 이름(입력 순서 보존), pluginPins 는 이름별 고정 버전.
func MainRequireBlock(pluginNames []string, pluginPins map[string]Pins, defaults map[string]string, forks []Fork) string {
	var buf strings.Builder
	buf.WriteString("\nrequire (\n")
	for _, name := range pluginNames {
		fmt.Fprintf(&buf, "\tgithub.com/conduix/plugins/%s v0.0.0\n", name)
	}
	for _, l := range mergedRequireLines(pluginNames, pluginPins, defaults) {
		fmt.Fprintf(&buf, "\t%s\n", l)
	}
	buf.WriteString(")\n\nreplace (\n")
	for _, name := range pluginNames {
		fmt.Fprintf(&buf, "\tgithub.com/conduix/plugins/%s => ./plugins/%s\n", name, name)
	}
	for _, f := range forks {
		fmt.Fprintf(&buf, "\t%s => ./forked/%s\n", f.ForkPath, f.DirName)
	}
	buf.WriteString(")\n")
	return buf.String()
}

// mergedRequireLines 는 모든 플러그인의 pins 를 합친 require 줄이다.
// 같은 모듈을 서로 다른 버전으로 고정한 stage 들이 있어도, 기본이 아닌 쪽은 각기 다른
// fork 경로가 되므로 줄이 겹치지 않는다.
func mergedRequireLines(pluginNames []string, pluginPins map[string]Pins, defaults map[string]string) []string {
	seen := map[string]bool{}
	var lines []string
	for _, name := range pluginNames {
		for _, l := range requireLines(pluginPins[name], defaults) {
			if !seen[l] {
				seen[l] = true
				lines = append(lines, l)
			}
		}
	}
	sort.Strings(lines)
	return lines
}

// CollectForks 는 플러그인들의 pins 에서 기본과 다른 (module, version) 조합을 모은다.
// 결과는 (module_path, version) 정렬 — 빌드 산출물 결정성 유지.
func CollectForks(pluginIDsByName map[string]string, pluginNames []string, pluginPins map[string]Pins, defaults map[string]string) []Fork {
	byKey := map[string]*Fork{}
	for _, name := range pluginNames {
		for mod, v := range pluginPins[name] {
			if def, ok := defaults[mod]; !ok || def == v {
				continue
			}
			key := mod + "@" + v
			f, ok := byKey[key]
			if !ok {
				f = &Fork{ModulePath: mod, Version: v, ForkPath: ForkPath(mod, v), DirName: ForkDirName(mod, v)}
				byKey[key] = f
			}
			if id := pluginIDsByName[name]; id != "" {
				f.PluginIDs = append(f.PluginIDs, id)
			}
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	forks := make([]Fork, 0, len(keys))
	for _, k := range keys {
		f := byKey[k]
		sort.Strings(f.PluginIDs)
		forks = append(forks, *f)
	}
	return forks
}

// TestGoMod 는 인-에디터 테스트 빌드용 go.mod 다.
// stage 가 고정한 버전으로 컴파일해야 "에디터 테스트 통과 = 실제 빌드 통과" 가 성립한다.
// 단일 stage 빌드라 fork 는 불필요 — 고정 버전을 원래 경로로 require 한다.
func TestGoMod(pins Pins, sdkPath string) string {
	return singleModuleGoMod("conduix-plugin-test", pins, sdkPath)
}

// WorkspaceGoMod 는 gopls workspace 용 go.mod 다. 자동완성이 실제 빌드와 같은 버전을 보게 한다.
func WorkspaceGoMod(pins Pins, sdkPath string) string {
	return singleModuleGoMod("conduix-plugin-workspace", pins, sdkPath)
}

// singleModuleGoMod 는 단일 stage 를 다루는 임시 모듈(테스트·workspace)의 go.mod 다.
// fork 가 필요 없으므로 고정 버전을 원래 모듈 경로로 그대로 require 한다.
func singleModuleGoMod(moduleName string, pins Pins, sdkPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo 1.26\n\n", moduleName)
	b.WriteString("require github.com/conduix/conduix/plugin-sdk v0.0.0\n")
	if mods := pins.SortedModules(); len(mods) > 0 {
		b.WriteString("\nrequire (\n")
		for _, m := range mods {
			fmt.Fprintf(&b, "\t%s %s\n", m, pins[m])
		}
		b.WriteString(")\n")
	}
	if sdkPath != "" {
		fmt.Fprintf(&b, "\nreplace github.com/conduix/conduix/plugin-sdk => %s\n", sdkPath)
	}
	return b.String()
}
