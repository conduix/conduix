package assignment

// roundRobinStrategy 는 sub 들을 살아있는 agent 에 순차 배정한다.
// 상태는 호출 범위 내 로컬 인덱스뿐(무상태 전략) — 한 워크플로우 실행의 sub 들만 고르게 나누면 되고,
// 전역 카운터는 락 경합·재시작 소실·테스트 비결정성을 유발하므로 두지 않는다.
type roundRobinStrategy struct{}

func init() { Register(roundRobinStrategy{}) }

func (roundRobinStrategy) Name() string { return "round_robin" }

func (roundRobinStrategy) Assign(subs []Sub, liveAgents []Agent) []Assignment {
	if err := mustHaveAgents(liveAgents); err != nil {
		return broadcastAssignments(subs) // 후보 없으면 안전 폴백
	}
	out := make([]Assignment, len(subs))
	for i, s := range subs {
		out[i] = Assignment{
			ExecutionID:      s.ExecutionID,
			PreferredAgentID: liveAgents[i%len(liveAgents)].AgentID,
		}
	}
	return out
}
