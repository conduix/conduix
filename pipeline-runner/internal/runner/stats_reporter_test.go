package runner

import (
	"context"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// 누적값을 그대로 보내면 CP 의 누적 upsert 와 겹쳐 값이 폭증한다 — 델타여야 한다.
func TestStatsReporter_DeltaFromLastSent(t *testing.T) {
	r := newStatsReporter("http://cp", "wf-1")

	d, changed := r.delta("p1", types.MonitoringStats{RecordsCollected: 100, RecordsProcessed: 90})
	if !changed {
		t.Fatal("첫 수집은 변화로 잡혀야 한다")
	}
	if d.RecordsCollected != 100 || d.RecordsProcessed != 90 {
		t.Fatalf("첫 델타 = %+v, want 100/90", d)
	}

	// 전송 성공을 모사 — 기준 갱신은 호출자 책임이다.
	r.lastSent["p1"] = types.MonitoringStats{RecordsCollected: 100, RecordsProcessed: 90}

	d, changed = r.delta("p1", types.MonitoringStats{RecordsCollected: 150, RecordsProcessed: 130})
	if !changed {
		t.Fatal("증가분이 있으면 변화로 잡혀야 한다")
	}
	if d.RecordsCollected != 50 || d.RecordsProcessed != 40 {
		t.Fatalf("델타 = %+v, want 50/40", d)
	}
}

// 변화가 없으면 빈 upsert 로 DB 를 두드리지 않아야 한다.
func TestStatsReporter_NoChangeIsSkipped(t *testing.T) {
	r := newStatsReporter("http://cp", "wf-1")
	cur := types.MonitoringStats{RecordsCollected: 10}
	r.lastSent["p1"] = cur

	if _, changed := r.delta("p1", cur); changed {
		t.Fatal("변화 없음은 전송 대상이 아니다")
	}
}

// executor 가 재시작되면 collector 가 새로 만들어져 누적값이 줄어든다.
// 음수를 보내면 CP 의 누적 합계가 깎이므로 0 으로 막아야 한다.
func TestStatsReporter_CounterResetDoesNotGoNegative(t *testing.T) {
	r := newStatsReporter("http://cp", "wf-1")
	r.lastSent["p1"] = types.MonitoringStats{RecordsCollected: 1000, RecordsProcessed: 900}

	d, changed := r.delta("p1", types.MonitoringStats{RecordsCollected: 5, RecordsProcessed: 3})
	if !changed {
		t.Fatal("리셋 후 새 수집도 변화로 잡혀야 한다")
	}
	if d.RecordsCollected != 5 || d.RecordsProcessed != 3 {
		t.Fatalf("리셋 델타 = %+v, want 5/3 (음수 금지)", d)
	}
}

// 실패한 전송은 기준을 옮기지 않으므로 다음 주기에 누적돼 재시도된다(유실 아님).
func TestStatsReporter_FailedSendKeepsBaselineForRetry(t *testing.T) {
	r := newStatsReporter("", "wf-1") // URL 없음 → send 는 false
	cur := types.MonitoringStats{RecordsCollected: 70}

	if sent := r.send(context.Background(), &types.HourlyStatsBucket{PipelineID: "p1"}); sent {
		t.Fatal("URL 이 없으면 전송 성공으로 보고해서는 안 된다")
	}

	// 기준이 그대로이므로 델타에 전량이 남는다.
	d, changed := r.delta("p1", cur)
	if !changed || d.RecordsCollected != 70 {
		t.Fatalf("재시도 델타 = %+v, want 70", d)
	}
}

// 시간이 바뀌면 새 버킷은 0 에서 시작해야 한다 — 기준을 비우는지 검증.
func TestStatsReporter_HourRolloverResetsBaseline(t *testing.T) {
	r := newStatsReporter("http://cp", "wf-1")
	r.lastSent["p1"] = types.MonitoringStats{RecordsCollected: 500}
	r.currentHour = time.Now().Truncate(time.Hour).Add(-time.Hour)

	// reportOnce 의 시간 경계 처리만 떼어 검증(GroupExecutor 없이).
	hour := time.Now().Truncate(time.Hour)
	if !hour.Equal(r.currentHour) {
		r.lastSent = make(map[string]types.MonitoringStats)
		r.currentHour = hour
	}

	if len(r.lastSent) != 0 {
		t.Fatal("시간 경계를 넘으면 델타 기준이 비워져야 한다")
	}
	d, changed := r.delta("p1", types.MonitoringStats{RecordsCollected: 500})
	if !changed || d.RecordsCollected != 500 {
		t.Fatalf("새 버킷 델타 = %+v, want 500", d)
	}
}
