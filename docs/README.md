# Conduix 문서 인덱스

> 이 디렉토리에는 **현재 유효한** 설계·참조·가이드 문서만 둔다.
> 완료된 작업 계획/진행 로그와 폐기된 아키텍처 문서는 [`archive/`](archive/)로 옮겨 현재 문서와 섞이지 않게 한다.

## 현재 유효 문서

### 아키텍처 · 설계
- [design-v2.md](design-v2.md) — 파이프라인 근간 설계(배치/실시간, 소스, Input→Stage→Output 모델)
- [PLUGIN_ARCHITECTURE_V4.md](PLUGIN_ARCHITECTURE_V4.md) — **현재 플러그인 아키텍처(V4)**: native Go stage 레지스트리 + goja 스크립트 stage
- [STAGE_SCHEMA_ARCHITECTURE.md](STAGE_SCHEMA_ARCHITECTURE.md) — Stage 스키마 기반 GUI 자동생성 시스템

### 참조 · 가이드
- [STAGE_SCHEMA_USER_GUIDE.md](STAGE_SCHEMA_USER_GUIDE.md) — 새 Stage 추가 가이드
- [STAGE_IMPLEMENTATION_STATUS.md](STAGE_IMPLEMENTATION_STATUS.md) — 내장 Stage 구현 현황
- [standalone-usage.md](standalone-usage.md) — pipeline-core 단독 실행 가이드
- [E2E_TEST_SCENARIOS.md](E2E_TEST_SCENARIOS.md) — E2E 테스트 시나리오

### 로드맵 · 남은 작업
- [PIPELINE_EXTENSION_PLAN.md](PIPELINE_EXTENSION_PLAN.md) — 파이프라인 확장 로드맵(구현 현황 포함)
- [REMAINING_WORK_CHECKLIST.md](REMAINING_WORK_CHECKLIST.md) — 남은 작업 체크리스트

## archive/

완료됐거나 폐기된 문서. 히스토리 참고용이며 **현재 설계를 반영하지 않는다**:
- 완료된 스프린트 계획/진행: `PHASE*`, `WORK_PLAN_*`, `WORK_PROGRESS*`, `REMAINING_WORK_PLAN`, `MULTI_CLUSTER_IMPLEMENTATION_PROGRESS`, `OPERATOR_CONTROL_RESTORE_PLAN`
- 폐기된 아키텍처: `PLUGIN_ARCHITECTURE_PLAN_V2`(Docker), `PLUGIN_ARCHITECTURE_V3`(gRPC go-plugin), `PLUGIN_DEVELOPMENT_GUIDE`(V2 컨테이너 방식), `technical-design-review`(actor 엔진 선택 — actor는 이후 제거됨)

## 주요 아키텍처 사실 (혼동 방지)

- **실행 엔진**: `GroupExecutor` 단일. actor 엔진은 제거됨. (archive의 actor 관련 서술은 무효)
- **플러그인**: V4만 유효. V2(Docker)/V3(gRPC)는 폐기.
- **Stage 실행**: 워크플로우 executor가 stream 레지스트리로 위임 → 28개 내장 stage + native plugin이 워크플로우에서 실행됨.
