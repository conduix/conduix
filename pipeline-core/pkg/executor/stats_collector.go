// Package executor 파이프라인 통계 수집기
package executor

import (
	"sync"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// StatsCollector 파이프라인 실행 통계 수집기
// 수집량, 처리량, 필터별 처리량, 에러 등을 추적
type StatsCollector struct {
	mu sync.RWMutex

	pipelineID   string
	pipelineName string

	// 핵심 카운터
	recordsCollected int64 // 수집량 - 소스에서 읽은 레코드
	recordsProcessed int64 // 처리량 - 싱크에 기록된 레코드
	collectionErrors int64 // 수집에러 - 소스 읽기 오류
	processingErrors int64 // 처리에러 - Transform/Sink 오류

	// Transform별 통계
	transformStats map[string]*TransformStatsTracker

	// 타이밍
	startedAt time.Time
}

// TransformStatsTracker Transform별 통계 추적기
type TransformStatsTracker struct {
	Name        string
	Type        string
	InputCount  int64 // Transform 입력 레코드 수
	OutputCount int64 // Transform 출력 레코드 수
	ErrorCount  int64 // Transform 에러 수
}

// NewStatsCollector 새 통계 수집기 생성
func NewStatsCollector(pipelineID, pipelineName string) *StatsCollector {
	return &StatsCollector{
		pipelineID:     pipelineID,
		pipelineName:   pipelineName,
		transformStats: make(map[string]*TransformStatsTracker),
		startedAt:      time.Now(),
	}
}

// RecordCollected 소스에서 레코드 읽음 (수집량 증가)
func (sc *StatsCollector) RecordCollected() {
	sc.mu.Lock()
	sc.recordsCollected++
	sc.mu.Unlock()
}

// RecordCollectedN 소스에서 N개 레코드 읽음
func (sc *StatsCollector) RecordCollectedN(n int64) {
	sc.mu.Lock()
	sc.recordsCollected += n
	sc.mu.Unlock()
}

// RecordProcessed 싱크에 레코드 기록됨 (처리량 증가)
func (sc *StatsCollector) RecordProcessed() {
	sc.mu.Lock()
	sc.recordsProcessed++
	sc.mu.Unlock()
}

// RecordProcessedN 싱크에 N개 레코드 기록됨
func (sc *StatsCollector) RecordProcessedN(n int64) {
	sc.mu.Lock()
	sc.recordsProcessed += n
	sc.mu.Unlock()
}

// RecordCollectionError 수집 에러 발생
func (sc *StatsCollector) RecordCollectionError() {
	sc.mu.Lock()
	sc.collectionErrors++
	sc.mu.Unlock()
}

// RecordCollectionErrorN N개 수집 에러 발생
func (sc *StatsCollector) RecordCollectionErrorN(n int64) {
	sc.mu.Lock()
	sc.collectionErrors += n
	sc.mu.Unlock()
}

// RecordProcessingError 처리 에러 발생 (Transform 또는 Sink)
func (sc *StatsCollector) RecordProcessingError() {
	sc.mu.Lock()
	sc.processingErrors++
	sc.mu.Unlock()
}

// RecordProcessingErrorN N개 처리 에러 발생
func (sc *StatsCollector) RecordProcessingErrorN(n int64) {
	sc.mu.Lock()
	sc.processingErrors += n
	sc.mu.Unlock()
}

// RecordTransformInput Transform에 레코드 입력
func (sc *StatsCollector) RecordTransformInput(transformName, transformType string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if _, ok := sc.transformStats[transformName]; !ok {
		sc.transformStats[transformName] = &TransformStatsTracker{
			Name: transformName,
			Type: transformType,
		}
	}
	sc.transformStats[transformName].InputCount++
}

// RecordTransformOutput Transform에서 레코드 출력
func (sc *StatsCollector) RecordTransformOutput(transformName string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if tracker, ok := sc.transformStats[transformName]; ok {
		tracker.OutputCount++
	}
}

// RecordTransformError Transform 처리 중 에러
func (sc *StatsCollector) RecordTransformError(transformName string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if tracker, ok := sc.transformStats[transformName]; ok {
		tracker.ErrorCount++
	}
	sc.processingErrors++
}

// GetStatistics 현재까지 수집된 통계 반환
func (sc *StatsCollector) GetStatistics() *types.PipelineStatistics {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	now := time.Now()
	transformCounts := make(map[string]int64)
	for name, tracker := range sc.transformStats {
		transformCounts[name] = tracker.OutputCount
	}

	return &types.PipelineStatistics{
		PipelineID:       sc.pipelineID,
		PipelineName:     sc.pipelineName,
		RecordsCollected: sc.recordsCollected,
		RecordsProcessed: sc.recordsProcessed,
		PerStageCounts:   transformCounts,
		CollectionErrors: sc.collectionErrors,
		ProcessingErrors: sc.processingErrors,
		StartedAt:        sc.startedAt,
		CompletedAt:      &now,
		DurationMs:       now.Sub(sc.startedAt).Milliseconds(),
	}
}

// GetStageStats 모든 Stage 통계 반환
func (sc *StatsCollector) GetStageStats() []types.StageStatistics {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := make([]types.StageStatistics, 0, len(sc.transformStats))
	for _, tracker := range sc.transformStats {
		result = append(result, types.StageStatistics{
			Name:        tracker.Name,
			Type:        tracker.Type,
			InputCount:  tracker.InputCount,
			OutputCount: tracker.OutputCount,
			ErrorCount:  tracker.ErrorCount,
		})
	}
	return result
}

// Snapshot 현재 통계 스냅샷 (수정 없이 복사)
func (sc *StatsCollector) Snapshot() StatsSnapshot {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	return StatsSnapshot{
		RecordsCollected: sc.recordsCollected,
		RecordsProcessed: sc.recordsProcessed,
		CollectionErrors: sc.collectionErrors,
		ProcessingErrors: sc.processingErrors,
		Timestamp:        time.Now(),
	}
}

// StatsSnapshot 특정 시점의 통계 스냅샷
type StatsSnapshot struct {
	RecordsCollected int64
	RecordsProcessed int64
	CollectionErrors int64
	ProcessingErrors int64
	Timestamp        time.Time
}

// Reset 통계 초기화
func (sc *StatsCollector) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.recordsCollected = 0
	sc.recordsProcessed = 0
	sc.collectionErrors = 0
	sc.processingErrors = 0
	sc.transformStats = make(map[string]*TransformStatsTracker)
	sc.startedAt = time.Now()
}

