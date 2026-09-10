# 작업 지시 — 환경 분리 정리 + 파이프라인 정의를 Git 정본으로

작성 2026-09-07 / 상태: 미착수

## 요구사항 (확정)

| 환경 | 용도 | 관리 방식 |
|---|---|---|
| **`conduix`** | **서비스용** — "모두의 복지맵" 파이프라인 실행 | **ArgoCD (GitOps)** |
| **`conduix-e2e`** | e2e 테스트 | `make e2e-up` / `e2e-down` (일회성) |

**둘 다 필요하다. 어느 쪽도 없애지 않는다.**

---

## 현재 상태 — 환경 분리 자체는 이미 맞다

| | `conduix` | `conduix-e2e` |
|---|---|---|
| ArgoCD | ✅ `Synced/Healthy` (`main` / `values.yaml`) | ❌ 관리 밖 (helm 수동) |
| DB | 외부 `mysql-cluster` | 내장 `appdb` |
| mock 소스 | ❌ | ✅ REST/Kafka/ES/MySQL |
| 노출 | **NodePort 30000** | ClusterIP |
| helm 릴리스 | 없음(ArgoCD 직접) | `conduix` rev 13 |

`values.yaml` 과 `values-e2e.yaml` 이 `mocks.enabled`·`appdb.enabled` 로 갈린다 — **설계는 올바르다.**

문제는 e2e 환경이 테스트용인데 **3일째 상주**하면서 실작업이 그쪽에 쌓인 것이다.

---

## 문제 1 — 파이프라인 정의가 Git 에 없다

**이것이 근본 원인이다.** 나머지 증상은 전부 여기서 파생된다.

### 지금 어떻게 되어 있나

```go
// control-plane/internal/seed/seed.go:204  — Go 코드에 하드코딩
func sampleWorkflows(projectID string) []*models.Workflow {
    return []*models.Workflow{
        newWorkflow(projectID, "[batch] MySQL → MySQL", ...),
        newWorkflow(projectID, "[batch] REST → MySQL", ...),
        // ... 6종
    }
}
```

- 샘플 6종이 **Go 코드**에 박혀 있다 → 고치려면 재빌드
- `pipeline-core/configs/v2/*.yaml` 에는 **다른 파이프라인들**이 있고 seed 와 무관하다
- **복지맵 파이프라인은 어디에도 없다** — DB 에만 있었다

### 왜 문제인가

ArgoCD 로 관리한다는 것은 **Git 이 정본**이라는 뜻이다.
그런데 파이프라인은 DB 에만 있어서:

- 환경을 다시 올리면 사라진다 (실제로 겪음)
- 서비스 환경(`conduix`)과 테스트 환경(`conduix-e2e`)의 내용이 달라진다
- 무엇이 배포돼야 정상인지 Git 을 봐도 알 수 없다

### 해야 할 것

**샘플·서비스 파이프라인 정의를 YAML 로 옮기고, seed 가 그것을 읽게 한다.**

```
deploy/helm/conduix/pipelines/          (또는 config/pipelines/)
├── samples/                            values.yaml 의 seed.samples=true 일 때만
│   ├── bulk-mysql-to-mysql.yaml
│   ├── bulk-rest-to-mysql.yaml
│   ├── bulk-rest-to-postgres.yaml
│   ├── cdc-rest-polling-to-mysql.yaml
│   ├── cdc-kafka-to-mysql.yaml
│   └── cdc-mysql-to-mysql.yaml
└── welfare-map/                        서비스 환경에 항상 배포
    ├── bulk-공중화장실-geocode-sweep.yaml
    └── cdc-restrooms-지오코딩.yaml
```

**설계 포인트**
- `seed.go` 는 YAML 을 **읽어서** 등록만 한다 (정의를 코드에 두지 않는다)
- ConfigMap 으로 마운트하거나 이미지에 포함 — 어느 쪽이든 **Git 이 원본**
- 접속정보는 지금처럼 `${VAR}` / `SEED_*` env 로 주입 (환경별로 다르므로)
- 샘플 여부는 values 로 제어: 서비스 환경은 복지맵만, e2e 는 샘플까지

