# Bulk / Realtime 파이프라인 비교 — Conduix vs Spark·Flink·Debezium 등

> 이 문서는 **코드에서 확인된 사실**만 담는다(추측 금지). 근거는 `pipeline-core/pkg/executor/group_executor.go`, `pkg/source/cdc*.go`, `pkg/output/sql.go`, `pkg/stream/*.go`, `pipeline-worker/internal/agent/agent.go`.
> 관련: [COMPARISON.md](COMPARISON.md) · [cdc-roadmap.md](plans/cdc-roadmap.md) · [ADR-0004](adr/0004-cdc-safety.md)

## 결론부터 (TL;DR)

- **Bulk**: Conduix는 **파이프라인당 단일 프로세스 배치 엔진**이다. 파티션 병렬(partitioned source)·스테이지 병렬은 **존재하고 잘 동작**하지만, 그 병렬성은 **한 프로세스 안의 goroutine 병렬**이지 여러 노드로 데이터를 샤딩하는 분산 실행이 아니다(설계상 server-local). "여러 저장소 간 이동 + 소스별 병렬 읽기 + 스테이지 병렬 변환 + 소스별 서로 다른 싱크 적재"에 강하다. **파티션 경계를 넘는 전역 GROUP BY/JOIN(= 분산 셔플: 같은 key 를 한 곳에 모으려 여러 노드로 데이터 재분배)** 은 Conduix 에 없다 — 이건 Spark/Flink 가 맞다. (단일 노드 spill-to-disk 는 원리상 Conduix 도 넣을 수 있는 개선이지 근본 한계가 아니다. §1.2 참고.)
- **Realtime**: **at-least-once**(정확히-한번 아님) + **upsert 수렴** 모델. CDC(MySQL·PostgreSQL)는 **Debezium 없이 단독 처리 가능**하도록 개선·검증됐다(과거 "Debezium 경유 권장" 근거는 대부분 해소). 다만 **초기 스냅샷·다중 컨슈머 fan-out·스키마 레지스트리·대규모 상태연산**은 여전히 Debezium/Flink가 낫다.
- **한 줄 기준**: *데이터가 노드 메모리에 들어가고, exactly-once 상태연산이 필수가 아니며, 커넥터+변환+오케스트레이션을 한 플랫폼에서 굴리고 싶다* → Conduix. 그 반대면 Spark/Flink/Debezium.

---

## Part 1. Bulk 파이프라인 — vs Spark / Flink(batch)

### 1.1 Conduix bulk가 실제로 하는 것 (코드 확인)

| 능력 | 상태 | 근거 |
|------|------|------|
| **스테이지 병렬 처리** | ✅ 워커 풀(세마포어), 파이프라인당 최대 100 | `group_executor.go` batch 경로(`workers` 세마포어) |
| **파티션 병렬 읽기** | ✅ Kafka partition / partitioned_sql / partitioned_http / CDC | `runMultiPartitionSource` (파티션별 goroutine → 단일 채널 merge) |
| **소스별 서로 다른 싱크 적재** | ✅ Output별 PreStages(한 소스 → ES엔 @timestamp·S3엔 원본·DB엔 마스킹) | PreStages 경로 |
| **bulk write** | ✅ SQL/ES/Kafka/S3/Mongo/REST batch write(bulk/individual 모드) | `output/*`, `BatchOutput.WriteBatch` |
| **스트리밍 읽기 + backpressure** | ✅ 소스→채널(버퍼 1000) 블로킹, 워커 세마포어 | 채널 버퍼 + 세마포어 |
| **소스 offset 재개** | ✅ Kafka/CDC/SQL offset 체크포인트 후 재시작 이어받기 | `checkpoint/client.go`, `LoadCheckpoints` |

### 1.2 Spark/Flink가 하고 Conduix가 **못 하는** 것 (정직하게)

| 항목 | Conduix | Spark/Flink | 영향 |
|------|---------|-------------|------|
| **분산 실행(데이터 샤딩)** | ❌ 단일 프로세스. K8s Job은 파이프라인 1개=Job 1개(데이터 안 나눔) | ✅ 클러스터가 데이터를 파티션으로 샤딩 | 노드 메모리 초과 데이터 처리 불가 |
| **분산 셔플(대용량 GROUP BY/JOIN)** | ❌ 없음 | ✅ 셔플 스테이지 | 넓은 조인·전역 집계는 OOM 위험 |
| **spill-to-disk** | ❌ 집계/조인 상태를 전량 메모리 보관(무제한) | ✅ 메모리 초과분 디스크로 | 대용량 상태 연산 시 OOM |
| **스테이지 레벨 체크포인트** | ❌ 소스 offset만 재개(중간 stage 상태 유실) | ✅ 세이브포인트/체크포인트 | 중간 실패 시 처음부터 재처리 |
| **Output 병렬** | ❌ 여러 Output은 순차 처리 | ✅ | 느린 싱크가 병목 |

