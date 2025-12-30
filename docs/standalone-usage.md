# Standalone 파이프라인 실행 가이드

운영툴(Control Plane, Web UI) 없이 데이터 파이프라인을 독립적으로 실행하는 방법을 설명합니다.

## 개요

Conduix의 `pipeline-core`는 독립 실행이 가능한 모듈입니다. YAML 설정 파일만으로 데이터 파이프라인을 구성하고 실행할 수 있습니다.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Standalone 실행 아키텍처                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   [YAML 설정 파일]                                                           │
│         │                                                                    │
│         ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    pipeline-core (독립 실행)                         │   │
│   │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│   │  │   Config    │  │   Actor     │  │  Pipeline   │                  │   │
│   │  │   Parser    │──│   System    │──│   Runner    │                  │   │
│   │  └─────────────┘  └─────────────┘  └─────────────┘                  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│         │                                                                    │
│         ▼                                                                    │
│   [데이터 소스] ──▶ [변환] ──▶ [데이터 싱크]                                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 빠른 시작

### 1. 빌드

```bash
cd conduix/pipeline-core

# 의존성 설치
go mod download

# 빌드
go build -o pipeline ./cmd/pipeline

# 또는 전체 프로젝트에서
cd conduix
make build-core
```

### 2. 설정 파일 작성

```yaml
# my-pipeline.yaml
version: "1.0"
name: "my-first-pipeline"

sources:
  demo_logs:
    type: demo
    interval: 1s
    format: apache_common

transforms:
  parse:
    type: remap
    inputs: ["demo_logs"]
    source: |
      . = parse_common_log!(.message)

sinks:
  console:
    type: console
    inputs: ["parse"]
    encoding: json
```

### 3. 실행

```bash
# 기본 실행
./pipeline --config my-pipeline.yaml

# 또는 go run 사용
go run ./cmd/pipeline --config my-pipeline.yaml
```

---

## 명령줄 옵션

```bash
./pipeline [옵션]

옵션:
  --config, -c <파일>     파이프라인 설정 파일 경로 (필수)
  --validate              설정 파일 검증만 수행 (실행하지 않음)
  --dry-run               시뮬레이션 모드 (실제 데이터 전송 없음)
  --log-level <레벨>      로그 레벨 (debug, info, warn, error)
  --log-format <형식>     로그 형식 (text, json)
  --metrics-port <포트>   Prometheus 메트릭 포트 (기본: 9090)
  --health-port <포트>    헬스체크 포트 (기본: 8080)
  --checkpoint-dir <경로> 체크포인트 저장 디렉토리
  --help, -h              도움말 출력
  --version, -v           버전 출력
```

### 예시

```bash
# 설정 검증만 수행
./pipeline --config my-pipeline.yaml --validate

# 디버그 로그와 함께 실행
./pipeline --config my-pipeline.yaml --log-level debug

# JSON 로그 형식으로 실행
./pipeline --config my-pipeline.yaml --log-format json

# 커스텀 포트 설정
./pipeline --config my-pipeline.yaml --metrics-port 9091 --health-port 8081

# 체크포인트 활성화
./pipeline --config my-pipeline.yaml --checkpoint-dir /var/lib/pipeline/checkpoints
```

---

## 설정 파일 형식

### Flat 구조 (기본)

Vector와 호환되는 간단한 구조입니다.

```yaml
version: "1.0"
name: "flat-pipeline"
type: flat  # 기본값, 생략 가능

# 데이터 소스 정의
sources:
  <source_name>:
    type: <source_type>
    # source 설정...

# 데이터 변환 정의
transforms:
  <transform_name>:
    type: <transform_type>
    inputs: ["<source_or_transform_name>"]
    # transform 설정...

# 데이터 출력 정의
sinks:
  <sink_name>:
    type: <sink_type>
    inputs: ["<source_or_transform_name>"]
    # sink 설정...

# 체크포인트 설정 (선택)
checkpoint:
  enabled: true
  storage: file  # file 또는 redis
  directory: ./checkpoints
  interval: 10s
```

### 계층적 Actor 구조 (고급)

Apache Flink와 유사한 계층적 구조로, 세밀한 장애 복구 제어가 가능합니다.