> 기존 `configs/v2/*.yaml` 과 형식을 통일할지, 별도 스키마로 갈지는 판단이 필요하다.
> 통일하는 편이 낫지만, `StepV2` 에 `type` 필드가 없어 커스텀 stage 를 못 쓴다
> (→ `docs/work/커스텀-stage-GUI-노출.md` 참조). 이 제약을 먼저 풀어야 할 수 있다.

---

## 문제 2 — 복지맵 프로젝트가 서비스 환경에 없다

e2e 에만 만들어져 있다. 서비스용이므로 `conduix` 에 있어야 한다.

**백업해 둔 정의** (환경 정리 전에 추출함):

```
<scratchpad>/rescue/
├── samples/       6종 (bulk 3 + cdc 3)
└── welfare-map/
    ├── bulk-공중화장실-MySQL-geocode-sweep.json
    └── cdc-restrooms-지오코딩-MySQL.json
```

문제 1 을 해결하면서 이 정의를 YAML 로 옮기면 된다.

**주의**: 복지맵 파이프라인은 mock 이 아니라 **실제 공공데이터 API** 를 호출한다.
서비스 환경에는 mock 이 없으므로(`mocks.enabled: false`) 그대로 맞다.
다만 `KAKAO_REST_KEY`, `cache_dsn` 등 시크릿이 서비스 환경에 있어야 한다
(`deploy/helm/conduix/templates/pipeline-secrets.yaml` 확인).

---

## 문제 3 — e2e 환경 수명 관리

테스트용인데 상주하면서 실데이터가 쌓였다:

```
restrooms       53,578건
geocode_cache   27,018건 (ok 27,018 / not_found 126 / unfixable 2,606)
```

지오코딩에 실제 API 호출과 시간이 들었으므로 **버리기 전에 판단이 필요하다.**

### 정할 것
1. 이 데이터를 서비스 환경으로 옮길지, 서비스에서 파이프라인을 다시 돌릴지
2. e2e 를 `make e2e-down` 으로 내릴지, 남길지
3. 남긴다면 **실데이터를 넣지 않는 규칙**을 세울지 (e2e 는 mock 만)

> 권고: e2e 는 mock 데이터만 쓰고, 실 공공데이터 파이프라인은 서비스 환경에서만 돌린다.
> 지금처럼 섞이면 "어느 쪽 숫자가 진짜인지" 알 수 없게 된다.

---

## 부수 문제 — NodePort 30000 충돌 (해결됨, 기록용)

GoLand 의 JCEF 내장 브라우저(`cef_server --port=30000`)가 30000 을 선점해
web-ui 접속이 `Empty reply from server` 로 실패했다.

- 조치: `cef_server` 종료 → 포트가 Colima ssh 터널로 복귀, HTTP 200 확인
- 재발 방지: `goland.vmoptions` 에 `-Dide.browser.jcef.debug.port=31000` 추가
  (**옵션명이 버전에 따라 다를 수 있어 미검증** — GoLand 재시작 후
  `lsof -nP -iTCP:30000 -sTCP:LISTEN` 로 확인 필요)

---

## 작업 순서 (권고)

```
1. 파이프라인 정의 YAML 스키마 확정
   └ configs/v2 형식과 통일할지 결정 (StepV2 의 type 필드 부재 제약 확인)
2. seed.go 를 "YAML 읽어 등록" 으로 리팩터
   └ 정의를 코드에서 제거, 샘플/서비스 분리를 values 로 제어
3. 복지맵 파이프라인 2종을 YAML 로 작성 (백업본 기반)
4. 서비스 환경 시크릿 확인 (KAKAO_REST_KEY 등)
5. main 머지 → ArgoCD 자동 동기화 확인
6. e2e 데이터 처리 방침 결정 후 정리
```

1·2 가 본체다. 3 이후는 그 위에서 자연히 풀린다.

---

## 함께 볼 문서

- `docs/work/커스텀-stage-GUI-노출.md` — `StepV2` 에 `type` 없음, GUI 에 커스텀 stage 미노출
- `docs/공중화장실-지오코딩-실패케이스.md` — 복지맵 파이프라인의 알고리즘 근거
