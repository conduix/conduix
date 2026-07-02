# 기술 설계 검토: 데이터 파이프라인 엔진 선택

## 1. 현재 상태 분석

### 1.1 실제 구현 현황

**중요 발견**: 현재 코드는 "Conduix"이라는 이름만 사용하고, 실제로 Vector를 사용하지 않습니다.

```
현재 구현:
┌─────────────────────────────────────────────────────────────────┐
│                    순수 Go Actor Model 구현                      │
├─────────────────────────────────────────────────────────────────┤
│  ✅ Actor System (system.go)       - 완성                       │
│  ✅ Mailbox (mailbox.go)           - 완성 (backpressure 지원)   │
│  ✅ Supervisor (supervisor.go)     - 완성 (3가지 전략)          │
│  ✅ Dispatcher (system.go)         - 완성 (work-stealing)       │
│  ⚠️  Source Actors                 - 기본 구조만 (TODO 다수)     │
│  ⚠️  Transform Actors              - 부분 구현 (remap, filter)  │
│  ⚠️  Sink Actors                   - 기본 구조만 (TODO 다수)     │
│  ❌ Kafka 클라이언트               - 미구현 (스텁만 존재)        │
│  ❌ Elasticsearch 클라이언트       - 미구현 (스텁만 존재)        │
│  ❌ S3 클라이언트                  - 미구현 (스텁만 존재)        │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 구현된 기능 상세

| 컴포넌트 | 상태 | 설명 |
|---------|------|------|
| Actor 코어 | ✅ 완성 | Receive, PreStart, PostStop, 메시지 전달 |
| Mailbox | ✅ 완성 | Backpressure, DropOldest, DropNewest |
| Supervisor | ✅ 완성 | OneForOne, OneForAll, RestForOne |
| Config Parser | ✅ 완성 | Flat/Actor 모드 YAML 파싱 |
| Source: Kafka | ❌ 스텁 | TODO: 실제 Kafka 클라이언트 필요 |
| Source: HTTP | ❌ 스텁 | TODO: 실제 HTTP 서버 필요 |
| Transform: Remap | ⚠️ 기본 | JSON 파싱만, VRL 미지원 |
| Transform: Filter | ⚠️ 기본 | 단순 조건만 (.field == "value") |
| Sink: ES | ❌ 스텁 | TODO: 실제 HTTP 클라이언트 필요 |
| Sink: S3 | ❌ 스텁 | TODO: AWS SDK 필요 |

---

## 2. 기술 옵션 비교 분석

### 2.1 Option A: Vector (Rust) 바이너리 사용

```
┌─────────────────────────────────────────────────────────────────┐
│                   Vector 바이너리 래핑 방식                       │
├─────────────────────────────────────────────────────────────────┤
│  Go Agent                                                        │
│  └── Vector Binary (subprocess)                                  │
│      └── YAML Config → Vector 내부 파이프라인 실행               │
└─────────────────────────────────────────────────────────────────┘
```

**장점:**
- 검증된 고성능 (Rust, 수백만 이벤트/초)
- 풍부한 커넥터 (300+ sources/sinks)
- VRL (Vector Remap Language) 내장
- Datadog이 유지보수 (안정성)
- 즉시 프로덕션 가능

**단점:**
- 외부 바이너리 의존성 (배포 복잡성)
- Go-Rust 프로세스 간 통신 오버헤드
- 세밀한 제어 어려움 (Actor 레벨 커스터마이징 불가)
- Vector 설정 문법에 종속
- 라이선스: MPL-2.0

**성능 참고:**
- Vector 공식: "10x faster than alternatives"
- 실제 벤치마크: 초당 수십만~수백만 이벤트

### 2.2 Option B: Benthos/Bento (Go) 라이브러리 사용

```
┌─────────────────────────────────────────────────────────────────┐
│                   Bento 라이브러리 통합 방식                      │
├─────────────────────────────────────────────────────────────────┤
│  Go Agent                                                        │
│  └── import "github.com/warpstreamlabs/bento"                   │
│      └── Bento Stream Builder API                               │
│          └── 프로그래매틱 파이프라인 구성                        │
└─────────────────────────────────────────────────────────────────┘
```

**장점:**
- 순수 Go (동일 런타임, 쉬운 통합)
- MIT 라이선스 (Bento 포크)
- 풍부한 커넥터 (Kafka, ES, S3, HTTP 등)
- Bloblang (VRL과 유사한 변환 언어)
- 트랜잭션 기반 at-least-once 보장
- 활발한 커뮤니티

**단점:**
- Vector보다 성능 낮음 (Go vs Rust)
- Actor Model이 아닌 Pipeline 모델
- Supervisor 패턴 직접 구현 필요
- 2024년 Redpanda 인수 후 포크됨 (불안정기)

**성능 참고:**
- Go 기반: 초당 수십만 이벤트
- Sub-millisecond latency

### 2.3 Option C: 커스텀 Actor Model 완성 (현재 방식)

```
┌─────────────────────────────────────────────────────────────────┐
│                   커스텀 Actor Model 방식                        │
├─────────────────────────────────────────────────────────────────┤
│  Go Agent                                                        │
│  └── Custom Actor System                                        │
│      ├── Source Actors (Kafka, HTTP, File)                      │
│      ├── Transform Actors (Remap, Filter, Aggregate)            │
│      └── Sink Actors (ES, S3, Kafka)                            │
└─────────────────────────────────────────────────────────────────┘
```

**장점:**
- 완전한 제어 (Flink 스타일 Actor 모델)
- 외부 의존성 없음
- 커스텀 Supervision 전략
- 세밀한 체크포인트/복구 제어
- 순수 Go (배포 단순)

**단점:**
- 개발 비용 높음 (커넥터 직접 구현)
- 검증되지 않은 코드 (버그 리스크)
- 성능 최적화 직접 수행 필요
- 유지보수 부담

**예상 추가 개발 항목:**
1. Kafka 클라이언트 통합 (franz-go 또는 sarama)
2. Elasticsearch 클라이언트 통합 (olivere/elastic)
3. S3 클라이언트 통합 (aws-sdk-go)
4. VRL 유사 변환 언어 구현 또는 통합
5. Window/Aggregate 완성
6. 성능 테스트 및 최적화

### 2.4 Option D: 하이브리드 (Actor + Bento 커넥터)

```
┌─────────────────────────────────────────────────────────────────┐
│                   하이브리드 방식 (권장)                          │
├─────────────────────────────────────────────────────────────────┤
│  Go Agent                                                        │
│  └── Custom Actor System (Supervisor, Mailbox 유지)             │
│      ├── Source Actors                                          │
│      │   └── Bento Input 어댑터 (Kafka, HTTP 등)                │
│      ├── Transform Actors                                       │
│      │   └── Bento Processor 어댑터 (Bloblang 등)               │
│      └── Sink Actors                                            │
│          └── Bento Output 어댑터 (ES, S3 등)                    │
└─────────────────────────────────────────────────────────────────┘
```

**장점:**
- Actor Model 장점 유지 (Supervision, 계층 구조)
- Bento의 검증된 커넥터 재사용
- 순수 Go (단일 바이너리)
- 개발 비용 절감
- 확장성 유지

**단점:**
- 통합 레이어 개발 필요
- Bento API 변경에 영향 받음
- 두 시스템 이해 필요

---

## 3. 기술 비교 매트릭스

| 기준 | Vector | Bento | 커스텀 | 하이브리드 |
|-----|--------|-------|-------|-----------|
| **성능** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **개발 비용** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ |
| **커넥터 수** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐ | ⭐⭐⭐⭐ |
| **제어 수준** | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **배포 단순성** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Actor Model** | ❌ | ❌ | ✅ | ✅ |
| **Supervision** | ❌ | ❌ | ✅ | ✅ |
| **라이선스** | MPL-2.0 | MIT | MIT | MIT |

---

## 4. 권장 사항

### 4.1 최종 권장: Option D (하이브리드)

**이유:**
1. **Actor Model 유지**: 현재 구현된 Supervisor, Mailbox, Actor 시스템은 잘 설계됨
2. **커넥터 재사용**: 커넥터 직접 구현은 비효율적, Bento의 검증된 코드 활용
3. **순수 Go**: 단일 바이너리 배포, Vector 외부 프로세스 관리 불필요
4. **MIT 라이선스**: 상업적 사용 자유
5. **확장성**: 필요시 커스텀 커넥터 추가 가능

### 4.2 구현 전략

```
Phase 1: Bento 어댑터 레이어 구현
├── pkg/adapter/bento/
│   ├── source.go      # Bento Input → Actor Message 변환
│   ├── transform.go   # Bento Processor → Actor Transform 통합
│   └── sink.go        # Actor Message → Bento Output 변환

