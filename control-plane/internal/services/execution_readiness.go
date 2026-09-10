package services

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 실행 명령은 Redis pub/sub 로 발행된다(at-most-once). 구독 중인 agent 가 없으면
// Publish 는 에러를 내지 않고 명령이 조용히 사라진다 — 서버는 성공으로 응답하고
// 워크플로우는 running 으로 남는다.
//
// realtime 은 스케줄러의 stale 감지(2분 유예)가 이를 error 로 되돌리지만,
// batch 는 그 감지에서 제외되어 있어(scheduler_service.go 의 type='batch' 제외)
// 어떤 자동 복구도 없다. 즉 사용자는 "실행 시작됨" 을 보고도 영구히 아무 진행이 없는
// 상태를 마주하고, 실패조차 표시되지 않는다.
//
// 발행 전에 받을 수 있는 agent 가 있는지 확인해 이 상태 자체를 만들지 않는다.
//
// 판정 근거는 Redis heartbeat 다. DB 의 agents 테이블은 등록 이력이 누적되는 곳이라
// 죽은 pod 레코드가 정리되지 않고 남는다(실측: 살아있는 agent 2개인데 테이블에 66행).
// DB 를 근거로 삼으면 살아있는 agent 를 죽었다고 오판해 정상 실행을 막는다.
// 시스템의 다른 판정(에이전트 목록 API, 클러스터 agent_count, stale 감지)도 모두
// Redis heartbeat 를 쓴다 — 같은 근거를 써야 화면과 실행 판정이 어긋나지 않는다.

// ErrNoLiveAgent 는 대상 클러스터에 실행을 받을 agent 가 없을 때 반환한다.
var ErrNoLiveAgent = errors.New("no live agent in target cluster")

// agentLivenessGrace 는 heartbeat 가 이만큼 끊긴 agent 를 죽은 것으로 본다.
//
// agent heartbeat 주기보다 충분히 길어야 한다 — 짧으면 살아있는 agent 를 죽었다고 보고
// 정상 실행을 막는다. 클러스터 목록 API 의 online 기준(30초)보다 넉넉히 두는 이유는,
// 이 판정이 틀리면 실행 자체가 거부되어 실패 비용이 더 크기 때문이다.
const agentLivenessGrace = 90 * time.Second

// NoLiveAgentError 는 왜 실행할 수 없는지와 사용자가 무엇을 확인해야 하는지 담는다.
type NoLiveAgentError struct {
	ClusterID string
	// Reporting 은 heartbeat 를 보내고 있는(클러스터 무관) agent 수다.
	// 0 이면 agent 가 아예 없는 것이고, 0 이 아니면 다른 클러스터에만 있는 것이다 — 조치가 다르다.
	Reporting int
	// StaleAgents 는 이 클러스터에서 heartbeat 가 유예를 넘긴 agent 수다.
	StaleAgents int
}

func (e *NoLiveAgentError) Error() string {
	switch {
	case e.Reporting == 0:
		return fmt.Sprintf("실행 가능한 agent 가 없습니다(클러스터 %s). agent pod 가 떠 있는지 확인하세요.", e.ClusterID)
	case e.StaleAgents > 0:
		return fmt.Sprintf(
			"클러스터 %s 의 agent %d개가 응답하지 않습니다(heartbeat %s 이상 끊김). agent pod 상태를 확인하세요.",
			e.ClusterID, e.StaleAgents, agentLivenessGrace)
	default:
		return fmt.Sprintf(
			"클러스터 %s 에 agent 가 없습니다(다른 클러스터에 %d개 실행 중). 워크플로우의 클러스터 설정을 확인하세요.",
			e.ClusterID, e.Reporting)
	}
}

func (e *NoLiveAgentError) Is(target error) bool { return target == ErrNoLiveAgent }

// AgentLivenessSource 는 살아있는 agent 판정에 필요한 heartbeat 조회다.
// RedisService 가 구현하며, 테스트에서는 대역을 넣는다.
type AgentLivenessSource interface {
	GetAllAgentHeartbeats() (map[string]*types.AgentHeartbeat, error)
}

// EnsureLiveAgent 는 대상 클러스터에 실행을 받을 agent 가 있는지 확인한다.
//
// heartbeat 조회가 실패하면 실행을 막지 않는다 — 검증 오류로 정상 실행을 차단하는 것이
// 유실보다 나쁘다. 그 경우는 발행 후 stale 감지가 후속 방어를 맡는다.
func EnsureLiveAgent(src AgentLivenessSource, clusterID string) error {
	if src == nil {
		return nil
	}

	heartbeats, err := src.GetAllAgentHeartbeats()
	if err != nil {
		slog.Warn("agent liveness check skipped — heartbeat query failed", "error", err)
		return nil
	}

	cutoff := time.Now().Add(-agentLivenessGrace)
	reporting := 0
	stale := 0

	for _, hb := range heartbeats {
		if hb == nil {
			continue
		}
		reporting++

		if heartbeatClusterID(hb) != clusterID {
			continue
		}
		if hb.Timestamp.After(cutoff) {
			return nil // 이 클러스터에 살아있는 agent 가 있다
		}
		stale++
	}

	return &NoLiveAgentError{ClusterID: clusterID, Reporting: reporting, StaleAgents: stale}
}

// heartbeatClusterID 는 클러스터 미지정 agent 를 default 로 본다.
// 클러스터 목록 API(cluster.go)와 같은 규칙을 써야 화면과 실행 판정이 어긋나지 않는다.
func heartbeatClusterID(hb *types.AgentHeartbeat) string {
	if hb.ClusterID == "" {
		return "default"
	}
	return hb.ClusterID
}