// RealtimeStatsAggregator 실시간 파이프라인 시간별 통계 집계기
type RealtimeStatsAggregator struct {
	mu sync.Mutex

	pipelineID   string
	pipelineName string
	workflowID   string
	currentHour  time.Time

	// 시간별 누적 통계
	recordsCollected int64
	recordsProcessed int64
	collectionErrors int64
	processingErrors int64
	transformCounts  map[string]int64
	sampleCount      int

	// 플러시 콜백
	onFlush func(bucket *types.HourlyStatsBucket)
}

// NewRealtimeStatsAggregator 실시간 통계 집계기 생성
func NewRealtimeStatsAggregator(pipelineID, pipelineName, workflowID string, onFlush func(bucket *types.HourlyStatsBucket)) *RealtimeStatsAggregator {
	return &RealtimeStatsAggregator{
		pipelineID:      pipelineID,
		pipelineName:    pipelineName,
		workflowID:      workflowID,
		currentHour:     time.Now().Truncate(time.Hour),
		transformCounts: make(map[string]int64),
		onFlush:         onFlush,
	}
}

// Add 통계를 현재 시간 버킷에 추가
func (rsa *RealtimeStatsAggregator) Add(stats *types.PipelineStatistics) {
	rsa.mu.Lock()
	defer rsa.mu.Unlock()

	currentHour := time.Now().Truncate(time.Hour)

	// 시간이 변경되면 이전 버킷 플러시
	if !currentHour.Equal(rsa.currentHour) {
		rsa.flushLocked()
		rsa.resetLocked(currentHour)
	}

	// 누적
	rsa.recordsCollected += stats.RecordsCollected
	rsa.recordsProcessed += stats.RecordsProcessed
	rsa.collectionErrors += stats.CollectionErrors
	rsa.processingErrors += stats.ProcessingErrors
	rsa.sampleCount++

	for name, count := range stats.PerStageCounts {
		rsa.transformCounts[name] += count
	}
}

// Flush 현재 버킷 강제 플러시
func (rsa *RealtimeStatsAggregator) Flush() {
	rsa.mu.Lock()
	defer rsa.mu.Unlock()
	rsa.flushLocked()
}

func (rsa *RealtimeStatsAggregator) flushLocked() {
	if rsa.sampleCount == 0 {
		return
	}

	if rsa.onFlush != nil {
		bucket := &types.HourlyStatsBucket{
			PipelineID:       rsa.pipelineID,
			WorkflowID:       rsa.workflowID,
			BucketHour:       rsa.currentHour,
			RecordsCollected: rsa.recordsCollected,
			RecordsProcessed: rsa.recordsProcessed,
			PerStageCounts:   rsa.transformCounts,
			CollectionErrors: rsa.collectionErrors,
			ProcessingErrors: rsa.processingErrors,
			SampleCount:      rsa.sampleCount,
			UpdatedAt:        time.Now(),
		}
		rsa.onFlush(bucket)
	}
}

func (rsa *RealtimeStatsAggregator) resetLocked(hour time.Time) {
	rsa.currentHour = hour
	rsa.recordsCollected = 0
	rsa.recordsProcessed = 0
	rsa.collectionErrors = 0
	rsa.processingErrors = 0
	rsa.transformCounts = make(map[string]int64)
	rsa.sampleCount = 0
}
