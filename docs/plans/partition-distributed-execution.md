# 설계안: Partitioned Source 의 다중 노드 분산 실행

> 상태: **설계 완료 · 구현 미착수**. 목적: partitioned source 의 각 파티션을 **여러 agent/K8s Job 으로 분산 실행**해, 단일 노드 메모리·CPU 한계를 넘는 스케일아웃을 가능하게 한다.
> 이 문서는 **현재 코드에서 확인된 사실**을 진단으로 깔고, 그 위에 변경 지점을 구체화한다. 진단 부분은 file:line 근거, 설계 부분은 제안이다.

## 진행 상태 (이번 goal)

- [x] 설계(진단 + 계층별 변경 지점 + 구현 순서)
- [x] 1단계: 타입 필드(ParentExecutionID/AssignedPartitions) 추가 + 상위호환 no-op
- [x] 2단계: executor 파티션 필터(WithAssignedPartitions) + 분할기(control-plane planPartitionGroups/publishSubExecutions) — 단위검증
- [x] 3단계: 취합기(aggregateSubExecutionResult, == 완료판정) + DB 스키마(ParentExecutionID/Total·CompletedSubExecutions, AutoMigrate)
- [ ] 4단계: Batch(K8s Job) 분산 — 현재 realtime/agent 발행 경로만(sub-execution 이 batch 면 각자 Job 위임되나 미검증)
- [ ] 5단계: 부하 균형·재개·고아 감지
- [ ] 6단계: 실 다중 agent/Job 분산 + 취합 e2e (현재 단위/로직 검증까지)

## 1. 문제 (현재 진단, 코드 확인)

partitioned source(`partitioned_sql`, `partitioned_http`, Kafka partition, CDC table shards)의 파티션들은 **한 파이프라인 프로세스 안의 goroutine 으로만 병렬 실행**된다. 여러 노드로 분산되지 않는다.

| 계층 | 현재 동작 | 근거 |
|------|-----------|------|
| Control-Plane 배정 | 워크플로우 → cluster **1개** 배정, Redis 채널 1개 발행 | `execution_cluster.go` `ResolveExecutionCluster`, `redis_service.go` `PublishWorkflowExecution` |
| Worker claim | execution → agent **1개**만 SETNX 소유권 | `agent.go` `claimExecution` |
| Batch 위임 | 파이프라인 전체 = K8s Job **1개**, `PIPELINES_CONFIG` 통째 전달 | `agent.go` `delegateBatchJob`, `k8s/job_manager.go` `CreateBatchJob` |
| Executor | 파티션마다 goroutine, 단일 채널 merge (한 프로세스) | `group_executor.go` `runMultiPartitionSource` |

**결과**: 파티션이 N개여도 CPU/메모리는 **한 노드**의 것. throughput 은 그 노드의 코어 수·메모리에 갇힌다. Spark/Flink 처럼 파티션을 여러 executor 노드에 흩뿌리지 못한다.

**분산에 유리한 기존 자산(그대로 쓸 수 있음):**
- 체크포인트가 이미 `PipelineID:PartitionKey` 단위로 저장됨 (`checkpoint/client.go`, `cdc.go` `GetSourceCheckpoints`). → 파티션별 독립 재개 가능.
- 파티션별 config 병합이 이미 있음 (`runMultiPartitionSource` 의 `maps.Copy(config, p.Config)`). → 파티션을 독립 실행 단위로 떼어내기 쉬움.
- cluster/label 기반 agent 라우팅, execution claim(SETNX), 결과 REST 보고 경로가 이미 있음. → sub-execution 에 재사용.

## 2. 목표 / 비목표

**목표**
- partitioned source 의 각 파티션(또는 파티션 그룹)을 **별도 실행 단위(sub-execution)** 로 여러 agent/Job 에 분산.
- 파티션별 독립 체크포인트로 부분 실패·재개.
- 부모 execution 이 sub-execution 결과를 취합해 기존 `GroupExecutionResult` 로 보고(상위 호환).

