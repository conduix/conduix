# Plugin V4 Phase 5: Stage Revision + Build History

## 핵심 요구사항

1. Stage 추가/수정/삭제마다 글로벌 seq 순번으로 revision 히스토리 저장 (소스 zstd 압축)
2. Runner가 어떤 seq까지 빌드되었는지 확인 가능
3. **실행 정책**:
   - 변경된 stage를 **사용하지 않는** pipeline → 빌드 중이어도 실행 가능
   - 변경된 stage를 **사용하는** pipeline → 이전 runner에서 실행 시도 시 메시지로 안내
4. 재빌드 (Re-run) 기능

## 데이터 모델

### StageRevision (신규)

```sql
stage_revisions
├── id              PK (UUID)
├── seq             AUTO_INCREMENT, UNIQUE  -- 글로벌 순번
├── plugin_id       FK → plugins.id
├── plugin_name     VARCHAR(255)  -- 조회 편의
├── action          "create" | "update" | "delete"
├── source_data     MEDIUMBLOB (zstd 압축)  -- 변경 시점 소스 스냅샷
├── go_mod_data     BLOB (zstd 압축, nullable)
├── source_hash     VARCHAR(64)
├── diff_summary    VARCHAR(500)  -- "+12 -3 lines" 등
├── message         VARCHAR(500)  -- 사용자 메모 (optional)
├── created_by      VARCHAR(36)
├── created_at      TIMESTAMP
```

### RunnerVersion 확장

```sql
runner_versions (기존 + 추가)
├── revision_seq    INT           -- 빌드 시점 최신 seq (여기까지 포함)
├── trigger         VARCHAR(20)   -- "manual" | "auto" | "rebuild"
├── parent_id       VARCHAR(36)   -- 재빌드 시 원본 버전 ID
```

### 실행 정책 로직 변경

```
resolveRunnerImage(workflow):
  1. pipeline에서 사용하는 stage type 추출
  2. 그 중 native plugin stage만 필터
  3. 변경된(SourceHash != DeployedHash) plugin 목록 = pendingPlugins

  if pendingPlugins가 비어있으면:
    → 기본 runner 또는 최신 ready runner로 실행 OK

  if pendingPlugins가 있지만, 이 workflow의 pipeline이 사용하는 stage와 겹치지 않으면:
    → 최신 ready runner로 실행 OK (변경 무관한 pipeline)

  if pendingPlugins가 이 workflow의 pipeline이 사용하는 stage와 겹치면:
    → 실행 차단 + 메시지:
      "stage [crm-enrichment]가 seq #7에서 수정되었습니다.
       현재 runner(rv-3)는 seq #5 기준 빌드입니다.
       빌드 후 실행해주세요."
```

## 작업 항목

### 5-1. zstd 유틸 + 모델 — ✅ 완료
- [x] `go.mod` — zstd 의존성 (klauspost/compress, 이미 indirect로 존재)
- [x] `pkg/models/compress.go` — CompressZstd/DecompressZstd 유틸
- [x] `pkg/models/models.go` — StageRevision 모델
- [x] `pkg/models/models.go` — RunnerVersion에 RevisionSeq, Trigger, ParentID 추가
- [x] `pkg/database/database.go` — AutoMigrate에 StageRevision 추가

### 5-2. Revision 서비스 — ✅ 완료
- [x] `internal/services/revision_service.go`
  - [x] CreateRevision() — zstd 압축 + diff summary 계산
  - [x] ListRevisions(pluginID) — seq 역순 조회
  - [x] GetRevision(id) — 소스 압축 해제
  - [x] GetLatestSeq()
  - [x] GetRevisionsBySeqRange(fromSeq, toSeq)

### 5-3. Plugin API revision 연동 — ✅ 완료
- [x] POST /plugins — create revision (native plugin 생성 시)
- [x] PUT /plugins/:name — update revision (소스 해시 변경 시)
- [x] DELETE /plugins/:name — delete revision

