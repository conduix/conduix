# Script Stage → JavaScript(goja) 전환 + 미구현 Go Native Stage 구현

## 목표
1. Starlark → JavaScript(goja)로 Script stage VM 교체
2. 미구현 Go native stage 9개 구현 (builtin으로 빠르게 처리)
3. GUI에서 Script(JavaScript) stage 선택 가능하도록 UI 업데이트
4. wf-enc-article-collect 마이그레이션

## 작업 순서

### Phase 1: Go Native Stage 구현 (미구현 9개)
Schema는 이미 stage_registry.go에 등록됨. Go 구현체 + NewStage factory 등록만 필요.

| # | Stage | 파일 | 핵심 로직 | 상태 |
|---|-------|------|-----------|------|
| 1 | **encrypt** | encrypt_stage.go | sha256/sha512/bcrypt/mask/aes256 | [x] |
| 2 | **drop** | drop_stage.go | 지정 필드 삭제 | [x] |
| 3 | **default** | default_stage.go | null/빈값에 기본값 설정 | [x] |
| 4 | **merge** | merge_stage.go | 여러 필드 → 하나 (delimiter/template) | [x] |
| 5 | **split** | split_stage.go | 정규식으로 필드 분리 | [x] |
| 6 | **cast** | cast_stage.go | 타입 변환 (int/float/bool/string/date) | [x] |
| 7 | **timestamp** | timestamp_stage.go | add/convert/format | [x] |
| 8 | **dedupe** | dedupe_stage.go | 키 기반 중복 제거 (window) | [x] |
| 9 | **throttle** | throttle_stage.go | token_bucket/sliding_window/fixed_window | [x] |

각 stage 패턴:
```go
type EncryptStage struct {
    BaseStage
    // config fields
}

func NewEncryptStage(name string, config map[string]any) (*EncryptStage, error) { ... }

func (s *EncryptStage) Process(ctx context.Context, record *Record) (*Record, error) { ... }
```

NewStage factory에 case 추가:
```go
case "encrypt":
    return NewEncryptStage(cfg.Name, cfg.Config)
```

### Phase 2: JavaScript(goja) Script Stage 구현

#### 2-1. goja 의존성 추가
```bash
cd pipeline-core && go get github.com/dop251/goja
```

#### 2-2. js_script_stage.go 신규 생성
- 기존 script_stage.go(Starlark) 구조 참고
- `sync.Pool`로 goja.Runtime 재사용
- timeout/context cancellation 동일 패턴
- Go에서 등록할 함수 (JS 표준에 없는 것만):
  - `console.log(level, msg)` → Go log 출력
  - `hash.sha256(s)` → hex string 반환
  - `base64.encode(s)`, `base64.decode(s)`

```go
type JSScriptStage struct {
    BaseStage
    code    string
    timeout time.Duration
    pool    sync.Pool
}
```

사용자 코드 형식:
```javascript
function process(record) {
    // JS 표준 내장 객체 모두 사용 가능
    record.name = record.name.trim().toUpperCase();
    record.tags = record.tags.map(t => t.toLowerCase());
    record.data = JSON.parse(record.raw_json);
    record.created = new Date().toISOString();

    // 조건 필터링
    if (record.status === "deleted") return null;  // drop

    return record;
}
```

#### 2-3. NewStage factory 등록
```go
case "js_script":
    return NewJSScriptStage(cfg.Name, cfg.Config)
```

#### 2-4. Stage Registry에 Schema 등록
```go
StageRegistry.Register(JSScriptStageSchema())
// CustomEditor: "JSScriptStageEditor"
```

### Phase 3: 기존 Starlark Script Stage 제거

#### 3-1. script_stage.go
- builtin 함수 12개 + 구현 코드 전부 삭제
- Starlark 의존성 제거 (`go.starlark.net`)
- 파일 자체 삭제

#### 3-2. script_stage_test.go
- 파일 삭제

#### 3-3. NewStage factory
- `case "script"` 제거 또는 → `NewJSScriptStage`로 연결 (호환성)

#### 3-4. stage_registry.go
- `ScriptStageSchema()` → `JSScriptStageSchema()`로 교체

