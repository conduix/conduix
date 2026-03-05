package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// WindowType defines the type of window
type WindowType string

const (
	WindowTumbling WindowType = "tumbling"
	WindowSliding  WindowType = "sliding"
	WindowSession  WindowType = "session"
)

// EmitMode defines when to emit aggregation results
type EmitMode string

const (
	EmitOnClose  EmitMode = "on_close"
	EmitPeriodic EmitMode = "periodic"
	EmitOnUpdate EmitMode = "on_update"
)

// AggregationFunction defines supported aggregation functions
type AggregationFunction string

const (
	AggCount         AggregationFunction = "count"
	AggSum           AggregationFunction = "sum"
	AggAvg           AggregationFunction = "avg"
	AggMin           AggregationFunction = "min"
	AggMax           AggregationFunction = "max"
	AggCountDistinct AggregationFunction = "count_distinct"
	AggFirst         AggregationFunction = "first"
	AggLast          AggregationFunction = "last"
)

// AggregationConfig defines a single aggregation
type AggregationConfig struct {
	Field     string              `yaml:"field" json:"field"`       // output field name
	Function  AggregationFunction `yaml:"function" json:"function"` // aggregation function
	Source    string              `yaml:"source" json:"source"`     // source field (optional, defaults to Field)
	countOnly bool                // internal: true if count without explicit source
}

// WindowedAggregateStage performs windowed aggregations
type WindowedAggregateStage struct {
	BaseStage
	windowType        WindowType
	windowSize        time.Duration
	slideInterval     time.Duration // for sliding windows
	sessionGap        time.Duration // for session windows
	gracePeriod       time.Duration
	groupBy           []string
	aggregations      []AggregationConfig
	emitMode          EmitMode
	includeWindowInfo bool
	timestampField    string // field to use for event time

	// Window state
	windows   map[string]*windowState // key: group key, value: window state
	windowsMu sync.RWMutex

	// Output channel for aggregated results
	outputCh chan *Record

	// Watermark
	watermark time.Time

	// Cleanup ticker
	cleanupTicker *time.Ticker
	closeCh       chan struct{}

	// Redis state store (optional)
	redisClient        *redis.Client
	redisKeyPrefix     string
	statePersistPeriod time.Duration
	persistTicker      *time.Ticker
}

// windowState holds the state for a window
type windowState struct {
	startTime    time.Time
	endTime      time.Time
	groupKey     string
	groupValues  map[string]any
	aggregators  map[string]*aggregator
	lastActivity time.Time // for session windows
}

// aggregator holds state for a single aggregation
type aggregator struct {
	function    AggregationFunction
	count       int64
	sum         float64
	min         float64
	max         float64
	first       any
	last        any
	distinctSet map[uint64]struct{}
	initialized bool
}

func newAggregator(fn AggregationFunction) *aggregator {
	return &aggregator{
		function:    fn,
		distinctSet: make(map[uint64]struct{}),
	}
}

func (a *aggregator) Add(value any) {
	if value == nil {
		return
	}

	switch a.function {
	case AggCount:
		a.count++
	case AggSum, AggAvg:
		if v := toFloat64(value); v != nil {
			a.sum += *v
			a.count++
		}
	case AggMin:
		if v := toFloat64(value); v != nil {
			if !a.initialized || *v < a.min {
				a.min = *v
				a.initialized = true
			}
		}
	case AggMax:
		if v := toFloat64(value); v != nil {
			if !a.initialized || *v > a.max {
				a.max = *v
				a.initialized = true
			}
		}
	case AggCountDistinct:
		h := hashValue(value)
		a.distinctSet[h] = struct{}{}
	case AggFirst:
		if !a.initialized {
			a.first = value
			a.initialized = true
		}
	case AggLast:
		a.last = value
		a.initialized = true
	}
}

