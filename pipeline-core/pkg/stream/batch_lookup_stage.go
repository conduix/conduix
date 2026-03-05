package stream

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// BatchLookupStage performs batch lookups for multiple records
// This is more efficient than individual lookups when processing many records
type BatchLookupStage struct {
	BaseStage
	sourceType   string // redis, sql, http
	sourceConfig map[string]any
	joinField    string
	targetField  string
	onMissing    string // skip, error, default
	defaultValue any
	timeout      time.Duration

	// Batch settings
	batchSize     int
	batchTimeout  time.Duration
	maxConcurrent int

	// Source clients
	redisClient *redis.Client
	sqlDB       *sql.DB
	httpClient  *http.Client

	// SQL batch query
	sqlBatchQuery string // e.g., "SELECT id, data FROM users WHERE id IN (?)"

	// Cache
	cacheEnabled bool
	cacheTTL     time.Duration
	cacheMaxSize int
	cache        *lruCache

	// Batch buffer
	bufferMu   sync.Mutex
	buffer     []*batchItem
	bufferCond *sync.Cond

	// Background processing
	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type batchItem struct {
	record   *Record
	joinKey  string
	resultCh chan batchResult
}

type batchResult struct {
	value any
	err   error
}

// NewBatchLookupStage creates a new batch lookup stage
func NewBatchLookupStage(name string, config map[string]any) (*BatchLookupStage, error) {
	s := &BatchLookupStage{
		BaseStage:     BaseStage{name: name, typ: "batch_lookup", config: config},
		onMissing:     "skip",
		timeout:       5 * time.Second,
		batchSize:     100,
		batchTimeout:  50 * time.Millisecond,
		maxConcurrent: 4,
		cacheEnabled:  true,
		cacheTTL:      5 * time.Minute,
		cacheMaxSize:  10000,
		buffer:        make([]*batchItem, 0),
		closeCh:       make(chan struct{}),
	}
	s.bufferCond = sync.NewCond(&s.bufferMu)

	// Parse source config
	if source, ok := config["source"].(map[string]any); ok {
		if t, ok := source["type"].(string); ok {
			s.sourceType = t
		}
		if cfg, ok := source["config"].(map[string]any); ok {
			s.sourceConfig = cfg
		}
	}

	// Parse join/target fields
	if jf, ok := config["join_field"].(string); ok {
		s.joinField = jf
	}
	if tf, ok := config["target_field"].(string); ok {
		s.targetField = tf
	}

	// Parse on_missing behavior
	if om, ok := config["on_missing"].(string); ok {
		s.onMissing = om
	}
	if dv, ok := config["default_value"]; ok {
		s.defaultValue = dv
	}

	// Parse timeout
	if t, ok := config["timeout"].(string); ok {
		if d, err := time.ParseDuration(t); err == nil {
			s.timeout = d
		}
	}

	// Parse batch settings
	if batch, ok := config["batch"].(map[string]any); ok {
		if size, ok := batch["size"].(int); ok {
			s.batchSize = size
		}
		if size, ok := batch["size"].(float64); ok {
			s.batchSize = int(size)
		}
		if timeout, ok := batch["timeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				s.batchTimeout = d
			}
		}
		if concurrent, ok := batch["max_concurrent"].(int); ok {
			s.maxConcurrent = concurrent
		}
		if concurrent, ok := batch["max_concurrent"].(float64); ok {
			s.maxConcurrent = int(concurrent)
		}
	}

	// Parse cache config
	if cache, ok := config["cache"].(map[string]any); ok {
		if enabled, ok := cache["enabled"].(bool); ok {
			s.cacheEnabled = enabled
		}
		if ttl, ok := cache["ttl"].(string); ok {
			if d, err := time.ParseDuration(ttl); err == nil {
				s.cacheTTL = d
			}
		}
		if maxSize, ok := cache["max_size"].(int); ok {
			s.cacheMaxSize = maxSize
		}
		if maxSize, ok := cache["max_size"].(float64); ok {
			s.cacheMaxSize = int(maxSize)
		}
	}

	// Initialize cache
	if s.cacheEnabled {
		s.cache = newLRUCache(s.cacheMaxSize)
	}

	// Initialize source
	if err := s.initSource(); err != nil {
		return nil, fmt.Errorf("failed to initialize source: %w", err)
	}

	// Start background batch processor
	s.wg.Add(1)
	go s.batchProcessor()

	return s, nil
}

func (s *BatchLookupStage) initSource() error {
	switch s.sourceType {
	case "redis":
		return s.initRedis()
	case "sql":
		return s.initSQL()
	case "http":
		return s.initHTTP()
	default:
		return fmt.Errorf("unsupported batch lookup source type: %s", s.sourceType)
	}
}

