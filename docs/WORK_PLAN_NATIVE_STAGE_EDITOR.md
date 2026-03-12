# Native Stage Web Editor — 설계 및 실행 계획

## 결론

**Monaco Editor + server-side gopls (LSP over WebSocket) + `go build` 테스트 + Runner 자동 빌드**

Go Playground와 동일한 패턴(서버 사이드 빌드/실행)을 기반으로, gopls LSP를 WebSocket으로 연결하여 IDE 수준 자동완성을 제공한다.

## 타 플랫폼 비교 결과

| 플랫폼 | 에디터 | 자동완성 | 테스트 | 배포 | Conduix 적용성 |
|--------|--------|---------|--------|------|---------------|
| **dbt Cloud** | Monaco/CodeMirror | Fusion LSP 기반 IntelliSense | 브라우저에서 SQL 즉시 실행 | Git 커밋 → CI/CD | ✅ LSP 패턴 참고 |
| **Databricks** | Monaco | AI 기반 Assistant Autocomplete | 셀 단위 실행 (클러스터 연결) | 노트북 즉시 실행 / Jobs | ✅ 서버 연결 컴퓨팅 |
| **NiFi 2.0** | 기본 textarea | 없음 | TestRunner API (로컬) | 디렉토리 배치 자동 감지 | ❌ 에디터 경험 부족 |
| **Airflow/Prefect/Dagster** | 웹 에디터 없음 | N/A | 로컬 IDE | CLI 배포 | ❌ 웹 편집 불가 |
| **Gitpod/Codespaces** | VS Code (웹) | 완전한 gopls | 환경 내 직접 실행 | 환경 내 빌드 | ❌ 사용자당 VM 비용 |
| **Theia** | Monaco + LSP 네이티브 | 완전한 gopls | 환경 내 직접 실행 | 환경 내 빌드 | ⚠ 과도한 프레임워크 |
| **Go Playground** | CodeMirror | 없음 | **서버 `go build` → 실행** | N/A | ✅ 빌드/실행 패턴 참고 |

**핵심 인사이트:**
- dbt Cloud와 Databricks가 가장 성공적인 웹 코드 에디터 — 둘 다 **서버 사이드 컴퓨팅 + LSP** 기반
- Go Playground 패턴(서버 빌드/실행)이 Conduix에 가장 적합
- Cloud IDE(Gitpod/Codespaces)는 사용자당 인프라 비용이 과도
- Airflow/Prefect/Dagster는 웹 코드 편집 자체를 제공하지 않음 — Conduix의 차별화 포인트

## 아키텍처

```
┌─ Browser ──────────────────────────────────────────────────┐
│                                                             │
│  Monaco Editor (language: "go")                             │
│    + monaco-languageclient (LSP over WebSocket)             │
│    + Go syntax highlighting, bracket matching               │
│    + 플러그인 인터페이스 코드 템플릿                          │
│                                                             │
│  WebSocket ────────────────────────────────────────────────  │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ Code Editor  │  │ Sample Data  │  │ Test Result      │  │
│  │ (main.go)    │  │ (JSON)       │  │ ✅ / ❌ + output │  │
│  │ (go.mod) tab │  │              │  │                  │  │
│  └──────────────┘  └──────────────┘  └──────────────────┘  │
│                                                             │
│  [▶ Test]  [Save] (테스트 성공해야 활성화)                    │
└────────────┬────────────────────────────────────────────────┘
             │
┌─ Control Plane ────────────────────────────────────────────┐
│                                                             │
│  ┌─ LSP Proxy ──────────────────────────────┐               │
│  │ WebSocket → gopls (사용자별 workspace)    │               │
│  │ /tmp/conduix-workspaces/{session}/        │               │
│  │   ├── go.mod (plugin-sdk 참조)           │               │
│  │   └── main.go (사용자 코드 sync)         │               │
│  └───────────────────────────────────────────┘              │
│                                                             │
│  ┌─ Test API ──────────────────────────────────┐            │
│  │ POST /api/v1/plugins/:name/test             │            │
│  │ 1. go/ast → 보안 검사 (금지 import 차단)    │            │
│  │ 2. go build -o /tmp/test-bin (타임아웃 60s) │            │
│  │ 3. 바이너리 실행 + sample_data 전달         │            │
│  │ 4. Process() 결과 수집 → JSON 응답         │            │
│  └─────────────────────────────────────────────┘           │
│                                                             │
│  ┌─ Save API ──────────────────────────────────┐            │
│  │ PUT /api/v1/plugins/:name                    │            │
│  │ 1. source_code, go_mod 저장                  │            │
│  │ 2. source_hash 계산                          │            │
│  │ 3. source_hash != deployed_hash → 빌드 트리거│            │
│  └─────────────────────────────────────────────┘           │
│                                                             │
│  ┌─ Runner Build (비동기) ─────────────────────┐            │
│  │ 1. 모든 native plugin 소스 수집              │            │
│  │ 2. registry_custom.go 자동 생성              │            │
│  │ 3. go build → Docker image → GHCR push      │            │
│  │ 4. RunnerVersion 생성 (status: ready)       │            │
│  │ 5. deployed_hash 갱신                        │            │
│  └─────────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────────┘
```

