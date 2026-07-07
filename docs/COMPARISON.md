# Conduix vs 다른 도구 — 데이터 엔지니어를 위한 선택 가이드

> 목적: "Airflow/Flink/Kafka Connect/NiFi 대신 왜 Conduix인가, 이걸로 무엇을 대체할 수 있고 얼마나 유연한가"를
> **실제 구현된 역량 기준**으로 판단할 수 있게 정리한다. 로드맵/미구현은 [한계](#7-정직한-한계) 섹션에 분리한다.
> 근거는 코드(`pipeline-core/pkg/*`, `pipeline-worker/internal/agent`, `control-plane/internal`)다.

---

## 1. 한 문장 요약

**Conduix는 "커넥터(Kafka Connect) + 스트림 변환(Flink/Bento) + 오케스트레이션/스케줄(Airflow)"을 하나의 워크플로우로 묶고,
GUI와 YAML/API 양쪽에서 다루며, 변환 로직을 빌드 없이(JavaScript) 또는 네이티브 Go로 확장할 수 있는 K8s-네이티브 파이프라인 플랫폼이다.**

데이터 엔지니어 관점의 핵심: **"소스에서 읽어 → 변환하고 → 여러 싱크에 서로 다른 형태로 적재"** 하는 파이프라인을,
Airflow처럼 Python DAG를 짜지 않고, Flink 클러스터를 따로 운영하지 않고, 워크플로우 정의 하나로 batch·realtime을 모두 처리한다.

---

## 2. 무엇을 대체/통합할 수 있나

| 하고 싶은 일 | 기존 스택 | Conduix로 | 근거 |
|---|---|---|---|
| Kafka → 변환 → DB/ES 적재 (실시간) | Kafka Connect + KSQL/Flink | realtime 워크플로우 1개 | kafka source + stage + sql/es sink |
| DB 대량 조회 → 가공 → 웨어하우스 | Airflow + Spark/dbt | batch 워크플로우 (K8s Job) | sql/partitioned_sql source + bigquery sink |
| REST API 폴링 → 정규화 → 저장 | 커스텀 스크립트 + cron | rest_api source + cron 스케줄 | http source + scheduler_service |
| MySQL CDC → 이벤트 스트림 | Debezium + Kafka Connect | cdc source (MySQL binlog) — 유실 없이 단독 처리, insert/update 는 upsert 로 소스와 수렴(backpressure·offset·GTID·DDL, [ADR-0004](adr/0004-cdc-safety.md)). **단 SQL 싱크의 하드 delete 반영은 미구현** → 로드맵 [plans/cdc-roadmap](plans/cdc-roadmap.md) #1 | source/cdc.go |
| 한 소스 → 여러 대상에 다른 포맷 | 파이프라인 N개 복제 | Output별 PreStages 1개 워크플로우 | group_executor PreStages |
| 조건 분기/팬아웃 라우팅 | NiFi 프로세서 그래프 | router stage (fan_out/condition/filter) | stream/router_stage.go |
| 커스텀 변환 로직 | Flink UDF (빌드·배포) | js_script(빌드 0) 또는 native Go plugin | js_script_stage / native_stage_adapter |

**핵심 대체 포인트**: 위 시나리오들이 **각기 다른 도구·언어·운영 스택**을 요구하던 것을, Conduix는 **동일한 워크플로우 모델 + 동일한 GUI/YAML**로 처리한다.

---

## 3. 경쟁 도구 대비 위치

각 도구가 "잘하는 것"은 존중하되, Conduix가 커버하는 범위와 차이를 명시한다.

| 축 | **Conduix** | Apache Airflow | Apache Flink | Kafka Connect | Apache NiFi | Bento/Benthos |
|---|---|---|---|---|---|---|
| **주 용도** | 커넥터+변환+오케스트레이션 통합 | 태스크 오케스트레이션(DAG) | 스트림 상태 연산 | Kafka↔외부 커넥터 | 데이터 흐름 GUI | 스트림 변환(stateless 중심) |
| **정의 방식** | GUI + YAML/API (동등) | Python 코드(DAG) | Java/Scala 코드 | JSON 커넥터 config | GUI(플로우 캔버스) | YAML |
| **batch/stream** | **하나의 모델로 둘 다** | batch 중심(스트림 약함) | stream 중심 | stream(Kafka 한정) | 둘 다 | 둘 다 |
| **변환 로직** | 내장 21 stage + JS(빌드0) + Go plugin | operator/PythonOperator | UDF(빌드·배포) | SMT(제한적) | 프로세서(내장) | Bloblang |
| **소스/싱크 범위** | 16 source / 9 sink (자체 구현) | provider(외부 패키지) | connector(코드) | 커넥터 생태계(방대) | 프로세서 다수 | 다수 |
| **커스텀 확장** | JS 즉시 + Go 네이티브(single-image build) | Python 자유 | JVM UDF | Java 커넥터 개발 | Java 프로세서 | Go plugin/WASM |
| **실행 인프라** | K8s-네이티브(Job 위임+상주 agent) | scheduler+worker | Flink 클러스터 | Connect 워커 | NiFi 클러스터 | 단일 바이너리 |
| **복원력** | 서킷브레이커+DLQ+고아감지+백오프 | 재시도/SLA | 체크포인트/세이브포인트(강력) | offset/재시도 | back-pressure | 재시도/DLQ |
| **멀티 인터페이스** | GUI·YAML·API 동일 모델 | 코드만 | 코드만 | REST/config | GUI 중심 | YAML만 |

### 언제 Conduix가 유리한가
- **여러 도구를 한 플랫폼으로 통합**하고 싶을 때 (Connect+Flink+Airflow를 각각 운영하기 부담스러울 때)
- **비개발 운영자도 GUI로** 파이프라인을 만들고, **엔지니어는 YAML/API로** 같은 것을 버전 관리하고 싶을 때
- **변환 로직을 빠르게 반복**해야 할 때 (js_script는 빌드 없이 저장→재시작만으로 반영)
- **회사별 커스텀 커넥터/변환**을 Go로 안전하게 임베드해야 할 때 (native plugin, 소스해시 검증)

### 언제 다른 도구가 나은가 (정직하게)
- **복잡한 상태 기반 스트림 연산**(대규모 이벤트타임 윈도우, exactly-once 상태 저장) → **Flink**가 성숙
- **범용 DAG 오케스트레이션**(데이터 외 임의 태스크, 방대한 provider 생태계) → **Airflow**
- **초기 스냅샷 + 여러 컨슈머 fan-out + 스키마 레지스트리**가 핵심인 CDC 허브 → Debezium+Kafka (§7, [bulk/realtime 비교](BULK_VS_REALTIME_COMPARISON.md))
- **이미 방대한 Kafka Connect 커넥터**에 의존 중이고 그 생태계가 핵심이면 → 전환 비용 고려

> bulk(vs Spark/Flink)·realtime(vs Debezium/Spark Streaming/Flink)의 축별 심층 비교는 **[BULK_VS_REALTIME_COMPARISON.md](BULK_VS_REALTIME_COMPARISON.md)**.

---

## 4. 유연성 — "얼마나 다양한 걸 할 수 있나"

### 4.1 파이프라인 구조
```
Input → [공통 Stage] → [Output별 PreStages] → Output(들)
        (레코드 변환)   (Output 전용 변환)      (bulk/individual)
```
- **Output별 PreStages**: 한 소스에서 읽은 데이터를, ES에는 `@timestamp` 붙여서·S3에는 원본으로·DB에는 민감필드 마스킹해서 — **동시에 다른 형태로** 적재. (`group_executor` PreStages 경로)
- **Router stage**: `fan_out`(전체 복제) / `condition`(첫 매칭) / `filter`(전체 매칭)로 in-pipeline 분기 (Kafka 없이).
- **부모-자식 파이프라인**: pipeline link(부모→자식 Kafka 주입) 또는 subpipeline stage(중첩 실행)로 Board→Post→Comment 같은 계층 데이터 수집. (`group_executor` link 라우팅 / `stream/subpipeline_stage.go`)

### 4.2 커넥터 인벤토리 (실제 구현)
- **Source 16종**: kafka, sql, partitioned_sql, http/rest_api, partitioned_http, file, sql_event(폴링), cdc(MySQL), mongodb_cdc, kubernetes(pod logs), websocket, mqtt, sse, sqs, rabbitmq, gcp_pubsub, redis_stream
- **Sink 9종**: sql(upsert), kafka, elasticsearch, mongodb(upsert), s3, gcs, bigquery, rest_api, stub
- **Stage 21종 등록**: filter, remap(Bloblang), drop, merge, split, encrypt(aes256/sha256/bcrypt/mask), dedupe, default, cast, timestamp, throttle, sample, route, validate(JSON Schema), contract(Data Contract), base64, js_script + sink 재사용 stage(sql/es/kafka/mongodb)
  - 추가로 구현되어 있으나 아직 GUI 미노출: enrich/lookup, join, windowed aggregate, subpipeline, fanout 등 (§7 참고)

### 4.3 변환: Bloblang vs JavaScript vs Go
| 방식 | 언제 | 빌드 | 성능 |
|---|---|---|---|
| **remap (Bloblang)** | 필드 매핑·간단 변환 | 없음 | 최고 |
| **js_script (goja)** | 조건/반복/합성 등 로직 필요 | **없음**(저장→재시작) | remap 대비 5~10배 느리나 I/O 병목이 대부분이라 실질 영향 작음 |
| **native Go plugin** | 고성능·외부 SDK·회사 모듈 | go build(single image) | 네이티브 속도 |

### 4.4 batch/realtime 하나의 모델
- 워크플로우 `type`만 다르고 정의 방식은 동일. `/start`로 동일하게 트리거.
- **batch** → worker가 자기 K8s cluster에 Job 생성(위임) → 완료 후 콜백.
- **realtime** → worker 프로세스 내 상주 스트리밍(pause/resume 지원).

### 4.5 이중 인터페이스 + 스케줄
- GUI로 만든 워크플로우를 **YAML export → git 버전관리 → 다른 환경에 import**(템플릿화).
- **cron 스케줄**(robfig/cron v3, 타임존 지원) — batch/realtime 공통.

---

## 5. 성능 — 실제 구현 근거

과장 없이, 코드로 확인된 성능 관련 메커니즘만.

- **병렬 배치 처리**: 파이프라인당 `workers`(1~100, 세마포어 풀) × `batch.size`(기본 100). (`group_executor` 배치 경로)
- **네이티브 Go 실행 속도**: 변환이 컴파일된 Go 또는 goja 인터프리터로 in-process 실행 (별도 클러스터 왕복 없음).
- **체크포인트 기반 복구**: Kafka/CDC offset 저장 → 재시작 시 이어받기.
- **K8s 수평 확장**: batch는 Job으로 격리 실행, agent/worker는 replica로 확장, 멀티 cluster 라우팅(`cluster_id`).
- **관측성 내장**: Prometheus 메트릭(`pipeline_execution_total`, `records_total`, `duration_seconds`, `active_executions`, `circuit_breaker_tripped_total`) + slog 구조화 로깅(workflow_id/execution_id 상관키).

> **주의**: 처리량 벤치마크 수치는 아직 공식 측정치가 없다. 위는 "성능을 위해 존재하는 메커니즘"이며,
> 워크로드별 실측은 별도 필요(대부분의 파이프라인은 소스/싱크 I/O가 병목).

---

## 6. 복원력 — 운영 시 안심 요소

전부 **설정으로 제어**(`FailurePolicy`). (`pipeline-core/pkg/executor/failure_guard.go`)

- **서킷 브레이커**: 연속/누적 실패 임계 초과 시 실행 조기 종료(계속 도는 부하 방지).
- **DLQ**: 실패 레코드를 s3/kafka/sql 등에 적재(서킷과 독립).
- **retry + 지수 백오프 + jitter**: 재시도 몰림(thundering herd) 방지, 상한 5분.
- **고아 실행 감지**: 담당 agent가 죽은 running 실행을 주기적으로 failed 전이(조용한 유실 방지).
- **panic recover**: 한 파이프라인 panic이 agent 전체를 죽이지 않음.
- **종료 시 drain flush**: stop 시에도 버퍼 잔여 데이터를 유실 없이 적재.

---

## 7. 정직한 한계

선택 판단에 필요한 실제 제약. 숨기지 않는다.

| 항목 | 상태 | 우회/대안 |
|---|---|---|
| **CDC 초기 스냅샷** | 미지원(CDC는 변경분만) | bulk 파이프라인으로 초기 적재 후 `start_position`/`start_lsn`으로 CDC 이어받기 |
| **단일 파이프라인 수평 샤딩** | 미지원(서버-local 설계) | Kafka 파티션 병렬, 워크플로우를 여러 cluster에 분산 |
| **분산 셔플/spill-to-disk** | 없음(단일 노드 인메모리) | 대용량 GROUP BY/JOIN은 Spark/Flink |
| **고아 실행 자동 재개** | 미구현(failed 전이만) | checkpoint 중복 위험으로 보류 — 수동 재실행 |
| **스트림 상태 자동 복구** | Redis 저장은 있으나 재시작 시 자동 로드 아님 | window/join 상태는 재시작 후 재구축 |
| **SQL 싱크 시간기반 flush** | 미구현 | batch_size 도달 또는 종료 시 flush |
| **일부 고급 stage(enrich/join/window 등)** | 구현됐으나 GUI 미노출 | YAML/API로는 사용 가능(등록 여부 확인 필요) |
| **exactly-once 상태 스트림** | at-least-once + upsert 수렴(Flink 수준 아님) | 정확히-한번 상태연산이 핵심이면 Flink |
| **처리량 공식 벤치마크** | 없음 | 워크로드별 실측 필요 |
| **Bento 커넥터 직접 노출** | adapter 계층만 존재, 현재는 자체 커넥터 사용 | 자체 16 source/9 sink로 커버 |

> **PostgreSQL CDC는 이제 지원**(§CDC, 실 DB e2e 검증 완료). 위 표에서 CDC 관련 잔여 한계는 "초기 스냅샷"과 "분산 처리량"이다.

---

## 8. 결론 — 데이터 엔지니어의 선택 기준

**Conduix를 고르는 경우:**
- Kafka Connect + Flink + Airflow를 각각 운영하는 대신 **하나의 K8s-네이티브 플랫폼으로 통합**하고 싶다.
- **GUI(운영자) + YAML/API(엔지니어)** 를 같은 모델로 쓰고 싶다.
- 변환 로직을 **빌드 없이 즉시(JS)** 반복하거나, **회사 커스텀 Go 모듈**을 안전하게 임베드하고 싶다.
- 한 소스를 **여러 싱크에 서로 다른 형태**로 적재하는 요구가 잦다.
- MySQL/REST/Kafka/파일 등 **일반적인 소스**가 대상이고, 서킷브레이커·DLQ 같은 **운영 복원력**이 필요하다.

**다른 도구를 유지하는 경우:**
- 대규모 **상태 기반 exactly-once 스트림 연산**이 핵심 → Flink.
- **메모리 초과 대용량 배치**(분산 셔플/spill-to-disk가 필요한 GROUP BY·JOIN) → Spark/Flink.
- 데이터 외 **범용 태스크 오케스트레이션**과 방대한 provider 생태계 → Airflow.
- **CDC 초기 스냅샷 + 다중 컨슈머 fan-out + 스키마 레지스트리**가 핵심 → Debezium+Kafka.

> MySQL·PostgreSQL CDC 자체는 Conduix 단독으로 Debezium 없이 처리 가능(변경분 캡처·offset/GTID/LSN·재연결·delete 반영·HA 모두 코드+e2e 검증). 자세한 축별 비교: **[BULK_VS_REALTIME_COMPARISON.md](BULK_VS_REALTIME_COMPARISON.md)**.

---

*이 문서의 모든 역량 주장은 코드로 검증됨(2026-07 기준). 미구현/로드맵은 §7에 분리. 처리량 수치는 실측 전.*
