// Package output 배치 처리 어댑터
package output

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

// BatchingWrapper 기존 Output을 감싸서 배칭 기능 추가
type BatchingWrapper struct {
	output        Output
	batchConfig   BatchConfig
	buffer        []source.Record
	bufferMu      sync.Mutex
	flushTicker   *time.Ticker
	done          chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	stats         OutputStats
	flushCallback func([]source.Record) error
}

// NewBatchingWrapper 배칭 래퍼 생성
func NewBatchingWrapper(output Output, config BatchConfig) *BatchingWrapper {
	if config.Size <= 0 {
		config.Size = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &BatchingWrapper{
		output:      output,
		batchConfig: config,
		buffer:      make([]source.Record, 0, config.Size),
		done:        make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// BatchOutput 지원 여부에 따라 flush 콜백 설정
	if bo, ok := output.(BatchOutput); ok && bo.SupportsBatch() {
		w.flushCallback = func(records []source.Record) error {
			return bo.WriteBatch(ctx, records)
		}
	} else {
		// fallback: 개별 Write 호출
		w.flushCallback = func(records []source.Record) error {
			for _, record := range records {
				if err := output.Write(ctx, record); err != nil {
					return err
				}
			}
			return nil
		}
	}

	return w
}

// Open 출력 열기 및 flush 타이머 시작
func (w *BatchingWrapper) Open(ctx context.Context) error {
	if err := w.output.Open(ctx); err != nil {
		return err
	}

	// 시간 기반 flush goroutine 시작
	w.startFlushTimer()

	log.Printf("[batching] Wrapper opened (batch_size=%d, flush_interval=%v)",
		w.batchConfig.Size, w.batchConfig.FlushInterval)
	return nil
}

// Write 레코드를 버퍼에 추가하고 필요시 flush
func (w *BatchingWrapper) Write(ctx context.Context, record source.Record) error {
	w.bufferMu.Lock()
	w.buffer = append(w.buffer, record)
	shouldFlush := len(w.buffer) >= w.batchConfig.Size
	w.bufferMu.Unlock()

	if shouldFlush {
		return w.Flush(ctx)
	}
	return nil
}

// Flush 버퍼의 레코드를 모두 전송
func (w *BatchingWrapper) Flush(ctx context.Context) error {
	w.bufferMu.Lock()
	if len(w.buffer) == 0 {
		w.bufferMu.Unlock()
		return nil
	}

	// 버퍼 복사 후 초기화
	batch := make([]source.Record, len(w.buffer))
	copy(batch, w.buffer)
	w.buffer = w.buffer[:0]
	w.bufferMu.Unlock()

	// 배치 전송
	atomic.AddInt64(&w.stats.TotalRecords, int64(len(batch)))

	if err := w.flushCallback(batch); err != nil {
		atomic.AddInt64(&w.stats.ErrorRecords, int64(len(batch)))
		return err
	}

	atomic.AddInt64(&w.stats.SuccessRecords, int64(len(batch)))
	atomic.AddInt64(&w.stats.BatchCount, 1)
	w.stats.LastWriteTime = time.Now()

	log.Printf("[batching] Flushed %d records", len(batch))
	return nil
}

// Close 출력 닫기 (남은 버퍼 flush)
func (w *BatchingWrapper) Close() error {
	// flush 타이머 중지
	w.cancel()
	close(w.done)
	w.wg.Wait()

	if w.flushTicker != nil {
		w.flushTicker.Stop()
	}

	// 남은 버퍼 flush
	if err := w.Flush(context.Background()); err != nil {
		log.Printf("[batching] Error flushing remaining buffer: %v", err)
	}

	log.Printf("[batching] Wrapper closed. Total: %d, Success: %d, Errors: %d, Batches: %d",
		w.stats.TotalRecords, w.stats.SuccessRecords, w.stats.ErrorRecords, w.stats.BatchCount)

	return w.output.Close()
}

// Name 출력 이름 반환
func (w *BatchingWrapper) Name() string {
	return w.output.Name() + "_batched"
}

// Stats 통계 반환
func (w *BatchingWrapper) Stats() OutputStats {
	return w.stats
}

// startFlushTimer 시간 기반 flush goroutine 시작
func (w *BatchingWrapper) startFlushTimer() {
	w.flushTicker = time.NewTicker(w.batchConfig.FlushInterval)
	w.wg.Add(1)

	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-w.flushTicker.C:
				if err := w.Flush(w.ctx); err != nil {
					log.Printf("[batching] Timer flush error: %v", err)
				}
			case <-w.done:
				return
			case <-w.ctx.Done():
				return
			}
		}
	}()
}

// BufferSize 현재 버퍼 크기 반환
func (w *BatchingWrapper) BufferSize() int {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()
	return len(w.buffer)
}

// BatchConfig 배치 설정 반환
func (w *BatchingWrapper) BatchConfig() BatchConfig {
	return w.batchConfig
}

// SupportsBatch 배치 지원 여부 (항상 true)
func (w *BatchingWrapper) SupportsBatch() bool {
	return true
}

// WriteBatch 배치 쓰기 (직접 전송, 버퍼 사용 안 함)
func (w *BatchingWrapper) WriteBatch(ctx context.Context, records []source.Record) error {
	return w.flushCallback(records)
}

// WrapWithBatching Output을 배칭 래퍼로 감싸기
// BatchOutput이고 이미 배칭이 활성화된 경우 래핑하지 않음
func WrapWithBatching(output Output, config BatchConfig) Output {
	if !config.Enabled {
		return output
	}

	// 이미 BatchOutput이고 배칭이 활성화된 경우 그대로 반환
	if bo, ok := output.(BatchOutput); ok && bo.SupportsBatch() {
		return output
	}

	return NewBatchingWrapper(output, config)
}
