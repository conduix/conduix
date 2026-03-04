# Conduix Pipeline 확장 계획

## 현재 상태 분석 (2026-03-04)

### 지원 기능

| 카테고리 | 현재 지원 | 구현 상태 |
|----------|-----------|-----------|
| **Input Types** | Kafka, REST API, SQL, CDC, SQL Event, File, K8s Logs, Partitioned HTTP/SQL | ✅ 완료 |
| **Output Types** | SQL, Kafka, REST API, Stub, DLQ, Batching Wrapper | ✅ 완료 |
| **Stage Types** | Filter, Remap, Drop, Merge, Split, Encrypt, Default, Cast, Timestamp, Dedupe, Throttle, Route, Validate, Contract 등 20+ | ✅ 완료 |
| **Multi-Output** | 단일 Input → 다중 Output (각 Output별 PreStages) | ✅ 완료 |
| **Batch Processing** | bulk/individual output mode | ✅ 완료 |
| **Realtime Processing** | Kafka 스트리밍, Deduplication | ✅ 완료 |
| **Data Contract** | Schema validation + Circuit breaker | ✅ 완료 |
| **Checkpoint** | Kafka offset, Timestamp 기반 복구 | ✅ 완료 |

### 현재 한계점

1. **라우팅 제한**: Route Stage가 CDC `_op` 필드만 지원, 일반적인 조건 분기 없음
2. **Aggregation 미완성**: AggregateStage 기본 구조만 존재
3. **Stream Join 없음**: 여러 Input 간 데이터 조인 불가
4. **Output Types 부족**: Elasticsearch, MongoDB, S3 등 주요 저장소 미구현
5. **In-Pipeline Branching 없음**: 복잡한 분기는 Kafka를 통한 별도 파이프라인 필요

---

## 확장 계획 개요

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Phase 1: Output 확장                            │
│  Elasticsearch, MongoDB, S3, GCS, BigQuery, Snowflake 등 주요 저장소 추가    │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Phase 2: 고급 라우팅                               │
│  조건 기반 Router Stage, Fan-out/Fan-in, 동적 Output 선택                    │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Phase 3: Input 확장                               │
│  WebSocket, gRPC, Pub/Sub, SQS, RabbitMQ, MQTT 등 메시징 프로토콜 추가       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Phase 4: 고급 처리                                  │
│  Windowed Aggregation, Stream Join, Enrichment (Lookup), Sub-pipeline       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Phase 5: GUI 개선                                   │
│  Visual Pipeline Builder, 드래그앤드롭 연결, 실시간 데이터 미리보기           │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Output 확장 (주요 저장소 추가)

### 1.1 Elasticsearch Output
**우선순위: 높음** | **예상 작업량: 중**

```yaml
outputs:
  - name: es_output
    type: elasticsearch
    config:
      addresses: ["http://localhost:9200"]
      index: "events-{{ .date }}"           # 동적 인덱스 지원
      id_field: "_id"                        # Document ID 필드
      bulk_size: 1000
      flush_interval: "5s"
      auth:
        type: basic                          # basic, api_key, bearer
        username: elastic
        password: "${ES_PASSWORD}"
      tls:
        enabled: true
        ca_cert: "/path/to/ca.crt"
```

**구현 사항:**
- [x] `pipeline-core/pkg/output/elasticsearch.go` 생성 ✅ (2026-03-04)
- [x] Bulk API 지원 (ndjson 포맷) ✅
- [x] 동적 인덱스 템플릿 (날짜, 필드값 기반) ✅
- [x] 인증 (Basic, API Key, Bearer Token) ✅
- [x] TLS/SSL 지원 ✅
- [ ] Retry with exponential backoff
- [ ] Index template 자동 생성 옵션

### 1.2 MongoDB Output
**우선순위: 높음** | **예상 작업량: 중**

```yaml
outputs:
  - name: mongo_output
    type: mongodb
    config:
      uri: "mongodb://localhost:27017"
      database: "analytics"
      collection: "events"
      write_concern: "majority"
      ordered: false                         # 순서 무관 bulk insert
      upsert:
        enabled: true
        key_fields: ["_id", "event_id"]
```

