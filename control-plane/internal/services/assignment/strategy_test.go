package assignment

import "testing"

func subs(ids ...string) []Sub {
	out := make([]Sub, len(ids))
	for i, id := range ids {
		out[i] = Sub{ExecutionID: id}
	}
	return out
}

func agents(specs ...Agent) []Agent { return specs }

func TestGet_UnknownFallsBackToBroadcast(t *testing.T) {
	if got := Get("no_such_strategy").Name(); got != "broadcast" {
		t.Fatalf("unknown strategy should fall back to broadcast, got %q", got)
	}
	if got := Get("round_robin").Name(); got != "round_robin" {
		t.Fatalf("expected round_robin, got %q", got)
	}
}

func TestBroadcast_NoPreferredAgent(t *testing.T) {
	got := Get("broadcast").Assign(subs("a", "b", "c"), agents(Agent{AgentID: "n1"}))
	if len(got) != 3 {
		t.Fatalf("want 3 assignments, got %d", len(got))
	}
	for _, a := range got {
		if a.PreferredAgentID != "" {
			t.Errorf("broadcast must not set preferred agent, got %q for %s", a.PreferredAgentID, a.ExecutionID)
		}
	}
}

func TestRoundRobin_CyclesAcrossAgents(t *testing.T) {
	got := Get("round_robin").Assign(subs("s0", "s1", "s2", "s3"),
		agents(Agent{AgentID: "n1"}, Agent{AgentID: "n2"}))
	want := []string{"n1", "n2", "n1", "n2"}
	for i, a := range got {
		if a.PreferredAgentID != want[i] {
			t.Errorf("sub %d: want %s, got %s", i, want[i], a.PreferredAgentID)
		}
	}
}

func TestRoundRobin_NoAgentsFallsBackToBroadcast(t *testing.T) {
	got := Get("round_robin").Assign(subs("s0", "s1"), nil)
	for _, a := range got {
		if a.PreferredAgentID != "" {
			t.Errorf("no agents → must broadcast (empty preferred), got %q", a.PreferredAgentID)
		}
	}
}

func TestLoadAware_PrefersLeastLoadedAndBalances(t *testing.T) {
	// n1 은 이미 2개 실행 중, n2 는 0개. 두 sub 는 모두 한가한 n2 가 아니라,
	// 첫 배정 후 자기 배정분을 반영해 두 번째는 다시 최소 부하로 가야 한다.
	got := Get("load_aware").Assign(subs("s0", "s1", "s2"),
		agents(Agent{AgentID: "n1", RunningExecs: 2}, Agent{AgentID: "n2", RunningExecs: 0}))
	// 초기 부하: n1=2, n2=0 → s0→n2(0→1), s1→n2(1→2), s2→ n1,n2 동률(2) → 인덱스 낮은 n1.
	want := []string{"n2", "n2", "n1"}
	for i, a := range got {
		if a.PreferredAgentID != want[i] {
			t.Errorf("sub %d: want %s, got %s (load-aware must reflect its own assignments)", i, want[i], a.PreferredAgentID)
		}
	}
}

func TestLoadAware_NoAgentsFallsBackToBroadcast(t *testing.T) {
	got := Get("load_aware").Assign(subs("s0"), nil)
	if got[0].PreferredAgentID != "" {
		t.Errorf("no agents → must broadcast, got %q", got[0].PreferredAgentID)
	}
}

func TestNames_IncludesAllRegistered(t *testing.T) {
	names := Names()
	for _, want := range []string{"broadcast", "round_robin", "load_aware"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("registered strategies missing %q; got %v", want, names)
		}
	}
}
