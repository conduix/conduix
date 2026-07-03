# 실행 토폴로지 — 의도·가치 우선 설계

> **이 문서는 코드가 아니라 의도를 먼저 확정하기 위한 것이다.**
> 원칙: *의도와 가치가 먼저이고, 코드·구현은 그것을 반영하는 도구다.*
> 이 문서가 구현·이름·다이어그램의 기준(single source of truth)이다.
> 상태: **확정** (2026-07-02). 아래 §6에서 미결정을 결정으로 전환. 구현은 이 문서를 따른다.
>
> 의도 확정 근거: 사용자가 "daemon 위임 구조로 구현하는 게 맞는데 이건 누락된 것이다.
> 이름도 재정의하라"고 명시 지시. → 위임 구조·이름 재정의는 합의 대기가 아니라 **내려진 지시**다.
> 세부 구현 선택(D1~D3)은 코드로 답할 수 있어 아래에서 스스로 결정한다(틀리면 되돌린다).

---

## 1. 왜 이 문서가 필요한가 (누락의 원인)

기존 리뷰는 **코드 → 문서** 순서로 진행됐다. "다이어그램이 코드와 일치하는가"만 검증했고, "코드가 의도를 반영하는가"는 묻지 않았다. 그 결과:

- control-plane이 **자기 단일 cluster에 K8s Job을 직접 생성**하는 현행 코드를 "정상 동작"으로 문서화했다.
- 코드에 남아 있던 **의도의 흔적**(아래)을 "데드코드/미구현"으로만 분류하고 버렸다. 사실 이것들은 *"원래 위임 구조로 가려다 멈춘 자취"* 였다.

**의도라는 렌즈가 없으면, 미완성 의도는 그냥 쓰레기 코드로 보인다.** 그래서 순서를 바로잡는다: 의도 확정 → gap 도출 → 구현 → 이름·문서 정합.

---

## 2. 의도 (Intent)

**멀티 K8s 환경을 지원한다.** control-plane은 여러 K8s cluster를 알고(관리·제어), 각 cluster에 소속된 worker 그룹에게 **"이 워크플로우를 실행하라"를 위임**한다. K8s Job을 만드는 것은 **각 cluster 안의 worker**이지, control-plane이 아니다.

**워크플로우 = 라이프사이클 단위 → 워크플로우마다 "실행할 cluster(그룹)" 지정이 있어야 한다.**
- 워크플로우는 `cluster_id`(그룹)를 갖는다. 이것이 **모든 실행의 라우팅 키**다(realtime·batch 공통).
- 미지정이면 **기본 cluster(default)로 폴백**한다.
- 폴백까지 해도 대상 cluster가 해결되지 않으면 **실행을 거부**한다(그룹 없이는 실행 불가).

```
control-plane (여러 cluster 인지 + 제어)
    │  워크플로우의 cluster_id(없으면 default)로 대상 그룹 결정
    │  "cluster C에서 이 워크플로우를 실행하라" (명령만, 채널 cluster:C:*)
    ▼
cluster C의 worker 그룹 (그룹 중 SETNX claim으로 1대)
    │  realtime → 상주 실행 (in-process)
    │  batch    → in-cluster 권한으로 K8s Job 생성 → 일회성 Pod 실행
    ▼
K8s Job / 상주 실행
    │  결과·상태 콜백(REST)
    ▼
control-plane (상태·결과 집계 = 진실원)
```

## 3. 가치 (Value) — 왜 위임이 옳은가

| 가치 | 위임 구조 | 직접 생성 구조(현행) |
|------|-----------|----------------------|
| **보안/크레덴셜** | control-plane이 각 cluster의 kubeconfig/token을 **보관하지 않음**. worker가 자기 cluster에 in-cluster 권한으로 생성. | control-plane이 모든 cluster 접속 크레덴셜을 중앙 보관 → 유출 시 전 cluster 위험. |
| **데이터·네트워크 로컬리티** | Job 생성·실행이 cluster 내부에서 완결. | control-plane이 원격 cluster API를 크로스로 호출. |
| **멀티 cluster 확장** | cluster 추가 = worker 그룹 배포. control-plane 변경 없음. | control-plane이 cluster별 클라이언트·라우팅을 중앙에서 관리. |
| **장애 격리** | 한 cluster API 장애가 control-plane이나 타 cluster에 전파되지 않음. | control-plane이 모든 cluster API의 가용성에 결합. |

