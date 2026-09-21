# 커스텀 stage 의존성 버전 충돌

> 상태: **방안 확정 (2026-09-21) — 구현 준비 중**
> 구현 계획: [plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md](plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md)
> 결정 기록: [adr/0005-dependency-version-coexistence.md](adr/0005-dependency-version-coexistence.md)
>
> 이 문서의 이전 판(2026-09-20)은 "compile-in 을 유지하는 한 해결 불가능" 이라고 썼다.
> 그 전제가 틀렸음을 실험으로 확인했고(§5), 단일 바이너리를 유지하는 해법으로 확정했다.

커스텀 stage 는 하나의 바이너리에 compile-in 된다. 그래서 **서로 다른 회사가 서로 다른
시점에 만든 stage 들이 같은 라이브러리의 다른 버전을 요구하면 빌드가 깨진다.**

---

## 한 줄 요약

| | |
|---|---|
| **무엇이** | 같은 모듈의 서로 다른 버전을 여러 stage 가 요구하는 상황 |
| **왜 생기나** | 개발 **시점**이 다르기 때문. 아무도 고집하지 않아도 시간이 흐르면 발생한다 |
| **지금 어떻게 되나** | admin 이 레지스트리 버전을 올리면 옛 API 를 쓰던 stage 가 컴파일 실패 → **모든 워크플로 실행이 막힌다**(§3) |
| **막을 수 있나** | **가능하다.** Go 가 금지하는 것은 "같은 import 경로의 두 버전" 이지 "같은 라이브러리의 두 버전" 이 아니다(§5) |
| **어떻게** | 버전마다 다른 import 경로로 소스를 복사(fork)해 한 바이너리에 함께 링크한다(§6). 바이너리 하나·파드 하나·배포 경로는 그대로다 |

---

## 1. 왜 피할 수 없는가

이것은 사용자가 무리한 요구를 해서 생기는 문제가 아니다.

```
2026년  A사 개발자가 stage 를 만든다  →  그 시점 최신인 resty 0.8 로 코딩
2027년  admin 이 레지스트리의 resty 를 1.0 으로 올린다 (B사 요청 등)
        →  A사 stage 가 1.0 API 에서 컴파일 실패
```

**정확한 발생 지점**: 현재 구조에서 stage 작성자는 버전을 고를 수 없다. stage 의 go.mod 는
사용자 입력이 아니라 레지스트리에서 생성된다(`runner_builder.go:370`). 따라서 충돌의 유일한
진입점은 **admin 의 모듈 버전 갱신(PUT)** 이다. 그러나 갱신하지 않으면 새 stage 가 새 API 를
못 쓰니, 갱신은 언젠가 반드시 일어난다.

그러므로 질문은 "얼마나 자주 일어나는가" 가 아니라
**"언젠가 반드시 일어나는데 그때 무엇이 일어나는가"** 다.

---

## 2. 기술적 근거

### 하나의 바이너리, 경로당 하나의 버전

모든 커스텀 stage 는 `package main` 하나에 import 되어 단일 바이너리로 링크된다.

```go
// runner_builder.go:639 (GenerateRegistryCustom)
for _, p := range plugins {
    modPath := fmt.Sprintf("github.com/conduix/plugins/%s", sanitizeName(p.Name))
    fmt.Fprintf(&buf, "\t%s %q\n", alias, modPath)   // 모든 plugin 을 한 main 에 import
}
```

Go 모듈의 MVS(Minimal Version Selection)는 한 빌드에서 **import 경로당 정확히 하나의
버전**을 고른다. 이것은 Go 의 의도된 설계이며, 우회하는 언어 기능은 없다.

### 레지스트리는 단일 버전을 강제한다 (현행, D1~D5)

```go
// models.go:644
ModulePath string `gorm:"primaryKey;size:255"`  // PK — 모듈당 한 행
Version    string // "module 당 단일 — 충돌 방지의 물리 근거"
```

