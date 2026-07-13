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