→ **위임이 보안·로컬리티·확장·격리 모두에서 우월하다.** 현재 Cluster 모델이 kubeconfig를 저장하지 않고 메타데이터만 갖는 것도 이 방향과 정합한다(우연히 옳게 돼 있음).

## 4. 역할 재정의 (이름의 근거)

worker의 정체는 단순 "실행 데몬"이 아니다:

1. **cluster-scoped**: 특정 K8s cluster에 소속되어 그 cluster를 대표한다.
2. **상주(long-running)**: 실시간 파이프라인을 직접 실행하며 pause/resume·하트비트.
3. **control-plane의 cluster 내 대리자**: 위임받아 자기 cluster에 K8s Job을 만든다.

`daemon`은 (2)만 담고 (1)(3) — 특히 **위임 대리** 역할을 못 담는다. → §7에서 `pipeline-worker`로 확정.

## 5. 현재 상태 대비 gap (의도를 렌즈로 본 조각들)

의도의 조각 중 **이미 코드에 있는 것**(= 이게 "누락된 의도"라는 증거):

| 의도의 조각 | 현재 | 근거 |
|---|---|---|
| cluster별 worker 그룹핑 | ✅ 있음 | `cluster_id` register/heartbeat, `cluster:<id>:execute` 채널 pub/sub |
| 그룹 내 중복 실행 방지 | ✅ 있음 | SETNX claim (`agent.go:627`) |
| worker가 자기 cluster에 Job 생성하는 능력 | ✅ **활성화됨** | `job_manager.go`를 `delegateBatchJob`이 호출(구 데드코드) |
| cluster당 대표 1대 선출 | ❌ **삭제됨** | `leader/election.go` 제거(D1: claim 단일화, dead code 정리) |
| Cluster 메타데이터 모델 | ✅ 있음(위임에 적합) | `models.go` Cluster (kubeconfig 미보관) |
| Job 결과 콜백 수신 | ✅ 있음 | `workflow.go` HandleJobResultCallback (`/internal/job-result`) |
| **실제 살아있는 Job 생성 주체** | ❌ **의도와 반대** | control-plane 직접 생성 (`kubernetes_job_service.go:248`), `workflow.ClusterID` 무시 |

**의도와 반대로 구현된 유일한 핵심 = "누가 Job을 만드는가".** 나머지 인프라(그룹핑·claim·콜백·모델)는 위임 구조에 재사용 가능.

### 구현 gap 목록 (§6 결정 반영) — 진행 현황

