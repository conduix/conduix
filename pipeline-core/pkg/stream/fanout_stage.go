// Package stream provides fan-out / fan-in stage implementation
package stream

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

// MergeStrategy defines how to merge results from multiple branches
type MergeStrategy string

const (
	// MergeDeep performs deep merge of nested maps
	MergeDeep MergeStrategy = "deep_merge"
	// MergeShallow performs shallow merge (last wins)
	MergeShallow MergeStrategy = "shallow_merge"
	// MergeArray collects results into an array
	MergeArray MergeStrategy = "array"
	// MergeFirst takes the first non-nil result
	MergeFirst MergeStrategy = "first"
)

// FanOutBranch represents a single branch in fan-out processing
type FanOutBranch struct {
	Name    string        `json:"name"`
	Stages  []Stage       `json:"-"` // Stages to execute in this branch
	Timeout time.Duration `json:"timeout"`
	// Optional: only execute if condition matches
	Condition string `json:"condition,omitempty"`
}

// FanOutStageConfig is the configuration for FanOutStage
type FanOutStageConfig struct {
	Branches      []FanOutBranch `json:"branches"`
	MergeStrategy MergeStrategy  `json:"merge_strategy"`
	Parallel      bool           `json:"parallel"`      // Execute branches in parallel
	FailOnError   bool           `json:"fail_on_error"` // Fail if any branch fails
	Timeout       time.Duration  `json:"timeout"`       // Global timeout
}

// FanOutStage splits processing into multiple branches and merges results
type FanOutStage struct {
	BaseStage
	branches      []FanOutBranch
	mergeStrategy MergeStrategy
	parallel      bool
	failOnError   bool
	timeout       time.Duration

	// Metrics per branch
	branchMetrics map[string]*branchMetrics
	metricsMu     sync.RWMutex
}

type branchMetrics struct {
	processed    int64
	errors       int64
	totalLatency int64 // in nanoseconds
}

// NewFanOutStage creates a new fan-out stage
func NewFanOutStage(name string, cfg *FanOutStageConfig) (*FanOutStage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("fan-out stage config is required")
	}

	if len(cfg.Branches) == 0 {
		return nil, fmt.Errorf("at least one branch is required")
	}

	mergeStrategy := cfg.MergeStrategy
	if mergeStrategy == "" {
		mergeStrategy = MergeDeep
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	s := &FanOutStage{
		BaseStage:     BaseStage{name: name, typ: "fan_out", config: map[string]any{}},
		branches:      cfg.Branches,
		mergeStrategy: mergeStrategy,
		parallel:      cfg.Parallel,
		failOnError:   cfg.FailOnError,
		timeout:       timeout,
		branchMetrics: make(map[string]*branchMetrics),
	}

	// Initialize metrics
	for _, branch := range cfg.Branches {
		s.branchMetrics[branch.Name] = &branchMetrics{}
	}

	return s, nil
}

