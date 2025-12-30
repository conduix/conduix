package types

import (
	"context"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
)

// SourceActor 소스 Actor
type SourceActor struct {
	*actor.BaseActor
	sourceType string
	config     map[string]any
	outputs    []string
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewSourceActor 새 SourceActor 생성
func NewSourceActor(name string, config map[string]any) *SourceActor {
	sourceType := "unknown"
	if st, ok := config["source_type"].(string); ok {
		sourceType = st
	}

	return &SourceActor{
		BaseActor:  actor.NewBaseActor(name, config),
		sourceType: sourceType,
		config:     config,
	}
}

// PreStart 시작 전 초기화
func (s *SourceActor) PreStart(ctx actor.ActorContext) error {
	if err := s.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	// 소스 타입에 따른 초기화
	switch s.sourceType {
	case "kafka":
		go s.runKafkaSource(ctx)
	case "http_server":
		go s.runHTTPSource(ctx)
	case "file":
		go s.runFileSource(ctx)
	case "demo":
		go s.runDemoSource(ctx)
	}

	ctx.Logger().Info("Source actor started", "type", s.sourceType)
	return nil
}

// PostStop 종료 후 정리
func (s *SourceActor) PostStop(ctx actor.ActorContext) error {
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
	ctx.Logger().Info("Source actor stopped", "type", s.sourceType)
	return s.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (s *SourceActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeCommand:
		return s.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (s *SourceActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	if cmd, ok := msg.Payload.(string); ok {
		switch cmd {
		case "pause":
			s.running = false
			ctx.Logger().Info("Source paused")
		case "resume":
			s.running = true
			ctx.Logger().Info("Source resumed")
		}
	}
	return nil
}

// runKafkaSource Kafka 소스 실행
func (s *SourceActor) runKafkaSource(ctx actor.ActorContext) {
	// TODO: 실제 Kafka 클라이언트 연동
	ctx.Logger().Info("Kafka source started",
		"brokers", s.config["brokers"],
		"topics", s.config["topics"])

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !s.running {
				continue
			}

			// 데모용 데이터 생성
			data := map[string]any{
				"message":   "Demo kafka message",
				"timestamp": time.Now().UnixMilli(),
				"source":    "kafka",
			}

			s.emit(ctx, data)
		}
	}
}

// runHTTPSource HTTP 소스 실행
func (s *SourceActor) runHTTPSource(ctx actor.ActorContext) {
	// TODO: 실제 HTTP 서버 구현
	ctx.Logger().Info("HTTP source started",
		"address", s.config["address"],
		"path", s.config["path"])
}

// runFileSource 파일 소스 실행
func (s *SourceActor) runFileSource(ctx actor.ActorContext) {
	// TODO: 실제 파일 읽기 구현
	ctx.Logger().Info("File source started",
		"path", s.config["path"])
}

// runDemoSource 데모 소스 실행
func (s *SourceActor) runDemoSource(ctx actor.ActorContext) {
	ctx.Logger().Info("Demo source started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	counter := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !s.running {
				continue
			}

			counter++
			data := map[string]any{
				"id":        counter,
				"message":   "Demo event",
				"timestamp": time.Now().Format(time.RFC3339),
				"level":     []string{"info", "warn", "error"}[counter%3],
			}

			s.emit(ctx, data)
		}
	}
}

// emit 데이터 전송
func (s *SourceActor) emit(ctx actor.ActorContext, data map[string]any) {
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
}
