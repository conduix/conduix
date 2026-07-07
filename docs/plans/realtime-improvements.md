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

- [x] **R1** 초기적재+CDC 동시실행 수렴
  - [x] sink position 버전 가드 upsert (`08fc757`, 실 MySQL/PostgreSQL e2e 통과)
  - [x] CDC 레코드에 단조 position(`_pos`) 부착 — mysql binlog(파일<<32|pos)·postgres LSN, 단위 검증(rotation/passthrough)
  - [x] workflow 병렬 기동 — 기존 ExecutionModeParallel 로 bulk+CDC 동시 실행(별도 구현 불요)
- [x] **R4a** Kafka at-least-once ack 커밋
  - [x] 소스 ack 로직 + 실 Kafka e2e (`15b8973`)
  - [x] executor flush→Ack 배선(realtime) + 파이프라인 레벨 실 Kafka e2e(GroupExecutor: 1차 5건 커밋 → 재시작 새 5건만, 유실 0)
- [x] **CDC 소스 ack 기반 커밋 전환** (seq 부착 + Ack 시 committed 전진, 유실 창 제거. 단위+race 검증, CDC e2e 회귀 통과)
- [x] **R2** 다중 컨슈머 fan-out — **폐기**(사용법으로 커버: realtime 파이프라인 여러 개 등록). 코드 작업 아님.
- [x] **R3** DDL 방어(안전 정지 + 샘플 validation)
  - [x] A: DDL 감지 시 "schema_changed" 정지 (실 MySQL e2e)
  - [x] B: 샘플 dry-run validation(ValidatePipelines) — 재개 게이트 (단위 검증)
- [ ] **R3b** JSON-only DDL 허용 (후순위, on_ddl=allow 옵션 — 실제 코드)
- [x] **R4** 상태 자동복구 — 코드 확인: 윈도우 집계는 이미 자동 복구(생성 시 restoreStateFromRedis), stream_join 은 at-least-once replay 로 커버. 추가 작업 없음(중복 구현 회피).

## 남은 gap (이 계획의 대상)

BULK_VS_REALTIME_COMPARISON §2.4 의 잔여 항목(R2 는 사용법으로 커버되어 폐기 — 실제 코드 gap 은 R1/R3/R4):

| # | gap | 현재 상태(코드) | 대체 목표 |
|---|-----|-----------------|-----------|
| R1 | **초기 적재+CDC 동시 실행 수렴** | 동시 실행 시 snapshot old 값이 CDC 최신값을 덮는 race 를 막을 sink 버전 가드 없음 | 초기적재·CDC **동시 시작** + position 단조 가드로 순서 무관 수렴(초기적재 대기 제거) |
| ~~R2~~ | **다중 컨슈머 fan-out** | **gap 아님** — realtime 파이프라인 여러 개 등록으로 커버(사용법) | 폐기 |
| R3 | **DDL 방어(안전 정지+validation)** | DDL 이벤트 발행(`_cdc_type=ddl`)만 있고, 정지·검증 게이트 없음 | 스키마 변경 시 자동 흡수 대신 **정합성 우선 정지 + 재개 게이트** |
| R4 | **상태 자동복구** | 윈도우 집계는 이미 생성 시 자동 복구(restoreStateFromRedis), stream_join 은 소스 replay 로 커버 | 추가 작업 없음(코드 확인) |

---

## [x] R1. 초기 적재 + CDC **동시 실행** → position 버전 가드로 수렴 — 규모: 중, 우선순위: **최상**

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

## ~~R2. 다중 컨슈머 fan-out~~ — 폐기(사용법으로 커버됨, 코드 작업 아님)

