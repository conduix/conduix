# 파이프라인 연결 가이드

파이프라인을 여러 개 엮는 방법이 세 가지 있다. **이름은 모두 "의존"처럼 들리지만 목적이 다르다.**

> 이 문서가 생긴 이유: 실제로 `depends_on` 을 걸었는데 두 파이프라인이 0.1초 차로
> 동시에 실행됐다. `execution_mode` 를 `dag` 로 바꾸지 않았기 때문이다.
> 아래 표를 먼저 보면 그런 실수를 피할 수 있다.

---

## 30초 요약: 무엇을 쓸 것인가

| 하고 싶은 것 | 쓸 것 | 필수 설정 |
|---|---|---|
| **A 가 끝나야 B 시작** (같은 테이블을 쓰는 등) | `depends_on` | `execution_mode: dag` ⚠️ |
| 단순히 **하나씩 차례로** 실행 | `priority` | `execution_mode: sequential` |
| **A 의 출력 레코드를** B 가 받아서 처리 | pipeline link | 링크 생성 API + Kafka |
| 서로 무관, 동시에 빨리 | (아무것도) | `execution_mode: parallel` (기본) |

**가장 흔한 실수**: `depends_on` 만 쓰고 `execution_mode` 를 그대로 둔 것.
기본값이 `parallel` 이라 `depends_on` 이 **조용히 무시된다** — 에러도 로그도 없다.

---

## 1. `depends_on` — 실행 순서 (DAG)

"A 가 완료돼야 B 를 시작한다."

### 언제

- 두 파이프라인이 **같은 테이블**을 쓴다 (수집 → 보강)
- B 가 A 의 **결과가 DB 에 있어야** 동작한다
- 순서가 뒤집히면 데이터가 어긋난다

### 설정

```yaml
execution_mode: dag        # ← 이게 없으면 depends_on 이 무시된다

pipelines:
  - id: restroom-api-collect        # 수집
    priority: 0
    input: { type: rest_api, ... }

  - id: restroom-csv-enrich         # 보강
    priority: 1
    depends_on: [restroom-api-collect]
    input: { type: rest_api, ... }
```

### 동작

`GroupExecutor.runDAG()` 가 의존 그래프를 만들고, 의존 대상이 **모두 완료된**
파이프라인만 실행한다. 의존이 없는 것들끼리는 여전히 병렬로 돈다.

### 확인 방법

`runDAG` 는 전용 로그를 남기지 않는다. **파이프라인 시작 시각 간격**으로 판별한다.

```bash
kubectl logs -n conduix <job-pod> | grep "creating input source"
```

```
07:59:14.858  restroom-api-collect     ← 먼저
08:02:00.447  restroom-csv-enrich      ← 2분 46초 뒤 = 의존 지켜짐 ✅
```

간격이 **0.1초 이내**면 `parallel` 로 돌고 있다는 뜻이다 — `execution_mode` 를 확인하라.

---

## 2. `priority` — 단순 순차 실행

"낮은 번호부터 하나씩."

### 언제

- 그냥 순서대로 돌리고 싶다
- 의존 관계를 일일이 적기엔 번거롭다
- 리소스를 아끼려고 동시 실행을 피한다

### 설정

```yaml
execution_mode: sequential   # ← 이게 없으면 priority 가 무시된다

pipelines:
  - id: first
    priority: 0
  - id: second
    priority: 1
```

### `depends_on` 과의 차이

| | `sequential` + `priority` | `dag` + `depends_on` |
|---|---|---|
| 순서 근거 | 번호 순 | 의존 관계 |
| 병렬 실행 | 없음 (항상 1개씩) | 의존 없는 것끼리는 병렬 |
| 앞이 실패하면 | 설정에 따름 | 뒤가 시작되지 않음 |
| 적합한 상황 | 단순 줄 세우기 | 실제 데이터 의존 |

파이프라인이 5개인데 그중 2개만 순서가 중요하다면 `dag` 가 낫다 — 나머지 3개는
기다릴 이유가 없다.

---

## 3. pipeline link — 데이터 전달 (부모→자식)

"A 가 뽑은 레코드를 B 가 받아서 이어서 처리한다."

### 언제

- 게시판 목록을 수집(A) → **각 게시글**을 수집(B)
- 부모의 **출력 레코드 하나하나**가 자식의 입력이 된다
- 계층형 데이터 (Board → Post → Comment)

### `depends_on` 과 근본적으로 다른 점