#### 3-5. go.mod
```bash
cd pipeline-core && go mod tidy  # starlark 제거
```

### Phase 4: Web UI 업데이트

#### 4-1. JSScriptStageEditor 컴포넌트 생성
기존 ScriptStageEditor.tsx 기반으로:
- Monaco Editor language: `"python"` → `"javascript"`
- 기본 템플릿:
```javascript
function process(record) {
    // Transform each record. Return object to pass, null to drop.
    record.processed = true;
    return record;
}
```
- Builtin 함수 안내: `console.log()`, `hash.sha256()`, `base64.encode/decode()` 만
- 테스트 실행 기능 유지 (API endpoint 변경)

#### 4-2. 기존 ScriptStageEditor 삭제
- ScriptStageEditor/ 디렉토리 제거
- DynamicStageForm에서 참조 업데이트

#### 4-3. Stage 선택 GUI
Pipeline 편집 화면에서:
- Stage 타입 목록에 "JavaScript" 표시
- 아이콘: `"Javascript"` or `"Code"`
- 카테고리: Transform

### Phase 5: 마이그레이션

#### 5-1. wf-enc-article-collect (DB 마이그레이션 필요)
기존 Starlark script stage의 `hash_sha1` 사용 → encrypt stage(sha256) 로 교체.
DB 접속 후 pipelines_config의 script stage를 encrypt stage로 변경 필요.

```sql
-- wf-enc-article-collect의 pipelines_config에서
-- type: "script" (hash_sha1 사용) → type: "encrypt" (sha256)로 변경
-- 정확한 SQL은 현재 pipelines_config 값 확인 후 작성
```

#### 5-2. test-script API endpoint ✅ 완료
- control-plane의 `/plugins/test-script` → JS(goja) 실행으로 변경 완료 (Phase 3에서 처리됨)

### Phase 6: 테스트 & 검증

| 항목 | 명령어 |
|------|--------|
| Go native stage 테스트 | `cd pipeline-core && go test -v ./pkg/stream/...` |
| JS script stage 테스트 | `cd pipeline-core && go test -v -run TestJSScript ./pkg/stream/...` |
| control-plane 빌드 | `cd control-plane && go build ./...` |
| Web UI 빌드 | `cd web-ui && npm run build` |
| 전체 테스트 | `make test` |

## 파일 변경 목록

### 신규 생성
- `pipeline-core/pkg/stream/encrypt_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/drop_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/default_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/merge_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/split_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/cast_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/timestamp_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/dedupe_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/throttle_stage.go` + `_test.go`
- `pipeline-core/pkg/stream/js_script_stage.go` + `_test.go`
- `web-ui/src/components/JSScriptStageEditor/JSScriptStageEditor.tsx`

### 수정
- `pipeline-core/pkg/stream/stage.go` — NewStage에 9개 case 추가 + js_script
- `pipeline-core/pkg/stream/stage_registry.go` — ScriptStageSchema → JSScriptStageSchema
- `pipeline-core/go.mod` — goja 추가, starlark 제거
- `control-plane/internal/api/handlers/plugin_handler.go` — test-script endpoint JS 대응

### 삭제
- `pipeline-core/pkg/stream/script_stage.go`
- `pipeline-core/pkg/stream/script_stage_test.go`
- `web-ui/src/components/ScriptStageEditor/`

## 상태
- Phase 1: ✅ 완료 (Go Native Stage 9개 구현 + factory registry 리팩토링)
- Phase 2: ✅ 완료 (JavaScript goja Stage 구현, 15개 테스트 PASS + base64 builtin stage 추가)
- Phase 3: ✅ 완료 (Starlark 제거, go.starlark.net 의존성 제거, control-plane TestScript→JS 전환)
- Phase 4: ✅ 완료 (JSScriptStageEditor 생성, ScriptStageEditor 삭제, StageSchemaForm 참조 변경)
- Phase 5: ✅ 완료 (DB 마이그레이션: script→js_script+encrypt, encrypt에 sha1/target_field/truncate 추가)
- Phase 6: ✅ 완료 (pipeline-core, control-plane 테스트 PASS, web-ui 빌드 성공)
