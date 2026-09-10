package services

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func newResolverTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Plugin{}, &models.RunnerVersion{}, &models.Workflow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// 커스텀 native stage 를 쓰지 않는 워크플로우도 최신 ready 바이너리로 실행되어야 한다.
// 예전에는 여기서 ("", DefaultRunnerImage) 로 조기 반환해 이미지에 구워진 낡은
// 바이너리로 돌았고, 실행 이력에 runner_version_id 가 남지 않았다.
func TestResolveRunnerVersion_NoCustomStage_UsesBuiltBinary(t *testing.T) {
	db := newResolverTestDB(t)

	// 활성 native plugin 이 하나 있고 배포까지 끝난 상태
	if err := db.Create(&models.Plugin{
		ID: "p1", Name: "geocode_kakao", Type: "native", Status: "active",
		SourceHash: "h1", DeployedHash: "h1", Version: "v1",
	}).Error; err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	if err := db.Create(&models.RunnerVersion{
		ID: "rv-1", BuildNumber: 1, Status: "ready",
		ImageTag:   "ghcr.io/conduix/pipeline-runner:rv-1",
		BinarySize: 1024,
	}).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	r := NewRunnerResolver(db)
	// stages 가 비어 커스텀 stage 를 전혀 쓰지 않는 워크플로우
	wf := &models.Workflow{
		ID:              "wf-1",
		PipelinesConfig: `[{"name":"collect","input":{"type":"rest_api"},"outputs":[{"type":"sql"}]}]`,
	}

	versionID, image, usesNative, err := r.ResolveRunnerVersion(wf)

	// 핵심: 커스텀 stage 가 없다는 이유로 "이미지 실행" 경로가 선택되면 안 된다.
	// 조기 반환이 살아 있으면 ("", DefaultRunnerImage, false, nil) 이 돌아온다.
	if err == nil && !usesNative {
		t.Fatalf("이미지 실행 경로로 빠졌다 — 낡은 바이너리로 돈다: version=%q image=%q", versionID, image)
	}

	// coreChangedSince 가 소스 트리를 읽어 BuildRequired 를 낼 수 있다.
	// 그 경우에도 "이미지로 실행" 은 아니어야 한다 — 빌드를 요구하는 것이 맞다.
	var bre *BuildRequiredError
	if errors.As(err, &bre) {
		if versionID != "" || image != "" {
			t.Fatalf("build required 인데 실행 대상이 반환됨: version=%q image=%q", versionID, image)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if versionID == "" {
		t.Fatal("커스텀 stage 가 없다는 이유로 runner version 이 비었다 — 낡은 바이너리로 실행된다")
	}
	if versionID != "rv-1" {
		t.Fatalf("최신 ready 버전이어야 한다: got %q", versionID)
	}
}

// 리졸버의 판정 집합은 빌더와 같아야 한다 — 활성 native plugin 전체.
// 워크플로우가 참조하는 플러그인만 보면 CombinedSourceHash 가 빌더와 달라져
// 항상 coreChanged 로 오판한다.
func TestFindAllActiveNativePlugins_MatchesBuilderScope(t *testing.T) {
	db := newResolverTestDB(t)
	seed := []models.Plugin{
		{ID: "p1", Name: "a", Type: "native", Status: "active", Version: "v1"},
		{ID: "p2", Name: "b", Type: "native", Status: "active", Version: "v1"},
		{ID: "p3", Name: "c", Type: "native", Status: "inactive", Version: "v1"}, // 제외
		{ID: "p4", Name: "d", Type: "script", Status: "active", Version: "v1"},   // 제외
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got, err := NewRunnerResolver(db).findAllActiveNativePlugins()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("active native 만 2개여야 한다: got %d", len(got))
	}
}
