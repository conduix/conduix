package stream

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// JoinType defines the type of join operation
type JoinType string

const (
	JoinInner JoinType = "inner" // Only matching records from both streams
	JoinLeft  JoinType = "left"  // All from left, matching from right (null if no match)
	JoinRight JoinType = "right" // All from right, matching from left (null if no match)
	JoinOuter JoinType = "outer" // All from both (null where no match)
)

// JoinWindow defines the time window for join
type JoinWindow struct {
	Before time.Duration // Look back from current record
	After  time.Duration // Look ahead from current record
}

// StreamJoinStage joins two streams based on key and time window
type StreamJoinStage struct {
	BaseStage
	joinType       JoinType
	leftKey        string // Join key field in left stream
	rightKey       string // Join key field in right stream
	leftPrefix     string // Prefix for left stream fields in output
	rightPrefix    string // Prefix for right stream fields in output
	window         JoinWindow
	leftStream     string   // Name/ID of left stream
	rightStream    string   // Name/ID of right stream
	outputFields   []string // Fields to include in output (empty = all)
	timestampField string   // Field to use for event time

	// State buffers for windowed join
	leftBuffer  *joinBuffer
	rightBuffer *joinBuffer
	bufferMu    sync.RWMutex

	// Output channel for joined results
	outputCh chan *Record

	// Cleanup
	cleanupTicker *time.Ticker
	closeCh       chan struct{}

	// Watermark processing
	leftWatermark     time.Time     // Left stream watermark
	rightWatermark    time.Time     // Right stream watermark
	allowedLateness   time.Duration // Grace period for late data
	watermarkInterval time.Duration // Watermark update interval
	emitOnWatermark   bool          // Emit unmatched records when watermark advances
	lateDataHandler   LateDataHandler
	watermarkMu       sync.RWMutex
}

// LateDataHandler defines how to handle late data
type LateDataHandler string

const (
	LateDataDrop   LateDataHandler = "drop"   // Drop late data silently
	LateDataLog    LateDataHandler = "log"    // Log warning and drop
	LateDataEmit   LateDataHandler = "emit"   // Emit with _late flag
	LateDataBuffer LateDataHandler = "buffer" // Buffer for later processing (default)
)

// joinBuffer holds records within a time window
type joinBuffer struct {
	records []*timedRecord
	index   map[string][]*timedRecord // key (stringified) -> records with that key
	maxAge  time.Duration
	mu      sync.RWMutex
}

// timedRecord wraps a record with timestamp
type timedRecord struct {
	record    *Record
	timestamp time.Time
	key       any
}

