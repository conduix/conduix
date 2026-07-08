package assignment

// loadAwareStrategy 는 현재 실행 개수(RunningExecs)가 가장 적은 agent 에 우선 배정한다.
// 한 배정 호출 안에서 자기 배정분을 즉시 로컬 반영(+1)해, 같은 호출의 여러 sub 가
// 한 agent 에 몰리지 않게 한다.
//
// 한계(문서화): heartbeat 는 최대 ~10초 stale 이라, 호출 간(여러 워크플로우 동시 실행)에는
// "방금 배정한 부하"가 다음 호출에 반영 안 돼 herd 가 생길 수 있다. v1 은 호출 내 균형만 보장.
// 부하 지표는 개수 기반(CPU/MEM 은 현재 미수집) — Agent.RunningExecs 로 캡슐화해 후속 확장 가능.
type loadAwareStrategy struct{}

func init() { Register(loadAwareStrategy{}) }

func (loadAwareStrategy) Name() string { return "load_aware" }

func (loadAwareStrategy) Assign(subs []Sub, liveAgents []Agent) []Assignment {
	if err := mustHaveAgents(liveAgents); err != nil {
		return broadcastAssignments(subs) // 후보 없으면 안전 폴백
	}
	// 로컬 부하 카운터 복사(원본 스냅샷 불변).
	load := make([]int, len(liveAgents))
	for i, a := range liveAgents {
		load[i] = a.RunningExecs
	}
	out := make([]Assignment, len(subs))
	for i, s := range subs {
		// 매 배정마다 현재 부하 최소 agent 선택. 동률이면 인덱스 낮은 쪽(입력 순서) — 결정적.
		min := 0
		for j := 1; j < len(load); j++ {
			if load[j] < load[min] {
				min = j
			}
		}
		out[i] = Assignment{ExecutionID: s.ExecutionID, PreferredAgentID: liveAgents[min].AgentID}
		load[min]++ // 방금 배정분 즉시 반영
	}
	return out
}