> **핵심**: Conduix bulk의 처리 한계는 **노드 메모리**다. partitioned source 의 파티션들도 현재는 **한 프로세스의 goroutine 병렬**이지 여러 노드 분산이 아니다("distribution-model: 서버-로컬" 설계). 안전 가이드: 데이터셋이 노드 메모리에 들어오고, 셔플이 필요 없는 map-style 변환(필터/리맵/캐스트/소스별 라우팅) 중심이면 Conduix가 간결하고 빠르다.
>
> 파티션을 여러 노드로 분산 실행하는 스케일아웃은 **설계안 존재(미구현)**: [partition-distributed-execution.md](plans/partition-distributed-execution.md). 단 이는 소스 읽기·map·싱크의 fan-out 이지 분산 셔플이 아니다(대용량 GROUP BY/JOIN 은 여전히 Spark/Flink).

### 1.3 그럼에도 Conduix bulk를 고르는 이유

- **통합**: Spark 잡 + Airflow 스케줄 + 별도 커넥터를 각각 운영하는 대신 하나의 K8s-네이티브 플랫폼.
- **여러 저장소 이동이 주 목적일 때**(kafka→RDB→mongodb 같은 이동/적재): 분산 셔플이 필요 없는 이 워크로드는 Conduix의 스윗스팟. 소스별 병렬 읽기 + 소스별 서로 다른 싱크가 코드로 지원됨.
- **반복 속도**: 변환을 JS로 빌드 없이(저장→재시작) 수정. Spark는 잡 재빌드/재배포.
- **운영 복원력 내장**: 서킷브레이커·DLQ·retry+백오프·고아감지(`failure_guard.go`).

---

## Part 2. Realtime 파이프라인 — vs Debezium / Spark Streaming / Flink / Kafka Streams

### 2.1 전달 보장 (코드 확인)

- **CDC(MySQL/PostgreSQL)**: **처리 성공 기반 offset 커밋** = at-least-once. 레코드가 다운스트림으로 소비된 뒤 `committedPos`/`committedGTID`/`committedLSN`을 커밋한다(`cdc.go` Read 소비 루프, `cdc_postgres.go` standby update). 채널 포화 시 **drop 없이 blocking**(backpressure) → 유실 없음.
- **Kafka source**: kafka-go Reader의 자동 커밋(`CommitInterval`)이 **처리 성공과 비동기**다. 처리 실패해도 offset이 진행할 수 있어 **해당 소스는 at-most-once로 전락할 위험**이 있다(`kafka.go`). → CDC만큼의 유실보장은 아님.
- **exactly-once**: **없음**. 트랜잭션/2PC 없음. SQL 싱크의 upsert(`ON DUPLICATE KEY UPDATE`/`ON CONFLICT DO UPDATE`)는 *중복 감지 후 덮어쓰기*라, **CDC full-row + upsert 조합에서 타깃이 소스로 "수렴"**하는 것이지 정확히-한번은 아니다.

### 2.2 시간·상태 (코드 확인)

| 항목 | Conduix | Flink/Kafka Streams |
|------|---------|---------------------|
| **event-time vs processing-time** | 둘 다(윈도우 stage에 `timestamp_field` 지정 시 event-time, 미지정 시 processing-time) | ✅ event-time 성숙 |
| **watermark / late data** | 부분: watermark 진행 + late 감지 있으나 late는 로그만/즉시 집계(side-output 없음) | ✅ allowed lateness + side output |
| **상태 저장** | 윈도우 집계는 Redis 저장 있음(선택), stream join은 저장 없음 | ✅ RocksDB/changelog |
| **상태 자동 복구** | ❌ 저장은 하되 재시작 시 자동 로드 아님(수동) → 재구축됨 | ✅ 자동 |
| **상태 메모리 관리** | ❌ 집계/조인 버퍼 무제한(spill 없음) → 대용량 시 OOM | ✅ TTL/spill |

### 2.3 확장·처리량

- **단일 프로세스 서버-로컬**. 파티션 병렬 소비는 가능하나(`runMultiPartitionSource`), 한 파이프라인을 여러 노드로 rescale하지는 못한다. 확장은 "여러 파이프라인을 여러 agent/cluster에 분산" 수준.
- **중복 실행 방지(HA)**: execution claim(Redis SETNX)을 **CDC 라이프사이클에 결합**. TTL 갱신 루프가 돌고, claim을 잃으면(다른 에이전트 인수) 실행 ctx를 취소해 CDC를 멈추고, 새 소유자가 체크포인트부터 재개한다(`agent.go` `renewClaim`/`claimRenewalLoop`). → 같은 binlog/slot 이중 소비 방지.

### 2.4 "예전엔 Debezium→Kafka→Conduix가 낫다"고 한 근거 — 지금 재평가

