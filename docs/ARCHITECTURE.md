# Conduix 아키텍처 개요

> 프로젝트의 가치·설계·구현을 핵심 위주로 정리한 단일 참조 문서. (2026-07-02 기준, 현재 구현 반영)
> 세부 문서는 [docs/README.md](README.md) 인덱스 참조. 완료·폐기 문서는 [archive/](archive/).

## 1. 가치 (왜 만드는가)

성격이 다르지만 흐름이 유사한 데이터 파이프라인을 **매번 새로 만들지 않고 플랫폼화**한다.

- **유사 흐름 재사용**: 벌크/실시간, 소스만 다른 파이프라인을 템플릿으로 복제 (id만 빼면 템플릿)
- **이중 인터페이스**: 사람은 **web-ui**, AI·프로그래밍은 **YAML**로 동일하게 생성/제어
- **성능 타협 없는 유연성**: Python 같은 편의를 **Go 네이티브 속도**로
- **분산 클러스터링**: 여러 워크플로우를 여러 서버에 배치 (파이프라인은 서버-로컬 실행으로 데이터 로컬리티 유지)
- **web-ui로 커스텀 stage 추가**: 스크립트(즉시) 또는 Go 네이티브(빌드)

## 2. 모듈 구성

| 모듈 | 역할 | 의존 |
|------|------|------|
| `shared/` | 공통 타입·유틸 (Redis ResilientClient, 메트릭) | 없음 |
| `plugin-sdk/` | 네이티브 stage 인터페이스 (`NativeStage`) | 없음 |
| `pipeline-core/` | 실행 엔진 (GroupExecutor, source/stage/output, 체크포인트) | shared, plugin-sdk |
| `pipeline-runner/` | 배치용 실행 바이너리 (K8s Job에서 GroupExecutor 구동) | shared, pipeline-core |
| `pipeline-agent/` | 실행 에이전트 (워크플로우 수신·실행, 하트비트) | shared, pipeline-core |
| `control-plane/` | 운영 백엔드 (Gin+GORM+MySQL, 스케줄러, Redis pub/sub) | shared, pipeline-core |
| `web-ui/` | 프론트엔드 (React+TS+MUI) | control-plane REST |

### 모듈 의존 관계

```mermaid
graph TD
    webui["web-ui (React)"] -->|REST| cp["control-plane<br/>(API·스케줄러·pub/sub)"]
    cp -->|Redis pub/sub| agent["pipeline-agent"]
    cp -->|K8s Job 생성| runner["pipeline-runner<br/>(batch 바이너리)"]
    agent --> core["pipeline-core<br/>(GroupExecutor)"]
    runner --> core
    core --> sdk["plugin-sdk<br/>(NativeStage)"]
    core --> shared["shared<br/>(types·redis·metrics)"]
    cp --> shared
    agent --> shared

    classDef svc fill:#e3f2fd,stroke:#1976d2
    classDef lib fill:#f1f8e9,stroke:#689f38
    class webui,cp,agent,runner svc
    class core,sdk,shared lib
```

## 3. 파이프라인 모델

```mermaid
flowchart LR
    IN[Input<br/>kafka·rest·sql·cdc·file] --> S1[공통 Stage 1..N<br/>filter·cast·js_script...]
    S1 --> FO{Output별 분기}
    FO --> P1[PreStages A] --> O1[(Output A<br/>sql·es·kafka)]
    FO --> P2[PreStages B] --> O2[(Output B<br/>s3·mongo...)]
```

- 공통 Stage는 모든 Output 공유, PreStages는 Output 전용 변환. bulk(배치 벌크쓰기)/individual(건별) 선택.

- **Input**: kafka, rest_api, sql, sql_event, cdc, file, k8s_logs, mqtt, rabbitmq, sqs, websocket, redis_stream, pubsub 등
- **Stage**: filter, remap, cast, timestamp, encrypt, dedupe, validate, throttle, route, aggregate, enrich, js_script(goja) 등 28종
- **Output**: sql(mysql/postgres), elasticsearch, kafka, mongodb, s3, gcs, bigquery, rest_api
- **PreStages**: Output별 전용 변환 (bulk/record 양 모드 모두 적용)

**커스텀 stage**: 2-tier (플러그인 V4)
- **Script tier**: `js_script` — JavaScript(goja) 스크립트, 빌드 없이 즉시 실행. text→number, json 가공/추출 등.
- **Native tier**: Go 코드 → 단일 runner 이미지로 통합 빌드 (web-ui 편집기 → RunnerBuilder).

## 4. 실행 엔진 (pipeline-core)

- **GroupExecutor 단일 엔진.** (구 actor 엔진은 제거됨)
- 워크플로우 executor가 **stream 레지스트리로 위임**하여 28개 내장 stage + 네이티브 플러그인이 실제 실행됨.
- **배치 모드**(기본)와 레코드 모드. 배치는 워커 병렬 처리.
- **pause/resume**: context-aware pause gate (배치/레코드 양 루프).
- **재시도**: FailurePolicy.retry — 지수 백오프 + jitter(thundering herd 방지).
- **체크포인트**: 소스 오프셋 기반 복구.

