# Plugin Architecture V4: 2-Tier Hybrid Model

> **정정(2026-07-02):** 이 문서는 스크립트 Tier를 "Starlark"로 기술하나, 실제 구현은
> **JavaScript(goja)** 로 교체됐다(`js_script` stage). 아래 본문의 "Starlark"는
> "JavaScript(goja)"로 읽는다. 나머지 2-tier 구조(스크립트/네이티브)와 단일 runner 이미지
> 빌드 모델은 현재 구현과 일치한다.

## 개요

커스텀 Stage를 2가지 티어로 제공:
- **Tier 1 (Script)**: JavaScript(goja) 스크립트 → 빌드 없음, 즉시 적용
- **Tier 2 (Native)**: Go 코드 → pipeline-runner에 통합 빌드 → 단일 이미지

### V3 대비 변경점
- go-plugin (gRPC over unix socket) 제거 → 단일 바이너리 인프로세스 실행
- 파이프라인별 이미지 X → **pipeline-runner 이미지 1개에 모든 stage 포함**
- runner 이미지 버전(RunnerVersion) 도입으로 빌드/배포/실행 일관성 보장
- Starlark 스크립트 Tier 추가 (80% 케이스 커버)

### 커스텀 가능 범위

| 구분 | Tier 1 (Script) | Tier 2 (Native) | Builtin |
|------|-----------------|-----------------|---------|
| **Input** | — | — | kafka, rest_api, sql, cdc 등 |
| **Stage** | ✅ Starlark 스크립트 | ✅ Go 코드 | filter, remap, split 등 |
| **Output** | — | — | elasticsearch, kafka, sql 등 |

> Input/Output 커스텀은 V4 범위 외. Stage만 커스텀 대상.

---

## Tier 1: Script Stage

### 특징
- **빌드 불필요** — 기본 runner 이미지에 Starlark 인터프리터 내장
- **즉시 적용** — 파이프라인 config 저장 → 재시작만으로 반영
- **여러 개 자유롭게** — script stage를 원하는 만큼 추가/수정/삭제, 이미지 재빌드 없음
- **RunnerVersion과 무관** — SourceHash/DeployedHash 검증 대상 아님
- **보안** — Starlark는 설계상 샌드박스 (파일/네트워크/시스템 접근 불가)

### 사용법
```yaml
stages:
  - name: "score-classify"
    type: script
    config:
      language: starlark
      code: |
        def process(record):
          score = record.get("score", 0)
          if score >= 80:
            record["grade"] = "A"
          elif score >= 60:
            record["grade"] = "B"
          else:
            record["grade"] = "C"
          return record
```

### 성능
- Starlark 인터프리터 오버헤드: 레코드당 ~10-50μs (추정)
- builtin remap 대비 ~5-10x 느림, 하지만 대부분의 데이터 파이프라인에서는 병목이 I/O (네트워크, DB)이므로 실질적 영향 미미
- 성능이 중요한 대량 처리 → Tier 2 (Native) 사용

### Starlark 내장 함수 (제공 예정)
| 함수 | 설명 |
|------|------|
| `hash_sha256(s)` | SHA256 해시 |
| `base64_encode(s)` / `base64_decode(s)` | Base64 인코딩/디코딩 |
| `json_encode(obj)` / `json_decode(s)` | JSON 직렬화/역직렬화 |
| `regex_match(pattern, s)` | 정규식 매칭 |
| `regex_replace(pattern, s, replacement)` | 정규식 치환 |
| `timestamp_now()` | 현재 시간 (ISO 8601) |
| `timestamp_parse(s, format)` | 시간 파싱 |
| `log(level, message)` | 로그 출력 (debug/info/warn/error) |

### 무한루프/리소스 방어
- 실행 타임아웃: 레코드당 1초 (설정 가능)
- 메모리 제한: Starlark 인터프리터 자체 제한 + Go runtime 감시
- 스택 깊이 제한: 재귀 호출 100 depth

### 에러 처리
```python
def process(record):
  # None 반환 → 레코드 드롭 (filter와 동일 효과)
  if record.get("status") == "deleted":
    return None

  # dict 반환 → 변환된 레코드
  record["processed_at"] = timestamp_now()
  return record

  # 예외 발생 → 에러 로깅, 레코드는 원본 그대로 통과 (또는 설정에 따라 드롭)
```

### GUI
```
┌──────────────────────────────────────────┐
│ Script Stage: score-classify             │
├──────────────────────────────────────────┤
│ Language: [Starlark ▼]                   │
│ ┌──────────────────────────────────────┐ │
│ │ def process(record):                │ │
│ │   score = record.get("score", 0)    │ │
│ │   if score >= 80:                   │ │
│ │     record["grade"] = "A"           │ │
│ │   elif score >= 60:                 │ │
│ │     record["grade"] = "B"           │ │
│ │   else:                             │ │
│ │     record["grade"] = "C"           │ │
│ │   return record                     │ │
│ └──────────────────────────────────────┘ │
│                                          │
│ 테스트 데이터:                            │
│ ┌──────────────────────────────────────┐ │
│ │ [{"score": 85}, {"score": 42}]      │ │
│ └──────────────────────────────────────┘ │
│ 결과: ✅                                 │
│ ┌──────────────────────────────────────┐ │
│ │ [{"score":85,"grade":"A"},          │ │
│ │  {"score":42,"grade":"C"}]          │ │
│ └──────────────────────────────────────┘ │
│                                          │
│ [▶ 테스트] [저장]  ← 빌드 불필요, 즉시 적용│
└──────────────────────────────────────────┘
```

