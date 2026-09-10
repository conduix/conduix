package services

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
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

// ErrNoLiveAgent 는 대상 클러스터에 실행을 받을 agent 가 없을 때 반환한다.
var ErrNoLiveAgent = errors.New("no live agent in target cluster")

// agentLivenessGrace 는 heartbeat 가 이만큼 끊긴 agent 를 죽은 것으로 본다.
//
// agent heartbeat 주기보다 충분히 길어야 한다 — 짧으면 살아있는 agent 를 죽었다고 보고
// 정상 실행을 막는다. status 컬럼만 믿을 수 없는 이유: agent 가 비정상 종료하면
// online 인 채로 남는다(정상 종료 경로만 offline 로 갱신한다).
const agentLivenessGrace = 90 * time.Second

// NoLiveAgentError 는 왜 실행할 수 없는지와 사용자가 무엇을 확인해야 하는지 담는다.
type NoLiveAgentError struct {
	ClusterID string
	// Registered 는 이 클러스터에 등록된 agent 수다. 0 이면 배포가 안 된 것이고,
	// 0 이 아니면 떠 있어야 할 agent 가 죽은 것이다 — 조치가 다르다.
	Registered int
	// StaleAgents 는 등록됐지만 heartbeat 가 끊긴 agent 수다.
	StaleAgents int
}

func (e *NoLiveAgentError) Error() string {
	if e.Registered == 0 {
		return fmt.Sprintf("클러스터 %s 에 등록된 agent 가 없습니다. agent 를 배포한 뒤 실행하세요.", e.ClusterID)
	}
	return fmt.Sprintf(
		"클러스터 %s 의 agent %d개가 모두 응답하지 않습니다(heartbeat %s 이상 끊김). agent pod 상태를 확인하세요.",
		e.ClusterID, e.StaleAgents, agentLivenessGrace)
}

func (e *NoLiveAgentError) Is(target error) bool { return target == ErrNoLiveAgent }

// EnsureLiveAgent 는 대상 클러스터에 실행을 받을 agent 가 있는지 확인한다.
//
// tx 를 받는 이유: 실행 시작 트랜잭션 안에서 검사해, 검사 통과 후 실행 레코드를 만드는
// 사이에 상태가 갈리지 않게 한다.
func EnsureLiveAgent(tx *gorm.DB, clusterID string) error {
	var agents []models.Agent
	if err := tx.Select("id", "status", "last_heartbeat").
		Where("cluster_id = ?", clusterID).Find(&agents).Error; err != nil {
		// 조회 자체가 실패하면 실행을 막지 않는다 — 검증 실패로 정상 실행을 차단하는 것이
		// 더 나쁘다. 발행 후 stale 감지가 후속 방어를 맡는다.
		return nil
	}

	if len(agents) == 0 {
		return &NoLiveAgentError{ClusterID: clusterID}
	}

	cutoff := time.Now().Add(-agentLivenessGrace)
	stale := 0
	for _, a := range agents {
		if agentIsLive(a, cutoff) {
			return nil
		}
		stale++
	}

	return &NoLiveAgentError{ClusterID: clusterID, Registered: len(agents), StaleAgents: stale}
}

// agentIsLive 는 heartbeat 기준으로 살아있는지 본다.
//
// heartbeat 가 아예 없는(방금 등록돼 아직 한 번도 보내지 않은) agent 는 살아있는 것으로
// 본다 — 등록 직후 실행을 막으면 첫 배포에서 실행이 안 된다.
func agentIsLive(a models.Agent, cutoff time.Time) bool {
	if a.Status == "offline" {
		return false
	}
	if a.LastHeartbeat == nil {
		return true
	}
	return a.LastHeartbeat.After(cutoff)
}