// NewFanOutStageFromConfig creates a fan-out stage from config map
func NewFanOutStageFromConfig(name string, config map[string]any) (*FanOutStage, error) {
	cfg := &FanOutStageConfig{
		Parallel:    true,
		FailOnError: false,
	}

	// Parse merge strategy
	if ms, ok := config["merge_strategy"].(string); ok {
		cfg.MergeStrategy = MergeStrategy(ms)
	}

	// Parse parallel
	if p, ok := config["parallel"].(bool); ok {
		cfg.Parallel = p
	}

	// Parse fail_on_error
	if f, ok := config["fail_on_error"].(bool); ok {
		cfg.FailOnError = f
	}

	// Parse timeout
	if t, ok := config["timeout"].(string); ok {
		if d, err := time.ParseDuration(t); err == nil {
			cfg.Timeout = d
		}
	}

	// Parse branches
	if branchesRaw, ok := config["branches"].([]any); ok {
		for _, b := range branchesRaw {
			if branchMap, ok := b.(map[string]any); ok {
				branch := FanOutBranch{}
				if n, ok := branchMap["name"].(string); ok {
					branch.Name = n
				}
				if cond, ok := branchMap["condition"].(string); ok {
					branch.Condition = cond
				}
				if t, ok := branchMap["timeout"].(string); ok {
					if d, err := time.ParseDuration(t); err == nil {
						branch.Timeout = d
					}
				}

				// Parse stages within branch
				if stagesRaw, ok := branchMap["stages"].([]any); ok {
					for _, s := range stagesRaw {
						if stageMap, ok := s.(map[string]any); ok {
							stageCfg := StageConfig{}
							if t, ok := stageMap["type"].(string); ok {
								stageCfg.Type = t
							}
							if n, ok := stageMap["name"].(string); ok {
								stageCfg.Name = n
							}
							if c, ok := stageMap["config"].(map[string]any); ok {
								stageCfg.Config = c
							}

							stage, err := NewStage(stageCfg)
							if err != nil {
								return nil, fmt.Errorf("failed to create stage %s in branch %s: %w",
									stageCfg.Name, branch.Name, err)
							}
							branch.Stages = append(branch.Stages, stage)
						}
					}
				}

				cfg.Branches = append(cfg.Branches, branch)
			}
		}
	}

	return NewFanOutStage(name, cfg)
}

// SetBranches allows setting branches programmatically
func (s *FanOutStage) SetBranches(branches []FanOutBranch) {
	s.branches = branches
	s.metricsMu.Lock()
	for _, branch := range branches {
		s.branchMetrics[branch.Name] = &branchMetrics{}
	}
	s.metricsMu.Unlock()
}

// Process executes all branches and merges results
func (s *FanOutStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	if len(s.branches) == 0 {
		s.incrementOutput()
		return record, nil
	}

	// Apply global timeout
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	var results []*Record
	var errs []error

	if s.parallel {
		results, errs = s.processParallel(ctx, record)
	} else {
		results, errs = s.processSequential(ctx, record)
	}

	// Handle errors
	if len(errs) > 0 && s.failOnError {
		s.incrementError()
		return nil, fmt.Errorf("fan-out failed: %v", errs)
	}

	// Merge results
	mergedRecord := s.mergeResults(record, results)
	s.incrementOutput()
	return mergedRecord, nil
}

// processParallel executes all branches concurrently
func (s *FanOutStage) processParallel(ctx context.Context, record *Record) ([]*Record, []error) {
	var wg sync.WaitGroup
	results := make([]*Record, len(s.branches))
	errors := make([]error, len(s.branches))

	for i, branch := range s.branches {
		wg.Add(1)
		go func(idx int, b FanOutBranch) {
			defer wg.Done()

			// Apply branch-specific timeout
			branchCtx := ctx
			if b.Timeout > 0 {
				var cancel context.CancelFunc
				branchCtx, cancel = context.WithTimeout(ctx, b.Timeout)
				defer cancel()
			}

			start := time.Now()
			result, err := s.processBranch(branchCtx, b, record)
			latency := time.Since(start)

			s.updateBranchMetrics(b.Name, err == nil, latency)

			results[idx] = result
			errors[idx] = err
		}(i, branch)
	}

	wg.Wait()

	// Filter out nil errors
	var actualErrors []error
	for _, err := range errors {
		if err != nil {
			actualErrors = append(actualErrors, err)
		}
	}

	return results, actualErrors
}

// processSequential executes branches one by one
func (s *FanOutStage) processSequential(ctx context.Context, record *Record) ([]*Record, []error) {
	var results []*Record
	var errors []error

	for _, branch := range s.branches {
		// Apply branch-specific timeout
		branchCtx := ctx
		if branch.Timeout > 0 {
			var cancel context.CancelFunc
			branchCtx, cancel = context.WithTimeout(ctx, branch.Timeout)
			defer cancel()
		}

		start := time.Now()
		result, err := s.processBranch(branchCtx, branch, record)
		latency := time.Since(start)

		s.updateBranchMetrics(branch.Name, err == nil, latency)

		if err != nil {
			errors = append(errors, err)
			if s.failOnError {
				break
			}
		} else {
			results = append(results, result)
		}
	}

	return results, errors
}

