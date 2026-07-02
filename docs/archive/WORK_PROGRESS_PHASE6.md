# Plugin V4 Phase 6: go-plugin gRPC Cleanup

## 목표
HashiCorp go-plugin 기반 gRPC 방식을 제거하고 NativeStage 인터페이스만 유지.
V4에서는 native plugin이 Runner 이미지에 직접 빌드되므로 별도 바이너리/gRPC 불필요.

## 삭제 대상

### 파일 삭제
- [x] `pipeline-core/pkg/stream/plugin_stage.go` — gRPC PluginStage
- [x] `pipeline-core/pkg/stream/plugin_stage_test.go` — 해당 테스트
- [x] `plugin-sdk/sdk.go` — go-plugin Serve/Handshake
- [x] `plugin-sdk/grpc_server.go` — gRPC 서버 구현
- [x] `plugin-sdk/grpc_client.go` — gRPC 클라이언트 구현
- [x] `plugin-sdk/sdk_test.go` — SDK 테스트
- [x] `plugin-sdk/example/` — 예제 플러그인
- [x] `plugin-sdk/proto/` — 생성된 protobuf 코드
- [x] `proto/` — protobuf 정의 (.proto 파일)
- [x] `control-plane/internal/builder/builder.go` — V3 빌더
- [x] `control-plane/internal/builder/builder_test.go` — V3 빌더 테스트

### 코드 수정
- [x] `pipeline-core/pkg/stream/stage.go` — `case "plugin"` 제거
- [x] `pipeline-core/go.mod` — `hashicorp/go-plugin` 의존성 제거
- [x] `plugin-sdk/go.mod` — gRPC/protobuf 의존성 제거 (NativeStage만 유지)
- [x] `control-plane/pkg/models/models.go` — PluginBinary 모델 제거
- [x] `control-plane/pkg/database/database.go` — PluginBinary AutoMigrate 제거
- [x] `control-plane/internal/api/handlers/plugin_handler.go` — BuildPlugin, executeBuild, GetBinary, GetBuild, ListBuilds, ValidatePluginSource 제거
- [x] `control-plane/internal/api/routes.go` — V3 라우트 5개 제거 (/plugins/build, /plugins/validate, /plugins/builds/:id, /:name/builds, /:name/binary)
- [x] `control-plane/internal/builder/runner_builder_test.go` — contains 헬퍼 함수 추가 (builder_test.go 삭제로 인한 의존성 수정)
- [x] go mod tidy — 모든 모듈 (plugin-sdk, pipeline-core, control-plane, pipeline-agent)

### 유지 대상 (변경하지 않음)
- `plugin-sdk/native_stage.go` — NativeStage 인터페이스
- `plugin_handler.go` CRUD, TestScript, Revision 함수
- Plugin, PluginStage, PluginBuild 모델 (이력 보존)
- RunnerBuilder (V4 빌드 시스템)
- Frontend Plugin 관련 코드

## 검증 결과
- [x] 모든 모듈 빌드 성공 (control-plane, pipeline-core, pipeline-agent)
- [x] 모든 테스트 통과 (handlers, builder, services, models, stream)
- [x] go.mod에서 hashicorp/go-plugin 의존성 완전 제거 확인
- [x] Go 소스에서 hashicorp/go-plugin import 없음 확인

## 상태: ✅ 완료
