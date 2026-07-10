package handlers

import "testing"

func TestParseImportPaths(t *testing.T) {
	src := `package uuidtag
import (
	"strconv"
	"github.com/google/uuid"
	sdk "github.com/conduix/conduix/plugin-sdk"
)
func x() {}`
	paths, err := parseImportPaths(src)
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

func TestIsStdlibImport(t *testing.T) {
	cases := map[string]bool{
		"fmt":                        true,
		"encoding/json":              true,
		"strconv":                    true,
		"github.com/google/uuid":     false,
		"golang.org/x/sync/errgroup": false,
	}
	for imp, want := range cases {
		if got := isStdlibImport(imp); got != want {
			t.Errorf("isStdlibImport(%q)=%v want %v", imp, got, want)
		}
	}
}

func TestIsCoveredByAllowedModule(t *testing.T) {
	allowed := []string{"github.com/google/uuid", "github.com/shopspring/decimal"}
	if !isCoveredByAllowedModule("github.com/google/uuid", allowed) {
		t.Error("exact match should be covered")
	}
	if !isCoveredByAllowedModule("github.com/google/uuid/subpkg", allowed) {
		t.Error("subpackage should be covered")
	}
	if isCoveredByAllowedModule("github.com/evil/pkg", allowed) {
		t.Error("unlisted module must not be covered")
	}
	// prefix 유사(다른 모듈)는 커버 안 됨.
	if isCoveredByAllowedModule("github.com/google/uuidx", allowed) {
		t.Error("uuidx must not match uuid")
	}
}