// processBranch processes a single branch
func (s *FanOutStage) processBranch(ctx context.Context, branch FanOutBranch, record *Record) (*Record, error) {
	// Copy record for this branch
	branchRecord := &Record{
		Data:      make(map[string]any),
		Metadata:  record.Metadata,
		Timestamp: record.Timestamp,
	}
	maps.Copy(branchRecord.Data, record.Data)

	// Process through all stages in the branch
	current := branchRecord
	var err error

	for _, stage := range branch.Stages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		current, err = stage.Process(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("branch %s stage %s failed: %w", branch.Name, stage.Name(), err)
		}
		if current == nil {
			// Record was filtered out
			return nil, nil
		}
	}

	// Add branch info to result
	if current != nil {
		current.Data["_branch"] = branch.Name
	}

	return current, nil
}

// mergeResults combines results from all branches
func (s *FanOutStage) mergeResults(original *Record, results []*Record) *Record {
	// Start with a copy of original
	merged := &Record{
		Data:      make(map[string]any),
		Metadata:  original.Metadata,
		Timestamp: original.Timestamp,
	}
	maps.Copy(merged.Data, original.Data)

	switch s.mergeStrategy {
	case MergeArray:
		// Collect all results into an array
		var branchResults []map[string]any
		for _, r := range results {
			if r != nil {
				branchResults = append(branchResults, r.Data)
			}
		}
		merged.Data["_branch_results"] = branchResults

	case MergeFirst:
		// Take first non-nil result
		for _, r := range results {
			if r != nil {
				maps.Copy(merged.Data, r.Data)
				break
			}
		}

	case MergeShallow:
		// Simple shallow merge (last wins)
		for _, r := range results {
			if r != nil {
				maps.Copy(merged.Data, r.Data)
			}
		}

	case MergeDeep:
		fallthrough
	default:
		// Deep merge
		for _, r := range results {
			if r != nil {
				deepMerge(merged.Data, r.Data)
			}
		}
	}

	return merged
}

// deepMerge performs deep merge of maps
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			// If both are maps, merge recursively
			if dstMap, ok := dstVal.(map[string]any); ok {
				if srcMap, ok := srcVal.(map[string]any); ok {
					deepMerge(dstMap, srcMap)
					continue
				}
			}
		}
		// Otherwise, overwrite
		dst[key] = srcVal
	}
}

func (s *FanOutStage) updateBranchMetrics(branchName string, success bool, latency time.Duration) {
	s.metricsMu.RLock()
	m, ok := s.branchMetrics[branchName]
	s.metricsMu.RUnlock()

	if ok {
		atomic.AddInt64(&m.processed, 1)
		atomic.AddInt64(&m.totalLatency, int64(latency))
		if !success {
			atomic.AddInt64(&m.errors, 1)
		}
	}
}

// GetBranchMetrics returns metrics for all branches
func (s *FanOutStage) GetBranchMetrics() map[string]map[string]int64 {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	result := make(map[string]map[string]int64)
	for name, m := range s.branchMetrics {
		processed := atomic.LoadInt64(&m.processed)
		avgLatency := int64(0)
		if processed > 0 {
			avgLatency = atomic.LoadInt64(&m.totalLatency) / processed
		}
		result[name] = map[string]int64{
			"processed":      processed,
			"errors":         atomic.LoadInt64(&m.errors),
			"avg_latency_ns": avgLatency,
		}
	}
	return result
}

// Close releases resources
func (s *FanOutStage) Close() error {
	for _, branch := range s.branches {
		for _, stage := range branch.Stages {
			if err := stage.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}