- **G1 ✅**: control-plane이 직접 K8s Job 생성하던 분기 제거. realtime·batch 모두 대상 cluster 채널(`cluster:<id>:execute`)로 실행 명령 발행. batch 표시는 `WorkflowConfig.Type`, 리소스 스펙은 `cmd.JobConfig`로 전달. (`workflow.go` StartWorkflow, `agent.go` 추가 필드)
- **G2 ✅**: worker가 batch 명령 수신 시 SETNX claim 후 `delegateBatchJob`이 `job_manager.go`(in-cluster client)로 자기 cluster에 Job 생성. realtime은 기존 in-process `executeGroup`. leader election 대신 claim 단일화(D1) — **`leader/election.go` 삭제**. 사용되지 않던 `k8s/deployment_manager.go`도 함께 제거(D6로 자기 스케일 안 하므로 불필요).
- **G3 ✅**: Job Pod(pipeline-batch-job)가 `CALLBACK_URL`(`/internal/job-result`)로 결과 콜백 — 기존 `HandleJobResultCallback` 재사용. worker는 Job 생성만 하고 결과 감시는 안 함(control-plane이 콜백 수신).
- **G4 ✅**: control-plane WorkflowHandler에서 `startBatchJob`/`jobService`/Watch 제거(D2). **`KubernetesJobService`(파일 전체) 삭제** — `ClusterHandler.ScaleAgents`/`UpdateAgentConfig`도 K8s 직접 호출 제거하고 DB의 `DesiredAgents`(의도)만 기록. **control-plane은 이제 K8s 클라이언트를 전혀 갖지 않는다.** (실제 replica는 배포 차트가 반영 — [D6](#) 아래.)
- **G5 ✅**: RBAC — cluster-wide `pod-log-reader` ClusterRole(jobs/deployments/pods 포함)을 제거하고, 네임스페이스 한정 `worker-job-manager` **Role**(jobs create + pods/log read)로 교체. 최소 권한. control-plane 몫의 deployment-scale 권한 삭제.
- **G6 ✅**: 실행 시작에서 `resolveExecutionCluster`로 cluster 확정(지정→default→`errNoExecutionCluster` 4xx). `Cluster.IsDefault` 필드 추가. execution에 확정값 스냅샷. `CreateWorkflowRequest`·`UpdateWorkflowRequest`·YAML `WorkflowSpec` 모두 `cluster_id` 처리(이미 존재). web-ui 워크플로우 생성/수정 폼에 cluster selector 추가(default 폴백 안내).
- **G7 ✅**: 이름 재정의 `pipeline-daemon` → `pipeline-worker` 완료. 디렉토리·go.mod·import·Makefile·Dockerfile·lefthook·CI·문서 일괄, 5개 모듈 빌드+테스트 통과.
- **G8 ✅**: stop/pause/resume이 execution의 `ClusterID`로 라우팅하도록 교정(D5). `runningExecution`이 (ID, ClusterID) 반환.

## 6. 설계 결정 (확정)

의도는 사용자 지시로 확정됨. 아래 세부는 코드로 답할 수 있어 원칙에 따라 스스로 결정한다.

- **D1. 그룹 내 Job 생성 담당 = SETNX claim (확정)**
  근거: realtime 경로가 이미 SETNX claim(`agent.go:627`)으로 그룹 내 단독 실행을 보장하고 동작 중. "같은 정책(그룹 내 단독 실행)은 한 곳에서" 원칙상 batch도 동일 메커니즘을 재사용한다. leader election(K8s Lease, `election.go`)은 데드코드이고, 이를 살리면 동일 정책에 두 메커니즘이 공존(중복 정책)하게 되어 배제. → leader election은 제거 대상(§5 G2에서 정리).

- **D2. control-plane 직접 생성 경로 = 제거 (확정)**
  근거: control-plane이 K8s 크레덴셜을 보관하지 않는 것이 위임 구조의 핵심 가치(§3 보안). 직접 생성 코드(`kubernetes_job_service.go`의 Job Create)를 남기면 그 가치가 깨지고, "살아있는 경로가 의도와 반대"인 상태가 재발한다. 단일 cluster 개발 환경은 "control-plane과 같은 cluster에 worker 1대"로 동일하게 커버되므로 fallback 불필요.

- **D3. batch 위임 트리거 = "batch 워크플로우는 항상 위임 Job" (확정)**
  근거: `JobConfig != ""` 조건은 채울 수단(UI/API/YAML)이 없어 사실상 죽은 분기(도달 불가). 죽은 조건을 살리려 UI/API/YAML까지 확장하는 건 범위 확대이자 의도(위임)와 무관. `JobConfig`는 트리거에서 격하하여 **선택적 리소스 스펙(CPU/mem/namespace/재시도)** 으로만 쓴다(있으면 적용, 없으면 기본값). "어느 cluster에서?"는 D4가 결정한다.

- **D4. cluster 라우팅 = 워크플로우의 `cluster_id`(없으면 default), 미해결 시 실행 거부 (확정, 사용자 지시)**
  근거: 워크플로우가 라이프사이클 단위이므로 "실행 cluster 그룹"은 워크플로우의 필수 속성이다. `workflow.ClusterID` 필드는 이미 존재(`models.go:39`)하고 `TargetClusterID`로 명령에 전달까지 되지만, batch 분기가 이를 무시하고 control-plane이 직접 생성했다(= 살려야 할 의도의 화석). 결정:
  - 실행 시작 시 `cluster_id`를 확정한다: 지정 → 그 cluster / 미지정 → **default cluster** / 그래도 없음 → **실행 거부(4xx)**.
  - realtime·batch 모두 이 cluster의 채널(`cluster:<id>:*`)로만 명령을 보낸다.
  - default cluster를 표시할 수단이 필요하다: Cluster 모델에 `IsDefault` 플래그 추가(현재 없음). agent 등록 측의 `"default"` 문자열 관례(`cluster.go:110`)와 정합시킨다.

- **D5. cluster 지정은 실행 시점 스냅샷 (확정, 사용자 지시)**
  근거: 워크플로우의 `cluster_id`는 영구 고정이 아니라 **실행을 시작하는 순간 읽어 그 execution에 고정**된다. 다음 실행 전에 워크플로우의 그룹을 바꾸면 **다음 실행부터 새 그룹**에서 돈다. 진행 중인 execution은 시작 당시 cluster에 묶여 영향받지 않는다. 결정:
  - 실행 시작 시 D4로 확정한 cluster를 `WorkflowExecution.ClusterID`에 **복사 저장**하고, 이후 그 execution의 모든 제어(stop/pause/resume)·결과 라우팅은 **execution의 ClusterID**를 쓴다(워크플로우의 현재 값이 아니라).
  - 이 개념은 이미 코드에 존재: `WorkflowExecution.ClusterID`("실행 시점 클러스터 저장", `models.go:332`), `workflow.go:386`에서 복사. → 유지·강화한다.
  - **주의(현행 버그)**: 현재 stop/pause/resume은 `workflow.ClusterID`(현재 값)로 명령을 보낸다(`workflow.go:643,673,718`). 실행 시작 후 워크플로우 그룹을 바꾸면 **엉뚱한 cluster로 제어 명령**이 간다. execution.ClusterID 기준으로 교정해야 한다(G8).

- **D6. worker 수(스케일)는 배포 차트가 관리, control-plane은 의도만 기록 (확정, 사용자 지시)**
  근거: 사용자가 제시한 3안 중 "worker가 배포 시점에 그룹(`CLUSTER_ID`)을 갖고 접속하고, worker 수는 배포 차트(Helm/ArgoCD)로 관리" 방식을 채택. control-plane이 deployment를 스케일하면(구 `ScaleAgentDeployment`) K8s 크레덴셜이 필요해 D2와 충돌하고, worker가 0개일 때 "스케일 명령 받을 대상이 없는" 부트스트랩 문제가 생긴다. 결정:
  - `POST /clusters/:id/scale`·`PUT /clusters/:id/agent-config`는 DB의 `DesiredAgents`/`AgentConfig`(의도)만 갱신한다.
  - 실제 replica는 각 cluster의 배포 차트가 `DesiredAgents`를 반영해 맞춘다(control-plane 밖).
  - worker는 배포 시 `CLUSTER_ID`로 자기 그룹을 선언하고 control-plane에 register/heartbeat(agent 방식). control-plane은 그룹을 "인지"만 하고 "관리(배포)"하지 않는다.

## 7. 이름 재정의 (확정)

- **`pipeline-daemon` → `pipeline-worker` (확정, 적용 완료)**
  근거: 이 모듈은 특정 cluster에 소속되어 ① realtime 파이프라인을 상주 실행하고 ② control-plane의 위임을 받아 자기 cluster에 K8s Job을 만든다. 두 역할(실행+위임수행)을 포괄하는 중립어가 `worker`다. `daemon`은 상주만 담고 위임을 못 담아 부정확. `cluster-agent`는 "위임 대리"만 강조해 realtime 실행 역할이 흐려지고, `pipeline-` 접두 네이밍 일관성(core/batch-job/worker)에서 벗어남. → **`pipeline-worker`**.

- **`pipeline-batch-job` = 유지 (확정)**
  근거: "일회성 K8s Job으로 batch를 실행하는 바이너리"라는 의미가 정확. worker가 위임받아 만드는 그 Job의 실행 이미지가 이것이다. 역할·이름 일치.

정리된 3자 관계:
`control-plane`(제어·라우팅) → `pipeline-worker`(cluster 소속, 실행+Job위임생성) → `pipeline-batch-job`(worker가 만든 일회성 Job의 실행 바이너리).
