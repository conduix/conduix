package services

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func newClusterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Cluster{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// 워크플로우가 cluster를 지정하면 그 값을 그대로 신뢰한다(default 조회 없이).
func TestResolveExecutionCluster_WorkflowSpecified(t *testing.T) {
	db := newClusterTestDB(t)
	got, err := ResolveExecutionCluster(db, "wf-cluster-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "wf-cluster-1" {
		t.Errorf("got %q, want wf-cluster-1", got)
	}
}

// 미지정 시 default cluster로 폴백한다 — 이 값이 발행 채널(cluster:<id>:execute)을 결정한다.
func TestResolveExecutionCluster_FallbackToDefault(t *testing.T) {
	db := newClusterTestDB(t)
	if err := db.Create(&models.Cluster{ID: "def-cluster", IsDefault: true}).Error; err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	got, err := ResolveExecutionCluster(db, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "def-cluster" {
		t.Errorf("got %q, want def-cluster", got)
	}
}

// 미지정 + default 없음 → ErrNoExecutionCluster (agent가 받을 채널을 확정 못 하므로 실행 거부).
func TestResolveExecutionCluster_NoDefault(t *testing.T) {
	db := newClusterTestDB(t)
	_, err := ResolveExecutionCluster(db, "")
	if !errors.Is(err, ErrNoExecutionCluster) {
		t.Errorf("got %v, want ErrNoExecutionCluster", err)
	}
}
