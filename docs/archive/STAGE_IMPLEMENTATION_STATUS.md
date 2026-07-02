# Stage 구현 현황 문서

> 최종 업데이트: 2026-02-11

이 문서는 Conduix 프로젝트의 Pipeline Stage 구현 현황을 정리한 것입니다.

## 목차

- [개요](#개요)
- [구현 현황 요약](#구현-현황-요약)
- [상세 분석](#상세-분석)
  - [완전 미구현](#-완전-미구현)
  - [부분 구현](#-부분-구현-시뮬레이션만)
  - [완전 구현](#-완전-구현)
- [구현 우선순위](#구현-우선순위)
- [구현 가이드](#구현-가이드)

---

## 개요

Conduix의 Pipeline Stage는 **두 가지 레이어**로 구성되어 있습니다:

| 레이어 | 위치 | 용도 |
|--------|------|------|
| **Stream 레이어** | `pipeline-core/pkg/stream/` | 로컬 파이프라인 처리 (직접 함수 호출) |
| **Executor 레이어** | `pipeline-core/pkg/executor/` | 워크플로우 실행 및 Stage 적용 |

### Stage 인터페이스

```go
// pipeline-core/pkg/stream/types.go:28-42
type Stage interface {
    Name() string
    Type() string
    Process(ctx context.Context, record *Record) (*Record, error)
    Close() error
}
```

- 레코드 필터링: `Process()`에서 `nil` 반환
- 직접 함수 호출 방식 (Actor 메시지 패싱 없음)

---

## 구현 현황 요약

| 상태 | Stage/Component | 비고 |
|:----:|-----------------|------|
| ❌ | AggregateStage | 윈도우 집계 로직 없음 |
| ❌ | TriggerStage | 코드에 정의 없음 |
| ❌ | RouterStage | 코드에 정의 없음 |
| ❌ | HTTPSource | 빈 구현 |
| ⚠️ | FilterStage | 하드코딩 조건만 지원 |
| ⚠️ | KafkaSource/Sink | 시뮬레이션만 |
| ⚠️ | FileSource/Sink | 시뮬레이션만 |
| ⚠️ | ElasticsearchSink | 로그만 출력 |
| ⚠️ | S3Sink | 로그만 출력 |
| ✅ | PassthroughStage | 완전 구현 |
| ✅ | RemapStage | 완전 구현 |
| ✅ | SampleStage | 완전 구현 |
| ✅ | EnrichStage | 완전 구현 |
| ✅ | ValidationStage | 완전 구현 |
| ✅ | ConsoleSink | 완전 구현 |

---

## 상세 분석

### ❌ 완전 미구현

#### 1. AggregateStage

- **파일**: `pipeline-core/pkg/stream/stage.go:301-307`
- **현재 상태**: 레코드를 그대로 통과시킴

```go
// 현재 구현 (미완료)
func (s *AggregateStage) Process(ctx context.Context, record *Record) (*Record, error) {
    // TODO: Implement actual aggregation logic
    return record, nil
}
```

- **필요한 구현**:
  - 시간 윈도우 관리 (`windowStart`, `windowDuration`)
  - 버킷 기반 집계 (`buckets` 필드 활용)
  - 윈도우 만료 시 집계 결과 방출
  - 지원해야 할 집계 함수: `count`, `sum`, `avg`, `min`, `max`

#### 2. TriggerStage

- **파일**: 없음 (미정의)
- **설계 의도** (CLAUDE.md 기반): 자식 파이프라인 트리거
- **필요한 구현**:
  - 조건 기반 트리거 로직
  - 자식 파이프라인 호출 메커니즘
  - 부모-자식 간 데이터 전달

#### 3. RouterStage

- **파일**: 없음 (미정의)
- **설계 의도** (CLAUDE.md 기반): in-pipeline branching
- **지원해야 할 모드**:
  - `fan_out`: 모든 라우트로 복사
  - `condition`: 첫 번째 매칭 라우트
  - `filter`: 모든 매칭 라우트

#### 4. HTTPSource

- **파일**: `pipeline-core/pkg/stream/source.go:333-340`
- **현재 상태**: context 종료 대기만 함

```go
// 현재 구현 (미완료)
func (s *HTTPSource) Start(ctx context.Context, output chan<- *Record) error {
    // TODO: Implement actual HTTP server
    <-ctx.Done()
    return nil
}
```

- **필요한 구현**:
  - HTTP 서버 시작 (`net/http`)
  - `/events` 엔드포인트 구현
  - 요청 → Record 변환

---

### ⚠️ 부분 구현 (시뮬레이션만)

#### 1. FilterStage

- **파일**: `pipeline-core/pkg/stream/stage.go:79-108`
- **문제점**: 하드코딩된 3가지 조건만 지원

```go
// 현재 지원 조건 (buildEvaluator 함수)
switch s.condition {
case `.level == "error"`:
    // ...
case `.level == "warn"`:
    // ...
case `.level != "debug"`:
    // ...
}
```

- **필요한 구현**:
  - 동적 조건 평가 엔진 (Bloblang/VRL 스타일)
  - 비교 연산자: `==`, `!=`, `>`, `<`, `>=`, `<=`
  - 논리 연산자: `&&`, `||`, `!`
  - 필드 접근: `.field.nested.value`

#### 2. KafkaSource

- **파일**: `pipeline-core/pkg/stream/source.go:178-223`
- **현재 상태**: 시뮬레이션 데이터 생성

```go
// TODO: Implement actual Kafka consumer
// 현재는 데모 데이터 생성
```

- **필요한 구현**:
  - `github.com/segmentio/kafka-go` 또는 `confluent-kafka-go` 연동
  - Consumer Group 관리
  - Offset 커밋/체크포인트

#### 3. KafkaSink

- **파일**: `pipeline-core/pkg/stream/sink.go:355-359`
- **현재 상태**: 로그만 출력

```go
// TODO: Implement actual Kafka producer
s.logger.Info("Would send to Kafka", ...)
```

- **필요한 구현**:
  - Kafka Producer 초기화
  - 배치 전송
  - 에러 핸들링 및 재시도

#### 4. FileSource / FileSink

- **파일**: `source.go:253-297`, `sink.go:392-396`
- **필요한 구현**:
  - `os.Open()`, `bufio.Scanner` (Source)
  - `os.Create()`, `bufio.Writer` (Sink)
  - 파일 로테이션 (Sink)

#### 5. ElasticsearchSink

- **파일**: `pipeline-core/pkg/stream/sink.go:240-262`
- **현재 상태**: 로그만 출력

```go
// TODO: Send to Elasticsearch
s.logger.Info("Would send to Elasticsearch", ...)
```

- **필요한 구현**:
  - Bulk API 호출 (`/_bulk` 엔드포인트)
  - 인덱스 템플릿 관리
  - 재시도 로직

#### 6. S3Sink

- **파일**: `pipeline-core/pkg/stream/sink.go:308-312`
- **필요한 구현**:
  - AWS SDK 연동 (`github.com/aws/aws-sdk-go-v2`)
  - Multipart Upload
  - 파티셔닝 전략

---

### ✅ 완전 구현

| Stage | 파일 | 기능 |
|-------|------|------|
| **PassthroughStage** | `stage.go` | 레코드 통과, 입출력 통계 기록 |
| **RemapStage** | `stage.go` | JSON 파싱, 필드 리매핑 |
| **SampleStage** | `stage.go` | 확률 기반 샘플링 (`rate` 설정) |
| **EnrichStage** | `stage.go` | 정적 필드 추가, lookup_table 메타데이터 |
| **ValidationStage** | `stage.go` | JSON Schema 검증, 실패 시 드롭/에러 |
| **ConsoleSink** | `sink.go` | stdout에 JSON 출력 |
| **ValidatingSink** | `sink.go` | 래퍼 Sink - 검증 후 전달 |

---

## 구현 우선순위

### P0 - Critical (프로덕션 필수)

| 항목 | 영향도 | 예상 노력 | 담당 |
|------|--------|----------|------|
| AggregateStage 윈도우 집계 | 높음 | 중간 | - |
| Kafka 실제 연동 (Consumer/Producer) | 높음 | 높음 | - |

### P1 - High (핵심 기능)

| 항목 | 영향도 | 예상 노력 | 담당 |
|------|--------|----------|------|
| FilterStage 동적 조건 평가 | 높음 | 높음 | - |
| TriggerStage 구현 | 중간 | 중간 | - |
| RouterStage 구현 | 중간 | 중간 | - |

### P2 - Medium (외부 시스템 연동)

| 항목 | 영향도 | 예상 노력 | 담당 |
|------|--------|----------|------|
| File I/O 실제 구현 | 중간 | 낮음 | - |
| Elasticsearch 실제 통합 | 중간 | 중간 | - |
| S3 실제 통합 | 중간 | 중간 | - |
| HTTPSource 구현 | 낮음 | 중간 | - |

---

## 구현 가이드

### AggregateStage 구현 예시

```go
type AggregateStage struct {
    name           string
    windowDuration time.Duration
    aggregateFunc  string // "count", "sum", "avg", "min", "max"
    groupBy        []string

    // 내부 상태
    windowStart    time.Time
    buckets        map[string]*aggregateBucket
    mu             sync.Mutex
}

type aggregateBucket struct {
    count int64
    sum   float64
    min   float64
    max   float64
}

func (s *AggregateStage) Process(ctx context.Context, record *Record) (*Record, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := time.Now()

    // 윈도우 만료 체크
    if now.Sub(s.windowStart) >= s.windowDuration {
        result := s.flushWindow()
        s.windowStart = now
        s.buckets = make(map[string]*aggregateBucket)
        return result, nil
    }

    // 버킷에 레코드 추가
    key := s.buildGroupKey(record)
    bucket := s.getOrCreateBucket(key)
    s.updateBucket(bucket, record)

    return nil, nil // 윈도우 내에서는 레코드 방출 안함
}
```

### FilterStage 동적 조건 평가 예시

```go
// 간단한 조건 파서 구현
type Condition struct {
    Field    string
    Operator string // "==", "!=", ">", "<", ">=", "<="
    Value    interface{}
}

func (s *FilterStage) buildEvaluator() func(map[string]interface{}) bool {
    cond := parseCondition(s.condition) // ".level == \"error\""

    return func(data map[string]interface{}) bool {
        fieldValue := getNestedField(data, cond.Field)
        return compare(fieldValue, cond.Operator, cond.Value)
    }
}
```

---

## 관련 파일 목록

```
pipeline-core/pkg/stream/
├── stage.go          # Stage 구현체들
├── source.go         # Source 구현체들
├── sink.go           # Sink 구현체들
├── types.go          # Stage 인터페이스 정의
├── pipeline.go       # Pipeline 실행 로직
└── pipeline_actor.go # Actor 기반 파이프라인

pipeline-core/pkg/executor/
└── group_executor.go # Executor 레이어 Stage 적용
```

---

## 변경 이력

| 날짜 | 내용 | 작성자 |
|------|------|--------|
| 2026-02-11 | 초기 문서 작성 | Claude Code |
