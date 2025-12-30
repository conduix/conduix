package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/stream"
	"github.com/conduix/conduix/shared/types"
)

// Runner 파이프라인 실행기
type Runner struct {
	config       *config.PipelineConfig
	system       *actor.System
	rootActor    *actor.ActorRef
	processor    *stream.StreamProcessor // For stream pipeline type
	status       types.PipelineStatus
	startTime    time.Time
	stopTime     time.Time
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	logger       actor.Logger
	slogger      *slog.Logger
	checkpointer actor.Checkpointer
}

// NewRunner 새 Runner 생성
func NewRunner(cfg *config.PipelineConfig, opts ...RunnerOption) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &Runner{
		config: cfg,
		status: types.PipelineStatusPending,
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r, nil
}

// RunnerOption Runner 옵션
type RunnerOption func(*Runner)

// WithRunnerLogger 로거 설정
func WithRunnerLogger(logger actor.Logger) RunnerOption {
	return func(r *Runner) {
		r.logger = logger
	}
}

// WithRunnerCheckpointer 체크포인터 설정
func WithRunnerCheckpointer(cp actor.Checkpointer) RunnerOption {
	return func(r *Runner) {
		r.checkpointer = cp
	}
}

// Start 파이프라인 시작
func (r *Runner) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == types.PipelineStatusRunning {
		return fmt.Errorf("pipeline is already running")
	}

	// Actor 시스템 생성
	sysOpts := []actor.SystemOption{}
	if r.logger != nil {
		sysOpts = append(sysOpts, actor.WithLogger(r.logger))
	}
	if r.checkpointer != nil {
		sysOpts = append(sysOpts, actor.WithCheckpointer(r.checkpointer))
	}

	r.system = actor.NewSystem(r.config.Name, r.config.ActorSystem, sysOpts...)

	if err := r.system.Start(); err != nil {
		return fmt.Errorf("failed to start actor system: %w", err)
	}

	// 파이프라인 타입에 따른 시작
	var err error
	switch r.config.Type {
	case types.PipelineTypeFlat:
		err = r.startFlatPipeline()
	case types.PipelineTypeActor:
		err = r.startActorPipeline()
	case types.PipelineTypeStream:
		err = r.startStreamPipeline()
	default:
		err = fmt.Errorf("unknown pipeline type: %s", r.config.Type)
	}

	if err != nil {
		_ = r.system.Stop()
		return err
	}

	r.status = types.PipelineStatusRunning
	r.startTime = time.Now()

	return nil
}

// startFlatPipeline Flat 모드 파이프라인 시작
func (r *Runner) startFlatPipeline() error {
	// Sources 생성
	sourceRefs := make(map[string]*actor.ActorRef)
	for name, src := range r.config.Sources {
		srcConfig := map[string]any{
			"source_type": src.Type,
		}
		for k, v := range src.Options {
			srcConfig[k] = v
		}

		ref, err := r.system.Spawn(actor.Props{
			Name: name,
			Factory: func() actor.Actor {
				return actor.NewSourceActor(name, srcConfig)
			},
		})
		if err != nil {
			return fmt.Errorf("failed to spawn source %s: %w", name, err)
		}
		sourceRefs[name] = ref
	}

	// Transforms 생성
	transformRefs := make(map[string]*actor.ActorRef)
	for name, trans := range r.config.Transforms {
		transConfig := map[string]any{
			"transform_type": trans.Type,
			"inputs":         trans.Inputs,
		}
		for k, v := range trans.Options {
			transConfig[k] = v
		}

		ref, err := r.system.Spawn(actor.Props{
			Name: name,
			Factory: func() actor.Actor {
				return actor.NewTransformActor(name, transConfig)
			},
		})
		if err != nil {
			return fmt.Errorf("failed to spawn transform %s: %w", name, err)
		}
		transformRefs[name] = ref
	}

	// Sinks 생성
	for name, sink := range r.config.Sinks {
		sinkConfig := map[string]any{
			"sink_type": sink.Type,
			"inputs":    sink.Inputs,
		}
		for k, v := range sink.Options {
			sinkConfig[k] = v
		}

		_, err := r.system.Spawn(actor.Props{
			Name: name,
			Factory: func() actor.Actor {
				return actor.NewSinkActor(name, sinkConfig)
			},
		})
		if err != nil {
			return fmt.Errorf("failed to spawn sink %s: %w", name, err)
		}
	}

	return nil
}

