# Custom Stage 의존성 레지스트리 — 설계·실행 방안

> 작성 2026-07-10. 대상: 이 작업을 이어서 구현할 Claude Code / 개발자.
> 이 문서는 **native custom stage 의 외부 의존성 충돌을 원천 제거**하는 방안이다.
> batch·realtime 공통의 선행 수정 — realtime native 지원([[REALTIME_NATIVE_IMPL_PLAN.md]])보다 먼저 풀어야 한다.

---

## 1. 문제 (코드로 확인된 사실)

RunnerBuilder 는 `type='native' AND status='active'` 인 **모든 stage 를 하나의 Go 모듈로 병합**해 빌드한다(`runner_builder.go:127`, `pluginRequireBlock`, `GenerateRegistryCustom`). 그런데:

- stage 의 go.mod 를 **web-ui 에서 사용자가 자유 입력**한다(`NativeStageEditor.tsx` go.mod 탭, `plugins.go_mod` 컬럼).
- 서로 무관한 stage 들이 **같은 외부 패키지의 다른 버전**을 require 하면 → 한 go.mod 병합 시 Go 의 MVS 가 충돌하거나 호환 안 되는 버전이 섞임 → **`go mod tidy`/`go build` 전체 실패**.
- 한 stage 의 실패가 **그것을 안 쓰는 파이프라인의 빌드까지 마비**시킨다(전체 글로벌 빌드라서).

**근본 원인**: 의존성 **버전을 사용자가 제각각 정하게 둔 것**. Go 충돌은 "같은 패키지의 서로 다른 버전"에서만 난다.

## 2. 해결 원리

**플랫폼이 허용 의존성의 버전을 단일하게 관리**하면, 모든 stage 가 같은 버전 하나만 참조하므로 **버전이 갈릴 수 없어 병합해도 충돌이 발생 불가능**하다. 글로벌 단일 빌드 구조(파드 폭증 없음)를 그대로 유지한 채 충돌만 제거한다.

```
지금:  web-ui go.mod 자유 입력 → stage 마다 제각각 버전 → 병합 시 충돌
바꿈:  web-ui 에서 "허용 모듈"만 선택(버전 입력 불가) → 플랫폼이 버전 하나로 고정
       → 모든 stage 가 동일 버전 참조 → MVS 가 고를 게 하나뿐 → 충돌 0
```

## 3. 핵심 결정 (사용자 확정)

| # | 결정 |
|---|------|
| D1 | **의존성 레지스트리** 도입: `{모듈경로 → 고정버전}` 을 플랫폼이 관리. 모든 stage 빌드가 이 버전을 강제 사용. |
| D2 | web-ui 는 go.mod **자유 입력 폐지** → 허용 모듈 **선택**만. 사용자는 버전을 못 정한다. |
| D3 | **사용자가 레지스트리에 모듈 추가 가능**. 추가 시 **그 시점의 최신 버전으로 등록**(플랫폼이 조회해 고정). |
| D4 | **버전업은 전역 일괄**: 레지스트리의 버전을 올리면 다음 빌드에서 **모든 파이프라인이 함께** 그 버전으로. 개별 파이프라인이 버전을 고정/파편화할 수 없다. |
| D5 | stage 저장 시 **import 검증**: 소스가 import 하는 외부 패키지가 레지스트리(+표준 라이브러리 + plugin-sdk/pipeline-core)에 없으면 저장 거부. |

## 4. 데이터 모델 (신규)

```
allowed_modules 테이블 (신규):
  module_path   string  PK   // "github.com/google/uuid"
  version       string       // "v1.6.0" — 등록 시점 최신, 플랫폼이 관리
  added_by      string       // 등록 사용자
  created_at / updated_at

  ※ version 은 module_path 당 하나(단일 버전 강제의 물리적 근거).
```

- `plugins.go_mod` 자유 입력 컬럼은 **더 이상 신뢰 소스가 아님**. 빌드는 레지스트리에서 go.mod 를 생성한다(§6 W-b). (기존 컬럼은 남기되 빌드에 미사용 → 후속 제거 가능.)

## 5. 흐름

### 5.1 모듈 추가 (D3)
```
개발자가 web-ui 에서 "github.com/google/uuid 추가" 요청
 → control-plane: GOPROXY 에 최신 버전 질의(proxy.golang.org/.../@latest)
 → allowed_modules 에 {uuid, v1.6.0(당시 최신)} INSERT
 → 이후 이 모듈은 목록에 노출, 버전 고정
```

### 5.2 stage 작성 (D2, D5)
```
web-ui 에디터: main.go 만 작성. 의존성은 "허용 모듈 목록에서 체크"
 → 저장 시 control-plane 이 소스 import 를 파싱해, 선택/사용된 외부 import 가
   전부 allowed_modules 에 있는지 검증. 없으면 400 거부(어떤 모듈이 미허용인지 명시).
```

