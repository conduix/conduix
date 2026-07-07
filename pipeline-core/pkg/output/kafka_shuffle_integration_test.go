//go:build kafkaintegration

// 분산 셔플의 핵심 불변식 검증: partitioning=key 면 "같은 key = 같은 파티션".
// 이게 성립해야 "파이프라인1(key 파티셔닝 Kafka sink) → Kafka → 파이프라인2(파티션 병렬 소비 +
// 파티션-로컬 집계)" 워크플로우로 분산 GROUP BY/JOIN 을 사용법으로 구성할 수 있다.
package output

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

func kbroker() string {
	if v := os.Getenv("KAFKA_BROKER"); v != "" {
		return v
	}
	return "localhost:19092"
}

func TestIntegration_KafkaSink_KeyPartitioning(t *testing.T) {
	topic := fmt.Sprintf("shuffle-%d", time.Now().UnixNano())

	// 3 파티션 토픽 생성 + 전파 대기.
	conn, err := kafka.Dial("tcp", kbroker())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1}); err != nil {
		conn.Close()
		t.Fatalf("create topic: %v", err)
	}
	conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, _ := kafka.Dial("tcp", kbroker())
		parts, err := c.ReadPartitions(topic)
		c.Close()
		if err == nil && len(parts) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("topic not ready")
		}
		time.Sleep(300 * time.Millisecond)
	}

	// key 파티셔닝 sink.
	sink, err := NewKafkaOutput(config.OutputConfig{
		Brokers:      []string{kbroker()},
		Topic:        topic,
		Partitioning: "key",
		KeyField:     "customer_id",
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := sink.Open(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sink.Close()

	// 같은 customer_id 를 여러 번(값 다르게) 쓴다. 셔플이 맞으면 같은 customer_id 는 한 파티션에만.
	keys := []string{"cust-A", "cust-B", "cust-C", "cust-D", "cust-E"}
	for round := 0; round < 4; round++ {
		for _, k := range keys {
			rec := source.Record{Data: map[string]any{"customer_id": k, "amount": round}}
			if err := sink.Write(context.Background(), rec); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// 각 파티션을 읽어 key→partition 매핑 수집. 같은 key 가 여러 파티션에 나타나면 셔플 위반.
	keyToPartition := map[string]int{}
	total := 0
	for p := 0; p < 3; p++ {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:   []string{kbroker()},
			Topic:     topic,
			Partition: p,
			MinBytes:  1, MaxBytes: 10e6,
		})
		if err := r.SetOffset(kafka.FirstOffset); err != nil {
			r.Close()
			t.Fatalf("set offset: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		for {
			m, err := r.ReadMessage(ctx)
			if err != nil {
				break // 타임아웃 = 이 파티션 소진
			}
			total++
			key := string(m.Key)
			if prev, ok := keyToPartition[key]; ok && prev != p {
				cancel()
				r.Close()
				t.Fatalf("shuffle 위반: key %q 가 파티션 %d 와 %d 양쪽에 존재", key, prev, p)
			}
			keyToPartition[key] = p
		}
		cancel()
		r.Close()
	}

	if total != len(keys)*4 {
		t.Errorf("메시지 수 = %d, want %d", total, len(keys)*4)
	}
	if len(keyToPartition) != len(keys) {
		t.Errorf("고유 key 수 = %d, want %d", len(keyToPartition), len(keys))
	}
	// 파티션이 실제로 여러 개 쓰였는지(1개에 다 몰리지 않았는지) — 셔플이 분산 효과가 있는지.
	used := map[int]bool{}
	for _, p := range keyToPartition {
		used[p] = true
	}
	t.Logf("key→partition: %v (파티션 %d개 사용)", keyToPartition, len(used))
	if len(used) < 2 {
		t.Errorf("5개 key 가 파티션 %d개에만 분산됨 — 셔플 분산 효과 의심(Hash balancer 미동작?)", len(used))
	}
}