func (a *aggregator) Result() any {
	switch a.function {
	case AggCount:
		return a.count
	case AggSum:
		return a.sum
	case AggAvg:
		if a.count == 0 {
			return 0.0
		}
		return a.sum / float64(a.count)
	case AggMin:
		if !a.initialized {
			return nil
		}
		return a.min
	case AggMax:
		if !a.initialized {
			return nil
		}
		return a.max
	case AggCountDistinct:
		return int64(len(a.distinctSet))
	case AggFirst:
		return a.first
	case AggLast:
		return a.last
	default:
		return nil
	}
}

// NewWindowedAggregateStage creates a new windowed aggregation stage
func NewWindowedAggregateStage(name string, config map[string]any) (*WindowedAggregateStage, error) {
	s := &WindowedAggregateStage{
		BaseStage:          BaseStage{name: name, typ: "windowed_aggregate", config: config},
		windowType:         WindowTumbling,
		windowSize:         time.Minute,
		gracePeriod:        10 * time.Second,
		emitMode:           EmitOnClose,
		includeWindowInfo:  true,
		timestampField:     "_timestamp",
		windows:            make(map[string]*windowState),
		outputCh:           make(chan *Record, 1000),
		closeCh:            make(chan struct{}),
		statePersistPeriod: 30 * time.Second, // 기본 30초마다 상태 저장
	}

	// Parse window config
	if window, ok := config["window"].(map[string]any); ok {
		if t, ok := window["type"].(string); ok {
			s.windowType = WindowType(t)
		}
		if size, ok := window["size"].(string); ok {
			if d, err := time.ParseDuration(size); err == nil {
				s.windowSize = d
			}
		}
		if slide, ok := window["slide"].(string); ok {
			if d, err := time.ParseDuration(slide); err == nil {
				s.slideInterval = d
			}
		}
		if gap, ok := window["session_gap"].(string); ok {
			if d, err := time.ParseDuration(gap); err == nil {
				s.sessionGap = d
			}
		}
		if grace, ok := window["grace_period"].(string); ok {
			if d, err := time.ParseDuration(grace); err == nil {
				s.gracePeriod = d
			}
		}
	}

	// Parse group_by
	if gb, ok := config["group_by"].([]any); ok {
		for _, g := range gb {
			if gs, ok := g.(string); ok {
				s.groupBy = append(s.groupBy, gs)
			}
		}
	}

	// Parse aggregations
	if aggs, ok := config["aggregations"].([]any); ok {
		for _, agg := range aggs {
			if aggMap, ok := agg.(map[string]any); ok {
				ac := AggregationConfig{}
				if f, ok := aggMap["field"].(string); ok {
					ac.Field = f
				}
				if fn, ok := aggMap["function"].(string); ok {
					ac.Function = AggregationFunction(fn)
				}
				if src, ok := aggMap["source"].(string); ok {
					ac.Source = src
				}
				// For count without explicit source, just count records
				if ac.Function == AggCount && ac.Source == "" {
					ac.countOnly = true
				}
				if ac.Source == "" {
					ac.Source = ac.Field
				}
				s.aggregations = append(s.aggregations, ac)
			}
		}
	}

	// Parse emit config
	if emit, ok := config["emit"].(map[string]any); ok {
		if mode, ok := emit["mode"].(string); ok {
			s.emitMode = EmitMode(mode)
		}
		if incl, ok := emit["include_window_info"].(bool); ok {
			s.includeWindowInfo = incl
		}
	}

	// Parse timestamp field
	if tf, ok := config["timestamp_field"].(string); ok {
		s.timestampField = tf
	}

	// Parse Redis state store config
	if stateStore, ok := config["state_store"].(map[string]any); ok {
		if storeType, ok := stateStore["type"].(string); ok && storeType == "redis" {
			if addr, ok := stateStore["address"].(string); ok {
				password := ""
				if p, ok := stateStore["password"].(string); ok {
					password = p
				}
				db := 0
				if d, ok := stateStore["db"].(int); ok {
					db = d
				}
				if df, ok := stateStore["db"].(float64); ok {
					db = int(df)
				}

				s.redisClient = redis.NewClient(&redis.Options{
					Addr:     addr,
					Password: password,
					DB:       db,
				})

				// Key prefix
				s.redisKeyPrefix = fmt.Sprintf("conduix:window:%s:", name)
				if prefix, ok := stateStore["key_prefix"].(string); ok {
					s.redisKeyPrefix = prefix
				}

				// Persist period
				if period, ok := stateStore["persist_period"].(string); ok {
					if d, err := time.ParseDuration(period); err == nil {
						s.statePersistPeriod = d
					}
				}

				// Test connection
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := s.redisClient.Ping(ctx).Result(); err != nil {
					cancel()
					return nil, fmt.Errorf("failed to connect to Redis: %w", err)
				}
				cancel()

				// Restore state from Redis
				if err := s.restoreStateFromRedis(); err != nil {
					fmt.Printf("[windowed_aggregate] Warning: failed to restore state from Redis: %v\n", err)
				}

				// Start persist goroutine
				s.persistTicker = time.NewTicker(s.statePersistPeriod)
				go s.persistLoop()

				fmt.Printf("[windowed_aggregate] Redis state store enabled: %s (prefix=%s, persist_period=%s)\n",
					addr, s.redisKeyPrefix, s.statePersistPeriod)
			}
		}
	}

	// Start cleanup goroutine
	s.cleanupTicker = time.NewTicker(s.windowSize / 2)
	go s.cleanupLoop()

	return s, nil
}

