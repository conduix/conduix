package handlers

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

func TestPlanPartitionGroups(t *testing.T) {
	mk := func(parts ...types.PartitionConfig) []types.GroupedPipeline {
		return []types.GroupedPipeline{
			{ID: "p1", Input: types.WorkflowInput{Type: "partitioned_sql", Partitions: parts}},
		}
	}

	// 파티션 없음 → nil(단일 실행).
	if g := planPartitionGroups(mk()); g != nil {
		t.Errorf("no partitions: want nil, got %v", g)
	}

	// 파티션 1개 → nil(분산 불필요).
	if g := planPartitionGroups(mk(types.PartitionConfig{ID: "a", Enabled: true})); g != nil {
		t.Errorf("1 partition: want nil, got %v", g)
	}

	// enabled 3개 → 3개 그룹(각 1개).
	g := planPartitionGroups(mk(
		types.PartitionConfig{ID: "a", Enabled: true},
		types.PartitionConfig{ID: "b", Enabled: true},
		types.PartitionConfig{ID: "c", Enabled: true},
	))
	if len(g) != 3 {
		t.Fatalf("3 enabled: want 3 groups, got %d (%v)", len(g), g)
	}
	seen := map[string]bool{}
	for _, grp := range g {
		if len(grp) != 1 {
			t.Errorf("group size = %d, want 1", len(grp))
		}
		seen[grp[0]] = true
	}
	for _, id := range []string{"a", "b", "c"} {
		if !seen[id] {
			t.Errorf("partition %s 누락", id)
		}
	}

	// disabled 는 제외 → enabled 2개만.
	g2 := planPartitionGroups(mk(
		types.PartitionConfig{ID: "a", Enabled: true},
		types.PartitionConfig{ID: "b", Enabled: false},
		types.PartitionConfig{ID: "c", Enabled: true},
	))
	if len(g2) != 2 {
		t.Errorf("disabled 제외: want 2 groups, got %d (%v)", len(g2), g2)
	}
}