```yaml
version: "1.0"
name: "actor-pipeline"
type: actor

# Actor 시스템 설정
actor_system:
  dispatcher:
    type: fork-join
    parallelism: 8
  mailbox:
    capacity: 10000
    overflow_strategy: backpressure

# 계층적 파이프라인 정의
pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
    max_restarts: 3
    within_seconds: 60

  children:
    - name: "SourceSupervisor"
      type: supervisor
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            # kafka 설정...
          outputs: ["TransformSupervisor"]

    - name: "TransformSupervisor"
      type: supervisor
      children:
        - name: "Parser"
          type: transform
          config:
            transform_type: remap
            # remap 설정...
          outputs: ["SinkSupervisor"]

    - name: "SinkSupervisor"
      type: supervisor
      children:
        - name: "Output"
          type: sink
          config:
            sink_type: console
            # sink 설정...

checkpoint:
  enabled: true
  storage: file
  directory: ./checkpoints
  interval: 10s
```

---

## Source 타입

### demo (테스트용)

```yaml
sources:
  demo:
    type: demo
    interval: 1s              # 데이터 생성 간격
    format: apache_common     # apache_common, syslog, json
    count: 0                  # 생성 개수 (0 = 무한)
```

### kafka

```yaml
sources:
  kafka_input:
    type: kafka
    brokers: ["localhost:9092"]
    topics: ["my-topic"]
    group_id: "my-consumer-group"
    auto_offset_reset: earliest  # earliest, latest

    # 인증 (선택)
    sasl:
      mechanism: PLAIN  # PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
      username: "user"
      password: "password"

    # TLS (선택)
    tls:
      enabled: true
      ca_file: /path/to/ca.crt
      cert_file: /path/to/client.crt
      key_file: /path/to/client.key
```

### http_server

```yaml
sources:
  http_input:
    type: http_server
    address: "0.0.0.0:8080"
    path: "/ingest"
    method: POST

    # 인증 (선택)
    auth:
      type: basic  # basic, bearer
      username: "admin"
      password: "secret"
```

### file

```yaml
sources:
  file_input:
    type: file
    include: ["/var/log/*.log"]
    exclude: ["/var/log/debug.log"]
    read_from: beginning  # beginning, end

    # 파일 로테이션 처리
    multiline:
      start_pattern: '^\d{4}-\d{2}-\d{2}'
      mode: continue_through
      timeout_ms: 1000
```

---

## Transform 타입

### remap (VRL 변환)

```yaml
transforms:
  parse_json:
    type: remap
    inputs: ["source"]
    source: |
      # JSON 파싱
      . = parse_json!(.message)

      # 필드 추가
      .processed_at = now()
      .environment = "production"

      # 필드 제거
      del(.debug_info)

      # 조건부 변환
      if .level == "error" {
        .priority = "high"
      }
```

### filter

```yaml
transforms:
  filter_errors:
    type: filter
    inputs: ["parse"]
    condition: '.level == "error" || .level == "warn"'
```

### aggregate (집계)

```yaml
transforms:
  count_by_status:
    type: aggregate
    inputs: ["parse"]
    window:
      type: tumbling  # tumbling, sliding
      size: 60s
    group_by: [".status_code"]
    aggregations:
      - field: "count"
        function: count
      - field: "avg_response_time"
        function: avg
        source: ".response_time"
```

### sample (샘플링)

```yaml
transforms:
  sample_10_percent:
    type: sample
    inputs: ["parse"]
    rate: 0.1  # 10% 샘플링
```

### route (라우팅)

```yaml
transforms:
  route_by_level:
    type: route
    inputs: ["parse"]
    routes:
      errors: '.level == "error"'
      warnings: '.level == "warn"'
      _default: true  # 매칭되지 않은 경우
```

---

## Sink 타입

### console (표준출력)

```yaml
sinks:
  stdout:
    type: console
    inputs: ["transform"]
    encoding: json  # json, text
    pretty: true    # JSON 포맷팅
```

### elasticsearch