**구현 사항:**
- [x] `pipeline-core/pkg/output/mongodb.go` 생성 ✅ (2026-03-04)
- [x] Bulk write 지원 (InsertMany, BulkWrite) ✅
- [x] Upsert 지원 (UpdateOne with upsert) ✅
- [x] Write Concern 설정 ✅
- [x] 동적 Collection 이름 지원 ✅

### 1.3 S3/GCS Output
**우선순위: 높음** | **예상 작업량: 중**

```yaml
outputs:
  - name: s3_output
    type: s3
    config:
      bucket: "data-lake"
      region: "ap-northeast-2"
      path_template: "events/year={{ .year }}/month={{ .month }}/day={{ .day }}/"
      file_format: parquet                   # json, ndjson, csv, parquet
      compression: gzip                      # none, gzip, snappy, zstd
      partition_by: ["event_type", "region"] # 필드 기반 파티셔닝
      max_file_size: "128MB"
      max_file_age: "1h"
```

**구현 사항:**
- [x] `pipeline-core/pkg/output/s3.go` 생성 ✅ (2026-03-04)
- [ ] `pipeline-core/pkg/output/gcs.go` 생성
- [x] 다중 파일 포맷 (JSON, NDJSON, CSV) ✅
- [x] 압축 지원 (gzip) ✅
- [x] Hive 스타일 파티셔닝 ✅
- [x] 파일 로테이션 (크기, 시간 기반) ✅
- [ ] Multipart upload 지원
- [ ] Parquet 포맷 지원
- [ ] snappy, zstd 압축 지원

### 1.4 Cloud Data Warehouse Output
**우선순위: 중** | **예상 작업량: 높음**

```yaml
outputs:
  - name: bq_output
    type: bigquery
    config:
      project: "my-project"
      dataset: "analytics"
      table: "events"
      write_disposition: "WRITE_APPEND"      # WRITE_APPEND, WRITE_TRUNCATE
      schema_update_options: ["ALLOW_FIELD_ADDITION"]
      clustering_fields: ["event_type", "user_id"]
```

**구현 사항:**
- [ ] `pipeline-core/pkg/sink/bigquery.go` 생성
- [ ] `pipeline-core/pkg/sink/snowflake.go` 생성
- [ ] Streaming insert / Batch load
- [ ] 스키마 자동 감지
- [ ] 파티션/클러스터링 지원

---

## Phase 2: 고급 라우팅

### 2.1 Conditional Router Stage
**우선순위: 높음** | **예상 작업량: 중**

```yaml
stages:
  - name: smart_router
    type: router
    config:
      mode: condition                        # fan_out, condition, filter
      routes:
        - name: high_priority
          condition: '.priority >= 8'
          outputs: ["alert_output", "kafka_output"]
        - name: normal
          condition: '.priority >= 5 && .priority < 8'
          outputs: ["es_output"]
        - name: low_priority
          condition: '.priority < 5'
          outputs: ["s3_archive"]
        - name: default
          outputs: ["default_output"]        # 조건 매칭 없을 때
```

**구현 사항:**
- [x] `pipeline-core/pkg/stream/router_stage.go` 생성 ✅ (2026-03-05)
- [x] VRL 스타일 조건식 파서 (==, !=, >, <, >=, <=, exists, =~) ✅
- [x] 다중 Output 선택 지원 ✅
- [x] Default route 지원 ✅
- [x] Route 메트릭 수집 ✅
- [x] AND/OR 조건 조합 지원 ✅
- [x] 중첩 필드 접근 지원 ✅

### 2.2 Dynamic Output Selection
**우선순위: 중** | **예상 작업량: 중**

```yaml
outputs:
  - name: dynamic_output
    type: dynamic
    config:
      output_field: "_target_output"         # 레코드 필드로 Output 결정
      fallback: "default_output"
      mapping:
        "es": "elasticsearch_output"
        "s3": "s3_output"
        "kafka": "kafka_output"
```

**구현 사항:**
- [x] `pipeline-core/pkg/output/dynamic.go` 생성 ✅ (2026-03-05)
- [x] 런타임 Output 선택 ✅
- [x] Output 매핑 테이블 ✅
- [x] Fallback 처리 ✅
- [x] 조건 기반 라우팅 지원 ✅
- [x] Router Stage 연동 (_target_outputs 필드) ✅

### 2.3 Fan-out / Fan-in Pattern
**우선순위: 중** | **예상 작업량: 높음**

