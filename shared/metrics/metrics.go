// Package metrics는 Conduix 파이프라인 실행 관련 Prometheus 메트릭을 정의한다.
// 기본(default) registry에 등록되므로 promhttp.Handler()로 그대로 노출할 수 있다.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "conduix"

var (
	// PipelineExecutionsTotal은 완료된 파이프라인 실행 수를 상태별로 센다.
	PipelineExecutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "pipeline",
		Name:      "executions_total",
		Help:      "Total number of pipeline executions by terminal status.",
	}, []string{"status"}) // completed | failed | canceled

	// PipelineRecordsTotal은 처리된 레코드 수를 방향별로 센다.
	PipelineRecordsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "pipeline",
		Name:      "records_total",
		Help:      "Total records processed by direction.",
	}, []string{"direction"}) // read | written | error

	// PipelineDurationSeconds는 파이프라인 실행 소요 시간 분포다.
	PipelineDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "pipeline",
		Name:      "duration_seconds",
		Help:      "Pipeline execution duration in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.1, 3, 10), // 0.1s ~ ~1.6h
	})

	// ActiveExecutions는 현재 실행 중인 파이프라인 수다.
	ActiveExecutions = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "pipeline",
		Name:      "active_executions",
		Help:      "Number of pipeline executions currently running.",
	})

	// PipelineRetriesTotal은 재시도 횟수를 센다.
	PipelineRetriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "pipeline",
		Name:      "retries_total",
		Help:      "Total number of pipeline execution retries.",
	})
)

// RecordExecution은 파이프라인 실행 1건의 결과 메트릭을 기록한다.
// status는 completed|failed|canceled, durationSeconds는 실행 소요 시간이다.
func RecordExecution(status string, durationSeconds float64, recordsRead, recordsWritten, errorCount int64) {
	PipelineExecutionsTotal.WithLabelValues(status).Inc()
	PipelineDurationSeconds.Observe(durationSeconds)
	if recordsRead > 0 {
		PipelineRecordsTotal.WithLabelValues("read").Add(float64(recordsRead))
	}
	if recordsWritten > 0 {
		PipelineRecordsTotal.WithLabelValues("written").Add(float64(recordsWritten))
	}
	if errorCount > 0 {
		PipelineRecordsTotal.WithLabelValues("error").Add(float64(errorCount))
	}
}