## 사용자 플로우

```
1. /plugins 페이지 → "Register Plugin" 클릭
2. Plugin 이름/설명 입력
3. Stage 추가 → "Native Stage" 선택
4. Monaco Editor 열림 (Go 코드 템플릿 + gopls 자동완성)
5. 코드 작성 → 실시간 에러 표시 (gopls diagnostics)
6. Sample Data 입력 (JSON)
7. [▶ Test] 클릭 → 서버에서 빌드+실행 → 결과 표시
   - 컴파일 에러 → 에러 위치 + 메시지 표시
   - 런타임 에러 → 에러 메시지 표시
   - 성공 → 변환 결과 JSON 표시
8. 테스트 성공 → [Save] 버튼 활성화
9. [Save] 클릭 → 소스 저장 → Runner 자동 빌드 트리거
10. 빌드 상태 표시: pending → building → ready
11. ready 상태 → 파이프라인에서 이 Stage 사용 가능
```

## 보안 정책

### 금지된 import (go/ast 검사)
```go
var blockedImports = []string{
    "os/exec",     // 시스템 명령 실행
    "syscall",     // 시스템 콜 직접 호출
    "unsafe",      // 메모리 직접 접근
    "plugin",      // Go plugin 로딩
    "debug/",      // 디버거 접근
    "runtime/cgo", // CGo 호출
    "C",           // CGo
}
```

### 실행 제한
- 빌드 타임아웃: 60초
- 실행 타임아웃: 10초 (테스트), 파이프라인 내 설정 가능
- 네트워크: 허용 (외부 API 호출이 필요한 use case 존재)
- 파일시스템: 읽기만 허용, 쓰기 금지

### go.mod 제한
- `go.mod`에서 사용자가 추가할 수 있는 의존성은 허용 (회사별 커스텀 모듈)
- `replace` directive는 시스템이 관리 (사용자 수정 불가)

## 플러그인 인터페이스 (plugin-sdk)

```go
// plugin-sdk/stage.go — 사용자가 구현할 인터페이스
package sdk

// NativeStage 커스텀 Stage 인터페이스
type NativeStage interface {
    // Init 초기화 (config 전달)
    Init(config map[string]any) error

    // Process 단일 레코드 처리. nil 반환 → 드롭
    Process(record map[string]any) (map[string]any, error)

    // Close 리소스 정리
    Close() error
}
```

