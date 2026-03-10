# Plugin V4 구현 진행 상황

## Phase 1: Script Stage (Tier 1)

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
- [ ] web-ui: Script Stage 에디터 (Monaco + Python 하이라이트 + 테스트 UI)

### 테스트 커버리지

**pipeline-core (16 tests, all PASS)**
- BasicProcess, NoneDropsRecord, Timeout, CompileError
- MissingProcessFunc, MissingCode, RuntimeError (passthrough)
- BuiltinHashSHA256, BuiltinBase64, BuiltinJSON, BuiltinRegex, BuiltinTimestamp
- ContextCancellation, Stats, NewStageFactory, RegistryHasScript

**control-plane (3 tests, all PASS)**
- TestTestScript_Success, TestTestScript_Drop, TestTestScript_CompileError

### 주요 구현 노트
- Starlark `ExecFileOptions`에 `&syntax.FileOptions{}` 전달 필수 (nil → panic)
- Starlark에서 `**` 연산자 미지원 (Python과 다름)
- 메모리 제한: `thread.SetMaxAllocs` 미지원 → 타임아웃으로 대체

### API 엔드포인트
- `POST /api/v1/plugins/test-script` — Starlark 스크립트 테스트 실행
  - Request: `{ code, timeout?, sample_data }`
  - Response: `{ success, output?, dropped, error?, elapsed }`