이 결정([archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md](archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md), 2026-07-10)은
**사용자가 제각각 버전을 입력해 생기던 충돌**을 없앴다. 그 목적은 달성됐다. 남은 것은 이 문서가
다루는 **시간 경과에 따른 API 비호환**이며, 단일 버전 규칙은 이것을 "admin 갱신 시점의 일괄 파손"
으로 바꿔 놓았을 뿐 없애지 못했다.

### 메이저 버전은 이미 공존한다

Go 는 v2 부터 **모듈 경로에 메이저를 넣는다.** 경로가 다르면 다른 모듈이므로 공존한다.

```
github.com/go-resty/resty/v2   v2.16.5   ← 별개 행으로 등록 가능
github.com/go-resty/resty/v3   v3.0.0    ← 별개 행으로 등록 가능
```

**이 사실이 해법의 열쇠다.** Go 는 "경로가 다르면 공존" 을 이미 허용한다. v0/v1 이나 같은 메이저의
마이너 사이에 경로 차이가 없을 뿐이다. 그 경로 차이를 플랫폼이 만들어 주면 된다(§5, §6).

---

## 3. 지금 실제로 벌어지는 일 (코드로 확인, 2026-09-21)

admin 이 모듈 버전을 갱신(PUT)하면:

| 단계 | 결과 | 근거 |
|---|---|---|
| resty 를 0.8 → 1.0 으로 갱신 | 레지스트리 반영. **빌드는 일어나지 않는다** | `CombinedSourceHash` 가 모듈 버전을 포함하지 않아 다음 빌드가 "동일 해시" 로 스킵됨 (`runner_builder.go:574`) |
| 며칠 뒤 B사가 무관한 stage 추가 | 그때 빌드가 돌고 A사 stage 에서 컴파일 실패 | 원인 귀속이 B사 변경으로 오해됨 |
| 빌드 처리 | `go build` 실패 시 즉시 중단 — `status=failed` | `runner_builder.go:414` |
| 기존 실행 | 계속 동작(이전 `ready` 바이너리) | |
| **모든 워크플로의 새 실행** | **차단** | 리졸버의 pending 판정이 워크플로가 쓰는 stage 가 아니라 **활성 native 전체**를 본다 (`runner_resolver.go:170`). pending 이 하나라도 있으면 `BuildRequired`, 자동 빌드는 다시 실패 → 루프 |

이전 판은 "새 stage 추가·코어 변경이 막힌다" 고 썼는데 실제 폭발 반경은 더 넓다.
**native stage 를 전혀 쓰지 않는 워크플로의 예약 실행까지 멈춘다**(`workflow_start.go:76`,
스케줄러도 같은 함수 호출).

또한 PUT 은 무검증이며 version 을 비우면 GOPROXY 의 `@latest` 를 그대로 쓴다
(`module_handler.go:129`). v0/v1 은 경로에 메이저가 없으니 0.8 → 1.0 점프가 클릭 한 번이다.

---

## 4. 기각한 선택지

| 선택지 | 기각 이유 |
|---|---|
| **완화만** (영향 분석·드라이런·실패 stage 제외 빌드) | 단일 시점 강제를 유지한 채 통증만 줄인다. "언젠가 반드시 일어난다" 에 대한 답이 아니다. 단 그중 에러 귀속·워크플로 기준 리졸버는 §6 에서 재사용된다 |
| **파이프라인/파티션 단위 빌드** (호환 stage 묶음마다 바이너리) | 사용자가 2026-07-10 에 "비현실적" 으로 기각. realtime 은 cluster 당 파드 하나가 바이너리 하나를 들고 여러 실행을 수용하므로(`ARCHITECTURE.md` §4) 바이너리를 나누면 파드도 나눠야 하고, 이는 2026-09-15 에 파드 폭증 문제로 되돌린 구조다 |
| **프로세스 분리** (go-plugin/gRPC, V3 회귀) | 같은 격리를 주지만 `Process(record)` 가 레코드 단위 호출이라 IPC 비용이 처리량에 직결되고, stage 별 바이너리 저장·파드 내 서브프로세스 관리가 새로 생긴다. ADR-0003 이 "신뢰할 수 없는 3rd-party 코드" 를 재검토 트리거로 남긴 별도 문제다. 정량 근거(벤치마크)는 여전히 없다 |
| **import alias** | alias 는 파일 안의 이름일 뿐 패키지 식별자(경로)가 아니다. 실험 1(§5) |