func (s *BatchLookupStage) initRedis() error {
	addr := "localhost:6379"
	if a, ok := s.sourceConfig["address"].(string); ok {
		addr = a
	}

	password := ""
	if p, ok := s.sourceConfig["password"].(string); ok {
		password = p
	}

	db := 0
	if d, ok := s.sourceConfig["db"].(int); ok {
		db = d
	}
	if d, ok := s.sourceConfig["db"].(float64); ok {
		db = int(d)
	}

	s.redisClient = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return nil
}

func (s *BatchLookupStage) initSQL() error {
	driver := ""
	if d, ok := s.sourceConfig["driver"].(string); ok {
		driver = d
	}

	dsn := ""
	if d, ok := s.sourceConfig["dsn"].(string); ok {
		dsn = d
	}

	if driver == "" || dsn == "" {
		return fmt.Errorf("sql source requires driver and dsn")
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	s.sqlDB = db

	// Parse batch query
	if q, ok := s.sourceConfig["batch_query"].(string); ok {
		s.sqlBatchQuery = q
	}

	return nil
}

func (s *BatchLookupStage) initHTTP() error {
	s.httpClient = &http.Client{
		Timeout: s.timeout,
	}
	return nil
}

func (s *BatchLookupStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Get join key value
	joinValue, exists := getNestedValue(record.Data, s.joinField)
	if !exists || joinValue == nil {
		if s.onMissing == "error" {
			s.incrementError()
			return nil, fmt.Errorf("join field '%s' not found in record", s.joinField)
		}
		s.incrementOutput()
		return record, nil
	}

	joinKey := fmt.Sprintf("%v", joinValue)

	// Check cache first
	if s.cacheEnabled && s.cache != nil {
		if cached, ok := s.cache.Get(joinKey); ok {
			setNestedValueHelper(record.Data, s.targetField, cached)
			s.incrementOutput()
			return record, nil
		}
	}

	// Add to batch buffer
	item := &batchItem{
		record:   record,
		joinKey:  joinKey,
		resultCh: make(chan batchResult, 1),
	}

	s.bufferMu.Lock()
	s.buffer = append(s.buffer, item)
	if len(s.buffer) >= s.batchSize {
		s.bufferCond.Signal()
	}
	s.bufferMu.Unlock()

	// Wait for result
	select {
	case <-ctx.Done():
		s.incrementError()
		return nil, ctx.Err()
	case result := <-item.resultCh:
		if result.err != nil {
			s.incrementError()
			if s.onMissing == "error" {
				return nil, result.err
			}
			if s.defaultValue != nil {
				setNestedValueHelper(record.Data, s.targetField, s.defaultValue)
			}
		} else if result.value != nil {
			setNestedValueHelper(record.Data, s.targetField, result.value)
		} else {
			// Not found
			switch s.onMissing {
			case "error":
				s.incrementError()
				return nil, fmt.Errorf("lookup key '%s' not found", joinKey)
			case "default":
				if s.defaultValue != nil {
					setNestedValueHelper(record.Data, s.targetField, s.defaultValue)
				}
			}
		}
	}

	s.incrementOutput()
	return record, nil
}

// batchProcessor processes batches in background
func (s *BatchLookupStage) batchProcessor() {
	defer s.wg.Done()

	timer := time.NewTimer(s.batchTimeout)
	defer timer.Stop()

	for {
		select {
		case <-s.closeCh:
			// Process remaining buffer
			s.processBatch()
			return
		case <-timer.C:
			s.processBatch()
			timer.Reset(s.batchTimeout)
		default:
			s.bufferMu.Lock()
			for len(s.buffer) < s.batchSize {
				s.bufferCond.Wait()
				select {
				case <-s.closeCh:
					s.bufferMu.Unlock()
					s.processBatch()
					return
				default:
				}
			}
			s.bufferMu.Unlock()
			s.processBatch()
			timer.Reset(s.batchTimeout)
		}
	}
}

func (s *BatchLookupStage) processBatch() {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return
	}

	// Take items from buffer
	items := s.buffer
	s.buffer = make([]*batchItem, 0)
	s.bufferMu.Unlock()

	// Collect unique keys
	keySet := make(map[string][]*batchItem)
	for _, item := range items {
		keySet[item.joinKey] = append(keySet[item.joinKey], item)
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}

	// Perform batch lookup
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	results, err := s.batchLookup(ctx, keys)
	if err != nil {
		// Send error to all items
		for _, itemList := range keySet {
			for _, item := range itemList {
				item.resultCh <- batchResult{err: err}
			}
		}
		return
	}

	// Send results to items
	for key, itemList := range keySet {
		value, exists := results[key]
		for _, item := range itemList {
			if exists {
				// Cache result
				if s.cacheEnabled && s.cache != nil {
					s.cache.Set(key, value, s.cacheTTL)
				}
				item.resultCh <- batchResult{value: value}
			} else {
				item.resultCh <- batchResult{}
			}
		}
	}
}

