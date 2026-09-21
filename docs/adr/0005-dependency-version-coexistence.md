# ADR-0005: 커스텀 stage 의존성 버전 공존 (stage 별 고정 + 비기본 버전 fork 링크)

- **Status**: Accepted (2026-09-21). W1~W6 구현 완료. 로컬 K8s 실동작(e2e §5)은 미완 — 아래 "구현 결과" 참조.
- **대체**: [archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md](../archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md) 의 **D4** ("버전업은 전역 일괄, 개별 고정 불가")를 부분 대체. D1·D2·D3·D5 는 유지.
- **근거 수준**: 기법 자체는 Go 1.27.1 로컬 실험으로 검증(CONFLICT.md §5). 운영 규모의 바이너리 크기·빌드 시간 영향은 미측정.

## Context

커스텀 stage 는 단일 바이너리에 compile-in 되고, 그 바이너리 하나를 batch Job 과 cluster 당 하나의
realtime 파드가 공유한다. 2026-07 의 단일 버전 레지스트리(D1~D5)는 "사용자가 제각각 입력한 버전" 으로
인한 충돌을 없앴지만, **시간이 흐르며 생기는 API 비호환**은 "admin 이 버전을 올리는 순간 옛 stage 가
일괄 파손" 으로 바뀌어 남았다. 파손 시 리졸버가 활성 native 전체를 pending 으로 보기 때문에
**모든 워크플로의 새 실행이 막힌다**(CONFLICT.md §3).

## Decision

1. **레지스트리는 모듈당 여러 버전을 보유**하고 하나를 기본(default)으로 둔다.
2. **stage 는 자기 의존성 버전을 고정**한다(`plugins.dep_versions`). 처음 저장 시 그 시점의 기본
   버전으로 채워지고, 소유자가 명시적으로 올리기 전까지 유지된다. admin 의 기본 버전 변경은
   **기존 stage 를 건드리지 않는다.**
3. **빌더는 고정값을 존중**한다. 기본과 같은 버전은 원래 경로로, 다른 버전은 해당 소스를
   `forked/<경로>@<버전>` 으로 복사하고 복사본 내부 import 와 stage 의 import 를 그 경로로
   재작성해 **같은 바이너리**에 링크한다. Go 는 import 경로가 다르면 별개 패키지로 취급한다.
4. init 시점에 전역 등록을 하는 모듈(`database/sql` 드라이버 등)은 두 버전을 링크할 수 없다.
   빌드 후 init 자가점검으로 잡고, 해당 모듈은 `single_version_only` 로 표시해 현행 단일 버전
   규칙에 남긴다.
5. 바이너리 수·파드 수·배포 경로는 바꾸지 않는다.

## Alternatives

- **완화만**(영향 분석·드라이런·실패 stage 제외): 파손 자체를 없애지 못함. 일부 요소는 재사용.
- **파티션/파이프라인 단위 빌드**: 2026-07-10 사용자 기각("비현실적"). 바이너리를 나누면 realtime
  파드도 나눠야 하며 2026-09-15 에 되돌린 파드 폭증 구조로 회귀.
- **프로세스 분리(go-plugin/gRPC)**: 레코드 단위 호출에 IPC 비용. 신뢰할 수 없는 코드 격리라는
  다른 문제의 해법(ADR-0003 재검토 트리거). 벤치마크 없음.
- **import alias**: 경로가 아니라 이름일 뿐. 실험 1 로 기각.

## Consequences

**긍정**
- admin 의 버전 갱신이 남의 stage 를 깨는 경로가 사라진다. "볼모" 문제 소멸.
- 모든 stage 가 기본 버전이면 산출물은 현행과 동일 — 회귀 안전선.
- 되돌리기는 `dep_versions` 를 기본으로 맞추는 데이터 조작만으로 가능.

**부정 / 트레이드오프**
- Go 가 지원하지 않는 다중 버전 링크를 플랫폼이 자동 fork 로 우회한다. 재작성기·자가점검을
  플랫폼이 유지해야 한다.
