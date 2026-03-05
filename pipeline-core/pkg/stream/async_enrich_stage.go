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
	"text/template"
	"time"

	"github.com/go-redis/redis/v8"
)

// AsyncEnrichStage performs asynchronous enrichment with configurable concurrency
// Records are enriched in parallel using a worker pool
type AsyncEnrichStage struct {
	BaseStage
	sourceType   string // redis, http, sql
	sourceConfig map[string]any
	joinField    string
	targetField  string
	onMissing    string // skip, error, default
	defaultValue any
	timeout      time.Duration

	// Async settings
	workers     int
	queueSize   int
	ordered     bool // preserve record order
	maxInFlight int

	// Source clients
	redisClient *redis.Client
	httpClient  *http.Client
	sqlDB       *sql.DB
	keyTemplate *template.Template
	urlTemplate *template.Template
	sqlTemplate *template.Template

	// Cache
	cacheEnabled bool
	cacheTTL     time.Duration
	cacheMaxSize int
	cache        *lruCache

	// Worker pool
	inputCh   chan *asyncEnrichItem
	outputCh  chan *asyncEnrichResult
	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	// Order preservation
	orderMu      sync.Mutex
	nextSeq      int64
	pendingOrder map[int64]*asyncEnrichResult
}

type asyncEnrichItem struct {
	seq     int64
	record  *Record
	joinKey string
}

type asyncEnrichResult struct {
	seq    int64
	record *Record
	err    error
}

