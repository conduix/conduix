// Package lsp provides gopls LSP proxy for Monaco Editor Go autocomplete.
package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AllowedModuleRef 는 gopls workspace go.mod 에 넣을 허용 모듈(경로+고정버전).
// lsp 패키지가 models/db 에 의존하지 않도록 최소 형태만 받는다.
type AllowedModuleRef struct {
	ModulePath string
	Version    string
}

// WorkspaceManager 사용자별 gopls workspace 관리
// 각 세션에 임시 디렉토리를 생성하고, gopls가 참조할 go.mod + 소스를 배치
type WorkspaceManager struct {
	baseDir    string
	sdkModPath string // plugin-sdk 모듈 경로 (로컬 replace용)
	// modulesFn 은 현재 허용 모듈 목록을 반환한다(레지스트리 조회 주입). nil 이면 외부 모듈 없음.
	modulesFn  func() ([]AllowedModuleRef, error)
	goProxy    string // go mod download 시 GOPROXY
	goCacheDir string // GOCACHE/GOMODCACHE 공유(재다운로드 회피)
	workspaces map[string]*Workspace
	mu         sync.Mutex
	logger     *slog.Logger
}

// Workspace 단일 사용자 workspace
type Workspace struct {
	ID        string
	Dir       string
	CreatedAt time.Time
	LastUsed  time.Time
}

// NewWorkspaceManager WorkspaceManager 생성.
// modulesFn: 허용 모듈 목록 provider(레지스트리 조회 주입). nil 허용.
func NewWorkspaceManager(sdkModPath string, modulesFn func() ([]AllowedModuleRef, error)) *WorkspaceManager {
	baseDir := filepath.Join(os.TempDir(), "conduix-lsp-workspaces")
	_ = os.MkdirAll(baseDir, 0o755)

	// runner 빌드와 GOMODCACHE 를 공유해 이미 받은 모듈 재다운로드를 피한다.
	goCacheDir := os.Getenv("CONDUIX_BUILD_CACHE_DIR")
	if goCacheDir == "" {
		goCacheDir = filepath.Join(os.TempDir(), "conduix-runner-cache")
	}
	goProxy := os.Getenv("GOPROXY")
	if goProxy == "" {
		goProxy = "https://proxy.golang.org,direct"
	}

	wm := &WorkspaceManager{
		baseDir:    baseDir,
		sdkModPath: sdkModPath,
		modulesFn:  modulesFn,
		goProxy:    goProxy,
		goCacheDir: goCacheDir,
		workspaces: make(map[string]*Workspace),
		logger:     slog.Default().With("component", "workspace-manager"),
	}

	// idle workspace 자동 정리 (5분마다)
	go wm.cleanupLoop()

	return wm
}

// GetOrCreate workspace 가져오기 (없으면 생성)
func (wm *WorkspaceManager) GetOrCreate(sessionID string) (*Workspace, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if ws, ok := wm.workspaces[sessionID]; ok {
		ws.LastUsed = time.Now()
		return ws, nil
	}

	dir, err := os.MkdirTemp(wm.baseDir, fmt.Sprintf("ws-%s-*", sessionID[:8]))
	if err != nil {
		return nil, fmt.Errorf("create workspace dir: %w", err)
	}

	// go.mod 생성 (plugin-sdk local replace + 허용 모듈 require)
	goMod := wm.generateGoMod()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write go.mod: %w", err)
	}

	// 기본 main.go 생성 (gopls가 패키지를 인식하도록)
	mainGo := wm.generateDefaultMain()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write main.go: %w", err)
	}

	ws := &Workspace{
		ID:        sessionID,
		Dir:       dir,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
	wm.workspaces[sessionID] = ws

	wm.logger.Info("Workspace created", "session_id", sessionID, "dir", dir)

	// 허용 모듈을 디스크에 받아둔다(gopls 가 외부 심볼을 분석하려면 모듈 캐시에 있어야 함).
	// 비동기 — 첫 자동완성이 약간 늦을 수 있으나 workspace 생성을 블록하지 않는다.
	go wm.downloadModules(dir)

	return ws, nil
}