> Script Stage 테스트는 **서버에서 Starlark 인터프리터로 직접 실행** (빌드 없음, <100ms)

---

## Tier 2: Native Stage

### 특징
- **Web UI에서 개발 + 테스트 가능** — Monaco Editor + 서버 사이드 빠른 테스트
- **인프로세스 실행** — gRPC 없음, 직접 함수 호출 (네이티브 속도)
- **외부 모듈 import 가능** — 회사별 커스텀 SDK 연동
- **RunnerVersion 관리** — SourceHash != DeployedHash이면 실행 차단

### 테스트와 빌드 분리

핵심: **테스트는 빠르게 (5-10초), 배포 빌드는 확정 후 1회 (20-40초)**

```
[▶ 테스트] — 플러그인 단독 빌드 + 샘플 데이터 실행 (5-10초)
    서버에서 플러그인 소스만 go build (runner 전체 X)
    빌드된 바이너리에 샘플 데이터 전달 → Process() 결과 반환
    Go 빌드 캐시로 반복 테스트 시 2-3초

[저장 & 빌드] — runner 전체 재빌드 + 이미지 push (20-40초)
    모든 native stage를 포함한 pipeline-runner 전체 빌드
    Docker 이미지 빌드 + push → RunnerVersion 생성
```

#### 테스트 실행 구현
```
POST /api/v1/plugins/:id/test
Request:
  {
    "sample_data": [
      {"user_id": "u-123", "score": 85},
      {"user_id": "u-456", "score": 42}
    ],
    "config": {                    ← Init()에 전달할 config (선택)
      "api_url": "https://..."
    }
  }

서버 내부:
  1. 플러그인 소스 + 테스트 하네스 코드를 임시 디렉토리에 생성
  2. go build (플러그인만, runner 전체 X)
  3. 빌드된 바이너리: Init(config) → ProcessBatch(sample_data) → 결과 수집
  4. 임시 파일 정리 (빌드 캐시는 유지)

Response (성공):
  {
    "status": "success",
    "build_time_ms": 3200,
    "exec_time_ms": 12,
    "results": [
      {"user_id": "u-123", "score": 85, "grade": "A"},
      {"user_id": "u-456", "score": 42, "grade": "C"}
    ]
  }

Response (컴파일 에러):
  {
    "status": "compile_error",
    "error": "./main.go:15:3: undefined: http.NewRequest",
    "build_log": "..."
  }

Response (런타임 에러):
  {
    "status": "runtime_error",
    "error": "panic: runtime error: index out of range [2] with length 1",
    "build_time_ms": 2100
  }
```

#### 테스트 보안
- 빌드 타임아웃: 60초
- 실행 타임아웃: 10초
- 임시 디렉토리 격리
- 빌드 시 위험 import 차단 (`os/exec`, `syscall`, `unsafe` 등)
- 테스트 바이너리는 실행 후 삭제 (빌드 캐시만 유지)

### 플러그인 인터페이스
```go
// plugin-sdk/stage.go (V4 - 인프로세스용)
package sdk

// NativeStage 인프로세스 실행 Stage 인터페이스
// Process 또는 ProcessBatch 중 하나만 구현해도 됨
type NativeStage interface {
    // Init 초기화 (config 전달, 외부 연결 설정 등)
    Init(config map[string]any) error

    // Process 단일 레코드 처리
    // nil 반환 → 레코드 드롭
    Process(record map[string]any) (map[string]any, error)

    // ProcessBatch 배치 처리 (선택적 구현)
    // 미구현 시 Process를 반복 호출
    ProcessBatch(records []map[string]any) ([]map[string]any, error)

    // Close 리소스 정리 (DB 연결, HTTP 클라이언트 등)
    Close() error
}
```

### 자동 생성 코드 (빌드 시)

빌드 시 Builder Service가 모든 native stage를 import하는 코드를 자동 생성:

```go
// auto-generated: registry_custom.go
package main

import (
    plugin_crm "github.com/company-a/crm-enrichment"
    plugin_score "github.com/company-b/score-classifier"
)

func init() {
    stream.RegisterCustomStage("crm-enrichment", func(config map[string]any) (stream.Stage, error) {
        s := &plugin_crm.CRMStage{}
        return newNativeStageAdapter(s, config)
    })
    stream.RegisterCustomStage("score-classifier", func(config map[string]any) (stream.Stage, error) {
        s := &plugin_score.ScoreStage{}
        return newNativeStageAdapter(s, config)
    })
}
```