과거 판단의 근거들이 이번 CDC 개선(cdc-roadmap #1~#6, 실 MySQL/PostgreSQL e2e 검증 완료)으로 어떻게 바뀌었나:

| 과거 근거 | 현재 | 근거(코드) |
|-----------|------|-----------|
| backpressure에서 이벤트 drop | ✅ 해소 — blocking으로 유실 없음 | `cdc.go` OnRow 블로킹 전송 |
| offset이 처리 성공과 무관 | ✅ 해소 — 처리 성공 후 커밋 | `cdc.go` Read 소비 루프 |
| 재연결 없음(끊기면 종료) | ✅ 해소 — 지수 백오프 재연결, committedPos/GTID/LSN부터 재개 | `runWithReconnect` |
| **하드 DELETE가 타깃에 반영 안 됨** | ✅ 해소 — `_cdc_type=delete` → PK 기준 DELETE | `output/sql.go` `partitionCDC`/`batchDelete` |
| **PostgreSQL CDC 미지원** | ✅ 해소 — pglogrepl 논리복제 | `cdc_postgres.go` |
| 소스 HA(이중 실행) | ✅ 해소 — claim 갱신/상실 시 정지 | `agent.go` claim 루프 |
| 바이너리 컬럼 손상 | ✅ 해소 — TYPE_BINARY는 []byte 보존 | `cdc.go` `rowToMap` |

**여전히 Debezium/Kafka가 나은 경우**(해소 안 된 잔여):

- **초기 스냅샷**: CDC는 "지금부터의 변경분"만 준다. 기존 데이터 전량 적재는 **Conduix bulk 파이프라인으로 초기 적재 후 `start_position`/`start_lsn`으로 CDC 이어받기**(경계 중복은 upsert가 흡수). Debezium은 이걸 한 컴포넌트에서(snapshot→streaming) 자동 전환한다.
- **다중 컨슈머 fan-out**: 한 변경 스트림을 여러 독립 소비자가 각자 offset으로 재생하려면 Kafka가 자연스럽다. Conduix CDC 소스는 파이프라인당 1개 리더(중복 실행 방지). fan-out은 워크플로우 링크(부모→Kafka→자식)로 우회.
- **스키마 레지스트리 / 스키마 진화 관리**: Conduix는 DDL 이벤트를 흘리지만(`_cdc_type=ddl`) Confluent Schema Registry 같은 중앙 스키마 관리는 없다.
- **대규모 상태 스트림 연산 / exactly-once**: 정확히-한번 상태 저장이 핵심이면 Flink.

### 2.5 다른 스트리밍 도구와의 위치

| 축 | **Conduix** | Debezium | Spark Streaming | Flink | Kafka Streams |
|----|-------------|----------|-----------------|-------|---------------|
| 주 역할 | CDC/스트림 캡처+변환+적재 통합 | CDC 캡처(→Kafka) | 마이크로배치 스트림 | 진짜 스트림 상태연산 | Kafka 내 스트림 |
| CDC 캡처 | ✅ MySQL·PostgreSQL 내장 | ✅ 다수 DB(성숙) | ❌(별도 소스 필요) | ❌ | ❌ |
| 전달 보장 | at-least-once(+upsert 수렴) | at-least-once(→Kafka) | exactly-once(옵션) | exactly-once | exactly-once |
| event-time/watermark | 부분 | N/A | ✅ | ✅(성숙) | ✅ |
| 상태 자동복구 | ❌(수동) | N/A | ✅ | ✅ | ✅ |
| 초기 스냅샷 | ❌(bulk로 대체) | ✅ | 소스 의존 | 소스 의존 | ❌ |
| 분산 확장 | ❌(서버-로컬) | 스케일 제한 | ✅ | ✅ | ✅(파티션) |
| Kafka 필요 | ❌(단독 가능) | ✅ | 소스 의존 | 선택 | ✅(필수) |
| 운영 부담 | 낮음(단일 플랫폼) | 중(Connect+Kafka) | 높음 | 높음 | 중 |

---

## Part 3. 선택 가이드 (실무)

**Conduix로 충분 / 유리:**
- kafka → RDB → mongodb 등 **여러 저장소 간 이동·적재**(분산 셔플 불필요, map-style 변환).
- MySQL/PostgreSQL **CDC를 Debezium/Kafka 없이** 단독으로: insert/update/delete 반영, upsert 수렴, 재연결, HA까지 필요할 때.
- 소스 하나를 **여러 싱크에 서로 다른 형태**로 적재.
- 운영자 GUI + 엔지니어 YAML/API를 같은 모델로, 서킷브레이커·DLQ 등 복원력 내장이 필요할 때.

**다른 도구가 맞음:**
- **노드 메모리를 넘는 대용량 배치**, 분산 셔플/대용량 조인/전역 집계 → **Spark/Flink**.
- **exactly-once 상태 스트림 연산**(대규모 이벤트타임 윈도우, 정확한 상태 저장·복구) → **Flink/Kafka Streams**.
- **CDC 초기 스냅샷 자동화 + 다중 컨슈머 fan-out + 스키마 레지스트리**가 핵심인 CDC 허브 → **Debezium + Kafka**.
- 데이터 외 **범용 태스크 오케스트레이션** → **Airflow**.

**하이브리드(권장 패턴):**
- 초기 전량 적재는 Conduix **bulk**, 이후 변경분은 Conduix **CDC realtime**(`start_position`/`start_lsn`로 경계 맞춤) → Debezium 없이 스냅샷+스트리밍 근사.
- 한 변경 스트림을 여러 팀이 소비해야 하면, Conduix CDC → Kafka 싱크 → 각 팀이 Kafka에서 소비(이때만 Kafka 도입).