```yaml
sinks:
  es_output:
    type: elasticsearch
    inputs: ["transform"]
    endpoints: ["http://localhost:9200"]
    index: "logs-%Y-%m-%d"  # 날짜 패턴 지원

    # 인증
    auth:
      strategy: basic
      user: "elastic"
      password: "password"

    # 버퍼링
    buffer:
      max_events: 5000
      max_bytes: 10mb
      timeout: 10s

    # 재시도
    retry:
      max_retries: 3
      initial_backoff: 1s
      max_backoff: 30s
```

### kafka

```yaml
sinks:
  kafka_output:
    type: kafka
    inputs: ["transform"]
    brokers: ["localhost:9092"]
    topic: "output-topic"

    # 파티셔닝
    partition_key: "{{ .user_id }}"

    # 압축
    compression: snappy  # none, gzip, snappy, lz4, zstd

    # 버퍼링
    buffer:
      max_events: 10000
      timeout: 5s
```

### s3

```yaml
sinks:
  s3_output:
    type: s3
    inputs: ["transform"]
    bucket: "my-data-bucket"
    prefix: "logs/"
    region: "ap-northeast-2"

    # 인증 (환경변수 또는 IAM 역할 사용 가능)
    auth:
      access_key_id: "${AWS_ACCESS_KEY_ID}"
      secret_access_key: "${AWS_SECRET_ACCESS_KEY}"

    # 파일 설정
    filename_template: "{{ .timestamp }}-{{ .uuid }}.json"
    compression: gzip

    # 파티셔닝
    partition_by:
      - ".date"
      - ".service"
```

### file

```yaml
sinks:
  file_output:
    type: file
    inputs: ["transform"]
    path: "/var/log/pipeline/output-%Y-%m-%d.log"
    encoding: json

    # 로테이션
    rotation:
      max_size: 100mb
      max_age: 7d
```

---

## 체크포인트 설정

### 파일 기반 체크포인트

```yaml
checkpoint:
  enabled: true
  storage: file
  directory: ./checkpoints
  interval: 10s

  # 복구 옵션
  on_startup: restore_latest  # restore_latest, ignore
```

### Redis 기반 체크포인트

```yaml
checkpoint:
  enabled: true
  storage: redis
  interval: 10s

  redis:
    host: localhost
    port: 6379
    password: ""
    db: 0
    key_prefix: "pipeline:checkpoint:"
```

---

## 환경 변수 사용

설정 파일에서 환경 변수를 참조할 수 있습니다.

```yaml
sources:
  kafka:
    type: kafka
    brokers: ["${KAFKA_BROKERS}"]

sinks:
  elasticsearch:
    type: elasticsearch
    endpoints: ["${ES_ENDPOINT}"]
    auth:
      user: "${ES_USER}"
      password: "${ES_PASSWORD}"
```

```bash
# 환경 변수 설정 후 실행
export KAFKA_BROKERS="kafka1:9092,kafka2:9092"
export ES_ENDPOINT="http://elasticsearch:9200"
export ES_USER="elastic"
export ES_PASSWORD="secret"

./pipeline --config my-pipeline.yaml
```

---

## 실행 예시

### 예시 1: Kafka to Elasticsearch

```yaml
# kafka-to-es.yaml
version: "1.0"
name: "kafka-to-elasticsearch"

sources:
  kafka:
    type: kafka
    brokers: ["localhost:9092"]
    topics: ["application-logs"]
    group_id: "pipeline-consumer"

transforms:
  parse:
    type: remap
    inputs: ["kafka"]
    source: |
      . = parse_json!(.message)
      .@timestamp = now()
      .index_name = "logs-" + format_timestamp!(.@timestamp, "%Y-%m-%d")

  filter_errors:
    type: filter
    inputs: ["parse"]
    condition: '.level == "error"'

sinks:
  elasticsearch:
    type: elasticsearch
    inputs: ["filter_errors"]
    endpoints: ["http://localhost:9200"]
    index: "{{ .index_name }}"

checkpoint:
  enabled: true
  storage: file
  directory: ./checkpoints
  interval: 10s
```

```bash
./pipeline --config kafka-to-es.yaml
```

### 예시 2: HTTP 수집 서버

