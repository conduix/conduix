// Package bento provides adapters for integrating Bento (formerly Benthos)
// components with the Actor-based pipeline system.
//
// Bento is an MIT-licensed fork of Benthos, providing high-performance
// stream processing with connectors for Kafka, Elasticsearch, S3, and more.
package bento

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/warpstreamlabs/bento/public/service"
)

// Note: If the import fails, run:
// go get github.com/warpstreamlabs/bento@latest

// Common errors
var (
	ErrAdapterClosed  = errors.New("adapter is closed")
	ErrNotInitialized = errors.New("adapter not initialized")
)

// MessageConverter converts between Bento messages and map[string]any
type MessageConverter struct{}

// ToMap converts a Bento message to map[string]any
func (c *MessageConverter) ToMap(msg *service.Message) (map[string]any, error) {
	// Try structured data first
	structured, err := msg.AsStructured()
	if err == nil {
		if m, ok := structured.(map[string]any); ok {
			return m, nil
		}
	}

	// Fall back to bytes
	data, err := msg.AsBytes()
	if err != nil {
		return nil, err
	}

	// Try JSON parsing
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		// If not JSON, wrap raw data
		result = map[string]any{
			"message":   string(data),
			"timestamp": time.Now().Format(time.RFC3339),
		}
	}

	// Copy metadata
	_ = msg.MetaWalkMut(func(k string, v any) error {
		result["_meta_"+k] = v
		return nil
	})

	return result, nil
}

// FromMap converts map[string]any to a Bento message
func (c *MessageConverter) FromMap(data map[string]any) *service.Message {
	jsonData, _ := json.Marshal(data)
	msg := service.NewMessage(jsonData)

	// Extract metadata
	for k, v := range data {
		if len(k) > 6 && k[:6] == "_meta_" {
			// Convert value to string for MetaSet
			if strVal, ok := v.(string); ok {
				msg.MetaSet(k[6:], strVal)
			}
		}
	}

	return msg
}

// InputAdapter wraps a Bento input as a message source
type InputAdapter struct {
	input     *service.OwnedInput
	converter *MessageConverter
	running   bool
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewInputAdapter creates a new input adapter
func NewInputAdapter(input *service.OwnedInput) *InputAdapter {
	return &InputAdapter{
		input:     input,
		converter: &MessageConverter{},
	}
}

// Start begins reading from the input
func (a *InputAdapter) Start(ctx context.Context, handler func(map[string]any) error) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.running = true
	a.mu.Unlock()

	go a.readLoop(handler)
	return nil
}

func (a *InputAdapter) readLoop(handler func(map[string]any) error) {
	// TODO: Bento OwnedInput API 변경으로 인해 재구현 필요
	// 현재 버전의 Bento는 다른 메서드를 사용합니다.
	// 실제 구현에서는 Bento의 stream builder를 사용하여
	// 메시지를 읽어야 합니다.
	//
	// 예시 (구버전):
	// msg, ackFunc, err := a.input.Read(a.ctx)
	//
	// 새 버전에서는 StreamBuilder를 통해 전체 파이프라인을
	// 구성하는 방식으로 변경되었습니다.
	<-a.ctx.Done()
}

// Stop stops the input adapter
func (a *InputAdapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	a.running = false
	if a.cancel != nil {
		a.cancel()
	}

	return a.input.Close(context.Background())
}

// OutputAdapter wraps a Bento output as a message sink
type OutputAdapter struct {
	output    *service.OwnedOutput
	converter *MessageConverter
	running   bool
	mu        sync.RWMutex
}

// NewOutputAdapter creates a new output adapter
func NewOutputAdapter(output *service.OwnedOutput) *OutputAdapter {
	return &OutputAdapter{
		output:    output,
		converter: &MessageConverter{},
		running:   true,
	}
}

// Write sends data to the output
func (a *OutputAdapter) Write(ctx context.Context, data map[string]any) error {
	a.mu.RLock()
	if !a.running {
		a.mu.RUnlock()
		return ErrAdapterClosed
	}
	a.mu.RUnlock()

	msg := a.converter.FromMap(data)
	return a.output.Write(ctx, msg)
}

// WriteBatch sends multiple data items to the output
func (a *OutputAdapter) WriteBatch(ctx context.Context, batch []map[string]any) error {
	a.mu.RLock()
	if !a.running {
		a.mu.RUnlock()
		return ErrAdapterClosed
	}
	a.mu.RUnlock()

	msgs := make(service.MessageBatch, len(batch))
	for i, data := range batch {
		msgs[i] = a.converter.FromMap(data)
	}

	return a.output.WriteBatch(ctx, msgs)
}

// Stop stops the output adapter
func (a *OutputAdapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	a.running = false
	return a.output.Close(context.Background())
}

// ProcessorAdapter wraps a Bento processor for transformations
type ProcessorAdapter struct {
	processor *service.OwnedProcessor
	converter *MessageConverter
}

// NewProcessorAdapter creates a new processor adapter
func NewProcessorAdapter(processor *service.OwnedProcessor) *ProcessorAdapter {
	return &ProcessorAdapter{
		processor: processor,
		converter: &MessageConverter{},
	}
}

// Process transforms input data
func (a *ProcessorAdapter) Process(ctx context.Context, data map[string]any) ([]map[string]any, error) {
	msg := a.converter.FromMap(data)
	batch := service.MessageBatch{msg}

	results, err := a.processor.ProcessBatch(ctx, batch)
	if err != nil {
		return nil, err
	}

	var outputs []map[string]any
	for _, result := range results {
		for _, m := range result {
			if output, err := a.converter.ToMap(m); err == nil {
				outputs = append(outputs, output)
			}
		}
	}

	return outputs, nil
}

// Stop stops the processor adapter
func (a *ProcessorAdapter) Stop() error {
	return a.processor.Close(context.Background())
}
