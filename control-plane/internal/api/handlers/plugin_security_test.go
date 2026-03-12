package handlers

import (
	"testing"
)

func TestCheckSourceSecurity_ValidCode(t *testing.T) {
	code := `package main

import (
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type MyStage struct{}

func (s *MyStage) Init(config map[string]any) error { return nil }
func (s *MyStage) Process(record map[string]any) (map[string]any, error) {
	record["processed"] = true
	return record, nil
}
func (s *MyStage) ProcessBatch(records []map[string]any) ([]map[string]any, error) { return nil, nil }
func (s *MyStage) Close() error { return nil }

var Stage sdk.NativeStage = &MyStage{}
`

	result := CheckSourceSecurity(code)
	if !result.Passed {
		t.Errorf("expected passed=true, got errors: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}
}

func TestCheckSourceSecurity_BlockedImport(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect string
	}{
		{
			name: "os/exec",
			code: `package main
import "os/exec"
var Stage interface{} = nil
`,
			expect: "os/exec",
		},
		{
			name: "syscall",
			code: `package main
import "syscall"
var Stage interface{} = nil
`,
			expect: "syscall",
		},
		{
			name: "unsafe",
			code: `package main
import "unsafe"
var Stage interface{} = nil
`,
			expect: "unsafe",
		},
		{
			name: "debug/elf",
			code: `package main
import "debug/elf"
var Stage interface{} = nil
`,
			expect: "debug/elf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckSourceSecurity(tt.code)
			if result.Passed {
				t.Error("expected passed=false for blocked import")
			}
			found := false
			for _, e := range result.Errors {
				if contains(e, tt.expect) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected error mentioning %q, got: %v", tt.expect, result.Errors)
			}
		})
	}
}

func TestCheckSourceSecurity_MissingStageVar(t *testing.T) {
	code := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	result := CheckSourceSecurity(code)
	if !result.Passed {
		t.Errorf("expected passed=true (missing Stage is warning, not error)")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about missing Stage variable")
	}
}

func TestCheckSourceSecurity_SyntaxError(t *testing.T) {
	code := `package main

func broken( {
`
	result := CheckSourceSecurity(code)
	if result.Passed {
		t.Error("expected passed=false for syntax error")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
