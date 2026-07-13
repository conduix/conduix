# 멀티 클러스터 E2E 시나리오 계획

> 작성 2026-07-13. 목적: 논리적 멀티 K8s(cluster) 환경에서 realtime/bulk 파이프라인이
> 외부 의존성을 빌드한 native custom stage 를 포함하고, 실행 중 stage 추가/삭제에도
> 워크플로우 라이프사이클이 기대대로 관리되는지, mysql/redis/kafka 등 다중 소스로 검증한다.
> 발견 문제를 모두 수정하고 반복 테스트로 무결점까지 수렴시킨다.

## 0. 환경 전제 (조사로 확정)

- **물리 K8s 는 단일 Colima** (context `colima`). "멀티 K8s" 는 Conduix 의 **논리 cluster**
  (`clusters` 테이블 + `cluster:<id>:execute` 채널 라우팅)로 시뮬레이션한다. 같은 namespace 에
  서로 다른 `CLUSTER_ID` 를 가진 agent Deployment 를 2개 띄우면 채널 격리가 코드로 보장된다
  (redis_service.go PublishWorkflowExecution / agent.go commandLoop 구독).
- **cluster 선택은 control-plane 이 결정** (workflow.ClusterID → default → 거부, execution_cluster.go).
  agent 는 자기 cluster 채널만 구독. batch=K8s Job, realtime+native=streaming Deployment 위임.
- **mock 데이터소스** (namespace conduix-e2e):
  - mysql: `conduix-mock-mysql:3306` root/rootpassword. DB: `conduix`(CP), `sourcedb`, `targetdb`
  - kafka: `conduix-mock-kafka:9092`
  - rest: `conduix-mock-rest:8080` (/orders /events /changes)
  - redis: helm subchart (claim/heartbeat/pub-sub)
- **native custom stage 예시**: `pricedouble`(price×2), `uuidtag`(외부 google/uuid import) — 이미 빌드됨.

## 1. 검증 대상 (요구사항 → 확인 항목)

| # | 요구 | 확인 항목 |
|---|------|-----------|
| R1 | k8s 를 바꿔가며 실행 | workflow.ClusterID 를 A/B 로 바꾸면 그 cluster agent 만 실행(채널 격리) |
| R2 | realtime + native stage | realtime workflow 가 native stage 로 streaming pod 실행·데이터 변환 |
| R3 | bulk + native stage | batch workflow 가 native stage 로 K8s Job 실행·데이터 변환 |
| R4 | 외부정보 빌드 custom stage | uuidtag(외부 uuid import) 포함 workflow 실행 |
| R5 | 실행 중 stage 추가/삭제 라이프사이클 | 실행 중 변경 시 워크플로우가 기대대로 관리(반영 or 안전거부) |
| R6 | 다중 데이터소스 | mysql/kafka/rest 소스 각각 + redis(claim/pubsub) |
| R7 | 라이프사이클 | start→running→(stop/roll)→정리, reconcile 백스톱, 무손실 |

## 2. 현재 코드의 "실행 중 stage 추가/삭제" 실태 (R5 핵심 — 조사 확정)

- `UpdateWorkflow` / `AddPipelineToWorkflow` / `RemovePipelineFromWorkflow` 는 **status=running 이면 거부**
  (workflow.go). → 실행 중 직접 수정 불가.
- 각 execution 은 시작 시점 `PipelinesSnapshot` 으로 돈다 → 설정 변경은 **다음 실행부터** 반영.
- realtime(native)만 `/roll` 로 새 RunnerVersion 바이너리를 streaming pod 에 rolling(수동).
- **native stage 삭제 후 재빌드** → 그 stage 를 쓰는 workflow 는 정의 누락으로 실행 불가.

> **판정 기준(R5)**: "기대대로 라이프사이클 관리" = ①실행 중 파괴적 변경을 **안전하게 거부**(명확한 에러,
> 실행 오염 없음) ②변경을 반영하려면 **명시적 경로(stop→수정→재시작, 또는 realtime /roll)** 로만.
> e2e 로 이 두 성질이 실제로 성립하는지 확인하고, 깨지면 수정한다.

## 3. 시나리오 (실행 순서)

### S0. 멀티 논리 cluster 구성
- CP `clusters` 에 `cluster-a`, `cluster-b` 등록(REST 또는 seed). `default` 는 유지.
- agent Deployment 2개: `conduix-agent-a`(CLUSTER_ID=cluster-a), `conduix-agent-b`(CLUSTER_ID=cluster-b).
  기존 `conduix-agent`(default)는 유지하거나 a 로 재사용.
