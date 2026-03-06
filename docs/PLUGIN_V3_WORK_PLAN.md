# Plugin Architecture V3 - Work Plan

## Phase 1: Plugin SDK + gRPC Interface ✅
- [x] proto/plugin/v1/plugin.proto 재작성 (ProcessBatch 기반)
- [x] plugin-sdk/ Go 모듈 생성
- [x] sdk.Serve() - go-plugin 서버 boilerplate
- [x] sdk.Record, sdk.Stage interface
- [x] 예제 플러그인 (example/)
- [x] 테스트 5/5 통과

## Phase 2: Builder Service ⬜
- [ ] builder/ Go 모듈 생성
- [ ] 소스코드 → 바이너리 빌드 파이프라인
- [ ] 금지 패키지 검사 (AST 파싱)
- [ ] 빌드 타임아웃, 리소스 제한
- [ ] 빌드 로그 수집
- [ ] gRPC 또는 HTTP API

## Phase 3: 바이너리 저장소 ⬜
- [ ] StorageBackend 인터페이스
- [ ] MySQL BLOB 구현 (plugin_binaries 테이블)
- [ ] OCI Registry 구현 (oras-go)
- [ ] plugin_builds 테이블 (빌드 이력)
- [ ] DB 마이그레이션

## Phase 4: Pipeline Runner 연동 ⬜
- [ ] go-plugin 기반 Plugin 로더
- [ ] gRPC 클라이언트 (ProcessBatch 호출)
- [ ] 바이너리 캐싱 (로컬 파일시스템)
- [ ] 프로세스 라이프사이클 (시작/중지/재시작)

## Phase 5: Control Plane API ⬜
- [ ] POST /api/v1/plugins/build (빌드 요청)
- [ ] GET /api/v1/plugins/build/:id/logs (SSE 스트리밍)
- [ ] GET /api/v1/plugins/:name/binary (바이너리 다운로드)
- [ ] 기존 CRUD API와 연동

## Phase 6: Web UI ⬜
- [ ] Monaco Editor (Go 언어)
- [ ] go.mod 편집기
- [ ] 빌드 로그 실시간 표시 (SSE)
- [ ] Plugin 관리 페이지 개선
