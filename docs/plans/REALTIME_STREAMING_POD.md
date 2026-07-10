# Realtime 전용 Streaming Pod — 설계·구현 계획 (A안 확정)

> 작성 2026-07-10. 대상: 이 작업을 이어서 구현할 Claude Code / 개발자.
> 목적: web-ui 로 만든 native custom stage 를 realtime pipeline 에도 적용. bulk 는 매 실행 새 pod 라
> 최신 stage 바이너리를 자동으로 받는데, realtime 은 agent in-process 라 못 받던 것을 해결한다.

## 0. 핵심 판정 (사용자 질문에 대한 답 — 실측 근거)

- **Go/설계의 한계 아님.** realtime·bulk 는 **같은 엔진(GroupExecutor)** 을 쓰고 stage 호출은 둘 다 **인프로세스 네이티브 method 호출**(`native_stage_adapter.go:76`, 레코드마다, IPC 없음). realtime 이 새 stage 를 못 받는 건 "이미 켜진 agent 안에서 도는 선택" 때문이지 언어 한계가 아니다.
- **성능 벤치(실측, 2026-07-10)**: 네이티브 직접호출 36 ns/op vs Go plugin(.so) 125 ns/op. 둘 다 나노초 — 실제 stage 처리 대비 무시 가능. **어느 방식도 Python 같은 성능 저하 없음.**
- **채택 = A안**: realtime 을 "안 죽고 계속 도는 전용 pod" 로 분리. bulk 의 검증된 "새 pod = 최신 stage 바이너리 주입" 인프라를 그대로 확장. stage 는 여전히 네이티브 인프로세스(성능 100%). (기각: Go plugin .so — 버전 완전일치 강제·언로드 불가·panic 이 프로세스 전체 죽임. 멀티테넌트 운영 리스크.)

## 1. 확정된 결정 (사용자)

| # | 결정 |
|---|------|
| C1 | **명령 전달(stop/pause/resume)**: streaming pod 가 **REST** 로 수신(health server 확장). Redis 의존 없음. |
| C2 | **pod 수명주기 관리 주체**: **agent** 가 K8s Deployment 생성/삭제/rolling(batch Job 위임 구조 재사용, agent 는 이미 in-cluster K8s client 보유). |
| C3 | **K8s 리소스**: **Deployment**(replicas=1/파티션그룹) — 무한 실행이라 Job(완료 후 종료)이 아니라 Deployment(RestartPolicy=Always). |
| C4 | stage 실행은 **네이티브 인프로세스** 유지 — 성능 포기 없음. |

## 2. 아키텍처

```
현재:  realtime 명령 → agent.executeGroup (in-process GroupExecutor) → 새 stage 못 받음

A안:   realtime 명령 → agent → CreateStreamingDeployment (K8s)
                                  └─ pod:
                                     - initContainer(fetch-runner): CP 에서 RunnerVersionID 바이너리 주입 [bulk 재사용]
                                     - main: pipeline-batch-job, EXECUTION_MODE=streaming → runStreaming [기존]
                                     - stage: 네이티브 인프로세스(36ns) [성능 100%]
                                     - health/command REST 서버(:8082): stop/pause/resume 수신 [신설]
       stop/pause/resume: CP/agent → pod REST → GroupExecutor 제어
       상태/모니터링: pod → CP REST 보고
       native stage 변경: 새 RunnerVersion → agent 가 Deployment pod template 갱신 → rolling
                          → 구 pod graceful stop(checkpoint flush) → 신 pod 가 checkpoint offset 재개
```

## 3. 재사용 (조사로 확인 — 만들지 말 것)

- **runStreaming 무한 실행 + checkpoint + graceful shutdown**: `pipeline-batch-job/internal/runner/runner.go:86-146`. EXECUTION_MODE=streaming 으로 진입(`config/loader.go:55`). SIGTERM→ctx.Done→GroupExecutor.Stop→최종 checkpoint flush.
- **바이너리 주입 initContainer(fetch-runner)**: `k8s/job_manager.go:167-188`(RunnerVersionID → wget→gunzip→/runner/pipeline-batch-job, main command override). bulk e2e 검증됨.
- **agent 의 위임 구조**: `agent.go delegateBatchJob:661-728` (cmd→spec 추출: pipelinesJSON/jobConfig/AssignedPartitions/RunnerVersionID). agent 가 in-cluster K8s client 보유.
- **파티션 분산**: `workflow_partition.go publishSubExecutions:99-172` — realtime 도 이미 sub-execution 발행(type 무관). AssignedPartitions 전달됨.
- **health server**: `runStreaming` 이 이미 health server 기동(runner.go:35 근처). command REST 는 여기에 엔드포인트 추가.
- **RunnerVersion resolve**: `runner_resolver.go` — native stage → 최신 ready 바이너리 ID.

