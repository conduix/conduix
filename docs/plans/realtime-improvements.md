# 작업계획: Realtime 파이프라인 개선 — Debezium/Flink 없이 Conduix 단독

> 목표: **Debezium/Kafka/Flink 를 복잡하게 혼합하지 않고 Conduix 하나로** realtime(CDC 포함) 요구를 충족.
> 이 문서는 [BULK_VS_REALTIME_COMPARISON.md](../BULK_VS_REALTIME_COMPARISON.md) §2.4 에서 "여전히 Debezium/Flink 가 낫다" 고 남긴 잔여 항목을 **실제로 없애는** 계획이다.
> 근거·완료 항목은 코드 file:line. 상태: [ ] 미착수 · [~] 진행중 · [x] 완료.
> 관련 완료 이력: [cdc-roadmap.md](cdc-roadmap.md)(#1~#6 + e2e 검증 완료), bulk 분산은 [partition-distributed-execution.md](partition-distributed-execution.md).

## 완료된 것 (이전 세션, 코드+e2e 검증)

CDC 안전성/정합성 기반은 이미 확보됨 — Debezium 경유가 필요했던 과거 근거는 대부분 해소:
- backpressure(drop 없음), 처리성공 기반 offset 커밋, GTID/LSN 체크포인트, 지수백오프 재연결
- INSERT/UPDATE/DELETE 반영(SQL 싱크 delete 포함), 바이너리 컬럼 보존
- PostgreSQL 논리복제 CDC(pglogrepl), CDC 소스 중복 실행 방지(claim 갱신/상실 정지)
- 실 MySQL/PostgreSQL e2e 검증(`test-cdc-integration` CI job)

## 진행 상태 (이번 goal)

- [~] **R1** 초기적재+CDC 동시실행 수렴
  - [x] sink position 버전 가드 upsert (`08fc757`, 실 MySQL/PostgreSQL e2e 통과)
  - [ ] CDC 레코드에 단조 position 부착
  - [ ] workflow 병렬 기동(bulk+CDC)
- [x] **R4a** Kafka at-least-once ack 커밋
  - [x] 소스 ack 로직 + 실 Kafka e2e (`15b8973`)
  - [x] executor flush→Ack 배선(realtime) + 파이프라인 레벨 실 Kafka e2e(GroupExecutor: 1차 5건 커밋 → 재시작 새 5건만, 유실 0)
- [x] **CDC 소스 ack 기반 커밋 전환** (seq 부착 + Ack 시 committed 전진, 유실 창 제거. 단위+race 검증, CDC e2e 회귀 통과)
- [ ] **R2** 다중 컨슈머 fan-out
- [ ] **R3** DDL 방어(안전 정지 + 샘플 validation)
- [ ] **R3b** JSON-only DDL 허용 (후순위)
- [ ] **R4** exactly-once/상태 복구 (Kafka at-most-once 제거는 R4a 로 일부, 상태 자동복구 남음)

## 남은 gap (이 계획의 대상)

BULK_VS_REALTIME_COMPARISON §2.4 의 잔여 4항목:

| # | gap | 현재 상태(코드) | 대체 목표 |
|---|-----|-----------------|-----------|
| R1 | **초기 적재+CDC 동시 실행 수렴** | 동시 실행 시 snapshot old 값이 CDC 최신값을 덮는 race 를 막을 sink 버전 가드 없음 | 초기적재·CDC **동시 시작** + position 단조 가드로 순서 무관 수렴(초기적재 대기 제거) |
| R2 | **다중 컨슈머 fan-out** | 부분 — fanout stage/pipeline link 존재하나, 한 소스 스트림을 "각자 offset 으로 독립 재생" 하는 소비자 다중화는 아님 | Kafka consumer group 별 독립 offset 대체 |
| R3 | **DDL 방어(안전 정지+validation)** | DDL 이벤트 발행(`_cdc_type=ddl`)만 있고, 정지·검증 게이트 없음 | 스키마 변경 시 자동 흡수 대신 **정합성 우선 정지 + 재개 게이트** |
| R4 | **effectively-once / 상태 복구** | at-least-once + upsert 수렴. Kafka 소스 자동커밋(at-most-once 위험), 윈도우/조인 상태 자동복구 없음 | 멱등+처리성공 커밋으로 재처리 안전, 상태 자동복구 |

---

## [~] R1. 초기 적재 + CDC **동시 실행** → position 버전 가드로 수렴 — 규모: 중, 우선순위: **최상**

**핵심(순차 아님)**: 초기적재와 CDC 를 **동시에 시작**한다. CDC upsert 는 수렴적이라, 초기적재가 끝날 때까지 CDC 를 미루지 않아도 CDC 가 offset 을 다 따라가면 최신 상태에 도달한다. 총 동기화 완료 시간 = **max(초기적재, CDC 따라잡기)** 이지 합이 아니다 → 초기적재 대기 시간만큼 단축. (순차로 "bulk 완료 후 그 position 부터 CDC" 하던 이전 설계는 폐기.)

**관심사 경계**: CDC 소스 = 변경분 캡처, bulk 파이프라인 = 전량 적재, sink = 수렴 규칙(버전 가드), workflow = 둘을 동시 기동. 한 레이어가 다른 레이어 일을 안 떠안는다.

**정합성 문제와 이론적 최선의 해법 — position 버전 가드**:
동시 실행의 유일한 위험은 **초기적재(snapshot)의 old 값이 CDC 의 최신 변경을 뒤늦게 덮는 race**(도착 순서 비결정적). 이를 도착 순서·타이밍과 **완전히 무관하게** 푸는 이론적으로 옳은 방법은 **단조증가 position 기반 "최신이 항상 이긴다" 불변식**이다:
- 모든 레코드에 소스 position 을 싣는다: CDC 는 이벤트의 binlog pos/GTID/LSN(단조증가), 초기적재 행은 **스냅샷을 캡처한 시점의 position**(그 이전 상태이므로 모든 CDC 이벤트보다 작거나 같음).
- sink upsert 는 **`incoming.position > existing.position` 일 때만 덮어쓴다**(조건부 upsert). position 컬럼을 타깃에 유지.
  - PostgreSQL: `ON CONFLICT (pk) DO UPDATE SET ... WHERE existing._pos < EXCLUDED._pos`
  - MySQL: `ON DUPLICATE KEY UPDATE col = IF(VALUES(_pos) > _pos, VALUES(col), col), _pos = GREATEST(_pos, VALUES(_pos))`
- 결과: snapshot 이 CDC 뒤에 도착해도 position 이 낮아 무시됨 → **순서 무관 수렴**. 삭제도 delete 이벤트의 position 이 이후 snapshot insert 보다 크면 유지(insert-if-absent 방식의 삭제 race 구멍 없음).

**왜 이게 최선인가**: insert-if-absent(snapshot 은 없을 때만 삽입)는 간단하지만 "snapshot 중 삭제된 행을 snapshot 이 뒤늦게 되살리는" race 에 취약하다. position 가드는 삭제 포함 모든 시나리오에서 단일 불변식으로 정합 → 일반적이고 견고.

**구현 대상**:
1. **sink 버전 가드 upsert**: SQL sink 에 `version_column`(예: `_pos`) 옵션 + 조건부 upsert(위 SQL). 타깃 테이블에 position 컬럼 필요.
2. **레코드에 position 부착**: CDC 이벤트는 pos/gtid/lsn → 정렬 가능한 단조 값으로 변환해 레코드 필드로. 초기적재(bulk SQL 소스)는 스냅샷 시점 position 을 상수로 부여.
3. **workflow 동시 기동**: 한 workflow 에 bulk + CDC 파이프라인을 **병렬**로. (순차 의존 불필요.)

**완료 기준(DoD)**:
- 초기적재와 CDC 를 동시 시작 → 타깃이 소스 최신 상태로 수렴(순서 무관).
- snapshot old 값이 CDC 최신값을 덮지 않음(position 가드).
- 초기적재 중 발생한 update/delete 가 정확히 반영.
- e2e: 초기 N행 + 동시에 M행 변경(update/delete 포함) → 타깃 = 소스 최종 상태, snapshot 지연 주입해도 동일.

> 단조 position 비교가 성립하려면 CDC pos/gtid/lsn 을 **정렬 가능한 단일 값**으로 정규화해야 한다(GTID set 은 단순 비교 불가 → binlog pos 또는 LSN 우선). 이 정규화 규칙을 sink 가 이해하도록 정의.

## [ ] R2. 다중 컨슈머 fan-out — 규모: 중

**현재**: fanout stage(한 파이프라인 내 브랜치 복제)와 pipeline link(부모→Kafka→자식)가 있으나, **한 CDC/소스 스트림을 여러 독립 소비자가 각자 진도(offset)로** 소비하는 모델은 아니다. 소스는 파이프라인당 1 리더.

**설계**:
- 목표: Kafka 없이도 "한 번 캡처한 변경 스트림 → 여러 다운스트림(각기 다른 변환/싱크/속도)" 을 안전하게.
- 접근: **내부 브로드캐스트 버퍼(fan-out hub)** — CDC 소스 1개가 읽은 이벤트를 N개 구독 채널로 복제. 각 구독자는 자기 커서(committed position)를 독립 관리.
  - 느린 구독자가 빠른 구독자를 막지 않도록 **per-subscriber bounded ring buffer** + 정책(block / drop-oldest / spill). 기본 block(유실 없음, 가장 느린 구독자에 backpressure) — 단 이건 소스까지 backpressure 전파되므로, "독립 속도" 를 원하면 bounded + 명시적 정책 선택.
  - 각 구독자 offset 을 체크포인트에 별도 키로 저장(`PipelineID:subscriber:<id>`) → 구독자별 독립 재개.
- **경고**: 완전한 Kafka 대체(무한 보존·임의 시점 재생)는 비목표. 이건 "실시간 다중 소비" 이지 로그 보존이 아니다. 장기 보존·임의 재생이 필요하면 Kafka 싱크로 내보내는 게 맞다(문서에 명시).

**완료 기준(DoD)**:
- 한 CDC 소스 → 2개 이상 구독자가 각자 변환·싱크로 동시 소비.
- 한 구독자가 느려도 다른 구독자 진도 독립(정책에 따라).
- 구독자별 offset 재개.
- 테스트: 2 구독자, 서로 다른 처리 속도, 각자 정확히 소비 확인.

## [ ] R3. DDL 방어 (스키마 변경 시 안전 정지 + validation) — 규모: 중

**설계 원칙(중요)**: 데이터 파이프라인은 도중에 소스 스키마가 바뀌면 **그 전후 데이터의 정합성·유효성이 깨진다.** 컬럼 타입 변경은 기존 적재 값의 의미를 바꾸고, 컬럼 삭제는 downstream 계약을 무너뜨린다. 따라서 **스키마 진화를 "자동 흡수"하는 것은 금물** — 스키마 레지스트리/자동 마이그레이션은 **만들지 않는다**(정합성 위험 + 관심사 초과). 올바른 기본 동작은 **DDL 감지 시 안전 정지 → 사람이 판단**이다.

**기능 A: DDL 안전 정지 + 사유 표시**
- CDC 가 DDL 이벤트 감지 시, 단순 error 가 아니라 **"현재 파이프라인 설정이 이 스키마에서 동작 불가 → 정지"** 라는 명시적 상태로 파이프라인을 멈춘다(전용 상태/사유 코드).
- 운영자가 UI/API 에서 "스키마 변경으로 정지됨" 을 명확히 인지.

**기능 B: 샘플 기반 validation**
- 전체 데이터를 수집하지 않고 **소스 앞부분 샘플 N건만으로** 현재 파이프라인 설정(stage/변환/싱크 매핑)이 **동작 가능한지 검증**하는 기능.
- 운영자가 DDL 에 맞춰 파이프라인 설정을 수정한 뒤 이 validation 을 실행 → **통과해야만** 파이프라인을 재개(운영)할 수 있게 게이트.
- validation 은 실제 적재 없이 dry-run(stage 파이프라인을 샘플에 태워 에러/타입불일치 검출).

**완료 기준(DoD)**:
- CDC 대상 테이블 ALTER 시 파이프라인이 "스키마 변경으로 동작 불가" 사유로 정지(무조건 계속 흘리지 않음).
- 샘플 N건으로 파이프라인 설정 validation 실행 → pass/fail + 실패 지점 리포트.
- validation 통과 전에는 재개 불가(게이트).
- 테스트: ALTER TABLE → 정지 + 사유 표시 / 잘못된 매핑으로 validation fail / 수정 후 pass.

## [ ] R3b. (후순위) JSON-only 파이프라인의 DDL 허용 — 규모: 소, 우선순위: 낮음

**전제**: R3(DDL 방어)가 완성된 **이후에만** 고려.
- 스키마 검사 없이 JSON 으로만 처리하고 `id` 정도만 유지하는 특정 파이프라인은, 스키마가 자유로우므로 DDL 변경을 허용해도 정합성 문제가 없다.
- 그런 파이프라인에 한해 `on_ddl: allow`(스키마 무관 처리) 를 옵션으로 허용. **기본은 여전히 정지(R3).**
- 필수 아님 — R3 방어 기능 이후 필요 시.

## [ ] R4. exactly-once / 상태 복구 — 규모: 대, 우선순위: 낮음(정직히 Flink 영역)

**현재**: at-least-once + upsert 수렴. 윈도우/조인 상태 Redis 저장은 있으나 재시작 자동복구 없음(`windowed_aggregate_stage.go` LoadFromRedis 수동).

**설계(현실적 범위)**:
- **상태 자동 복구**: 윈도우/조인 stage 가 Open 시 Redis 에서 상태 자동 로드(현재 수동 호출을 자동화). stream_join 도 Redis 저장 추가.
- **effectively-once(실효적 정확히-한번)**: 진짜 2PC EOS 는 비목표(Flink 영역). 대신 **멱등 싱크 + 처리성공 기반 offset 커밋** 조합으로 "재처리해도 결과 동일" 을 보장(CDC full-row upsert 는 이미 이 속성). 이 경계를 문서에 명확히.
- Kafka 소스의 처리성공 기반 커밋(현재 자동커밋=at-most-once 위험)을 **FetchMessage + 처리 후 CommitMessages** 로 전환 → CDC 와 동일한 at-least-once 보장.

**완료 기준(DoD)**:
- Kafka 소스가 처리 성공 후 offset 커밋(at-most-once 위험 제거).
- 윈도우/조인 상태가 재시작 후 자동 복구.
- 테스트: 처리 중 재시작 → 상태·offset 복구, 유실/중복 최소.

---

## 우선순위 & 순서

1. **R4-부분: Kafka 소스 at-least-once 커밋** (작고 명확, 정합성 직결 — 먼저 처리).
2. **R3 DDL 방어**(안전 정지 + 샘플 validation) — 정합성 최우선, 데이터 오염 방지.
3. **R1 초기 적재→CDC 전환**(workflow 오케스트레이션 — 순차 의존 + position 동적 주입).
4. **R2 다중 컨슈머 fan-out**.
5. **R4-나머지: 상태 자동복구**.
6. **R3b JSON-only DDL 허용**(R3 이후, 후순위).

각 항목은 구현 후 **실 DB(가능하면) 또는 단위 테스트로 검증**하고 커밋. Bulk 분산(partition-distributed-execution.md)은 병행 트랙.

**관심사 경계(전 항목 공통)**: CDC 소스는 변경분 캡처만, bulk 는 전량 적재, workflow 는 조합. 한 레이어가 다른 레이어의 일을 떠안지 않는다(R1 을 CDC 안 스냅샷으로 넣지 않는 이유).

## 비목표 (정직하게 — 이건 여전히 전용 도구가 맞음)

- **분산 셔플/대용량 GROUP BY·JOIN**: Spark/Flink. (bulk 문서 참조)
- **진짜 2PC exactly-once 상태연산**: Flink. Conduix 는 effectively-once(멱등+at-least-once)로 접근.
- **무한 로그 보존·임의 시점 재생**: Kafka. fan-out 은 실시간 다중 소비이지 로그 저장이 아님.