// NewStreamJoinStage creates a new stream join stage
func NewStreamJoinStage(name string, config map[string]any) (*StreamJoinStage, error) {
	s := &StreamJoinStage{
		BaseStage:         BaseStage{name: name, typ: "stream_join"},
		joinType:          JoinInner,
		outputCh:          make(chan *Record, 100),
		closeCh:           make(chan struct{}),
		allowedLateness:   10 * time.Second, // Default 10 seconds grace period
		watermarkInterval: 5 * time.Second,  // Default 5 seconds
		emitOnWatermark:   false,            // Default: don't emit on watermark
		lateDataHandler:   LateDataBuffer,   // Default: buffer late data
	}

	// Parse join type
	if jt, ok := config["join_type"].(string); ok {
		switch jt {
		case "inner":
			s.joinType = JoinInner
		case "left":
			s.joinType = JoinLeft
		case "right":
			s.joinType = JoinRight
		case "outer":
			s.joinType = JoinOuter
		default:
			return nil, fmt.Errorf("invalid join_type: %s", jt)
		}
	}

	// Parse keys
	if lk, ok := config["left_key"].(string); ok {
		s.leftKey = lk
	} else {
		return nil, fmt.Errorf("left_key is required")
	}

	if rk, ok := config["right_key"].(string); ok {
		s.rightKey = rk
	} else {
		s.rightKey = s.leftKey // Default to same key
	}

	// Parse stream names
	if ls, ok := config["left_stream"].(string); ok {
		s.leftStream = ls
	}
	if rs, ok := config["right_stream"].(string); ok {
		s.rightStream = rs
	}

	// Parse prefixes
	s.leftPrefix = "left_"
	if lp, ok := config["left_prefix"].(string); ok {
		s.leftPrefix = lp
	}
	s.rightPrefix = "right_"
	if rp, ok := config["right_prefix"].(string); ok {
		s.rightPrefix = rp
	}

	// Parse window
	windowBefore := 5 * time.Second
	windowAfter := 5 * time.Second

	if windowConfig, ok := config["window"].(map[string]any); ok {
		if before, ok := windowConfig["before"].(string); ok {
			if d, err := time.ParseDuration(before); err == nil {
				windowBefore = d
			}
		}
		if after, ok := windowConfig["after"].(string); ok {
			if d, err := time.ParseDuration(after); err == nil {
				windowAfter = d
			}
		}
	}
	s.window = JoinWindow{Before: windowBefore, After: windowAfter}

	// Parse output fields
	if fields, ok := config["output_fields"].([]any); ok {
		for _, f := range fields {
			if fs, ok := f.(string); ok {
				s.outputFields = append(s.outputFields, fs)
			}
		}
	}

	// Parse timestamp field
	s.timestampField = "_timestamp"
	if ts, ok := config["timestamp_field"].(string); ok {
		s.timestampField = ts
	}

	// Parse watermark config
	if wmConfig, ok := config["watermark"].(map[string]any); ok {
		if lateness, ok := wmConfig["allowed_lateness"].(string); ok {
			if d, err := time.ParseDuration(lateness); err == nil {
				s.allowedLateness = d
			}
		}
		if interval, ok := wmConfig["interval"].(string); ok {
			if d, err := time.ParseDuration(interval); err == nil {
				s.watermarkInterval = d
			}
		}
		if emit, ok := wmConfig["emit_on_advance"].(bool); ok {
			s.emitOnWatermark = emit
		}
		if handler, ok := wmConfig["late_data"].(string); ok {
			switch handler {
			case "drop":
				s.lateDataHandler = LateDataDrop
			case "log":
				s.lateDataHandler = LateDataLog
			case "emit":
				s.lateDataHandler = LateDataEmit
			case "buffer":
				s.lateDataHandler = LateDataBuffer
			}
		}
	}

	// Initialize buffers
	maxAge := windowBefore + windowAfter + time.Minute // Extra buffer for late arrivals
	s.leftBuffer = newJoinBuffer(maxAge)
	s.rightBuffer = newJoinBuffer(maxAge)

	// Start cleanup routine
	s.cleanupTicker = time.NewTicker(time.Minute)
	go s.cleanupRoutine()

	return s, nil
}

func newJoinBuffer(maxAge time.Duration) *joinBuffer {
	return &joinBuffer{
		records: make([]*timedRecord, 0),
		index:   make(map[string][]*timedRecord),
		maxAge:  maxAge,
	}
}

// stringifyKey converts any key to a string for consistent map lookup
func stringifyKey(key any) string {
	return fmt.Sprintf("%v", key)
}

func (b *joinBuffer) Add(record *Record, key any, timestamp time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyStr := stringifyKey(key)
	tr := &timedRecord{
		record:    record,
		timestamp: timestamp,
		key:       key,
	}

	b.records = append(b.records, tr)
	b.index[keyStr] = append(b.index[keyStr], tr)
}

func (b *joinBuffer) FindMatches(key any, windowStart, windowEnd time.Time) []*Record {
	b.mu.RLock()
	defer b.mu.RUnlock()

	keyStr := stringifyKey(key)
	records, exists := b.index[keyStr]
	if !exists {
		return nil
	}

	var matches []*Record
	for _, tr := range records {
		if !tr.timestamp.Before(windowStart) && !tr.timestamp.After(windowEnd) {
			matches = append(matches, tr.record)
		}
	}

	return matches
}

func (b *joinBuffer) Cleanup(cutoff time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Filter records
	newRecords := make([]*timedRecord, 0, len(b.records)/2)
	for _, tr := range b.records {
		if tr.timestamp.After(cutoff) {
			newRecords = append(newRecords, tr)
		}
	}
	b.records = newRecords

	// Rebuild index
	b.index = make(map[string][]*timedRecord)
	for _, tr := range b.records {
		keyStr := stringifyKey(tr.key)
		b.index[keyStr] = append(b.index[keyStr], tr)
	}
}

func (s *StreamJoinStage) cleanupRoutine() {
	for {
		select {
		case <-s.closeCh:
			return
		case <-s.cleanupTicker.C:
			cutoff := time.Now().Add(-s.window.Before - s.window.After - time.Minute)
			s.leftBuffer.Cleanup(cutoff)
			s.rightBuffer.Cleanup(cutoff)
		}
	}
}