---

## 5. 실험으로 확인한 사실 (Go 1.27.1, 2026-09-21)

같은 라이브러리의 두 "버전" 을 흉내낸 로컬 모듈 두 개(`foo_a`=v1.0, `foo_b`=v0.8)로 검증했다.

| # | 실험 | 결과 | 의미 |
|---|---|---|---|
| 1 | 한 경로를 alias 둘로 import | 둘 다 같은 버전 출력 | alias 는 경로가 아니다 |
| 2 | 경로 둘(`example.com/foo`, `example.com/foo_v0_8`) + 디렉토리 `replace`. 복사본 go.mod 의 `module` 선언은 원래 경로 그대로 | **두 버전 공존** | Go 는 디렉토리 replace 의 module 선언 불일치를 허용한다. 복사본 go.mod 수정 없이도 동작 |
| 3 | 2 + 복사본 go.mod `module` 선언도 새 경로로 수정 | 두 버전 공존 | 동일. 안전을 위해 수정하는 쪽을 택한다 |
| 4 | 다중 패키지 모듈(`foo/sub`), 복사본 내부의 자기 참조 import(`example.com/foo/sub`) 를 **안 고침** | **컴파일 성공, 그러나 `v0.8 + sub of v1.0`** | 가장 위험한 함정. 에러 없이 서브패키지만 새 버전이 섞인다 |
| 5 | 복사본 내부 import 를 `example.com/foo_v0_8/sub` 로 재작성 | `v0.8 + sub of v0.8` | 내부 import 재작성은 **필수**이며 기계적이다 |
| 6 | `go mod download -json github.com/google/uuid@v1.3.0` | 모듈 캐시 디렉토리(`Dir`) 반환 | 복사 원천을 go 도구로 확보. 별도 다운로드 코드 불필요 |

결론: **"compile-in 유지 시 불가능" 이 아니라 "동일 import 경로 유지 시 불가능" 이다.**

이 기법은 Go 언어 기능이 아니다. 이름도 없다. JVM 의 shading/relocation 과 같은 원리이고,
Go 생태계에서는 Kubernetes 의 `third_party/forked/` 처럼 **사람이 수동으로 fork 를 떠서 유지**하는
형태로 존재한다. 우리가 하는 것은 그 fork 를 **빌드 시점에 플랫폼이 자동으로 뜨는 것**이다.

---

## 6. 확정 방안: stage 별 버전 고정 + 비기본 버전 자동 fork

### 원리

- **레지스트리는 모듈당 여러 버전을 보유**하고, 그중 하나가 기본(default)이다.
- **stage 는 자기 의존성 버전을 고정**한다(`plugins.dep_versions`). 처음 저장될 때 그 시점의
  기본 버전으로 채워지고, 이후 소유자가 명시적으로 올리기 전까지 유지된다.
- **빌드는 고정값을 그대로 존중**한다. 기본 버전과 같은 것은 지금처럼 원래 경로로 링크한다.
  기본과 다른 버전은 해당 버전 소스를 `forked/<경로>@<버전>/` 으로 복사하고, 복사본 내부
  import 와 그 stage 의 import 문을 그 경로로 재작성해 함께 링크한다.
- **결과는 바이너리 하나**다. RunnerVersion·streaming 파드·initContainer 주입·rolling 은 바뀌지 않는다.

```
admin 이 resty 기본을 0.8 → 1.0 으로 올림
  A사 stage: dep_versions = {resty: 0.8}  → forked/go-resty_resty_v0_8 로 링크
  B사 stage: dep_versions = {resty: 1.0}  → github.com/go-resty/resty 로 링크
  빌드 성공, 모두 새 바이너리로 실행. 아무 것도 깨지지 않는다.
```

