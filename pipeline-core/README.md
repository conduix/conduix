# Pipeline Core

Actor Model 기반의 데이터 파이프라인 코어 엔진

## 개요

`pipeline-core`는 Conduix 시스템의 핵심 엔진입니다. Apache Flink에서 영감을 받은 Actor Model을 구현하여 확장 가능하고 장애에 강한 데이터 파이프라인을 제공합니다.

**하이브리드 아키텍처**: Actor Model의 장점(Supervisor, Mailbox, 계층 구조)과 [Bento](https://github.com/warpstreamlabs/bento)의 검증된 커넥터(Kafka, Elasticsearch, S3 등)를 결합했습니다.

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Pipeline Core 아키텍처                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         Config Parser                                │    │
│  │  • YAML 설정 파일 파싱                                                │    │
│  │  • Flat/Actor 구조 자동 감지                                          │    │
│  │  • 환경 변수 치환                                                     │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│                                 ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         Actor System                                 │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │    │
│  │  │  Dispatcher │  │   Mailbox   │  │  Supervisor │                  │    │
│  │  │ (스레드 풀)  │  │ (메시지 큐) │  │ (장애 복구) │                  │    │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                  │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│                                 ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         Actor Types                                  │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │    │
│  │  │  Source   │  │ Transform │  │   Sink    │  │  Router   │        │    │
│  │  │  Actor    │  │   Actor   │  │   Actor   │  │   Actor   │        │    │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 디렉토리 구조

```
pipeline-core/
├── cmd/
│   └── pipeline/
│       └── main.go              # CLI 진입점
├── pkg/
│   ├── actor/                   # Actor 시스템
│   │   ├── actor.go             # Actor 인터페이스 및 기본 구현
│   │   ├── mailbox.go           # 메시지 큐 (Backpressure 지원)
│   │   ├── supervisor.go        # Supervisor 패턴 구현
│   │   ├── system.go            # Actor System 관리
│   │   ├── factory.go           # Actor 팩토리
│   │   └── types/               # Actor 타입별 구현
│   │       ├── source.go        # Source Actor (레거시)
│   │       ├── transform.go     # Transform Actor (레거시)
│   │       ├── sink.go          # Sink Actor (레거시)
│   │       ├── router.go        # Router Actor (조건부 라우팅)
│   │       ├── bento_source.go  # Bento 기반 Source Actor
│   │       ├── bento_transform.go # Bento 기반 Transform Actor
│   │       ├── bento_sink.go    # Bento 기반 Sink Actor
│   │       └── init.go          # Actor 타입 등록
│   ├── adapter/
│   │   └── bento/               # Bento 어댑터 레이어
│   │       ├── adapter.go       # Input/Output/Processor 어댑터
│   │       └── config.go        # 설정 변환 빌더
│   ├── config/
│   │   └── config.go            # YAML 설정 파서
│   └── pipeline/
│       └── runner.go            # 파이프라인 실행기
├── configs/                     # 샘플 설정 파일
│   ├── sample-flat.yaml         # Flat 구조 예시
│   └── sample-actor.yaml        # 계층적 Actor 구조 예시
└── go.mod
```

## 핵심 컴포넌트

### 1. Actor System

Actor Model의 핵심 구현체로, 동시성과 장애 복구를 담당합니다.

```go
// Actor 인터페이스
type Actor interface {
    Receive(ctx ActorContext, msg Message) error
    PreStart(ctx ActorContext) error
    PostStop(ctx ActorContext) error
    PreRestart(ctx ActorContext, reason error) error
    PostRestart(ctx ActorContext) error
}

// ActorRef - Actor 참조
type ActorRef struct {
    ID      string
    Path    string
    Mailbox *Mailbox
}
```

#### Dispatcher

스레드 풀을 관리하여 Actor에 실행 컨텍스트를 제공합니다.

```go
type DispatcherConfig struct {
    Type        string // "fork-join", "thread-pool"
    Parallelism int    // 병렬 처리 수준
}
```

#### Mailbox

Actor 간 비동기 메시지 전달을 위한 큐입니다.

```go
type MailboxConfig struct {
    Capacity         int    // 큐 크기
    OverflowStrategy string // "backpressure", "drop_oldest", "drop_newest"
}
```

### 2. Supervisor

계층적 장애 복구 전략을 구현합니다.

```go
type SupervisionStrategy string

const (
    OneForOne  SupervisionStrategy = "one_for_one"  // 실패한 Actor만 재시작
    OneForAll  SupervisionStrategy = "one_for_all"  // 모든 자식 재시작
    RestForOne SupervisionStrategy = "rest_for_one" // 실패한 Actor 이후 모두 재시작
)

type SupervisionConfig struct {
    Strategy      SupervisionStrategy
    MaxRestarts   int           // 최대 재시작 횟수
    WithinSeconds int           // 재시작 카운트 윈도우 (초)
}
```

### 3. Source Actors

데이터 소스에서 이벤트를 수집합니다.

| 타입 | 설명 | 주요 설정 |
|-----|------|----------|
| `kafka` | Kafka Consumer | brokers, topics, group_id |
| `http_server` | HTTP 수신 서버 | address, path, method |
| `file` | 파일 모니터링 | include, exclude, read_from |
| `demo` | 테스트용 생성기 | interval, format, count |

### 4. Transform Actors

데이터를 변환하고 가공합니다.

| 타입 | 설명 | 주요 설정 |
|-----|------|----------|
| `remap` | VRL 기반 변환 | source (VRL 스크립트) |
| `filter` | 조건부 필터링 | condition |
| `aggregate` | 윈도우 집계 | window, group_by, aggregations |
| `sample` | 샘플링 | rate |

### 5. Sink Actors

처리된 데이터를 외부 시스템으로 전송합니다.

| 타입 | 설명 | 주요 설정 |
|-----|------|----------|
| `console` | 표준 출력 | encoding, pretty |
| `elasticsearch` | Elasticsearch | endpoints, index |
| `kafka` | Kafka Producer | brokers, topic |
| `s3` | AWS S3 | bucket, prefix, region |
| `file` | 파일 출력 | path, encoding |

### 6. Router Actor

조건에 따라 메시지를 다른 Actor로 라우팅합니다.

```go
type RoutingRule struct {
    Condition string // VRL 조건식
    Output    string // 대상 Actor 이름
}
```

## 설정 형식

### Flat 구조 (Vector 호환)

```yaml
version: "1.0"
name: "my-pipeline"
type: flat  # 기본값

sources:
  input:
    type: kafka
    brokers: ["localhost:9092"]
    topics: ["events"]

transforms:
  parse:
    type: remap
    inputs: ["input"]
    source: '. = parse_json!(.message)'

sinks:
  output:
    type: console
    inputs: ["parse"]
```

### 계층적 Actor 구조

```yaml
version: "1.0"
name: "my-pipeline"
type: actor

actor_system:
  dispatcher:
    parallelism: 4
  mailbox:
    capacity: 10000
    overflow_strategy: backpressure

pipeline:
  name: "RootSupervisor"
  supervision:
    strategy: one_for_one
    max_restarts: 5

  children:
    - name: "Source"
      type: source
      config:
        source_type: kafka
        brokers: ["localhost:9092"]
      outputs: ["Transform"]

    - name: "Transform"
      type: transform
      parallelism: 2
      config:
        transform_type: remap
        source: '. = parse_json!(.message)'
      outputs: ["Sink"]

    - name: "Sink"
      type: sink
      config:
        sink_type: console
```

## 빌드 및 실행

### 빌드

```bash
# 단독 빌드
cd pipeline-core
go build -o pipeline ./cmd/pipeline

# 전체 프로젝트에서 빌드
cd conduix
make build-core
```

### 실행

```bash
# 기본 실행
./pipeline --config config.yaml

# 옵션
./pipeline --config config.yaml \
  --log-level debug \
  --metrics-port 9090 \
  --checkpoint-dir ./checkpoints
```

### 테스트

```bash
# 단위 테스트
go test ./...

# 통합 테스트
go test -tags=integration ./...

# 벤치마크
go test -bench=. ./pkg/actor/...
```

## API

### Pipeline Runner

```go
// Runner 생성
runner, err := pipeline.NewRunner(config)

// 파이프라인 시작
err := runner.Start()

// 파이프라인 일시중지
err := runner.Pause()

// 파이프라인 재개
err := runner.Resume()

// 파이프라인 중지
err := runner.Stop()

// 상태 조회
status := runner.GetStatus()
```

### Config Parser

```go
// 파일에서 설정 로드
config, err := config.LoadFromFile("config.yaml")

// 문자열에서 설정 로드
config, err := config.LoadFromString(yamlString)

// 설정 검증
err := config.Validate()
```

## Bento 통합

### 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                     하이브리드 아키텍처                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  사용자 YAML 설정                                                │
│       │                                                          │
│       ▼                                                          │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                 Config Parser                            │    │
│  │  (기존 형식 그대로 사용 가능)                             │    │
│  └──────────────────────────┬──────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Actor System (유지)                         │    │
│  │  Supervisor + Mailbox + Dispatcher                       │    │
│  └──────────────────────────┬──────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Bento Adapter Layer                         │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐            │    │
│  │  │InputAdapt │  │ProcessAda │  │OutputAdap │            │    │
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘            │    │
│  └────────┼──────────────┼──────────────┼──────────────────┘    │
│           ▼              ▼              ▼                        │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Bento Connectors                            │    │
│  │  Kafka, ES, S3, HTTP, File, NATS, AMQP ...              │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Bento 커넥터 지원 목록

**Sources (입력):**
| 타입 | Bento 커넥터 | 설명 |
|-----|-------------|------|
| `kafka` | kafka | Kafka Consumer |
| `http_server` | http_server | HTTP 엔드포인트 |
| `file` | file | 파일 읽기 |
| `generate` | generate | 테스트 데이터 생성 |
| `stdin` | stdin | 표준 입력 |

**Transforms (변환):**
| 타입 | Bento 프로세서 | 설명 |
|-----|---------------|------|
| `remap` | bloblang | Bloblang 변환 (VRL 유사) |
| `filter` | 내장 | 조건부 필터링 |
| `json_parse` | mapping | JSON 파싱 |
| `compress` | compress | 압축 |
| `decompress` | decompress | 압축 해제 |

**Sinks (출력):**
| 타입 | Bento 커넥터 | 설명 |
|-----|-------------|------|
| `kafka` | kafka | Kafka Producer |
| `elasticsearch` | elasticsearch | Elasticsearch |
| `aws_s3` | aws_s3 | AWS S3 |
| `http_client` | http_client | HTTP 클라이언트 |
| `file` | file | 파일 쓰기 |
| `stdout` | stdout | 표준 출력 |

### Bento 모드 전환

```go
import "github.com/conduix/pipeline-core/pkg/actor/types"

// Bento 사용 (기본값)
types.SetUseBento(true)

// 레거시 모드 (Bento 없이 내장 구현 사용)
types.SetUseBento(false)
```

### Bloblang 변환 예시

```yaml
transforms:
  parse:
    type: remap
    inputs: ["source"]
    source: |
      # VRL 스타일 (자동 변환)
      . = parse_json!(.message)
      .processed_at = now()

      # 또는 Bloblang 직접 사용
      root = this.message.parse_json()
      root.processed_at = now()
```

## 의존성

```go
require (
    github.com/conduix/shared v0.0.0
    github.com/warpstreamlabs/bento v1.3.0   // MIT 라이선스
    gopkg.in/yaml.v3 v3.0.1
    github.com/google/uuid v1.5.0
    github.com/redis/go-redis/v9 v9.3.0
)
```

## 확장

### 커스텀 Source 추가

```go
// 1. SourceActor 인터페이스 구현
type MySource struct {
    *actor.BaseActor
    config MySourceConfig
}

func (s *MySource) Receive(ctx actor.ActorContext, msg actor.Message) error {
    // 데이터 수집 로직
    data := s.collectData()

    // 다음 Actor로 전송
    for _, output := range s.outputs {
        output.Send(actor.Message{Payload: data})
    }
    return nil
}

// 2. Factory에 등록
factory.RegisterSource("my_source", func(cfg map[string]any) actor.Actor {
    return NewMySource(cfg)
})
```

### 커스텀 Transform 추가

```go
type MyTransform struct {
    *actor.BaseActor
}

func (t *MyTransform) Receive(ctx actor.ActorContext, msg actor.Message) error {
    // 변환 로직
    transformed := t.transform(msg.Payload)

    // 다음 Actor로 전송
    return t.Forward(ctx, actor.Message{Payload: transformed})
}
```

## 성능 튜닝

### Dispatcher 설정

```yaml
actor_system:
  dispatcher:
    type: fork-join
    parallelism: 8  # CPU 코어 수에 맞게 조정
```

### Mailbox 설정

```yaml
actor_system:
  mailbox:
    capacity: 50000           # 대용량 처리 시 증가
    overflow_strategy: drop_oldest  # 메모리 제한 시
```

### 병렬 처리

```yaml
children:
  - name: "Parser"
    type: transform
    parallelism: 4  # 워커 수 조정
```

## 관련 문서

- [Standalone 실행 가이드](../docs/standalone-usage.md)
- [장애 처리](../README.md#장애-처리-fault-tolerance)
