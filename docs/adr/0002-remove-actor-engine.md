# ADR-0002: Actor 실행 엔진 제거, GroupExecutor 단일화

- **Status**: Accepted
- **결정 커밋**: `5abfd6f` `refactor(pipeline-core): remove dead actor execution engine` (2026-07-02)
- **근거 수준**: **강함** — 커밋 메시지에 제거 근거가 명시됨.

## Context (문제)

초기 설계(`archive/technical-design-review.md`)는 **Actor Model 기반 실행 엔진**을 채택했다. Kafka 복구를 위한 supervision 전략(`one_for_one`/`one_for_all`), mailbox, actor system 등이 `pkg/actor/`에 완전히 구현돼 있었다.

그러나 실제로는 **워크플로우 실행이 전부 `GroupExecutor`로만 이뤄졌다.** Actor 엔진은 어디서도 실행 경로에 연결되지 않았다:

- actor 엔진은 `Agent.StartPipeline`을 통해서만 도달 가능.
- 그 트리거인 `CommandStartPipeline`은 **control-plane이 발행하지 않음**.
- agent의 해당 REST 엔드포인트는 **호출하는 클라이언트가 없음**.

결과적으로 **두 개의 실행 엔진(actor + GroupExecutor)이 공존**했고, 하나는 도달 불가능한 dead code였다. 이중 엔진은 유지보수·인지 부담이며, "어느 엔진이 진짜인가"라는 혼동을 만든다.

## Decision (선택)

**Actor 실행 엔진을 완전히 제거하고 `GroupExecutor`를 유일한 실행 엔진으로 단일화한다.**

- 제거: `pkg/actor/`(actor/mailbox/supervisor/system 및 actor/types/*), `pkg/pipeline/runner.go`, `pkg/stream/pipeline_actor.go`.
- 총 **17개 파일, 4,482 LOC 삭제** (커밋 `5abfd6f` stat).
- **보존**: `shared/types`의 actor 타입 — control-plane의 그래프 시각화(`graph_handler`)가 사용하므로 유지.

## Alternatives (대안)

1. **두 엔진 유지** — 거부. dead code를 방치하면 계속 "이중 실행 엔진" 혼동·유지비 발생.
2. **actor 엔진을 실제 실행 경로로 승격**(GroupExecutor 제거) — 거부. GroupExecutor가 이미 모든 워크플로우를 처리하고 있고, PreStages·배치·링크 등 실제 기능이 여기 붙어 있음. 되돌리는 비용이 큼.
3. **actor 제거, GroupExecutor 단일화** — 채택.

## Consequences (결과·트레이드오프)

**긍정:**
- 실행 엔진 이중성 해소 → "GroupExecutor가 유일한 엔진"이라는 단일 정신 모델.
- 4,482 LOC 감소 → 유지보수 표면 축소.

**잃은 것과 대체:**
- Actor의 **supervision 전략**(`one_for_one`/`one_for_all` 재시작)은 사라짐. 대신 복원력을 다른 층에서 커버:
  - **panic recover**: `executeGroup` 고루틴 단위 recover로 한 파이프라인 panic이 agent 전체를 죽이지 않음.
  - **서킷 브레이커 + retry/backoff**: `failure_guard`가 실패 카운트·재시도를 담당(ADR과 별개 기능).
  - **고아 실행 감지**: 담당 agent가 죽은 running 실행을 scheduler가 failed 전이.
- GroupExecutor의 동시성 모델: 파이프라인당 고루틴 + 세마포어 풀(workers 1~100). actor mailbox 대신 Go 채널·고루틴 사용.

**주의:** actor의 세밀한 재시작 전략(개별 actor만 재시작 등)에 해당하는 기능은 **동등 대체가 아니다.** 현재는 "파이프라인 단위 실패 처리 + 서킷/재시도"로 커버하며, actor 수준의 부분 재시작 supervision은 없다. 필요해지면 재도입 검토.

## Evidence (근거)

- 커밋 `5abfd6f` — 메시지에 도달 불가능성(Agent.StartPipeline만 경유, control-plane 미발행, 클라이언트 미호출)과 4,700 LOC dead code 제거 근거 명시. stat: 17 files, 4,482 deletions.
- 연관 정리 커밋: `f10a854`(dead PipelineRunner type 제거).
- `pipeline-core/pkg/executor/group_executor.go` — 현행 유일 실행 엔진.
- `pipeline-worker/internal/agent/agent.go` — `executeGroup`의 panic recover.
- 초기 actor 선택 근거: `docs/archive/technical-design-review.md`(이후 이 결정으로 무효화).