func (s *BatchLookupStage) batchLookup(ctx context.Context, keys []string) (map[string]any, error) {
	switch s.sourceType {
	case "redis":
		return s.batchLookupRedis(ctx, keys)
	case "sql":
		return s.batchLookupSQL(ctx, keys)
	case "http":
		return s.batchLookupHTTP(ctx, keys)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", s.sourceType)
	}
}

func (s *BatchLookupStage) batchLookupRedis(ctx context.Context, keys []string) (map[string]any, error) {
	// Use MGET for batch lookup
	keyPrefix := ""
	if p, ok := s.sourceConfig["key_prefix"].(string); ok {
		keyPrefix = p
	}

	redisKeys := make([]string, len(keys))
	for i, k := range keys {
		redisKeys[i] = keyPrefix + k
	}

	vals, err := s.redisClient.MGet(ctx, redisKeys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget failed: %w", err)
	}

	results := make(map[string]any)
	for i, val := range vals {
		if val == nil {
			continue
		}

		valStr, ok := val.(string)
		if !ok {
			continue
		}

		// Try to parse as JSON
		var parsed any
		if err := json.Unmarshal([]byte(valStr), &parsed); err != nil {
			results[keys[i]] = valStr
		} else {
			results[keys[i]] = parsed
		}
	}

	return results, nil
}

func (s *BatchLookupStage) batchLookupSQL(ctx context.Context, keys []string) (map[string]any, error) {
	if s.sqlBatchQuery == "" {
		return nil, fmt.Errorf("batch_query not configured for SQL batch lookup")
	}

	// Build placeholders
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, k := range keys {
		placeholders[i] = "?"
		args[i] = k
	}

	// Replace ? with actual placeholders
	query := strings.Replace(s.sqlBatchQuery, "(?)", "("+strings.Join(placeholders, ",")+")", 1)

	rows, err := s.sqlDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sql query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Find key column (first column by default)
	keyColumn := "id"
	if kc, ok := s.sourceConfig["key_column"].(string); ok {
		keyColumn = kc
	}

	keyColumnIdx := 0
	for i, col := range columns {
		if col == keyColumn {
			keyColumnIdx = i
			break
		}
	}

	results := make(map[string]any)

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		// Get key value
		keyVal := fmt.Sprintf("%v", values[keyColumnIdx])

		// Build result map
		rowResult := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				rowResult[col] = string(b)
			} else {
				rowResult[col] = val
			}
		}

		results[keyVal] = rowResult
	}

	return results, nil
}

func (s *BatchLookupStage) batchLookupHTTP(ctx context.Context, keys []string) (map[string]any, error) {
	// HTTP batch lookup using POST with array of keys
	batchURL := ""
	if u, ok := s.sourceConfig["batch_url"].(string); ok {
		batchURL = u
	}
	if batchURL == "" {
		return nil, fmt.Errorf("batch_url not configured for HTTP batch lookup")
	}

	// Build request body
	reqBody, err := json.Marshal(map[string]any{
		"keys": keys,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", batchURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	if headers, ok := s.sourceConfig["headers"].(map[string]any); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var respData map[string]any
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract results field
	resultsField := "results"
	if rf, ok := s.sourceConfig["results_field"].(string); ok {
		resultsField = rf
	}

	if results, ok := respData[resultsField].(map[string]any); ok {
		return results, nil
	}

	// Try to use response directly
	return respData, nil
}

func (s *BatchLookupStage) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.bufferCond.Signal()
	})

	s.wg.Wait()

	var errs []error

	if s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("redis close: %w", err))
		}
	}

	if s.sqlDB != nil {
		if err := s.sqlDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("sql close: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// BatchStats returns batch processing statistics
func (s *BatchLookupStage) BatchStats() map[string]any {
	s.bufferMu.Lock()
	bufferLen := len(s.buffer)
	s.bufferMu.Unlock()

	input, output, errors := s.Stats()

	return map[string]any{
		"input_count":        input,
		"output_count":       output,
		"error_count":        errors,
		"batch_size":         s.batchSize,
		"batch_timeout":      s.batchTimeout.String(),
		"current_buffer_len": bufferLen,
		"cache_enabled":      s.cacheEnabled,
	}
}

// setNestedValueHelper sets a nested field value using dot notation
func setNestedValueHelper(data map[string]any, field string, value any) {
	parts := strings.Split(field, ".")

	if len(parts) == 1 {
		data[field] = value
		return
	}

	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if next, ok := current[part].(map[string]any); ok {
			current = next
		} else {
			// Create nested map
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
		}
	}

	current[parts[len(parts)-1]] = value
}