## 4. 신설 (조사로 확인 — 이것만 만든다)

- **S1. `CreateStreamingDeployment`** (`k8s/job_manager.go`): CreateBatchJob(:52-196) 기반, 차이만:
  - Job→Deployment, RestartPolicy=Always, replicas(파티션그룹당 1), TTLSecondsAfterFinished 제거.
  - EXECUTION_MODE=streaming 환경변수.
  - initContainer(fetch-runner) + env(EXECUTION_ID/WORKFLOW_ID/PIPELINES_CONFIG/ASSIGNED_PARTITIONS/RunnerVersionID) 는 CreateBatchJob 과 동일.
  - liveness/readiness probe(health server), terminationGracePeriodSeconds 크게(checkpoint flush 여유).
- **S2. `DeleteStreamingDeployment`** (`k8s/job_manager.go`): DeleteJob(:300-314) 동형. execution/workflow 로 Deployment 이름 규칙(예: `conduix-rt-{workflowID}` 또는 sub 별).
- **S3. agent realtime 분기 변경** (`agent.go:650-658`): realtime 도 in-process(executeGroup) 대신 `CreateStreamingDeployment` 위임. (in-process 경로는 비-K8s standalone 폴백으로만 남길지 결정 — §7 Q.)
- **S4. runner command REST 서버** (`pipeline-batch-job` runner): health server 에 `POST /commands`(stop/pause/resume) 추가 → 자기 GroupExecutor.Stop/Pause/Resume 호출. runStreaming 이 GroupExecutor 참조 보유하므로 연결.
- **S5. runner 상태 보고** (runner): 시작/주기 하트비트/종료를 CP REST 로. batch 는 최종 결과만 콜백(runner.go:192-244) — streaming 은 주기 보고 추가.
- **S6. CP StopWorkflow → pod 삭제 경로** (`workflow.go:643-691`): stop 시 agent 에 명령 → agent 가 DeleteStreamingDeployment. execution 에 Deployment 이름 기록.
- **S7. 차단 해제** (`workflow.go:393`): REALTIME_NATIVE_UNSUPPORTED 제거, realtime 도 RunnerVersionID resolve·전달.
- **S8. native 변경 rolling**: 새 RunnerVersion ready → 실행 중 realtime 의 Deployment pod template(fetch URL의 versionID) 갱신 → K8s rolling. 구 pod graceful stop(checkpoint) → 신 pod 재개.

## 5. 제어 흐름 (C1: REST)

```
stop:    CP StopWorkflow → agent → pod REST POST /commands {stop}  (+ agent 가 Deployment 삭제)
pause:   CP → agent → pod REST /commands {pause} → GroupExecutor.Pause (상태 유지, scale-0 아님)
resume:  CP → agent → pod REST /commands {resume}
monitor: pod → CP REST (stage 처리량/offset), 주기 하트비트
```
- pod 주소 발견: agent 가 Deployment 생성 시 pod/service 주소를 알거나, headless service/label 로 조회. (C1 단점 — §7 Q2.)

## 6. 구현 단계 (독립 검증) — 상태: W1~W5 구현 완료 (2026-07-10)

