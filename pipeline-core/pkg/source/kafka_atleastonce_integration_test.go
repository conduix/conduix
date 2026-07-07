//go:build kafkaintegration

// 실 Kafka 대상 at-least-once 커밋 검증(R4a).
// 실행 전 Kafka 기동 필요: docker run apache/kafka:3.7.0 ... (advertised localhost:19092)
//
//	go test -tags kafkaintegration ./pkg/source/ -run TestIntegration_Kafka -v
package source

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func kafkaBroker() string {
	if v := os.Getenv("KAFKA_BROKER"); v != "" {
		return v
	}
	return "localhost:19092"
}

func produce(t *testing.T, topic string, msgs []string) {
	t.Helper()
	w := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBroker()),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true, // metadata 전파 전이라도 자동 생성으로 방어
	}
	defer w.Close()
	km := make([]kafka.Message, len(msgs))
	for i, m := range msgs {
		km[i] = kafka.Message{Value: []byte(m)}
	}
	// metadata 전파 지연으로 "Unknown Topic" 이 날 수 있어 재시도.
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		lastErr = w.WriteMessages(ctx, km...)
		cancel()
		if lastErr == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("produce: %v", lastErr)
}

func createTopic(t *testing.T, topic string) {
	t.Helper()
	conn, err := kafka.Dial("tcp", kafkaBroker())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	// CreateTopics 는 비동기 — 토픽이 실제로 조회될 때까지 대기(전파 전 produce 하면 Unknown Topic).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		parts, err := conn.ReadPartitions(topic)
		if err == nil && len(parts) > 0 {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("topic %s not ready in time", topic)
}

func kafkaCfg(topic, group string) config.SourceV2 {
	return config.SourceV2{
		Type:        "kafka",
		Brokers:     []string{kafkaBroker()},
		Topics:      []string{topic},
		GroupID:     group,
		StartOffset: "earliest",
	}
}

// 소비 성공 → 커밋됨 → 재시작 시 다시 안 옴(중복 없음, 정상 진행).
func TestIntegration_KafkaAtLeastOnce_CommitsAfterConsume(t *testing.T) {
	topic := fmt.Sprintf("alo-commit-%d", time.Now().UnixNano())
	group := "grp-" + topic
	createTopic(t, topic)
	produce(t, topic, []string{`{"id":1}`, `{"id":2}`, `{"id":3}`})

	// 1차: 3건 모두 소비(→ 커밋).
	got := consumeN(t, kafkaCfg(topic, group), 3, 15*time.Second)
	if len(got) != 3 {
		t.Fatalf("1st run: expected 3, got %d", len(got))
	}

	// 2차: 같은 group 재시작 → 이미 커밋됐으므로 새 메시지 없음. 새로 1건 produce 하면 그것만.
	produce(t, topic, []string{`{"id":4}`})
	got2 := consumeN(t, kafkaCfg(topic, group), 1, 10*time.Second)
	if len(got2) != 1 {
		t.Fatalf("2nd run: expected only new msg (1), got %d — 커밋이 안 됐거나 중복", len(got2))
	}
	if fmt.Sprint(got2[0].Data["id"]) != "4" {
		t.Errorf("2nd run: got id=%v, want 4 (이전 메시지가 재전송됨=커밋 실패)", got2[0].Data["id"])
	}
}

// 부분 소비 후 크래시 재현 → 재시작 시 "소비 못 한 나머지"가 유실 없이 다시 온다(at-least-once).
// 5건 중 앞 2건만 소비하고 죽은 뒤, 재시작하면 나머지 3건(경계 중복 허용)이 와야 한다.
func TestIntegration_KafkaAtLeastOnce_PartialConsumeResumes(t *testing.T) {
	topic := fmt.Sprintf("alo-partial-%d", time.Now().UnixNano())
	group := "grp-" + topic
	createTopic(t, topic)
	produce(t, topic, []string{`{"id":1}`, `{"id":2}`, `{"id":3}`, `{"id":4}`, `{"id":5}`})

	// 1차: 앞 2건만 소비하고 종료(크래시 시뮬레이션). 소비한 2건은 커밋됨.
	first := consumeN(t, kafkaCfg(topic, group), 2, 15*time.Second)
	if len(first) < 2 {
		t.Fatalf("1st run: expected >=2, got %d", len(first))
	}

	// 1차 group member 의 session 이 만료되어 rebalance 가 끝나길 기다린다(즉시 2차 시작하면
	// 파티션 배정을 못 받아 fetch 가 빈다 — 소스 버그가 아니라 group 조정 지연).
	time.Sleep(5 * time.Second)

	// 2차: 재시작 → 소비/커밋 안 된 나머지가 유실 없이 온다. 최소 3건(3,4,5).
	// at-least-once 이므로 경계에서 2번이 다시 올 수도 있음(중복 허용). 유실만 없으면 된다.
	second := consumeN(t, kafkaCfg(topic, group), 3, 20*time.Second)
	if len(second) < 3 {
		t.Fatalf("2nd run: expected >=3 (나머지 재개, 유실 없음), got %d", len(second))
	}

	// 두 run 을 합치면 5건 전부(1..5) 가 최소 한 번씩 나와야 한다(유실 없음).
	seen := map[string]bool{}
	for _, r := range append(first, second...) {
		seen[fmt.Sprint(r.Data["id"])] = true
	}
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		if !seen[id] {
			t.Errorf("id=%s 유실됨 (at-least-once 위반)", id)
		}
	}
}

// consumeN 은 소스에서 n건을 소비(records 수신)하고 반환. 수신 = 처리 성공 → 커밋 트리거.
func consumeN(t *testing.T, cfg config.SourceV2, n int, timeout time.Duration) []Record {
	t.Helper()
	src, err := NewKafkaSource(cfg)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if err := src.Open(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records, errs := src.Read(ctx)

	var got []Record
	deadline := time.After(timeout)
	for len(got) < n {
		select {
		case r, ok := <-records:
			if !ok {
				return got
			}
			got = append(got, r)
			// executor 가 sink flush 성공 후 Ack 하는 것을 여기서 시뮬레이션(레코드 1건 처리 성공).
			src.Ack([]RecordOffset{{PartitionKey: r.Metadata.PartitionKey, Offset: r.Metadata.Offset}})
		case e := <-errs:
			if e != nil {
				t.Logf("kafka error: %v", e)
			}
		case <-deadline:
			return got
		}
	}
	// 마지막 커밋이 반영될 시간을 잠깐 준다.
	time.Sleep(500 * time.Millisecond)
	return got
}