> **주의**: 플러그인 소스가 DB에 저장되는 경우, go module로 참조할 수 없으므로
> 빌드 시 임시 디렉토리에 소스를 배치하고 local replace directive로 참조.
> ```
> // go.mod (빌드 시 자동 추가)
> replace github.com/conduix/plugins/crm-enrichment => ./plugins/crm-enrichment
> ```

### GUI — Native Plugin 에디터
```
┌──────────────────────────────────────────┐
│ Native Stage: crm-enrichment             │
├──────────────────────────────────────────┤
│ 설명: CRM API에서 고객 등급 조회하여       │
│       customer_grade 필드 추가            │
│                                          │
│ [main.go] [go.mod]                       │
│ ┌──────────────────────────────────────┐ │
│ │ package main                        │ │
│ │ import (                            │ │
│ │     "net/http"                      │ │
│ │     sdk "github.com/.../plugin-sdk" │ │
│ │ )                                   │ │
│ │ type CRMStage struct {              │ │
│ │     apiURL string                   │ │
│ │     client *http.Client             │ │
│ │ }                                   │ │
│ │ func (s *CRMStage) Init(           │ │
│ │     config map[string]any,          │ │
│ │ ) error {                           │ │
│ │     s.apiURL = config["api_url"]    │ │
│ │     ...                             │ │
│ │ }                                   │ │
│ │ func (s *CRMStage) Process(        │ │
│ │     record map[string]any,          │ │
│ │ ) (map[string]any, error) {        │ │
│ │     // CRM API 호출하여 등급 조회     │ │
│ │     ...                             │ │
│ │ }                                   │ │
│ └──────────────────────────────────────┘ │
│                                          │
│ Config:       (Init에 전달될 파라미터)     │
│ ┌──────────────────────────────────────┐ │
│ │ {"api_url":"https://crm.company.com"}│ │
│ └──────────────────────────────────────┘ │
│ 테스트 데이터:                            │
│ ┌──────────────────────────────────────┐ │
│ │ [{"user_id":"u-123","score":85}]    │ │
│ └──────────────────────────────────────┘ │
│ 테스트 결과: ✅ 통과 (3.2초)              │
│ ┌──────────────────────────────────────┐ │
│ │ [{"user_id":"u-123","score":85,     │ │
│ │   "customer_grade":"gold"}]         │ │
│ └──────────────────────────────────────┘ │
│                                          │
│ 배포 상태: ⚠ 빌드 필요 (소스 변경됨)      │
│   현재: abc123 → 배포: def456            │
│   rv-42 ✅ 배포 중 │ rv-43 대기           │
│                                          │
│ [▶ 테스트]  [저장]  [저장 & 빌드]          │
│  (5-10초)          (20-40초)              │
└──────────────────────────────────────────┘
```

---

## Runner 이미지 버전 관리

### 핵심 개념: RunnerVersion

모든 커스텀 native stage는 하나의 pipeline-runner 이미지에 포함된다.
stage가 하나라도 수정되면 runner 이미지를 재빌드해야 하고,
**빌드 완료된 이미지 버전을 기준으로 파이프라인 실행 가능 여부를 결정**한다.

```
RunnerVersion = "rv-{build-number}"  (예: rv-42)
```

### 데이터 모델

```go
// RunnerVersion pipeline-runner 이미지 빌드 버전
type RunnerVersion struct {
    ID            string     `json:"id"`              // "rv-42"
    BuildNumber   int        `json:"build_number"`    // 자동 증가
    Status        string     `json:"status"`          // pending → building → ready | failed
    ImageTag      string     `json:"image_tag"`       // "ghcr.io/.../pipeline-runner:rv-42"
    ImageDigest   string     `json:"image_digest"`    // sha256:... (이미지 무결성 검증)
    SourceHash    string     `json:"source_hash"`     // 모든 native stage 소스의 결합 해시
    PluginIDs     []string   `json:"plugin_ids"`      // 포함된 native stage ID 목록
    PluginHashes  map[string]string `json:"plugin_hashes"` // plugin_id → source_hash 스냅샷
    BuildLog      string     `json:"build_log"`
    Error         string     `json:"error,omitempty"`
    DurationMs    int        `json:"duration_ms"`
    CreatedBy     string     `json:"created_by"`
    StartedAt     *time.Time `json:"started_at"`
    FinishedAt    *time.Time `json:"finished_at"`
    CreatedAt     time.Time  `json:"created_at"`
}

// Plugin 커스텀 Stage 정의
type Plugin struct {
    ID             string    `json:"id"`
    Name           string    `json:"name"`
    Type           string    `json:"type"`              // "script" | "native"
    Description    string    `json:"description"`
    SourceCode     string    `json:"source_code"`       // Go 소스 또는 Starlark 스크립트
    GoMod          string    `json:"go_mod,omitempty"`  // native만
    SourceHash     string    `json:"source_hash"`       // 현재 소스의 SHA256
    DeployedHash   string    `json:"deployed_hash"`     // 최신 ready 이미지에 포함된 소스 해시
    RunnerVersion  string    `json:"runner_version"`    // 이 stage가 포함된 최신 ready 버전 ID
    Version        string    `json:"version"`
    CreatedBy      string    `json:"created_by"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