// startActorPipeline Actor 모드 파이프라인 시작
func (r *Runner) startActorPipeline() error {
	if r.config.Pipeline == nil {
		return fmt.Errorf("pipeline definition is required")
	}

	// 루트 Supervisor 생성
	rootProps := actor.Props{
		Name: r.config.Pipeline.Name,
		Factory: func() actor.Actor {
			return actor.NewSupervisor(r.config.Pipeline.Name, r.config.Pipeline.Supervision)
		},
		Supervision: r.config.Pipeline.Supervision,
	}

	ref, err := r.system.Spawn(rootProps)
	if err != nil {
		return fmt.Errorf("failed to spawn root actor: %w", err)
	}

	r.rootActor = ref

	// 자식 Actor들 생성 (재귀적)
	return r.spawnChildren(ref, r.config.Pipeline.Children)
}

func (r *Runner) spawnChildren(parent *actor.ActorRef, children []types.ActorDefinition) error {
	for _, child := range children {
		props := r.createProps(child)

		// 부모에게 자식 생성 요청
		_ = parent.Tell(actor.Message{
			Type: actor.MessageTypeLifecycle,
			Payload: SpawnChildRequest{
				Props:    props,
				Children: child.Children,
			},
		})
	}

	return nil
}

// SpawnChildRequest 자식 생성 요청
type SpawnChildRequest struct {
	Props    actor.Props
	Children []types.ActorDefinition
}

// startStreamPipeline Stream 모드 파이프라인 시작 (로컬 최적화)
// Actor 모델은 파이프라인 레벨에서만 사용하고,
// 내부 Source → Transform → Sink 처리는 직접 함수 호출 사용
func (r *Runner) startStreamPipeline() error {
	if r.slogger == nil {
		r.slogger = slog.Default()
	}

	// Source 생성
	var source stream.Source
	for name, src := range r.config.Sources {
		srcConfig := make(map[string]any)
		for k, v := range src.Options {
			srcConfig[k] = v
		}

		var err error
		source, err = stream.NewSource(stream.SourceConfig{
			Type:   src.Type,
			Name:   name,
			Config: srcConfig,
		})
		if err != nil {
			return fmt.Errorf("failed to create source %s: %w", name, err)
		}
		break // Use first source for now
	}

	if source == nil {
		return fmt.Errorf("no source defined")
	}

	// Stage 체인 생성
	var stages []stream.Stage
	for name, trans := range r.config.Transforms {
		transConfig := make(map[string]any)
		for k, v := range trans.Options {
			transConfig[k] = v
		}

		s, err := stream.NewStage(stream.StageConfig{
			Type:   trans.Type,
			Name:   name,
			Config: transConfig,
		})
		if err != nil {
			return fmt.Errorf("failed to create stage %s: %w", name, err)
		}
		stages = append(stages, s)
	}

	// Sink 생성
	var sink stream.Sink
	for name, s := range r.config.Sinks {
		sinkConfig := make(map[string]any)
		for k, v := range s.Options {
			sinkConfig[k] = v
		}

		var err error
		sink, err = stream.NewSink(stream.SinkConfig{
			Type:   s.Type,
			Name:   name,
			Config: sinkConfig,
		})
		if err != nil {
			return fmt.Errorf("failed to create sink %s: %w", name, err)
		}
		break // Use first sink for now
	}

	if sink == nil {
		return fmt.Errorf("no sink defined")
	}

	// StreamProcessor 생성 및 시작
	r.processor = stream.NewStreamProcessor(
		stream.ProcessorConfig{
			Name:       r.config.Name,
			BufferSize: 10000,
			Logger:     r.slogger,
		},
		source,
		stages,
		sink,
	)

	if err := r.processor.Start(r.ctx); err != nil {
		return fmt.Errorf("failed to start stream processor: %w", err)
	}

	return nil
}