```yaml
stages:
  - name: parallel_enrichment
    type: fan_out
    config:
      branches:
        - name: user_lookup
          stages:
            - type: enrich
              config:
                source: user_api
                join_key: user_id
        - name: geo_lookup
          stages:
            - type: enrich
              config:
                source: geo_db
                join_key: ip_address
      merge_strategy: deep_merge             # deep_merge, shallow_merge, array
```

**구현 사항:**
- [x] `pipeline-core/pkg/stream/fanout_stage.go` 생성 ✅ (2026-03-05)
- [x] 병렬 브랜치 실행 ✅
- [x] 결과 병합 전략 (deep_merge, shallow_merge, array, first) ✅
- [x] 브랜치별/전역 타임아웃 ✅
- [x] 브랜치 메트릭 수집 ✅
- [x] 실패 시 계속/중단 옵션 ✅

---

## Phase 3: Input 확장

### 3.1 메시징 시스템
**우선순위: 높음** | **예상 작업량: 중**

```yaml
# RabbitMQ
input:
  type: rabbitmq
  config:
    url: "amqp://guest:guest@localhost:5672/"
    queue: "events"
    prefetch: 100
    auto_ack: false

# AWS SQS
input:
  type: sqs
  config:
    queue_url: "https://sqs.us-east-1.amazonaws.com/123456789/my-queue"
    region: "us-east-1"
    max_messages: 10
    wait_time_seconds: 20

# Google Pub/Sub
input:
  type: pubsub
  config:
    project: "my-project"
    subscription: "events-sub"
    max_outstanding_messages: 1000
```

**구현 사항:**
- [ ] `pipeline-core/pkg/source/rabbitmq.go` 생성
- [ ] `pipeline-core/pkg/source/sqs.go` 생성
- [ ] `pipeline-core/pkg/source/pubsub.go` 생성
- [ ] Ack/Nack 처리
- [ ] Dead letter queue 연동
- [ ] Checkpoint 지원

### 3.2 실시간 스트리밍
**우선순위: 중** | **예상 작업량: 중**

```yaml
# WebSocket
input:
  type: websocket
  config:
    url: "wss://stream.example.com/events"
    reconnect_interval: "5s"
    heartbeat_interval: "30s"

# Server-Sent Events
input:
  type: sse
  config:
    url: "https://api.example.com/events"
    headers:
      Authorization: "Bearer ${TOKEN}"

# MQTT
input:
  type: mqtt
  config:
    broker: "tcp://localhost:1883"
    topic: "sensors/#"
    qos: 1
```

**구현 사항:**
- [ ] `pipeline-core/pkg/source/websocket.go` 생성
- [ ] `pipeline-core/pkg/source/sse.go` 생성
- [ ] `pipeline-core/pkg/source/mqtt.go` 생성
- [ ] 재연결 로직
- [ ] Heartbeat/Keep-alive
- [ ] Wildcard topic 지원

### 3.3 데이터베이스 확장
**우선순위: 중** | **예상 작업량: 중**

```yaml
# MongoDB Change Stream
input:
  type: mongodb_cdc
  config:
    uri: "mongodb://localhost:27017"
    database: "mydb"
    collection: "events"
    resume_after: "${RESUME_TOKEN}"

# Redis Streams
input:
  type: redis_stream
  config:
    address: "localhost:6379"
    stream: "events"
    group: "pipeline-group"
    consumer: "consumer-1"
```

**구현 사항:**
- [ ] `pipeline-core/pkg/source/mongodb_cdc.go` 생성
- [ ] `pipeline-core/pkg/source/redis_stream.go` 생성
- [ ] Resume token 관리
- [ ] Consumer group 지원

---

## Phase 4: 고급 처리

### 4.1 Windowed Aggregation
**우선순위: 높음** | **예상 작업량: 높음**

```yaml
stages:
  - name: minute_aggregation
    type: aggregate
    config:
      window:
        type: tumbling                       # tumbling, sliding, session
        size: "1m"
        grace_period: "10s"
      group_by: ["user_id", "event_type"]
      aggregations:
        - field: count
          function: count
        - field: total_amount
          function: sum
          source: amount
        - field: avg_duration
          function: avg
          source: duration
        - field: unique_items
          function: count_distinct
          source: item_id
      emit:
        mode: on_close                       # on_close, periodic, on_update
        include_window_info: true
```

