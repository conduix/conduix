package services

import (
	"errors"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// fakeHeartbeats 는 Redis heartbeat 조회를 흉내낸다.
type fakeHeartbeats struct {
	hbs map[string]*types.AgentHeartbeat
	err error
}

func (f fakeHeartbeats) GetAllAgentHeartbeats() (map[string]*types.AgentHeartbeat, error) {
	return f.hbs, f.err
}

func hb(agentID, clusterID string, age time.Duration) *types.AgentHeartbeat {
	return &types.AgentHeartbeat{
		AgentID:   agentID,
		ClusterID: clusterID,
		Timestamp: time.Now().Add(-age),
	}
}

// 실행 명령은 Redis pub/sub(at-most-once)로 발행된다. 받을 agent 가 없으면 Publish 는
// 에러를 내지 않고 명령이 사라지는데, batch 는 stale 감지 대상이 아니라 자동 복구조차
// 없어 워크플로우가 영구히 running 으로 남는다(실패조차 안 보인다).
//
// 판정은 Redis heartbeat 로 한다. DB agents 테이블은 등록 이력이 누적되어 죽은 pod
// 레코드가 남는다 — 실측으로 살아있는 agent 2개인데 테이블에 66행이 있었고, DB 기반
// 판정이 "66개가 모두 응답하지 않습니다" 로 정상 실행을 막았다.
func TestEnsureLiveAgent(t *testing.T) {
	t.Run("살아있는 agent 가 있으면 통과", func(t *testing.T) {
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"a1": hb("a1", "default", time.Second),
		}}
		if err := EnsureLiveAgent(src, "default"); err != nil {
			t.Fatalf("정상 실행이 차단됐다: %v", err)
		}
	})

	t.Run("heartbeat 가 하나도 없으면 거부", func(t *testing.T) {
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{}}
		err := EnsureLiveAgent(src, "default")
		if !errors.Is(err, ErrNoLiveAgent) {
			t.Fatalf("err = %v, want ErrNoLiveAgent", err)
		}
		var e *NoLiveAgentError
		errors.As(err, &e)
		if e.Reporting != 0 {
			t.Errorf("reporting = %d, want 0", e.Reporting)
		}
		if !contains(e.Error(), "agent pod") {
			t.Errorf("조치 안내가 없다: %s", e.Error())
		}
	})

	t.Run("유예를 넘긴 agent 만 있으면 거부", func(t *testing.T) {
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"a1": hb("a1", "default", 10*time.Minute),
		}}
		err := EnsureLiveAgent(src, "default")
		var e *NoLiveAgentError
		if !errors.As(err, &e) || e.StaleAgents != 1 {
			t.Fatalf("stale 판정이 안 됐다: %v", err)
		}
		if !contains(e.Error(), "응답하지 않습니다") {
			t.Errorf("사유가 불명확하다: %s", e.Error())
		}
	})

	t.Run("다른 클러스터에만 agent 가 있으면 그 사실을 알린다", func(t *testing.T) {
		// "agent 가 없다" 와 "클러스터 설정이 틀렸다" 는 조치가 다르다.
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"a1": hb("a1", "other", time.Second),
		}}
		err := EnsureLiveAgent(src, "default")
		var e *NoLiveAgentError
		if !errors.As(err, &e) {
			t.Fatalf("err = %v", err)
		}
		if e.Reporting != 1 || e.StaleAgents != 0 {
			t.Errorf("reporting=%d stale=%d, want 1/0", e.Reporting, e.StaleAgents)
		}
		if !contains(e.Error(), "클러스터 설정") {
			t.Errorf("클러스터 설정 안내가 없다: %s", e.Error())
		}
	})

	t.Run("클러스터 미지정 heartbeat 는 default 로 본다", func(t *testing.T) {
		// 클러스터 목록 API 와 같은 규칙 — 다르면 화면과 실행 판정이 어긋난다.
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"a1": hb("a1", "", time.Second),
		}}
		if err := EnsureLiveAgent(src, "default"); err != nil {
			t.Errorf("미지정 agent 가 default 로 인정되지 않았다: %v", err)
		}
	})

	t.Run("살아있는 것과 죽은 것이 섞이면 통과", func(t *testing.T) {
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"dead":  hb("dead", "default", 10*time.Minute),
			"alive": hb("alive", "default", time.Second),
		}}
		if err := EnsureLiveAgent(src, "default"); err != nil {
			t.Errorf("살아있는 agent 가 있는데 차단됐다: %v", err)
		}
	})

	t.Run("heartbeat 조회 실패는 실행을 막지 않는다", func(t *testing.T) {
		// 검증 오류로 정상 실행을 차단하는 것이 유실보다 나쁘다.
		src := fakeHeartbeats{err: errors.New("redis not connected")}
		if err := EnsureLiveAgent(src, "default"); err != nil {
			t.Errorf("조회 실패로 실행이 차단됐다: %v", err)
		}
	})

	t.Run("source 가 nil 이면 통과", func(t *testing.T) {
		if err := EnsureLiveAgent(nil, "default"); err != nil {
			t.Errorf("Redis 미구성에서 실행이 차단됐다: %v", err)
		}
	})

	t.Run("nil heartbeat 항목을 건너뛴다", func(t *testing.T) {
		src := fakeHeartbeats{hbs: map[string]*types.AgentHeartbeat{
			"broken": nil,
			"alive":  hb("alive", "default", time.Second),
		}}
		if err := EnsureLiveAgent(src, "default"); err != nil {
			t.Errorf("nil 항목 때문에 판정이 깨졌다: %v", err)
		}
	})
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