```yaml
# http-collector.yaml
version: "1.0"
name: "http-log-collector"

sources:
  http:
    type: http_server
    address: "0.0.0.0:8080"
    path: "/logs"

transforms:
  enrich:
    type: remap
    inputs: ["http"]
    source: |
      .received_at = now()
      .collector = "http-collector"

sinks:
  kafka:
    type: kafka
    inputs: ["enrich"]
    brokers: ["localhost:9092"]
    topic: "collected-logs"
```

```bash
./pipeline --config http-collector.yaml

# 테스트
curl -X POST http://localhost:8080/logs \
  -H "Content-Type: application/json" \
  -d '{"message": "test log", "level": "info"}'
```

### 예시 3: 파일 모니터링

```yaml
# file-monitor.yaml
version: "1.0"
name: "file-monitor"

sources:
  files:
    type: file
    include: ["/var/log/app/*.log"]
    read_from: end

transforms:
  parse:
    type: remap
    inputs: ["files"]
    source: |
      parsed = parse_regex!(.message, r'^(?P<timestamp>\S+) (?P<level>\w+) (?P<msg>.*)$')
      .timestamp = parsed.timestamp
      .level = parsed.level
      .message = parsed.msg

sinks:
  console:
    type: console
    inputs: ["parse"]
    encoding: json
```

```bash
./pipeline --config file-monitor.yaml
```

### 예시 4: Actor 모델 기반 복잡한 파이프라인

```yaml
# complex-pipeline.yaml
version: "1.0"
name: "multi-source-analytics"
type: actor

actor_system:
  dispatcher:
    parallelism: 4
  mailbox:
    capacity: 5000
    overflow_strategy: backpressure

pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
    max_restarts: 5
    within_seconds: 300

  children:
    - name: "Sources"
      type: supervisor
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            brokers: ["localhost:9092"]
            topics: ["events"]
          outputs: ["Transforms"]

        - name: "HTTPSource"
          type: source
          config:
            source_type: http_server
            address: "0.0.0.0:8080"
            path: "/ingest"
          outputs: ["Transforms"]

    - name: "Transforms"
      type: supervisor
      children:
        - name: "Parser"
          type: transform
          parallelism: 2
          config:
            transform_type: remap
            source: '. = parse_json!(.message)'
          outputs: ["Router"]

        - name: "Router"
          type: router
          config:
            routing:
              - condition: '.type == "metric"'
                output: "MetricsSink"
              - condition: '.type == "log"'
                output: "LogsSink"
              - condition: 'true'
                output: "DefaultSink"

    - name: "Sinks"
      type: supervisor
      children:
        - name: "MetricsSink"
          type: sink
          config:
            sink_type: console
            prefix: "[METRICS] "

        - name: "LogsSink"
          type: sink
          config:
            sink_type: elasticsearch
            endpoints: ["http://localhost:9200"]
            index: "logs"

        - name: "DefaultSink"
          type: sink
          config:
            sink_type: file
            path: "/var/log/pipeline/default.log"

checkpoint:
  enabled: true
  storage: file
  directory: ./checkpoints
  interval: 10s
```

```bash
./pipeline --config complex-pipeline.yaml --log-level debug
```

---

## Docker 실행

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY pipeline-core/ ./pipeline-core/
COPY shared/ ./shared/
WORKDIR /app/pipeline-core
RUN go build -o /pipeline ./cmd/pipeline

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /pipeline /usr/local/bin/pipeline
ENTRYPOINT ["pipeline"]
CMD ["--help"]
```

### 빌드 및 실행

```bash
# 이미지 빌드
docker build -t conduix:standalone -f Dockerfile.standalone .

# 실행
docker run -v $(pwd)/config:/config \
  conduix:standalone \
  --config /config/my-pipeline.yaml

# 포트 노출과 함께 실행
docker run -p 8080:8080 -p 9090:9090 \
  -v $(pwd)/config:/config \
  conduix:standalone \
  --config /config/http-collector.yaml
```

### Docker Compose

```yaml
# docker-compose.standalone.yml
version: '3.8'