// NewAsyncEnrichStage creates a new async enrichment stage
func NewAsyncEnrichStage(name string, config map[string]any) (*AsyncEnrichStage, error) {
	s := &AsyncEnrichStage{
		BaseStage:    BaseStage{name: name, typ: "async_enrich", config: config},
		onMissing:    "skip",
		timeout:      5 * time.Second,
		workers:      4,
		queueSize:    1000,
		ordered:      false,
		maxInFlight:  100,
		cacheEnabled: true,
		cacheTTL:     5 * time.Minute,
		cacheMaxSize: 10000,
		closeCh:      make(chan struct{}),
		pendingOrder: make(map[int64]*asyncEnrichResult),
	}

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

	// Parse async settings
	if async, ok := config["async"].(map[string]any); ok {
		if w, ok := async["workers"].(int); ok {
			s.workers = w
		}
		if w, ok := async["workers"].(float64); ok {
			s.workers = int(w)
		}
		if qs, ok := async["queue_size"].(int); ok {
			s.queueSize = qs
		}
		if qs, ok := async["queue_size"].(float64); ok {
			s.queueSize = int(qs)
		}
		if ord, ok := async["ordered"].(bool); ok {
			s.ordered = ord
		}
		if mif, ok := async["max_in_flight"].(int); ok {
			s.maxInFlight = mif
		}
		if mif, ok := async["max_in_flight"].(float64); ok {
			s.maxInFlight = int(mif)
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

	// Initialize channels
	s.inputCh = make(chan *asyncEnrichItem, s.queueSize)
	s.outputCh = make(chan *asyncEnrichResult, s.queueSize)

	// Start workers
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s, nil
}

func (s *AsyncEnrichStage) initSource() error {
	switch s.sourceType {
	case "redis":
		return s.initRedis()
	case "http":
		return s.initHTTP()
	case "sql":
		return s.initSQL()
	default:
		return fmt.Errorf("unsupported async enrich source type: %s", s.sourceType)
	}
}

func (s *AsyncEnrichStage) initRedis() error {
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

	// Parse key template
	keyTmpl := ""
	if kt, ok := s.sourceConfig["key_template"].(string); ok {
		keyTmpl = kt
	}
	if keyTmpl != "" {
		tmpl, err := template.New("key").Parse(keyTmpl)
		if err != nil {
			return fmt.Errorf("invalid key_template: %w", err)
		}
		s.keyTemplate = tmpl
	}

	return nil
}

func (s *AsyncEnrichStage) initHTTP() error {
	s.httpClient = &http.Client{
		Timeout: s.timeout,
	}

	// Parse URL template
	urlTmpl := ""
	if ut, ok := s.sourceConfig["url_template"].(string); ok {
		urlTmpl = ut
	}
	if urlTmpl != "" {
		tmpl, err := template.New("url").Parse(urlTmpl)
		if err != nil {
			return fmt.Errorf("invalid url_template: %w", err)
		}
		s.urlTemplate = tmpl
	}

	return nil
}

func (s *AsyncEnrichStage) initSQL() error {
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

	// Parse query template
	queryTmpl := ""
	if qt, ok := s.sourceConfig["query_template"].(string); ok {
		queryTmpl = qt
	}
	if queryTmpl != "" {
		tmpl, err := template.New("query").Parse(queryTmpl)
		if err != nil {
			return fmt.Errorf("invalid query_template: %w", err)
		}
		s.sqlTemplate = tmpl
	}

	return nil
}

// worker processes enrichment requests
func (s *AsyncEnrichStage) worker() {
	defer s.wg.Done()

	for {
		select {
		case <-s.closeCh:
			return
		case item, ok := <-s.inputCh:
			if !ok {
				return
			}
			s.processItem(item)
		}
	}
}

func (s *AsyncEnrichStage) processItem(item *asyncEnrichItem) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	// Check cache first
	if s.cacheEnabled && s.cache != nil {
		if cached, ok := s.cache.Get(item.joinKey); ok {
			setNestedValueHelper(item.record.Data, s.targetField, cached)
			s.outputCh <- &asyncEnrichResult{
				seq:    item.seq,
				record: item.record,
			}
			return
		}
	}

	// Perform lookup
	result, err := s.lookup(ctx, item.record.Data, item.joinKey)
	if err != nil {
		if s.onMissing == "error" {
			s.outputCh <- &asyncEnrichResult{
				seq: item.seq,
				err: fmt.Errorf("lookup failed: %w", err),
			}
			return
		}
		// Use default value
		if s.defaultValue != nil {
			setNestedValueHelper(item.record.Data, s.targetField, s.defaultValue)
		}
		s.outputCh <- &asyncEnrichResult{
			seq:    item.seq,
			record: item.record,
		}
		return
	}

	if result == nil {
		// Not found
		switch s.onMissing {
		case "error":
			s.outputCh <- &asyncEnrichResult{
				seq: item.seq,
				err: fmt.Errorf("lookup key '%s' not found", item.joinKey),
			}
			return
		case "default":
			if s.defaultValue != nil {
				setNestedValueHelper(item.record.Data, s.targetField, s.defaultValue)
			}
		}
	} else {
		// Cache and set result
		if s.cacheEnabled && s.cache != nil {
			s.cache.Set(item.joinKey, result, s.cacheTTL)
		}
		setNestedValueHelper(item.record.Data, s.targetField, result)
	}

	s.outputCh <- &asyncEnrichResult{
		seq:    item.seq,
		record: item.record,
	}
}

func (s *AsyncEnrichStage) lookup(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	switch s.sourceType {
	case "redis":
		return s.lookupRedis(ctx, data, joinKey)
	case "http":
		return s.lookupHTTP(ctx, data, joinKey)
	case "sql":
		return s.lookupSQL(ctx, data, joinKey)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", s.sourceType)
	}
}

func (s *AsyncEnrichStage) lookupRedis(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	// Build key from template
	key := joinKey
	if s.keyTemplate != nil {
		var buf strings.Builder
		if err := s.keyTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute key template: %w", err)
		}
		key = buf.String()
	}

	// Get from Redis
	val, err := s.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get failed: %w", err)
	}

	// Try to parse as JSON
	var result any
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return val, nil
	}
	return result, nil
}

func (s *AsyncEnrichStage) lookupHTTP(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	// Build URL from template
	url := ""
	if s.urlTemplate != nil {
		var buf strings.Builder
		if err := s.urlTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute url template: %w", err)
		}
		url = buf.String()
	} else {
		if baseURL, ok := s.sourceConfig["url"].(string); ok {
			url = fmt.Sprintf("%s/%s", strings.TrimSuffix(baseURL, "/"), joinKey)
		}
	}

	if url == "" {
		return nil, fmt.Errorf("no URL configured for HTTP lookup")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return string(body), nil
	}

	// Extract specific field if configured
	if field, ok := s.sourceConfig["response_field"].(string); ok {
		if resultMap, ok := result.(map[string]any); ok {
			if val, exists := resultMap[field]; exists {
				return val, nil
			}
		}
	}

	return result, nil
}

