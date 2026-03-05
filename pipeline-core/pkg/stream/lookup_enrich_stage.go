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

// LookupEnrichStage enriches records by looking up data from external sources
type LookupEnrichStage struct {
	BaseStage
	sourceType   string // redis, http, sql
	sourceConfig map[string]any
	joinField    string
	targetField  string
	onMissing    string // skip, error, default
	defaultValue any
	timeout      time.Duration

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
}

// lruCache is a simple LRU cache implementation
type lruCache struct {
	capacity int
	items    map[string]*cacheItem
	order    []string
	mu       sync.RWMutex
}

type cacheItem struct {
	value     any
	expiresAt time.Time
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*cacheItem),
		order:    make([]string, 0, capacity),
	}
}

func (c *lruCache) Get(key string) (any, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(item.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}

	return item.value, true
}

func (c *lruCache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove oldest if at capacity
	if len(c.items) >= c.capacity {
		if len(c.order) > 0 {
			oldest := c.order[0]
			delete(c.items, oldest)
			c.order = c.order[1:]
		}
	}

	c.items[key] = &cacheItem{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.order = append(c.order, key)
}

// NewLookupEnrichStage creates a new lookup enrich stage
func NewLookupEnrichStage(name string, config map[string]any) (*LookupEnrichStage, error) {
	s := &LookupEnrichStage{
		BaseStage:    BaseStage{name: name, typ: "lookup_enrich", config: config},
		onMissing:    "skip",
		timeout:      5 * time.Second,
		cacheEnabled: false,
		cacheTTL:     5 * time.Minute,
		cacheMaxSize: 10000,
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

	return s, nil
}

func (s *LookupEnrichStage) initSource() error {
	switch s.sourceType {
	case "redis":
		return s.initRedis()
	case "http":
		return s.initHTTP()
	case "sql":
		return s.initSQL()
	default:
		return fmt.Errorf("unsupported lookup source type: %s", s.sourceType)
	}
}

func (s *LookupEnrichStage) initRedis() error {
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

func (s *LookupEnrichStage) initHTTP() error {
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

func (s *LookupEnrichStage) initSQL() error {
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

func (s *LookupEnrichStage) Process(ctx context.Context, record *Record) (*Record, error) {
	s.incrementInput()

	// Get join key value
	joinValue := s.getNestedValue(record.Data, s.joinField)
	if joinValue == nil {
		if s.onMissing == "error" {
			s.incrementError()
			return nil, fmt.Errorf("join field '%s' not found in record", s.joinField)
		}
		s.incrementOutput()
		return record, nil
	}

	joinKey := fmt.Sprintf("%v", joinValue)

	// Check cache first
	if s.cacheEnabled {
		if cached, ok := s.cache.Get(joinKey); ok {
			s.setNestedValue(record.Data, s.targetField, cached)
			s.incrementOutput()
			return record, nil
		}
	}

	// Lookup from source
	lookupCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	result, err := s.lookup(lookupCtx, record.Data, joinKey)
	if err != nil {
		s.incrementError()
		if s.onMissing == "error" {
			return nil, fmt.Errorf("lookup failed: %w", err)
		}
		// Use default value
		if s.defaultValue != nil {
			s.setNestedValue(record.Data, s.targetField, s.defaultValue)
		}
		s.incrementOutput()
		return record, nil
	}

	if result == nil {
		// Not found
		switch s.onMissing {
		case "error":
			s.incrementError()
			return nil, fmt.Errorf("lookup key '%s' not found", joinKey)
		case "default":
			if s.defaultValue != nil {
				s.setNestedValue(record.Data, s.targetField, s.defaultValue)
			}
		case "skip":
			// Do nothing
		}
	} else {
		// Cache result
		if s.cacheEnabled {
			s.cache.Set(joinKey, result, s.cacheTTL)
		}
		s.setNestedValue(record.Data, s.targetField, result)
	}

	s.incrementOutput()
	return record, nil
}

func (s *LookupEnrichStage) lookup(ctx context.Context, data map[string]any, joinKey string) (any, error) {
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

func (s *LookupEnrichStage) lookupRedis(ctx context.Context, data map[string]any, joinKey string) (any, error) {
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
		// Return as string
		return val, nil
	}
	return result, nil
}

func (s *LookupEnrichStage) lookupHTTP(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	// Build URL from template
	url := ""
	if s.urlTemplate != nil {
		var buf strings.Builder
		if err := s.urlTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute url template: %w", err)
		}
		url = buf.String()
	} else {
		// Use base URL with join key
		if baseURL, ok := s.sourceConfig["url"].(string); ok {
			url = fmt.Sprintf("%s/%s", strings.TrimSuffix(baseURL, "/"), joinKey)
		}
	}

	if url == "" {
		return nil, fmt.Errorf("no URL configured for HTTP lookup")
	}

	// Make request
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

	// Parse JSON response
	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		// Return as string
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

func (s *LookupEnrichStage) lookupSQL(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	// Build query from template
	query := ""
	if s.sqlTemplate != nil {
		var buf strings.Builder
		if err := s.sqlTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute query template: %w", err)
		}
		query = buf.String()
	} else {
		// Use simple query
		if q, ok := s.sourceConfig["query"].(string); ok {
			query = q
		}
	}

	if query == "" {
		return nil, fmt.Errorf("no query configured for SQL lookup")
	}

	// Execute query
	rows, err := s.sqlDB.QueryContext(ctx, query, joinKey)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Read first row
	if !rows.Next() {
		return nil, nil
	}

	// Create result map
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
		// Convert []byte to string
		if b, ok := val.([]byte); ok {
			result[col] = string(b)
		} else {
			result[col] = val
		}
	}

	return result, nil
}

// getNestedValue gets a nested field value using dot notation (e.g., "user.id")
func (s *LookupEnrichStage) getNestedValue(data map[string]any, field string) any {
	parts := strings.Split(field, ".")
	current := any(data)

	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return nil
		}
	}

	return current
}

// setNestedValue sets a nested field value using dot notation
func (s *LookupEnrichStage) setNestedValue(data map[string]any, field string, value any) {
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

func (s *LookupEnrichStage) Close() error {
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