services:
  pipeline:
    build:
      context: .
      dockerfile: Dockerfile.standalone
    volumes:
      - ./configs:/config
      - ./checkpoints:/checkpoints
    environment:
      - KAFKA_BROKERS=kafka:9092
      - ES_ENDPOINT=http://elasticsearch:9200
    command: ["--config", "/config/my-pipeline.yaml", "--checkpoint-dir", "/checkpoints"]
    depends_on:
      - kafka
      - elasticsearch
    restart: unless-stopped

  kafka:
    image: confluentinc/cp-kafka:7.5.0
    # kafka 설정...

  elasticsearch:
    image: elasticsearch:8.11.0
    # elasticsearch 설정...
```

```bash
docker-compose -f docker-compose.standalone.yml up -d
```

---

## 헬스체크 및 메트릭

### 헬스체크 엔드포인트

```bash
# 기본 포트: 8080
curl http://localhost:8080/health
# {"status": "healthy", "uptime": "1h23m45s"}

curl http://localhost:8080/ready
# {"status": "ready", "pipelines": 1}
```

### Prometheus 메트릭

```bash
# 기본 포트: 9090
curl http://localhost:9090/metrics

# 주요 메트릭:
# pipeline_events_processed_total
# pipeline_events_failed_total
# pipeline_checkpoint_success_total
# pipeline_checkpoint_failed_total
# pipeline_actor_restarts_total
# pipeline_source_lag_seconds
```

### Grafana 대시보드 예시

```json
{
  "panels": [
    {
      "title": "처리량 (events/sec)",
      "targets": [
        {"expr": "rate(pipeline_events_processed_total[1m])"}
      ]
    },
    {
      "title": "에러율",
      "targets": [
        {"expr": "rate(pipeline_events_failed_total[1m]) / rate(pipeline_events_processed_total[1m])"}
      ]
    }
  ]
}
```

---

## 트러블슈팅

### 일반적인 문제

#### 1. 설정 파일 오류

```bash
# 설정 검증
./pipeline --config my-pipeline.yaml --validate

# 출력 예시:
# ✗ Error: transforms.parse: unknown field 'sources'
# ✗ Error: sinks.output: missing required field 'inputs'
```

#### 2. Kafka 연결 실패

```bash
# 디버그 로그 활성화
./pipeline --config my-pipeline.yaml --log-level debug

# 로그 확인:
# [DEBUG] Connecting to Kafka broker: localhost:9092
# [ERROR] Failed to connect: dial tcp: connection refused
```

해결:
- Kafka 브로커 주소 확인
- 네트워크 연결 확인
- SASL/TLS 설정 확인

#### 3. 체크포인트 복구 실패

```bash
# 체크포인트 디렉토리 확인
ls -la ./checkpoints/

# 체크포인트 무시하고 시작
./pipeline --config my-pipeline.yaml --checkpoint-dir ""
```

#### 4. 메모리 부족

```yaml
# 버퍼 크기 조정
actor_system:
  mailbox:
    capacity: 1000  # 기본 10000에서 축소
    overflow_strategy: drop_oldest  # backpressure 대신 삭제
```

### 로그 레벨별 정보

| 레벨 | 출력 내용 |
|-----|----------|
| error | 오류만 출력 |
| warn | 경고 및 오류 |
| info | 일반 운영 정보 (기본값) |
| debug | 상세 디버깅 정보 |

---

## 운영툴과의 비교

| 기능 | Standalone | 운영툴 사용 |
|-----|-----------|-----------|
| 파이프라인 실행 | O | O |
| YAML 설정 | O | O |
| 체크포인트/복구 | O (파일/Redis) | O (Redis) |
| 웹 UI | X | O |
| 스케줄링 | X (외부 cron 사용) | O |
| 멀티 에이전트 | X | O |
| 자동 장애 조치 | 제한적 | O |
| 실행 히스토리 | X | O |
| 사용자 인증 | X | O (SSO) |
| 실시간 모니터링 | 메트릭만 | 대시보드 |

### 언제 Standalone을 사용할까?

- 단일 파이프라인만 필요한 경우
- 간단한 데이터 처리 작업
- CI/CD 파이프라인 내 데이터 처리
- 개발/테스트 환경
- 리소스가 제한된 환경

### 언제 운영툴을 사용할까?

- 다수의 파이프라인 관리
- 팀 협업 필요
- 스케줄링 필요
- 웹 기반 관리 필요
- 고가용성 요구
