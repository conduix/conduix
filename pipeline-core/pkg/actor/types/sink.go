package types

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
)

// SinkActor 싱크 Actor
type SinkActor struct {
	*actor.BaseActor
	sinkType       string
	config         map[string]any
	buffer         []map[string]any
	bufferMu       sync.Mutex
	maxEvents      int
	flushTimeout   time.Duration
	processedCount int64
	errorCount     int64
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewSinkActor 새 SinkActor 생성
func NewSinkActor(name string, config map[string]any) *SinkActor {
	sinkType := "console"
	if st, ok := config["sink_type"].(string); ok {
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

	return &SinkActor{
		BaseActor:    actor.NewBaseActor(name, config),
		sinkType:     sinkType,
		config:       config,
		buffer:       make([]map[string]any, 0, maxEvents),
		maxEvents:    maxEvents,
		flushTimeout: flushTimeout,
	}
}

// PreStart 시작 전 초기화
func (s *SinkActor) PreStart(ctx actor.ActorContext) error {
	if err := s.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())

	// 주기적 플러시 시작
	go s.flushLoop(ctx)

	ctx.Logger().Info("Sink actor started", "type", s.sinkType)
	return nil
}

// PostStop 종료 후 정리
func (s *SinkActor) PostStop(ctx actor.ActorContext) error {
	if s.cancel != nil {
		s.cancel()
	}

	// 버퍼에 남은 데이터 플러시
	s.flush(ctx)

	ctx.Logger().Info("Sink actor stopped",
		"type", s.sinkType,
		"processed", s.processedCount,
		"errors", s.errorCount)

	return s.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (s *SinkActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeData:
		return s.handleData(ctx, msg)
	case actor.MessageTypeCommand:
		return s.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (s *SinkActor) handleData(ctx actor.ActorContext, msg actor.Message) error {
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

func (s *SinkActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	if cmd, ok := msg.Payload.(string); ok {
		switch cmd {
		case "flush":
			s.flush(ctx)
		}
	}
	return nil
}

// flushLoop 주기적 플러시
func (s *SinkActor) flushLoop(ctx actor.ActorContext) {
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
func (s *SinkActor) flush(ctx actor.ActorContext) {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return
	}

	data := s.buffer
	s.buffer = make([]map[string]any, 0, s.maxEvents)
	s.bufferMu.Unlock()

	var err error
	switch s.sinkType {
	case "console":
		err = s.writeConsole(data)
	case "elasticsearch":
		err = s.writeElasticsearch(ctx, data)
	case "s3":
		err = s.writeS3(ctx, data)
	case "kafka":
		err = s.writeKafka(ctx, data)
	case "prometheus":
		err = s.writePrometheus(ctx, data)
	case "file":
		err = s.writeFile(ctx, data)
	default:
		err = s.writeConsole(data)
	}

	if err != nil {
		s.errorCount += int64(len(data))
		ctx.Logger().Error("Sink flush failed", "error", err)
	} else {
		s.processedCount += int64(len(data))
	}
}

// writeConsole 콘솔 출력
func (s *SinkActor) writeConsole(data []map[string]any) error {
	for _, d := range data {
		jsonData, _ := json.Marshal(d)
		fmt.Println(string(jsonData))
	}
	return nil
}

// writeElasticsearch Elasticsearch 출력
func (s *SinkActor) writeElasticsearch(ctx actor.ActorContext, data []map[string]any) error {
	endpoints, ok := s.config["endpoints"].([]any)
	if !ok || len(endpoints) == 0 {
		return fmt.Errorf("elasticsearch endpoints not configured")
	}

	endpoint := fmt.Sprintf("%v", endpoints[0])
	index := "logs"
	if idx, ok := s.config["index"].(string); ok {
		index = idx
	}

	// Bulk API 형식으로 변환
	var buf bytes.Buffer
	for _, d := range data {
		meta := map[string]any{"index": map[string]any{"_index": index}}
		metaJSON, _ := json.Marshal(meta)
		dataJSON, _ := json.Marshal(d)
		buf.Write(metaJSON)
		buf.WriteByte('\n')
		buf.Write(dataJSON)
		buf.WriteByte('\n')
	}

	// TODO: 실제 HTTP 요청
	ctx.Logger().Debug("Would send to ES",
		"endpoint", endpoint,
		"index", index,
		"count", len(data))

	return nil
}

// writeS3 S3 출력
func (s *SinkActor) writeS3(ctx actor.ActorContext, data []map[string]any) error {
	bucket, ok := s.config["bucket"].(string)
	if !ok {
		return fmt.Errorf("s3 bucket not configured")
	}

	prefix := ""
	if p, ok := s.config["prefix"].(string); ok {
		prefix = p
	}

	// TODO: 실제 S3 업로드
	ctx.Logger().Debug("Would upload to S3",
		"bucket", bucket,
		"prefix", prefix,
		"count", len(data))

	return nil
}

// writeKafka Kafka 출력
func (s *SinkActor) writeKafka(ctx actor.ActorContext, data []map[string]any) error {
	topic, ok := s.config["topic"].(string)
	if !ok {
		return fmt.Errorf("kafka topic not configured")
	}

	// TODO: 실제 Kafka 전송
	ctx.Logger().Debug("Would send to Kafka",
		"topic", topic,
		"count", len(data))

	return nil
}

// writePrometheus Prometheus 출력
func (s *SinkActor) writePrometheus(ctx actor.ActorContext, data []map[string]any) error {
	endpoint, ok := s.config["endpoint"].(string)
	if !ok {
		return fmt.Errorf("prometheus endpoint not configured")
	}

	// TODO: Prometheus 메트릭 형식으로 변환
	ctx.Logger().Debug("Would push to Prometheus",
		"endpoint", endpoint,
		"count", len(data))

	return nil
}

// writeFile 파일 출력
func (s *SinkActor) writeFile(ctx actor.ActorContext, data []map[string]any) error {
	path, ok := s.config["path"].(string)
	if !ok {
		return fmt.Errorf("file path not configured")
	}

	// TODO: 실제 파일 쓰기
	ctx.Logger().Debug("Would write to file",
		"path", path,
		"count", len(data))

	return nil
}

// GetStats 통계 조회
func (s *SinkActor) GetStats() map[string]int64 {
	return map[string]int64{
		"processed":   s.processedCount,
		"errors":      s.errorCount,
		"buffer_size": int64(len(s.buffer)),
	}
}

// HTTPSink HTTP 싱크 헬퍼
type HTTPSink struct {
	client *http.Client
}

func NewHTTPSink() *HTTPSink {
	return &HTTPSink{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}
