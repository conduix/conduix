# Plugin V4 구현 진행 상황

## Phase 1: Script Stage (Tier 1) — 완료

### 작업 항목
- [x] `go.starlark.net` 의존성 추가 (pipeline-core/go.mod)
- [x] `pipeline-core/pkg/stream/script_stage.go` — Starlark 실행 엔진
  - [x] process(record) 함수 호출 규약
  - [x] 내장 함수 (hash_sha256, base64, json, regex, timestamp, log)
  - [x] 타임아웃 (sync.Pool + goroutine + select)
  - [x] None 반환 시 레코드 드롭
- [x] `pipeline-core/pkg/stream/stage.go` NewStage()에 "script" case 추가
- [x] `pipeline-core/pkg/stream/stage_registry.go` init()에 ScriptStageSchema 등록
- [x] `pipeline-core/pkg/stream/script_stage_test.go` — 16개 단위 테스트 (전부 PASS)
- [x] control-plane: `POST /api/v1/plugins/test-script` Script 테스트 API (3개 테스트 PASS)
- [x] web-ui: ScriptStageEditor (Monaco + Python 하이라이트 + 테스트 UI)

### 구현 파일

**Backend (Go)**
- `pipeline-core/pkg/stream/script_stage.go` — Starlark 실행 엔진
- `pipeline-core/pkg/stream/script_stage_test.go` — 16개 테스트
- `pipeline-core/pkg/stream/stage.go` — NewStage() "script" case
- `pipeline-core/pkg/stream/stage_registry.go` — ScriptStageSchema (CustomEditor)
- `control-plane/internal/api/handlers/plugin_handler.go` — TestScript API
- `control-plane/internal/api/handlers/plugin_handler_test.go` — 3개 테스트
- `control-plane/internal/api/routes.go` — POST /plugins/test-script

**Frontend (React)**
- `web-ui/src/components/ScriptStageEditor/ScriptStageEditor.tsx` — 전용 에디터
- `web-ui/src/components/StageSchemaForm/StageSchemaForm.tsx` — customEditors 등록
- `web-ui/src/services/pluginApi.ts` — testScript API 함수

### 주요 구현 노트
- Starlark `ExecFileOptions`에 `&syntax.FileOptions{}` 전달 필수 (nil → panic)
- Starlark에서 `**` 연산자 미지원 (Python과 다름)
- 메모리 제한: `thread.SetMaxAllocs` 미지원 → 타임아웃으로 대체
- Schema에서 CustomEditor="ScriptStageEditor" 사용 (Fields 비움, 에디터가 전체 폼 담당)

## Phase 2: RunnerVersion + NativeStage — 완료
- 상세: [WORK_PROGRESS_PHASE2.md](WORK_PROGRESS_PHASE2.md)

## Phase 3: Runner 빌드 시스템 — 완료
- 상세: [WORK_PROGRESS_PHASE3.md](WORK_PROGRESS_PHASE3.md)

## Phase 4: GUI 통합 — 완료
- 상세: [WORK_PROGRESS_PHASE4.md](WORK_PROGRESS_PHASE4.md)

## Phase 5: Stage Revision + Build History — 완료
- 상세: [WORK_PROGRESS_PHASE5.md](WORK_PROGRESS_PHASE5.md)

## Phase 6: go-plugin cleanup — 미구현
- [ ] plugin_stage.go (gRPC 방식) 제거
- [ ] plugin-sdk의 gRPC 관련 코드 제거 (NativeStage 인터페이스만 유지)
- [ ] proto/ gRPC 정의 정리
- [ ] PluginBinary DB 모델 제거
