package stream

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BaseSource provides common source functionality
type BaseSource struct {
	name       string
	typ        string
	config     map[string]any
	bufferSize int

	paused     atomic.Bool
	inputCount int64
	mu         sync.Mutex
}

func (s *BaseSource) Name() string { return s.name }
func (s *BaseSource) Type() string { return s.typ }

func (s *BaseSource) Pause() {
	s.paused.Store(true)
}

func (s *BaseSource) Resume() {
	s.paused.Store(false)
}

func (s *BaseSource) IsPaused() bool {
	return s.paused.Load()
}

func (s *BaseSource) incrementInput() {
	s.mu.Lock()
	s.inputCount++
	s.mu.Unlock()
}

func (s *BaseSource) Stats() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputCount
}

// DemoSource generates demo data for testing
type DemoSource struct {
	BaseSource
	interval time.Duration
	counter  int64
}

func NewDemoSource(name string, config map[string]any) *DemoSource {
	interval := time.Second
	if i, ok := config["interval"].(string); ok {
		if d, err := time.ParseDuration(i); err == nil {
			interval = d
		}
	}

	bufferSize := 1000
	if bs, ok := config["buffer_size"].(int); ok {
		bufferSize = bs
	}

	return &DemoSource{
		BaseSource: BaseSource{
			name:       name,
			typ:        "demo",
			config:     config,
			bufferSize: bufferSize,
		},
		interval: interval,
	}
}

func (s *DemoSource) Start(ctx context.Context, out chan<- *Record) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	defer close(out)

	levels := []string{"debug", "info", "warn", "error"}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.IsPaused() {
				continue
			}

			s.counter++
			s.incrementInput()

			record := &Record{
				Data: map[string]any{
					"id":        s.counter,
					"message":   fmt.Sprintf("Demo event %d", s.counter),
					"timestamp": time.Now().Format(time.RFC3339),
					"level":     levels[s.counter%4],
					"source":    s.name,
				},
				Metadata: RecordMetadata{
					Source: s.name,
					Offset: s.counter,
				},
				Timestamp: time.Now(),
			}

			select {
			case out <- record:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *DemoSource) Close() error {
	return nil
}

// KafkaSource reads from Kafka topics
type KafkaSource struct {
	BaseSource
	brokers []string
	topics  []string
	groupID string
}

func NewKafkaSource(name string, config map[string]any) *KafkaSource {
	var brokers []string
	if b, ok := config["brokers"].([]any); ok {
		for _, broker := range b {
			if bs, ok := broker.(string); ok {
				brokers = append(brokers, bs)
			}
		}
	}

	var topics []string
	if t, ok := config["topics"].([]any); ok {
		for _, topic := range t {
			if ts, ok := topic.(string); ok {
				topics = append(topics, ts)
			}
		}
	}

	groupID := "pipeline-group"
	if g, ok := config["group_id"].(string); ok {
		groupID = g
	}

	bufferSize := 10000
	if bs, ok := config["buffer_size"].(int); ok {
		bufferSize = bs
	}

	return &KafkaSource{
		BaseSource: BaseSource{
			name:       name,
			typ:        "kafka",
			config:     config,
			bufferSize: bufferSize,
		},
		brokers: brokers,
		topics:  topics,
		groupID: groupID,
	}
}

func (s *KafkaSource) Start(ctx context.Context, out chan<- *Record) error {
	defer close(out)

	// TODO: Implement actual Kafka consumer
	// For now, simulate Kafka behavior
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var counter int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.IsPaused() {
				continue
			}

			counter++
			s.incrementInput()

			record := &Record{
				Data: map[string]any{
					"message":   fmt.Sprintf("Kafka message %d", counter),
					"timestamp": time.Now().UnixMilli(),
					"topic":     s.topics[0],
					"partition": 0,
					"offset":    counter,
				},
				Metadata: RecordMetadata{
					Source:    s.name,
					Partition: 0,
					Offset:    counter,
				},
				Timestamp: time.Now(),
			}

			select {
			case out <- record:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *KafkaSource) Close() error {
	// TODO: Close Kafka consumer
	return nil
}

// FileSource reads from files
type FileSource struct {
	BaseSource
	path string
}

func NewFileSource(name string, config map[string]any) *FileSource {
	path := ""
	if p, ok := config["path"].(string); ok {
		path = p
	}

	return &FileSource{
		BaseSource: BaseSource{
			name:       name,
			typ:        "file",
			config:     config,
			bufferSize: 1000,
		},
		path: path,
	}
}

func (s *FileSource) Start(ctx context.Context, out chan<- *Record) error {
	defer close(out)

	// TODO: Implement actual file reading
	// For now, simulate file behavior
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var counter int64

	for i := 0; i < 100; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.IsPaused() {
				continue
			}

			counter++
			s.incrementInput()

			record := &Record{
				Data: map[string]any{
					"line":    counter,
					"message": fmt.Sprintf("Line %d from %s", counter, s.path),
					"file":    s.path,
				},
				Metadata: RecordMetadata{
					Source: s.name,
					Offset: counter,
				},
				Timestamp: time.Now(),
			}

			select {
			case out <- record:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

func (s *FileSource) Close() error {
	return nil
}

// HTTPSource receives data via HTTP
type HTTPSource struct {
	BaseSource
	address string
	path    string
}

func NewHTTPSource(name string, config map[string]any) *HTTPSource {
	address := ":8080"
	if a, ok := config["address"].(string); ok {
		address = a
	}

	path := "/events"
	if p, ok := config["path"].(string); ok {
		path = p
	}

	return &HTTPSource{
		BaseSource: BaseSource{
			name:       name,
			typ:        "http_server",
			config:     config,
			bufferSize: 10000,
		},
		address: address,
		path:    path,
	}
}

func (s *HTTPSource) Start(ctx context.Context, out chan<- *Record) error {
	defer close(out)

	// TODO: Implement actual HTTP server
	// For now, wait for context cancellation
	<-ctx.Done()
	return ctx.Err()
}

func (s *HTTPSource) Close() error {
	return nil
}

// NewSource creates a source from configuration
func NewSource(cfg SourceConfig) (Source, error) {
	switch cfg.Type {
	case "demo":
		return NewDemoSource(cfg.Name, cfg.Config), nil
	case "kafka":
		return NewKafkaSource(cfg.Name, cfg.Config), nil
	case "file":
		return NewFileSource(cfg.Name, cfg.Config), nil
	case "http_server":
		return NewHTTPSource(cfg.Name, cfg.Config), nil
	default:
		return nil, fmt.Errorf("unknown source type: %s", cfg.Type)
	}
}
