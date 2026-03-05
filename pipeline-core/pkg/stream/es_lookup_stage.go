package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"
)

// ElasticsearchLookupStage performs lookups against Elasticsearch
type ElasticsearchLookupStage struct {
	BaseStage
	endpoints    []string
	index        string
	joinField    string
	targetField  string
	onMissing    string // skip, error, default
	defaultValue any
	timeout      time.Duration

	// Query configuration
	queryTemplate *template.Template
	queryField    string   // field to match in ES (defaults to _id)
	sourceFields  []string // fields to retrieve from ES (_source filtering)

	// HTTP client
	httpClient *http.Client

	// Authentication
	username string
	password string
	apiKey   string

	// Cache
	cacheEnabled bool
	cacheTTL     time.Duration
	cacheMaxSize int
	cache        *lruCache

	// Endpoint rotation
	endpointIdx int
	endpointMu  sync.Mutex
}

// NewElasticsearchLookupStage creates a new Elasticsearch lookup stage
func NewElasticsearchLookupStage(name string, config map[string]any) (*ElasticsearchLookupStage, error) {
	s := &ElasticsearchLookupStage{
		BaseStage:    BaseStage{name: name, typ: "es_lookup", config: config},
		onMissing:    "skip",
		timeout:      5 * time.Second,
		queryField:   "_id",
		cacheEnabled: true,
		cacheTTL:     5 * time.Minute,
		cacheMaxSize: 10000,
	}

	// Parse endpoints
	if eps, ok := config["endpoints"].([]any); ok {
		for _, ep := range eps {
			if epStr, ok := ep.(string); ok {
				s.endpoints = append(s.endpoints, strings.TrimSuffix(epStr, "/"))
			}
		}
	}
	if ep, ok := config["endpoint"].(string); ok {
		s.endpoints = append(s.endpoints, strings.TrimSuffix(ep, "/"))
	}
	if len(s.endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint is required")
	}

	// Parse index
	if idx, ok := config["index"].(string); ok {
		s.index = idx
	}
	if s.index == "" {
		return nil, fmt.Errorf("index is required")
	}

	// Parse join/target fields
	if jf, ok := config["join_field"].(string); ok {
		s.joinField = jf
	}
	if s.joinField == "" {
		return nil, fmt.Errorf("join_field is required")
	}
	if tf, ok := config["target_field"].(string); ok {
		s.targetField = tf
	}
	if s.targetField == "" {
		s.targetField = "_lookup"
	}

	// Parse query configuration
	if qf, ok := config["query_field"].(string); ok {
		s.queryField = qf
	}

	if sf, ok := config["source_fields"].([]any); ok {
		for _, f := range sf {
			if fs, ok := f.(string); ok {
				s.sourceFields = append(s.sourceFields, fs)
			}
		}
	}

	// Parse query template
	if qt, ok := config["query_template"].(string); ok {
		tmpl, err := template.New("query").Parse(qt)
		if err != nil {
			return nil, fmt.Errorf("invalid query_template: %w", err)
		}
		s.queryTemplate = tmpl
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

	// Parse authentication
	if auth, ok := config["auth"].(map[string]any); ok {
		if u, ok := auth["username"].(string); ok {
			s.username = u
		}
		if p, ok := auth["password"].(string); ok {
			s.password = p
		}
		if ak, ok := auth["api_key"].(string); ok {
			s.apiKey = ak
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

	// Initialize HTTP client
	s.httpClient = &http.Client{
		Timeout: s.timeout,
	}

	return s, nil
}

func (s *ElasticsearchLookupStage) Process(ctx context.Context, record *Record) (*Record, error) {
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

	// Lookup from Elasticsearch
	lookupCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	result, err := s.lookup(lookupCtx, record.Data, joinKey)
	if err != nil {
		s.incrementError()
		if s.onMissing == "error" {
			return nil, fmt.Errorf("ES lookup failed: %w", err)
		}
		if s.defaultValue != nil {
			setNestedValueHelper(record.Data, s.targetField, s.defaultValue)
		}
		s.incrementOutput()
		return record, nil
	}

	if result == nil {
		// Not found
		switch s.onMissing {
		case "error":
			s.incrementError()
			return nil, fmt.Errorf("ES lookup key '%s' not found", joinKey)
		case "default":
			if s.defaultValue != nil {
				setNestedValueHelper(record.Data, s.targetField, s.defaultValue)
			}
		}
	} else {
		// Cache result
		if s.cacheEnabled && s.cache != nil {
			s.cache.Set(joinKey, result, s.cacheTTL)
		}
		setNestedValueHelper(record.Data, s.targetField, result)
	}

	s.incrementOutput()
	return record, nil
}

func (s *ElasticsearchLookupStage) lookup(ctx context.Context, data map[string]any, joinKey string) (any, error) {
	// Build query
	var query map[string]any

	if s.queryTemplate != nil {
		// Use custom query template
		var buf bytes.Buffer
		if err := s.queryTemplate.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute query template: %w", err)
		}
		if err := json.Unmarshal(buf.Bytes(), &query); err != nil {
			return nil, fmt.Errorf("invalid query JSON from template: %w", err)
		}
	} else if s.queryField == "_id" {
		// Direct document lookup by ID
		return s.lookupByID(ctx, joinKey)
	} else {
		// Term query on specified field
		query = map[string]any{
			"query": map[string]any{
				"term": map[string]any{
					s.queryField: joinKey,
				},
			},
			"size": 1,
		}
	}

	// Add source filtering if specified
	if len(s.sourceFields) > 0 {
		query["_source"] = s.sourceFields
	}

	return s.executeSearch(ctx, query)
}

func (s *ElasticsearchLookupStage) lookupByID(ctx context.Context, docID string) (any, error) {
	endpoint := s.getEndpoint()
	url := fmt.Sprintf("%s/%s/_doc/%s", endpoint, s.index, docID)

	// Add source filtering
	if len(s.sourceFields) > 0 {
		url += "?_source=" + strings.Join(s.sourceFields, ",")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ES returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract _source
	if source, ok := result["_source"].(map[string]any); ok {
		return source, nil
	}

	return result, nil
}

func (s *ElasticsearchLookupStage) executeSearch(ctx context.Context, query map[string]any) (any, error) {
	endpoint := s.getEndpoint()
	url := fmt.Sprintf("%s/%s/_search", endpoint, s.index)

	queryBytes, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(queryBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ES returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract first hit's _source
	if hits, ok := result["hits"].(map[string]any); ok {
		if hitsList, ok := hits["hits"].([]any); ok && len(hitsList) > 0 {
			if firstHit, ok := hitsList[0].(map[string]any); ok {
				if source, ok := firstHit["_source"].(map[string]any); ok {
					return source, nil
				}
				return firstHit, nil
			}
		}
	}

	return nil, nil
}

func (s *ElasticsearchLookupStage) getEndpoint() string {
	s.endpointMu.Lock()
	defer s.endpointMu.Unlock()

	endpoint := s.endpoints[s.endpointIdx]
	s.endpointIdx = (s.endpointIdx + 1) % len(s.endpoints)
	return endpoint
}

func (s *ElasticsearchLookupStage) addAuth(req *http.Request) {
	if s.apiKey != "" {
		req.Header.Set("Authorization", "ApiKey "+s.apiKey)
	} else if s.username != "" && s.password != "" {
		req.SetBasicAuth(s.username, s.password)
	}
}

func (s *ElasticsearchLookupStage) Close() error {
	// HTTP client doesn't need explicit close
	return nil
}

// ESLookupBatchStage performs batch lookups against Elasticsearch using mget or msearch
type ESLookupBatchStage struct {
	BaseStage
	endpoints    []string
	index        string
	joinField    string
	targetField  string
	onMissing    string
	defaultValue any
	timeout      time.Duration
	batchSize    int
	batchTimeout time.Duration
	queryField   string
	sourceFields []string
	httpClient   *http.Client
	username     string
	password     string
	apiKey       string
	cacheEnabled bool
	cacheTTL     time.Duration
	cacheMaxSize int
	cache        *lruCache
	endpointIdx  int
	endpointMu   sync.Mutex

	// Batch buffer
	bufferMu   sync.Mutex
	buffer     []*esLookupItem
	bufferCond *sync.Cond
	closeCh    chan struct{}
	closeOnce  sync.Once
	wg         sync.WaitGroup
}

type esLookupItem struct {
	record   *Record
	joinKey  string
	resultCh chan esLookupResult
}

type esLookupResult struct {
	value any
	err   error
}

// NewESLookupBatchStage creates a new batch Elasticsearch lookup stage
func NewESLookupBatchStage(name string, config map[string]any) (*ESLookupBatchStage, error) {
	s := &ESLookupBatchStage{
		BaseStage:    BaseStage{name: name, typ: "es_lookup_batch", config: config},
		onMissing:    "skip",
		timeout:      5 * time.Second,
		batchSize:    100,
		batchTimeout: 50 * time.Millisecond,
		queryField:   "_id",
		cacheEnabled: true,
		cacheTTL:     5 * time.Minute,
		cacheMaxSize: 10000,
		buffer:       make([]*esLookupItem, 0),
		closeCh:      make(chan struct{}),
	}
	s.bufferCond = sync.NewCond(&s.bufferMu)

	// Parse endpoints
	if eps, ok := config["endpoints"].([]any); ok {
		for _, ep := range eps {
			if epStr, ok := ep.(string); ok {
				s.endpoints = append(s.endpoints, strings.TrimSuffix(epStr, "/"))
			}
		}
	}
	if ep, ok := config["endpoint"].(string); ok {
		s.endpoints = append(s.endpoints, strings.TrimSuffix(ep, "/"))
	}
	if len(s.endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint is required")
	}

	// Parse index
	if idx, ok := config["index"].(string); ok {
		s.index = idx
	}
	if s.index == "" {
		return nil, fmt.Errorf("index is required")
	}

	// Parse join/target fields
	if jf, ok := config["join_field"].(string); ok {
		s.joinField = jf
	}
	if s.joinField == "" {
		return nil, fmt.Errorf("join_field is required")
	}
	if tf, ok := config["target_field"].(string); ok {
		s.targetField = tf
	}
	if s.targetField == "" {
		s.targetField = "_lookup"
	}

	// Parse query configuration
	if qf, ok := config["query_field"].(string); ok {
		s.queryField = qf
	}

	if sf, ok := config["source_fields"].([]any); ok {
		for _, f := range sf {
			if fs, ok := f.(string); ok {
				s.sourceFields = append(s.sourceFields, fs)
			}
		}
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
	}

	// Parse authentication
	if auth, ok := config["auth"].(map[string]any); ok {
		if u, ok := auth["username"].(string); ok {
			s.username = u
		}
		if p, ok := auth["password"].(string); ok {
			s.password = p
		}
		if ak, ok := auth["api_key"].(string); ok {
			s.apiKey = ak
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

	// Initialize HTTP client
	s.httpClient = &http.Client{
		Timeout: s.timeout,
	}

	// Start background batch processor
	s.wg.Add(1)
	go s.batchProcessor()

	return s, nil
}

func (s *ESLookupBatchStage) Process(ctx context.Context, record *Record) (*Record, error) {
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
	item := &esLookupItem{
		record:   record,
		joinKey:  joinKey,
		resultCh: make(chan esLookupResult, 1),
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
			switch s.onMissing {
			case "error":
				s.incrementError()
				return nil, fmt.Errorf("ES lookup key '%s' not found", joinKey)
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

func (s *ESLookupBatchStage) batchProcessor() {
	defer s.wg.Done()

	timer := time.NewTimer(s.batchTimeout)
	defer timer.Stop()

	for {
		select {
		case <-s.closeCh:
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

func (s *ESLookupBatchStage) processBatch() {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return
	}

	items := s.buffer
	s.buffer = make([]*esLookupItem, 0)
	s.bufferMu.Unlock()

	// Collect unique keys
	keySet := make(map[string][]*esLookupItem)
	for _, item := range items {
		keySet[item.joinKey] = append(keySet[item.joinKey], item)
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}

	// Perform batch lookup using mget
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	results, err := s.batchLookup(ctx, keys)
	if err != nil {
		for _, itemList := range keySet {
			for _, item := range itemList {
				item.resultCh <- esLookupResult{err: err}
			}
		}
		return
	}

	for key, itemList := range keySet {
		value, exists := results[key]
		for _, item := range itemList {
			if exists {
				if s.cacheEnabled && s.cache != nil {
					s.cache.Set(key, value, s.cacheTTL)
				}
				item.resultCh <- esLookupResult{value: value}
			} else {
				item.resultCh <- esLookupResult{}
			}
		}
	}
}

func (s *ESLookupBatchStage) batchLookup(ctx context.Context, keys []string) (map[string]any, error) {
	endpoint := s.getEndpoint()

	// Use _mget for batch lookup
	docs := make([]map[string]any, len(keys))
	for i, key := range keys {
		if s.queryField == "_id" {
			docs[i] = map[string]any{"_id": key}
		} else {
			docs[i] = map[string]any{
				"_source": s.sourceFields,
				"_id":     key,
			}
		}
	}

	mgetBody := map[string]any{"docs": docs}
	if len(s.sourceFields) > 0 {
		mgetBody["_source"] = s.sourceFields
	}

	bodyBytes, err := json.Marshal(mgetBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal mget request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/_mget", endpoint, s.index)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mget request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mget returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode mget response: %w", err)
	}

	// Extract results
	results := make(map[string]any)
	if docs, ok := result["docs"].([]any); ok {
		for i, doc := range docs {
			if docMap, ok := doc.(map[string]any); ok {
				if found, ok := docMap["found"].(bool); ok && found {
					if source, ok := docMap["_source"].(map[string]any); ok {
						results[keys[i]] = source
					}
				}
			}
		}
	}

	return results, nil
}

func (s *ESLookupBatchStage) getEndpoint() string {
	s.endpointMu.Lock()
	defer s.endpointMu.Unlock()

	endpoint := s.endpoints[s.endpointIdx]
	s.endpointIdx = (s.endpointIdx + 1) % len(s.endpoints)
	return endpoint
}

func (s *ESLookupBatchStage) addAuth(req *http.Request) {
	if s.apiKey != "" {
		req.Header.Set("Authorization", "ApiKey "+s.apiKey)
	} else if s.username != "" && s.password != "" {
		req.SetBasicAuth(s.username, s.password)
	}
}

func (s *ESLookupBatchStage) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.bufferCond.Signal()
	})

	s.wg.Wait()
	return nil
}
