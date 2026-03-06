// Package builder compiles user-submitted Go source code into native plugin binaries.
// It enforces security constraints (blocked imports, timeouts, size limits).
package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds builder configuration.
type Config struct {
	BuildTimeout   time.Duration `json:"build_timeout"`
	MaxSourceSize  int           `json:"max_source_size"` // bytes
	GoProxy        string        `json:"goproxy"`
	BlockedImports []string      `json:"blocked_imports"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		BuildTimeout:  60 * time.Second,
		MaxSourceSize: 1 * 1024 * 1024, // 1MB
		GoProxy:       "https://proxy.golang.org,direct",
		BlockedImports: []string{
			"os/exec",
			"syscall",
			"unsafe",
			"plugin",
			"net/http/cgi",
			"net/http/fcgi",
			"debug/buildinfo",
		},
	}
}

// BuildRequest contains source files for a plugin build.
type BuildRequest struct {
	PluginID   string
	PluginName string
	Version    string
	SourceCode string // main.go content
	GoMod      string // go.mod content (optional, auto-generated if empty)
	Platform   string // GOOS/GOARCH (default: linux/arm64)
}

// BuildResult contains the output of a successful build.
type BuildResult struct {
	Binary   []byte
	Checksum string // SHA256
	Size     int64
	BuildLog string
	Duration time.Duration
}

// Builder compiles Go source code into plugin binaries.
type Builder struct {
	config *Config
	logger *slog.Logger
}

// New creates a new Builder with the given config.
func New(cfg *Config) *Builder {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Builder{
		config: cfg,
		logger: slog.Default().With("component", "builder"),
	}
}

// Build compiles the source code into a native binary.
func (b *Builder) Build(ctx context.Context, req *BuildRequest) (*BuildResult, error) {
	start := time.Now()
	var logBuf strings.Builder

	fmt.Fprintf(&logBuf, "[%s] Starting build for plugin %s@%s\n", time.Now().Format(time.RFC3339), req.PluginName, req.Version)

	// Validate source size
	if len(req.SourceCode) > b.config.MaxSourceSize {
		return nil, fmt.Errorf("source code exceeds max size (%d > %d bytes)", len(req.SourceCode), b.config.MaxSourceSize)
	}

	// Check blocked imports
	if err := b.checkBlockedImports(req.SourceCode); err != nil {
		return nil, fmt.Errorf("blocked import: %w", err)
	}
	logBuf.WriteString("  Import validation passed\n")

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "conduix-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write source files
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(req.SourceCode), 0o644); err != nil {
		return nil, fmt.Errorf("write main.go: %w", err)
	}

	goMod := req.GoMod
	if goMod == "" {
		goMod = b.generateGoMod(req.PluginName)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return nil, fmt.Errorf("write go.mod: %w", err)
	}

	logBuf.WriteString("  Source files written\n")

	// Parse platform for cross-compilation
	goos, goarch := b.parsePlatform(req.Platform)
	platformEnv := []string{"GOOS=" + goos, "GOARCH=" + goarch}

	// Build with timeout
	buildCtx, cancel := context.WithTimeout(ctx, b.config.BuildTimeout)
	defer cancel()

	binaryPath := filepath.Join(tmpDir, "plugin.bin")

	// go mod tidy
	logBuf.WriteString("  Running go mod tidy...\n")
	tidyOut, err := b.runCommand(buildCtx, tmpDir, nil, []string{"go", "mod", "tidy"})
	logBuf.WriteString(tidyOut)
	if err != nil {
		fmt.Fprintf(&logBuf, "  go mod tidy failed: %v\n", err)
		return &BuildResult{BuildLog: logBuf.String(), Duration: time.Since(start)}, fmt.Errorf("go mod tidy: %w", err)
	}

	// go build
	logBuf.WriteString("  Running go build...\n")
	buildOut, err := b.runCommand(buildCtx, tmpDir, platformEnv, []string{
		"go", "build",
		"-ldflags=-s -w",
		"-trimpath",
		"-o", binaryPath,
		".",
	})
	logBuf.WriteString(buildOut)
	if err != nil {
		fmt.Fprintf(&logBuf, "  Build failed: %v\n", err)
		return &BuildResult{BuildLog: logBuf.String(), Duration: time.Since(start)}, fmt.Errorf("go build: %w", err)
	}

	logBuf.WriteString("  Build successful\n")

	// Read binary
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("read binary: %w", err)
	}

	// Calculate checksum
	hash := sha256.Sum256(binary)
	checksum := hex.EncodeToString(hash[:])

	duration := time.Since(start)
	fmt.Fprintf(&logBuf, "  Binary size: %d bytes, checksum: %s\n", len(binary), checksum)
	fmt.Fprintf(&logBuf, "  Build completed in %s\n", duration)

	return &BuildResult{
		Binary:   binary,
		Checksum: checksum,
		Size:     int64(len(binary)),
		BuildLog: logBuf.String(),
		Duration: duration,
	}, nil
}

// checkBlockedImports parses Go source and checks for blocked imports.
func (b *Builder) checkBlockedImports(source string) error {
	// Check for CGO directives via string scan (before parsing, since `import "C"` causes parse errors)
	if strings.Contains(source, "#cgo") || strings.Contains(source, `import "C"`) {
		return fmt.Errorf("CGO directives are not allowed")
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", source, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse source: %w", err)
	}

	blocked := make(map[string]bool, len(b.config.BlockedImports))
	for _, imp := range b.config.BlockedImports {
		blocked[imp] = true
	}

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if blocked[path] {
			return fmt.Errorf("import %q is not allowed for security reasons", path)
		}
		for _, blockedPkg := range b.config.BlockedImports {
			if strings.HasPrefix(path, blockedPkg+"/") {
				return fmt.Errorf("import %q is not allowed (sub-package of blocked %q)", path, blockedPkg)
			}
		}
	}

	return nil
}

// runCommand executes a command and returns combined output.
func (b *Builder) runCommand(ctx context.Context, dir string, extraEnv []string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOPROXY="+b.config.GoProxy,
	)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Env, extraEnv...)
	}

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// generateGoMod creates a default go.mod for the plugin.
func (b *Builder) generateGoMod(pluginName string) string {
	return fmt.Sprintf(`module conduix-plugin-%s

go 1.26

require github.com/conduix/conduix/plugin-sdk v0.0.0
`, pluginName)
}

// parsePlatform splits "linux/arm64" into GOOS and GOARCH.
func (b *Builder) parsePlatform(platform string) (goos, goarch string) {
	if platform == "" {
		return "linux", "arm64"
	}
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], "arm64"
}

// ValidateSource checks source code without building.
// Returns the list of imports found and any validation errors.
func (b *Builder) ValidateSource(source string) (imports []string, err error) {
	if len(source) > b.config.MaxSourceSize {
		return nil, fmt.Errorf("source code exceeds max size (%d > %d bytes)", len(source), b.config.MaxSourceSize)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", source, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Check package name
	if f.Name.Name != "main" {
		return nil, fmt.Errorf("package must be 'main', got %q", f.Name.Name)
	}

	// Check for main function (need full parse for this)
	fFull, err := parser.ParseFile(fset, "main.go", source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	hasMain := false
	for _, decl := range fFull.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			hasMain = true
			break
		}
	}
	if !hasMain {
		return nil, fmt.Errorf("source must contain a main() function")
	}

	// Collect imports and check blocked
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		imports = append(imports, path)
	}

	if err := b.checkBlockedImports(source); err != nil {
		return imports, err
	}

	return imports, nil
}