func (s *WindowedAggregateStage) cleanupLoop() {
	for {
		select {
		case <-s.closeCh:
			return
		case <-s.cleanupTicker.C:
			s.cleanupExpiredWindows()
		}
	}
}

func (s *WindowedAggregateStage) cleanupExpiredWindows() {
	s.windowsMu.Lock()
	defer s.windowsMu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	for key, ws := range s.windows {
		switch s.windowType {
		case WindowTumbling, WindowSliding:
			if now.After(ws.endTime.Add(s.gracePeriod)) {
				// Emit result and mark for deletion
				s.emitWindowResult(ws)
				expiredKeys = append(expiredKeys, key)
			}
		case WindowSession:
			if now.Sub(ws.lastActivity) > s.sessionGap+s.gracePeriod {
				s.emitWindowResult(ws)
				expiredKeys = append(expiredKeys, key)
			}
		}
	}

	for _, key := range expiredKeys {
		delete(s.windows, key)
	}
}

func (s *WindowedAggregateStage) emitWindowResult(ws *windowState) {
	result := make(map[string]any)

	// Add group by values
	for k, v := range ws.groupValues {
		result[k] = v
	}

	// Add aggregation results
	for field, agg := range ws.aggregators {
		result[field] = agg.Result()
	}

	// Add window info if configured
	if s.includeWindowInfo {
		result["_window_start"] = ws.startTime.Format(time.RFC3339)
		result["_window_end"] = ws.endTime.Format(time.RFC3339)
		result["_window_group"] = ws.groupKey
	}

	record := &Record{
		Data: result,
		Metadata: RecordMetadata{
			Source: "aggregate",
		},
		Timestamp: time.Now(),
	}

	select {
	case s.outputCh <- record:
	default:
		// Channel full, log warning
		fmt.Printf("[windowed_aggregate] Output channel full, dropping aggregation result\n")
	}
}

func (s *WindowedAggregateStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Get event time
	eventTime := time.Now()
	if ts, ok := record.Data[s.timestampField]; ok {
		if t, ok := ts.(time.Time); ok {
			eventTime = t
		} else if tStr, ok := ts.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, tStr); err == nil {
				eventTime = parsed
			}
		} else if tMs, ok := ts.(int64); ok {
			eventTime = time.UnixMilli(tMs)
		} else if tMs, ok := ts.(float64); ok {
			eventTime = time.UnixMilli(int64(tMs))
		}
	}

	// Update watermark
	if eventTime.After(s.watermark) {
		s.watermark = eventTime
	}

	// Get group key
	groupKey, groupValues := s.getGroupKey(record.Data)

	// Get or create window
	ws := s.getOrCreateWindow(groupKey, groupValues, eventTime)

	// Add record to window
	s.addToWindow(ws, record.Data)

	// Check if we should emit immediately (on_update mode)
	if s.emitMode == EmitOnUpdate {
		s.emitWindowResult(ws)
	}

	s.incrementOutput()

	// Check for any aggregated results to emit
	select {
	case result := <-s.outputCh:
		return result, nil
	default:
		// No aggregated result ready, pass through
		return record, nil
	}
}

