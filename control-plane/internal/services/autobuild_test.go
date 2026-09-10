package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// fakeBuilder 는 실제 go build 없이 빌더 계약만 흉내낸다.
type fakeBuilder struct {
	calls  atomic.Int32
	result *builder.RunnerBuildResult
	err    error
	// onBuild 는 빌드 호출 시점에 DB 상태를 바꿔 "빌드 진행/완료" 를 재현한다.
	onBuild func()
}

func (f *fakeBuilder) Build(_ context.Context, _ string) (*builder.RunnerBuildResult, error) {
	f.calls.Add(1)
	if f.onBuild != nil {
		f.onBuild()
	}
	return f.result, f.err
}

func newTestAutoBuilder(t *testing.T, fb *fakeBuilder) (*AutoBuilder, *gorm.DB) {
	t.Helper()
	// :memory: 는 커넥션마다 별개 DB 다. AutoBuilder 가 별도 고루틴에서 조회하므로
	// 공유 캐시를 쓰고 커넥션을 1개로 묶어야 같은 DB 를 본다.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.RunnerVersion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	a := NewAutoBuilder(db, fb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// 테스트에서 실제 폴링 간격(2s)·상한(20m)을 기다릴 수 없다.
	a.pollInterval = 5 * time.Millisecond
	a.waitTimeout = 2 * time.Second
	return a, db
}

func insertVersion(t *testing.T, db *gorm.DB, id, status string, binarySize int) {
	t.Helper()
	v := &models.RunnerVersion{ID: id, Status: status, BinarySize: binarySize, CreatedAt: time.Now()}
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("insert version: %v", err)
	}
}

// 핵심 시나리오: 빌드가 필요해 실행이 막힌 요청이, 빌드 후 자동으로 실행까지 이어져야 한다.
// 예전에는 여기서 409 를 던지고 끝나 사용자가 직접 빌드·재실행했다.
func TestAutoBuilder_BuildsThenStarts(t *testing.T) {
	var db *gorm.DB
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "ready"}}
	a, d := newTestAutoBuilder(t, fb)
	db = d
	// 빌드가 호출되면 ready 버전이 생긴 것으로 만든다.
	fb.onBuild = func() { insertVersion(t, db, "v1", "ready", 1024) }

	started := make(chan string, 1)
	ok := a.Reserve("wf-1", "user-1", func(workflowID, userID string) error {
		started <- workflowID
		return nil
	})
	if !ok {
		t.Fatal("첫 예약이 거부됐다")
	}

	select {
	case got := <-started:
		if got != "wf-1" {
			t.Errorf("started workflow = %q, want wf-1", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("빌드 후 실행이 시작되지 않았다 — 사용자가 다시 실행을 눌러야 하는 상태")
	}

	if n := fb.calls.Load(); n != 1 {
		t.Errorf("build 호출 %d회, want 1", n)
	}
}

// 실행 버튼을 연달아 누르면 빌드 대기가 쌓여 같은 워크플로우가 여러 번 시작된다.
func TestAutoBuilder_RejectsDuplicateReservation(t *testing.T) {
	release := make(chan struct{})
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "ready"}}
	a, db := newTestAutoBuilder(t, fb)
	fb.onBuild = func() {
		<-release // 첫 빌드를 붙잡아 두 번째 예약이 겹치게 한다
		insertVersion(t, db, "v1", "ready", 1024)
	}

	var startCount atomic.Int32
	start := func(string, string) error { startCount.Add(1); return nil }

	if !a.Reserve("wf-1", "u", start) {
		t.Fatal("첫 예약이 거부됐다")
	}
	// 첫 빌드가 진행 중인 동안 같은 워크플로우로 다시 요청
	if a.Reserve("wf-1", "u", start) {
		t.Error("중복 예약이 통과했다 — 빌드 후 같은 워크플로우가 두 번 시작된다")
	}
	// 다른 워크플로우는 막지 않는다
	if !a.Reserve("wf-2", "u", start) {
		t.Error("다른 워크플로우 예약이 거부됐다")
	}

	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for startCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if startCount.Load() < 1 {
		t.Fatal("빌드 후 실행이 시작되지 않았다")
	}
}

// 예약이 끝나면 다시 예약할 수 있어야 한다 — 안 그러면 한 번 실패한 워크플로우는
// 재시작을 영구히 못 한다.
func TestAutoBuilder_AllowsReservationAfterCompletion(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "skipped"}}
	a, _ := newTestAutoBuilder(t, fb)

	done := make(chan struct{}, 2)
	start := func(string, string) error { done <- struct{}{}; return nil }

	if !a.Reserve("wf-1", "u", start) {
		t.Fatal("첫 예약 거부")
	}
	<-done

	// 예약 해제까지 약간의 여유
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a.Reserve("wf-1", "u", start) {
			<-done
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("완료 후에도 재예약이 거부됐다")
}

// skipped 는 동일 소스의 ready 버전이 이미 있다는 뜻이므로 대기 없이 실행 가능하다.
func TestAutoBuilder_SkippedBuildStartsImmediately(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v-existing", Status: "skipped"}}
	a, _ := newTestAutoBuilder(t, fb)

	started := make(chan struct{}, 1)
	a.Reserve("wf-1", "u", func(string, string) error { started <- struct{}{}; return nil })

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("skipped 인데 실행이 시작되지 않았다 — ready 버전이 이미 있으므로 기다릴 이유가 없다")
	}
}

