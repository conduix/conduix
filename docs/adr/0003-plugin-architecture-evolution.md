# ADR-0003: 플러그인 아키텍처 진화 (V2 → V3 → V4)

- **Status**: Accepted (V4 현행). V2·V3는 Superseded.
- **결정 커밋**: `aed9bae`(gRPC go-plugin 제거), `6099d12`(Starlark→JavaScript + native stages), `aae382e`(V2/V3 UI 제거)
- **근거 수준**: V2→V3 전환 근거는 **문서화됨**(`archive/PLUGIN_ARCHITECTURE_V3.md`). V3→V4 폐기 근거는 **커밋 메시지 수준**(성능·배포 단순화)만 있고 측정 자료는 없음.

## Context (문제)

**사용자(다른 회사)가 각자 커스텀 변환 로직을 넣어야 한다.** 즉 플러그인은 이 플랫폼의 핵심 확장점이다. 요구:
- 회사별 커스텀 모듈 import 가능해야 함(임의 외부 Go 모듈).
- 네이티브에 가까운 성능(느린 스크립트 인터프리터만으론 "플랫폼 쓸 이유"가 없음).
- 배포·버전 관리가 감당 가능해야 함.

이 요구를 만족시키는 방법을 세 번 바꿨다.

## Decision (선택) — 버전별 변천

### V2: Docker 컨테이너 (폐기)
- Stage 하나 = 컨테이너 하나, stdin/stdout JSONL 프로토콜.
- **폐기 이유**(`archive/PLUGIN_ARCHITECTURE_V3.md`에 명시):
  - Docker 컨테이너: stage당 컨테이너 → **과도한 오버헤드**.
  - Wasm 대안: **외부 모듈 import 불가** → 회사별 커스텀 모듈 요구 위반.
  - Lua/스크립트 대안: 네이티브 대비 **10~100배 느림** → 플랫폼 이점 상실.

### V3: HashiCorp go-plugin + gRPC (폐기)
- 플러그인을 별도 바이너리로 빌드, gRPC over unix socket으로 통신. 바이너리는 MySQL BLOB 또는 OCI Registry에 저장. 배치(1000건) gRPC 호출.
- **폐기 이유**(커밋 `aed9bae` 메시지): "V4는 native plugin을 runner 이미지에 직접 컴파일해 별도 바이너리와 gRPC 통신을 제거한다."
  - 즉 **프로세스 분리 오버헤드 + 바이너리 저장소/버전 관리 복잡성**을 없애려는 것.
  - **주의**: gRPC를 버린 정량적 근거(벤치마크 등)는 기록되지 않음. 커밋 메시지의 "제거한다"는 방향성만 있음.

### V4: 인프로세스 하이브리드 (현행)
사용자 확장을 두 tier로, 그 아래 built-in을 합쳐 사실상 3계층:

| 계층 | 무엇 | 빌드 | 성능 | 용도 |
|------|------|------|------|------|
| **Built-in** | 21개 등록 stage(filter/remap/js_script 등) | 컴파일됨 | 최고 | 표준 변환 |
| **Tier 1: Script** | JavaScript(goja) `js_script` stage | **없음**(저장→재시작) | remap 대비 5~10배 느림 | 로직 있는 변환, 빠른 반복(~80% 케이스) |
| **Tier 2: Native** | 사용자 Go 코드 | runner 이미지 재빌드(single image) | 네이티브 | 고성능·외부 SDK·회사 모듈 |

- native plugin은 별도 바이너리·gRPC 없이 **runner 이미지(pipeline-batch-job) 하나에 전부 컴파일**되어 인프로세스 실행.
- 배포 일관성: `RunnerVersion` + `SourceHash` vs `DeployedHash` 검증(불일치 시 실행 거부).
- 초기 V4 문서는 스크립트 tier로 **Starlark**를 계획했으나, 커밋 `6099d12`에서 **JavaScript(goja)로 교체** + native Go stage 10종 추가.

> 문서 제목은 "2-Tier Hybrid"(사용자 확장 관점: Script+Native). Built-in까지 세면 3계층.

## Alternatives (대안)

- **컨테이너/Wasm/Lua**(V2 대안들) — 오버헤드 또는 import 불가 또는 성능으로 모두 거부.
- **gRPC go-plugin 유지**(V3) — 프로세스 격리·플러그인별 독립 버전이라는 장점이 있으나, 배포 복잡성·프로세스 오버헤드로 폐기.
- **인프로세스 단일 이미지**(V4) — 채택.

## Consequences (결과·트레이드오프)

**긍정:**
- **빠른 반복**: js_script는 빌드 0 → GUI에서 작성·테스트·저장→재시작만으로 반영.
- **네이티브 성능**: native plugin은 인프로세스라 gRPC 왕복 없음.
- **배포 단순화**: 플러그인별 이미지·바이너리 저장소 없이 runner 이미지 1개.
- **일관성**: RunnerVersion/해시 검증으로 "빌드된 것 == 실행되는 것" 보장.

**부정 / 트레이드오프:**
- **장애 격리 약화**: V3는 플러그인 프로세스가 죽어도 runner 유지. V4는 인프로세스라 native plugin panic이 runner에 영향. → panic recover로 완화하나 프로세스 격리만큼 강하진 않음.
- **native 배포 지연**: native plugin 반영에 runner 이미지 재빌드(수십 초) 필요. (그래서 빠른 반복은 js_script로 유도.)
- **메모리 격리 없음**: 같은 프로세스에서 실행 → 신뢰할 수 없는 코드에는 부적합(사용자는 "자기 회사 코드"를 넣는 전제).
- **gRPC 폐기 근거 미측정**: 성능·오버헤드 주장은 정성적. 재도입 판단이 필요하면 측정 필요.

## 재검토 트리거

- 신뢰할 수 없는 3rd-party 플러그인을 실행해야 한다면 → 프로세스/메모리 격리(V3 방식 또는 Wasm) 재검토.
- native 재빌드 지연이 운영에 병목이면 → 플러그인 hot-load 방안 재검토.

## Evidence (근거)

- `docs/PLUGIN_ARCHITECTURE_V4.md` — 현행 V4 설계 상세.
- `docs/archive/PLUGIN_ARCHITECTURE_PLAN_V2.md`, `archive/PLUGIN_ARCHITECTURE_V3.md` — 폐기된 V2/V3 설계 및 V2 폐기 근거.
- 커밋: `aed9bae`(go-plugin gRPC 제거), `6099d12`(Starlark→JS + native 10종), `aae382e`/`fd20890`(V2/V3 UI 제거), `7de3c1c`(RunnerBuilder), `1e0f3c8`(stage revision/build tracking).
- 구현: `pipeline-core/pkg/stream/js_script_stage.go`(Tier 1), `native_stage_adapter.go`·`stage_registry.go`(Tier 2 + built-in), `control-plane/internal/builder/runner_builder.go`(native 빌드).
- **기록 없음**: V3 gRPC 폐기의 정량 근거(벤치마크). 커밋 메시지의 방향성 서술만 존재.