func (s *WindowedAggregateStage) getGroupKey(data map[string]any) (string, map[string]any) {
	if len(s.groupBy) == 0 {
		return "_global_", make(map[string]any)
	}

	values := make(map[string]any)
	keyParts := make([]string, 0, len(s.groupBy))

	for _, field := range s.groupBy {
		val, _ := getNestedValue(data, field)
		values[field] = val
		keyParts = append(keyParts, fmt.Sprintf("%v", val))
	}

	return fmt.Sprintf("%v", keyParts), values
}

func (s *WindowedAggregateStage) getOrCreateWindow(groupKey string, groupValues map[string]any, eventTime time.Time) *windowState {
	s.windowsMu.Lock()
	defer s.windowsMu.Unlock()

	var windowKey string
	var startTime, endTime time.Time

	switch s.windowType {
	case WindowTumbling:
		// Align to window boundaries
		windowStart := eventTime.Truncate(s.windowSize)
		windowKey = fmt.Sprintf("%s_%d", groupKey, windowStart.Unix())
		startTime = windowStart
		endTime = windowStart.Add(s.windowSize)

	case WindowSliding:
		// For sliding windows, we may need multiple windows
		// Simplified: use the most recent window that contains this event
		windowStart := eventTime.Truncate(s.slideInterval)
		windowKey = fmt.Sprintf("%s_%d", groupKey, windowStart.Unix())
		startTime = windowStart
		endTime = windowStart.Add(s.windowSize)

	case WindowSession:
		// Session windows use activity-based grouping
		windowKey = groupKey
		// Check if existing session window
		if existing, ok := s.windows[windowKey]; ok {
			if eventTime.Sub(existing.lastActivity) <= s.sessionGap {
				// Extend existing session
				existing.lastActivity = eventTime
				if eventTime.After(existing.endTime) {
					existing.endTime = eventTime
				}
				return existing
			}
			// Session expired, emit old and create new
			s.emitWindowResult(existing)
		}
		startTime = eventTime
		endTime = eventTime
	}

	// Get or create window
	ws, exists := s.windows[windowKey]
	if !exists {
		ws = &windowState{
			startTime:    startTime,
			endTime:      endTime,
			groupKey:     groupKey,
			groupValues:  groupValues,
			aggregators:  make(map[string]*aggregator),
			lastActivity: eventTime,
		}

		// Initialize aggregators
		for _, ac := range s.aggregations {
			ws.aggregators[ac.Field] = newAggregator(ac.Function)
		}

		s.windows[windowKey] = ws
	}

	ws.lastActivity = eventTime
	return ws
}

func (s *WindowedAggregateStage) addToWindow(ws *windowState, data map[string]any) {
	for _, ac := range s.aggregations {
		agg := ws.aggregators[ac.Field]
		if agg == nil {
			continue
		}

		var value any
		if ac.countOnly {
			// Count without explicit source: just count records
			value = 1
		} else {
			value, _ = getNestedValue(data, ac.Source)
		}

		agg.Add(value)
	}
}

// GetAggregatedResults returns buffered aggregation results
func (s *WindowedAggregateStage) GetAggregatedResults() []*Record {
	results := make([]*Record, 0)
	for {
		select {
		case r := <-s.outputCh:
			results = append(results, r)
		default:
			return results
		}
	}
}