**구현 사항:**
- [ ] `pipeline-core/pkg/stream/aggregate_stage.go` 확장
- [ ] 윈도우 타입 (Tumbling, Sliding, Session)
- [ ] 집계 함수 (count, sum, avg, min, max, count_distinct)
- [ ] 상태 저장 (Memory, Redis)
- [ ] Late event 처리 (grace period)
- [ ] Watermark 관리

### 4.2 Stream Join
**우선순위: 중** | **예상 작업량: 높음**

```yaml
stages:
  - name: order_user_join
    type: join
    config:
      type: left                             # inner, left, right, outer
      right_source:
        type: kafka
        config:
          topic: users
      join_key:
        left: user_id
        right: id
      window: "5m"                           # 시간 윈도우 내에서만 조인
      output_fields:
        - left.order_id
        - left.amount
        - right.name as user_name
        - right.email
```

**구현 사항:**
- [ ] `pipeline-core/pkg/stream/join_stage.go` 생성
- [ ] Join 타입 (Inner, Left, Right, Outer)
- [ ] 시간 기반 윈도우 조인
- [ ] 상태 저장 (조인 대기 레코드)
- [ ] Late arrival 처리

### 4.3 Enrichment (Lookup)
**우선순위: 높음** | **예상 작업량: 중**

```yaml
stages:
  - name: user_enrichment
    type: enrich
    config:
      source:
        type: redis                          # redis, http, sql, elasticsearch
        config:
          address: "localhost:6379"
          key_template: "user:{{ .user_id }}"
      join_field: user_id
      target_field: user_info
      cache:
        enabled: true
        ttl: "5m"
        max_size: 10000
      on_missing: skip                       # skip, error, default
      default_value: {}
```

**구현 사항:**
- [ ] `pipeline-core/pkg/stream/enrich_stage.go` 생성
- [ ] 다양한 Lookup 소스 (Redis, HTTP API, SQL, ES)
- [ ] 로컬 캐시 (LRU)
- [ ] Batch lookup (성능 최적화)
- [ ] 비동기 enrichment 옵션

### 4.4 Sub-pipeline (Inline Pipeline)
**우선순위: 중** | **예상 작업량: 중**

```yaml
stages:
  - name: nested_processing
    type: sub_pipeline
    config:
      condition: '.type == "complex_event"'  # 조건부 실행
      pipeline:
        stages:
          - type: split
            config:
              field: items
              target: item
          - type: enrich
            config:
              source: product_api
              join_field: item.product_id
          - type: aggregate
            config:
              group_by: [category]
              aggregations:
                - field: total
                  function: sum
                  source: item.price
```

**구현 사항:**
- [ ] `pipeline-core/pkg/stream/subpipeline_stage.go` 생성
- [ ] 중첩 파이프라인 실행
- [ ] 조건부 실행
- [ ] 결과 병합

---

## Phase 5: GUI 개선

### 5.1 Visual Pipeline Builder
**우선순위: 중** | **예상 작업량: 높음**

```
┌─────────────────────────────────────────────────────────────────┐
│ Visual Pipeline Builder                              [Save] [Run]│
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────┐     ┌─────────┐     ┌─────────┐     ┌─────────┐   │
│  │  Kafka  │────►│ Filter  │────►│ Router  │────►│   ES    │   │
│  │  Input  │     │         │     │         │     │ Output  │   │
│  └─────────┘     └─────────┘     └────┬────┘     └─────────┘   │
│                                       │                         │
│                                       │          ┌─────────┐   │
│                                       └─────────►│   S3    │   │
│                                                  │ Output  │   │
│                                                  └─────────┘   │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│ Components:  [Input ▼]  [Stage ▼]  [Output ▼]                   │
└─────────────────────────────────────────────────────────────────┘
```

**구현 사항:**
- [ ] React Flow 기반 노드 에디터
- [ ] 드래그앤드롭 컴포넌트 추가
- [ ] 노드 간 연결선 드래그
- [ ] 실시간 유효성 검사
- [ ] YAML 자동 생성

### 5.2 실시간 데이터 미리보기
**우선순위: 중** | **예상 작업량: 중**