- **W1. CreateStreamingDeployment + DeleteStreamingDeployment** (S1,S2). ✅ 구현+단위테스트. Recreate 전략(Q4)·probe·container port 포함.
- **W2. runner command/상태 REST** (S4). ✅ 구현. health server 에 `POST /commands`(stop/pause/resume). e2e 로 pause/resume/invalid 실동작 확인. (S5 주기 상태보고는 미구현 — CP 가 execution status 를 이미 추적하므로 비필수, 후속.)
- **W3. agent realtime 위임 전환 + 차단 해제** (S3,S7). ✅ 구현+e2e. realtime+native(RunnerVersionID 有)→ `delegateStreamingDeployment`. non-native realtime 은 in-process 유지(Q1). workflow.go REALTIME_NATIVE_UNSUPPORTED 제거.
- **W4. stop/pause/resume 배선 + pod 삭제** (S6, C1 흐름). ✅ 구현+e2e. CP StopWorkflow→agent→pod REST stop + DeleteStreamingDeployment. 파티션 다중 sub-execution 전부 제어(matchingExecutions). pod IP 발견=execution-id 라벨 조회(Q2 확정).
- **W5. native 변경 rolling + 무손실** (S8). ✅ rolling API 구현. `POST /workflows/:id/roll`→RunnerVersion resolve→PublishWorkflowRollCommand→agent UpdateStreamingDeployment(initContainer URL 교체). Recreate 로 겹침 없음(Q4). **자동 rolling(reconciliation loop)은 후속** — 사용자 결정으로 수동 트리거 API 만.

### e2e 검증 결과 (2026-07-10, conduix-e2e)
- ✅ realtime+native 워크플로우 start → `conduix-rt-*` Deployment 생성, injected 바이너리로 `runStreaming` 진입, native `pricedouble` stage compile-in, health server `/health`·`/ready`·`/commands` 동작(:8082).
- ✅ W4 stop: CP stop → agent "stopped streaming execution" 로그 → pod Terminating + Deployment 삭제(~2s).
- ✅ W2 pause/resume: pod REST `/commands` 직접 호출 → `{"success":true}` + "pause/resume command received" 로그. invalid→500.
- ⚠️ **RBAC 신규 필요(발견·수정)**: agent SA 에 `apps/deployments` create/get/list/watch/update/patch/delete 권한 추가(helm rbac.yaml). 없으면 CreateStreamingDeployment 가 403.
- ⚠️ **미해결(W1~W5 무관, 별도 이슈)**: kafka-go ConsumerGroup 이 topic(1 partition) 에서 0 partition 배정 → 데이터 미소비. streaming pod 라이프사이클·제어는 정상, 데이터플레인 kafka source offset/group 배정 이슈로 별도 조사 필요. checkpoint 로드 401(auth 헤더 누락)도 기존 이슈.

### 빌드 사실(중요)
- streaming pod 는 **injected RunnerVersion 바이너리**를 실행(image 아님). W2 runStreaming 변경을 반영하려면 **새 RunnerVersion 빌드 필요**.
- RunnerVersion source_hash 는 **plugin 소스 기준** — pipeline-batch-job harness 코드 변경은 hash 를 바꾸지 않아 빌드 스킵됨. harness 변경 반영하려면 plugin 소스를 건드려 hash 를 바꿔야 재빌드(향후 harness 버전을 hash 에 포함 검토).

## 7. 열린 질문 (구현 중 결정)

- **Q1 in-process 경로 존치**: 비-K8s(standalone) realtime 을 위해 agent in-process(executeGroup)를 폴백으로 남길지, 완전 제거할지. (agent.go:738-742 getJobManager nil 허용). 추천: native stage 없는 realtime 은 in-process 유지, native 있으면 streaming pod.
- **Q2 pod 주소 발견(C1)**: REST 명령을 보내려면 pod 주소 필요 — per-workflow headless service vs label 조회 vs agent 가 pod IP 캐시. W2 에서 확정.
- **Q3 파티션→pod 수**: 파티션 그룹당 pod 1개(1:1) vs 그룹핑. bulk 현행 1:1 재사용 시작, 낭비되면 그룹핑(후속).
- **Q4 rolling replicas**: Deployment replicas=1 이면 rolling 중 순간 2 pod(구/신) — checkpoint claim 으로 이중 실행 차단 필요(bulk 의 SETNX claim 재사용 가능한지 확인).

## 8. 관련
- memory: `realtime-native-design-decision`(A안 이력), `native-plugin-external-import`(레지스트리+e2e), `native-plugin-build-pitfalls`, `pipeline-execution-paths`, `custom-stage-plugin-flow`.
- 커밋: f0d7e07/ef66782/dc08fac/ee443c4(의존성 레지스트리 D1~D5), c471f86(빌드 프로세스그룹).
