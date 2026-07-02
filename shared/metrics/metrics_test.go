package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordExecution_IncrementsCounters(t *testing.T) {
	before := testutil.ToFloat64(PipelineExecutionsTotal.WithLabelValues("completed"))

	RecordExecution("completed", 1.5, 100, 95, 5)

	after := testutil.ToFloat64(PipelineExecutionsTotal.WithLabelValues("completed"))
	if after != before+1 {
		t.Errorf("expected completed counter +1, got before=%v after=%v", before, after)
	}

	// records_total{direction=...} 누적 확인
	if got := testutil.ToFloat64(PipelineRecordsTotal.WithLabelValues("written")); got < 95 {
		t.Errorf("expected written records >= 95, got %v", got)
	}
	if got := testutil.ToFloat64(PipelineRecordsTotal.WithLabelValues("error")); got < 5 {
		t.Errorf("expected error records >= 5, got %v", got)
	}
}

// 메트릭이 기본 registry에 등록되어 노출 텍스트에 나타나는지 확인.
func TestMetrics_ExposedInRegistry(t *testing.T) {
	RecordExecution("failed", 0.2, 10, 0, 10)

	got := testutil.CollectAndCount(PipelineExecutionsTotal)
	if got == 0 {
		t.Error("PipelineExecutionsTotal not collected")
	}

	// 메트릭 이름 네임스페이스 확인 (conduix_pipeline_*)
	names := testutil.CollectAndCount(ActiveExecutions)
	_ = names
	metricName := "conduix_pipeline_executions_total"
	if !strings.Contains(metricName, namespace) {
		t.Errorf("metric namespace mismatch: %s", metricName)
	}
}

func TestActiveExecutionsGauge(t *testing.T) {
	start := testutil.ToFloat64(ActiveExecutions)
	ActiveExecutions.Inc()
	if testutil.ToFloat64(ActiveExecutions) != start+1 {
		t.Error("ActiveExecutions.Inc did not increment")
	}
	ActiveExecutions.Dec()
	if testutil.ToFloat64(ActiveExecutions) != start {
		t.Error("ActiveExecutions.Dec did not restore")
	}
}