### 핵심 필드: SourceHash vs DeployedHash

```
SourceHash   = 현재 저장된 소스 코드의 해시 (수정할 때마다 변경)
DeployedHash = 최신 ready 이미지에 포함된 소스의 해시 (빌드 성공 시 갱신)

SourceHash == DeployedHash  → 배포 완료 (실행 가능)
SourceHash != DeployedHash  → 빌드 필요 (실행 차단)
```

---

## 파이프라인 실행 제어

### 실행 전 검증 흐름

```
파이프라인 실행 요청
    ↓
1. 워크플로우 내 모든 파이프라인의 stage 목록 확인
    ↓
2. native plugin stage가 있는가?
    ├─ 없음 (builtin + script만) → 기본 runner 이미지로 즉시 실행
    └─ 있음 → 3으로
    ↓
3. 각 native plugin의 SourceHash == DeployedHash 확인
    ├─ 모두 일치 → 해당 RunnerVersion 이미지로 실행
    └─ 하나라도 불일치 → 실행 차단
        ↓
4. 실행 차단 시 응답:
   HTTP 409 Conflict
   {
     "error": "runner_build_required",
     "message": "커스텀 stage가 수정되었습니다. 빌드가 필요합니다.",
     "pending_plugins": [
       {
         "id": "plugin-1",
         "name": "crm-enrichment",
         "source_hash": "abc...",
         "deployed_hash": "def..."
       }
     ],
     "latest_ready_version": "rv-42",
     "action": "build"
   }
```

### 실행 시 이미지 결정 로직

```go
func resolveRunnerImage(workflow *Workflow) (string, error) {
    nativePlugins := findNativePluginsInWorkflow(workflow)

    if len(nativePlugins) == 0 {
        return defaultRunnerImage, nil
    }

    // 모든 native plugin이 배포 완료 상태인지 확인
    var pendingList []Plugin
    for _, p := range nativePlugins {
        if p.SourceHash != p.DeployedHash {
            pendingList = append(pendingList, p)
        }
    }
    if len(pendingList) > 0 {
        return "", &BuildRequiredError{PendingPlugins: pendingList}
    }

    // 최신 ready RunnerVersion의 이미지 사용
    latestReady, err := getLatestReadyVersion()
    if err != nil {
        return "", fmt.Errorf("no ready runner version found: %w", err)
    }
    return latestReady.ImageTag, nil
}
```

### Workflow 상태와 연동

```
Workflow 시작 요청
    ↓
resolveRunnerImage()
    ├─ 성공 → 정상 실행 (기존 흐름)
    │   ├─ Batch: K8s Job 생성 시 resolved 이미지 지정
    │   └─ Realtime: Agent에 이미지 태그 전달 → runner Pod 생성
    │
    └─ BuildRequiredError → Workflow 시작 거부
        ↓ HTTP 409 Conflict 응답
        ↓ GUI에서 빌드 필요 안내 + 빌드 버튼 표시
```

### 스케줄된 Workflow의 실행 제어

```
Cron 트리거 → resolveRunnerImage()
    ├─ 성공 → 정상 실행
    └─ BuildRequiredError
        → 실행 스킵 (에러 아님, 워크플로우 상태 유지)
        → 알림 발송: "스케줄 실행이 지연되고 있습니다. 빌드가 필요합니다."
        → 대시보드에 경고 표시
        → 빌드 완료 후 다음 스케줄에서 정상 실행
```

---

## 빌드 흐름 상세

### 1. Stage 수정 → 빌드 필요 표시

```
GUI에서 native stage 소스 수정 & 저장
    ↓
PUT /api/v1/plugins/{id}  (소스 업데이트)
    ↓
Plugin.SourceHash 갱신 (DeployedHash와 달라짐)
    ↓
응답에 build_required: true 포함
    ↓
GUI에서 "빌드 필요" 배지 표시
    ↓
이 stage를 사용하는 워크플로우 목록도 "빌드 필요" 표시
```

### 2. 빌드 실행

