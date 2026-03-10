# Plugin V4 Phase 2: Native Stage + RunnerVersion

## 작업 항목

### 2-1. 데이터 모델 (control-plane) — ✅ 완료
- [x] `models/models.go` — RunnerVersion 모델 추가
- [x] `models/models.go` — Plugin 모델에 Type, SourceCode, GoMod, SourceHash, DeployedHash, RunnerVersionID 필드 추가
- [x] `database/database.go` — AutoMigrate에 RunnerVersion 추가

### 2-2. plugin-sdk 인터페이스 (plugin-sdk) — ✅ 완료
- [x] `plugin-sdk/native_stage.go` — NativeStage 인터페이스 + BaseNativeStage
- [x] `pipeline-core/pkg/stream/native_stage_adapter.go` — NativeStageAdapter + RegisterCustomStage 레지스트리
- [x] `pipeline-core/pkg/stream/stage.go` — NewStage() default에 커스텀 스테이지 레지스트리 조회 추가

### 2-3. Runner 빌드 시스템 (control-plane) — 🔲 Phase 3에서 구현
- [ ] `internal/builder/runner_builder.go` — RunnerBuilder
  - [ ] 소스 배치 + registry_custom.go 자동 생성
  - [ ] go.mod local replace directive
  - [ ] go build + Docker build & push
  - [ ] SourceHash 결합 해시 계산

### 2-4. 실행 제어 (control-plane) — ✅ 완료
- [x] `internal/services/runner_resolver.go` — resolveRunnerImage()
- [x] `internal/api/handlers/runner_handler.go` — Runner API
  - [x] GET /api/v1/runner/versions — 버전 목록
  - [x] GET /api/v1/runner/versions/:id — 버전 상세
  - [x] GET /api/v1/runner/status — 배포 상태 확인
  - [x] POST /api/v1/runner/resolve — Runner 이미지 결정

### 2-5. API 라우트 — ✅ 완료
- [x] `internal/api/routes.go` — runner 라우트 추가

### 2-6. 테스트 — ✅ 완료
- [x] `pipeline-core/pkg/stream/native_stage_adapter_test.go` — 8개 테스트 (PASS)
  - NativeStageAdapter Process/Drop/Error/InitError/Close/Stats/ContextCancelled
  - RegisterCustomStage 레지스트리
- [x] `control-plane/internal/services/runner_resolver_test.go` — 7개 테스트 (PASS)
  - extractStageTypes Basic/Empty/InvalidJSON/NoStages/Dedup
  - BuildRequiredError/DefaultRunnerImage

## 구현 순서
1. ~~데이터 모델 (RunnerVersion, Plugin 확장)~~ ✅
2. ~~plugin-sdk NativeStage 인터페이스~~ ✅
3. ~~resolveRunnerImage 로직~~ ✅
4. ~~Runner API (handler + routes)~~ ✅
5. ~~테스트~~ ✅

## 구현 파일 요약

**plugin-sdk (Go)**
- `plugin-sdk/native_stage.go` — NativeStage 인터페이스, BaseNativeStage

**pipeline-core (Go)**
- `pipeline-core/pkg/stream/native_stage_adapter.go` — NativeStageAdapter, RegisterCustomStage
- `pipeline-core/pkg/stream/native_stage_adapter_test.go` — 8개 테스트
- `pipeline-core/pkg/stream/stage.go` — NewStage() 커스텀 스테이지 fallback 추가

**control-plane (Go)**
- `control-plane/pkg/models/models.go` — RunnerVersion 모델, Plugin 모델 확장
- `control-plane/pkg/database/database.go` — AutoMigrate에 RunnerVersion 추가
- `control-plane/internal/services/runner_resolver.go` — RunnerResolver, BuildRequiredError
- `control-plane/internal/services/runner_resolver_test.go` — 7개 테스트
- `control-plane/internal/api/handlers/runner_handler.go` — RunnerHandler (4 endpoints)
- `control-plane/internal/api/routes.go` — runner 라우트 그룹 추가
