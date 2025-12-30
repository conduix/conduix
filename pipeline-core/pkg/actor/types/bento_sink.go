package types

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
	"github.com/conduix/conduix/pipeline-core/pkg/adapter/bento"
)

// BentoSinkActor Bento 기반 싱크 Actor
type BentoSinkActor struct {
	*actor.BaseActor
	sinkType       string
	config         map[string]any
	adapter        *bento.OutputAdapter
	configBuilder  *bento.ConfigBuilder
	buffer         []map[string]any
	bufferMu       sync.Mutex
	maxEvents      int
	flushTimeout   time.Duration
	processedCount int64
	errorCount     int64
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewBentoSinkActor 새 BentoSinkActor 생성
func NewBentoSinkActor(name string, config map[string]any) *BentoSinkActor {
	sinkType := "stdout"
	if st, ok := config["sink_type"].(string); ok {
		sinkType = st
	}
	// type 필드도 지원
	if st, ok := config["type"].(string); ok {
		sinkType = st
	}

	maxEvents := 5000
	if buf, ok := config["buffer"].(map[string]any); ok {
		if me, ok := buf["max_events"].(int); ok {
			maxEvents = me
		}
	}

	flushTimeout := 10 * time.Second
	if buf, ok := config["buffer"].(map[string]any); ok {
		if to, ok := buf["timeout"].(string); ok {
			if d, err := time.ParseDuration(to); err == nil {
				flushTimeout = d
			}
		}
	}

	return &BentoSinkActor{
		BaseActor:     actor.NewBaseActor(name, config),
		sinkType:      sinkType,
		config:        config,
		buffer:        make([]map[string]any, 0, maxEvents),
		maxEvents:     maxEvents,
		flushTimeout:  flushTimeout,
		configBuilder: bento.NewConfigBuilder(),
	}
}

// PreStart 시작 전 초기화
func (s *BentoSinkActor) PreStart(ctx actor.ActorContext) error {
	if err := s.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	// Bento output 생성
	bentoType := s.mapToBentoOutput()
	if bentoType != "" {
		output, err := s.configBuilder.BuildOutput(bentoType, s.config)
		if err != nil {
			ctx.Logger().Warn("Failed to create Bento output, using fallback",
				"type", s.sinkType, "error", err)
		} else {
			s.adapter = bento.NewOutputAdapter(output)
		}
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())

	// 주기적 플러시 시작
	go s.flushLoop(ctx)

	ctx.Logger().Info("Bento sink actor started", "type", s.sinkType)
	return nil
}

// mapToBentoOutput 우리 설정을 Bento output 타입으로 매핑
func (s *BentoSinkActor) mapToBentoOutput() string {
	switch s.sinkType {
	case "kafka":
		return "kafka"
	case "elasticsearch":
		return "elasticsearch"
	case "s3", "aws_s3":
		return "aws_s3"
	case "http", "http_client":
		return "http_client"
	case "file":
		return "file"
	case "console", "stdout":
		return "stdout"
	default:
		return ""
	}
}

// PostStop 종료 후 정리
func (s *BentoSinkActor) PostStop(ctx actor.ActorContext) error {
	if s.cancel != nil {
		s.cancel()
	}

	// 버퍼에 남은 데이터 플러시
	s.flush(ctx)

	if s.adapter != nil {
		if err := s.adapter.Stop(); err != nil {
			ctx.Logger().Error("Failed to stop adapter", "error", err)
		}
	}

	ctx.Logger().Info("Bento sink actor stopped",
		"type", s.sinkType,
		"processed", s.processedCount,
		"errors", s.errorCount)

	return s.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (s *BentoSinkActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeData:
		return s.handleData(ctx, msg)
	case actor.MessageTypeCommand:
		return s.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (s *BentoSinkActor) handleData(ctx actor.ActorContext, msg actor.Message) error {
	data, ok := msg.Payload.(map[string]any)
	if !ok {
		s.errorCount++
		return fmt.Errorf("invalid payload type: %T", msg.Payload)
	}

	s.bufferMu.Lock()
	s.buffer = append(s.buffer, data)
	shouldFlush := len(s.buffer) >= s.maxEvents
	s.bufferMu.Unlock()

	if shouldFlush {
		s.flush(ctx)
	}

	return nil
}

func (s *BentoSinkActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	if cmd, ok := msg.Payload.(string); ok {
		switch cmd {
		case "flush":
			s.flush(ctx)
		}
	}
	return nil
}

// flushLoop 주기적 플러시
func (s *BentoSinkActor) flushLoop(ctx actor.ActorContext) {
	ticker := time.NewTicker(s.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.flush(ctx)
		}
	}
}

// flush 버퍼 플러시
func (s *BentoSinkActor) flush(ctx actor.ActorContext) {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return
	}

	data := s.buffer
	s.buffer = make([]map[string]any, 0, s.maxEvents)
	s.bufferMu.Unlock()

	var err error

	// Bento adapter가 있으면 사용
	if s.adapter != nil {
		err = s.adapter.WriteBatch(context.Background(), data)
	} else {
		// 폴백: 내장 처리
		err = s.builtinWrite(ctx, data)
	}

	if err != nil {
		s.errorCount += int64(len(data))
		ctx.Logger().Error("Sink flush failed", "error", err)
	} else {
		s.processedCount += int64(len(data))
	}
}

// builtinWrite 내장 쓰기 로직 (폴백)
func (s *BentoSinkActor) builtinWrite(ctx actor.ActorContext, data []map[string]any) error {
	switch s.sinkType {
	case "console", "stdout":
		return s.writeConsole(data)
	default:
		// 지원하지 않는 타입은 콘솔에 출력
		return s.writeConsole(data)
	}
}

// writeConsole 콘솔 출력
func (s *BentoSinkActor) writeConsole(data []map[string]any) error {
	for _, d := range data {
		jsonData, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(jsonData))
	}
	return nil
}

// GetStats 통계 조회
func (s *BentoSinkActor) GetStats() map[string]int64 {
	return map[string]int64{
		"processed":   s.processedCount,
		"errors":      s.errorCount,
		"buffer_size": int64(len(s.buffer)),
	}
}
