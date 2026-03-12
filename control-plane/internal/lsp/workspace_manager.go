// Package lsp provides gopls LSP proxy for Monaco Editor Go autocomplete.
package lsp

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WorkspaceManager 사용자별 gopls workspace 관리
// 각 세션에 임시 디렉토리를 생성하고, gopls가 참조할 go.mod + 소스를 배치
type WorkspaceManager struct {
	baseDir    string
	sdkModPath string // plugin-sdk 모듈 경로 (로컬 replace용)
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

// NewWorkspaceManager WorkspaceManager 생성
func NewWorkspaceManager(sdkModPath string) *WorkspaceManager {
	baseDir := filepath.Join(os.TempDir(), "conduix-lsp-workspaces")
	_ = os.MkdirAll(baseDir, 0o755)

	wm := &WorkspaceManager{
		baseDir:    baseDir,
		sdkModPath: sdkModPath,
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

	// go.mod 생성 (plugin-sdk local replace)
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
	return ws, nil
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

	// go.mod 업데이트 (사용자가 커스텀 모듈을 추가한 경우)
	if goMod != "" {
		if err := os.WriteFile(filepath.Join(ws.Dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			return fmt.Errorf("write go.mod: %w", err)
		}
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

// generateGoMod workspace용 go.mod 생성
func (wm *WorkspaceManager) generateGoMod() string {
	goMod := `module conduix-plugin-workspace

go 1.26

require github.com/conduix/conduix/plugin-sdk v0.0.0
`
	if wm.sdkModPath != "" {
		goMod += fmt.Sprintf("\nreplace github.com/conduix/conduix/plugin-sdk => %s\n", wm.sdkModPath)
	}
	return goMod
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
