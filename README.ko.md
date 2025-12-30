<p align="center">
  <img src="images/logo-with-title.png" alt="Conduix Logo" width="600">
</p>

**Conduix** = **Intelligent Conduit**

데이터를 연결하고, 흐름을 제어하고, 파이프라인을 조직하는 플랫폼 서비스

Actor Model 기반의 대용량 데이터 파이프라인 플랫폼

[English](README.md)

## 개요

Conduix는 [Bento](https://github.com/warpstreamlabs/bento)(MIT 라이선스)의 검증된 커넥터와 Apache Flink 스타일의 Actor Model을 결합한 확장 가능한 데이터 파이프라인 시스템입니다.

**하이브리드 아키텍처**:
- **Actor System**: Supervisor 패턴, Mailbox, Backpressure 등 Flink 스타일 제어
- **Bento Connectors**: Kafka, Elasticsearch, S3 등 검증된 커넥터 재사용
- **순수 Go**: 단일 바이너리, 외부 의존성 없음

## 주요 기능

- **Actor Model 기반 파이프라인**: 계층적 Supervisor 패턴으로 자동 장애 복구
- **Flat/계층적 구조 지원**: 간단한 flat 구조와 고급 계층적 Actor 구조 모두 지원
- **Bento 커넥터 통합**: Kafka, ES, S3, HTTP, NATS, AMQP 등 풍부한 커넥터
- **고가용성**: Redis 기반 체크포인트, 자동 장애 대응
- **운영툴**: 웹 기반 파이프라인 설정, 모니터링, 스케줄링
- **SSO 지원**: OAuth2/OIDC 기반 로그인
- **유연한 배포**: 물리서버, Docker, Kubernetes(Helm) 지원

## 아키텍처

```
┌─────────────────────────────────────────────────────────────┐
│                    Control Plane (운영툴)                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │   Web UI    │  │  API Server │  │  Scheduler  │         │
│  │  (React)    │  │  (Go+Gin)   │  │  (Go+Cron)  │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
└─────────────────────────────────────────────────────────────┘
                              │
                    REST API + Redis Pub/Sub
                              │
┌─────────────────────────────────────────────────────────────┐
│                   Pipeline Agent Cluster                     │
│  ┌───────────────────────────────────────────────────┐      │
│  │  Agent (Actor System + Bento Connectors)          │      │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐           │      │
│  │  │ Source  │→ │Transform│→ │  Sink   │           │      │
│  │  │ (Kafka) │  │(Bloblang│  │  (ES)   │           │      │
│  │  └─────────┘  └─────────┘  └─────────┘           │      │
│  └───────────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## 파이프라인 설계

### 핵심 개념

Conduix는 **Unix Pipe 스타일의 선형 파이프라인**과 **DataType 기반 DAG**을 결합한 설계를 사용합니다.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           파이프라인 설계 철학                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  1. 단일 파이프라인 = Unix Pipe (선형 체이닝)                                │
│     ┌────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌──────┐    │
│     │ Source │───→│ Stage 1 │───→│ Stage 2 │───→│ Stage 3 │───→│ Sink │    │
│     └────────┘    └─────────┘    └─────────┘    └─────────┘    └──────┘    │
│                                                                               │
│  2. 다중 파이프라인 = DataType 종속성 DAG                                    │
│     ┌──────────────┐                                                         │
│     │ 게시판 파이프라인│ (DataType: Board)                                    │
│     └───────┬──────┘                                                         │
│             │ 트리거                                                          │
│             ▼                                                                 │
│     ┌──────────────┐                                                         │
│     │ 게시글 파이프라인│ (DataType: Post, Parent: Board)                      │
│     └───────┬──────┘                                                         │
│             │ 트리거                                                          │
│             ▼                                                                 │
│     ┌──────────────┐                                                         │
│     │ 댓글 파이프라인 │ (DataType: Comment, Parent: Post)                     │
│     └──────────────┘                                                         │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Stage: 추상화 단위

**Stage**는 `input → output` 인터페이스를 따르는 핵심 추상화 단위입니다. 구현에 따라 역할이 결정됩니다:

| Stage 타입 | 설명 | 사용 예시 |
|------------|------|----------|
| **FilterStage** | 조건에 따라 레코드 필터링 | 유효하지 않은 데이터 제거 |
| **RemapStage** | 필드 변환/이름 변경 | JSON 필드 매핑 |
| **AggregateStage** | 윈도우 기반 집계 | count, sum, average |
| **EnrichStage** | 외부 데이터 추가 | 룩업 테이블 조인 |
| **ElasticsearchStage** | Elasticsearch에 저장 | 문서 인덱싱 |
| **KafkaStage** | Kafka로 전송 | 파이프라인 간 경계 |
| **TriggerStage** | 다른 파이프라인 트리거 | 부모-자식 파이프라인 연동 |

### DataType 종속 관계 패턴

#### 패턴 1: 다른 DataType (계층적 수집)

```
사용 사례: 게시판 수집 → 각 게시판의 게시글 수집

파이프라인 A: 게시판 수집
  API(/boards) → Transform → Elasticsearch
  Target DataType: Board

파이프라인 B: 게시글 수집
  API(/boards/{board_id}/posts) → Transform → Elasticsearch
  Target DataType: Post
  Parent DataType: Board  ← 다름!

실행: 파이프라인 A 완료 → 파이프라인 B 시작 (각 게시판마다)
```

#### 패턴 2: 같은 DataType, 다른 처리 (Fan-out)

```
사용 사례: 같은 데이터를 다르게 처리해야 할 때

파이프라인 1: 주문 수집 (Source)
  API → Kafka
  Target DataType: Order

파이프라인 2: 주문 상세 저장 (Consumer A)
  Kafka → Enrich → Elasticsearch
  Target DataType: Order  ← 같음!
  Parent: 파이프라인 1

파이프라인 3: 주문 분석 (Consumer B)
  Kafka → Aggregate → Dashboard DB
  Target DataType: Order  ← 같음!
  Parent: 파이프라인 1

┌──────────────┐
│ 파이프라인 1  │ API → Kafka
│ (Order)      │
└──────┬───────┘
       │ Kafka Topic
       ├─────────────────────┐
       ▼                     ▼
┌──────────────┐     ┌──────────────┐
│ 파이프라인 2  │     │ 파이프라인 3  │
│ (Order→ES)   │     │ (Order→집계) │
└──────────────┘     └──────────────┘
```

### Router Stage (파이프라인 내 분기 - 선택적)

Kafka 없이 단일 파이프라인 내에서 간단한 분기가 필요할 때:

```yaml
stages:
  - id: router-1
    type: router
    config:
      mode: fan_out          # fan_out | condition | filter
      routes:
        - name: es-path
          next: stage-es
        - name: agg-path
          next: stage-agg

  - id: stage-es
    type: elasticsearch
    config: {...}

  - id: stage-agg
    type: aggregate
    config: {...}
```

| Router 모드 | 설명 | 사용 사례 |
|-------------|------|----------|
| **fan_out** | 모든 경로로 복제 | 같은 데이터를 ES + DB에 저장 |
| **condition** | 첫 매칭 경로만 실행 | 에러/성공 분기 처리 |
| **filter** | 조건 맞는 경로들만 실행 | 태그별 라우팅 |

### 언제 어떤 방식을 사용할까

| 시나리오 | 권장 방식 |
|----------|----------|
| 다른 데이터 타입 (게시판→게시글) | DataType 종속 관계 |
| 같은 데이터, 다른 처리 | Kafka 경계 + 별도 파이프라인 |
| 간단한 프로세스 내 분기 | Router Stage |
| 독립적 스케일링 필요 | Kafka 경계 + 별도 파이프라인 |
| 장애 격리 필요 | Kafka 경계 + 별도 파이프라인 |
| 낮은 지연시간 필요 | Router Stage (Kafka 홉 없음) |

## 프로젝트 구조

```
conduix/
├── pipeline-core/     # 파이프라인 코어 (Actor 시스템, Bento 통합)
├── pipeline-agent/    # 파이프라인 실행 에이전트
├── control-plane/     # 운영툴 백엔드 API
├── web-ui/            # 운영툴 프론트엔드
├── shared/            # 공유 타입/상수
└── deploy/            # 배포 (Docker, Helm, 스크립트)
```

## 문서

- [Standalone 파이프라인 실행 가이드](docs/standalone-usage.md) - 운영툴 없이 독립 실행
- [장애 처리 시나리오](#장애-처리-fault-tolerance) - Redis/Kafka 장애 대응

## 빠른 시작

### 사전 요구사항

- Go 1.21+
- Node.js 18+
- Docker & Docker Compose
- MySQL 8.0
- Redis 7.0

### 개발 환경 실행

```bash
# 의존성 설치
make deps

# 인프라 실행 (MySQL, Redis)
make infra-up

# 모든 서비스 빌드
make build

# 개발 모드 실행
make dev
```

### Docker Compose로 실행

```bash
docker-compose up -d
```

### Kubernetes 배포

```bash
helm install conduix ./deploy/helm/conduix
```

## 파이프라인 설정 예시

### Flat 구조 (Bento 호환)

```yaml
version: "1.0"
name: "log-pipeline"

sources:
  kafka_input:
    type: kafka
    brokers: ["kafka:9092"]
    topics: ["logs"]

transforms:
  parse:
    type: remap
    inputs: ["kafka_input"]
    source: '. = parse_json!(.message)'

sinks:
  elasticsearch:
    type: elasticsearch
    inputs: ["parse"]
    endpoints: ["http://es:9200"]
```

### 계층적 Actor 구조

```yaml
version: "1.0"
name: "analytics-pipeline"
type: actor

pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
    max_restarts: 3

  children:
    - name: "SourceSupervisor"
      type: supervisor
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            brokers: ["kafka:9092"]
```

## 장애 처리 (Fault Tolerance)

Conduix은 다양한 장애 시나리오에 대응하는 복원력(Resilience) 메커니즘을 내장하고 있습니다.

### 장애 처리 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           장애 복구 메커니즘                                      │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                    ResilientClient (Redis 공통)                           │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │   │
│  │  │Auto-Reconnect│  │Circuit Breaker│  │ Local Cache │  │Auto-Resubscribe│ │   │
│  │  │(Exp.Backoff) │  │ (Open/Close) │  │  (Fallback) │  │   (Pub/Sub)   │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                  Actor Supervisor (Kafka/Source 공통)                     │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │   │
│  │  │  Checkpoint  │  │   Restart    │  │   Backoff    │  │    Offset    │  │   │
│  │  │   Recovery   │  │   Strategy   │  │   Strategy   │  │   Tracking   │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

### Redis 장애 시나리오

Redis는 Control Plane과 Agent 간의 통신, 체크포인트 저장, 실시간 메트릭 전달에 사용됩니다.

#### 장애 유형별 대응

| 시나리오 | Agent 동작 | Control Plane 동작 | 복구 방식 |
|---------|-----------|-------------------|----------|
| **일시적 네트워크 단절** (< 30초) | 자동 재연결 (Exponential Backoff), 로컬 캐시 사용 | Circuit Breaker 활성화, 요청 대기 | 연결 복구 시 자동 정상화 |
| **Redis 서버 다운** | REST API 폴백 모드 전환 | 명령 큐에 저장, 202 Accepted 응답 | Redis 재시작 후 대기 명령 재전송 |
| **Redis 장기 장애** (> 5분) | REST 폴링으로 명령 수신 (5초 간격) | DB 기반 상태 관리로 전환 | 수동 개입 없이 계속 운영 |
| **Redis 복구** | Hybrid → Redis 모드 복귀, Pub/Sub 재구독 | 대기 명령 일괄 재전송 | 자동 |

#### Agent의 통신 모드 전환

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Agent 통신 모드 상태 다이어그램                         │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                    Redis 연결 성공                                           │
│                         │                                                    │
│                         ▼                                                    │
│   ┌─────────────────────────────────────┐                                   │
│   │         ModeRedis (기본)            │◀──────────────────────┐           │
│   │  • Redis Pub/Sub로 명령 수신         │                       │           │
│   │  • Redis에 하트비트 저장             │     Redis 연결 복구    │           │
│   │  • 실시간 메트릭 전송                │     (안정화 후)        │           │
│   └──────────────┬──────────────────────┘                       │           │
│                  │                                               │           │
│                  │ Redis 연결 실패 감지                          │           │
│                  ▼                                               │           │
│   ┌─────────────────────────────────────┐                       │           │
│   │          ModeHybrid                 │───────────────────────┘           │
│   │  • Redis + REST 동시 시도           │                                   │
│   │  • 점진적 복구 확인                  │                                   │
│   └──────────────┬──────────────────────┘                                   │
│                  │                                                           │
│                  │ Redis 계속 실패                                           │
│                  ▼                                                           │
│   ┌─────────────────────────────────────┐                                   │
│   │          ModeREST (폴백)            │                                   │
│   │  • REST API로 하트비트 전송          │                                   │
│   │  • REST 폴링으로 명령 수신 (5초)     │                                   │
│   │  • 기능 제한 없이 운영 지속          │                                   │
│   └─────────────────────────────────────┘                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### ResilientClient 주요 설정

```yaml
# 재연결 설정
max_retries: 0              # 무한 재시도
initial_backoff: 100ms      # 초기 대기 시간
max_backoff: 30s            # 최대 대기 시간
backoff_multiplier: 2.0     # 백오프 증가 배수

# Circuit Breaker 설정
failure_threshold: 5        # Circuit Open까지 실패 횟수
success_threshold: 2        # Circuit Close까지 성공 횟수
open_timeout: 30s           # Circuit Open 유지 시간

# 로컬 캐시 설정 (읽기 폴백)
enable_local_cache: true
local_cache_ttl: 5m
local_cache_max_size: 1000
```

#### Redis 장애 시 데이터 보존

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Redis 장애 시 데이터 흐름                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  [Control Plane]                                                             │
│       │                                                                      │
│       │ 파이프라인 시작 명령                                                  │
│       ▼                                                                      │
│  ┌─────────────┐    Redis 실패    ┌─────────────────────────────┐           │
│  │ Redis 전송  │ ───────────────▶ │ Pending Queue (메모리)       │           │
│  │   시도      │                  │ • TTL: 24시간                │           │
│  └─────────────┘                  │ • Redis 복구 시 자동 재전송   │           │
│       │                           └─────────────────────────────┘           │
│       │ Redis 성공                                                          │
│       ▼                                                                      │
│  ┌─────────────┐                  ┌─────────────────────────────┐           │
│  │   Agent     │ ────────────────▶│ 실행 상태 → MySQL 저장       │           │
│  │  명령 수신   │                  │ (장애 시에도 상태 추적 가능)  │           │
│  └─────────────┘                  └─────────────────────────────┘           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

### Kafka 장애 시나리오

Kafka는 Source(데이터 수집)와 Sink(데이터 전송)에서 사용됩니다.

#### 장애 유형별 대응

| 시나리오 | Source Actor 동작 | Sink Actor 동작 | 데이터 보장 |
|---------|------------------|-----------------|------------|
| **Broker 일시 단절** | 자동 재연결, Consumer 재시작 | 버퍼링 후 재전송 | At-least-once |
| **Broker 다운** | Supervisor가 Actor 재시작 | 로컬 버퍼에 저장 | At-least-once |
| **Partition Rebalance** | Offset 재조정, 체크포인트 복구 | 진행 중 배치 완료 후 재연결 | Exactly-once (with checkpoint) |
| **Leader 변경** | 새 Leader 자동 감지 | 새 Leader로 자동 전환 | At-least-once |
| **토픽 삭제/권한 오류** | 에러 로깅, Supervisor에 보고 | 에러 로깅, 재시도 중단 | 수동 개입 필요 |

#### Kafka Source Actor 복구 흐름

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Kafka Source Actor 장애 복구 흐름                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────┐     │
│  │                    SourceSupervisor                                 │     │
│  │                                                                     │     │
│  │   전략: one_for_one (실패한 Actor만 재시작)                          │     │
│  │   max_restarts: 5 (5분 내 최대 5회)                                 │     │
│  │                                                                     │     │
│  └────────────────────────────────────────────────────────────────────┘     │
│         │                    │                    │                          │
│         ▼                    ▼                    ▼                          │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐                    │
│  │ KafkaSource │     │ KafkaSource │     │ KafkaSource │                    │
│  │ Partition-0 │     │ Partition-1 │     │ Partition-2 │                    │
│  └──────┬──────┘     └─────────────┘     └─────────────┘                    │
│         │                                                                    │
│         │ Broker 연결 실패                                                   │
│         ▼                                                                    │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ 1. Actor Crash → Supervisor에 알림                                   │    │
│  │ 2. Supervisor가 재시작 정책 확인                                      │    │
│  │ 3. 체크포인트에서 마지막 offset 조회                                   │    │
│  │ 4. 새 Actor 생성 → offset부터 재시작                                  │    │
│  │ 5. 재시작 실패 시 Backoff (1s → 2s → 4s → 8s → 16s)                  │    │
│  │ 6. max_restarts 초과 시 Supervisor에 escalate                        │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 체크포인트 기반 복구 (Exactly-Once)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Kafka 체크포인트 복구 메커니즘                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  [정상 운영 시]                                                              │
│                                                                              │
│  Kafka ──▶ Source Actor ──▶ Transform ──▶ Sink                             │
│    │              │                          │                               │
│    │              │ 주기적 체크포인트 (10초)   │                               │
│    │              ▼                          ▼                               │
│    │       ┌─────────────────────────────────────┐                          │
│    │       │           Redis                     │                          │
│    │       │  pipeline:{id}:checkpoint           │                          │
│    │       │  {                                  │                          │
│    │       │    "kafka_offsets": {               │                          │
│    └──────▶│      "partition_0": 12345,          │                          │
│            │      "partition_1": 67890           │                          │
│            │    },                               │                          │
│            │    "processed_count": 1000000,      │                          │
│            │    "timestamp": "2025-01-01T12:00"  │                          │
│            │  }                                  │                          │
│            └─────────────────────────────────────┘                          │
│                                                                              │
│  ─────────────────────────────────────────────────────────────────────────  │
│                                                                              │
│  [장애 복구 시]                                                              │
│                                                                              │
│  1. Agent 또는 Actor 재시작                                                  │
│  2. Redis에서 체크포인트 조회                                                │
│  3. Kafka Consumer를 저장된 offset으로 seek                                 │
│  4. 해당 지점부터 재처리 시작                                                │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Kafka Consumer                                                      │    │
│  │  consumer.Seek(partition_0, 12345)  // 마지막 체크포인트              │    │
│  │  consumer.Seek(partition_1, 67890)                                   │    │
│  │  // 12345, 67890 offset 이후부터 재처리                               │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Kafka Sink 버퍼링 전략

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Kafka Sink 버퍼링 및 재시도                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Transform ──▶ Sink Actor ──▶ Buffer ──▶ Kafka Producer                    │
│                    │            │              │                             │
│                    │            │              │ 전송 실패                    │
│                    │            │              ▼                             │
│                    │            │    ┌─────────────────┐                    │
│                    │            │    │  Retry Queue    │                    │
│                    │            │    │  • 최대 3회      │                    │
│                    │            │    │  • Backoff 적용  │                    │
│                    │            │    └────────┬────────┘                    │
│                    │            │             │                              │
│                    │            │             │ 3회 실패                     │
│                    │            │             ▼                              │
│                    │            │    ┌─────────────────┐                    │
│                    │            │    │   Dead Letter   │                    │
│                    │            └───▶│   Queue (DLQ)   │                    │
│                    │                 │  • 로컬 파일     │                    │
│                    │                 │  • 별도 토픽     │                    │
│                    │                 └─────────────────┘                    │
│                    │                                                         │
│                    │ Buffer 설정                                             │
│                    │ ┌────────────────────────────────┐                     │
│                    └▶│ max_events: 5000               │                     │
│                      │ max_bytes: 10MB                │                     │
│                      │ timeout: 10s                   │                     │
│                      │ overflow: block | drop_oldest  │                     │
│                      └────────────────────────────────┘                     │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Supervision 전략

```yaml
# 파이프라인 설정 예시
pipeline:
  name: "KafkaPipeline"
  supervision:
    strategy: one_for_one    # 실패한 Actor만 재시작
    max_restarts: 5          # 5분 내 최대 재시작 횟수
    within_seconds: 300      # 재시작 카운트 윈도우

  children:
    - name: "SourceSupervisor"
      type: supervisor
      supervision:
        strategy: one_for_one
        max_restarts: 10     # Source는 더 많은 재시도 허용
      children:
        - name: "KafkaSource"
          type: source
          config:
            source_type: kafka
            brokers: ["kafka1:9092", "kafka2:9092", "kafka3:9092"]
            topics: ["events"]
            group_id: "pipeline-consumer"
            # Kafka Consumer 설정
            auto_offset_reset: earliest
            enable_auto_commit: false  # 수동 커밋 (체크포인트 연동)
            session_timeout_ms: 30000
            heartbeat_interval_ms: 10000
            max_poll_interval_ms: 300000
```

---

### 전체 장애 복구 시나리오

#### 시나리오 1: Agent 프로세스 크래시

```
1. Agent 프로세스가 예기치 않게 종료
2. Control Plane이 하트비트 타임아웃 감지 (30초)
3. Agent 상태를 "offline"으로 변경
4. 다른 가용 Agent에 파이프라인 재할당
5. 새 Agent가 Redis에서 체크포인트 조회
6. 마지막 체크포인트부터 파이프라인 재시작
7. Kafka offset 복구 → 데이터 손실 없음
```

#### 시나리오 2: Redis + Kafka 동시 장애

```
1. Redis 장애 발생 → Agent가 REST 폴백 모드 전환
2. Kafka 장애 발생 → Source Actor 재시작 시도
3. Source Actor가 max_restarts 도달 → Supervisor에 escalate
4. Pipeline이 일시 중지 상태로 전환
5. 로컬 버퍼에 미전송 데이터 보존
6. Redis 복구 → 체크포인트 조회 가능
7. Kafka 복구 → 체크포인트 offset부터 재시작
8. 버퍼 데이터 + Kafka 데이터 정상 처리
```

#### 시나리오 3: Network Partition (Split-Brain)

```
1. Control Plane ↔ Agent 네트워크 분리
2. Agent는 독립적으로 파이프라인 계속 실행
3. 로컬 체크포인트 저장 (Redis 불가 시 파일)
4. 네트워크 복구 시 Control Plane과 상태 동기화
5. 중복 실행 방지를 위한 Leader Election 확인
```

---

### 모니터링 및 알림

#### Redis 메트릭

```go
type Metrics struct {
    TotalRequests        int64   // 총 요청 수
    SuccessfulRequests   int64   // 성공 요청 수
    FailedRequests       int64   // 실패 요청 수
    CacheHits            int64   // 로컬 캐시 히트
    CacheMisses          int64   // 로컬 캐시 미스
    ReconnectAttempts    int64   // 재연결 시도 횟수
    CircuitBreakerTrips  int64   // Circuit Breaker 트립 횟수
    AverageLatencyMs     float64 // 평균 지연시간
}
```

#### 권장 알림 설정

| 메트릭 | 경고 임계값 | 심각 임계값 | 설명 |
|-------|-----------|-----------|-----|
| Redis 연결 상태 | Disconnected > 30초 | Disconnected > 5분 | Redis 연결 끊김 |
| Circuit Breaker | Open 상태 진입 | 5분 이상 Open | Redis 장애 지속 |
| Kafka Consumer Lag | > 10,000 | > 100,000 | 처리 지연 |
| Actor 재시작 횟수 | > 3회/5분 | > 5회/5분 | 반복 장애 |
| 체크포인트 실패 | > 3회 연속 | > 10회 연속 | 상태 저장 실패 |

## 라이선스

Apache License 2.0