// Process processes a record from either stream
func (s *StreamJoinStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Determine which stream this record belongs to
	streamName, _ := record.Data["_stream"].(string)
	// Check for explicit markers from ProcessLeft/ProcessRight
	isLeft := streamName == "_left_" || streamName == s.leftStream || (streamName == "" && s.rightStream != "") // Default to left stream
	isRight := streamName == "_right_" || streamName == s.rightStream
	if isRight {
		isLeft = false
	}

	// Get timestamp
	timestamp := time.Now()
	if ts, ok := record.Data[s.timestampField].(int64); ok {
		timestamp = time.UnixMilli(ts)
	} else if ts, ok := record.Data[s.timestampField].(time.Time); ok {
		timestamp = ts
	}

	// Check for late data based on watermark
	if isLate := s.checkAndHandleLateData(record, timestamp, isLeft); isLate {
		if s.lateDataHandler == LateDataDrop || s.lateDataHandler == LateDataLog {
			return nil, nil // Drop late data
		}
		// For LateDataEmit, continue processing but mark as late
		if s.lateDataHandler == LateDataEmit {
			record.Data["_late"] = true
		}
	}

	// Update watermark
	s.updateWatermark(timestamp, isLeft)

	// Get join key
	var key any
	if isLeft {
		key = getNestedValueOrDefault(record.Data, s.leftKey, nil)
	} else {
		key = getNestedValueOrDefault(record.Data, s.rightKey, nil)
	}

	if key == nil {
		// No join key, pass through for left/outer joins
		if isLeft && (s.joinType == JoinLeft || s.joinType == JoinOuter) {
			output := s.createOutputRecord(record, nil, timestamp)
			s.incrementOutput()
			return output, nil
		}
		if !isLeft && (s.joinType == JoinRight || s.joinType == JoinOuter) {
			output := s.createOutputRecord(nil, record, timestamp)
			s.incrementOutput()
			return output, nil
		}
		return nil, nil // Skip record for inner join
	}

	// Calculate window
	windowStart := timestamp.Add(-s.window.Before)
	windowEnd := timestamp.Add(s.window.After)

	// Add to buffer
	if isLeft {
		s.leftBuffer.Add(record, key, timestamp)
	} else {
		s.rightBuffer.Add(record, key, timestamp)
	}

	// Find matches from the other stream
	var matches []*Record
	if isLeft {
		matches = s.rightBuffer.FindMatches(key, windowStart, windowEnd)
	} else {
		matches = s.leftBuffer.FindMatches(key, windowStart, windowEnd)
	}

	// Handle join based on type
	if len(matches) > 0 {
		// Found matches - emit first joined record
		match := matches[0]
		var output *Record
		if isLeft {
			output = s.createOutputRecord(record, match, timestamp)
		} else {
			output = s.createOutputRecord(match, record, timestamp)
		}
		s.incrementOutput()
		return output, nil
	}

	// No matches
	switch s.joinType {
	case JoinLeft:
		if isLeft {
			output := s.createOutputRecord(record, nil, timestamp)
			s.incrementOutput()
			return output, nil
		}
	case JoinRight:
		if !isLeft {
			output := s.createOutputRecord(nil, record, timestamp)
			s.incrementOutput()
			return output, nil
		}
	case JoinOuter:
		var output *Record
		if isLeft {
			output = s.createOutputRecord(record, nil, timestamp)
		} else {
			output = s.createOutputRecord(nil, record, timestamp)
		}
		s.incrementOutput()
		return output, nil
	}

	// Inner join - record buffered, will be joined when matching record arrives
	return nil, nil
}

func (s *StreamJoinStage) createOutputRecord(left, right *Record, timestamp time.Time) *Record {
	data := make(map[string]any)

	// Add left stream fields
	if left != nil {
		if s.leftPrefix == "" {
			for k, v := range left.Data {
				if !isInternalField(k) {
					data[k] = v
				}
			}
		} else {
			for k, v := range left.Data {
				if !isInternalField(k) {
					data[s.leftPrefix+k] = v
				}
			}
		}
	}

	// Add right stream fields
	if right != nil {
		if s.rightPrefix == "" {
			for k, v := range right.Data {
				if !isInternalField(k) {
					data[k] = v
				}
			}
		} else {
			for k, v := range right.Data {
				if !isInternalField(k) {
					data[s.rightPrefix+k] = v
				}
			}
		}
	}

	// Add join metadata
	data["_joined_at"] = timestamp.UnixMilli()
	data["_join_type"] = string(s.joinType)

	// Filter output fields if specified
	if len(s.outputFields) > 0 {
		filtered := make(map[string]any)
		for _, f := range s.outputFields {
			if v, ok := data[f]; ok {
				filtered[f] = v
			}
		}
		// Always include metadata
		filtered["_joined_at"] = data["_joined_at"]
		filtered["_join_type"] = data["_join_type"]
		data = filtered
	}

	return &Record{Data: data}
}