func (s *AsyncEnrichStage) lookupSQL(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	query := ""
	if s.sqlTemplate != nil {
		var buf strings.Builder
		if err := s.sqlTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute query template: %w", err)
		}
		query = buf.String()
	} else {
		if q, ok := s.sourceConfig["query"].(string); ok {
			query = q
		}
	}

	if query == "" {
		return nil, fmt.Errorf("no query configured for SQL lookup")
	}

	rows, err := s.sqlDB.QueryContext(ctx, query, joinKey)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	if !rows.Next() {
		return nil, nil
	}

	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	result := make(map[string]any)
	for i, col := range columns {
		val := values[i]
		if b, ok := val.([]byte); ok {
			result[col] = string(b)
		} else {
			result[col] = val
		}
	}

	return result, nil
}

// Process queues a record for async enrichment
func (s *AsyncEnrichStage) Process(ctx context.Context, record *Record) (*Record, error) {
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

	// Check cache first (fast path)
	if s.cacheEnabled && s.cache != nil {
		if cached, ok := s.cache.Get(joinKey); ok {
			setNestedValueHelper(record.Data, s.targetField, cached)
			s.incrementOutput()
			return record, nil
		}
	}

	// Queue for async processing
	s.orderMu.Lock()
	seq := s.nextSeq
	s.nextSeq++
	s.orderMu.Unlock()

	item := &asyncEnrichItem{
		seq:     seq,
		record:  record,
		joinKey: joinKey,
	}

	select {
	case s.inputCh <- item:
	case <-ctx.Done():
		s.incrementError()
		return nil, ctx.Err()
	}

	// Wait for result (in ordered mode, handle order preservation)
	if s.ordered {
		return s.waitForOrderedResult(ctx, seq)
	}

	// Wait for result (unordered)
	select {
	case result := <-s.outputCh:
		if result.err != nil {
			s.incrementError()
			return nil, result.err
		}
		s.incrementOutput()
		return result.record, nil
	case <-ctx.Done():
		s.incrementError()
		return nil, ctx.Err()
	}
}

func (s *AsyncEnrichStage) waitForOrderedResult(ctx context.Context, expectedSeq int64) (*Record, error) {
	for {
		select {
		case result := <-s.outputCh:
			s.orderMu.Lock()
			if result.seq == expectedSeq {
				s.orderMu.Unlock()
				if result.err != nil {
					s.incrementError()
					return nil, result.err
				}
				s.incrementOutput()
				return result.record, nil
			}
			// Store for later
			s.pendingOrder[result.seq] = result
			s.orderMu.Unlock()
		case <-ctx.Done():
			s.incrementError()
			return nil, ctx.Err()
		}
	}
}

// ProcessAsync queues a record and returns immediately
// Results can be collected via GetResults()
func (s *AsyncEnrichStage) ProcessAsync(ctx context.Context, record *Record) error {
	s.incrementInput()

	joinValue, exists := getNestedValue(record.Data, s.joinField)
	if !exists || joinValue == nil {
		if s.onMissing == "error" {
			s.incrementError()
			return fmt.Errorf("join field '%s' not found in record", s.joinField)
		}
		// Still queue it for consistency
	}

	joinKey := ""
	if joinValue != nil {
		joinKey = fmt.Sprintf("%v", joinValue)
	}

	s.orderMu.Lock()
	seq := s.nextSeq
	s.nextSeq++
	s.orderMu.Unlock()

	item := &asyncEnrichItem{
		seq:     seq,
		record:  record,
		joinKey: joinKey,
	}

	select {
	case s.inputCh <- item:
		return nil
	case <-ctx.Done():
		s.incrementError()
		return ctx.Err()
	}
}

// GetResults collects available enriched records
func (s *AsyncEnrichStage) GetResults() []*Record {
	results := make([]*Record, 0)
	for {
		select {
		case result := <-s.outputCh:
			if result.err == nil && result.record != nil {
				results = append(results, result.record)
			}
		default:
			return results
		}
	}
}

// QueueLength returns current input queue length
func (s *AsyncEnrichStage) QueueLength() int {
	return len(s.inputCh)
}

// AsyncStats returns async processing statistics
func (s *AsyncEnrichStage) AsyncStats() map[string]any {
	input, output, errors := s.Stats()

	return map[string]any{
		"input_count":   input,
		"output_count":  output,
		"error_count":   errors,
		"workers":       s.workers,
		"queue_size":    s.queueSize,
		"queue_length":  len(s.inputCh),
		"output_length": len(s.outputCh),
		"ordered":       s.ordered,
		"cache_enabled": s.cacheEnabled,
	}
}

func (s *AsyncEnrichStage) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		close(s.inputCh)
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