`depends_on` 은 **순서만** 정한다. 데이터는 전달되지 않는다 —
B 는 DB 를 다시 읽어야 한다.

pipeline link 는 **Kafka 토픽으로 레코드가 흐른다.** A 가 출력하는 즉시
B 가 소비하므로, A 가 끝나기를 기다리지 않는다.

```
depends_on :  [A 완료] ──→ [B 시작]          (순서)
link       :  [A] ──Kafka──→ [B]             (데이터 스트림)
```

### 설정

파이프라인 설정이 아니라 **별도 API** 로 링크를 만든다.

```bash
POST /api/v1/pipeline-links
{
  "workflow_id": "...",
  "parent_pipeline_id": "board-collect",
  "child_pipeline_id": "post-collect"
}
```

링크가 생기면 실행 시 자식 파이프라인의 input 이 **Kafka 로 자동 치환**된다
(`kafka_link_input_<parent>`). 자식 쪽에 input 을 따로 적지 않아도 된다.

Kafka 가 필요하다 — 로컬에서는 `docker-compose --profile with-kafka up -d`.

---

## `execution_mode` 세 가지

워크플로 **전체**에 적용된다. 파이프라인별로 다르게 줄 수 없다.

| 값 | 동작 | `priority` | `depends_on` |
|---|---|---|---|
| `parallel` (기본) | 전부 동시 | 무시 | **무시** ⚠️ |
| `sequential` | 하나씩, 번호 순 | **사용** | 무시 |
| `dag` | 의존 그래프 순 | 정렬에만 | **사용** |

설정 위치:

```bash
# API
curl -X PUT .../api/v1/workflows/<id> -d '{"execution_mode":"dag"}'

# DB 확인
SELECT execution_mode FROM workflows WHERE id='<id>';
```

---

## 실제 사례: 공중화장실 수집 + 보강

같은 `restrooms` 테이블에 두 배포본이 쓴다.

- **API(15155058)** — 수집 주체. 화장실명·주소·좌표
- **CSV(15012892)** — 보강. 장애인용·어린이용 변기수, 기저귀교환대 위치

관리번호가 99.99% 겹치므로 **대체가 아니라 보강**이다. 순서가 뒤집히면
CSV 보강값을 API 수집이 덮거나, CSV 에만 있는 신규 건이 이름 없이 INSERT 된다
(실제로 8건이 그렇게 들어갔다).

```yaml
execution_mode: dag

pipelines:
  - id: restroom-api-collect
    priority: 0
    # rest_api → 지오코딩 → restrooms

  - id: restroom-csv-enrich
    priority: 1
    depends_on: [restroom-api-collect]
    # CSV(cp949) → remap → restrooms (UPDATE)
```

실행 결과: 107,164건(수집 53,582 + 보강 53,582), 실패 0, 7분 43초.

---

## 문제 해결

### `depends_on` 을 걸었는데 동시에 실행된다

`execution_mode` 를 확인하라. `parallel`(기본)이면 무시된다.

```sql
SELECT execution_mode FROM workflows WHERE id='<id>';   -- dag 여야 한다
```

이 증상은 **에러가 나지 않는다.** 로그로도 구분되지 않아 시작 시각 간격으로만
알 수 있다.

### `priority` 를 줬는데 순서가 뒤죽박죽이다

`sequential` 이 아니면 `priority` 는 정렬에만 쓰이고 실행을 막지 않는다.
실제로 순서를 지키려면 `sequential` 또는 `dag`+`depends_on` 이어야 한다.

### 자식 파이프라인에 데이터가 안 들어온다

`depends_on` 은 데이터를 전달하지 않는다. 부모의 레코드를 받으려면
pipeline link 를 만들어야 하고, Kafka 가 떠 있어야 한다.

### 순환 의존을 걸면

`runDAG` 가 감지해 실행을 중단한다.

```
circular dependency detected
```

A → B → A 처럼 서로 가리키게 두지 말 것. 자기 자신을 `depends_on` 에 넣는 것도 같다.

---

## 관련 문서

- [ARCHITECTURE.md](ARCHITECTURE.md) — 실행 위임 구조(agent → K8s Job / 상주 파드)
- [EXECUTION_TOPOLOGY_INTENT.md](EXECUTION_TOPOLOGY_INTENT.md) — 실행 토폴로지 설계 의도
- [design-v2.md](design-v2.md) — Input/Stage/Output 모델
