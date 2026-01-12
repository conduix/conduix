package executor

import (
	"sync"
	"time"

	"github.com/conduix/conduix/shared/types"
)

const DefaultSampleSize = 5

// SampleBuffer Stage별 데이터 샘플을 저장하는 링버퍼
type SampleBuffer struct {
	mu      sync.RWMutex
	stages  map[string]*stageSampleBuffer
	maxSize int
}

// stageSampleBuffer 개별 Stage의 샘플 버퍼
type stageSampleBuffer struct {
	samples []types.DataSample
	head    int
	count   int
	maxSize int
}

// NewSampleBuffer 새 샘플 버퍼 생성
func NewSampleBuffer(maxSize int) *SampleBuffer {
	if maxSize <= 0 {
		maxSize = DefaultSampleSize
	}
	return &SampleBuffer{
		stages:  make(map[string]*stageSampleBuffer),
		maxSize: maxSize,
	}
}

// AddSample Stage에 데이터 샘플 추가
func (sb *SampleBuffer) AddSample(stageName string, data map[string]any) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	buf, ok := sb.stages[stageName]
	if !ok {
		buf = &stageSampleBuffer{
			samples: make([]types.DataSample, sb.maxSize),
			maxSize: sb.maxSize,
		}
		sb.stages[stageName] = buf
	}

	sample := types.DataSample{
		Data:      copyMap(data),
		Timestamp: time.Now().UnixMilli(),
	}

	buf.samples[buf.head] = sample
	buf.head = (buf.head + 1) % buf.maxSize
	if buf.count < buf.maxSize {
		buf.count++
	}
}

// GetSamples Stage의 최근 샘플들 조회 (최신순)
func (sb *SampleBuffer) GetSamples(stageName string) []types.DataSample {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	buf, ok := sb.stages[stageName]
	if !ok || buf.count == 0 {
		return nil
	}

	result := make([]types.DataSample, buf.count)
	for i := 0; i < buf.count; i++ {
		// 최신 데이터부터 역순으로
		idx := (buf.head - 1 - i + buf.maxSize) % buf.maxSize
		result[i] = buf.samples[idx]
	}
	return result
}

// GetAllSamples 모든 Stage의 샘플 조회
func (sb *SampleBuffer) GetAllSamples() map[string][]types.DataSample {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	result := make(map[string][]types.DataSample, len(sb.stages))
	for stageName, buf := range sb.stages {
		if buf.count == 0 {
			continue
		}
		samples := make([]types.DataSample, buf.count)
		for i := 0; i < buf.count; i++ {
			idx := (buf.head - 1 - i + buf.maxSize) % buf.maxSize
			samples[i] = buf.samples[idx]
		}
		result[stageName] = samples
	}
	return result
}

// Clear 모든 샘플 삭제
func (sb *SampleBuffer) Clear() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.stages = make(map[string]*stageSampleBuffer)
}

// copyMap 맵 깊은 복사 (샘플 데이터 보존용)
func copyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		// 중첩 맵 처리
		if nested, ok := v.(map[string]any); ok {
			dst[k] = copyMap(nested)
		} else {
			dst[k] = v
		}
	}
	return dst
}