```
사용자가 "빌드" 버튼 클릭
    ↓
POST /api/v1/runner/build
    ↓
1. 빌드 중복 방지: 이미 building 상태인 버전이 있으면 거부 (409)
    ↓
2. 소스 결합 해시 계산 → 동일 해시 ready 버전 있으면 빌드 스킵
    ↓
3. 새 RunnerVersion 레코드 생성 (status: building)
    ↓
4. 비동기 빌드 시작 (goroutine):
   a. 모든 native plugin 소스를 임시 디렉토리에 배치
   b. registry_custom.go 자동 생성 (import + RegisterCustomStage)
   c. go.mod에 local replace directive 추가
   d. go build -o pipeline-runner (CGO_ENABLED=0)
   e. Dockerfile: FROM alpine + COPY pipeline-runner
   f. docker build & push → ghcr.io/{org}/pipeline-runner:rv-{N}
    ↓
5. 빌드 성공 시:
   - RunnerVersion.Status = "ready"
   - RunnerVersion.ImageDigest = sha256:...
   - 각 Plugin.DeployedHash = Plugin.SourceHash (빌드 시점 소스 해시)
   - 각 Plugin.RunnerVersion = 새 버전 ID
    ↓
6. 빌드 실패 시:
   - RunnerVersion.Status = "failed"
   - RunnerVersion.Error = 에러 메시지
   - DeployedHash 변경 없음 → 기존 ready 이미지로 실행 가능 유지
```

### 3. 빌드 중 stage 수정 시

```
rv-43 빌드 중 (plugin-A의 hash: aaa)
    ↓
사용자가 plugin-A 소스 수정 (hash: bbb)
    ↓
rv-43 빌드 완료 → DeployedHash = aaa (빌드 시작 시점 해시)
    ↓
하지만 현재 SourceHash = bbb → 여전히 불일치
    ↓
다시 빌드 필요 (rv-44)
```

> RunnerVersion.PluginHashes에 빌드 시점 해시 스냅샷 저장.
> 빌드 완료 시 현재 SourceHash와 비교하여 DeployedHash 갱신 여부 결정.

### 4. 빌드 환경 관리

```
빌드 서버 요구사항:
  - Go 1.26+ 컴파일러
  - Docker (이미지 빌드/push용)
  - 레지스트리 인증 정보

빌드 위치 선택지:
  a. Control Plane 내부 (현재 V3 방식)
     - 장점: 별도 서비스 불필요
     - 단점: Control Plane 리소스 사용, Go 컴파일러 포함으로 이미지 커짐

  b. 별도 Builder Pod (권장)
     - Go + Docker 환경이 포함된 builder 이미지
     - 빌드 요청 시 K8s Job으로 실행
     - Control Plane은 Job 결과만 수신
     - 장점: Control Plane 경량 유지, 빌드 리소스 격리
```

---

## GUI 에디터 통합

### Stage 추가 다이얼로그
```
┌─────────────────────────────────────┐
│ Stage 추가                           │
├─────────────────────────────────────┤
│ ● Builtin Stage                     │
│   filter, remap, drop, merge,       │
│   split, encrypt, dedupe, ...       │
│                                     │
│ ● Script Stage (Tier 1)             │
│   빌드 없이 즉시 적용                 │
│   Starlark 스크립트로 자유 변환       │
│                                     │
│ ● Native Plugin (Tier 2)            │
│   Go 코드로 고성능 처리              │
│   외부 모듈 import 가능              │
│   빌드 필요                          │
│                                     │
│ 기존 Native Plugin 사용:             │
│   [crm-enrichment ▼]  ← 등록된 목록  │
└─────────────────────────────────────┘
```

> "기존 Native Plugin 사용" 섹션: 이미 등록된 native stage를 파이프라인에 추가.
> 같은 native stage를 여러 파이프라인에서 config만 다르게 사용 가능.

### Workflow 실행 버튼 상태

```
[▶ 실행] — 정상 (모든 stage 배포 완료 또는 native 미사용)

[▶ 실행] (비활성) — 빌드 필요
  ⚠ "crm-enrichment" stage가 수정되었습니다. 빌드가 필요합니다.
  [🔨 빌드]

[▶ 실행] (비활성) — 빌드 중
  ⏳ Runner 이미지 빌드 중... (rv-43)

[▶ 실행] — 빌드 완료 후 자동 활성화
```

### Plugin 관리 페이지 (신규)

Workflow/Pipeline 에디터와 별도로, 전체 native plugin을 관리하는 페이지:

```
┌──────────────────────────────────────────────────────────┐
│ Custom Stages                                [+ 새 Stage]│
├──────────────────────────────────────────────────────────┤
│ 이름              │ 타입   │ 상태      │ 사용 중          │
│ crm-enrichment    │ native │ ✅ 배포됨  │ wf-1, wf-3      │
│ score-classifier  │ native │ ⚠ 빌드필요 │ wf-2             │
│ data-masking      │ script │ ✅ 즉시    │ wf-1, wf-2, wf-4│
│ field-normalizer  │ script │ ✅ 즉시    │ wf-5             │
├──────────────────────────────────────────────────────────┤
│ Runner 이미지: rv-42 (2026-03-10 14:30)     [🔨 빌드]    │
│ 빌드 필요: score-classifier 수정됨                        │
└──────────────────────────────────────────────────────────┘
```

---

## 실행 시나리오별 동작

### 시나리오 1: Builtin + Script만 사용
```
실행 요청 → native plugin 없음 → 기본 runner 이미지 → 즉시 실행
(RunnerVersion 검증 불필요)
```

