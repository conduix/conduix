# ADR-0004: CDC 안전성 — backpressure·처리성공 offset·GTID·DDL

- **Status**: Accepted
- **결정 커밋**: `e8f57eb`(backpressure), `b2aa9e5`(offset 커밋 + GTID), `25cbeca`(DDL)
- **근거 수준**: 강함 — 코드/테스트로 검증.

## Context (문제)

Conduix MySQL CDC 는 소스 DB(binlog)가 진실의 원천이므로, 이론상 유실 없이 처리할 수 있어야 한다(처리 안 된 offset 을 다시 읽으면 됨). 그러나 구현에 다음 결함이 있어 실제로는 유실·복구 문제가 있었다:

1. **이벤트 drop**: `OnRow` 가 내부 채널(eventCh, 버퍼 1000)이 가득 차면 `select default` 로 이벤트를 **조용히 버렸다**. 다운스트림이 느리면 유실.
2. **offset 이 "읽은 위치"**: `OnPosSynced` 가 canal 이 binlog 를 읽는 즉시 position 을 갱신했다. 싱크 적재 성공과 무관 → 재시작 시 "읽었지만 미소비" 구간을 건너뛰어 유실 가능.
3. **GTID 미저장**: `OnPosSynced` 가 GTIDSet 을 받지만 저장하지 않아, 서버 페일오버·binlog 파일 교체에 취약(binlog position 만 의존).
4. **DDL 무시**: Insert/Update/Delete 만 처리하고 ALTER/CREATE/DROP 등 스키마 변경을 무시 → 컬럼 매핑이 조용히 어긋날 수 있었다.

이것들은 CDC 이론의 한계가 아니라 **구현 부족**이었다.

## Decision (선택)

CDC 소스가 소스 DB 를 진실의 원천으로 삼아 **유실 없이** 처리하도록, 네 가지를 구현한다:

1. **Backpressure(drop 금지)**: eventCh 전송을 blocking 으로 바꾼다. 채널이 가득 차면 canal 이 자연히 느려진다(backpressure). 종료 시 hang 방지를 위해 `stopCh` 로만 탈출.
2. **처리성공 기반 offset 커밋**: 각 CDCEvent 에 그 시점 position/GTID 를 실어, `records` 채널로 **실제 소비된** 이벤트의 위치만 `committedPos`/`committedGTID` 로 커밋한다. `GetSourceCheckpoints` 는 read-ahead 가 아니라 committed 값을 저장 → 재시작 시 미소비 구간을 다시 읽어 유실 방지.
3. **GTID 체크포인트**: 소비된 GTID 를 checkpoint(OffsetType=gtid)로 저장·복원. 복원 시 GTID 가 있으면 `canal.StartFromGTID` 로 시작(페일오버 강함), 없으면 position 폴백.
4. **DDL 추적**: canal `OnDDL` 훅으로 스키마 변경을 `ddl` 이벤트(type=ddl, Data.ddl=SQL)로 발행하고 로그로 남긴다. canal 이 스키마 캐시를 갱신하므로 컬럼 매핑은 자동 유지되고, downstream 은 `_cdc_type=ddl` 로 스키마 진화를 인지한다.

## Alternatives (대안)

- **Debezium→Kafka 경유** — CDC 도구로 성숙하지만 별도 스택(Kafka Connect+Kafka) 운영 부담. "지금부터의 변경분 유실 없이"라는 목표에는 Conduix 단독 구현이 스택을 줄인다. (기존 데이터 초기 적재는 Conduix 의 bulk 파이프라인이 담당하므로 CDC 소스에 스냅샷을 넣지 않는다. bulk↔CDC 경계 조율·PostgreSQL·HA 는 별도 로드맵 — `docs/plans/cdc-roadmap.md`.)
- **drop 유지 + 무한 버퍼** — 거부. 무한 버퍼는 메모리 폭증, drop 은 유실. backpressure 가 정답(소스 DB 가 데이터를 보관하므로 느려도 됨).
- **at-most-once 수용** — 거부. CDC 는 유실이 곧 데이터 불일치.

## Consequences (결과·트레이드오프)

**긍정:**
- MySQL CDC "지금부터의 변경분"을 **유실 없이** 단독 처리. Kafka/Debezium 불필요.
- GTID 복원으로 서버 페일오버·binlog 교체에 강함.
- 스키마 변경이 조용히 무시되지 않음.

**트레이드오프/한계:**
- **at-least-once**(exactly-once 아님): 처리성공 offset 커밋으로 유실은 없으나, 재시작 시 committed 이후 이벤트가 재처리될 수 있음 → downstream 은 PK upsert 등 멱등 처리 권장.
- **소비 기준 커밋의 정밀도**: "records 채널로 나감"을 소비 성공으로 본다. 싱크 최종 적재 실패까지 offset 을 되돌리는 완전한 end-to-end ack 은 아니다(그건 소스-싱크 offset 전파가 필요한 별도 작업).
- **아직 남은 작업(코드 확인된 gap 만)**: SQL 싱크가 CDC delete 를 반영 안 함(insert/update 만 upsert), canal 연결 끊김 재연결 없음, BLOB `string([]byte)` 강제, bulk↔CDC 경계 start position config, PostgreSQL 미지원, CDC 소스 중복실행 방지. → `docs/plans/cdc-roadmap.md`. (타입 매핑·트랜잭션 원자성은 go-mysql/canal 이 이미 처리 — 앞선 우려는 오진이었음.)

## Evidence (근거)

- 커밋 `e8f57eb`(backpressure), `b2aa9e5`(committedPos/GTID), `25cbeca`(OnDDL).
- 구현: `pipeline-core/pkg/source/cdc.go` — `committedPos`/`committedGTID`/`OnDDL`/blocking send.
- 테스트: `cdc_backpressure_test.go`(drop 없음+Close 탈출), `cdc_checkpoint_test.go`(committed vs read-ahead, GTID round-trip), `cdc_ddl_test.go`(DDL 이벤트).
- 남은 작업: `docs/plans/cdc-roadmap.md`.
