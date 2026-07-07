# 설계: 진짜 at-least-once — sink flush 기반 offset ack

> 상태: 설계+구현 진행. 목적: 소스가 "records 채널 전송" 이 아니라 **"sink 적재(flush) 성공"** 시점에 offset 을 커밋하도록 바꿔, 크래시 시 유실 창을 없앤다.
> 실 Kafka 검증 중 트레이스로 확인된 결함에서 출발(아래 §1).

## 1. 결함 (실 Kafka 트레이스로 확정)

소스는 `records := make(chan Record, 100)`(버퍼 100) 에 레코드를 넣는 **즉시** offset 을 커밋한다(`kafka.go`, `cdc.go`). 버퍼 채널이라 다운스트림(executor→stage→sink)이 실제 처리하기 전에도 채널이 찰 때까지 계속 넣고 커밋한다.

KDEBUG 트레이스(2건만 소비했는데 5건 다 커밋):
```
got 1 → commit off=0
got 2 → commit off=1
(소비자 멈춤)  ← 소스는 계속: fetch/commit off=2,3,4
```

**의미**: 소스가 커밋한 뒤 파이프라인이 죽으면, **버퍼에 있거나 stage 처리 중이던 레코드는 커밋됐지만 미적재 = 유실**. CDC·Kafka 공통. 즉 현재는 진짜 at-least-once 가 아니라 유실 창이 있다.

## 2. 목표

- **at-least-once**: sink 에 적재(flush)된 것만 커밋. 크래시 시 미적재분은 재시작 후 재처리(유실 0).
- **성능(대량 동시처리)**: per-record 왕복 없이 **배치 단위 ack + 워터마크(연속 최댓값) 커밋**. 처리량 저하 최소.
- **하위호환**: ack 를 구현 안 한 소스는 기존 동작(현행 유지). optional 인터페이스.

## 3. 설계: 배치 ack + 워터마크 커밋

### 3.1 흐름
```
Source: Record 에 offset 토큰 부착(파티션키 + offset) → 채널 전송(커밋 안 함)
Executor: stage 처리 → sink.Write → 배치 경계에서 sink.Flush 성공
          → 그 배치의 (파티션키별) 최대 offset 을 source.Ack(offsets) 호출
Source: Ack 수신 → 파티션별로 "연속 확인된 최댓값(워터마크)" 까지 committed 갱신
        → checkpoint 는 committed(=적재 확인된 지점)만 저장
```

- **워터마크**: 배치 처리가 순서를 보존하므로(한 파티션은 순차) ack 된 offset 의 연속 최댓값까지 커밋. gap 이 생기면(부분 실패) 그 앞까지만 → 유실 없음, 재처리는 gap 이후.
- **배치 ack**: sink flush 는 이미 배치 단위(batch.size / BatchingWrapper). flush 성공 1회당 ack 1회 → per-record 오버헤드 없음.

### 3.2 인터페이스 (source.go)
```go
// Record.Metadata 에 offset 은 이미 있음(Kafka). CDC 는 Record 에 offset 을 실어야 함(현재 전역 committedPos 만).
type AckableSource interface {
    CheckpointableSource
    // Ack 는 다운스트림이 sink 적재까지 성공한 레코드들의 offset 을 소스에 알린다.
    // 소스는 파티션별 연속 최댓값까지 committed 를 전진시킨다. 배치 단위 호출.
    Ack(offsets []RecordOffset)
}

type RecordOffset struct {
    PartitionKey string // 파티션 식별(kafka: topic-partition, cdc: db)
    Offset       string // 소스가 해석하는 offset(kafka: 숫자, cdc: binlog pos / gtid / lsn)
}
```
- Record 에 offset 을 싣는 방법: `Record.Metadata.Offset`(kafka 는 이미 "part:off") + `Metadata.PartitionKey`(신규) 또는 Metadata 에 소스가 채운 불투명 토큰. CDC 는 convertEventToRecord 에서 pos/gtid/lsn 을 Metadata 에 부착.

### 3.3 소스별 구현
- **Kafka**: 채널 전송 시 커밋 제거. `Ack` 에서 받은 offset 을 `CommitMessages`(또는 group offset commit)로 반영. 파티션별 워터마크.
- **CDC(MySQL)**: 채널 전송 시 `committedPos/GTID` 갱신 제거. Record 에 pos/gtid 부착. `Ack` 에서 워터마크까지 committedPos/GTID 전진. (기존 재연결·checkpoint 는 committed 기준이라 그대로 동작.)
- **CDC(PostgreSQL)**: 동일. committedLSN 을 Ack 기반으로 전진. standby update 는 committedLSN 사용(이미).

### 3.4 executor (group_executor.go)
- realtime: sink 전송 루프에서 **Flush 성공 후** 그 레코드의 offset 을 모아 `Ack`. (record 단위면 배치 flush 주기에 맞춰 모아서.)
- batch: `WriteBatch`+`Flush` 성공 후 그 배치 레코드들의 offset 을 `Ack`.
- 여러 output: **모든** output flush 성공해야 ack(하나라도 실패면 그 배치 미ack → 재처리).
- ack 대상 소스가 `AckableSource` 아니면 현행(기존 checkpoint 경로) 유지.

## 4. 구현 순서 (각 단계 테스트)

1. **인터페이스 + Record offset 부착**: `AckableSource`, `RecordOffset`, `Metadata.PartitionKey`. Kafka/CDC 가 Record 에 offset 부착.
2. **Kafka**: 채널전송-커밋 제거 → Ack 기반 커밋. 실 Kafka 로 "2건 소비 후 크래시 → 나머지 재처리, 유실 0" 검증(이번에 실패한 시나리오가 통과해야 함).
3. **CDC(MySQL/PostgreSQL)**: 채널전송-커밋 제거 → Ack 기반. 실 DB 로 유실 0 검증.
4. **executor**: flush 성공 후 Ack 배선(realtime/batch, 다중 output).
5. **회귀**: 기존 e2e(CDC/Kafka) 전부 통과 + 크래시-유실 시나리오 추가.

## 5. 리스크
- **성능**: 배치 ack 라 왕복은 flush 당 1회. 다만 flush 주기가 너무 길면 미ack 구간(재처리 후보)이 커짐 → batch.size/flush 주기로 조절.
- **순서 보존 전제**: 워터마크는 파티션 내 순서를 전제. 한 파티션을 여러 goroutine 이 재정렬하면 안 됨(현재 파티션=순차라 OK).
- **at-least-once 이지 exactly-once 아님**: ack 후 커밋 전 크래시면 재처리(중복) 가능 → 멱등 sink(upsert) 전제. exactly-once 는 비목표(Flink).
- **큰 리팩터**: 소스 3종 + executor 2경로 + 인터페이스. 단계별 테스트로 회귀 방지.