// 빌드가 실패하면 실행을 시작하지 않는다 — 낡은 바이너리로 도는 것보다 안전하다.
func TestAutoBuilder_DoesNotStartOnBuildFailure(t *testing.T) {
	fb := &fakeBuilder{err: errors.New("compile error: undefined symbol")}
	a, _ := newTestAutoBuilder(t, fb)

	started := make(chan struct{}, 1)
	a.Reserve("wf-1", "u", func(string, string) error { started <- struct{}{}; return nil })

	select {
	case <-started:
		t.Fatal("빌드가 실패했는데 실행이 시작됐다")
	case <-time.After(300 * time.Millisecond):
	}
}

// 다른 빌드가 이미 돌고 있으면 새로 걸지 않고 그것을 기다린다 —
// 중복 빌드는 GOCACHE 경합만 만든다(빌더가 TryLock 으로 거부하는 이유와 같다).
func TestAutoBuilder_WaitsForRunningBuildInsteadOfStartingAnother(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "ready"}}
	a, db := newTestAutoBuilder(t, fb)

	// 이미 building 인 버전이 있는 상태
	insertVersion(t, db, "v-building", "building", 0)

	started := make(chan struct{}, 1)
	a.Reserve("wf-1", "u", func(string, string) error { started <- struct{}{}; return nil })

	// 잠시 뒤 그 빌드가 완료된 것으로 만든다
	time.Sleep(30 * time.Millisecond)
	if fb.calls.Load() != 0 {
		t.Error("이미 빌드가 도는데 새 빌드를 걸었다")
	}
	db.Model(&models.RunnerVersion{}).Where("id = ?", "v-building").
		Updates(map[string]any{"status": "ready", "binary_size": 2048})

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("진행 중이던 빌드가 끝났는데 실행이 시작되지 않았다")
	}
}

// 빌더의 동시성 거부는 실패가 아니다 — 방금 다른 호출이 빌드를 잡은 것이므로 기다려야 한다.
func TestAutoBuilder_TreatsConcurrencyRejectionAsWait(t *testing.T) {
	fb := &fakeBuilder{err: errors.New(buildInProgressMessage)}
	a, db := newTestAutoBuilder(t, fb)
	// 빌드 호출 시점에 building 이 생긴 것으로 만든다(다른 호출이 잡은 상황)
	fb.onBuild = func() { insertVersion(t, db, "v-other", "building", 0) }

	started := make(chan struct{}, 1)
	a.Reserve("wf-1", "u", func(string, string) error { started <- struct{}{}; return nil })

	time.Sleep(30 * time.Millisecond)
	db.Model(&models.RunnerVersion{}).Where("id = ?", "v-other").
		Updates(map[string]any{"status": "ready", "binary_size": 2048})

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("동시성 거부를 실패로 처리해 실행이 시작되지 않았다")
	}
}

func TestIsBuildInProgressErr(t *testing.T) {
	// 문구가 바뀌면 중복 빌드를 걸게 된다(치명적이지 않지만 낭비).
	if !isBuildInProgressErr(errors.New(buildInProgressMessage)) {
		t.Error("빌더의 동시성 거부 문구를 인식하지 못했다")
	}
	if isBuildInProgressErr(errors.New("no native plugins found")) {
		t.Error("무관한 에러를 동시성 거부로 오인했다")
	}
	if isBuildInProgressErr(nil) {
		t.Error("nil 을 동시성 거부로 판정했다")
	}
}

// building 이 사라졌는데 ready 도 없으면 실패로 끝난 것이다 — 사유를 담아 반환한다.
func TestAutoBuilder_ReportsFailureReasonWhenNoReadyVersion(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "ready"}}
	a, db := newTestAutoBuilder(t, fb)
	fb.onBuild = func() {
		v := &models.RunnerVersion{
			ID: "v-failed", Status: "failed",
			Error: "go build: undefined: pluginstage.New", CreatedAt: time.Now(),
		}
		_ = db.Create(v).Error
	}

	err := a.buildAndWait("u")
	if err == nil {
		t.Fatal("ready 버전이 없는데 성공으로 반환됐다")
	}
	// 사유 없이 "빌드 실패" 만 남기면 사용자가 로그를 직접 찾아야 한다.
	if !strings.Contains(err.Error(), "undefined: pluginstage.New") {
		t.Errorf("빌드 실패 사유가 전달되지 않았다: %v", err)
	}
}

// 상한을 넘기면 무한 대기하지 않는다.
func TestAutoBuilder_TimesOutOnStuckBuild(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "ready"}}
	a, db := newTestAutoBuilder(t, fb)
	a.waitTimeout = 80 * time.Millisecond
	// building 이 끝나지 않는 상황
	insertVersion(t, db, "v-stuck", "building", 0)

	err := a.buildAndWait("u")
	if !errors.Is(err, ErrBuildWaitTimeout) {
		t.Fatalf("상한 초과인데 err = %v, want ErrBuildWaitTimeout", err)
	}
}

// 여러 워크플로우가 동시에 실행을 요청해도 예약 맵이 깨지지 않아야 한다.
func TestAutoBuilder_ConcurrentReservationsAreSafe(t *testing.T) {
	fb := &fakeBuilder{result: &builder.RunnerBuildResult{VersionID: "v1", Status: "skipped"}}
	a, _ := newTestAutoBuilder(t, fb)

	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// 절반은 같은 워크플로우 → 중복 거부 대상
			id := "wf-shared"
			if n%2 == 0 {
				id = "wf-" + string(rune('a'+n))
			}
			if a.Reserve(id, "u", func(string, string) error { return nil }) {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if accepted.Load() == 0 {
		t.Error("동시 요청이 모두 거부됐다")
	}
}
