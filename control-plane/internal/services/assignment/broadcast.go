package assignment

// broadcastStrategy 는 선호 지정 없이 모든 sub 를 broadcast 한다(=현행 SETNX 경쟁).
// 기본 전략 — 활성화해도 동작이 현행과 100% 동일(상위호환의 기준).
type broadcastStrategy struct{}

func init() { Register(broadcastStrategy{}) }

func (broadcastStrategy) Name() string { return "broadcast" }

func (broadcastStrategy) Assign(subs []Sub, _ []Agent) []Assignment {
	return broadcastAssignments(subs)
}