### 운영 규칙

| 상황 | 동작 |
|---|---|
| admin 이 기본 버전 변경 | **기존 stage 는 건드리지 않는다.** 새 stage 만 새 기본값을 받는다 |
| stage 소유자가 "기본 버전으로 올리기" 실행 | 기본 버전으로 테스트 컴파일 → 성공 시 `dep_versions` 갱신, 실패 시 유지 + 에러 표시 |
| stage 가 기본과 다른 버전에 고정됨 | UI 에 "구버전 고정" 배지. 레지스트리 화면에 버전별 사용 stage 수 표시 |
| 비기본 버전을 쓰는 stage 가 0 | admin 이 그 버전을 retire 가능 |
| 갈라진 버전 수가 계속 늘어남 | 수렴이 안 되고 있다는 운영 신호. 해당 stage 소유자에게 통보 |

### 내장 stage 와의 관계

pipeline-core 가 쓰는 모듈은 core 의 go.mod 버전이 사실상 기본값이다. 레지스트리 기본값이 그보다
높으면 MVS 가 core 도 그 버전으로 올리는데, 이는 **지금도 일어나는 동작**이라 새 위험이 아니다.
custom stage 가 core 와 다른 버전에 고정되면 위 절차로 갈라지고 core 는 영향받지 않는다.

---

## 7. 한계 (이 방안이 못 푸는 것)

| 한계 | 내용 | 대응 |
|---|---|---|
| **init 전역 등록 모듈** | `database/sql` 드라이버는 같은 이름을 두 번 `Register` 하면 panic. protobuf 생성 코드·prometheus 기본 레지스트리도 중복 등록을 거부한다. 이런 모듈은 두 버전을 링크할 수 없다 | 빌드 직후 바이너리를 **init 만 실행하고 종료**하는 자가점검으로 panic 을 빌드 실패로 잡고, 해당 모듈을 `single_version_only` 로 표시해 비기본 고정을 저장 시점에 거부한다. 이 부류는 현행 단일 버전 규칙에 남는다 |
| **간접 의존성** | fork 복사본이 require 하는 하위 모듈은 MVS 로 한 버전에 모인다 | Go 의 같은 메이저 호환 가정에 기댄다. 그 가정이 깨지는 경우는 잡지 못한다 |
| **문자열 경로 참조** | `go:linkname`, 리플렉션·문자열로 자기 패키지 경로를 쓰는 코드는 재작성에서 빠진다 | 드물다. 런타임에 드러난다 |
| **도구 체인 보장 일부 상실** | `govulncheck`·`go mod verify` 는 원래 경로 기준이라 fork 복사본은 스캔 밖 | 복사 원천이 모듈 캐시라 go.sum 검증은 다운로드 시 한 번 된다. 취약점 점검은 fork 디렉토리에 별도 실행 |
| **바이너리 크기** | 갈라진 버전 수만큼 해당 모듈이 중복 링크. 파드마다 내려받음 | 갈라진 수를 운영 지표로 노출. 수렴 규칙으로 억제 |
| **D4 결정의 부분 번복** | "개별 고정 불가, 전역 일괄" → "기본은 전역, stage 는 자기 시점 고정" | ADR-0005 로 기록 |

---

## 관련

- [plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md](plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md) — 구현 계획(파일 단위)
- [adr/0005-dependency-version-coexistence.md](adr/0005-dependency-version-coexistence.md) — 결정 기록
- [archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md](archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md) — 단일 버전 레지스트리(D1~D5) 원 설계
- [ARCHITECTURE.md](ARCHITECTURE.md) — 실행 구조(단일 바이너리·streaming 파드)
- [adr/0003-plugin-architecture-evolution.md](adr/0003-plugin-architecture-evolution.md) — V2→V4 변천, gRPC 폐기 근거 미측정