**판정**: "한 소스를 여러 독립 소비자가 각자 처리"는 **realtime 파이프라인을 여러 개 등록**하면 된다.
각 파이프라인이 CDC 소스를 자기 execution 으로 읽고(claim·checkpoint 가 execution 단위라 독립),
서로 다른 stage·sink 를 붙인다. 별도 "fan-out hub" 신기능은 불필요 — 이건 Conduix 사용법이지 gap 이 아니다.
(주의: 이건 "같은 스트림을 여러 소비자" 케이스다. "한 배치 데이터셋을 여러 노드로 샤딩" 은 별개이며 그건
bulk partition 분산 — partition-distributed-execution.md 에서 다룬다.)

## [x] R3. DDL 방어 (스키마 변경 시 안전 정지 + validation) — 규모: 중

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

## [x] R4. 윈도우/조인 상태 자동복구 — 코드 확인 결과 이미 충족(추가 작업 없음)

**코드 확인(정정)**:
- **윈도우 집계**: 이미 자동 복구된다. `NewWindowedAggregateStage`(`windowed_aggregate_stage.go:341`)가 Redis state store 설정 시 생성 시점에 `restoreStateFromRedis()` 를 호출하고 `persistLoop` 로 주기 저장한다. → "수동 호출" 이라던 계획 전제가 틀렸음. **이미 됨.**
- **stream_join**: Redis 지속성이 없다(재시작 시 left/right 버퍼 유실). 그러나 조인 버퍼는 `maxAge` 시간창 안의 레코드만 담고, **at-least-once 소스 재전송(R4a/CDC ack)** 으로 미ack 레코드가 재유입되면 버퍼가 자연 재구성된다 → 창 안에서 조인 재성립. 즉 별도 상태 영속화 없이 **소스 replay + windowing 으로 복구**된다.

**결론**: 윈도우 집계는 이미 자동복구, stream_join 은 소스 replay 로 커버 → **R4 를 위한 추가 코드 작업 없음.**
"stream_join 전용 Redis 영속화" 는 replay 로 이미 커버되는 것을 중복 구현하는 것이라 하지 않는다(가짜 작업 회피).

**비목표(하지 않음, 사유)**:
- **exactly-once 2PC**: Flink 영역. Conduix 는 at-least-once + 멱등(upsert) 수렴으로 충분(이미 확보).
- Kafka at-most-once 는 R4a(ack 커밋)로 **이미 해소**됨.

---

## 현재 상태 요약 (이번 goal)

- ✅ **R4a** Kafka at-least-once ack 커밋 — 소스+executor 배선, 실 Kafka e2e.
- ✅ **CDC ack 전환** — 유실 창 제거.
- ✅ **R3** DDL 방어 — 안전 정지(실 MySQL e2e) + 샘플 validation.
- ✅ **R1** 초기적재+CDC 동시 수렴 — sink 버전가드 + CDC `_pos` + 병렬 기동.
- ✅ **R2** — 폐기(사용법 커버).
- ✅ **R4** 상태 자동복구 — 코드 확인 결과 이미 충족(윈도우 자동복구 + join replay).
- ⏳ **R3b** JSON-only DDL 허용(`on_ddl=allow`) — 후순위, 미착수.

Bulk 분산(partition-distributed-execution.md)은 병행 트랙 — 배선 완료, 실 다중노드 e2e 남음.

**관심사 경계(전 항목 공통)**: CDC 소스는 변경분 캡처만, bulk 는 전량 적재, workflow 는 조합. 한 레이어가 다른 레이어의 일을 떠안지 않는다(R1 을 CDC 안 스냅샷으로 넣지 않는 이유).

## 비목표 (정직하게 — 이건 여전히 전용 도구가 맞음)

- **분산 셔플/대용량 GROUP BY·JOIN**: Spark/Flink. (bulk 문서 참조)
- **진짜 2PC exactly-once 상태연산**: Flink. Conduix 는 effectively-once(멱등+at-least-once)로 접근.
- **무한 로그 보존·임의 시점 재생**: Kafka. fan-out 은 실시간 다중 소비이지 로그 저장이 아님.