```
┌─────────────────────────────────────────────────────────────────┐
│ Data Preview                                        [▶ Sample]  │
├─────────────────────────────────────────────────────────────────┤
│ Stage: Filter (.status == "active")                             │
├─────────────────────────────────────────────────────────────────┤
│ Input Record                    │ Output Record                 │
│ ┌─────────────────────────────┐ │ ┌─────────────────────────┐   │
│ │ {                           │ │ │ {                       │   │
│ │   "id": "123",              │ │ │   "id": "123",          │   │
│ │   "status": "active",       │→│ │   "status": "active",   │   │
│ │   "name": "Test"            │ │ │   "name": "Test"        │   │
│ │ }                           │ │ │ }                       │   │
│ └─────────────────────────────┘ │ └─────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│ Stats: 100 records sampled, 85 passed, 15 filtered             │
└─────────────────────────────────────────────────────────────────┘
```

**구현 사항:**
- [ ] Sample 데이터 수집 API
- [ ] Stage별 입/출력 비교
- [ ] 변환 결과 diff 표시
- [ ] 필터링 통계

### 5.3 파이프라인 모니터링 대시보드
**우선순위: 중** | **예상 작업량: 중**

**구현 사항:**
- [ ] 실시간 처리량 그래프
- [ ] Stage별 지연 시간
- [ ] 에러율 모니터링
- [ ] Backpressure 상태

---

## 구현 우선순위 및 일정

### 높은 우선순위 (Phase 1-2 일부)

| 작업 | 예상 시간 | 상태 |
|------|-----------|------|
| Elasticsearch Output | 3일 | ✅ 완료 (2026-03-04) |
| MongoDB Output | 2일 | ✅ 완료 (2026-03-04) |
| S3 Output | 3일 | ✅ 완료 (2026-03-04) |
| Conditional Router | 2일 | ⬜ 대기 |
| Enrich Stage (Redis/HTTP) | 3일 | ⬜ 대기 |

### 중간 우선순위 (Phase 3-4 일부)

| 작업 | 예상 시간 | 상태 |
|------|-----------|------|
| RabbitMQ Input | 2일 | ⬜ 대기 |
| SQS Input | 2일 | ⬜ 대기 |
| WebSocket Input | 2일 | ⬜ 대기 |
| Windowed Aggregation | 5일 | ⬜ 대기 |
| Dynamic Output | 2일 | ⬜ 대기 |

### 낮은 우선순위 (Phase 4-5)

| 작업 | 예상 시간 | 상태 |
|------|-----------|------|
| Stream Join | 5일 | ⬜ 대기 |
| Visual Pipeline Builder | 10일 | ⬜ 대기 |
| BigQuery Output | 3일 | ⬜ 대기 |
| Data Preview | 3일 | ⬜ 대기 |

---

## 기술 스택 추가

### 신규 의존성

```go
// go.mod 추가 예정
require (
    // Elasticsearch
    github.com/elastic/go-elasticsearch/v8 v8.x

    // MongoDB
    go.mongodb.org/mongo-driver v1.x

    // AWS
    github.com/aws/aws-sdk-go-v2 v1.x
    github.com/aws/aws-sdk-go-v2/service/s3 v1.x
    github.com/aws/aws-sdk-go-v2/service/sqs v1.x

    // GCP
    cloud.google.com/go/storage v1.x
    cloud.google.com/go/pubsub v1.x
    cloud.google.com/go/bigquery v1.x

    // Messaging
    github.com/rabbitmq/amqp091-go v1.x
    github.com/eclipse/paho.mqtt.golang v1.x

    // Parquet
    github.com/xitongsys/parquet-go v1.x
)
```

### 프론트엔드 추가 예정

```json
// package.json 추가 예정
{
  "dependencies": {
    "reactflow": "^11.x",           // Visual Pipeline Builder
    "react-diff-viewer": "^3.x"     // Data Preview diff
  }
}
```

---

## 변경 이력

| 날짜 | 버전 | 변경 내용 |
|------|------|----------|
| 2026-03-04 | 1.0 | 초기 계획 수립 |
| 2026-03-04 | 1.1 | sink → output 용어 통일, Elasticsearch Output 구현 완료 |
| 2026-03-04 | 1.2 | MongoDB Output 구현 완료 (InsertMany, BulkWrite, Upsert, 동적 Collection) |
| 2026-03-04 | 1.3 | S3 Output 구현 완료 (JSON/NDJSON/CSV, gzip, Hive 파티셔닝, MinIO 호환) |