### 5.3 빌드 (W-b)
```
RunnerBuilder.Build:
 → 각 plugin 의 go.mod 를 사용자 입력이 아니라 allowed_modules 에서 생성
   (require 목록 = 그 stage 가 쓰는 허용 모듈들, 버전은 레지스트리 값)
 → 모든 stage 가 동일 버전을 참조하므로 batch-job go.mod 병합 시 충돌 없음
 → go mod tidy / go build 성공
```

### 5.4 버전업 (D4)
```
플랫폼(또는 사용자)이 allowed_modules.version 갱신 (uuid v1.6.0 → v1.7.0)
 → 다음 RunnerBuilder.Build 부터 모든 stage 가 v1.7.0 참조
 → 전 파이프라인 일괄 반영. 개별 고정 불가.
```

## 6. 구현 단계

의존: W-b←W-a. W-c(web-ui)는 W-a 후 병행.

### W-a. allowed_modules 레지스트리 (control-plane)
- `models.AllowedModule` + 마이그레이션(§4).
- CRUD API: 목록 조회 / 추가(GOPROXY @latest 조회 후 버전 고정) / 버전 갱신 / 삭제.
- 추가 시 GOPROXY 로 최신 버전 확인하는 헬퍼(`proxy.golang.org/{module}/@latest` 응답의 Version).
- **검증**: 모듈 추가 → 최신 버전 자동 고정 확인. 목록 조회.

### W-b. 빌드가 레지스트리 기반 go.mod 생성 (runner_builder.go)
- `pluginRequireBlock` / plugin 개별 go.mod 생성부(`:281`, `:494`)를 수정: 사용자 `p.GoMod` 대신 **allowed_modules 에서 require 블록 생성**. 각 stage 가 실제 import 하는 모듈만, 버전은 레지스트리 값.
- stage 가 쓰는 import 목록은 소스 파싱 또는 저장 시 기록한 것에서.
- **검증**: 서로 다른 stage 2개가 같은 모듈(uuid) 쓸 때, 동일 버전으로 병합돼 빌드 성공. (기존엔 다른 버전 넣으면 실패했음 — 회귀 대비.)

### W-c. web-ui 의존성 선택 UI (NativeStageEditor)
- go.mod 자유 입력 탭 → **허용 모듈 선택 목록**(체크박스 + 버전 표시, 읽기전용). "모듈 추가" 버튼(W-a 추가 API 호출).
- 저장 시 W-a 검증 API 로 import 유효성 확인.
- **검증**: 미허용 모듈 import 시 저장 거부 메시지. 허용 모듈 선택 후 빌드까지.

### W-d. import 검증 (control-plane, 저장 경로)
- plugin 저장(PUT) 시 소스의 import 문 파싱 → 표준 라이브러리 + plugin-sdk/pipeline-core + allowed_modules 외의 외부 import 있으면 거부.
- **검증**: 미등록 모듈 import 하는 stage 저장 → 400 + 어떤 모듈이 문제인지.

## 7. 이 방안이 문제를 푸는 근거 (자기검증)

- **충돌 원천 제거**: Go 충돌은 "같은 패키지 다른 버전"에서만 발생. 레지스트리가 module_path 당 버전 1개만 보유(§4) → 모든 stage 가 같은 버전 참조 → 병합 시 MVS 가 선택할 후보가 하나뿐 → **충돌 불가능**. (파이프라인별 빌드/파드 분리 불필요 — 글로벌 빌드 유지.)
- **전파 차단**: 버전 충돌로 인한 전체 빌드 실패가 사라짐. (단 stage **소스 자체의 컴파일 에러**는 여전히 전체 빌드에 전파 — 이건 별개 문제, §8.)
- **버전 일관성**: D4 로 전 파이프라인이 항상 동일 버전 → "어느 파이프라인이 어느 버전 쓰는지" 파편화 없음, 형상 단순.
- **보안 보너스**: 임의 패키지 import 불가 → 공급망 공격 표면 축소.

## 8. 범위 밖 / 남는 것

- **stage 소스 컴파일 에러의 전체 빌드 전파**: 의존성이 아니라 stage 코드 자체가 깨지면 글로벌 빌드가 실패해 남의 파이프라인도 영향. 이 문서 범위 아님(빌드 격리는 별도 트랙 — 파이프라인 단위 빌드는 사용자가 "비현실적"으로 판단, 후속 재검토).
- **버전 다운그레이드/롤백**, 모듈별 다중 버전 허용(major 분기): 초기 범위 아님.
- realtime 실행(agent 반영)은 [[REALTIME_NATIVE_IMPL_PLAN.md]] — 단 그 문서의 "바이너리 주입" 부분은 재검토 필요(별도).

## 9. 관련
- memory: `custom-stage-plugin-flow`(compile-in 아키텍처), `native-plugin-external-import`(uuid import e2e 실증 — 이 레지스트리의 실증 근거), `native-plugin-e2e-gaps`, `over-engineering-feedback`.
