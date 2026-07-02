package services

import "testing"

func TestIsStaleExecution(t *testing.T) {
	live := map[string]struct{}{
		"exec-alive-1": {},
		"exec-alive-2": {},
	}

	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"live execution is not stale", "exec-alive-1", false},
		{"another live execution is not stale", "exec-alive-2", false},
		{"execution absent from live set is stale", "exec-orphaned", true},
		{"empty id absent is stale", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isStaleExecution(c.id, live); got != c.want {
				t.Errorf("isStaleExecution(%q) = %v, want %v", c.id, got, c.want)
			}
		})
	}
}

func TestIsStaleExecution_NoLiveAgents(t *testing.T) {
	// 살아있는 에이전트가 하나도 없으면 모든 실행이 stale
	empty := map[string]struct{}{}
	if !isStaleExecution("any-exec", empty) {
		t.Error("with no live agents, every running execution should be stale")
	}
}
