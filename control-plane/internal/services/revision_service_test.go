package services

import (
	"testing"
)

func TestComputeDiffSummary_Create(t *testing.T) {
	result := computeDiffSummary("", "line1\nline2\nline3", "create")
	if result != "+3 lines (new)" {
		t.Errorf("got %q, want %q", result, "+3 lines (new)")
	}
}

func TestComputeDiffSummary_Delete(t *testing.T) {
	result := computeDiffSummary("line1\nline2", "", "delete")
	if result != "-2 lines (deleted)" {
		t.Errorf("got %q, want %q", result, "-2 lines (deleted)")
	}
}

func TestComputeDiffSummary_Update(t *testing.T) {
	old := "line1\nline2\nline3"
	new_ := "line1\nline2_modified\nline3\nline4"
	result := computeDiffSummary(old, new_, "update")
	// line2_modified (added) + line4 (added) = +2, line2 (removed) = -1
	if result != "+2 -1 lines" {
		t.Errorf("got %q, want %q", result, "+2 -1 lines")
	}
}

func TestComputeDiffSummary_UpdateNoOld(t *testing.T) {
	result := computeDiffSummary("", "line1\nline2", "update")
	if result != "+2 lines" {
		t.Errorf("got %q, want %q", result, "+2 lines")
	}
}

func TestComputeDiffSummary_EmptyAction(t *testing.T) {
	result := computeDiffSummary("", "", "unknown")
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"single", 1},
		{"line1\nline2", 2},
		{"line1\nline2\nline3", 3},
	}

	for _, tt := range tests {
		got := countLines(tt.input)
		if got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
