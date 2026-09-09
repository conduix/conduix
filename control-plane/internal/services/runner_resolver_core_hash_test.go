package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// writeCoreTree 는 CoreSourceHash 가 훑는 모듈 디렉토리에 .go 파일을 만든다.
// CoreSourceHash 는 모듈 하나라도 walk 에 실패하면 빈 문자열을 반환하므로
// runnerSourceModules 전체가 존재해야 한다.
func writeCoreTree(t *testing.T, root, body string) {
	t.Helper()
	for _, mod := range []string{"pipeline-runner", "pipeline-core", "shared", "plugin-sdk"} {
		dir := filepath.Join(root, mod)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "doc.go"), []byte("package "+"x\n"), 0o644))
	}
	dir := filepath.Join(root, "pipeline-runner", "internal", "runner")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runner.go"), []byte(body), 0o644))
}

// 코어가 그대로면 기존 ready 버전을 계속 쓴다 — 불필요한 재빌드 강요를 막는다.
func TestCoreChangedSince_FalseWhenCoreUnchanged(t *testing.T) {
	root := t.TempDir()
	writeCoreTree(t, root, "package runner\n")
	t.Setenv("CONDUIX_SOURCE_ROOT", root)

	plugins := []models.Plugin{{ID: "p1", SourceHash: "h1"}}
	coreHash := builder.CoreSourceHash(root, nil)
	require.NotEmpty(t, coreHash, "임시 트리에서 코어 해시가 계산돼야 테스트가 의미를 가진다")

	version := &models.RunnerVersion{
		SourceHash: builder.CombinedSourceHash(map[string]string{"p1": "h1"}, coreHash),
	}

	r := &RunnerResolver{}
	assert.False(t, r.coreChangedSince(version, plugins))
}

// 코어 코드만 바뀐 경우 — plugin 해시는 그대로라 기존 판정으로는 안 잡히던 케이스.
func TestCoreChangedSince_TrueWhenCoreEdited(t *testing.T) {
	root := t.TempDir()
	writeCoreTree(t, root, "package runner\n")
	t.Setenv("CONDUIX_SOURCE_ROOT", root)

	plugins := []models.Plugin{{ID: "p1", SourceHash: "h1"}}
	version := &models.RunnerVersion{
		SourceHash: builder.CombinedSourceHash(
			map[string]string{"p1": "h1"}, builder.CoreSourceHash(root, nil)),
	}

	writeCoreTree(t, root, "package runner\n\nfunc Added() {}\n")

	r := &RunnerResolver{}
	assert.True(t, r.coreChangedSince(version, plugins),
		"코어가 바뀌면 기존 바이너리는 낡았으므로 재빌드로 유도해야 한다")
}

// 소스가 없는 환경(소스 미포함 이미지)에서 실행을 막지 않아야 한다.
func TestCoreChangedSince_FalseWhenSourceRootMissing(t *testing.T) {
	t.Setenv("CONDUIX_SOURCE_ROOT", filepath.Join(t.TempDir(), "does-not-exist"))

	version := &models.RunnerVersion{SourceHash: "whatever"}
	r := &RunnerResolver{}
	assert.False(t, r.coreChangedSince(version, []models.Plugin{{ID: "p1", SourceHash: "h1"}}),
		"코어 해시를 못 구하면 판정을 건너뛰어 현행 유지")
}

// plugin 이 추가되면 결합 해시가 달라져 재빌드가 필요하다.
func TestCoreChangedSince_TrueWhenPluginSetChanged(t *testing.T) {
	root := t.TempDir()
	writeCoreTree(t, root, "package runner\n")
	t.Setenv("CONDUIX_SOURCE_ROOT", root)

	version := &models.RunnerVersion{
		SourceHash: builder.CombinedSourceHash(
			map[string]string{"p1": "h1"}, builder.CoreSourceHash(root, nil)),
	}

	r := &RunnerResolver{}
	assert.True(t, r.coreChangedSince(version, []models.Plugin{
		{ID: "p1", SourceHash: "h1"},
		{ID: "p2", SourceHash: "h2"},
	}))
}