Phase 2: 기존 Actor 타입 리팩토링
├── pkg/actor/types/
│   ├── source.go      # BentoSourceActor 추가
│   ├── transform.go   # BentoTransformActor 추가
│   └── sink.go        # BentoSinkActor 추가

Phase 3: 설정 통합
└── pkg/config/
    └── bento.go       # YAML → Bento Config 변환
```

### 4.3 대안 시나리오

**성능이 최우선인 경우 (Option A: Vector):**
- 초당 100만 이벤트 이상 필요
- 커넥터 다양성이 매우 중요
- Actor Model보다 처리량이 중요

**빠른 출시가 필요한 경우 (Option B: Bento 직접 사용):**
- Actor Model 포기 가능
- 최소 기능으로 빠른 출시 필요
- 커스터마이징 요구 적음

---

## 5. 세부 구현 계획

### 5.1 Bento 어댑터 구현 예시

```go
// pkg/adapter/bento/source.go
package bento

import (
    "github.com/warpstreamlabs/bento/public/service"
    "github.com/conduix/pipeline-core/pkg/actor"
)

// BentoSourceAdapter wraps Bento input as Actor message source
type BentoSourceAdapter struct {
    input   *service.Input
    outputs []actor.ActorRef
}

func (a *BentoSourceAdapter) Start(ctx context.Context) error {
    go func() {
        for {
            msg, ack, err := a.input.Read(ctx)
            if err != nil {
                return
            }
            // Convert Bento message to Actor message
            actorMsg := actor.Message{
                Type:    actor.MessageTypeData,
                Payload: msg.AsBytes(),
            }
            for _, out := range a.outputs {
                out.Tell(actorMsg)
            }
            ack(ctx, nil)
        }
    }()
    return nil
}
```

### 5.2 설정 예시 (하이브리드)

```yaml
version: "1.0"
name: "hybrid-pipeline"
type: actor