### 시나리오 2: Native plugin 포함, 빌드 완료
```
실행 요청 → native plugin 있음
→ SourceHash == DeployedHash (모두 일치)
→ 최신 ready RunnerVersion 이미지(rv-42)로 실행
```

### 시나리오 3: Native plugin 수정, 빌드 안 함
```
실행 요청 → native plugin 있음
→ crm-enrichment: SourceHash != DeployedHash
→ 실행 차단 (HTTP 409)
→ GUI에서 "빌드 필요" 안내 + 빌드 버튼
→ 사용자가 빌드 → 성공 → rv-43 ready
→ 재실행 → rv-43 이미지로 실행
```

### 시나리오 4: 빌드 실패
```
빌드 → go build 실패 (컴파일 에러)
→ rv-43 status: failed, 빌드 로그 저장
→ DeployedHash 변경 없음
→ 이전 소스 기반 파이프라인: rv-42로 실행 가능 (영향 없음)
→ 수정된 소스 기반 파이프라인: 계속 차단
→ 소스 수정 후 재빌드
```

### 시나리오 5: 실행 중 + stage 수정
```
rv-42 이미지로 파이프라인 실행 중
→ 사용자가 crm-enrichment 소스 수정 → SourceHash 변경
→ 실행 중인 파이프라인: 영향 없음 (이미 rv-42 Pod 실행 중)
→ 새 실행 요청: 차단 (빌드 필요)
→ 빌드 → rv-43 ready
→ 새 실행: rv-43 이미지로 실행
→ 기존 실행: rv-42로 계속 (자연 종료까지)
```

### 시나리오 6: 여러 native plugin 중 일부만 수정
```
plugin-A: SourceHash == DeployedHash (변경 없음)
plugin-B: SourceHash != DeployedHash (수정됨)
→ plugin-B를 사용하는 워크플로우: 실행 차단
→ plugin-A만 사용하는 워크플로우: 정상 실행 가능 (rv-42 이미지)
→ 빌드 시 A+B 모두 포함하여 재빌드 → rv-43
→ 빌드 성공 → A, B 모두 DeployedHash 갱신
```

### 시나리오 7: Native plugin 삭제
```
사용자가 crm-enrichment 삭제 요청
→ 이 plugin을 사용하는 파이프라인이 있는지 확인
  ├─ 있음 → 삭제 거부 (먼저 파이프라인에서 제거 필요)
  └─ 없음 → 삭제 허용, 다음 빌드에서 자동 제외
```

### 시나리오 8: 첫 번째 Native plugin 등록 (Runner 이미지 없음)
```
native plugin 최초 등록 → DeployedHash = "" (빌드 이력 없음)
→ 워크플로우에 추가 → 실행 차단 (빌드 필요)
→ 첫 빌드 → rv-1 생성 → DeployedHash 갱신
→ 실행 가능
```

---

## 이미지 태그 전략

### 태그 규칙
```
기본 이미지:    ghcr.io/{org}/pipeline-runner:base
                (builtin + script만, native plugin 없음)

버전 이미지:    ghcr.io/{org}/pipeline-runner:rv-42
                ghcr.io/{org}/pipeline-runner:rv-43
                (builtin + script + native plugins 포함)
```

### 기본 이미지 vs 버전 이미지
- **base**: CI/CD에서 빌드, native plugin 없는 워크플로우용
- **rv-N**: Builder Service에서 빌드, native plugin 포함

### 이미지 정리 정책
- 실행 중인 파이프라인이 참조할 수 있으므로 즉시 삭제 안 함
- 정리 기준: `ready` 상태의 최근 N개 버전만 유지 (기본: 10)
- `failed` 상태: 이미지 없으므로 정리 대상 아님 (빌드 로그만 보관)
- 정리 시 실행 중인 워크플로우가 참조하는 버전은 보호

### Batch Job에서의 이미지 참조
```go
// V4: resolveRunnerImage() 결과 사용
image := resolveRunnerImage(workflow)
// native plugin 없으면: "ghcr.io/{org}/pipeline-runner:base"
// native plugin 있으면: "ghcr.io/{org}/pipeline-runner:rv-42"
```

### Realtime Agent에서의 이미지 참조
```
Agent는 오케스트레이터 역할, 파이프라인 실행은 별도 runner Pod
→ Agent가 runner Pod 생성 시 resolved 이미지 지정
→ 또는 Agent 이미지 자체가 runner 역할인 경우:
   → Agent deployment image를 최신 ready 버전으로 rolling update
```

---

## API 설계

### Runner 빌드 관련

```
POST   /api/v1/runner/build              빌드 트리거
GET    /api/v1/runner/versions            빌드 버전 목록
GET    /api/v1/runner/versions/:id        빌드 상세 (로그 포함)
GET    /api/v1/runner/versions/latest     최신 ready 버전
DELETE /api/v1/runner/versions/:id        버전 삭제 (이미지 정리)
GET    /api/v1/runner/status              전체 상태 (빌드 필요 여부, 최신 버전 등)
```