## 5. 분산 · 통신

- **통신**: control-plane → Redis Pub/Sub → agent (REST 폴백). ResilientClient(재연결·서킷브레이커·로컬캐시).
- **배치 단위**: 워크플로우 → 클러스터 채널(`cluster:<id>:execute`). **여러 에이전트 중 하나가 SETNX claim**으로 단독 실행 (중복 실행 방지).
- **파이프라인 = 서버-로컬**: stage 간 레코드가 인프로세스로 흐름(네트워크 홉 없음). 수평 확장은 Kafka 컨슈머 그룹(파티션 분산) 또는 워크플로우 배치로.
- **고아 실행 감지**: 담당 에이전트 크래시 시 scheduler가 running 실행을 failed로 전이(조용한 유실 방지).

### 실행 트리거 흐름 (start → 실행)

```mermaid
sequenceDiagram
    participant U as web-ui / YAML(AI)
    participant CP as control-plane
    participant R as Redis
    participant A as agent (1..N in cluster)
    participant GE as GroupExecutor

    U->>CP: POST /workflows/:id/start
    CP->>CP: execution 레코드 생성(running)
    alt batch + JobConfig
        CP->>CP: K8s Job 생성 → pipeline-runner 구동
    else realtime / agent
        CP->>R: publish cluster:<id>:execute
        R-->>A: 명령 fan-out (전 에이전트 수신)
        A->>R: SETNX execution:claim (단독 실행 획득)
        Note over A: 획득한 1대만 실행, 나머지 skip
        A->>GE: executeGroup → 파이프라인 실행
    end
    A-->>CP: 결과/하트비트(RunningExecs)
    Note over CP: 하트비트 소실 시 고아 실행 → failed 전이
```

### 실행 프로세스 종류

```mermaid
flowchart TD
    W{워크플로우 type}
    W -->|realtime<br/>kafka·cdc·ws| AG[agent 상주 프로세스<br/>장기 실행 + pause/resume]
    W -->|batch + JobConfig| KJ[K8s Job<br/>pipeline-runner 일회성]
    W -->|batch<br/>기본| AG2[agent 프로세스<br/>소스 소진 시 완료]
    AG --> GE[GroupExecutor]
    KJ --> GE
    AG2 --> GE
    GE -->|병렬/순차/DAG| PIPE[파이프라인들<br/>서버-로컬 인프로세스]
```

## 6. 복원력 · 안전

- **서킷 브레이커**: 실행 중 sink/변환 실패가 임계(연속/누적) 초과 시 실행을 조기 에러 종료 — 실패 누적이 부하가 되는 것을 차단. (`FailurePolicy.CircuitBreaker`)
- **DLQ**: 설정 시 실패 레코드 적재(보존용, 서킷과 독립).
- **panic recover**: 한 파이프라인 panic이 에이전트 전체를 죽이지 않음.
- **실패 카운트/로그**: 모든 실패를 상관키(workflow/pipeline id) 포함 slog + Prometheus 메트릭.

## 7. 관측성

- **Prometheus**: `/metrics` (agent + control-plane). 실행 수(status별), 처리 레코드, 실행 시간 히스토그램, 활성 실행 게이지, 재시도·서킷트립 카운터.
- **구조화 로깅**: 파이프라인 실행 완료/실패에 workflow_id·pipeline_id 상관키(slog).

## 8. 운영 인터페이스

- **web-ui**: 워크플로우 생성·제어(start/stop/pause/resume), 처리량·실행상태 모니터링, YAML 내보내기/복제/가져오기.
- **YAML(프로그래밍/AI)**: `GET /workflows/:id/yaml`(export), `POST /workflows/import`(생성). export→id/project 교체→import = 템플릿 인스턴스화. web-ui와 동일 모델(`buildWorkflowModel`로 일원화).
- **샘플**: 첫 실행 시 "Sample Pipelines" 프로젝트에 6종(bulk 3 + cdc 3) 워크플로우가 자동 등록된다(삭제 가능, 삭제 후 재시딩 안 함). 각 샘플은 `js_script` 커스텀 stage(text→number, json 가공, json 추출)를 포함해 빌드 없이 즉시 실행된다. (`control-plane/internal/seed`)

## 9. 알려진 제약 (정직한 현황)

- **PostgreSQL CDC**: 미지원(생성 시 조기 거부). MySQL CDC 또는 Kafka(Debezium) 경유.
- **단일 파이프라인 수평 샤딩**: 미지원 — 로컬리티 우선. Kafka 파티션/워크플로우 배치로 확장.
- **자동 실행 재개**: 고아 실행은 failed 전이까지(인지 가능). checkpoint 기반 자동 재실행은 미구현.
- 상세: [REMAINING_WORK_CHECKLIST.md](REMAINING_WORK_CHECKLIST.md)
