package builder

import (
	"context"
	"testing"
)

func TestCheckBlockedImports(t *testing.T) {
	b := New(DefaultConfig())

	tests := []struct {
		name    string
		source  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "allowed imports",
			source: `package main

import (
	"fmt"
	"encoding/json"
)

func main() { fmt.Println("hello") }
`,
			wantErr: false,
		},
		{
			name: "blocked os/exec",
			source: `package main

import "os/exec"

func main() { exec.Command("ls") }
`,
			wantErr: true,
			errMsg:  "os/exec",
		},
		{
			name: "blocked syscall",
			source: `package main

import "syscall"

func main() { _ = syscall.Getpid() }
`,
			wantErr: true,
			errMsg:  "syscall",
		},
		{
			name: "blocked unsafe",
			source: `package main

import "unsafe"

func main() { _ = unsafe.Sizeof(0) }
`,
			wantErr: true,
			errMsg:  "unsafe",
		},
		{
			name: "blocked CGO directive",
			source: `package main

// #cgo LDFLAGS: -lm
import "C"

func main() {}
`,
			wantErr: true,
			errMsg:  "CGO",
		},
		{
			name: "blocked sub-package",
			source: `package main

import "syscall/js"

func main() {}
`,
			wantErr: true,
			errMsg:  "syscall",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.checkBlockedImports(tt.source)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSource(t *testing.T) {
	b := New(DefaultConfig())

	tests := []struct {
		name       string
		source     string
		wantErr    bool
		wantImport string
	}{
		{
			name: "valid plugin source",
			source: `package main

import (
	"fmt"
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type MyStage struct{}
func (s *MyStage) Init(config map[string]any) error { return nil }
func (s *MyStage) ProcessBatch(records []*sdk.Record) ([]*sdk.Record, error) { return records, nil }
func (s *MyStage) Close() error { return nil }
func main() { fmt.Println("starting"); sdk.Serve(&MyStage{}) }
`,
			wantErr:    false,
			wantImport: "github.com/conduix/conduix/plugin-sdk",
		},
		{
			name: "missing main function",
			source: `package main

import "fmt"

func helper() { fmt.Println("no main") }
`,
			wantErr: true,
		},
		{
			name: "wrong package name",
			source: `package lib

func main() {}
`,
			wantErr: true,
		},
		{
			name:    "source too large",
			source:  string(make([]byte, 2*1024*1024)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imports, err := b.ValidateSource(tt.source)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.wantImport != "" {
					found := false
					for _, imp := range imports {
						if imp == tt.wantImport {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected import %q in %v", tt.wantImport, imports)
					}
				}
			}
		})
	}
}

func TestGenerateGoMod(t *testing.T) {
	b := New(DefaultConfig())
	mod := b.generateGoMod("test-plugin")

	if !contains(mod, "conduix-plugin-test-plugin") {
		t.Errorf("expected module name in go.mod, got: %s", mod)
	}
	if !contains(mod, "plugin-sdk") {
		t.Errorf("expected plugin-sdk dependency in go.mod, got: %s", mod)
	}
}

func TestParsePlatform(t *testing.T) {
	b := New(DefaultConfig())

	tests := []struct {
		platform string
		goos     string
		goarch   string
	}{
		{"", "linux", "arm64"},
		{"linux/arm64", "linux", "arm64"},
		{"linux/amd64", "linux", "amd64"},
		{"darwin/arm64", "darwin", "arm64"},
		{"linux", "linux", "arm64"},
	}

	for _, tt := range tests {
		goos, goarch := b.parsePlatform(tt.platform)
		if goos != tt.goos || goarch != tt.goarch {
			t.Errorf("parsePlatform(%q) = (%q, %q), want (%q, %q)", tt.platform, goos, goarch, tt.goos, tt.goarch)
		}
	}
}

func TestBuildSourceTooLarge(t *testing.T) {
	b := New(DefaultConfig())

	_, err := b.Build(context.Background(), &BuildRequest{
		PluginName: "test",
		Version:    "v1",
		SourceCode: string(make([]byte, 2*1024*1024)),
	})

	if err == nil {
		t.Error("expected error for oversized source")
	}
}

func TestBuildBlockedImport(t *testing.T) {
	b := New(DefaultConfig())

	_, err := b.Build(context.Background(), &BuildRequest{
		PluginName: "test",
		Version:    "v1",
		SourceCode: `package main

import "os/exec"

func main() { exec.Command("ls") }
`,
	})

	if err == nil {
		t.Error("expected error for blocked import")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