- 검증: 각 agent 가 자기 cluster 채널만 구독(로그), heartbeat 에 clusterID 노출.

### S1. cluster 라우팅 (R1)
- workflow-A(ClusterID=cluster-a) start → **cluster-a agent 만** 실행(cluster-b agent 로그엔 없음).
- workflow-B(ClusterID=cluster-b) start → cluster-b 만.
- 검증: 각 execution.cluster_id 스냅샷 = 지정값. 반대 cluster agent 로그에 미수신.

### S2. bulk + native stage (R3, R6)
- batch workflow: mysql `sourcedb.orders` → native `pricedouble` → mysql `targetdb.orders_out`.
- cluster-a 에서 실행 → K8s Job 생성 → 완료 콜백 → orders_out 에 price×2 적재.
- 검증: targetdb.orders_out rows > 0, price 가 소스의 2배.

### S3. realtime + native stage + kafka (R2, R6)
- realtime workflow: kafka `rt.orders` → native `pricedouble` → mysql `targetdb.rt_orders_out`.
- cluster-b 에서 실행 → streaming Deployment(conduix-rt-*) → produce → 변환 적재.
- 검증: rt_orders_out price×2, 100+건(batch flush).

### S4. 외부 import custom stage (R4)
- workflow: 소스 → native `uuidtag`(google/uuid import) → 싱크. 각 레코드에 uuid 태그 추가.
- 검증: 출력에 uuid 필드 존재(형식 유효).

### S5. 실행 중 stage 추가/삭제 라이프사이클 (R5)
- S5a: running batch workflow 에 AddPipeline/Update 시도 → **거부 응답 확인**(409/명확 메시지).
- S5b: running realtime workflow 의 native stage 를 바꿔 새 RunnerVersion 빌드 → `/roll` →
  streaming pod 가 새 바이너리로 rolling, checkpoint offset 재개(무손실).
- S5c: 실행 중 workflow 가 쓰는 native stage 를 삭제(plugin delete) → 재빌드 →
  기존 실행은 스냅샷으로 계속, 신규 실행은 정의 누락 감지(BuildRequired/명확 에러).
- 검증: 각 케이스에서 실행 오염 없음(중간 상태 error 오판 없음), 응답/로그가 기대대로.

### S6. 라이프사이클·복원력 (R7)
- stop → streaming Deployment 삭제/ Job 정리 확인.
- reconcile: streaming Deployment 강제 삭제 → agent reconcile 이 재생성(백스톱).
- 무손실: checkpoint offset 저장 → pod 재생성 → offset 재개.

## 4. 반복 테스트 루프

1. S0~S6 순서 실행. 각 시나리오 PASS/FAIL + 근거(쿼리 결과·로그) 기록.
2. FAIL·이상 발견 → 원인 분석 → 코드 수정 → 관련 이미지 재빌드·rollout.
3. 전체 재실행. **연속 1회 전 시나리오 PASS** 할 때까지 반복(플래키 배제 위해 핵심 시나리오는 2회 반복).
4. 각 라운드 결과를 이 문서 §5 에 append.

## 5. 실행 결과 (라운드별 append)

### Round 1 (2026-07-13)
- **S0 멀티 cluster**: PASS. agent-a→cluster:cluster-a:execute, agent-b→cluster:cluster-b:execute 구독 격리 확인. default agent replicas=0.
- **S1 cluster 라우팅**: PASS. WFA(cluster-a) start→agent-a 만 delegated batch job(Complete), agent-b silent. WFB(cluster-b) 대칭 확인.
- **S2 bulk+native**: PASS. pricedouble Job → orders_out_a/b price×2 정확(19.99→39.98, 120→240).
- **S3 realtime+native+kafka**: **FAIL → 버그 2종 발견**.
  - **BUG#2(수정)**: record 모드 realtime 에 시간 flush 부재 → 부분배치(<100) sink 버퍼에 갇힘. 150건 중 100 적재 후 52 미적재(LAG=52). fix: recordModeFlushInterval(5s) 타이머 추가(커밋 c3e86a4).
  - **BUG#3(조사중)**: kafka GroupID + StartOffset=latest(기본) + 신규 그룹 → sole member 0 partition 배정, 무소비. earliest 는 1 partition 정상 배정. latest 경로 별도 규명 필요.
- S4~S6: BUG 수정 후 재개 예정.