**비목표(이 설계 범위 밖)**
- 분산 셔플/spill-to-disk(대용량 GROUP BY·JOIN). 이건 여전히 Spark/Flink 영역 — 파티션 분산은 **소스 읽기·map-style 처리·싱크 적재의 스케일아웃**이지, cross-partition 상태 연산을 분산하는 게 아니다.
- 파티션 간 데이터 재분배(re-shuffle). 각 파티션은 자기 데이터만 읽어 자기 싱크로 적재(embarrassingly parallel 워크로드 전제).

## 3. 설계

### 3.1 실행 모델: fan-out sub-execution

```
Control-Plane
  │  워크플로우 실행 요청
  ▼
[분할기] partitioned source 감지 → 파티션 배열을 sub-execution N개로 분해
  │   (파티션 수가 많으면 그룹핑: maxSubExecutions 로 묶음)
  ├─ sub-exec #0 (partitions [0..k])  → cluster/label 라우팅 → agent A
  ├─ sub-exec #1 (partitions [k+1..2k]) → agent B
  └─ sub-exec #2 (...)                  → agent C
  │
  ▼ 각 sub-exec 는 기존 executor 경로로 자기 파티션만 실행
  │   (runMultiPartitionSource 를 "할당된 파티션 부분집합"으로 호출)
  ▼
[취합기] 모든 sub-exec 결과(GroupExecutionResult) 를 부모 execution 으로 병합
      (TotalRecords 합산, 하나라도 error 면 부모 error/partial)
```

### 3.2 계층별 변경 지점 (구체)

**(1) 타입 — `shared/types`**
- 발행 명령 `WorkflowExecutionCommand`(`shared/types/agent.go`)와 worker 처리 명령 `GroupExecutionCommand`(`delegateBatchJob`/`executeGroup` 가 받음) **둘 다** 에 분산 필드 추가:
  - `ParentExecutionID string` (sub-exec 이면 부모 id)
  - `AssignedPartitions []string` (이 sub-exec 이 처리할 파티션 ID 부분집합; 빈 값=전체=기존 동작)
- `PartitionConfig` 는 그대로. 분할은 ID 부분집합으로 표현.
- 상위 호환: 위 필드가 비면 **현재와 100% 동일 동작**(단일 실행).

**(2) 분할기 — control-plane `scheduler_service.go` / 신규 `partition_planner.go`**
- 실행 트리거 시 워크플로우 input 의 `Partitions` 를 검사.
- 분산 조건: `input.distributed: true`(신규 config) 또는 파티션 수 ≥ 임계값.
- 파티션을 `maxSubExecutions`(cluster 의 가용 agent 수 기반) 개 그룹으로 분할.
- 각 그룹마다 `WorkflowExecutionCommand{ParentExecutionID, AssignedPartitions}` 를 발행.
  - 라우팅: 기존 `cluster:<id>:execute` 채널 재사용. **여러 agent 가 각각 다른 sub-exec 을 claim** 하도록, sub-exec 마다 고유 `ExecutionID`.
- 부모 execution row 는 `status=running`, sub-exec 수·완료 수 추적 컬럼 추가(DB 마이그레이션).

**(3) claim — worker `agent.go`**
- 변경 최소. `claimExecution` 은 이미 execution 단위 SETNX 라 **sub-exec 마다 서로 다른 agent 가 자연히 나눠 가짐**(고유 ExecutionID 이므로).
- 주의: 같은 cluster 의 agent 가 여러 sub-exec 을 **골고루** 가져가도록, 이미 하나 실행 중인 agent 는 claim 을 양보하는 힌트(부하 인지) 고려 — 없어도 SETNX 경쟁으로 분산은 되나 편중 가능.

**(4) executor — `group_executor.go` `runMultiPartitionSource`**
- 시그니처에 "할당 파티션 필터" 반영: `AssignedPartitions` 가 있으면 그 부분집합만 goroutine 기동.
- 나머지 로직(체크포인트 파티션 단위, config 병합) 그대로 → 각 노드가 자기 파티션만 읽고 자기 싱크로 적재.

