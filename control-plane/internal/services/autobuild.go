package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/internal/builder"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// AutoBuilder 는 "빌드가 필요해서 실행이 막히는" 상황을 사용자 조작 없이 해소한다.
//
// 왜 필요한가: 실행 요청 시 runner 재빌드가 필요하면 control-plane 은 원인(어떤 stage 가
// 바뀌었는지, 코어가 바뀌었는지)을 이미 알고 있다. 그런데 예전에는 409 와 "다시 빌드한 뒤
// 실행해주세요" 메시지만 던지고 끝냈다. 사용자는 다른 화면으로 이동해 빌드를 누르고,
// 완료를 눈으로 확인한 뒤 실행을 다시 눌러야 했다 — 서버가 아는 일을 사람이 대신 했다.
//
// 이제 빌드를 자동으로 걸고, 완료되면 실행을 이어서 시작한다. 사용자는 실행 버튼 한 번만
// 누른다.
type AutoBuilder struct {
	db      *gorm.DB
	builder RunnerBuildRunner
	logger  *slog.Logger

	// 같은 워크플로우에 대한 중복 예약을 막는다. 사용자가 실행을 연달아 누르면
	// 빌드 대기가 여러 개 쌓여 빌드 후 같은 워크플로우가 여러 번 시작된다.
	//
	// in-process 맵만으로는 부족하다 — control-plane 이 다중 레플리카로 도는 환경에서
	// 두 요청이 서로 다른 pod 로 가면 각 pod 가 자기 맵만 보고 둘 다 예약한다
	// (실측: 2 레플리카에서 newly_reserved=true 가 두 번, 빌드 후 실행 2건 생성).
	// 그래서 Redis 분산 락을 1차 관문으로 쓰고, 맵은 같은 pod 내 경합만 막는다.
	mu       sync.Mutex
	reserved map[string]struct{}
	locker   ReservationLocker

	// 테스트에서 대기 시간을 줄이기 위해 주입 가능하게 둔다.
	pollInterval time.Duration
	waitTimeout  time.Duration
}

// RunnerBuildRunner 는 AutoBuilder 가 쓰는 빌더 계약이다.
// 실제 구현은 builder.RunnerBuilder 이고, 테스트에서는 대역을 넣는다
// (실제 빌드는 go build 를 돌려 단위 테스트에서 쓸 수 없다).
type RunnerBuildRunner interface {
	Build(ctx context.Context, createdBy string) (*builder.RunnerBuildResult, error)
}

// 빌드 완료를 기다리는 주기와 상한.
// 빌드는 go build 를 포함해 수 분이 걸릴 수 있다(실측 native stage 빌드 수십 초~수 분).
// 상한을 넘으면 예약을 포기하고 실행을 시작하지 않는다 — 낡은 바이너리로 도는 것보다 안전하다.
const (
	defaultBuildPollInterval = 2 * time.Second
	defaultBuildWaitTimeout  = 20 * time.Minute
)

// ErrBuildWaitTimeout 은 빌드가 상한 안에 끝나지 않았을 때 반환한다.
var ErrBuildWaitTimeout = errors.New("runner build did not finish within the wait timeout")

// ReservationLocker 는 레플리카 간 중복 예약을 막는 분산 락이다.
// RedisService 의 ResilientClient 가 구현하며(SetNX/Del), 테스트에서는 대역을 넣는다.
type ReservationLocker interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	Del(ctx context.Context, keys ...string) error
}

// reservationTTL 은 예약 락의 수명이다.
//
// 빌드 대기 상한(20분)보다 길어야 한다 — 짧으면 빌드 도중 락이 풀려 다른 레플리카가
// 중복 예약한다. 반대로 무한이면 pod 가 죽었을 때 그 워크플로우가 영구히 예약 불가가
// 되므로, 상한을 넘긴 여유값으로 자동 만료시킨다.
const reservationTTL = 25 * time.Minute

func reservationKey(workflowID string) string {
	return "autobuild:reserve:" + workflowID
}