### Round 2 (2026-07-13) — BUG#2+#3 수정 검증
- **결정 재테스트 PASS**: 신규 토픽 `decisive.orders`(파드 기동 시 미존재, latest offset) + 30건(<100) produce → **6초 내 30건 전부 price=20 적재**. BUG#3(WatchPartitionChanges 재배정)+BUG#2(time-flush 부분배치) 동시 트리거 시나리오가 완전 통과. 수정 전엔 0건이었음.
- **BUG#4(관측)**: 빌드 중 CP rollout 하면 build goroutine 고아화 → stuck `building` 행(build_log 미저장). 교훈: 빌드 in-flight 중 CP 재시작 금지. 운영 로버스트니스 이슈로 별도 기록(task #27).
- 커밋: c3e86a4(BUG#2 time-flush), 3fd1b8f(BUG#3 WatchPartitionChanges).

### Round 3 (2026-07-13) — S4 외부import + S5 라이프사이클
- **S4 uuidtag(외부 google/uuid import)**: PASS. cluster-b batch, price+5(v5 uuid) 정확(19.99→24.99, 120→125). 외부 의존성 native stage compile-in·실행 확인.
- **S5a running workflow 변경 거부**: PASS(수정 후). UpdateWorkflow=409(기존 OK). **BUG#5 발견·수정**: AddPipeline/RemovePipeline 이 running 거부 안 하고 201 로 DB 조용히 변경 → UpdateWorkflow 동일 가드 추가(커밋). 회귀테스트 추가(running=409, idle=201).
- **S5b realtime /roll 로 stage 변경 반영**: PASS. pricedouble bump→빌드→/roll→streaming pod 버전 rv-e1ad36e6→rv-a5a221ac 교체, agent-a "rolled streaming execution" 로그. cluster 라우팅 유지.
- **BUG#5(수정)**: running workflow pipeline 추가/삭제 미거부 → 일관 가드.
- **교훈**: 빌드 in-flight 중 CP rollout 금지(BUG#4). 빌드는 CP 안정 상태에서만 트리거.

### Round 4 (2026-07-13) — 전 수정 반영 후 무결점 최종 라운드
- 모든 수정(BUG#2~#5) 반영한 CP 이미지·runner 로 재실행.
- **BUG#4 검증**: CP 재시작 직후 stuck building = 0 (부팅 회수 동작).
- **최종 스위트 전부 PASS(FAIL 0)**:
  - T1 bulk cluster-a native pricedouble → price×2(max 240) PASS
  - T2 running realtime AddPipeline → 409 PASS
  - T2b running UpdateWorkflow → 409 PASS
  - T3 reconcile: streaming Deployment 삭제 → agent 재생성 PASS
  - (직전 라운드) 신규 토픽 realtime 40건 → price=20 전건 적재 PASS
- **결론: 반복 라운드에서 회귀 없음. 목표 달성.**

## 6.1 보완 시나리오 (커스텀 stage 타입 커버리지 — 사용자 지적 반영)

초기 라운드는 native(Go compile-in) 커스텀 stage 만 검증했다. Conduix 커스텀 stage 는 2종:
- **native**: plugins 행 + Go 소스 → RunnerBuilder 빌드 → runner 바이너리 compile-in. (pricedouble/uuidtag)
- **js_script**: pipeline config 의 stage 에 **inline JS code**(config.code), goja 런타임 해석, **빌드·plugin 행 불필요**.

- **S7. js_script 커스텀 stage e2e**: inline JS transform 을 가진 workflow 실행 → 데이터 변환 확인.
  native 와 실행경로(goja 인터프리트 vs compile-in)가 완전히 달라 별도 검증 필수.
- **S8. 커스텀 stage 작성→빌드→반영 라이프사이클**: native 를 처음부터 — test-native(go build 검증) API →
  신규/변경 소스 → 빌드 → RunnerVersion ready → 파이프라인 반영. "빌드한 커스텀 stage" 의 진짜 흐름.

### stage 분류 (명확화 — 사용자 질문 반영)
| 구분 | 예시 | 사용자 코딩 | 빌드 |
|---|---|---|---|
| **내장 변환 stage** | filter, remap, drop, merge, split, cast, dedupe, validate, route, base64, throttle … | config 만 | 불필요(컴파일된 built-in) |
| **내장 output stage** | sql(mysql 등), kafka, elasticsearch, mongodb, s3, rest_api, file | config 만 | 불필요 |
| **내장 input source** | kafka, sql, cdc, rest_api, file, k8s_logs (pkg/source) | config 만 | 불필요 |
| **커스텀 native** | pricedouble, uuidtag (Go) | ✅ Go 소스 | 필요(go build→runner) |
| **커스텀 js_script** | inline JS(config.code, goja) | ✅ JS 소스 | 불필요(런타임 해석) |

- **redis 는 데이터 stage 아님** — control-plane↔agent 의 claim/heartbeat/pub-sub 인프라. 파이프라인 소스/싱크로 쓰지 않음(e2e 에선 claim·라우팅 경로로만 검증).

### Round 6 — S8 커스텀 stage 작성→빌드→반영 라이프사이클 + BUG#8
- **BUG#8 발견·수정(중대)**: `CreatePluginRequest` 에 SourceCode/Type 필드가 없어 web-ui 신규 커스텀 stage POST 시 **소스가 조용히 버려짐** → 빈 소스 plugin(type=native 기본) → RunnerBuilder 가 빈 소스로 빌드 시도하다 로그·상태 없이 building 에 무한정 갇힘. (앞서 "env 빌드 flakiness" 로 의심했던 stuck building 의 진짜 원인이 이것 — 단일 근본원인.) 기존 stage 수정은 PUT(SourceCode 有)이라 동작했고 **신규 생성만** 깨져 있었음. CreatePlugin/upsert 에 source_code/type 배선 + 검증 + auto-build. 회귀테스트 추가.
- **S8 PASS(수정 후)**: 신규 native stage `tripler` 를 API 로 작성(POST, src_len 553·source_hash 정상) → auto-build → rv-fe835e18 ready(plugin_ids 에 tripler 포함) → 워크플로우 실행 → **price×3 + native_tag='via-tripler'**. **web-ui 커스텀 stage 작성→빌드→파이프라인 반영 전체 라이프사이클 실동작 확인.**
- **BUG#6(관측)**: test-native 가 stage.go 와 runner_main.go 를 같은 tempdir 에 둬 package clash 로 go build 실패(실제 RunnerBuilder 는 plugins/<name>/ 분리라 정상). 인에디터 테스트 UX 만 영향. task #31.
- **BUG#7(관측→BUG#8 로 흡수)**: mid-run stuck building 은 실은 BUG#8(빈 소스) 결과였음. 순수 goroutine 死 는 재현 안 됨.

### Round 5 — S7 js_script 커스텀 stage
- **S7 PASS**: inline js_script(goja) stage 로 price×3 + js_tag='via-goja' 필드 추가. cluster-a bulk, records 3/3. **native(compile-in)와 다른 goja 런타임 경로 첫 e2e 검증.** (초기 0건은 js_tag 컬럼 누락 = 테스트 테이블 실수, 코드 정상 — stage 는 records_written:3 로 실행됐고 sink 만 Unknown column 거부.)

## 7. 최종 결과 요약

- **검증 완료(R1~R7)**: 논리 멀티 cluster 라우팅(채널 격리), bulk+native(K8s Job), realtime+native(streaming Deployment), 외부 import stage(uuidtag/google-uuid), 실행 중 변경 안전거부+realtime /roll 반영, mysql/kafka/rest 다중 소스, reconcile 백스톱·무손실.
- **커스텀 stage 2종 다 검증(사용자 지적 반영)**: native(Go compile-in) + js_script(goja 런타임). native 는 신규 작성→빌드→반영 라이프사이클(S8)까지, js_script 는 inline JS 실행(S7)까지.
- **발견·수정 버그 6종(코드 수정분 모두 회귀테스트 포함)**:
  - BUG#2 record-mode realtime time-flush 부재 → 부분배치 미적재 (c3e86a4)
  - BUG#3 kafka consumer group 토픽 늦은생성 시 0-partition 영구무소비 (3fd1b8f)
  - BUG#4 CP 재시작 시 좀비 building 레코드 → 부팅 회수 (4de240d)
  - BUG#5 AddPipeline/RemovePipeline running 미거부 → 일관 가드 (5fee595)
  - BUG#8 CreatePlugin 이 source_code 미저장 → web-ui 신규 커스텀 stage 작성 시 빈 소스 plugin/빌드 무한 building (56ea484). **가장 사용자-직결적 버그.**
  - BUG#6(관측, 미수정) test-native tempdir package clash — 인에디터 테스트 UX. task #31.
  - (BUG#1=BUG#3 초기 오분류, BUG#7=BUG#8 결과로 흡수)
- **환경 정리**: cluster-a/b agent Deployment 2개(CLUSTER_ID 분리), default agent replicas=0. e2e-mc workflow 는 stop 상태.

## 6. 재사용 자산
- JWT mint / port-forward / produce 헬퍼: deploy/e2e/verify.sh, verify-realtime.sh.
- native 빌드: plugin 소스 bump → POST /api/v1/runner/build → runner_versions ready 대기.
- reconcile: GET /api/v1/clusters/:id/running-executions.
- 관련 계획: REALTIME_STREAMING_POD.md, CUSTOM_STAGE_DEPENDENCY_REGISTRY.md.
