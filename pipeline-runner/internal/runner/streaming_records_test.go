package runner

import (
	"testing"

	"github.com/conduix/conduix/shared/types"
)

// realtime 실행의 처리량은 실시간 통계에서 읽어야 한다.
//
// execution.TotalRecords 는 파이프라인이 끝날 때 result.RecordsWritten 을 누적해
// 만들기 때문에, 끝나지 않는 realtime 에서는 영원히 0 이다. 실측에서 3건을
// 처리했는데 /executions 화면의 Records 열이 0 으로 보였다.
func TestLiveRecordCounts_UsesStatistics(t *testing.T) {
	info := &types.ExecutionMonitoringInfo{
		Pipelines: []types.PipelineMonitoringInfo{
			{
				Statistics: &types.MonitoringStats{
					RecordsProcessed: 3,
					ProcessingErrors: 1,
					CollectionErrors: 2,
				},
			},
		},
	}

	total, failed := liveRecordCounts(info)
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if failed != 3 { // ProcessingErrors + CollectionErrors
		t.Errorf("failed = %d, want 3", failed)
	}
}

// 여러 파이프라인이면 합산한다.
func TestLiveRecordCounts_SumsPipelines(t *testing.T) {
	info := &types.ExecutionMonitoringInfo{
		Pipelines: []types.PipelineMonitoringInfo{
			{Statistics: &types.MonitoringStats{RecordsProcessed: 10}},
			{Statistics: &types.MonitoringStats{RecordsProcessed: 5}},
		},
	}
	if total, _ := liveRecordCounts(info); total != 15 {
		t.Errorf("total = %d, want 15", total)
	}
}

// statistics 가 없으면 마지막 stage 의 출력이 그 파이프라인이 내보낸 수다.
func TestLiveRecordCounts_FallsBackToLastStage(t *testing.T) {
	info := &types.ExecutionMonitoringInfo{
		Pipelines: []types.PipelineMonitoringInfo{
			{
				Stages: []types.StageMonitorInfo{
					{Name: "filter", InputCount: 100, OutputCount: 40, ErrorCount: 1},
					{Name: "remap", InputCount: 40, OutputCount: 40, ErrorCount: 2},
				},
			},
		},
	}
	total, failed := liveRecordCounts(info)
	if total != 40 {
		t.Errorf("total = %d, want 40 (마지막 stage 출력)", total)
	}
	if failed != 3 {
		t.Errorf("failed = %d, want 3 (전 stage 에러 합)", failed)
	}
}

// 모니터링 정보가 없어도 터지지 않는다 — 시작 직후엔 아직 통계가 없다.
func TestLiveRecordCounts_NilSafe(t *testing.T) {
	total, failed := liveRecordCounts(nil)
	if total != 0 || failed != 0 {
		t.Errorf("nil 입력에서 (%d, %d), want (0, 0)", total, failed)
	}
	if total, _ := liveRecordCounts(&types.ExecutionMonitoringInfo{}); total != 0 {
		t.Errorf("빈 파이프라인에서 total = %d, want 0", total)
	}
}
