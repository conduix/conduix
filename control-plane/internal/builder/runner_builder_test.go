package builder

import (
	"testing"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func TestCombinedSourceHash_Deterministic(t *testing.T) {
	hashes := map[string]string{
		"plugin-a": "hash-aaa",
		"plugin-b": "hash-bbb",
		"plugin-c": "hash-ccc",
	}

	hash1 := CombinedSourceHash(hashes)
	hash2 := CombinedSourceHash(hashes)

	if hash1 != hash2 {
		t.Errorf("CombinedSourceHash is not deterministic: %s != %s", hash1, hash2)
	}
	if len(hash1) != 64 {
		t.Errorf("expected SHA256 hex length 64, got %d", len(hash1))
	}
}

func TestCombinedSourceHash_OrderIndependent(t *testing.T) {
	// 같은 데이터, 다른 삽입 순서 → 결과가 같아야 함 (정렬 기반)
	h1 := map[string]string{"a": "1", "b": "2"}
	h2 := map[string]string{"b": "2", "a": "1"}

	if CombinedSourceHash(h1) != CombinedSourceHash(h2) {
		t.Error("CombinedSourceHash should be order-independent")
	}
}

func TestCombinedSourceHash_DifferentHashes(t *testing.T) {
	h1 := map[string]string{"a": "1"}
	h2 := map[string]string{"a": "2"}

	if CombinedSourceHash(h1) == CombinedSourceHash(h2) {
		t.Error("different source hashes should produce different combined hashes")
	}
}

func TestGenerateRegistryCustom(t *testing.T) {
	plugins := []models.Plugin{
		{Name: "crm-enrichment"},
		{Name: "score-classifier"},
	}

	code := GenerateRegistryCustom(plugins)

	// 기본 구조 확인
	if !contains(code, "package main") {
		t.Error("expected package main")
	}
	if !contains(code, "DO NOT EDIT") {
		t.Error("expected DO NOT EDIT header")
	}
	if !contains(code, "stream.RegisterCustomStage") {
		t.Error("expected RegisterCustomStage calls")
	}
	if !contains(code, "plugin_crm_enrichment") {
		t.Error("expected crm_enrichment import alias")
	}
	if !contains(code, "plugin_score_classifier") {
		t.Error("expected score_classifier import alias")
	}
	if !contains(code, "stream.NewNativeStageAdapter") {
		t.Error("expected NewNativeStageAdapter call")
	}
}

func TestPluginRequireBlock(t *testing.T) {
	plugins := []models.Plugin{
		{Name: "crm-enrichment"},
		{Name: "score-classifier"},
	}

	block := pluginRequireBlock(plugins)

	if !contains(block, "github.com/conduix/plugins/crm_enrichment v0.0.0") {
		t.Error("expected crm_enrichment require")
	}
	if !contains(block, "github.com/conduix/plugins/score_classifier v0.0.0") {
		t.Error("expected score_classifier require")
	}
	if !contains(block, "github.com/conduix/plugins/crm_enrichment => ./plugins/crm_enrichment") {
		t.Error("expected local replace for crm_enrichment")
	}
	if !contains(block, "github.com/conduix/plugins/score_classifier => ./plugins/score_classifier") {
		t.Error("expected local replace for score_classifier")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"crm-enrichment", "crm_enrichment"},
		{"score classifier", "score_classifier"},
		{"My-Plugin", "my_plugin"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		result := sanitizeName(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestRunnerParsePlatform(t *testing.T) {
	tests := []struct {
		input  string
		goos   string
		goarch string
	}{
		{"linux/arm64", "linux", "arm64"},
		{"linux/amd64", "linux", "amd64"},
		{"darwin/arm64", "darwin", "arm64"},
		{"", "linux", "arm64"},
		{"linux", "linux", "arm64"},
	}

	for _, tt := range tests {
		goos, goarch := parsePlatform(tt.input)
		if goos != tt.goos || goarch != tt.goarch {
			t.Errorf("parsePlatform(%q) = (%q, %q), expected (%q, %q)", tt.input, goos, goarch, tt.goos, tt.goarch)
		}
	}
}

func TestExtractPluginIDs(t *testing.T) {
	plugins := []models.Plugin{
		{ID: "id-1"},
		{ID: "id-2"},
		{ID: "id-3"},
	}

	ids := extractPluginIDs(plugins)
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
	if ids[0] != "id-1" || ids[1] != "id-2" || ids[2] != "id-3" {
		t.Errorf("unexpected IDs: %v", ids)
	}
}

func TestDefaultRunnerBuilderConfig(t *testing.T) {
	cfg := DefaultRunnerBuilderConfig()
	if cfg.BuildTimeout == 0 {
		t.Error("expected non-zero build timeout")
	}
	if cfg.ImagePrefix == "" {
		t.Error("expected non-empty image prefix")
	}
	if cfg.Platform == "" {
		t.Error("expected non-empty platform")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