- fork 복사본은 `govulncheck`·`go mod verify` 스캔 밖. 별도 점검 필요.
- 간접 의존성은 MVS 로 한 버전에 모인다(Go 의 같은 메이저 호환 가정에 의존).
- 갈라진 버전 수만큼 바이너리가 커진다. 수렴(소유자의 명시적 업그레이드)이 안 되면 누적.
- 문자열·`go:linkname` 으로 자기 경로를 참조하는 코드는 재작성 밖(드묾).

## 재검토 트리거

- 갈라진 버전 수가 지속 증가하고 소유자 업그레이드가 일어나지 않을 때 → 강제 수렴 정책 검토.
- `single_version_only` 모듈의 버전 갱신이 반복적으로 stage 를 깨뜨릴 때 → 그 부류에 한해 프로세스 분리 재검토(벤치마크 선행).

## 구현 결과 (2026-09-21)

브랜치 `feat/dep-version-coexistence`, 커밋 6개(W1~W6). 계획·인수인계는
[plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md](../plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md) §8.

| Decision | 구현 |
|---|---|
| 1. 모듈당 여러 버전 보유 | `allowed_module_versions` 테이블 + `/module-versions` API. 기존 행은 멱등 백필 |
| 2. stage 가 자기 버전 고정 | `plugins.dep_versions`. 저장 시 `dependency.ResolvePins` 가 기존 고정은 유지하고 새 import 만 기본 버전으로. 레거시(빈 값)는 빌드 성공 후 백필 |
| 3. 빌더가 고정값 존중 + fork 링크 | `builder.materializeForks` — `go mod download` → 복사 → 쓰기권한 → `RewriteModuleTree`(복사본 자기참조 + go.mod module 줄) → stage import 재작성(alias 자동) |
| 4. init 자가점검 | `CONDUIX_INIT_CHECK=1` 로 빌드된 러너를 한 번 실행. fork 가 있을 때만. `single_version_only` 는 저장 시점과 빌드 시점 양쪽에서 거부 |
| 5. 바이너리·파드·배포 경로 불변 | 건드리지 않음. fork 가 없으면 이전과 동일한 산출물 |

**검증된 것**
- 같은 모듈의 두 버전이 한 바이너리에서 각기 다른 값을 반환 — 실제 `go build` + 실행
  (`builder/fork_integration_test.go`, `//go:build integration`). `RewriteModuleTree` 를 빼면
  이 테스트가 실패하는 것까지 확인했다(자기참조 미재작성 함정의 회귀선).
- 중복 `init()` 등록이 빌드는 통과하고 init 에서 panic 하는 것 — 같은 파일의 통합 테스트.
- 고정값이 하나도 없으면 결합 해시가 이 기능 도입 전과 같다 — 전면 재빌드가 일어나지 않는다.
- 배포 검증: control-plane 을 이 브랜치 이미지로 교체해 마이그레이션 성공,
  신규 라우트 3종이 401(등록됨)·없는 경로는 404 로 대조 확인.

**아직 검증 안 된 것**
- §5 e2e(실제 stage 두 개를 다른 버전으로 고정해 빌드·실행). 인증 토큰이 필요해 보류.
- 운영 규모의 바이너리 크기·빌드 시간 영향(ADR 작성 시점부터 미측정).
- fork 복사본에 대한 `govulncheck`·`go mod verify` 대체 점검 수단.

## Evidence

- `docs/CUSTOM_STAGE_DEPENDENCY_CONFLICT.md` §5 — 실험 6종(alias/경로/replace 관용/자기참조 함정/재작성/모듈 캐시).
- `docs/plans/CUSTOM_STAGE_DEP_VERSION_COEXIST_PLAN.md` — 구현 계획(W1~W7).
- 현행 코드: `runner_builder.go:683,702,721`(go.mod 생성 3종), `stage_import_validation.go:153`, `workspace_manager.go:210`, `runner_resolver.go:170`(전체 pending 판정), `module_handler.go:112`(무검증 PUT).
