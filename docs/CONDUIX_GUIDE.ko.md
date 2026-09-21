# Conduix 사용 가이드

처음 쓰는 사람이 위에서부터 읽고 따라 할 수 있게 쓴 문서다.
각 절은 **무엇을 / 왜 / 어떻게 / 확인법** 순서다.

| 절 | 내용 |
|---|---|
| [개요](#개요) | 무엇을 하는 플랫폼인가 |
| [1. 파이프라인 만들기](#1-파이프라인-만들기) | Input → Stage → Output |
| [2. 배치와 실시간](#2-배치와-실시간) | 언제 무엇을 고르나 |
| [3. 파이프라인 엮기](#3-파이프라인-엮기) | depends_on / priority / link |
| [4. 커스텀 stage](#4-커스텀-stage) | JavaScript / native Go |
| [5. 스케줄과 운영](#5-스케줄과-운영) | 모니터링·체크포인트·장애 대응 |
| [부록. 자주 겪는 함정](#부록-자주-겪는-함정) | 실측으로 확인된 것들 |

---

## 개요

Conduix 는 **수집 → 변환 → 적재**를 하나의 워크플로로 묶는 Kubernetes-네이티브
데이터 파이프라인 플랫폼이다. Kafka Connect·Flink·Airflow 를 각각 운영하지 않아도
같은 일을 한다.

```
Input  →  Stage  →  Output
(수집)    (변환)    (적재)
```

- **Input 16종 / Output 9종** 내장 — Kafka, SQL, MySQL/PostgreSQL CDC, REST, 파일,
  S3, Elasticsearch, MongoDB, BigQuery 등
- **내장 변환 stage 21종** + 커스텀 로직(JavaScript 또는 native Go)
- 배치·실시간을 **같은 모델**로 정의 — GUI 또는 YAML/API

### 용어

| 용어 | 뜻 |
|---|---|
| **워크플로(workflow)** | 실행·제어의 단위. 파이프라인 여러 개를 담는다 |
| **파이프라인(pipeline)** | Input → Stage → Output 한 줄기 |
| **stage** | 데이터 변환 한 단계 (filter, remap, 커스텀…) |
| **실행(execution)** | 워크플로를 한 번 돌린 기록 |

개별 파이프라인은 따로 제어하지 않는다 — **제어는 언제나 워크플로 단위**다.

### 어디서 하나

| 하는 일 | 화면 |
|---|---|
| 워크플로 만들기·실행 | `/workflows` |
| 실행 현황 보기 | `/executions` |
| 커스텀 stage 작성 | `/stages` |
| runner 빌드 상태 | `/runner` |

---

## 1. 파이프라인 만들기

### 무엇을

데이터를 어디서 읽어(Input), 어떻게 바꾸고(Stage), 어디에 넣을지(Output) 정한다.

### 어떻게

```yaml
pipelines:
  - id: my-pipeline
    name: 예시 파이프라인

    input:                      # 1) 어디서 읽나
      type: rest_api
      config:
        url: https://api.example.com/items

    stages:                     # 2) 어떻게 바꾸나 (없어도 된다)
      - type: filter
        config:
          condition: '.status == "active"'
      - type: remap
        config:
          mappings:             # {원본필드: 새이름} 방향이다
            item_id: id
            item_name: name

    outputs:                    # 3) 어디에 넣나 (여러 개 가능)
      - name: mysql-sink
        type: sql
        config:
          driver: mysql
          dsn: "user:${DB_PASSWORD}@tcp(host:3306)/db"
          table: items
          on_conflict: update
          conflict_columns: [id]
```

### 알아둘 것

**① `${VAR}` 로 비밀값을 숨긴다**

API 키·비밀번호를 설정에 평문으로 쓰지 않는다. 워크플로 설정은 DB 에 저장되고
화면·API 로도 노출된다.

```yaml
url: https://api.example.com?serviceKey=${PUBLIC_DATA_KEY}
headers:
  Authorization: "KakaoAK ${KAKAO_REST_KEY}"
```

실제 값은 실행 파드의 환경변수에서 온다(K8s Secret 주입). URL·헤더·DSN 모두 지원한다.

**② Output 마다 다른 변환을 줄 수 있다**

`stages` 는 모든 Output 에 공통 적용된다. Output 별로 다르게 하려면 `pre_stages` 를 쓴다.

```yaml
outputs:
  - name: es-sink
    type: elasticsearch
    pre_stages:                 # 이 Output 에만 적용
      - type: remap
        config:
          mappings: { created_at: "@timestamp" }
```

**③ 배치 설정은 Output 전송 방식이다**

```yaml
batch:
  enabled: true
  output_mode: bulk     # bulk(N건 한 번에) 또는 individual(1건씩)
  size: 500
  workers: 4            # Stage 병렬 워커 수
```

Stage 는 **항상 병렬**로 돈다. `output_mode` 는 Output 이 어떻게 내보낼지만 정한다.

### 확인법

워크플로를 실행한 뒤 `/executions` 에서 처리 건수와 실패 건수를 본다.

---

## 2. 배치와 실시간

### 무엇을

같은 파이프라인 모델로 두 가지 성격의 작업을 돌린다. **워크플로의 `type` 으로 정한다.**

### 어떻게 고르나

| | batch | realtime |
|---|---|---|
| 성격 | 한 번 돌고 끝난다 | 계속 흐른다 |
| 예 | 공공데이터 전량 수집, 일 1회 동기화 | CDC, Kafka 소비 |
| 실행 단위 | **실행마다 K8s Job 1개** (끝나면 종료) | **cluster 당 상주 파드 1개**, 실행은 그 안의 고루틴 |
| 트리거 | `POST /workflows/{id}/trigger` | `POST /workflows/{id}/start` ⚠️ |
| 중지 | 끝나면 자동 | `POST /workflows/{id}/stop` |

⚠️ **realtime 은 `/trigger` 가 아니라 `/start` 다.** `/trigger` 는 batch 전용이라
realtime 에 쓰면 이렇게 거부된다.

```
only batch workflows can be triggered manually
```

### realtime 이 파드 하나를 공유하는 이유

realtime 실행은 변경 스트림을 싱글스레드로 따라가는 작고 오래 사는 작업이다.
실행마다 Deployment 를 만들면 파이프라인 10개에 파드 10개가 놀고 있게 된다.
그래서 **cluster 당 상주 파드(`conduix-rt`) 하나**를 두고 실행을 고루틴으로 배정한다.

- 실행이 0건이어도 파드는 떠 있는다
- 실행 하나를 stop 해도 다른 실행과 파드는 그대로다
- 파드가 죽으면 체크포인트에서 재개한다

batch 는 정반대다 — 무겁고 끝이 있는 작업이라 **실행마다 Job 하나**가 맞다.

### 확인법

```bash
kubectl get pods -n conduix -l app.kubernetes.io/component=streaming-runner
```

realtime 실행이 여러 개여도 파드는 **1개**여야 한다.

---

## 3. 파이프라인 엮기

### 무엇을

한 워크플로 안에 파이프라인이 여러 개일 때, 순서나 데이터 흐름을 정한다.

### 세 가지 방식 — 이름은 비슷하지만 목적이 다르다

| 하고 싶은 것 | 쓸 것 | 필수 설정 |
|---|---|---|
| **A 가 끝나야 B 시작** | `depends_on` | `execution_mode: dag` ⚠️ |
| 단순히 **하나씩 차례로** | `priority` | `execution_mode: sequential` |
| **A 의 출력 레코드를** B 가 처리 | pipeline link | 링크 API + Kafka |
| 서로 무관, 동시에 | (아무것도) | `parallel` (기본) |

> **가장 흔한 실수**: `depends_on` 만 쓰고 `execution_mode` 를 그대로 두는 것.
> 기본값이 `parallel` 이라 **조용히 무시된다** — 에러도 로그도 없다.

### (1) `depends_on` — 실행 순서

두 파이프라인이 **같은 테이블**을 쓰거나, B 가 A 의 결과를 DB 에서 읽어야 할 때.

```yaml
execution_mode: dag           # ← 이게 없으면 depends_on 이 무시된다

pipelines:
  - id: api-collect
    priority: 0

  - id: csv-enrich
    priority: 1
    depends_on: [api-collect]
```

의존이 없는 파이프라인끼리는 여전히 병렬로 돈다.

### (2) `priority` — 순차 실행

의존 관계를 일일이 적기 번거롭거나, 리소스를 아끼려 동시 실행을 피할 때.

```yaml
execution_mode: sequential    # ← 이게 없으면 priority 가 무시된다

pipelines:
  - id: first
    priority: 0
  - id: second
    priority: 1
```

파이프라인 5개 중 2개만 순서가 중요하다면 `dag` 가 낫다 — 나머지는 기다릴 이유가 없다.

### (3) pipeline link — 데이터 전달

부모가 뽑은 **레코드 하나하나**가 자식의 입력이 될 때(게시판 → 게시글 → 댓글).

```
depends_on :  [A 완료] ──→ [B 시작]      순서만, 데이터는 안 넘어간다
link       :  [A] ──Kafka──→ [B]         레코드 스트림, A 완료를 안 기다린다
```

파이프라인 설정이 아니라 **별도 API** 로 만든다.

```bash
POST /api/v1/pipeline-links
{ "workflow_id": "...", "parent_pipeline_id": "board", "child_pipeline_id": "post" }
```

링크가 생기면 자식의 input 이 Kafka 로 자동 치환된다. Kafka 가 떠 있어야 한다.

### `execution_mode` 요약

| 값 | 동작 | `priority` | `depends_on` |
|---|---|---|---|
| `parallel` (기본) | 전부 동시 | 무시 | **무시** ⚠️ |
| `sequential` | 하나씩, 번호 순 | **사용** | 무시 |
| `dag` | 의존 그래프 순 | 정렬에만 | **사용** |

워크플로 **전체**에 적용된다. 파이프라인별로 다르게 줄 수 없다.

### 확인법

`runDAG` 는 전용 로그를 남기지 않는다. **시작 시각 간격**으로 판별한다.

```bash
kubectl logs -n conduix <job-pod> | grep "creating input source"
```

```
07:59:14.858  api-collect     ← 먼저
08:02:00.447  csv-enrich      ← 2분 46초 뒤 = 의존 지켜짐 ✅
```

간격이 **0.1초 이내**면 `parallel` 로 돌고 있다는 뜻이다.

---

## 4. 커스텀 stage

### 무엇을

내장 stage 로 부족할 때 직접 로직을 짠다. 방식이 둘인데 **성격이 다르다** —
하나는 설정에 코드를 적는 내장 stage 고, 하나는 별도 등록·빌드가 필요한 플러그인이다.

### 두 가지 방식

| | `js_script` stage | native Go stage |
|---|---|---|
| 정체 | **내장 stage 타입** — 워크플로 설정에 코드를 적는다 | `/stages` 에 등록하는 **별도 플러그인** |
| 빌드 | **불필요** — 설정 저장하면 바로 실행 | runner 재빌드 필요 |
| 성능 | JS 인터프리터(goja) | 네이티브 |
| 외부 라이브러리 | 불가 | **가능**(허용 모듈 한정) |
| 적합 | 간단한 변환, 빠른 반복 | 고성능, 외부 SDK, 회사 내부 모듈 |

### js_script 로 쓸 때

파이프라인 설정 안에 바로 적는다. 등록도 빌드도 없다.

```yaml
stages:
  - type: js_script
    config:
      code: |
        function process(record) {
          record.full_name = record.first + " " + record.last;
          return record;          // null 을 반환하면 그 레코드는 드롭된다
        }
```

`process(record)` 함수를 **반드시** 정의해야 한다. 없으면 이렇게 거부된다.

```
js_script stage "x": must define a 'process(record)' function
```

### native Go 로 쓸 때

**① 계약이 고정돼 있다**

```go
package my_stage

import sdk "github.com/conduix/conduix/plugin-sdk"

// struct 이름은 반드시 Stage 여야 한다(빌더가 &Stage{} 로 생성한다)
type Stage struct {
    sdk.BaseNativeStage
    myField string
}

func (s *Stage) Init(config map[string]any) error {
    s.myField, _ = config["my_field"].(string)
    return nil
}

// Process 는 병렬로 호출된다 — 내부 상태는 mutex/atomic 으로 보호할 것
// nil 을 반환하면 그 레코드는 드롭된다
func (s *Stage) Process(record map[string]any) (map[string]any, error) {
    record["new_field"] = "value"
    return record, nil
}
```

**② 외부 모듈은 허용 목록에서만 쓴다**

`/stages` 의 **의존성(허용 모듈)** 탭에서 고른다. 버전은 플랫폼이 관리하며
**등록 시점의 최신 버전으로 고정**된다(이후 자동으로 올라가지 않는다).

⚠️ 같은 모듈의 **서로 다른 마이너 버전은 공존할 수 없다.** 모든 커스텀 stage 가
하나의 바이너리에 컴파일되기 때문이다. 자세한 내용과 한계는
[커스텀 stage 의존성 버전 충돌](CUSTOM_STAGE_DEPENDENCY_CONFLICT.md) 참고.

**③ 재빌드는 자동으로 걸린다**

stage 코드나 conduix 코어가 바뀌면 runner 를 다시 빌드해야 실행할 수 있다.
**이건 직접 챙기지 않아도 된다** — 실행을 누르면 control-plane 이 필요 여부를 판단해
빌드를 걸고, 끝나면 실행을 이어서 시작한다. 실행 버튼 한 번이면 된다.

수동으로 빌드하려면 `/runner` 화면 또는 `POST /api/v1/runner/build` 를 쓴다.
빌드가 실패해도 기존 runner 는 그대로 돌아간다 — 돌던 실행이 멈추지는 않는다.

### 확인법

```bash
# 빌드 상태
SELECT build_number, status, duration_ms FROM runner_versions ORDER BY build_number DESC LIMIT 1;
```

`status` 가 `ready` 여야 실행할 수 있다.

---

## 5. 스케줄과 운영

### 스케줄

워크플로에 cron 을 걸어 자동 실행한다.

```bash
PUT /api/v1/workflows/{id}/schedule
{ "type": "cron", "cron": "0 21 * * *", "timezone": "Asia/Seoul" }
```

`timezone` 을 지정하지 않으면 **UTC 로 돈다.** 한국 시각 기준으로 돌리려면
반드시 `Asia/Seoul` 을 넣는다. (DB 컬럼은 `schedule_*` 이지만 API 필드명은 접두사가 없다.)

### 모니터링

| 보고 싶은 것 | 어디서 |
|---|---|
| 지금 뭐가 돌고 있나 | `/executions` — 파드별로 묶여 보인다 |
| agent 가 살아 있나 | `/agents` |
| 지난 실행 기록 | `/history` |
| runner 빌드 상태 | `/runner` |

realtime 파드는 30초마다 실행별 상태를 control-plane 에 올린다.

### 체크포인트

realtime 은 어디까지 읽었는지 기록한다(Kafka offset, CDC binlog 위치 등).
파드가 죽거나 재시작해도 **그 지점부터 재개**한다.

그래서 이상이 감지되면 살려두기보다 **정리하고 체크포인트에서 재시작**하는 쪽이 낫다 —
멈춘 채 살아 있는 실행은 아무도 모르게 데이터만 밀리게 한다.

### 장애 대응

| 증상 | 확인 |
|---|---|
| 실행이 `running` 인데 진행이 없다 | 파드 생존 여부, 체크포인트 갱신 여부 |
| 파드가 CrashLoopBackOff | `kubectl logs` 로 기동 실패 원인 |
| 실행이 고아로 남았다 | agent 의 sweep 이 주기적으로 정리한다 |

---

## 부록. 자주 겪는 함정

실제로 겪어 확인된 것들이다.

### 설정

| 함정 | 증상 | 해결 |
|---|---|---|
| `depends_on` 만 설정 | 파이프라인이 동시 실행 | `execution_mode: dag` 추가 |
| realtime 에 `/trigger` | `only batch workflows can be triggered manually` | `/start` 사용 |
| `remap` 방향 착각 | `Column 'x' cannot be null` | `{원본필드: 새이름}` 이다 |
| cron 에 timezone 누락 | 9시간 어긋난 시각에 실행 | `schedule_timezone: Asia/Seoul` |

### 데이터

| 함정 | 증상 | 해결 |
|---|---|---|
| 국내 공공데이터 CSV | 한글이 전부 깨짐 | `encoding: cp949` (서버가 UTF-8 이라 해도 믿지 말 것) |
| EUC-KR 로 디코딩 | 행이 중간에 잘림 | `cp949` 를 쓴다(확장 한글 때문) |
| DB 시각 비교 | 9시간 어긋난 판정 | DB 는 UTC — `CONVERT_TZ(NOW(),'+00:00','+09:00')` |
| mysql 클라이언트 인코딩 | 한글이 이중 인코딩되어 저장 | `--default-character-set=utf8mb4` 명시 |

### 운영

| 함정 | 증상 | 해결 |
|---|---|---|
| `colima start` 만 실행 | control-plane CrashLoop | `make infra-up` 도 함께 (개발용 MySQL 은 별도) |
| 코어 변경 후 바로 실행 | `runner 가 낡았습니다` | runner 재빌드 후 실행 |
| 이미지 태그가 `:main` 고정 | 배포해도 파드가 안 바뀜 | `kubectl rollout restart` 로 새 이미지를 당긴다 |

---

## 더 읽을 것

- [ARCHITECTURE.md](ARCHITECTURE.md) — 실행 위임 구조(agent → K8s Job / 상주 파드)
- [CUSTOM_STAGE_DEPENDENCY_CONFLICT.md](CUSTOM_STAGE_DEPENDENCY_CONFLICT.md) — 커스텀 stage 의존성 한계
- [COMPARISON.md](COMPARISON.md) — 다른 도구와의 비교·선택 기준
- [adr/](adr/) — 설계 결정 기록
