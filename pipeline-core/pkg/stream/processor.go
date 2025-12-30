package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ProcessorState represents the current state of the processor
type ProcessorState int32

const (
	ProcessorStateCreated ProcessorState = iota
	ProcessorStateRunning
	ProcessorStatePaused
	ProcessorStateStopping
	ProcessorStateStopped
	ProcessorStateFailed
)

func (s ProcessorState) String() string {
	switch s {
	case ProcessorStateCreated:
		return "created"
	case ProcessorStateRunning:
		return "running"
	case ProcessorStatePaused:
		return "paused"
	case ProcessorStateStopping:
		return "stopping"
	case ProcessorStateStopped:
		return "stopped"
	case ProcessorStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// StreamProcessor orchestrates local pipeline processing.
// It runs Source -> Stage chain -> Sink in a single goroutine
// using direct function calls instead of actor message passing.
//
// Architecture:
//
//	┌──────────────────────────────────────────────────────────────┐
//	│                    StreamProcessor                            │
//	│                                                               │
//	│  Source ─(chan)─> ProcessLoop ─(direct calls)─> Sink         │
//	│                        │                                      │
//	│              Stage1.Process()                                 │
//	│              Stage2.Process()                                 │
//	│              Stage3.Process()                                 │
//	│                        │                                      │
//	│              All in single goroutine                          │
//	│              (cache-friendly, no message passing)             │
//	└──────────────────────────────────────────────────────────────┘
type StreamProcessor struct {
	name   string
	source Source
	stages []Stage
	sink   Sink
	logger *slog.Logger

	// State
	state   atomic.Int32
	stateMu sync.RWMutex //nolint:unused

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Stats
	stats      ProcessorStats
	statsMu    sync.RWMutex
	statsStart time.Time

	// Configuration
	bufferSize int
}

// ProcessorConfig holds configuration for StreamProcessor
type ProcessorConfig struct {
	Name       string
	BufferSize int // Channel buffer size between source and processor
	Logger     *slog.Logger
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(cfg ProcessorConfig, source Source, stages []Stage, sink Sink) *StreamProcessor {
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1000
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	p := &StreamProcessor{
		name:       cfg.Name,
		source:     source,
		stages:     stages,
		sink:       sink,
		logger:     cfg.Logger,
		bufferSize: cfg.BufferSize,
		stats: ProcessorStats{
			SourceName: source.Name(),
			StageStats: make(map[string]*StageStats),
		},
	}

	p.state.Store(int32(ProcessorStateCreated))

	// Initialize stage stats
	for _, s := range stages {
		p.stats.StageStats[s.Name()] = &StageStats{Name: s.Name()}
	}

	return p
}

// Start begins processing records
func (p *StreamProcessor) Start(ctx context.Context) error {
	if !p.compareAndSwapState(ProcessorStateCreated, ProcessorStateRunning) &&
		!p.compareAndSwapState(ProcessorStateStopped, ProcessorStateRunning) {
		return fmt.Errorf("processor not in valid state to start: %s", p.State())
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.statsStart = time.Now()

	p.logger.Info("Starting stream processor",
		"name", p.name,
		"source", p.source.Name(),
		"stages", len(p.stages),
		"sink", p.sink.Name())

	// Create channel for source -> processor
	recordChan := make(chan *Record, p.bufferSize)

	// Start source in separate goroutine (it produces to channel)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.source.Start(p.ctx, recordChan); err != nil && err != context.Canceled {
			p.logger.Error("Source error", "error", err)
		}
	}()

	// Start processing loop in main goroutine (reads from channel)
	p.wg.Add(1)
	go p.processLoop(recordChan)

	return nil
}

// processLoop is the main processing loop.
// It reads records from the source channel and processes them
// through the stage chain using DIRECT FUNCTION CALLS.
// This runs in a single goroutine for maximum cache locality.
func (p *StreamProcessor) processLoop(records <-chan *Record) {
	defer p.wg.Done()
	defer func() {
		// Flush sink on exit
		if err := p.sink.Flush(context.Background()); err != nil {
			p.logger.Error("Sink flush error on shutdown", "error", err)
		}
	}()

	for {
		select {
		case <-p.ctx.Done():
			return

		case record, ok := <-records:
			if !ok {
				// Channel closed, source finished
				p.logger.Info("Source finished, exiting process loop")
				return
			}

			// Check if paused
			if p.State() == ProcessorStatePaused {
				// In paused state, we still drain the channel but don't process
				continue
			}

			// Update input stats
			p.updateStats(func(s *ProcessorStats) {
				s.InputCount++
				s.LastRecord = time.Now()
			})

			// Process through stage chain using DIRECT CALLS
			// This is the key optimization: no message passing, no actor overhead
			result, err := p.processRecord(record)
			if err != nil {
				p.updateStats(func(s *ProcessorStats) {
					s.ErrorCount++
				})
				p.logger.Debug("Stage error", "error", err)
				continue
			}

			if result == nil {
				// Record was filtered out
				p.updateStats(func(s *ProcessorStats) {
					s.FilteredCount++
				})
				continue
			}

			// Write to sink (direct call, sink handles batching internally)
			if err := p.sink.Write(p.ctx, result); err != nil {
				p.updateStats(func(s *ProcessorStats) {
					s.ErrorCount++
				})
				p.logger.Error("Sink write error", "error", err)
				continue
			}

			// Update output stats
			p.updateStats(func(s *ProcessorStats) {
				s.OutputCount++
			})
		}
	}
}

// processRecord applies the stage chain to a single record.
// This uses DIRECT FUNCTION CALLS - no message passing.
// All stages execute in the same goroutine for cache locality.
func (p *StreamProcessor) processRecord(record *Record) (*Record, error) {
	current := record

	for _, stage := range p.stages {
		if current == nil {
			return nil, nil
		}

		startTime := time.Now()

		// DIRECT FUNCTION CALL - no actor, no message
		result, err := stage.Process(p.ctx, current)

		latency := time.Since(startTime)

		// Update per-stage stats
		p.updateStageStats(stage.Name(), func(s *StageStats) {
			s.InputCount++
			if err != nil {
				s.ErrorCount++
			} else if result == nil {
				s.FilteredCount++
			} else {
				s.OutputCount++
			}
			// Running average of latency
			if s.AvgLatency == 0 {
				s.AvgLatency = latency
			} else {
				s.AvgLatency = (s.AvgLatency + latency) / 2
			}
		})

		if err != nil {
			return nil, fmt.Errorf("stage %s: %w", stage.Name(), err)
		}

		current = result
	}

	return current, nil
}

// Stop stops the processor
func (p *StreamProcessor) Stop() error {
	if !p.compareAndSwapState(ProcessorStateRunning, ProcessorStateStopping) &&
		!p.compareAndSwapState(ProcessorStatePaused, ProcessorStateStopping) {
		return fmt.Errorf("processor not in valid state to stop: %s", p.State())
	}

	p.logger.Info("Stopping stream processor", "name", p.name)

	// Cancel context to signal shutdown
	if p.cancel != nil {
		p.cancel()
	}

	// Wait for goroutines to finish
	p.wg.Wait()

	// Close resources
	if err := p.source.Close(); err != nil {
		p.logger.Error("Source close error", "error", err)
	}
	for _, s := range p.stages {
		if err := s.Close(); err != nil {
			p.logger.Error("Stage close error", "stage", s.Name(), "error", err)
		}
	}
	if err := p.sink.Close(); err != nil {
		p.logger.Error("Sink close error", "error", err)
	}

	p.state.Store(int32(ProcessorStateStopped))

	// Update processing time
	p.updateStats(func(s *ProcessorStats) {
		s.ProcessingTime = time.Since(p.statsStart)
	})

	p.logger.Info("Stream processor stopped",
		"name", p.name,
		"input", p.stats.InputCount,
		"output", p.stats.OutputCount,
		"filtered", p.stats.FilteredCount,
		"errors", p.stats.ErrorCount,
		"duration", p.stats.ProcessingTime)

	return nil
}

// Pause pauses the processor
func (p *StreamProcessor) Pause() error {
	if !p.compareAndSwapState(ProcessorStateRunning, ProcessorStatePaused) {
		return fmt.Errorf("processor not running, cannot pause: %s", p.State())
	}

	p.source.Pause()
	p.logger.Info("Stream processor paused", "name", p.name)
	return nil
}

// Resume resumes a paused processor
func (p *StreamProcessor) Resume() error {
	if !p.compareAndSwapState(ProcessorStatePaused, ProcessorStateRunning) {
		return fmt.Errorf("processor not paused, cannot resume: %s", p.State())
	}

	p.source.Resume()
	p.logger.Info("Stream processor resumed", "name", p.name)
	return nil
}

// State returns the current processor state
func (p *StreamProcessor) State() ProcessorState {
	return ProcessorState(p.state.Load())
}

func (p *StreamProcessor) compareAndSwapState(old, new ProcessorState) bool {
	return p.state.CompareAndSwap(int32(old), int32(new))
}

// Stats returns a copy of current statistics
func (p *StreamProcessor) Stats() ProcessorStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	// Deep copy
	stats := p.stats
	stats.StageStats = make(map[string]*StageStats)
	for k, v := range p.stats.StageStats {
		copied := *v
		stats.StageStats[k] = &copied
	}

	return stats
}

func (p *StreamProcessor) updateStats(fn func(*ProcessorStats)) {
	p.statsMu.Lock()
	fn(&p.stats)
	p.statsMu.Unlock()
}

func (p *StreamProcessor) updateStageStats(name string, fn func(*StageStats)) {
	p.statsMu.Lock()
	if ss, ok := p.stats.StageStats[name]; ok {
		fn(ss)
	}
	p.statsMu.Unlock()
}

// Name returns the processor name
func (p *StreamProcessor) Name() string {
	return p.name
}

// Source returns the source
func (p *StreamProcessor) Source() Source {
	return p.source
}

// Stages returns the stage chain
func (p *StreamProcessor) Stages() []Stage {
	return p.stages
}

// Sink returns the sink
func (p *StreamProcessor) Sink() Sink {
	return p.sink
}