### 5-4. Runner Build revision 연동 — ✅ 완료
- [x] 빌드 시 revision_seq 기록 (StartBuild에서 latestSeq 조회)
- [x] POST /runner/rebuild/:id — 재빌드 (RebuildVersion 핸들러)
- [x] trigger 필드 설정 (manual/rebuild)
- [x] parent_id 설정 (재빌드 시 원본 ID)

### 5-5. resolveRunnerImage 정책 변경 — ✅ 완료
- [x] pipeline별 사용 stage 기반으로 실행 가능 여부 판단
- [x] 변경 무관 pipeline → 빌드 중이어도 실행 허용
- [x] 변경 관련 pipeline → seq 정보 포함 안내 메시지
- [x] extractStageTypesPerPipeline() — pipeline별 stage type 추출
- [x] getPendingStageTypes() — 변경된 plugin의 stage type 조회

### 5-6. API — ✅ 완료
- [x] GET /plugins/:name/revisions — 히스토리
- [x] GET /plugins/revisions/:revisionId — 상세 (소스 해제)
- [x] GET /runner/versions — revision_seq 포함 (모델에 필드 추가)
- [x] POST /runner/rebuild/:id — 재빌드

### 5-7. Frontend — ✅ 완료
- [x] StageRevision, StageRevisionDetail 타입 추가
- [x] RunnerVersion에 revision_seq, trigger, parent_id 필드 추가
- [x] API 클라이언트: getPluginRevisions, getRevisionDetail, rebuildRunnerVersion
- [x] Build History 탭 — seq, status, trigger, duration, rebuild 버튼
- [x] Revision History 탭 — seq, action, hash, diff summary, message
- [x] Build Log 다이얼로그 (dark theme pre block)
- [x] Runner Status 패널에 revision_seq 표시
- [x] DataGrid actions에 History 버튼 추가 (native plugin만)

### 5-8. 테스트 — ✅ 완료
- [x] zstd 압축/해제 (5개 테스트: Basic, Empty, LargeData, InvalidData, GoSourceCode)
- [x] Revision 서비스 (6개 테스트: DiffSummary Create/Delete/Update/UpdateNoOld/EmptyAction, CountLines)
- [x] resolveRunnerImage 정책 변경 (2개 테스트: extractStageTypesPerPipeline Basic/Empty)
- [x] BuildRequiredError seq 정보 포함 확인

## 구현 파일

### 신규 파일
- `control-plane/pkg/models/compress.go` — zstd 압축/해제 유틸
- `control-plane/pkg/models/compress_test.go` — 5개 테스트
- `control-plane/internal/services/revision_service.go` — RevisionService
- `control-plane/internal/services/revision_service_test.go` — 6개 테스트

### Frontend 수정 파일
- `web-ui/src/types/plugin.ts` — StageRevision, StageRevisionDetail, RunnerVersion 필드 확장
- `web-ui/src/services/pluginApi.ts` — getPluginRevisions, getRevisionDetail, rebuildRunnerVersion
- `web-ui/src/pages/Plugins.tsx` — Build History 탭, Revision History 탭, Build Log 다이얼로그

### 수정 파일
- `control-plane/pkg/models/models.go` — StageRevision 모델 추가, RunnerVersion 필드 확장
- `control-plane/pkg/database/database.go` — AutoMigrate에 StageRevision 추가
- `control-plane/internal/api/handlers/plugin_handler.go` — revision 생성 연동, ListRevisions/GetRevision API
- `control-plane/internal/api/handlers/runner_handler.go` — revision_seq 기록, RebuildVersion 핸들러
- `control-plane/internal/api/routes.go` — revision/rebuild 라우트 추가
- `control-plane/internal/services/runner_resolver.go` — 실행 정책 변경 (pipeline별 판단)
- `control-plane/internal/services/runner_resolver_test.go` — 정책 변경 테스트 추가

## 구현 순서
1. ~~zstd 유틸 + 모델 + migrate~~ ✅
2. ~~Revision 서비스 + 테스트~~ ✅
3. ~~Plugin API revision 연동~~ ✅
4. ~~Runner Build revision/rebuild 연동~~ ✅
5. ~~resolveRunnerImage 정책 변경 + 테스트~~ ✅
6. ~~API endpoints~~ ✅
7. ~~Frontend~~ ✅