// downloadModules 는 workspace 에서 go mod download 를 실행해 gopls 가 참조할
// 외부 모듈 소스를 GOMODCACHE 에 받아둔다. GOMODCACHE 는 runner 빌드와 공유한다.
func (wm *WorkspaceManager) downloadModules(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "mod", "download")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOPROXY="+wm.goProxy,
		"GOCACHE="+filepath.Join(wm.goCacheDir, "gocache"),
		"GOMODCACHE="+filepath.Join(wm.goCacheDir, "gopath", "pkg", "mod"),
		"GOPATH="+filepath.Join(wm.goCacheDir, "gopath"),
		"HOME="+dir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		wm.logger.Warn("go mod download failed (external symbol autocomplete may be limited)",
			"dir", dir, "error", err, "output", strings.TrimSpace(string(out)))
	}
}

// SyncSource 사용자 코드를 workspace에 동기화
func (wm *WorkspaceManager) SyncSource(sessionID, sourceCode, goMod string) error {
	wm.mu.Lock()
	ws, ok := wm.workspaces[sessionID]
	wm.mu.Unlock()

	if !ok {
		return fmt.Errorf("workspace not found: %s", sessionID)
	}

	ws.LastUsed = time.Now()

	// main.go 업데이트
	if sourceCode != "" {
		if err := os.WriteFile(filepath.Join(ws.Dir, "main.go"), []byte(sourceCode), 0o644); err != nil {
			return fmt.Errorf("write main.go: %w", err)
		}
	}

	// go.mod 는 사용자 입력(goMod 인자)이 아니라 레지스트리 기반으로 재생성한다.
	// (D2: 의존성 버전은 레지스트리 단일원천. 사용자 자유입력 go.mod 는 무시.)
	// goMod 인자는 하위호환으로 남기되 사용하지 않는다.
	_ = goMod
	regenerated := wm.generateGoMod()
	if err := os.WriteFile(filepath.Join(ws.Dir, "go.mod"), []byte(regenerated), 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	return nil
}

// Remove workspace 삭제
func (wm *WorkspaceManager) Remove(sessionID string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if ws, ok := wm.workspaces[sessionID]; ok {
		_ = os.RemoveAll(ws.Dir)
		delete(wm.workspaces, sessionID)
		wm.logger.Info("Workspace removed", "session_id", sessionID)
	}
}

const idleTimeout = 5 * time.Minute

// cleanupLoop idle workspace 정리 루프
func (wm *WorkspaceManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		wm.mu.Lock()
		now := time.Now()
		for id, ws := range wm.workspaces {
			if now.Sub(ws.LastUsed) > idleTimeout {
				_ = os.RemoveAll(ws.Dir)
				delete(wm.workspaces, id)
				wm.logger.Info("Workspace cleaned up (idle)", "session_id", id)
			}
		}
		wm.mu.Unlock()
	}
}

// generateGoMod workspace용 go.mod 생성.
// plugin-sdk(로컬 replace) + 허용 모듈(레지스트리 고정 버전) require. 사용자 자유입력 go.mod 는
// 쓰지 않는다 — gopls 자동완성 버전 = 실제 빌드 버전(둘 다 레지스트리) 일치를 보장한다.
func (wm *WorkspaceManager) generateGoMod() string {
	var b strings.Builder
	b.WriteString("module conduix-plugin-workspace\n\ngo 1.26\n\n")
	b.WriteString("require github.com/conduix/conduix/plugin-sdk v0.0.0\n")

	if wm.modulesFn != nil {
		if mods, err := wm.modulesFn(); err == nil && len(mods) > 0 {
			b.WriteString("\nrequire (\n")
			for _, m := range mods {
				fmt.Fprintf(&b, "\t%s %s\n", m.ModulePath, m.Version)
			}
			b.WriteString(")\n")
		} else if err != nil {
			wm.logger.Warn("allowed modules fetch failed for workspace go.mod", "error", err)
		}
	}

	if wm.sdkModPath != "" {
		fmt.Fprintf(&b, "\nreplace github.com/conduix/conduix/plugin-sdk => %s\n", wm.sdkModPath)
	}
	return b.String()
}

// generateDefaultMain 기본 main.go
func (wm *WorkspaceManager) generateDefaultMain() string {
	return `package main

import (
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type MyStage struct {
	sdk.BaseNativeStage
}

func (s *MyStage) Init(config map[string]any) error {
	return nil
}

func (s *MyStage) Process(record map[string]any) (map[string]any, error) {
	return record, nil
}

var Stage sdk.NativeStage = &MyStage{}
`
}
