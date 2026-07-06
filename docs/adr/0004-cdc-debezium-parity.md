# ADR-0004: CDC를 Debezium 없이 안전하게 — 진행 상황과 로드맵

- **Status**: In progress (안전성 완료, 완결성·범위·HA 남음)
- **목표**: Kafka/Debezium 같은 별도 스택 없이 Conduix 단독으로 안전·유연·빠른 CDC. 여러 솔루션 혼합 불필요화.
- **원칙**: CDC 는 소스 DB 가 진실의 원천이다. 느리면 backpressure, 실패하면 재처리, offset 은 처리 성공 기준으로 커밋 → 유실 0 이 이론상 당연하며 구현으로 보장한다.

## 배경

초기 진단에서 "제대로 된 CDC 면 Debezium 경유"라고 했으나, 이는 Conduix 의 **당시 구현 부족**이지 이론적 한계가 아니었다. CDC 데이터는 binlog offset 이 있어 처리 안 된 지점을 다시 읽으면 유실이 없어야 한다. 그 이론을 구현으로 채운다.

## 완료 (안전성 — MySQL CDC 유실/정확성)

| # | 항목 | 상태 | 커밋 |
|---|------|------|------|
| — | **backpressure(이벤트 drop 제거)** | ✅ | `e8f57eb` — 채널 포화 시 drop→blocking, 종료 시 stopCh 로 탈출 |
| 1 | **처리 성공 기반 offset 커밋** | ✅ | `b2aa9e5` — read-ahead 가 아닌 소비된 position(committedPos)만 checkpoint 저장 |
| 6 | **GTID 체크포인트** | ✅ | `b2aa9e5` — 소비된 GTID 저장·복원, 복원 시 StartFromGTID(페일오버 강함) |
| 3 | **DDL(스키마 변경) 추적** | ✅ | `25cbeca` — canal OnDDL 훅으로 ddl 이벤트 발행+로그 |

이로써 **MySQL CDC 는 "지금부터의 변경분을 유실 없이"** 처리한다. (backpressure + 소비 기준 offset + GTID + 스키마 변경 가시화)

## 남은 작업 (완결성·범위·HA — 규모 큼)

### #2 초기 스냅샷 (Initial Snapshot) — 대
- **문제**: 현재 binlog 현재 위치부터만 읽음 → 기존 데이터 전량이 필요한 경우(첫 적재) 불가.
- **방향**: `snapshot → streaming` 2단계. 스냅샷 중 시작 시점 binlog position 을 잡아두고, `SELECT` 로 기존 행을 훑은 뒤 그 position 부터 스트리밍(중복은 PK upsert 로 흡수). Debezium 의 `snapshot.mode` 대응.
- **규모**: 스냅샷 쿼리 페이지네이션 + position 고정 + 스냅샷/스트리밍 전환 상태기계. 수일.

### #4 PostgreSQL CDC — 대
- **현재**: 생성 시점 조기 거부(`cdc.go` — "use MySQL CDC or Debezium via Kafka").
- **방향**: `pglogrepl`(logical replication) + replication slot + `pgoutput`/`wal2json` 플러그인. 슬롯 생성·LSN 체크포인트·슬롯 정리(미정리 시 WAL 폭증) 운영 고려 필수.
- **규모**: 새 의존성 + PG 복제 인프라 + LSN 기반 checkpoint(위 #1 구조 재사용). 수일~수주. `REMAINING_WORK_CHECKLIST.md`에도 "무거운 투자"로 기록됨.

### #5 CDC HA / 중복 실행 방지 — 중
- **현재**: 파이프라인 레벨 SETNX claim 으로 단일 worker 실행(distribution-model). CDC 소스 레벨 전용 방어는 없음 — 같은 binlog 를 두 인스턴스가 읽으면 중복.
- **방향**: claim 을 CDC 소스 lifecycle 에 명시적으로 결합(리더만 canal 실행), claim 만료 시 안전 인계(offset 은 이미 처리 성공 기준이라 인계 후 재개 안전).
- **규모**: claim-소스 결합 + 페일오버 시 재개 검증. 중.

## Debezium 대비 현황표 (갱신)

| 항목 | Debezium | Conduix (현재) |
|------|----------|----------------|
| MySQL 변경분 유실 없이 | ✅ | ✅ (완료) |
| GTID 복구 | ✅ | ✅ (완료) |
| DDL 가시화 | ✅ | ✅ (이벤트+로그, 완료) |
| backpressure | ✅ | ✅ (완료) |
| 초기 스냅샷 | ✅ | ❌ (#2 로드맵) |
| PostgreSQL | ✅ | ❌ (#4 로드맵) |
| CDC HA | ✅ | ⚠️ 파이프라인 claim (소스 전용 #5 로드맵) |
| exactly-once 상태연산 | ✅ | ✗ (설계상 비목표 — 대규모 상태연산은 Flink) |

## 결론 (현시점 권장)

- **MySQL + 지금부터의 변경분** → **Conduix 단독으로 안전**(완료). Kafka/Debezium 불필요.
- **MySQL + 기존 데이터 전량 필요** → #2 완료 전까지는 초기 적재를 별도로(1회 벌크) 하거나 Debezium.
- **PostgreSQL** → #4 완료 전까지 Debezium 경유.
- **CDC 소스 고가용성** → #5 완료 전까지 단일 worker 운영 권장.

## Evidence
- 완료 커밋: `e8f57eb`, `b2aa9e5`, `25cbeca`.
- 구현: `pipeline-core/pkg/source/cdc.go` (committedPos/committedGTID/OnDDL/backpressure).
- 관련: `docs/COMPARISON.md`, `docs/REMAINING_WORK_CHECKLIST.md`.
