# 작업계획: CDC Debezium-parity 로드맵

> 목표: Conduix 단독(Kafka/Debezium 불필요)으로 CDC 를 안전·완결·광범위하게 처리.
> 안전성(유실 0)은 완료됨 — 근거·결정은 [ADR-0004](../adr/0004-cdc-safety.md). 이 문서는 **남은 작업의 실행 계획**이다.
> 상태 표기: [ ] 미착수 · [~] 진행중 · [x] 완료.

## 완료 (안전성 — 참고)

MySQL CDC "지금부터의 변경분"을 유실 없이 처리하는 안전성은 완료됐다(상세는 ADR-0004):
backpressure(`e8f57eb`), 처리성공 offset 커밋·GTID(`b2aa9e5`), DDL 추적(`25cbeca`).

아래는 "Debezium 을 완전히 대체"하기 위해 **남은 대규모 작업**이다.

---

## [ ] #2 초기 적재 ↔ CDC 경계 조율 (start position 지정) — 규모: 소~중

> **Debezium 식 "CDC 소스 내장 스냅샷"은 만들지 않는다.** Conduix 는 이미 bulk(batch) 파이프라인이
> 기존 데이터 전량 적재를 담당한다(`source/sql.go` 전량 SELECT, `source/partitioned_sql.go` 병렬).
> 따라서 CDC 소스에 SELECT 스냅샷 단계를 넣는 것은 **중복 기능**이다.

**진짜 문제(코드 검증됨)**: bulk 로 초기 적재하고 CDC 로 이후 변경을 이어받을 때 **경계에서 유실이 생긴다**.
CDC 는 checkpoint 가 없으면 `GetMasterPos()`(=지금 시점)부터 시작하고(`cdc.go` Read), **config 로 시작 position/GTID 를 지정할 수단이 없다**(`v2_config.go` 의 InputV2 에 cdc 용 start_position/start_gtid 없음 — kafka 의 start_offset 만 있음).
→ "bulk 시작 직전 position 을 잡아 → bulk 적재 → CDC 를 그 position 부터" 를 하고 싶어도 CDC 에 그 시작점을 넣을 방법이 없다. bulk 소요 시간 동안의 변경분이 유실된다.

**완료 기준(DoD)**:
- CDC config 에 `start_position`(binlog file:pos) 또는 `start_gtid` 지정 옵션 추가 → 지정 시 그 지점부터 시작.
- 운영 절차 문서화: (1) bulk 시작 전 `SHOW MASTER STATUS`/GTID 로 position 확보 → (2) bulk 적재 → (3) CDC 를 그 position 으로 시작. 경계 중복은 sink PK upsert(on_conflict=update)로 흡수.
- (선택) bulk→CDC 를 한 워크플로우로 연결(bulk 완료 시 잡아둔 position 을 자식 CDC 파이프라인에 주입)하는 편의 기능.
- 테스트: 지정한 position/GTID 부터 시작하는지, 경계 유실 없음(upsert 흡수).

**단계**:
1. `InputV2` 에 `start_position`/`start_gtid` 필드 추가 + `NewCDCSource` 가 이를 초기 committedPos/GTID 로 설정.
2. 운영 가이드(bulk→CDC 경계 맞추기) 문서화.
3. 회귀 테스트.

**규모 정정**: 애초 "스냅샷 구현(대)"으로 잘못 적었으나, 실제로는 "start position config + 경계 문서화"라 **소~중**. bulk 파이프라인 재사용이 핵심.

---

## [ ] #4 PostgreSQL CDC — 규모: 대 (~수일~수주)

**문제**: 현재 `NewCDCSource(driver=postgres)` 는 생성 시점 조기 거부. PostgreSQL 사용자는 Conduix 단독 CDC 불가.

**완료 기준**:
- `pglogrepl`(logical replication) + replication slot + `pgoutput`(또는 wal2json) 플러그인으로 변경 스트림 수신.
- LSN 기반 checkpoint(안전성 #1 의 committedPos 구조 재사용).
- replication slot 생성/정리(미정리 시 WAL 폭증) 운영 처리.
- 조기 거부 로직 제거, MySQL CDC 와 동일 인터페이스로 노출.
- 테스트: insert/update/delete + LSN 재개.

**단계**:
1. `pglogrepl` 의존성 추가 + 연결/슬롯 생성.
2. 변경 디코딩(pgoutput) → CDCEvent 매핑.
3. LSN checkpoint 커밋(committedPos 구조 확장).
4. 슬롯 정리·오류 복구 + 테스트.

**리스크**: 새 의존성 + PG 복제 인프라 요구(권한, wal_level=logical). `REMAINING_WORK_CHECKLIST.md`에 "무거운 투자"로 기록됨.

---

## [ ] #5 CDC HA / 중복 실행 방지 — 규모: 중

**문제**: CDC 소스는 단일 실행이어야 한다(같은 binlog 를 두 인스턴스가 읽으면 중복). 현재는 파이프라인 레벨 SETNX claim(distribution-model)에 의존하고, CDC 소스 전용 방어는 없다.

**완료 기준**:
- claim 을 CDC 소스 lifecycle 에 명시 결합 — 리더 worker 만 canal/replication 실행.
- claim 만료·리더 교체 시 안전 인계(offset 은 이미 처리성공 기준이라 인계 후 재개 안전).
- 테스트: 리더 교체 시 유실·중복 없음.

**단계**:
1. claim 획득/갱신을 소스 Open/Read 에 결합.
2. claim 상실 시 소스 정지 + 재획득 시 committedPos 부터 재개.
3. 페일오버 시나리오 테스트.

---

## 현시점 사용자 권장 (완료 전까지)

- MySQL + 지금부터 변경분 → **Conduix 단독으로 안전**(완료).
- MySQL + 기존 데이터 적재 → **bulk 파이프라인으로 지금도 가능**. 단 bulk→CDC 경계 유실 없이 이어받으려면 #2(CDC start position 지정) 필요 — 그 전까지는 sink upsert + 수동 position 맞춤으로 운영.
- PostgreSQL → #4 완료 전까지 Debezium 경유.
- CDC 소스 고가용성 → #5 완료 전까지 단일 worker 운영.