### Plugin 관련 (기존 확장)

```
GET    /api/v1/plugins                    플러그인 목록 (type, 배포 상태 포함)
POST   /api/v1/plugins                    플러그인 생성 (script 또는 native)
GET    /api/v1/plugins/:id               플러그인 상세
PUT    /api/v1/plugins/:id               플러그인 수정 (소스 업데이트)
DELETE /api/v1/plugins/:id               플러그인 삭제 (사용 중이면 거부)
GET    /api/v1/plugins/:id/status         배포 상태 (SourceHash vs DeployedHash)
GET    /api/v1/plugins/:id/usage          사용 중인 워크플로우/파이프라인 목록
POST   /api/v1/plugins/validate           소스 검증 (AST import 체크)
POST   /api/v1/plugins/:id/test           빠른 테스트 (단독 빌드 + 샘플 실행)
```

### Workflow 실행 (기존 수정)

```
POST /api/v1/workflows/:id/start
  → 기존 로직 + resolveRunnerImage() 검증 추가
  → 성공: 200 + resolved image tag 포함
  → 실패: 409 Conflict + pending_plugins + action: "build"
```

---

## 구현 단계

### Phase 1: Script Stage (Tier 1)
- [ ] `google/starlark-go` 의존성 추가
- [ ] `pipeline-core/pkg/stream/script_stage.go` — Starlark 실행 엔진
  - [ ] process(record) 함수 호출 규약
  - [ ] 내장 함수 (hash, base64, json, regex, timestamp, log)
  - [ ] 타임아웃, 메모리 제한
  - [ ] None 반환 시 레코드 드롭
- [ ] `pipeline-core/pkg/stream/stage.go` — stage factory에 "script" 타입 등록
- [ ] Script Stage 단위 테스트
- [ ] `POST /api/v1/plugins/:id/test` — Script 테스트 API (인터프리터 직접 실행)
- [ ] `web-ui` — Script Stage 에디터 (Monaco + Python 하이라이트 + 테스트 UI)

### Phase 2: RunnerVersion 관리 + 실행 제어
- [ ] `RunnerVersion` DB 모델 + 마이그레이션
- [ ] `Plugin` 모델에 `SourceHash`, `DeployedHash`, `RunnerVersion` 필드 추가
- [ ] `resolveRunnerImage()` — 실행 전 검증 로직
- [ ] Workflow 시작 API에 검증 통합 (409 응답)
- [ ] 스케줄 실행 시 빌드 필요 건 알림/스킵 처리
- [ ] `GET /api/v1/runner/status` — 전체 상태 API
- [ ] `GET /api/v1/plugins/:id/usage` — 사용처 조회 API

### Phase 3: Native Stage 빌드 시스템
- [ ] Builder Service: registry_custom.go 자동 생성
- [ ] Builder Service: go.mod replace directive 생성
- [ ] 모든 native stage를 포함한 단일 바이너리 빌드
- [ ] Docker 이미지 빌드 + push
- [ ] 소스 결합 해시 기반 빌드 캐싱 (중복 빌드 방지)
- [ ] 빌드 성공 시 DeployedHash 갱신 + 빌드 중 수정 감지
- [ ] 빌드 중복 실행 방지 (이미 building 상태면 거부)
- [ ] `POST /api/v1/plugins/:id/test` — Native 테스트 API (단독 빌드 + 실행)
- [ ] Builder Pod (K8s Job) 방식 검토

### Phase 4: GUI 통합
- [ ] Plugin 관리 페이지 (목록, 생성, 수정, 삭제, 배포 상태)
- [ ] Stage 추가 다이얼로그 (Builtin / Script / Native 선택 + 기존 plugin 선택)
- [ ] Native Plugin 에디터 (코드 + config + 테스트 + 배포 상태)
- [ ] Workflow 실행 버튼 상태 연동 (빌드 필요/빌드 중/실행 가능)
- [ ] 빌드 로그 조회
- [ ] 빌드 이력 목록

### Phase 5: go-plugin 제거 + 정리
- [ ] plugin_stage.go (gRPC 방식) 제거
- [ ] plugin-sdk의 gRPC 관련 코드 제거 (NativeStage 인터페이스만 유지)
- [ ] proto/ gRPC 정의 정리
- [ ] PluginBinary DB 모델 제거
- [ ] 이전 버전 이미지 정리 정책 구현

---

## 빌드 캐시 최적화

Runner 이미지 빌드는 stage 수정마다 발생하므로, **캐시 히트율이 빌드 시간을 결정**한다.
CI 운영 경험에서 확인된 교훈:
- Docker layer cache 키 변경 → 전체 재빌드 (6초 → 2분+)
- Go module cache miss → 의존성 재다운로드 (11초 → 3분+)

### Dockerfile 레이어 설계

**핵심 원칙: 변경 빈도가 낮은 것부터 위에, 높은 것을 아래에 배치**

