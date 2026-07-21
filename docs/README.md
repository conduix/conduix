# Conduix 문서 인덱스

> 이 디렉토리에는 **현재 유효한** 설계·참조·가이드 문서만 둔다.
> 완료된 작업 계획/진행 로그와 폐기된 아키텍처 문서는 [`archive/`](archive/)로 옮겨 현재 문서와 섞이지 않게 한다.

## 먼저 읽기

- **[COMPARISON.md](COMPARISON.md) — 왜 Conduix인가: Airflow/Flink/Kafka Connect/NiFi 대비 선택 가이드 (신규 사용자·의사결정자용)**
- **[ARCHITECTURE.md](ARCHITECTURE.md) — 프로젝트 가치·설계·구현 핵심 요약 (단일 진입점)**

## 현재 유효 문서

### 아키텍처 · 설계
- [adr/](adr/) — **Architecture Decision Records**: 주요 결정의 "왜"와 트레이드오프 (Bento 채택 / Actor 제거 / 플러그인 V2→V4)
- [design-v2.md](design-v2.md) — 파이프라인 근간 설계(배치/실시간, 소스, Input→Stage→Output 모델)
- [EXECUTION_TOPOLOGY_INTENT.md](EXECUTION_TOPOLOGY_INTENT.md) — **실행 토폴로지 설계 결정(D1~D6)**: 멀티 K8s 위임 구조의 의도·트레이드오프
- [PLUGIN_ARCHITECTURE_V4.md](PLUGIN_ARCHITECTURE_V4.md) — **현재 플러그인 아키텍처(V4)**: native Go stage 레지스트리 + goja 스크립트 stage
- [STAGE_SCHEMA_ARCHITECTURE.md](STAGE_SCHEMA_ARCHITECTURE.md) — Stage 스키마 기반 GUI 자동생성 시스템

### 참조 · 가이드
- [STAGE_SCHEMA_USER_GUIDE.md](STAGE_SCHEMA_USER_GUIDE.md) — 새 Stage 추가 가이드
- [standalone-usage.md](standalone-usage.md) — pipeline-core 단독 실행 가이드
  (내장 Stage 구현 현황 스냅샷은 archive/STAGE_IMPLEMENTATION_STATUS.md — 현행은 ARCHITECTURE.md)

### 현행 작업
- [REMAINING_WORK_CHECKLIST.md](REMAINING_WORK_CHECKLIST.md) — 남은 작업 체크리스트
- [plans/](plans/) — **진행 중** 작업 계획만 둔다(완료되면 archive/로 이동). 현재: `plans/TODO.md`.

> 현재 지원 기능·구현 현황은 [ARCHITECTURE.md](ARCHITECTURE.md)를 본다. 시점 스냅샷 문서는 archive에 있다.

## archive/

완료됐거나 폐기된 문서, 또는 **특정 시점 현황/계획 스냅샷**. 히스토리 참고용이며 **현재 상태를 반영하지 않는다**:
- 완료된 스프린트 계획/진행: `PHASE*`, `WORK_PLAN_*`, `WORK_PROGRESS*`, `REMAINING_WORK_PLAN`, `MULTI_CLUSTER_IMPLEMENTATION_PROGRESS`, `OPERATOR_CONTROL_RESTORE_PLAN`
- 완료된 설계·구현 계획(2026-07): `REALTIME_STREAMING_POD`(realtime 전용 streaming pod, W1~W5 완료), `MULTI_CLUSTER_E2E_PLAN`(멀티클러스터 e2e, 버그 6종 수정), `NATIVE_PLUGIN_WIP`(native stage e2e 갭), `CUSTOM_STAGE_DEPENDENCY_REGISTRY`(allowed_modules 레지스트리, 구현됨) → 현행은 ARCHITECTURE.md
- 시점 스냅샷/계획: `PIPELINE_EXTENSION_PLAN`(확장 계획, 2026-03-04 현황), `STAGE_IMPLEMENTATION_STATUS`(2026-02-11 현황) → 현행은 ARCHITECTURE.md
- 폐기된 아키텍처: `PLUGIN_ARCHITECTURE_PLAN_V2`(Docker), `PLUGIN_ARCHITECTURE_V3`(gRPC go-plugin), `PLUGIN_DEVELOPMENT_GUIDE`(V2 컨테이너 방식), `technical-design-review`(actor 엔진 선택 — actor는 이후 제거됨), `E2E_TEST_SCENARIOS`(V2 플러그인 기반 시나리오)

## 주요 아키텍처 사실 (혼동 방지)

- **실행 엔진**: `GroupExecutor` 단일. actor 엔진은 제거됨. (archive의 actor 관련 서술은 무효)
- **플러그인**: V4만 유효. V2(Docker)/V3(gRPC)는 폐기.
- **Stage 실행**: 워크플로우 executor가 stream 레지스트리로 위임 → 28개 내장 stage + 커스텀 stage 2종(native Go compile-in / js_script goja)이 워크플로우에서 실행됨.
- **realtime 실행**: realtime+native custom stage는 **streaming Deployment(상주 Pod)** 위임(최신 stage 바이너리 주입). non-native realtime만 worker in-process. batch는 K8s Job. worker는 오케스트레이터이고 유실 명령은 reconcile 백스톱으로 복구. (과거 "realtime=in-process"만 있던 서술은 낡음)