pipeline:
  name: "RootSupervisor"
  type: supervisor
  supervision:
    strategy: one_for_one
    max_restarts: 5

  children:
    - name: "KafkaSource"
      type: bento_source  # Bento 어댑터 사용
      config:
        # Bento input 설정
        kafka:
          addresses: ["kafka:9092"]
          topics: ["events"]
          consumer_group: "pipeline"
      outputs: ["TransformSupervisor"]

    - name: "TransformSupervisor"
      type: supervisor
      children:
        - name: "ParseAndFilter"
          type: bento_processor  # Bento processor 사용
          parallelism: 4
          config:
            # Bloblang 변환
            bloblang: |
              root = this.parse_json()
              root.processed_at = now()
          outputs: ["SinkSupervisor"]

    - name: "SinkSupervisor"
      type: supervisor
      children:
        - name: "ElasticsearchSink"
          type: bento_sink  # Bento output 사용
          config:
            elasticsearch:
              urls: ["http://es:9200"]
              index: "events-${!timestamp_unix()}"
```

---

## 6. 결론

| 현재 상태 | 문제점 | 권장 해결책 |
|----------|--------|------------|
| Actor 코어 완성 | 유지 가치 높음 | 그대로 유지 |
| 커넥터 미구현 | 개발 비용 높음 | Bento 어댑터로 대체 |
| "Vector" 네이밍 | 실제 Vector 미사용 | 네이밍 정리 필요 |

**최종 결정:**
- **Actor System**: 유지 (잘 설계됨)
- **커넥터**: Bento 어댑터로 대체
- **프로젝트명**: "Pipeline" 또는 "DataFlow"로 변경 고려

---

## 참고 자료

- [Vector.dev](https://vector.dev/) - Rust 기반 고성능 파이프라인
- [Bento (Benthos Fork)](https://www.warpstream.com/blog/announcing-bento-the-open-source-fork-of-the-project-formerly-known-as-benthos) - Go 기반 MIT 라이선스
- [Arroyo](https://www.arroyo.dev/blog/how-arroyo-beats-flink-at-sliding-windows/) - Rust 기반 Flink 대안
- [RisingWave](https://risingwave.com/blog/top-7-apache-flink-alternatives-a-deep-dive/) - Rust 기반 스트리밍 DB
