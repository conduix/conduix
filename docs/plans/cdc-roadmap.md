# 작업계획: Conduix CDC 개선

> 목표: Conduix 단독(Kafka/Debezium 불필요)으로 MySQL CDC 를 안전하게 운영.
> **이 목록은 `pipeline-core/pkg/source/cdc.go`·`pkg/output/sql.go` 코드에서 실제로 확인된 것만** 담는다.
> (이전 버전은 Debezium 일반론에 기댄 추측 항목이 섞여 폐기하고 재작성했다.)
> 근거·완료된 결정은 [ADR-0004](../adr/0004-cdc-safety.md). 상태: [ ] 미착수 · [~] 진행중 · [x] 완료.

## 완료된 것 (코드 확인)

| 항목 | 근거 |
|------|------|
| MySQL binlog CDC (go-mysql/canal) | `cdc.go` openMySQL |
| backpressure (채널 포화 시 drop 대신 blocking) | `cdc.go` OnRow, 커밋 `e8f57eb` |
| 처리성공 기반 offset 커밋 (committedPos) | `cdc.go` Read 소비 루프, 커밋 `b2aa9e5` |
| GTID 체크포인트 + StartFromGTID 복원 | `cdc.go`, 커밋 `b2aa9e5` |
| DDL 이벤트 발행 (OnDDL → `_cdc_type=ddl`) | `cdc.go` OnDDL, 커밋 `25cbeca` |
| INSERT/UPDATE 전체행(after) + SQL 싱크 upsert 수렴 | `cdc.go` rowToMap, `sql.go` ON DUPLICATE KEY UPDATE |
| 테이블 필터(canal IncludeTableRegex), TLS/mTLS | `cdc.go` openMySQL |

**현재 보장**: CDC 이벤트를 유실 없이 받고(재처리로 채움), INSERT/UPDATE 는 upsert 싱크로 소스와 수렴한다.

---

## 남은 작업 (코드에서 확인된 gap 만)

### [x] #1 SQL 싱크의 CDC delete 반영 — 규모: 중, 우선순위: **최상**

**확인된 사실**: CDC 소스는 delete 이벤트를 `_cdc_type=delete` + `_old_data` + `_primary_key` 로 만들어 흘린다(`cdc.go` convertEventToRecord). 그러나 **SQL 싱크(`sql.go`)는 `_cdc_type` 을 읽지 않고 모든 레코드를 INSERT/upsert 한다**(grep 결과 `sql.go` 에 `_cdc_type`/delete 처리 0건).
→ **소스에서 행이 삭제되면 타깃에 그대로 남는다.** upsert 로 수렴하는 것은 insert/update 뿐, delete 는 반영 안 됨.

**완료 기준(DoD)**:
- SQL 싱크가 `_cdc_type=delete` 레코드를 `_primary_key`(또는 conflict_columns) 기준 `DELETE` 로 실행.
- insert/update 는 기존대로 upsert.
- 옵션: delete 반영 on/off (soft-delete 표시만 원하는 경우 대비).
- 테스트: delete 이벤트 → 타깃 행 삭제 확인.

### [x] #2 canal 연결 끊김 재연결 — 규모: 중

**확인된 사실**: `cdc.go` Read 에서 `c.RunFrom(pos)` 가 에러나면 errorCh 로 보내고 goroutine 종료(재연결 로직 없음, `cdc.go:297-304`). DB 재시작·네트워크 순단 시 CDC 가 멈춘다.

**완료 기준(DoD)**:
- RunFrom/StartFromGTID 실패 시 backoff 재연결(상한 있는 지수 백오프).
- 재연결 시 committedPos/committedGTID 부터 재개(이미 있는 값 재사용).
- 반복 실패는 서킷/에러로 종료(무한 재시도 방지).
- 테스트: 연결 끊김 → 재연결 → committedPos 부터 재개.

### [x] #3 BLOB 바이너리 컬럼 처리 — 규모: 소

**확인된 사실**: `rowToMap`(`cdc.go`)이 모든 `[]byte` 를 `string(b)` 로 강제 변환한다. TEXT/JSON 은 문제없지만 순수 바이너리(BLOB/BINARY)는 UTF-8 이 아니면 손상될 수 있다. (go-mysql 은 BLOB 를 `[]byte` 로 준다.)

**완료 기준(DoD)**:
- 컬럼 타입이 바이너리(BLOB/BINARY/VARBINARY)면 `string` 강제 대신 원본 `[]byte` 유지 또는 base64 인코딩(싱크 직렬화 정책과 일치).
- TEXT/VARCHAR/JSON 은 기존대로 string.
- 테스트: 바이너리 라운드트립(비UTF-8 바이트 보존).

### [x] #4 bulk↔CDC 경계 (CDC 시작 position 지정) — 규모: 소~중

**확인된 사실**: CDC config(`SourceV2`)에 시작 position/GTID 필드가 없다(kafka 의 `start_offset` 만 있고 cdc 용 없음). checkpoint 없으면 `GetMasterPos()`(현재 시점)부터 시작(`cdc.go`). 기존 데이터는 bulk 파이프라인이 적재하지만, "bulk 시작 시점부터 CDC" 를 맞출 수단이 없어 bulk 소요 시간 동안의 변경분이 누락될 수 있다.

