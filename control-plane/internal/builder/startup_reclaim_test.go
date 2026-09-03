package builder

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// BUG#4 회귀: control-plane 재시작 시 build goroutine 이 죽으면 DB 에 building 레코드가 고아로 남는다.
// NewRunnerBuilder(부팅) 가 이를 즉시 failed 로 회수해야 새 빌드가 "already in progress"로 막히지 않는다.
func TestNewRunnerBuilder_ReclaimsStaleBuildingOnStartup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.RunnerVersion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 방금 시작된(오래되지 않은) building 레코드 — CP 재시작으로 고아가 된 상황.
	now := time.Now()
	if err := db.Create(&models.RunnerVersion{
		ID: "rv-orphan", Status: "building", SourceHash: "h", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 정상 ready 레코드는 건드리면 안 됨.
	if err := db.Create(&models.RunnerVersion{
		ID: "rv-ok", Status: "ready", SourceHash: "h2", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed ready: %v", err)
	}

	NewRunnerBuilder(db, nil) // 부팅 회수 트리거

	var orphan, ok models.RunnerVersion
	if err := db.First(&orphan, "id = ?", "rv-orphan").Error; err != nil {
		t.Fatalf("get orphan: %v", err)
	}
	if orphan.Status != "failed" {
		t.Errorf("orphan building status = %q, want failed (부팅 시 회수)", orphan.Status)
	}
	if err := db.First(&ok, "id = ?", "rv-ok").Error; err != nil {
		t.Fatalf("get ok: %v", err)
	}
	if ok.Status != "ready" {
		t.Errorf("ready version status = %q, want ready (건드리면 안 됨)", ok.Status)
	}
}

// build_number(autoIncrement) 회귀: 실패 기록이 전 컬럼 Save 였을 때 build_number 를
// 0 으로 덮어, 두 번째 실패부터 unique 충돌로 저장 자체가 실패해 building 좀비가 남았다.
// persistFailure 는 변경 필드만 갱신하므로 연속 실패가 전부 기록되어야 한다.
func TestPersistFailure_DoesNotClobberBuildNumber(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.RunnerVersion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rb := NewRunnerBuilder(db, nil)

	for i, id := range []string{"rv-fail-1", "rv-fail-2"} {
		v := models.RunnerVersion{ID: id, Status: "building", SourceHash: "h"}
		if err := db.Create(&v).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		v.Status = "failed"
		v.Error = "go build: signal: killed"
		if err := rb.persistFailure(&v); err != nil {
			t.Fatalf("persistFailure %s: %v", id, err)
		}
	}

	var got []models.RunnerVersion
	if err := db.Order("id").Find(&got).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(got) != 2 || got[0].Status != "failed" || got[1].Status != "failed" {
		t.Fatalf("expected both failed, got %+v", got)
	}
	// autoIncrement 로 채워진 build_number 가 보존되어야 한다 (0 덮어쓰기 금지)
	if got[0].BuildNumber == got[1].BuildNumber {
		t.Fatalf("build_number clobbered: %d == %d", got[0].BuildNumber, got[1].BuildNumber)
	}
}

// CONDUIX_BUILD_TIMEOUT 으로 빌드 타임아웃을 조절할 수 있어야 한다 (콜드 캐시 첫 빌드 대응)
func TestDefaultRunnerBuilderConfig_BuildTimeoutEnv(t *testing.T) {
	t.Setenv("CONDUIX_BUILD_TIMEOUT", "20m")
	if got := DefaultRunnerBuilderConfig().BuildTimeout; got != 20*time.Minute {
		t.Fatalf("expected 20m, got %v", got)
	}
	t.Setenv("CONDUIX_BUILD_TIMEOUT", "invalid")
	if got := DefaultRunnerBuilderConfig().BuildTimeout; got != 5*time.Minute {
		t.Fatalf("expected 5m fallback, got %v", got)
	}
}