// FlushWindows forces emission of all current windows
func (s *WindowedAggregateStage) FlushWindows() []*Record {
	s.windowsMu.Lock()
	defer s.windowsMu.Unlock()

	results := make([]*Record, 0, len(s.windows))

	for key, ws := range s.windows {
		s.emitWindowResult(ws)
		delete(s.windows, key)
	}

	// Collect from output channel
	for {
		select {
		case r := <-s.outputCh:
			results = append(results, r)
		default:
			return results
		}
	}
}

// persistLoop 주기적으로 상태를 Redis에 저장
func (s *WindowedAggregateStage) persistLoop() {
	for {
		select {
		case <-s.closeCh:
			return
		case <-s.persistTicker.C:
			if err := s.persistStateToRedis(); err != nil {
				fmt.Printf("[windowed_aggregate] Error persisting state to Redis: %v\n", err)
			}
		}
	}
}

// persistStateToRedis 현재 윈도우 상태를 Redis에 저장
func (s *WindowedAggregateStage) persistStateToRedis() error {
	if s.redisClient == nil {
		return nil
	}

	s.windowsMu.RLock()
	defer s.windowsMu.RUnlock()

	ctx := context.Background()
	pipe := s.redisClient.Pipeline()

	for key, ws := range s.windows {
		// Serialize window state
		stateData := serializableWindowState{
			StartTime:    ws.startTime.UnixMilli(),
			EndTime:      ws.endTime.UnixMilli(),
			GroupKey:     ws.groupKey,
			GroupValues:  ws.groupValues,
			LastActivity: ws.lastActivity.UnixMilli(),
			Aggregators:  make(map[string]serializableAggregator),
		}

		for field, agg := range ws.aggregators {
			stateData.Aggregators[field] = serializableAggregator{
				Function:    string(agg.function),
				Count:       agg.count,
				Sum:         agg.sum,
				Min:         agg.min,
				Max:         agg.max,
				First:       agg.first,
				Last:        agg.last,
				DistinctSet: agg.distinctSet,
				Initialized: agg.initialized,
			}
		}

		data, err := json.Marshal(stateData)
		if err != nil {
			continue
		}

		redisKey := s.redisKeyPrefix + key
		pipe.Set(ctx, redisKey, data, s.windowSize+s.gracePeriod+time.Hour) // TTL
	}

	// Store watermark
	pipe.Set(ctx, s.redisKeyPrefix+"__watermark__", s.watermark.UnixMilli(), 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist state: %w", err)
	}

	fmt.Printf("[windowed_aggregate] Persisted %d windows to Redis\n", len(s.windows))
	return nil
}

// restoreStateFromRedis Redis에서 윈도우 상태 복원
func (s *WindowedAggregateStage) restoreStateFromRedis() error {
	if s.redisClient == nil {
		return nil
	}

	ctx := context.Background()

	// Get all keys with prefix
	keys, err := s.redisClient.Keys(ctx, s.redisKeyPrefix+"*").Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	s.windowsMu.Lock()
	defer s.windowsMu.Unlock()

	restoredCount := 0
	for _, key := range keys {
		// Skip watermark key
		if key == s.redisKeyPrefix+"__watermark__" {
			wmData, err := s.redisClient.Get(ctx, key).Result()
			if err == nil {
				var wmMs int64
				if json.Unmarshal([]byte(wmData), &wmMs) == nil {
					s.watermark = time.UnixMilli(wmMs)
				}
			}
			continue
		}

		data, err := s.redisClient.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var stateData serializableWindowState
		if err := json.Unmarshal([]byte(data), &stateData); err != nil {
			continue
		}

		// Reconstruct window state
		ws := &windowState{
			startTime:    time.UnixMilli(stateData.StartTime),
			endTime:      time.UnixMilli(stateData.EndTime),
			groupKey:     stateData.GroupKey,
			groupValues:  stateData.GroupValues,
			lastActivity: time.UnixMilli(stateData.LastActivity),
			aggregators:  make(map[string]*aggregator),
		}

		for field, aggData := range stateData.Aggregators {
			agg := &aggregator{
				function:    AggregationFunction(aggData.Function),
				count:       aggData.Count,
				sum:         aggData.Sum,
				min:         aggData.Min,
				max:         aggData.Max,
				first:       aggData.First,
				last:        aggData.Last,
				distinctSet: aggData.DistinctSet,
				initialized: aggData.Initialized,
			}
			if agg.distinctSet == nil {
				agg.distinctSet = make(map[uint64]struct{})
			}
			ws.aggregators[field] = agg
		}

		windowKey := key[len(s.redisKeyPrefix):]
		s.windows[windowKey] = ws
		restoredCount++
	}

	if restoredCount > 0 {
		fmt.Printf("[windowed_aggregate] Restored %d windows from Redis (watermark: %s)\n",
			restoredCount, s.watermark.Format(time.RFC3339))
	}

	return nil
}