```dockerfile
# 1. Base image — 거의 안 바뀜 (CACHED)
FROM golang:1.26-alpine AS builder

# 2. 시스템 의존성 — 거의 안 바뀜 (CACHED)
RUN apk add --no-cache git

# 3. Go module 다운로드 — go.mod 변경 시만 재실행
#    ⚠ builtin go.mod와 plugin go.mod를 분리하여 COPY
COPY pipeline-core/go.mod pipeline-core/go.sum ./pipeline-core/
COPY shared/go.mod shared/go.sum ./shared/
COPY plugin-sdk/go.mod plugin-sdk/go.sum ./plugin-sdk/
# plugin go.mod는 별도 레이어 — plugin 의존성 추가 시에만 cache miss
COPY plugins/go.mod plugins/go.sum ./plugins/
RUN go mod download

# 4. Builtin 소스 — builtin 수정 시에만 재빌드 (대부분 CACHED)
COPY pipeline-core/ ./pipeline-core/
COPY shared/ ./shared/
COPY plugin-sdk/ ./plugin-sdk/

# 5. Plugin 소스 — stage 수정 시 여기만 cache miss
COPY plugins/ ./plugins/

# 6. 빌드 — plugin 소스만 바뀌면 incremental build
RUN CGO_ENABLED=0 go build -o /pipeline-runner ./cmd/runner
```

### 레이어별 캐시 영향

| 레이어 | 변경 빈도 | Cache miss 시 영향 | 대응 |
|--------|----------|-------------------|------|
| Base image | 거의 없음 | 전체 재빌드 (~3분) | Go 버전 업데이트 시에만 |
| go.mod (builtin) | 드묾 | module 재다운로드 (~1분) | builtin 의존성과 plugin 의존성 분리 |
| go.mod (plugin) | plugin 의존성 추가 시 | plugin module만 재다운로드 | builtin과 별도 레이어 |
| Builtin 소스 | CI 릴리스 시 | builtin 재컴파일 (~2분) | base 이미지에 포함하여 회피 |
| **Plugin 소스** | **자주 (개발 중)** | **incremental compile (~10초)** | **이 레이어만 변경되도록 설계** |
| go build | 위 변경 시 | Go incremental build | build cache mount 활용 |

### Go Build Cache 활용

```dockerfile
# Docker BuildKit cache mount — 빌드 간 Go 컴파일 캐시 유지
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /pipeline-runner ./cmd/runner
```

⚠ **주의**: `--mount=type=cache`는 **같은 빌드 서버에서만 유효**.
- K8s Job (Builder Pod) → Job마다 새 Pod이면 cache 없음
- 해결: Builder Pod에 PVC mount 또는 특정 노드에 고정 (nodeSelector)
- GitHub Actions → `cache-from: type=gha`로 layer cache만 활용 가능

### Base 이미지 전략

```
pipeline-runner:base (CI에서 빌드)
├── Alpine + Go runtime
├── builtin input/stage/output 컴파일 완료
└── Starlark 인터프리터 내장

pipeline-runner:rv-N (Builder Service에서 빌드)
├── FROM pipeline-runner:base  ← builtin 레이어 CACHED
├── COPY plugins/ (native stage 소스만 추가)
└── go build (incremental — plugin 코드만 컴파일)
```

**효과**: native stage만 수정하면 base 이미지 위에 plugin 레이어만 추가되어
**전체 빌드 3분 → incremental 빌드 10~20초**로 단축.

### 소스 결합 해시 기반 빌드 스킵

```
빌드 요청 시:
1. 모든 native plugin의 SourceHash를 결합하여 combinedHash 계산
2. 기존 ready RunnerVersion 중 동일한 combinedHash가 있으면 빌드 스킵
3. 해당 버전 재사용 → DeployedHash 갱신만 수행
```

이로써 **동일 소스로 반복 빌드하는 낭비를 방지**.

### Builder Pod 캐시 최적화

```yaml
# Builder Job 예시 — PVC로 Go cache 영속화
apiVersion: batch/v1
kind: Job
spec:
  template:
    spec:
      containers:
        - name: builder
          image: golang:1.26-alpine
          volumeMounts:
            - name: go-cache
              mountPath: /root/.cache/go-build
            - name: mod-cache
              mountPath: /go/pkg/mod
      volumes:
        - name: go-cache
          persistentVolumeClaim:
            claimName: builder-go-cache    # 빌드 간 Go 컴파일 캐시 유지
        - name: mod-cache
          persistentVolumeClaim:
            claimName: builder-mod-cache   # module 다운로드 캐시 유지
```

---

## 마이그레이션 (V3 → V4)

1. 기존 플러그인 소스 코드는 그대로 사용 가능 (인터페이스 유사)
2. `sdk.Serve()` 호출 제거 → struct만 export (빌드 시 자동 등록)
3. 최초 1회 runner 빌드로 모든 기존 플러그인 포함
4. PluginBinary DB 테이블 → 데이터 백업 후 삭제
5. go-plugin 의존성 제거 (go.mod에서 hashicorp/go-plugin 삭제)
