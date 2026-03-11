# Plugin GUI Cleanup — V4 아키텍처에 맞게 수정

## 배경
V4에서는 container image 방식이 제거됨. Plugin 등록 GUI가 아직 V2/V3 시절 container image 입력 방식으로 되어있음.

## 작업 내용

### 1. DB 테스트 데이터 정리
- [x] korean-test, ml-analytics 등 테스트 플러그인 삭제 (soft-deleted 포함)
- [x] 연관 plugin_stages 정리

### 2. Plugin 등록 다이얼로그 수정 (Plugins.tsx)
- [x] `image` 필드 제거 (다이얼로그, formData, submit 로직)
- [x] `version` 필드 제거 (DataGrid 컬럼 + 다이얼로그)
- [x] Plugin 등록 = 메타데이터(name, description, source_repo) + Stage 정의
- [x] handleSubmit에서 image/version 검증 제거
- [x] "Build Plugin" 버튼 제거 (V3 PluginBuilder 페이지 연결)

### 3. V3 코드 제거
- [x] PluginBuilder.tsx 삭제 (V3 buildPlugin/validateSource/getBuild 사용)
- [x] App.tsx에서 /plugins/build 라우트 제거
- [x] pluginApi.ts에서 V3 API 함수 제거 (buildPlugin, getBuild, getPluginBuilds, validateSource, getPluginBinaryUrl)
- [x] types/plugin.ts에서 V3 타입 제거 (PluginBuild, PluginBinary, BuildPluginRequest, ValidateSourceResponse)
- [x] types/plugin.ts: PluginCreateRequest에서 version/image 제거
- [x] types/plugin.ts: PluginStageInfo.pluginImage, StageSchemaResponse.pluginImage 제거
- [x] DynamicStageForm.tsx: pluginImage 참조 및 unused Chip import 제거

### 4. Backend API 정리
- [x] CreatePluginRequest: image, version → optional (binding 제거)
- [x] Stages → optional (binding:"required,dive" → omitempty)
- [x] 빈 version일 때 기본값 "v0.0.0" 설정

### 5. 검증
- [x] TypeScript 빌드 성공
- [x] Go 빌드 성공
- [x] Go 테스트 통과

## 상태: ✅ 완료