**완료 기준(DoD)**:
- `SourceV2` 에 `start_position`(file:pos) 또는 `start_gtid` 추가 → `NewCDCSource` 가 초기 committedPos/GTID 로 설정.
- 운영 가이드: bulk 시작 전 position 확보 → bulk 적재 → CDC 를 그 position 부터. 경계 중복은 upsert 로 흡수.
- 테스트: 지정한 position/GTID 부터 시작.

### [x] #5 PostgreSQL CDC — 규모: 대

**구현**: `pglogrepl`(pgoutput proto v2) 논리복제로 구현(`cdc_postgres.go`).
- 복제 연결(`replication=database`)로 스트림 수신, 별도 일반 연결로 퍼블리케이션 생성/확인.
- slot/publication idempotent 생성(중복 42710/pg_publication 조회 가드).
- LSN 기반 checkpoint(`committedLSN`, OffsetType="lsn"), standby status update 로 slot advance(소비 완료 LSN 만).
- unchanged TOAST('u') 컬럼은 직전 행 값 재사용(null 오염 방지), NULL/text 구분.
- replica identity 키(Flags==1) → `_primary_key(_columns)` 로 #1 delete 싱크와 연결.
- `NewCDCSource(driver=postgres)` 조기 거부 제거, `type: cdc` + `driver: postgres` 로 MySQL 과 동일 인터페이스.
- 시작 지점: `start_lsn`("0/1A2B3C4D").

**테스트**: TOAST 재사용/NULL·text, PK 추출, LSN checkpoint 라운드트립, 드라이버 수용(단위) + **실 PostgreSQL e2e**(아래).

**운영 전제**: `wal_level=logical`, 테이블 REPLICA IDENTITY(기본=PK; DELETE 반영 시 필수).

### [x] #6 CDC 소스 중복 실행 방지 — 규모: 중

**확인된 사실(수정 전)**: SETNX claim(`agent.go`)이 15분 TTL 로 **1회 획득 후 갱신 안 됨**. CDC 는 장기 실행이라 TTL 만료 후 다른 에이전트가 같은 execution 을 재획득 → 같은 binlog/slot 이중 소비(중복 이벤트).

**구현**:
- claim TTL 을 30s 로 줄이고 실행 중 `claimRenewalLoop` 이 TTL/3 마다 갱신(`agent.go`).
- 소유권 상실(다른 에이전트 인수 / 만료 후 회수 실패) 시 실행 ctx 를 cancel → CDC 소스가 ctx 관측해 정지(canal/pg 복제 중단). 새 소유자는 checkpoint(committedPos/GTID/LSN)부터 재개.
- 실행 종료 시 `releaseClaim`(우리 소유일 때만 Del)로 TTL 대기 없이 즉시 재배치.
- Redis 순단은 즉시 중단 아님(다음 tick 재시도) — false positive 방지. 종료 사유를 timeout↔claim-lost 로 구분 보고.

**테스트**(miniredis): 획득/갱신, 타 에이전트 인수 시 정지, 만료 후 재획득, 소유자만 해제, 갱신루프의 ctx cancel, standalone 항상 소유.

---

## 우선순위 근거

- **#1(delete 반영)** 이 최상: 코드상 소스 삭제가 타깃에 반영 안 되는 명확한 정합성 gap. CDC 를 "테이블 복제"로 쓰면 바로 드러난다.
- **#2(재연결)**: 장기 실행 CDC 의 실운영 안정성.
- **#3(BLOB)**: 바이너리 컬럼 쓰는 경우만. 범위 작음.
- **#4~#6**: 경계·범위확장·HA. 규모 크거나 특정 요건.

## 실환경 e2e 검증 (2026-07, 완료)

실 MySQL(binlog)·PostgreSQL(logical) 컨테이너 대상으로 이벤트 타입별 전수 검증.
테스트: `pkg/source/cdc_mysql_integration_test.go`, `pkg/cdce2e/` (build tag `cdcintegration`).

**검증 통과**:
- MySQL: insert/update(after+_old_data)/delete(_primary_key) 캡처, VARBINARY 보존, CDC→SQL 싱크 수렴(delete 로 타깃 행 삭제).
- PostgreSQL: insert/update/delete 캡처(REPLICA IDENTITY DEFAULT→PK=[id]), slot/publication 자동 생성, CDC→SQL 싱크 수렴+delete 반영.

**e2e 로 드러나 수정한 실결함**(커밋 f6876f1):
1. `Close`/`runCanalOnce` 재진입 데드락 — `s.mu` 쥔 채 `canal.Close()`(핸들러가 `s.mu` 재획득) → stop/재연결 무한 hang. 락 밖에서 Close 로 수정.
2. mysql `Open()` 의 불필요한 canal 선생성 → 미실행 canal Close 가 스트리밍 시작을 블록. Open() no-op 화.
3. `rowToMap` 바이너리 전달 타입 불일치 — go-mysql 은 VARBINARY 를 string 으로 준다. TYPE_BINARY→[]byte 로 정규화.
4. SQL 싱크 PostgreSQL upsert 누락 — `on_conflict=update` 의 postgres 분기가 비어 UPDATE 이벤트가 중복키 에러. `ON CONFLICT DO UPDATE` 추가.

## 현시점 사용자 권장

- MySQL/PostgreSQL + upsert 싱크 → **Conduix 단독으로 소스와 수렴**(insert/update/delete 실검증 완료).
- PostgreSQL 은 `wal_level=logical` + REPLICA IDENTITY(DELETE 반영 시), publication/slot 은 자동 생성.
- 바이너리 컬럼 / PostgreSQL / 소스 HA → 각각 #3 / #5 / #6 (모두 완료).
