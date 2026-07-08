// Package assignment 은 partition sub-execution 을 agent(노드)에 배정하는 전략을 제공한다.
//
// 배정은 "선호(preferred)" 지정일 뿐 강제가 아니다. control-plane 이 전략으로 고른 agent 를
// WorkflowExecutionCommand.PreferredAgentID 에 실어 broadcast 하면, 지정 agent 가 우선 claim 하고
// 나머지는 짧게 지연한다. 지정 agent 가 죽으면 지연 후 타 agent 가 SETNX claim 한다(안전망).
//
// 기본 전략은 broadcast(=현행 SETNX 경쟁, PreferredAgentID 비움) → 상위호환 100%.
// 새 전략은 init() 에서 자기 자신을 Register 한다(타입별 switch 하드코딩 금지).
package assignment

import (
	"fmt"
	"sort"
	"sync"
)

// Sub 는 배정 대상 sub-execution 1개(파티션 그룹).
type Sub struct {
	ExecutionID        string
	AssignedPartitions []string
}

// Agent 는 배정 후보(살아있는 agent) 스냅샷.
type Agent struct {
	AgentID      string
	RunningExecs int // 현재 실행 중 개수(heartbeat 관측값) — load_aware 가 부하 지표로 사용
}

// Assignment 은 sub → 선호 agent 매핑 결과. PreferredAgentID="" 면 순수 broadcast(현행).
type Assignment struct {
	ExecutionID      string
	PreferredAgentID string
}

// Strategy 는 sub 묶음을 살아있는 agent 에 배정한다.
// liveAgents 가 비면 구현은 반드시 모든 결과의 PreferredAgentID="" 로 폴백해야 한다(=broadcast, 안전).
type Strategy interface {
	Name() string
	Assign(subs []Sub, liveAgents []Agent) []Assignment
}

var (
	mu         sync.RWMutex
	registry   = map[string]Strategy{}
	defaultKey = "broadcast"
)

// Register 는 전략을 등록한다(각 전략 파일의 init() 에서 호출).
func Register(s Strategy) {
	mu.Lock()
	defer mu.Unlock()
	registry[s.Name()] = s
}

// Get 은 이름으로 전략을 반환한다. 없으면 기본(broadcast)로 폴백한다
// (오타·미지원 값이 실행을 막지 않도록 — 안전 기본값).
func Get(name string) Strategy {
	mu.RLock()
	defer mu.RUnlock()
	if s, ok := registry[name]; ok {
		return s
	}
	return registry[defaultKey]
}

// Names 는 등록된 전략 이름을 정렬해 반환한다(설정 검증·로깅용).
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// broadcastAssignments 는 모든 sub 를 선호 지정 없이(현행 경쟁) 배정한다.
// broadcast 전략과 "liveAgents 비었을 때 폴백" 양쪽에서 공유하는 안전 기본 동작.
func broadcastAssignments(subs []Sub) []Assignment {
	out := make([]Assignment, len(subs))
	for i, s := range subs {
		out[i] = Assignment{ExecutionID: s.ExecutionID, PreferredAgentID: ""}
	}
	return out
}

// mustHaveAgents 는 배정 전제(후보 존재)를 확인한다. 없으면 broadcast 폴백을 쓰라는 신호.
func mustHaveAgents(liveAgents []Agent) error {
	if len(liveAgents) == 0 {
		return fmt.Errorf("no live agents")
	}
	return nil
}