// serializableWindowState Redis 저장용 직렬화 가능한 윈도우 상태
type serializableWindowState struct {
	StartTime    int64                             `json:"start_time"`
	EndTime      int64                             `json:"end_time"`
	GroupKey     string                            `json:"group_key"`
	GroupValues  map[string]any                    `json:"group_values"`
	LastActivity int64                             `json:"last_activity"`
	Aggregators  map[string]serializableAggregator `json:"aggregators"`
}

// serializableAggregator Redis 저장용 직렬화 가능한 집계자
type serializableAggregator struct {
	Function    string              `json:"function"`
	Count       int64               `json:"count"`
	Sum         float64             `json:"sum"`
	Min         float64             `json:"min"`
	Max         float64             `json:"max"`
	First       any                 `json:"first,omitempty"`
	Last        any                 `json:"last,omitempty"`
	DistinctSet map[uint64]struct{} `json:"distinct_set,omitempty"`
	Initialized bool                `json:"initialized"`
}

func (s *WindowedAggregateStage) Close() error {
	close(s.closeCh)
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
	if s.persistTicker != nil {
		s.persistTicker.Stop()
	}

	// Persist final state to Redis before closing
	if s.redisClient != nil {
		if err := s.persistStateToRedis(); err != nil {
			fmt.Printf("[windowed_aggregate] Warning: failed to persist final state: %v\n", err)
		}
		_ = s.redisClient.Close()
	}

	// Flush remaining windows
	s.FlushWindows()

	close(s.outputCh)
	return nil
}

// Helper functions

func toFloat64(v any) *float64 {
	var result float64
	switch val := v.(type) {
	case float64:
		result = val
	case float32:
		result = float64(val)
	case int:
		result = float64(val)
	case int32:
		result = float64(val)
	case int64:
		result = float64(val)
	case uint:
		result = float64(val)
	case uint32:
		result = float64(val)
	case uint64:
		result = float64(val)
	default:
		return nil
	}
	return &result
}

func hashValue(v any) uint64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%v", v)
	return h.Sum64()
}

// WindowInfo returns current window statistics
func (s *WindowedAggregateStage) WindowInfo() map[string]any {
	s.windowsMu.RLock()
	defer s.windowsMu.RUnlock()

	windows := make([]map[string]any, 0, len(s.windows))
	for key, ws := range s.windows {
		windows = append(windows, map[string]any{
			"key":           key,
			"group_key":     ws.groupKey,
			"start_time":    ws.startTime.Format(time.RFC3339),
			"end_time":      ws.endTime.Format(time.RFC3339),
			"last_activity": ws.lastActivity.Format(time.RFC3339),
		})
	}

	// Sort by key for consistent output
	sort.Slice(windows, func(i, j int) bool {
		return windows[i]["key"].(string) < windows[j]["key"].(string)
	})

	return map[string]any{
		"window_type":    string(s.windowType),
		"window_size":    s.windowSize.String(),
		"grace_period":   s.gracePeriod.String(),
		"emit_mode":      string(s.emitMode),
		"active_windows": len(s.windows),
		"watermark":      s.watermark.Format(time.RFC3339),
		"windows":        windows,
	}
}
