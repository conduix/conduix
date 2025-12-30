package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
)

// PipelineActor is an actor that wraps StreamProcessor.
// It maintains the actor model for pipeline-level coordination
// (start/stop/pause, distributed deployment, supervision)
// but uses StreamProcessor internally for efficient local processing.
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                    Actor Model (Distribution)               │
//	│  ┌─────────────────────────────────────────────────────┐   │
//	│  │              PipelineActor                           │   │
//	│  │  - Receives commands via Tell()                      │   │
//	│  │  - Manages lifecycle (start/stop/pause)              │   │
//	│  │  - Can run on different servers                      │   │
//	│  │                                                       │   │
//	│  │  ┌─────────────────────────────────────────────────┐ │   │
//	│  │  │         StreamProcessor (Local)                 │ │   │
//	│  │  │  Source → Transform Chain → Sink               │ │   │
//	│  │  │  (Direct function calls, single goroutine)     │ │   │
//	│  │  └─────────────────────────────────────────────────┘ │   │
//	│  └─────────────────────────────────────────────────────┘   │
//	└─────────────────────────────────────────────────────────────┘
type PipelineActor struct {
	*actor.BaseActor
	name      string
	config    *PipelineConfig
	processor *StreamProcessor
	logger    *slog.Logger
	mu        sync.RWMutex
}

// PipelineConfig holds the pipeline configuration
type PipelineConfig struct {
	Name       string        `yaml:"name" json:"name"`
	Source     SourceConfig  `yaml:"source" json:"source"`
	Stages     []StageConfig `yaml:"stages" json:"stages"`
	Sink       SinkConfig    `yaml:"sink" json:"sink"`
	BufferSize int           `yaml:"buffer_size" json:"buffer_size"`
}

// NewPipelineActor creates a new pipeline actor
func NewPipelineActor(name string, config map[string]any) (*PipelineActor, error) {
	pipelineConfig, err := parsePipelineConfig(config)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline config: %w", err)
	}

	return &PipelineActor{
		BaseActor: actor.NewBaseActor(name, config),
		name:      name,
		config:    pipelineConfig,
		logger:    slog.Default().With("pipeline", name),
	}, nil
}

// parsePipelineConfig parses the raw config into PipelineConfig
func parsePipelineConfig(config map[string]any) (*PipelineConfig, error) {
	pc := &PipelineConfig{
		BufferSize: 1000,
	}

	if name, ok := config["name"].(string); ok {
		pc.Name = name
	}

	if bufSize, ok := config["buffer_size"].(int); ok {
		pc.BufferSize = bufSize
	}

	// Parse source
	if src, ok := config["source"].(map[string]any); ok {
		pc.Source = SourceConfig{
			Type:   getStringOrDefault(src, "type", "demo"),
			Name:   getStringOrDefault(src, "name", "source"),
			Config: src,
		}
	}

	// Parse stages
	if stages, ok := config["stages"].([]any); ok {
		for i, s := range stages {
			if sm, ok := s.(map[string]any); ok {
				pc.Stages = append(pc.Stages, StageConfig{
					Type:   getStringOrDefault(sm, "type", "passthrough"),
					Name:   getStringOrDefault(sm, "name", fmt.Sprintf("stage_%d", i)),
					Config: sm,
				})
			}
		}
	}

	// Parse sink
	if sink, ok := config["sink"].(map[string]any); ok {
		pc.Sink = SinkConfig{
			Type:   getStringOrDefault(sink, "type", "console"),
			Name:   getStringOrDefault(sink, "name", "sink"),
			Config: sink,
		}
	}

	return pc, nil
}

func getStringOrDefault(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

// PreStart initializes the pipeline
func (p *PipelineActor) PreStart(ctx actor.ActorContext) error {
	if err := p.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	p.logger.Info("Initializing pipeline actor", "name", p.name)

	// Create source
	source, err := NewSource(p.config.Source)
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}

	// Create stages
	var stages []Stage
	for _, sc := range p.config.Stages {
		s, err := NewStage(sc)
		if err != nil {
			return fmt.Errorf("create stage %s: %w", sc.Name, err)
		}
		stages = append(stages, s)
	}

	// Create sink
	sink, err := NewSink(p.config.Sink)
	if err != nil {
		return fmt.Errorf("create sink: %w", err)
	}

	// Create StreamProcessor
	p.processor = NewStreamProcessor(
		ProcessorConfig{
			Name:       p.name,
			BufferSize: p.config.BufferSize,
			Logger:     p.logger,
		},
		source,
		stages,
		sink,
	)

	// Start the processor
	if err := p.processor.Start(context.Background()); err != nil {
		return fmt.Errorf("start processor: %w", err)
	}

	p.logger.Info("Pipeline actor started", "name", p.name)
	return nil
}

// PostStop cleans up resources
func (p *PipelineActor) PostStop(ctx actor.ActorContext) error {
	p.logger.Info("Stopping pipeline actor", "name", p.name)

	p.mu.Lock()
	processor := p.processor
	p.mu.Unlock()

	if processor != nil {
		if err := processor.Stop(); err != nil {
			p.logger.Error("Error stopping processor", "error", err)
		}
	}

	return p.BaseActor.PostStop(ctx)
}

// Receive handles incoming messages
func (p *PipelineActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeCommand:
		return p.handleCommand(ctx, msg)
	case actor.MessageTypeData:
		// Query messages come as data type with specific payload
		return p.handleQuery(ctx, msg)
	default:
		return nil
	}
}

func (p *PipelineActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	cmd, ok := msg.Payload.(string)
	if !ok {
		return fmt.Errorf("invalid command payload type: %T", msg.Payload)
	}

	p.mu.RLock()
	processor := p.processor
	p.mu.RUnlock()

	if processor == nil {
		return fmt.Errorf("processor not initialized")
	}

	switch cmd {
	case "pause":
		return processor.Pause()
	case "resume":
		return processor.Resume()
	case "stop":
		return processor.Stop()
	default:
		p.logger.Warn("Unknown command", "command", cmd)
		return nil
	}
}

func (p *PipelineActor) handleQuery(ctx actor.ActorContext, msg actor.Message) error {
	query, ok := msg.Payload.(string)
	if !ok {
		return nil
	}

	p.mu.RLock()
	processor := p.processor
	p.mu.RUnlock()

	if processor == nil {
		return nil
	}

	switch query {
	case "stats":
		stats := processor.Stats()
		if msg.ReplyTo != nil {
			reply := actor.Message{
				ID:      actor.GenerateID(),
				Type:    actor.MessageTypeData,
				Payload: stats,
			}
			msg.ReplyTo <- reply
		}
	case "state":
		state := processor.State().String()
		if msg.ReplyTo != nil {
			reply := actor.Message{
				ID:      actor.GenerateID(),
				Type:    actor.MessageTypeData,
				Payload: state,
			}
			msg.ReplyTo <- reply
		}
	}

	return nil
}

// GetStats returns current pipeline statistics
func (p *PipelineActor) GetStats() ProcessorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.processor == nil {
		return ProcessorStats{}
	}

	return p.processor.Stats()
}

// GetState returns current pipeline state
func (p *PipelineActor) GetState() ProcessorState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.processor == nil {
		return ProcessorStateCreated
	}

	return p.processor.State()
}
