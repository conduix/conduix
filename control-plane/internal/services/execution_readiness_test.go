package services

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

func readinessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Agent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func addAgent(t *testing.T, db *gorm.DB, id, clusterID, status string, hb *time.Time) {
	t.Helper()
	a := &models.Agent{ID: id, ClusterID: clusterID, Status: status, LastHeartbeat: hb, Hostname: "h-" + id}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
}

// 실행 명령은 Redis pub/sub(at-most-once)로 발행된다. 받을 agent 가 없으면 Publish 는
// 에러를 내지 않고 명령이 사라지는데, batch 는 stale 감지 대상이 아니라 자동 복구조차
// 없어 워크플로우가 영구히 running 으로 남는다(실패조차 안 보인다).
func TestEnsureLiveAgent(t *testing.T) {
	t.Run("등록된 agent 가 없으면 거부하고 조치를 알린다", func(t *testing.T) {
		db := readinessTestDB(t)
		err := EnsureLiveAgent(db, "cluster-a")
		if !errors.Is(err, ErrNoLiveAgent) {
			t.Fatalf("err = %v, want ErrNoLiveAgent", err)
		}
		var e *NoLiveAgentError
		if !errors.As(err, &e) || e.Registered != 0 {
			t.Fatalf("registered = %v, want 0 (배포 안 됨과 죽음은 조치가 다르다)", err)
		}
		if !contains(e.Error(), "agent 를 배포") {
			t.Errorf("무엇을 해야 하는지 안내가 없다: %s", e.Error())
		}
	})

	t.Run("살아있는 agent 가 있으면 통과", func(t *testing.T) {
		db := readinessTestDB(t)
		now := time.Now()
		addAgent(t, db, "a1", "cluster-a", "online", &now)
		if err := EnsureLiveAgent(db, "cluster-a"); err != nil {
			t.Fatalf("정상 실행이 차단됐다: %v", err)
		}
	})

	t.Run("heartbeat 가 끊긴 agent 만 있으면 거부", func(t *testing.T) {
		db := readinessTestDB(t)
		stale := time.Now().Add(-10 * time.Minute)
		addAgent(t, db, "a1", "cluster-a", "online", &stale)
		err := EnsureLiveAgent(db, "cluster-a")
		if !errors.Is(err, ErrNoLiveAgent) {
			t.Fatalf("err = %v, want ErrNoLiveAgent", err)
		}
		var e *NoLiveAgentError
		errors.As(err, &e)
		if e.StaleAgents != 1 {
			t.Errorf("stale = %d, want 1", e.StaleAgents)
		}
	})

	t.Run("status online 이어도 heartbeat 로 판정한다", func(t *testing.T) {
		// agent 가 비정상 종료하면 status 는 online 으로 남는다(정상 종료만 offline 로 갱신).
		db := readinessTestDB(t)
		stale := time.Now().Add(-5 * time.Minute)
		addAgent(t, db, "a1", "cluster-a", "online", &stale)
		if err := EnsureLiveAgent(db, "cluster-a"); err == nil {
			t.Error("status 만 믿어 죽은 agent 를 살아있다고 판정했다")
		}
	})

	t.Run("heartbeat 가 없는 신규 등록 agent 는 통과", func(t *testing.T) {
		// 등록 직후 첫 heartbeat 전에 실행을 막으면 첫 배포에서 실행이 안 된다.
		db := readinessTestDB(t)
		addAgent(t, db, "a1", "cluster-a", "online", nil)
		if err := EnsureLiveAgent(db, "cluster-a"); err != nil {
			t.Errorf("신규 agent 가 차단됐다: %v", err)
		}
	})

	t.Run("offline 로 표시된 agent 는 죽은 것으로 본다", func(t *testing.T) {
		db := readinessTestDB(t)
		now := time.Now()
		addAgent(t, db, "a1", "cluster-a", "offline", &now)
		if err := EnsureLiveAgent(db, "cluster-a"); err == nil {
			t.Error("offline agent 를 살아있다고 판정했다")
		}
	})

	t.Run("다른 클러스터의 agent 는 세지 않는다", func(t *testing.T) {
		db := readinessTestDB(t)
		now := time.Now()
		addAgent(t, db, "a1", "cluster-b", "online", &now)
		if err := EnsureLiveAgent(db, "cluster-a"); err == nil {
			t.Error("다른 클러스터 agent 로 실행을 허용했다 — 명령은 대상 클러스터 채널로 간다")
		}
	})

	t.Run("여러 agent 중 하나만 살아있어도 통과", func(t *testing.T) {
		db := readinessTestDB(t)
		stale := time.Now().Add(-10 * time.Minute)
		now := time.Now()
		addAgent(t, db, "dead", "cluster-a", "online", &stale)
		addAgent(t, db, "alive", "cluster-a", "online", &now)
		if err := EnsureLiveAgent(db, "cluster-a"); err != nil {
			t.Errorf("살아있는 agent 가 있는데 차단됐다: %v", err)
		}
	})
}

// 검증 자체가 실패했을 때 정상 실행을 막으면 안 된다 — 후속 방어(stale 감지)가 있다.
func TestEnsureLiveAgent_DoesNotBlockOnQueryFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:queryfail?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// agents 테이블을 만들지 않아 조회가 실패하는 상태
	if err := EnsureLiveAgent(db, "cluster-a"); err != nil {
		t.Errorf("조회 실패로 실행이 차단됐다: %v — 검증 오류가 정상 실행을 막으면 더 나쁘다", err)
	}
}

func TestUnclaimedErrorMessage(t *testing.T) {
	msg := unclaimedErrorMessage("cluster-a")
	// "orphaned" 같은 내부 용어만 남기면 사용자가 조치를 알 수 없다.
	if !contains(msg, "cluster-a") {
		t.Errorf("어느 클러스터인지 없다: %s", msg)
	}
	if !contains(msg, "확인") {
		t.Errorf("무엇을 확인해야 하는지 안내가 없다: %s", msg)
	}
}
