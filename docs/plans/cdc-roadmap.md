# 작업계획: CDC Debezium-parity 로드맵

> 목표: Conduix 단독(Kafka/Debezium 불필요)으로 CDC 를 안전·정확·완결·광범위하게 처리.
> 근거·결정은 [ADR-0004](../adr/0004-cdc-safety.md). 이 문서는 **남은 작업의 실행 계획**이다.
> 상태 표기: [ ] 미착수 · [~] 진행중 · [x] 완료.

## 단계 기준 — "어디까지 하면 무엇을 주장할 수 있나"

CDC 성숙도는 누적 단계다. **각 단계를 넘어야 그 주장을 할 수 있다.** 아래 항목번호와 매핑.

| 단계 | 의미 | 필요 항목 | 이걸 넘으면 할 수 있는 주장 |
|------|------|----------|----------------------------|
| **A. 안전(유실 0)** | 처리 안 된 변경은 결국 다시 처리 | ✅ 완료(backpressure/offset/GTID/DDL) | "변경분을 **유실 없이** 받는다" |
| **B. 정확(왜곡 0)** | 받은 데이터가 **틀리지 않게** 들어감 | #6 타입매핑, #7 트랜잭션경계, #8 이벤트완전성 | "**데이터가 정확하다**" ← Debezium 대체의 핵심 |
| **C. 완결(경계)** | 초기적재↔CDC 경계 유실 없음 | #2 start position + bulk 연계 | "기존+증분을 이어서 다 받는다" |
| **D. 범위** | 지원 DB 확장 | #4 PostgreSQL | "MySQL 뿐 아니라 PG 도" |
| **E. HA** | 소스 고가용성 | #5 중복실행 방지 | "무중단 운영" |

> ⚠️ **A(안전)만으로는 Debezium 대체가 아니다.** "유실 없음 ≠ 정확함". 금융 DECIMAL·바이너리 BLOB·unsigned
> 카운터 등은 **B(정확)** 없이는 조용히 왜곡된다(silent corruption). "Debezium 대체" 주장은 최소 **A+B** 필요.
> 현재 A 만 완료 → **단순 숫자/텍스트 데이터(이벤트 로그·상태 동기화)에 한해 안전**하다.

## 완료 (A. 안전성)

MySQL CDC "지금부터의 변경분"을 유실 없이 처리(상세는 ADR-0004):
backpressure(`e8f57eb`), 처리성공 offset 커밋·GTID(`b2aa9e5`), DDL 추적(`25cbeca`).

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

# B. 정확성 (왜곡 0) — Debezium 대체의 핵심

> A(유실 0)와 별개로, **받은 데이터가 틀리지 않게** 들어가야 한다. 아래는 행이 통째로 사라지는 게 아니라
> **"데이터가 들어가긴 하는데 틀리게 들어가는" silent corruption** 문제다. Debezium 이 수년간 다듬은 지점이며,
> 금융·바이너리·복합 타입 도메인에서 필수. (근거: `cdc.go` 코드 조사)

## [ ] #6 데이터 타입 매핑 — 규모: 중, 우선순위: **최상** 🔴

**문제**: `rowToMap`(cdc.go)이 `[]byte→string` 변환만 하고 컬럼 타입을 무시한다. `schema.TableColumn.Type` 정보가 있는데 안 쓴다. 결과:
- **DECIMAL/NUMERIC**: 정밀도 손실(JSON 직렬화 시 float 화) → 금융 데이터 왜곡.
- **BLOB/BINARY**: `[]byte→string`(UTF-8 강제) → 바이너리 손상.
- **UNSIGNED INT**: int64 로 다뤄 2^31 초과 시 음수화(오버플로우).
- **BIT/SET/JSON/ENUM**: 타입 미보증·부정확.

**완료 기준(DoD)**:
- `rowToMap`/`getPrimaryKeyValues` 를 `schema.TableColumn.Type`(+ IsUnsigned 등) 기반 타입별 변환으로 재작성.
- DECIMAL→string(정밀도 보존), BLOB→base64 또는 명시적 []byte, UNSIGNED→uint64, DATETIME/TIMESTAMP→RFC3339, JSON→검증된 문자열/객체, BIT/ENUM/SET→정의된 표현.
- 각 타입 회귀 테스트(경계값: unsigned 최대, decimal 정밀도, 바이너리 라운드트립).

**규모**: 중. go-mysql 의 컬럼 타입 코드 매핑 + 변환기. 며칠.

## [ ] #7 트랜잭션 경계 — 규모: 중 🔴

**문제**: `OnXID`(트랜잭션 커밋) 훅 미구현. 한 트랜잭션의 여러 row 변경이 row 단위로 흩어져 발행 → 파이프라인이 트랜잭션 원자성을 알 수 없다. 부분 적용·부분 중복 가능.

**완료 기준(DoD)**:
- `OnXID` 훅 구현 + `CDCEvent` 에 트랜잭션 식별(txn_id/xid) + 순서 필드 추가.
- downstream 이 트랜잭션 경계를 인지할 수 있게 메타데이터로 전달(선택: 트랜잭션 단위 커밋).
- 순서 보장 문서화(단일 파티션/테이블 내 순서).
- 테스트: 멀티-row 트랜잭션이 같은 txn_id 로 묶이는지.

**규모**: 중.

## [ ] #8 이벤트 완전성 + 연결 복원력 — 규모: 중 🟠

세 가지 묶음(모두 정확성·안정성 관련):
- **UPDATE old/new 쌍 검증**: `OnRow` 가 홀수 row 를 조용히 버린다(`break`) → 검증·에러 처리 추가.
- **TRUNCATE/LOAD DATA**: 현재 무시됨. TRUNCATE 는 대량 삭제인데 데이터 이벤트로 안 옴 → 최소한 감지·경고, 가능하면 삭제 이벤트로 표현.
- **연결 복원력**: canal 연결이 끊기면 errorCh 로 에러만 내고 종료(재연결 없음). MongoDB CDC 처럼 exponential backoff 재연결 + committedPos 부터 재개 추가.

**완료 기준(DoD)**:
- UPDATE 쌍 불일치 시 에러 발행(조용한 버림 금지).
- TRUNCATE 감지 + 이벤트/경고.
- 연결 실패 시 backoff 재연결, 재개 시 committedPos/GTID 부터.
- 테스트: 각 시나리오.

**규모**: 중(세 항목).

---

## 현시점 사용자 권장 (완료 전까지)

현재 단계 = **A(안전) 완료, B(정확) 미완**. 즉 유실은 없으나 특정 타입은 왜곡될 수 있다.

- **단순 숫자/텍스트 데이터**(이벤트 로그, 상태 동기화 등) → MySQL 변경분을 **Conduix 단독으로 안전**하게 처리.
- **금융(DECIMAL)·바이너리(BLOB)·unsigned·복합 타입 포함** → **#6(타입 매핑) 완료 전까지는 왜곡 위험** → Debezium 병행 또는 #6 우선 구현 권장.
- **트랜잭션 원자성이 중요**(여러 테이블 동시 변경의 일관성) → #7 완료 전까지 주의.
- MySQL + 기존 데이터 적재 → bulk 파이프라인으로 지금도 가능. 경계 유실 없이 이어받으려면 #2 필요(그 전까지 sink upsert + 수동 position).
- PostgreSQL → #4 완료 전까지 Debezium 경유.
- CDC 소스 고가용성 → #5 완료 전까지 단일 worker 운영.

**"Debezium 대체" 주장 가능 시점**: 최소 A + B(#6·#7·#8) 완료. 그 전에는 "MySQL 단순 데이터 CDC 는 단독 가능"까지만.