**(5) 취합기 — control-plane 결과 핸들러**
- sub-exec 의 `reportGroupExecutionResult` 수신 시 `ParentExecutionID` 로 그룹핑.
- 부모의 sub-exec 완료 카운트 증가, `TotalRecords/FailedRecords` 합산.
- 모두 완료 → 부모 `status=completed`(하나라도 error → `error` 또는 `partial`, 정책화).
- 기존 단일 execution 보고 경로는 그대로(상위 호환).

**(6) Batch(K8s Job) 경로 — `job_manager.go`**
- sub-exec 마다 별도 Job 생성(파티션 부분집합만 담은 config). Job = sub-exec 단위로 변경.
- 부모는 Job 들의 완료를 취합(위 (5)와 동일 경로).

### 3.3 실패·재개
- 파티션별 체크포인트(`PipelineID:PartitionKey`)가 이미 있으므로, sub-exec 이 죽으면 **그 파티션 부분집합만** 재실행(committed offset 부터). 다른 파티션 진행에 영향 없음.
- 부모 execution 의 고아 감지: 기존 `detectStaleExecutions`(scheduler)를 sub-exec 에도 적용.

## 4. 단계별 구현 순서 (제안)

1. **타입 + 상위호환 no-op**: 필드 추가하되 분할기 미동작(전체를 sub-exec 1개로). 기존 동작 불변 검증.
2. **분할기(realtime 먼저)**: 파티션을 sub-exec 으로 쪼개 발행 + executor 파티션 필터. 결과 취합.
3. **취합기 + DB 스키마**: 부모/자식 상태 추적, 결과 병합, partial 정책.
4. **Batch(K8s Job) 분산**: sub-exec = Job.
5. **부하 균형·재개·고아 감지** 보강.
6. **e2e**: 여러 agent(또는 여러 Job)로 파티션이 실제로 나뉘어 실행되고, 결과가 취합되며, 한 sub-exec 실패 시 그 파티션만 재개되는지 실환경 검증.

## 5. 리스크 / 미해결 질문

- **부하 편중**: SETNX 경쟁만으로는 한 agent 가 여러 sub-exec 을 쥘 수 있음. 라운드로빈/부하 인지 배정이 필요할 수 있음(§3.2-(3)).
- **파티션 수 폭발**: 파티션이 수백 개면 sub-exec/Job 수를 그룹핑으로 제한해야 함(`maxSubExecutions`).
- **결과 취합의 부분 실패 의미론**: 일부 파티션만 성공한 배치를 "성공"으로 볼지 "partial"로 볼지 — 워크로드(멱등 upsert면 재실행 안전) 따라 정책 필요.
- **cross-partition 연산 불가 명시**: 분산 후에는 파티션 경계를 넘는 집계/조인이 각 노드 로컬로만 됨 → 전역 집계가 필요하면 이 모델로 부족(Spark/Flink). 사용자 문서에 못 박아야 함.
- **비목표 재확인**: 이 설계는 "소스 읽기·map·싱크"의 스케일아웃이지 분산 셔플이 아니다. 대용량 셔플 요구가 진짜라면 이 설계로는 해결 안 됨.

## 6. 대안 (설계 채택 전 검토)

- **대안 A(현행 유지 + 문서화)**: 파티션은 노드 내 병렬로 두고, "대용량은 Spark/Flink" 로 경계를 명확히. 구현 비용 0, 스케일아웃 없음.
- **대안 B(이 설계, sub-execution fan-out)**: embarrassingly-parallel 워크로드(여러 저장소 이동/적재)에 한해 스케일아웃. 셔플은 여전히 못 함.
- **대안 C(임베드 분산 프레임워크)**: 범위 초과. Conduix 의 "단일 바이너리·server-local" 설계 철학과 충돌.

→ 권장: **대안 B**. 단, "분산 셔플이 아니라 파티션 fan-out" 이라는 경계를 문서·config 이름(`distributed`)으로 분명히 한다.
