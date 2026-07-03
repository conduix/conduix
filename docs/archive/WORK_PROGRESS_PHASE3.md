# Plugin V4 Phase 3: Runner 빌드 시스템 — ✅ 완료

## 작업 항목

### 3-1. RunnerBuilder (control-plane) — ✅ 완료
- [x] `internal/builder/runner_builder.go` — RunnerBuilder
  - [x] 모든 native plugin 소스를 임시 디렉토리에 배치
  - [x] registry_custom.go 자동 생성 (import + RegisterCustomStage)
  - [x] go.mod local replace directive 추가
  - [x] CombinedSourceHash 결합 해시 계산 (정렬 기반, 결정적)
  - [x] go build (CGO_ENABLED=0)
  - [x] Docker build & push (DockerPush 설정으로 제어)
  - [x] 빌드 성공 시 Plugin.DeployedHash 갱신
  - [x] 빌드 중 수정 감지 (PluginHashes 스냅샷 비교)
  - [x] 동일 해시 ready 버전 재사용 (빌드 스킵)
  - [x] 빌드 중복 방지 (building 상태 확인)

### 3-2. Runner Build API — ✅ 완료
- [x] `internal/api/handlers/runner_handler.go` — POST /api/v1/runner/build 추가
  - [x] 비동기 빌드 (goroutine, HTTP context와 분리)
  - [x] 202 Accepted 즉시 응답
- [x] `internal/api/routes.go` — build 라우트 추가

### 3-3. 테스트 — ✅ 완료 (10개 PASS)
- [x] CombinedSourceHash (Deterministic, OrderIndependent, DifferentHashes)
- [x] GenerateRegistryCustom (import, RegisterCustomStage, NewNativeStageAdapter)
- [x] GenerateRunnerGoMod (module, go version, dependencies, replace directives)
- [x] SanitizeName (hyphen→underscore, space→underscore, lowercase)
- [x] RunnerParsePlatform (linux/arm64, empty, single)
- [x] ExtractPluginIDs
- [x] DefaultRunnerBuilderConfig

## 구현 파일

- `control-plane/internal/builder/runner_builder.go` — RunnerBuilder 핵심 로직
- `control-plane/internal/builder/runner_builder_test.go` — 10개 테스트
- `control-plane/internal/api/handlers/runner_handler.go` — StartBuild endpoint 추가
- `control-plane/internal/api/routes.go` — POST /runner/build 라우트
