package types

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
	"github.com/conduix/conduix/pipeline-core/pkg/adapter/bento"
)

// BentoSourceActor Bento 기반 소스 Actor
type BentoSourceActor struct {
	*actor.BaseActor
	sourceType     string
	config         map[string]any
	outputs        []string
	adapter        *bento.InputAdapter
	configBuilder  *bento.ConfigBuilder
	running        bool
	processedCount int64
	errorCount     int64
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewBentoSourceActor 새 BentoSourceActor 생성
func NewBentoSourceActor(name string, config map[string]any) *BentoSourceActor {
	sourceType := "generate"
	if st, ok := config["source_type"].(string); ok {
		sourceType = st
	}
	// type 필드도 지원
	if st, ok := config["type"].(string); ok {
		sourceType = st
	}

	outputs := make([]string, 0)
	if outs, ok := config["outputs"].([]string); ok {
		outputs = outs
	}

	return &BentoSourceActor{
		BaseActor:     actor.NewBaseActor(name, config),
		sourceType:    sourceType,
		config:        config,
		outputs:       outputs,
		configBuilder: bento.NewConfigBuilder(),
	}
}

// PreStart 시작 전 초기화
func (s *BentoSourceActor) PreStart(ctx actor.ActorContext) error {
	if err := s.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	// Bento input 생성
	input, err := s.configBuilder.BuildInput(s.sourceType, s.config)
	if err != nil {
		ctx.Logger().Warn("Failed to create Bento input, using fallback",
			"type", s.sourceType, "error", err)
		// Fallback to demo source
		return s.startFallbackSource(ctx)
	}

	s.adapter = bento.NewInputAdapter(input)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	// 데이터 읽기 시작
	if err := s.adapter.Start(s.ctx, func(data map[string]any) error {
		return s.emit(ctx, data)
	}); err != nil {
		return fmt.Errorf("failed to start input adapter: %w", err)
	}

	ctx.Logger().Info("Bento source actor started", "type", s.sourceType)
	return nil
}

// startFallbackSource 폴백 데모 소스 시작
func (s *BentoSourceActor) startFallbackSource(ctx actor.ActorContext) error {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		counter := 0
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.mu.RLock()
				running := s.running
				s.mu.RUnlock()
				if !running {
					continue
				}

				counter++
				data := map[string]any{
					"id":        counter,
					"message":   "Fallback demo event",
					"timestamp": time.Now().Format(time.RFC3339),
					"source":    s.sourceType,
				}

				_ = s.emit(ctx, data)
			}
		}
	}()

	ctx.Logger().Info("Fallback source started (demo mode)", "type", s.sourceType)
	return nil
}

// PostStop 종료 후 정리
func (s *BentoSourceActor) PostStop(ctx actor.ActorContext) error {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.adapter != nil {
		if err := s.adapter.Stop(); err != nil {
			ctx.Logger().Error("Failed to stop adapter", "error", err)
		}
	}

	ctx.Logger().Info("Bento source actor stopped",
		"type", s.sourceType,
		"processed", s.processedCount,
		"errors", s.errorCount)

	return s.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (s *BentoSourceActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeCommand:
		return s.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (s *BentoSourceActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	if cmd, ok := msg.Payload.(string); ok {
		switch cmd {
		case "pause":
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			ctx.Logger().Info("Source paused")
		case "resume":
			s.mu.Lock()
			s.running = true
			s.mu.Unlock()
			ctx.Logger().Info("Source resumed")
		}
	}
	return nil
}

// emit 데이터 전송
func (s *BentoSourceActor) emit(ctx actor.ActorContext, data map[string]any) error {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()

	if !running {
		return nil
	}

	s.processedCount++

	msg := actor.Message{
		ID:        actor.GenerateID(),
		Type:      actor.MessageTypeData,
		Payload:   data,
		Sender:    ctx.Self(),
		Timestamp: time.Now(),
	}

	// 출력 Actor들에게 전송
	for _, output := range s.outputs {
		if ref, err := ctx.System().Get(output); err == nil {
			_ = ref.Tell(msg)
		}
	}

	// 부모가 있으면 부모에게도 전송
	if parent := ctx.Parent(); parent != nil {
		_ = parent.Tell(msg)
	}

	return nil
}

// SetOutputs 출력 대상 설정
func (s *BentoSourceActor) SetOutputs(outputs []string) {
	s.outputs = outputs
}

// GetStats 통계 조회
func (s *BentoSourceActor) GetStats() map[string]int64 {
	return map[string]int64{
		"processed": s.processedCount,
		"errors":    s.errorCount,
	}
}