### 코드 템플릿 (새 Plugin 생성 시 기본 제공)
```go
package main

import (
    sdk "github.com/conduix/conduix/plugin-sdk"
)

// MyStage 커스텀 Stage
type MyStage struct {
    // config 필드
}

func (s *MyStage) Init(config map[string]any) error {
    return nil
}

func (s *MyStage) Process(record map[string]any) (map[string]any, error) {
    // Transform each record. Return nil to drop.
    record["processed"] = true
    return record, nil
}

func (s *MyStage) Close() error {
    return nil
}

// Export — 반드시 이 변수를 선언해야 함
var Stage sdk.NativeStage = &MyStage{}
```

## GUI 설계

### NativeStageEditor 컴포넌트
```
┌──────────────────────────────────────────────────────────────┐
│ Native Stage: crm-enrichment                                  │
├──────────────────────────────────────────────────────────────┤
│                                                                │
│ [main.go]  [go.mod]   ← 탭 전환                               │
│ ┌────────────────────────────────────────────────────────────┐ │
│ │ Monaco Editor (Go, gopls 자동완성)                         │ │
│ │                                                            │ │
│ │  package main                                              │ │
│ │                                                            │ │
│ │  import (                                                  │ │
│ │      sdk "github.com/conduix/conduix/plugin-sdk"           │ │
│ │  )                                                         │ │
│ │                                                            │ │
│ │  type CRMStage struct {                                    │ │
│ │      apiURL string                                         │ │
│ │  }                                                         │ │
│ │  ...                                                       │ │
│ │                                                            │ │
│ │  300px height                                              │ │
│ └────────────────────────────────────────────────────────────┘ │
│                                                                │
│ ┌──── Test ──────────────────────────────────────────────────┐ │
│ │                                                            │ │
│ │  ┌─ Config (JSON) ──────┐  ┌─ Sample Data (JSON) ───────┐ │ │
│ │  │ {"api_url":"https:.."}│  │ [{"user_id":"u-123"}]      │ │ │
│ │  └──────────────────────┘  └─────────────────────────────┘ │ │
│ │                                                            │ │
│ │  ┌─ Result ────────────────────────────────────────────┐   │ │
│ │  │ ✅ Build: 3.2s | Exec: 12ms                        │   │ │
│ │  │ [{"user_id":"u-123","customer_grade":"gold"}]       │   │ │
│ │  └─────────────────────────────────────────────────────┘   │ │
│ │                                                            │ │
│ │  [▶ Test]                                                  │ │
│ └────────────────────────────────────────────────────────────┘ │
│                                                                │
│ Deploy Status: ⚠ Build needed (source modified)               │
│                                                                │
│ [Save] (disabled until test passes)                            │
└──────────────────────────────────────────────────────────────┘
```

## 실행 계획

### Phase 1: 기반 — Monaco Go Editor + 서버 빌드/테스트 API (MVP)
Plugin 에디터 GUI의 핵심 기능. gopls 없이 기본 Go 하이라이팅 + 서버 빌드/테스트.

| # | 작업 | 파일 | 설명 |
|---|------|------|------|
| 1 | Plugin 모델에 source_code, go_mod 필드 복원 | models.go | V3 정리 시 UI에서만 제거, DB 모델은 유지 중인지 확인 |
| 2 | Test API 구현 | plugin_handler.go | `POST /plugins/:name/test` — go build + 실행 + 결과 반환 |
| 3 | 보안 검사 유틸 | plugin_security.go | go/ast 기반 금지 import 검사 |
| 4 | NativeStageEditor 컴포넌트 | NativeStageEditor.tsx | Monaco (Go) + Config/SampleData/Result 패널 |
| 5 | Plugins 페이지에 에디터 통합 | Plugins.tsx | Register/Edit 시 Native Stage 에디터 표시 |
| 6 | Plugin 타입에 source_code, go_mod 필드 복원 | plugin.ts | UI 타입 업데이트 |
| 7 | pluginApi에 testPlugin API 추가 | pluginApi.ts | Test API 호출 |

### Phase 2: gopls 자동완성 (WebSocket LSP)
IDE 수준 Go 자동완성 제공.

