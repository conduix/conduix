# Plugin Architecture V3 - Work Plan

## Phase 1: Plugin SDK + gRPC Interface ✅
- [x] proto/plugin/v1/plugin.proto 재작성 (ProcessBatch 기반)
- [x] plugin-sdk/ Go 모듈 생성
- [x] sdk.Serve() - go-plugin 서버 boilerplate
- [x] sdk.Record, sdk.Stage interface
- [x] 예제 플러그인 (example/)
- [x] 테스트 5/5 통과

## Phase 2: Builder Service ✅
- [x] control-plane/internal/builder/ 패키지 생성
- [x] 소스코드 → 바이너리 빌드 파이프라인 (go mod tidy + go build)
- [x] 금지 패키지 검사 (AST 파싱: os/exec, syscall, unsafe, plugin, CGO)
- [x] 빌드 타임아웃 (60초), 소스 크기 제한 (1MB)
- [x] 빌드 로그 수집
- [x] 소스코드 검증 API (package main, main() 함수 확인)
- [x] 테스트 6/6 통과

## Phase 3: 바이너리 저장소 + DB 모델 ✅
- [x] PluginBuild 모델 (빌드 이력: status, source_code, build_log, duration)
- [x] PluginBinary 모델 (바이너리 저장: LONGBLOB, checksum, platform)
- [x] DB 마이그레이션 (AutoMigrate에 추가)
- [ ] OCI Registry 구현 (oras-go) — 추후 구현

## Phase 4: Pipeline Runner 연동 ⬜
- [ ] go-plugin 기반 Plugin 로더
- [ ] gRPC 클라이언트 (ProcessBatch 호출)
- [ ] 바이너리 캐싱 (로컬 파일시스템)
- [ ] 프로세스 라이프사이클 (시작/중지/재시작)

## Phase 5: Control Plane API ✅
- [x] POST /api/v1/plugins/build (빌드 요청 — 비동기)
- [x] GET /api/v1/plugins/builds/:id (빌드 상태 조회)
- [x] GET /api/v1/plugins/:name/builds (빌드 이력)
- [x] GET /api/v1/plugins/:name/binary (바이너리 다운로드)
- [x] POST /api/v1/plugins/validate (소스코드 검증)
- [x] 기존 CRUD API 호환 유지
- [ ] GET /api/v1/plugins/builds/:id/logs (SSE 스트리밍) — 추후 구현

## Phase 6: Web UI ⬜
- [ ] Monaco Editor (Go 언어)
- [ ] go.mod 편집기
- [ ] 빌드 로그 실시간 표시 (SSE)
- [ ] Plugin 관리 페이지 개선
