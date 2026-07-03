# Plugin V4 Phase 4: GUI 통합 — ✅ 완료

## 작업 항목

### 4-1. 타입 정의 — ✅ 완료
- [x] `types/plugin.ts` — Plugin 모델에 type, source_code, go_mod, source_hash, deployed_hash, runner_version_id 추가
- [x] `types/plugin.ts` — RunnerVersion, RunnerStatusResponse, RunnerPluginStatus 타입 추가

### 4-2. API 클라이언트 — ✅ 완료
- [x] `services/pluginApi.ts` — getRunnerStatus() 추가
- [x] `services/pluginApi.ts` — getRunnerVersions(), getRunnerVersion() 추가
- [x] `services/pluginApi.ts` — startRunnerBuild() 추가

### 4-3. Plugins 페이지 개선 — ✅ 완료
- [x] `pages/Plugins.tsx` — Runner 상태 패널 (빌드 필요/배포 완료 표시)
- [x] `pages/Plugins.tsx` — Runner 빌드 버튼 (비동기, 상태 자동 갱신)
- [x] `pages/Plugins.tsx` — DataGrid에 Type 컬럼 추가 (script/native)
- [x] `pages/Plugins.tsx` — DataGrid에 Deploy 상태 컬럼 추가 (Instant/Deployed/Build needed)

## 미구현 (향후)
- [ ] Plugin 생성/수정 다이얼로그에 Type 선택 필드 추가
- [ ] Native Plugin 에디터 (코드 + config + 테스트)
- [ ] 빌드 로그 조회 다이얼로그
- [ ] 빌드 이력 목록 페이지
- [ ] Workflow 실행 버튼 상태 연동 (빌드 필요 시 비활성)
