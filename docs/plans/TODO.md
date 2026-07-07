# 남은 작업 (TODO)

> 이 문서 하나가 **지금 해야 할 일** 전부다. 완료 이력은 git 커밋에 있다(문서로 안 남김).
> 원칙: 파이프라인 조합·워크플로우로 되는 건 작업이 아니다(사용법). 아래는 **실제 코드/검증이 남은 것만**.

## 1. Bulk 파티션 분산 — 실 멀티노드 e2e 검증

**상태**: 기능 배선은 전부 완료(타입·분할기·executor 필터·취합기·batch Job 파티션 전달·고아 감지, 단위 검증). **실제 여러 agent 로 나뉘어 도는지 검증만 남음.**

**할 일**:
- e2e 스택(conduix-e2e)에서 **agent replica ≥ 2** 로 올리고 최신 코드 이미지 배포(`make e2e-restart`).
- partitioned source(파티션 ≥ 2) 워크플로우를 트리거.
- 검증:
  - [ ] 파티션이 **여러 agent 로 나뉘어** 실행되는가(sub-execution 별 다른 agent claim).
  - [ ] 부모 execution 이 sub 결과를 **취합**해 완료 처리되는가(total_records 합산, 완료판정).
  - [ ] sub 하나 실패(agent kill) 시 그 파티션만 재개되고 부모가 갇히지 않는가.

**참고 코드**: control-plane `handlers/workflow_partition.go`(분할기/취합기), `services/scheduler_service.go`(고아 감지 `advanceParentOnStaleSub`), pipeline-core `executor/group_executor.go`(`WithAssignedPartitions`), pipeline-worker `k8s/job_manager.go`(ASSIGNED_PARTITIONS env).

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
