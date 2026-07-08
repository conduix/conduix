# 남은 작업 (TODO)

> 이 문서 하나가 **지금 해야 할 일** 전부다. 완료 이력은 git 커밋에 있다(문서로 안 남김).
> 원칙: 파이프라인 조합·워크플로우로 되는 건 작업이 아니다(사용법). 아래는 **실제 코드/검증이 남은 것만**.

## 1. Bulk 파티션 분산 — 실 멀티노드 e2e 검증 ✅ 완료(2026-07-08)

**결과**: agent replica 2 환경에서 partitions=2 bulk 워크플로우(mock MySQL id%2 even/odd) 트리거로 검증 완료.
- [x] 파티션이 **여러 agent 로 분산** — sub 2개가 서로 다른 agent(rdzll, 5r66r)에서 실행, 파티션별 정확 분할(even total=1, odd total=2).
- [x] 부모 **취합·완료** — total_records=3(중복0), completed_sub=2/2, status=completed.
- [x] **부분 실패 시 부모 비-갇힘** — sub 하나 실패 시 부모 error 로 취합 완료.

**검증 중 발견·수정한 실행 경로 버그 3종**(단위테스트 통과했으나 통합에서만 드러남 — [[bulk-partition-distribution-bugs]]):
1. batch runner 파티션 필터 누락(`executeWorkflow` 에 `WithAssignedPartitions` 빠짐) → 각 sub 가 전체 실행·데이터 중복. **수정**.
2. bulk 취합 누락(`handleJobResult` 에 `aggregateSubExecutionResult` 없음) → 부모 영구 running 갇힘. **수정**.
3. scheduler stale 오판(bulk Job 이 heartbeat 미등록 → 2분 후 정상 Job 을 error 확정) → batch 를 stale 감지에서 제외. **수정**.

**함께 추가한 관측성**(사용자 요구): sub-execution 별 실행 agent 기록(`agent_id`) — worker/batch-job→콜백→execution.agent_id→web-ui 노출. "어느 노드/몰림" 조회 가능.

**참고 코드**: control-plane `handlers/workflow_partition.go`(분할기/취합기), `services/scheduler_service.go`(고아 감지 `advanceParentOnStaleSub`), pipeline-core `executor/group_executor.go`(`WithAssignedPartitions`), pipeline-worker `k8s/job_manager.go`(ASSIGNED_PARTITIONS env), pipeline-batch-job `runner/runner.go`(executeWorkflow 파티션 필터).

## 2. (후순위) Bulk 분산 부하 균형

**상태**: 현재 sub-execution 을 SETNX 경쟁으로 여러 agent 가 나눠 claim — 분산은 되나 편중 가능.

**할 일**: 라운드로빈 또는 부하 인지(heartbeat 기반) 배정. **필수 아님** — 1 검증 후 편중이 실제 문제일 때만.

---

## 하지 않는 것 (사용법으로 되거나 비목표 — 작업 아님)

- **다중 컨슈머 fan-out**: realtime 파이프라인 여러 개 등록.
- **초기적재+CDC**: workflow 에 bulk+CDC 파이프라인 + `ExecutionMode: parallel`.
- **분산 GROUP BY/JOIN(셔플)**: 파이프라인1(`partitioning=key` Kafka sink) → Kafka → 파이프라인2(consumer group 병렬 소비 + `group_by`). 전제(key 파티셔닝)는 구현·검증 완료.
- **DDL 무시**: `on_ddl: allow` config.
- **윈도우 상태 복구**: 이미 자동(Redis restore) + join 은 replay 로 복구.
- **exactly-once 2PC / 초대용량 spill / 무한 로그 보존**: 비목표(Flink/Kafka 영역). Conduix 는 at-least-once+멱등 수렴으로 대체.