// NewAutoBuilder 는 locker 없이 만든다(단일 프로세스·테스트용).
// 다중 레플리카 환경에서는 WithLocker 로 분산 락을 넣어야 중복 예약이 막힌다.
func NewAutoBuilder(db *gorm.DB, builder RunnerBuildRunner, logger *slog.Logger) *AutoBuilder {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoBuilder{
		db:           db,
		builder:      builder,
		logger:       logger,
		reserved:     make(map[string]struct{}),
		pollInterval: defaultBuildPollInterval,
		waitTimeout:  defaultBuildWaitTimeout,
	}
}

// WithLocker 는 레플리카 간 중복 예약을 막는 분산 락을 주입한다.
func (a *AutoBuilder) WithLocker(l ReservationLocker) *AutoBuilder {
	a.locker = l
	return a
}

// StartFn 은 빌드 완료 후 실행을 시작하는 콜백이다.
// 핸들러의 실행 시작 로직을 그대로 재사용하기 위해 함수로 받는다 —
// 여기서 실행 생성을 다시 구현하면 클러스터 확정·파티션 배정 같은 정책이 두 곳으로 갈린다.
type StartFn func(workflowID, userID string) error

// Reserve 는 빌드를 걸고, 완료되면 start 를 호출한다.
//
// 이미 이 워크플로우의 예약이 살아있으면 false 를 반환한다 — 중복 실행을 막는다.
// 빌드 트리거와 대기는 백그라운드에서 진행되므로 이 함수는 즉시 반환한다.
func (a *AutoBuilder) Reserve(workflowID, userID string, start StartFn) bool {
	a.mu.Lock()
	if _, dup := a.reserved[workflowID]; dup {
		a.mu.Unlock()
		a.logger.Info("auto-build already reserved for this workflow, skipping duplicate",
			"workflow_id", workflowID)
		return false
	}
	a.reserved[workflowID] = struct{}{}
	a.mu.Unlock()

	// 레플리카 간 중복 차단. 락을 못 잡으면 다른 pod 가 이미 예약했으므로 물러난다 —
	// 그 pod 가 빌드 후 실행을 시작한다.
	if !a.acquireReservation(workflowID) {
		a.mu.Lock()
		delete(a.reserved, workflowID)
		a.mu.Unlock()
		a.logger.Info("auto-build reserved by another replica, skipping duplicate",
			"workflow_id", workflowID)
		return false
	}

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.reserved, workflowID)
			a.mu.Unlock()
			a.releaseReservation(workflowID)
		}()

		if err := a.buildAndWait(userID); err != nil {
			// 빌드 실패는 실행을 시작하지 않는다. 사용자는 실행 이력의 사유로 원인을 본다.
			a.logger.Error("auto-build failed, workflow not started",
				"workflow_id", workflowID, "error", err)
			return
		}

		a.logger.Info("auto-build finished, starting workflow", "workflow_id", workflowID)
		if err := start(workflowID, userID); err != nil {
			a.logger.Error("failed to start workflow after auto-build",
				"workflow_id", workflowID, "error", err)
		}
	}()

	return true
}

// buildAndWait 는 빌드를 트리거하고 ready 가 될 때까지 기다린다.
//
// 다른 빌드가 이미 돌고 있으면(수동 빌드, 또는 다른 워크플로우의 자동 빌드) 새로 걸지 않고
// 그 빌드를 기다린다 — 같은 코어·stage 소스를 빌드하므로 결과를 공유할 수 있고,
// 중복 빌드는 GOCACHE 경합만 만든다.
func (a *AutoBuilder) buildAndWait(userID string) error {
	if a.buildInProgress() {
		a.logger.Info("a build is already running, waiting for it instead of starting another")
		return a.waitForReady()
	}

	result, err := a.builder.Build(context.Background(), userID)
	if err != nil {
		// TryLock 경합으로 거부된 경우는 실패가 아니다 — 방금 다른 빌드가 시작된 것이므로
		// 그 빌드를 기다린다. 이 문구는 builder.Build 의 동시성 거부 메시지다.
		if isBuildInProgressErr(err) {
			a.logger.Info("build was claimed by another caller, waiting for it")
			return a.waitForReady()
		}
		return fmt.Errorf("build: %w", err)
	}

	// skipped 는 동일 해시 ready 버전이 이미 있다는 뜻이므로 그대로 실행 가능하다.
	if result != nil && result.Status == "skipped" {
		a.logger.Info("build skipped — identical source already built", "version_id", result.VersionID)
		return nil
	}

	return a.waitForReady()
}