func (r *Runner) createProps(def types.ActorDefinition) actor.Props {
	props := actor.Props{
		Name:        def.Name,
		Parallelism: def.Parallelism,
		Supervision: def.Supervision,
		Outputs:     def.Outputs,
	}

	switch def.Type {
	case types.ActorTypeSupervisor:
		props.Factory = func() actor.Actor {
			return actor.NewSupervisor(def.Name, def.Supervision)
		}
	case types.ActorTypeSource:
		config := def.Config
		props.Factory = func() actor.Actor {
			return actor.NewSourceActor(def.Name, config)
		}
	case types.ActorTypeTransform:
		config := def.Config
		props.Factory = func() actor.Actor {
			return actor.NewTransformActor(def.Name, config)
		}
	case types.ActorTypeSink:
		config := def.Config
		props.Factory = func() actor.Actor {
			return actor.NewSinkActor(def.Name, config)
		}
	case types.ActorTypeRouter:
		config := def.Config
		props.Factory = func() actor.Actor {
			return actor.NewRouterActor(def.Name, config)
		}
	}

	return props
}

// Stop 파이프라인 중지
func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != types.PipelineStatusRunning && r.status != types.PipelineStatusPaused {
		return fmt.Errorf("pipeline is not running")
	}

	r.cancel()

	// Stop stream processor if running
	if r.processor != nil {
		if err := r.processor.Stop(); err != nil {
			return fmt.Errorf("failed to stop stream processor: %w", err)
		}
	}

	if r.system != nil {
		if err := r.system.Stop(); err != nil {
			return fmt.Errorf("failed to stop actor system: %w", err)
		}
	}

	r.status = types.PipelineStatusStopped
	r.stopTime = time.Now()

	return nil
}

// Pause 파이프라인 일시 중지
func (r *Runner) Pause() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != types.PipelineStatusRunning {
		return fmt.Errorf("pipeline is not running")
	}

	// Stream processor pause
	if r.processor != nil {
		if err := r.processor.Pause(); err != nil {
			return fmt.Errorf("failed to pause stream processor: %w", err)
		}
	}

	// TODO: Actor 모드에서는 모든 Actor에 pause 명령 전송

	r.status = types.PipelineStatusPaused
	return nil
}

// Resume 파이프라인 재개
func (r *Runner) Resume() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != types.PipelineStatusPaused {
		return fmt.Errorf("pipeline is not paused")
	}

	// Stream processor resume
	if r.processor != nil {
		if err := r.processor.Resume(); err != nil {
			return fmt.Errorf("failed to resume stream processor: %w", err)
		}
	}

	// TODO: Actor 모드에서는 모든 Actor에 resume 명령 전송

	r.status = types.PipelineStatusRunning
	return nil
}

// Status 상태 조회
func (r *Runner) Status() types.PipelineStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

// Stats 통계 조회
func (r *Runner) Stats() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := map[string]any{
		"status":     r.status,
		"start_time": r.startTime,
	}

	if !r.stopTime.IsZero() {
		stats["stop_time"] = r.stopTime
		stats["duration"] = r.stopTime.Sub(r.startTime).String()
	} else if r.status == types.PipelineStatusRunning {
		stats["duration"] = time.Since(r.startTime).String()
	}

	return stats
}

// Wait 파이프라인 종료 대기
func (r *Runner) Wait() error {
	<-r.ctx.Done()
	return nil
}