func isInternalField(key string) bool {
	return len(key) > 0 && key[0] == '_'
}

func getNestedValueOrDefault(data map[string]any, path string, defaultValue any) any {
	v, ok := getNestedValue(data, path)
	if !ok {
		return defaultValue
	}
	return v
}

// ProcessLeft processes a record from the left stream explicitly
func (s *StreamJoinStage) ProcessLeft(ctx context.Context, record *Record) (*Record, error) {
	record.Data["_stream"] = "_left_"
	return s.Process(ctx, record)
}

// ProcessRight processes a record from the right stream explicitly
func (s *StreamJoinStage) ProcessRight(ctx context.Context, record *Record) (*Record, error) {
	record.Data["_stream"] = "_right_"
	return s.Process(ctx, record)
}

// FlushPending flushes any pending records that haven't been joined
func (s *StreamJoinStage) FlushPending() []*Record {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	var results []*Record

	// For outer/left joins, emit unmatched left records
	if s.joinType == JoinLeft || s.joinType == JoinOuter {
		for _, tr := range s.leftBuffer.records {
			// Check if this record has been matched
			key := tr.key
			windowStart := tr.timestamp.Add(-s.window.Before)
			windowEnd := tr.timestamp.Add(s.window.After)

			matches := s.rightBuffer.FindMatches(key, windowStart, windowEnd)
			if len(matches) == 0 {
				results = append(results, s.createOutputRecord(tr.record, nil, tr.timestamp))
			}
		}
	}

	// For outer/right joins, emit unmatched right records
	if s.joinType == JoinRight || s.joinType == JoinOuter {
		for _, tr := range s.rightBuffer.records {
			key := tr.key
			windowStart := tr.timestamp.Add(-s.window.Before)
			windowEnd := tr.timestamp.Add(s.window.After)

			matches := s.leftBuffer.FindMatches(key, windowStart, windowEnd)
			if len(matches) == 0 {
				results = append(results, s.createOutputRecord(nil, tr.record, tr.timestamp))
			}
		}
	}

	return results
}

// JoinInfo returns information about the join configuration
func (s *StreamJoinStage) JoinInfo() map[string]any {
	s.bufferMu.RLock()
	s.watermarkMu.RLock()
	defer s.bufferMu.RUnlock()
	defer s.watermarkMu.RUnlock()

	info := map[string]any{
		"join_type":         string(s.joinType),
		"left_key":          s.leftKey,
		"right_key":         s.rightKey,
		"window_before":     s.window.Before.String(),
		"window_after":      s.window.After.String(),
		"left_buffer_size":  len(s.leftBuffer.records),
		"right_buffer_size": len(s.rightBuffer.records),
		"allowed_lateness":  s.allowedLateness.String(),
		"late_data_handler": string(s.lateDataHandler),
		"emit_on_watermark": s.emitOnWatermark,
	}

	// Add watermark info
	if !s.leftWatermark.IsZero() {
		info["left_watermark"] = s.leftWatermark.Format(time.RFC3339)
	}
	if !s.rightWatermark.IsZero() {
		info["right_watermark"] = s.rightWatermark.Format(time.RFC3339)
	}

	return info
}

// checkAndHandleLateData checks if a record is late based on watermark
func (s *StreamJoinStage) checkAndHandleLateData(record *Record, timestamp time.Time, isLeft bool) bool {
	s.watermarkMu.RLock()
	defer s.watermarkMu.RUnlock()

	var watermark time.Time
	if isLeft {
		watermark = s.leftWatermark
	} else {
		watermark = s.rightWatermark
	}

	// If watermark is not set yet, no data is late
	if watermark.IsZero() {
		return false
	}

	// Check if record is late (timestamp + allowed lateness < watermark)
	latenessBoundary := watermark.Add(-s.allowedLateness)
	isLate := timestamp.Before(latenessBoundary)

	if isLate && s.lateDataHandler == LateDataLog {
		streamName := "left"
		if !isLeft {
			streamName = "right"
		}
		fmt.Printf("[stream_join] Late data detected from %s stream: timestamp=%s, watermark=%s, allowed_lateness=%s\n",
			streamName, timestamp.Format(time.RFC3339), watermark.Format(time.RFC3339), s.allowedLateness)
	}

	return isLate
}

