//go:build kafkaintegration

// R4a executor 배선 검증: GroupExecutor(kafka 소스 + stub 싱크) 를 실제로 돌려,
// sink flush 후에만 offset 이 ack/커밋되는 at-least-once 를 파이프라인 레벨에서 확인.
package cdce2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/shared/types"
)

func kafkaBroker() string {
	if v := os.Getenv("KAFKA_BROKER"); v != "" {
		return v
	}
	return "localhost:19092"
}

func createTopicPL(t *testing.T, topic string) {
	t.Helper()
	conn, err := kafka.Dial("tcp", kafkaBroker())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if parts, err := conn.ReadPartitions(topic); err == nil && len(parts) > 0 {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("topic %s not ready", topic)
}

func producePL(t *testing.T, topic string, n int) {
	t.Helper()
	w := &kafka.Writer{Addr: kafka.TCP(kafkaBroker()), Topic: topic, AllowAutoTopicCreation: true}
	defer w.Close()
	msgs := make([]kafka.Message, n)
	for i := 0; i < n; i++ {
		msgs[i] = kafka.Message{Value: []byte(fmt.Sprintf(`{"id":%d}`, i+1))}
	}
	for attempt := 0; attempt < 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := w.WriteMessages(ctx, msgs...)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("produce failed")
}

func kafkaPipeline(topic, group string) *types.PipelineGroup {
	return &types.PipelineGroup{
		ID:            "kafka-alo-" + group,
		Name:          "kafka-alo",
		ExecutionMode: types.ExecutionModeSequential,
		Pipelines: []types.GroupedPipeline{
			{
				ID:   "p1",
				Name: "consume",
				Input: types.WorkflowInput{
					Type: "kafka",
					Name: "src",
					Config: map[string]any{
						"brokers":      []any{kafkaBroker()},
						"topics":       []any{topic},
						"group_id":     group,
						"start_offset": "earliest",
					},
				},
				Outputs: []types.Output{
					{Name: "sink", Type: "stub", Config: map[string]any{}},
				},
			},
		},
	}
}

// 파이프라인을 잠깐 돌려 일부 소비 후 취소 → 재시작 시 유실 없이 이어받는지.
func TestIntegration_KafkaPipeline_AtLeastOnce(t *testing.T) {
	topic := fmt.Sprintf("plalo-%d", time.Now().UnixNano())
	group := "plg-" + topic
	createTopicPL(t, topic)
	producePL(t, topic, 5)

	// run: GroupExecutor 를 최대 maxWait 동안 돌리되 want 건 처리되면 조기 종료.
	// (kafka 소스는 스트림이라 채널이 안 닫힘 → ctx timeout 으로 멈춘다. 타이밍 의존 제거.)
	run := func(want int, maxWait time.Duration) int64 {
		e := executor.NewGroupExecutor(kafkaPipeline(topic, group))
		ctx, cancel := context.WithTimeout(context.Background(), maxWait)
		defer cancel()
		if _, err := e.Start(ctx, "test"); err != nil {
			t.Fatalf("start: %v", err)
		}
		deadline := time.Now().Add(maxWait)
		for time.Now().Before(deadline) {
			if ex := e.Execution(); ex != nil && ex.TotalRecords >= int64(want) {
				break
			}
			if e.Status() != types.PipelineGroupStatusRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
		// 종료·flush·ack 반영 대기.
		for i := 0; i < 40 && e.Status() == types.PipelineGroupStatusRunning; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if ex := e.Execution(); ex != nil {
			return ex.TotalRecords
		}
		return 0
	}

	// 1차: 원본 5건 전량 처리. flushAndAck 가 offset 을 커밋해야 함.
	first := run(5, 15*time.Second)
	t.Logf("1st run processed=%d", first)
	if first < 5 {
		t.Fatalf("1st run: 원본 5건 처리 기대, got %d", first)
	}

	// group rebalance 여유 후 새 메시지 5건 추가.
	time.Sleep(5 * time.Second)
	producePL(t, topic, 5)

	// 2차: 1차 offset 이 커밋됐으면(at-least-once) 새 5건이 온다.
	second := run(5, 20*time.Second)
	t.Logf("2nd run processed=%d", second)

	// 유실 없음: 1차+2차 합계가 최소 10(원본5+추가5). 중복(재처리)은 허용이므로 >= 10.
	if first+second < 10 {
		t.Fatalf("at-least-once 위반(유실): 1차+2차=%d, 최소 10 기대(원본5+추가5)", first+second)
	}
}