| # | 작업 | 파일 | 설명 |
|---|------|------|------|
| 1 | LSP WebSocket Proxy | lsp_proxy.go | WebSocket → gopls stdio 중계 |
| 2 | 사용자별 workspace 관리 | workspace_manager.go | 임시 디렉토리 생성/정리, go.mod 템플릿 |
| 3 | monaco-languageclient 통합 | NativeStageEditor.tsx | LSP over WebSocket 연결 |
| 4 | gopls 설치/관리 | Dockerfile, Makefile | control-plane 이미지에 gopls 포함 |

### Phase 3: Runner 자동 빌드
코드 저장 시 Runner 이미지 자동 재빌드.

| # | 작업 | 파일 | 설명 |
|---|------|------|------|
| 1 | RunnerVersion 모델 활성화 | models.go | 빌드 버전 관리 |
| 2 | Builder Service 구현 | builder_service.go | registry_custom.go 생성 + go build + Docker push |
| 3 | 빌드 트리거 연동 | plugin_handler.go | Save 시 source_hash 비교 → 자동 빌드 |
| 4 | 빌드 상태 UI | Plugins.tsx | pending → building → ready 실시간 표시 |
| 5 | resolveRunnerImage 로직 | workflow_handler.go | 워크플로우 실행 전 빌드 상태 검증 |

### Phase 4: 테스트 강제 + UX 개선

| # | 작업 | 파일 | 설명 |
|---|------|------|------|
| 1 | 테스트 성공 기록 저장 | models.go | last_test_passed, last_test_at |
| 2 | Save 버튼 조건부 활성화 | NativeStageEditor.tsx | 테스트 미실행/실패 시 Save 비활성 |
| 3 | 코드 변경 감지 | NativeStageEditor.tsx | 테스트 후 코드 변경 시 Save 다시 비활성 |
| 4 | 빌드 로그 실시간 스트리밍 | Plugins.tsx | WebSocket 또는 SSE로 빌드 로그 표시 |
| 5 | go.mod 에디터 탭 | NativeStageEditor.tsx | 사용자 의존성 추가 가능 |

## 리스크 및 대응

| 리스크 | 영향 | 대응 |
|--------|------|------|
| gopls 메모리 (인스턴스당 200-500MB) | 동시 사용자 증가 시 메모리 부족 | Phase 2에서 도입, idle 세션 자동 종료 (5분) |
| gopls + monaco-languageclient 호환성 | 일부 LSP 기능 미작동 가능 | PoC 테스트 후 도입, Phase 1은 gopls 없이 동작 |
| go build 보안 (악성 코드 실행) | 서버 침해 | go/ast 사전 검사 + 빌드/실행 샌드박스 (nsjail) |
| 사용자별 workspace 파일시스템 | 디스크 사용량 증가 | 자동 정리 (세션 종료 후 삭제), /tmp 사용 |
| Runner 빌드 시간 (20-40초) | 사용자 대기 | 비동기 빌드 + 상태 폴링, Go 빌드 캐시 활용 |
| control-plane 이미지에 Go 컴파일러 포함 | 이미지 크기 증가 (~500MB) | Phase 3에서 별도 Builder Pod로 분리 가능 |

## 상태
- Phase 1: ✅ 완료
  - [x] 1. Plugin 모델 source_code, go_mod 필드 (DB에 이미 존재 확인)
  - [x] 2. Test API 구현 (plugin_handler.go - TestNativePlugin)
  - [x] 3. 보안 검사 유틸 (plugin_security.go + plugin_security_test.go - 4/4 테스트 통과)
  - [x] 4. NativeStageEditor 컴포넌트 (NativeStageEditor.tsx)
  - [x] 5. Plugins 페이지 에디터 통합 (Plugins.tsx - Native 토글 + 테스트 필수 Save)
  - [x] 6. Plugin 타입 source_code, go_mod 필드 복원 (plugin.ts)
  - [x] 7. pluginApi에 testNativePlugin API 추가 (pluginApi.ts)
  - [x] 8. routes.go에 /test-native 라우트 추가
  - [x] 9. UpdatePlugin에 source_code/go_mod/source_hash 저장 지원
  - [x] 10. i18n 키 추가 (en.json, ko.json)
  - [x] 11. Stage 유형 선택 GUI (None / Script JS / Native Go ToggleButtonGroup)
  - [x] 12. Script JS 에디터 Plugins 페이지 통합 (JSScriptStageEditor 재사용)
  - [x] 13. Dialog maxWidth lg로 확장 (에디터 공간 확보)