// updateWatermark updates the watermark for a stream
func (s *StreamJoinStage) updateWatermark(timestamp time.Time, isLeft bool) {
	s.watermarkMu.Lock()
	defer s.watermarkMu.Unlock()

	var currentWatermark *time.Time
	if isLeft {
		currentWatermark = &s.leftWatermark
	} else {
		currentWatermark = &s.rightWatermark
	}

	// Update watermark if timestamp is newer
	// Use timestamp minus allowed lateness as watermark (conservative)
	newWatermark := timestamp.Add(-s.allowedLateness)
	if currentWatermark.IsZero() || newWatermark.After(*currentWatermark) {
		oldWatermark := *currentWatermark
		*currentWatermark = newWatermark

		// Emit unmatched records if configured
		if s.emitOnWatermark && !oldWatermark.IsZero() {
			go s.emitOnWatermarkAdvance(oldWatermark, newWatermark, isLeft)
		}
	}
}

// emitOnWatermarkAdvance emits unmatched records when watermark advances
func (s *StreamJoinStage) emitOnWatermarkAdvance(oldWatermark, newWatermark time.Time, isLeftAdvance bool) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	// Find records that are now definitely unmatched (their window has closed)
	// based on the advanced watermark

	if isLeftAdvance {
		// Check left buffer for records that can't be matched anymore
		for _, tr := range s.leftBuffer.records {
			windowEnd := tr.timestamp.Add(s.window.After)
			if windowEnd.Before(newWatermark) && !windowEnd.Before(oldWatermark) {
				// This record's window just closed
				matches := s.rightBuffer.FindMatches(tr.key, tr.timestamp.Add(-s.window.Before), windowEnd)
				if len(matches) == 0 {
					// No match found, emit for left/outer joins
					if s.joinType == JoinLeft || s.joinType == JoinOuter {
						output := s.createOutputRecord(tr.record, nil, tr.timestamp)
						output.Data["_watermark_emit"] = true
						select {
						case s.outputCh <- output:
						default:
							// Channel full
						}
					}
				}
			}
		}
	} else {
		// Check right buffer
		for _, tr := range s.rightBuffer.records {
			windowEnd := tr.timestamp.Add(s.window.After)
			if windowEnd.Before(newWatermark) && !windowEnd.Before(oldWatermark) {
				matches := s.leftBuffer.FindMatches(tr.key, tr.timestamp.Add(-s.window.Before), windowEnd)
				if len(matches) == 0 {
					if s.joinType == JoinRight || s.joinType == JoinOuter {
						output := s.createOutputRecord(nil, tr.record, tr.timestamp)
						output.Data["_watermark_emit"] = true
						select {
						case s.outputCh <- output:
						default:
						}
					}
				}
			}
		}
	}
}

// GetWatermarks returns current watermarks for both streams
func (s *StreamJoinStage) GetWatermarks() (left, right time.Time) {
	s.watermarkMu.RLock()
	defer s.watermarkMu.RUnlock()
	return s.leftWatermark, s.rightWatermark
}

// GetMinWatermark returns the minimum of both watermarks
func (s *StreamJoinStage) GetMinWatermark() time.Time {
	s.watermarkMu.RLock()
	defer s.watermarkMu.RUnlock()

	if s.leftWatermark.IsZero() {
		return s.rightWatermark
	}
	if s.rightWatermark.IsZero() {
		return s.leftWatermark
	}
	if s.leftWatermark.Before(s.rightWatermark) {
		return s.leftWatermark
	}
	return s.rightWatermark
}

func (s *StreamJoinStage) Close() error {
	close(s.closeCh)
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}

	// Log watermark info
	s.watermarkMu.RLock()
	fmt.Printf("[stream_join] Final watermarks - Left: %s, Right: %s\n",
		s.leftWatermark.Format(time.RFC3339), s.rightWatermark.Format(time.RFC3339))
	s.watermarkMu.RUnlock()

	input, output, _ := s.Stats()
	fmt.Printf("[stream_join] Closed. Input: %d, Output: %d\n", input, output)
	return nil
}

// NewStreamJoinStageFromConfig creates a stream join stage from config
func NewStreamJoinStageFromConfig(name string, config map[string]any) (Stage, error) {
	return NewStreamJoinStage(name, config)
}