// waitForReady 는 building 이 사라지고 ready 버전이 생길 때까지 폴링한다.
func (a *AutoBuilder) waitForReady() error {
	deadline := time.Now().Add(a.waitTimeout)
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		if !a.buildInProgress() {
			if a.hasReadyVersion() {
				return nil
			}
			// building 이 없는데 ready 도 없다 = 빌드가 실패로 끝났다.
			return fmt.Errorf("build finished without a ready runner version: %s", a.lastBuildError())
		}
		if time.Now().After(deadline) {
			return ErrBuildWaitTimeout
		}
		<-ticker.C
	}
}

func (a *AutoBuilder) buildInProgress() bool {
	var n int64
	a.db.Model(&models.RunnerVersion{}).Where("status = ?", "building").Count(&n)
	return n > 0
}

func (a *AutoBuilder) hasReadyVersion() bool {
	var n int64
	a.db.Model(&models.RunnerVersion{}).
		Where("status = ? AND binary_size > 0", "ready").Count(&n)
	return n > 0
}

// lastBuildError 는 가장 최근 실패 빌드의 사유를 가져온다.
// 사유 없이 "빌드 실패" 만 남기면 사용자가 로그를 직접 찾아야 한다.
func (a *AutoBuilder) lastBuildError() string {
	var v models.RunnerVersion
	err := a.db.Select("error", "id").
		Where("status = ?", "failed").
		Order("created_at DESC").First(&v).Error
	if err != nil || v.Error == "" {
		return "원인 미확인 — /runner 화면의 빌드 로그를 확인하세요"
	}
	return v.Error
}

// isBuildInProgressErr 는 빌더의 동시성 거부인지 판정한다.
//
// builder 는 이 상황을 별도 에러 타입으로 노출하지 않아 문자열로 판정한다.
// 문자열이 바뀌면 중복 빌드를 걸게 되므로(치명적이지는 않으나 낭비) 테스트로 고정한다.
func isBuildInProgressErr(err error) bool {
	return err != nil && err.Error() == buildInProgressMessage
}

// buildInProgressMessage 는 builder.Build 가 동시 호출을 거부할 때 쓰는 문구다.
const buildInProgressMessage = "another build is already in progress"

// acquireReservation 은 이 워크플로우의 예약 소유권을 잡는다.
//
// locker 가 없으면(단일 프로세스 구성) 통과시킨다 — in-process 맵이 이미 막았다.
// Redis 조회가 실패하면 통과시킨다: 락을 못 잡았다고 실행을 포기하면 정상 요청이
// 조용히 사라진다. 중복 실행보다 실행 누락이 더 나쁘고, 빌더의 DB building 체크가
// 중복 빌드는 이미 막는다.
func (a *AutoBuilder) acquireReservation(workflowID string) bool {
	if a.locker == nil {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	acquired, err := a.locker.SetNX(ctx, reservationKey(workflowID), "1", reservationTTL)
	if err != nil {
		a.logger.Warn("reservation lock unavailable, proceeding without it",
			"workflow_id", workflowID, "error", err)
		return true
	}
	return acquired
}

// releaseReservation 은 예약을 해제해 다음 요청이 즉시 예약할 수 있게 한다.
// 해제에 실패해도 TTL 로 자연 만료되므로 영구 잠금은 되지 않는다.
func (a *AutoBuilder) releaseReservation(workflowID string) {
	if a.locker == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := a.locker.Del(ctx, reservationKey(workflowID)); err != nil {
		a.logger.Warn("failed to release reservation lock (will expire by TTL)",
			"workflow_id", workflowID, "error", err)
	}
}
