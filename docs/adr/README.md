# Architecture Decision Records (ADR)

주요 아키텍처 결정의 **배경·선택·대안·트레이드오프·결과**를 기록한다.
"어떻게 구현했는가"(코드/설계 문서)와 별개로, **"왜 그 선택을 했고 대안은 무엇이었는가"**를 남긴다.

> 원칙(`docs/EXECUTION_TOPOLOGY_INTENT.md`와 동일): 의도·근거가 먼저이고 코드는 그 반영이다.
> 근거가 당시 기록으로 남지 않은 결정은 **"기록 없음 — 코드/커밋 상태로 사후 정리"**라고 정직하게 표기한다.
> 지어낸 근거는 쓰지 않는다.

## 형식

각 ADR은 다음을 담는다: **Status · Context(문제) · Decision(선택) · Alternatives(대안) · Consequences(결과/트레이드오프) · Evidence(근거: 파일·커밋)**.

Status 값: `Accepted`(현행) / `Superseded by ADR-N`(대체됨) / `Proposed` / `Deprecated`.

## 목록

| # | 제목 | Status | 요약 |
|---|------|--------|------|
| [0001](0001-bento-adoption.md) | Bento(Benthos fork) 채택과 실제 사용 범위 | Accepted (제한적) | 라이선스 안전한 MIT fork를 의존성으로 두되, 커넥터는 자체 Go 구현 |
| [0002](0002-remove-actor-engine.md) | Actor 실행 엔진 제거, GroupExecutor 단일화 | Accepted | 도달 불가능한 이중 실행 엔진(~4,700 LOC) 제거 |
| [0003](0003-plugin-architecture-evolution.md) | 플러그인 아키텍처 V2→V3→V4 진화 | Accepted (V4) | Docker→gRPC go-plugin→인프로세스(built-in + JS + native Go) |
| [0004](0004-cdc-debezium-parity.md) | CDC를 Debezium 없이 안전하게 | In progress | 안전성(offset/GTID/DDL/backpressure) 완료, 스냅샷·PG·HA 로드맵 |

## 관련 문서
- [EXECUTION_TOPOLOGY_INTENT.md](../EXECUTION_TOPOLOGY_INTENT.md) — 실행 토폴로지 결정(D1~D6). ADR 이전에 작성됐으나 사실상 ADR 역할.
- [PLUGIN_ARCHITECTURE_V4.md](../PLUGIN_ARCHITECTURE_V4.md) — ADR-0003의 현행 구현 상세.
- [COMPARISON.md](../COMPARISON.md) — 경쟁 도구 대비 선택 근거.