- Phase 2: ✅ 완료
  - [x] 1. gorilla/websocket 의존성 추가 (go.mod)
  - [x] 2. WorkspaceManager 구현 (lsp/workspace_manager.go — 사용자별 임시 디렉토리, go.mod 템플릿, idle 5분 자동 정리)
  - [x] 3. LSP WebSocket Proxy 구현 (lsp/proxy.go — WebSocket ↔ gopls stdio, Content-Length 기반 LSP 프로토콜)
  - [x] 4. LSP Handler + 라우트 추가 (lsp_handler.go, routes.go — GET /api/v1/lsp/go, POST /api/v1/lsp/sync)
  - [x] 5. 커스텀 LSP 클라이언트 구현 (lspClient.ts — WebSocket JSON-RPC 2.0, initialize/didOpen/didChange/completion/hover/diagnostics)
  - [x] 6. NativeStageEditor에 LSP 통합 (자동완성 CompletionProvider, Hover, Diagnostics 마커, gopls 연결 상태 칩)
  - [x] 7. Vite WebSocket proxy 설정 (vite.config.ts — ws: true)
  - 참고: gopls가 설치되지 않은 환경에서는 자동으로 기본 모드(하이라이팅만)로 동작 (graceful degradation)
- Phase 3: ✅ 완료
  - [x] 1. RunnerVersion 모델 활성화 (이미 구현됨: runner_builder.go, runner_handler.go)
  - [x] 2. Builder Service 구현 (이미 구현됨: builder/runner_builder.go)
  - [x] 3. 빌드 트리거 연동 (plugin_handler.go — UpdatePlugin에서 source_hash 변경 시 auto-build 비동기 실행)
  - [x] 4. 빌드 상태 UI (Plugins.tsx — Runner status 배너, Build Now 수동 빌드 버튼, Deploy 상태 열)
  - [x] 5. Runner API 프론트엔드 연동 (pluginApi.ts — getRunnerStatus, startRunnerBuild, getRunnerVersions)
  - [x] 6. 빌드 상태 폴링 (needs_build 시 5초마다 자동 갱신)
  - [x] 7. i18n 키 추가 (en.json, ko.json — build/deploy 관련 10개 키)
  - [x] 8. PluginHandler에 RunnerBuilder 주입 (builder 패키지 의존성 추가)
- Phase 4: ✅ 완료
  - [x] 1. 테스트 성공 기록 저장 (models.go — last_test_passed, last_test_at, last_test_error)
  - [x] 2. Save 버튼 조건부 활성화 (Phase 1에서 이미 구현됨 — testPassed state)
  - [x] 3. 코드 변경 감지 (Phase 1에서 이미 구현됨 — onTestResult/onTestPassed 코드 변경 시 false)
  - [x] 4. 빌드 로그 표시 (Plugins.tsx — Runner Build History 다이얼로그, 빌드 이력/로그/에러 표시)
  - [x] 5. go.mod 에디터 탭 (Phase 1에서 이미 구현됨 — NativeStageEditor 탭)
  - [x] 6. TestNativePlugin/TestScript에서 plugin_name으로 테스트 결과 DB 기록
  - [x] 7. Plugin 목록에 Test 상태 열 + 플러그인 확장 영역에 마지막 테스트 결과 표시
  - [x] 8. Runner 배너에 Logs 버튼 추가
  - [x] 9. auto-build trigger에 "auto" 트리거 타입 기록
  - [x] 10. i18n 키 추가 (en.json, ko.json — test/build 관련 8개 키)
