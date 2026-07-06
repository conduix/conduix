# 작업계획: CDC Debezium-parity 로드맵

> 목표: Conduix 단독(Kafka/Debezium 불필요)으로 CDC 를 안전·완결·광범위하게 처리.
> 안전성(유실 0)은 완료됨 — 근거·결정은 [ADR-0004](../adr/0004-cdc-safety.md). 이 문서는 **남은 작업의 실행 계획**이다.
> 상태 표기: [ ] 미착수 · [~] 진행중 · [x] 완료.

## 완료 (안전성 — 참고)

MySQL CDC "지금부터의 변경분"을 유실 없이 처리하는 안전성은 완료됐다(상세는 ADR-0004):
backpressure(`e8f57eb`), 처리성공 offset 커밋·GTID(`b2aa9e5`), DDL 추적(`25cbeca`).

아래는 "Debezium 을 완전히 대체"하기 위해 **남은 대규모 작업**이다.

---

## [ ] #2 초기 스냅샷 (Initial Snapshot) — 규모: 대 (~수일)

**문제**: 현재 CDC 는 binlog 현재 위치부터만 읽는다. 기존 테이블 데이터 전량이 필요한 첫 적재를 못 한다(Debezium 의 snapshot 단계 부재).

**완료 기준(Definition of Done)**:
- 스냅샷 시작 시점의 binlog position(또는 GTID)을 고정한다.
- 대상 테이블의 기존 행을 `SELECT`(PK 기준 페이지네이션)로 전량 읽어 이벤트로 흘린다.
- 스냅샷 완료 후 고정해둔 position 부터 스트리밍 전환한다.
- 스냅샷/스트리밍 경계 중복은 downstream PK upsert 로 흡수(문서화).
- 설정: `snapshot.mode`(none/initial/when_needed 상당) config 노출.
- 테스트: 스냅샷→스트리밍 전환 시 유실·역전 없음.

**단계**:
1. config(`SnapshotMode`) + 상태기계(snapshot→streaming) 설계.
2. position 고정 + 페이지네이션 SELECT 리더.
3. 전환 로직 + 중복 흡수 검증.
4. 회귀 테스트.

**리스크**: 대용량 테이블 스냅샷 시간·부하 → 페이지네이션·rate limit 필요.

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
- MySQL + 기존 데이터 전량 → #2 완료 전까지 1회 벌크 초기적재 또는 Debezium.
- PostgreSQL → #4 완료 전까지 Debezium 경유.
- CDC 소스 고가용성 → #5 완료 전까지 단일 worker 운영.
