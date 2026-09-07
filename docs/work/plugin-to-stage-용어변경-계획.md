# 'plugin' → 'stage' 용어 변경 계획

작성: 2026-09-08 · 상태: **A단계 완료, B/C/D 미착수**

## 배경

'plugin'은 커스텀 stage(사용자가 web-ui에서 Go 코드로 작성해 컴파일-인하는 데이터 변환 단계)를
가리키는 옛 이름이다. 제품 개념은 'stage'이므로 용어를 통일한다.

사용자 결정 사항(확정):

- **`plugin-sdk` 모듈 경로는 유지한다.** 바꾸지 않는다.
- **API 경로는 `/stages/custom`** 으로 한다.
- **SQL 마이그레이션 러너는 배선하지 않는다.** DB rename은 SQL을 직접 1회 실행한다.
- DB 테이블까지 전면 변경한다.

---

## 단계 구분

| 단계 | 내용 | 상태 |
|---|---|---|
| **A** | UI 표시 문구 + i18n 값 | ✅ 완료 (`caecbda`, 브랜치 `feat/stage-terminology-ui`, **미머지**) |
| **B** | ~~마이그레이션 러너 배선~~ | ❌ **취소** (사유는 아래) |
| **C** | DB rename (테이블 2개 + 고아 2개 + 컬럼 5개) | 미착수 |
| **D** | Go 심볼 · 파일명 · API 경로 · 프론트 | 미착수 |

A단계는 i18n 값만 바꿨다(ko/en 각 22곳). i18n **네임스페이스는 그대로 뒀다** —
`plugin.*` → `stage.*` 병합 시 7개 키가 충돌하고, 이들은 서로 다른 화면의 다른 문구다:

```
plugin.title = "플러그인"        ← Stage 목록 페이지 제목
stage.title  = "Stage 편집기"    ← 편집기 제목 (다른 화면)
```

충돌 키: `plugin.*` ∩ `stage.*` = `title` `name` `type` `edit` `deleteConfirm` /
`pluginBuilder.*` ∩ `stage.*` = `title` `nameRequired`

키 rename은 D단계에서 코드 심볼과 함께 다뤄야 안전하다.

---

## B단계를 취소한 이유 (실측 근거)

마이그레이션은 **이중 구조이고 SQL 쪽은 dead code**다.

- 실제 실행: GORM `AutoMigrate` (`control-plane/pkg/database/database.go:64-101`,
  호출은 `cmd/server/main.go:139-158`, `-migrate` 플래그 또는 `AUTO_MIGRATE=true`)
- 미실행: golang-migrate SQL (`control-plane/pkg/database/migrations/000001~000007`)
  — 러너 함수 5개(`RunMigrations`, `RunMigrationsWithDB`, `RunMigrationsFromPath`,
  `MigrateDown`, `GetMigrationVersion`)가 **호출처 0건**

러너를 그냥 배선하면 안 되는 이유:

```
운영 DB 에 schema_migrations 테이블 없음 (실측 확인)
  → m.Up() 이 000001 부터 전부 실행
  → 000002_rename_pipeline_groups_to_workflows.up.sql 의
    RENAME TABLE pipeline_groups TO workflows 가 실패
    (pipeline_groups 는 이미 없고 workflows 만 존재)
```

000001은 `IF NOT EXISTS`로 멱등하지만 **000002·000003·000007은 멱등하지 않다.**

배선하려면 `schema_migrations`를 만들고 version=7로 baseline 처리해야 하는데,
AutoMigrate가 만든 실제 스키마와 SQL이 생성할 스키마가 일치하는지 검증이 필요하다.
로컬 단일 환경이므로 **rename SQL을 직접 1회 실행하는 편이 짧고 위험이 적다**는 판단.

> 다만 이 결정으로 스키마 변경 이력은 계속 추적되지 않는다. 환경이 늘어나면
> baseline + 러너 배선을 별도 작업으로 해야 한다.

---

## C단계: DB rename

### 대상 (실측 — `information_schema` 조회 결과)

| 테이블 | 행 수 | Go 모델 | 처리 |
|---|---|---|---|
| `plugins` | 2 | `models.Plugin` (models.go:598) | → `stages` |
| `plugin_builds` | 0 | `models.PluginBuild` (models.go:652) | → `stage_builds` |
| `plugin_stages` | 1 | **없음 (고아)** | 삭제 검토 |
| `plugin_binaries` | 0 | **없음 (고아)** | 삭제 검토 |

`plugin_stages`·`plugin_binaries`는 V3 gRPC 시절 테이블로, **Go 모델이 삭제된 뒤에도
DB에 남아 있다**(AutoMigrate는 테이블을 지우지 않는다). 코드에서 참조 0건.

`plugin_stages`의 1행은 확인 결과 **테스트 잔재**다 — `plugin_id`가
`test-v4-plugin`(2026-03-11 생성, `source_code` 길이 0으로 이전에 빌드를 깨뜨렸던
그 플러그인)을 가리키고 `stage_type='test-transform'`, `display_name` 비어 있음.
따라서 **두 고아 테이블 모두 DROP 해도 안전하다.**

### 이름에 plugin이 들어간 컬럼 (전수)

| 컬럼 | 테이블 | 모델 위치 | 새 이름(안) |
|---|---|---|---|
| `plugin_id` | `plugin_builds` | models.go:654 | `stage_id` |
| `plugin_id` | `stage_revisions` | models.go:708 | `stage_id` |
| `plugin_name` | `stage_revisions` | models.go:709 | `stage_name` |
| `plugin_ids` | `runner_versions` | models.go:689 | `stage_ids` |
| `plugin_hashes` | `runner_versions` | models.go:690 | `stage_hashes` |

⚠️ **`plugin_ids`·`plugin_hashes`는 JSON 컬럼이다.** 컬럼 rename만으로는 페이로드
내부 키가 바뀌지 않는다. `plugin_hashes`는 `{plugin_id: source_hash}` 맵이므로
내부 키 이름을 쓰는 코드가 있는지 D단계에서 함께 확인해야 한다.

### 실행 순서 (중요)

GORM `AutoMigrate`가 `TableName()`을 보고 테이블을 찾으므로 **순서를 지켜야 한다**:

```
1. CP 정지 (또는 AUTO_MIGRATE=false 확인)
2. rename SQL 직접 실행 (RENAME TABLE + ALTER TABLE CHANGE COLUMN)
3. 모델의 TableName()·필드 태그를 새 이름으로 변경 (D단계와 같은 커밋)
4. CP 배포
```

**순서를 어기면**(모델을 먼저 바꾸고 배포) AutoMigrate가 새 빈 테이블을 만들고
기존 데이터는 옛 테이블에 남아 **데이터가 사라진 것처럼 보인다.**

### 참고할 선례

`control-plane/pkg/database/migrations/000002_rename_pipeline_groups_to_workflows.up.sql`
이 완전한 rename 패턴을 제공한다: `RENAME TABLE` + `ALTER TABLE ... CHANGE COLUMN` +
인덱스 재생성 + `UPDATE resource_permissions SET resource_type=...`.

단 `resource_permissions`는 **행이 0개**임을 확인했으므로(실측) 그 UPDATE 부분은
이번 작업에 불필요하다.

---

## D단계: 코드 · API · 프론트

### API 경로 (`/stages/custom` 확정)

`/stages`는 **이미 점유돼 있다** (`routes.go:377-387`, stage 스키마 조회 전용):

```
GET /stages                        → ListAllStages (빌트인+커스텀 목록)
GET /stages/:type/schema           → GetStageSchema
GET /stages/schemas, /stages/schemas/:type
GET /stages/categories, /stages/categories/:category/schemas
GET /stages/field-types
```

⚠️ **gin radix 트리 충돌 주의.** `/stages/:type/schema`의 `:type`과 같은 위치에
다른 param 이름(`:name`)을 쓰면 **gin이 panic**한다. `routes.go:195` 주석이 같은
함정을 이미 경고한다. `/stages/custom/...` 하위로 넣으면 정적 세그먼트라 안전하다.

이동 계획:

| 현재 | 변경 후 |
|---|---|
| `GET /plugins` | `GET /stages/custom` |
| `POST /plugins` (내부, 인증 없음) | `POST /stages/custom` |
| `POST /plugins/test-script` | `POST /stages/custom/test-script` |
| `POST /plugins/test-native` | `POST /stages/custom/test-native` |
| `GET /plugins/revisions/:revisionId` | `GET /stages/custom/revisions/:revisionId` |
| `GET /plugins/:name` | `GET /stages/custom/:name` |
| `GET /plugins/:name/revisions` | `GET /stages/custom/:name/revisions` |
| `PUT /plugins/:name` (admin) | `PUT /stages/custom/:name` |
| `DELETE /plugins/:name` (admin) | `DELETE /stages/custom/:name` |

`routes.go:493`의 API index 응답 `"plugins": "/api/v1/plugins"`도 함께 변경.
하위호환 라우트를 남길지는 미결정 — 내부 호출자만 있으면 불필요.

### 핸들러 타입명 충돌

`handlers.StageHandler`가 **이미 존재**한다(`stage_handler.go`, 스키마 조회용).
`PluginHandler` → `StageHandler` 개명이 불가능하므로 이름을 정해야 한다:
`CustomStageHandler` 권장.

### Go 심볼 (주요)

- 모델: `Plugin`(models.go:598), `PluginBuild`(:652) + `TableName()` 2개
- 핸들러: `PluginHandler`(plugin_handler.go:29), `NewPluginHandler`(:37),
  `ListPlugins`(:83), `GetPlugin`(:119), `CreatePlugin`(:141), `UpdatePlugin`(:319),
  `DeletePlugin`(:436), `TestNativePlugin`(:613), `TestScript`(:492),
  `ListRevisions`(:549), `GetRevision`(:568)
- 요청/응답: `CreatePluginRequest`(:47), `UpdatePluginRequest`(:63),
  `TestNativePluginRequest`(:585), `TestNativePluginResponse`(:594)
- 빌더: `GenerateRegistryCustom(plugins []models.Plugin)`(runner_builder.go:623),
  `generatePluginGoMod`(:667), `appendPluginRequires`(:705), `extractPluginIDs`(:756)
- 서비스: `BuildRequiredError.PendingPlugins`(runner_resolver.go:18),
  `findNativePluginsInWorkflow`(:162)
- worker: `(*Client).GetPluginImage`(pipeline-worker/internal/controlplane/client.go:69)

### 파일명 rename

`control-plane/internal/api/handlers/` 아래 `plugin_handler.go`,
`plugin_handler_test.go`, `plugin_security.go`, `plugin_security_test.go`,
`plugin_schema_validation_test.go`

### JSON 필드명

`plugin_id`, `plugin_name`, `plugin_ids`, `plugin_hashes`, `plugin`(nested),
`plugins`(배열), `pending_plugins`
→ 프론트(`types/plugin.ts:66`, `services/pluginApi.ts:44` 등)와 **동시에** 바꿔야 한다.

### 프론트엔드 (13파일)

`pages/Plugins.tsx`(129건), `services/pluginApi.ts`(23), `types/plugin.ts`(7),
`components/NativeStageEditor/NativeStageEditor.tsx`(20),
`components/JSScriptStageEditor/JSScriptStageEditor.tsx`(6),
`components/DynamicStageForm.tsx`(5), `pages/WorkflowDetail.tsx`(4),
`components/Layout/MainLayout.tsx`(4 — `key: '/plugins'`, `nav.plugins`),
`pages/StageEditor.tsx`(2), `components/PipelineEditor/StageSection.tsx`(2),
`App.tsx`(2 — `<Route path="plugins">`)

URL `/plugins` → `/stages/custom` 변경 시 기존 북마크 리다이렉트 여부 미결정.

### 코드 생성 계약 (원자적으로 함께 변경)

`runner_builder.go`가 생성하는 문자열과 그것을 소비하는 검증기가 같은 계약을 공유한다:

- `runner_builder.go:632,642` — 패키지 alias `plugin_<name>`
- `runner_builder.go:633,669,690,698` — 모듈 경로 `github.com/conduix/plugins/<name>`,
  `replace ... => ./plugins/<name>`
- `runner_builder.go:357,361` — 파일 경로 `<dir>/plugins/<name>/stage.go`
- `stage_import_validation.go:105,116,120` — `plugin_<name>.Stage{}` 계약,
  패키지명 `pluginstage`, 모듈명 `conduix-plugin-test`
- `lsp/workspace_manager.go:212` — 모듈명 `conduix-plugin-workspace`

**세 파일을 같은 커밋에서 바꿔야 한다.** 하나만 바꾸면 커스텀 stage 빌드가 깨진다.

---

## 치환하면 안 되는 곳 (필수 제외)

1. **`handlers/plugin_security.go:16`** — `blockedImports`의 `"plugin"`은 **Go 표준
   라이브러리 `plugin` 패키지**다. 바꾸면 보안 검사가 무력화된다. (파일명은 rename 가능)
2. **`pipeline-core/pkg/source/cdc_postgres.go:189,194`** — `pluginArgs`,
   `pglogrepl.StartReplicationOptions{PluginArgs:...}`는 PostgreSQL 논리 복제
   output plugin 인자이고 외부 라이브러리 필드명이다.
3. **`web-ui/vite.config.ts:2,5`** — `@vitejs/plugin-react`, `plugins: [react()]`
4. **`web-ui/package.json:45,48,49`** + `package-lock.json` — npm 패키지명
   (`eslint-plugin-react-hooks` 등)
5. **`plugin-sdk/` 모듈 경로** (`github.com/conduix/conduix/plugin-sdk`) — **사용자
   결정으로 유지.** 사용자가 web-ui에서 작성하는 Go 소스에 이 경로가 그대로 들어가고,
   **DB에 저장된 모든 `plugins.source_code`가 이 경로를 포함**한다. 바꾸면 저장된
   커스텀 stage 전량이 컴파일 실패한다. 관련 위치: `plugin-sdk/go.mod:1`,
   각 모듈 go.mod의 `replace`, `stage_import_validation.go`, `lsp/workspace_manager.go`,
   `NativeStageEditor.tsx:57`(에디터 템플릿), `docker-compose.yml:61`,
   `.github/workflows/ci.yml`(빌드 매트릭스), `lefthook.yml:30-31`,
   `runner_builder.go:77`(`runnerSourceModules`)
6. **`control-plane/pkg/database/migrations/*.sql`** — 기존 마이그레이션 파일은 불변.
   특히 `000007_add_plugin_config_schema.*`의 `plugins` 테이블명을 수정하면 안 된다.
7. **`docs/archive/**`** — 폐기된 아키텍처 기록(`PLUGIN_ARCHITECTURE_V3.md` 등).
   치환하면 당시 사실이 왜곡된다. `docs/README.md:37`이 이들을 폐기 문서로 명시.

## 별건: 삭제 후보

**`pipeline-core/pkg/plugin/` 패키지 전체**(`registry.go`, `stage_interface.go`,
`subprocess.go`, `registry_test.go`) — V3 gRPC/subprocess 시절 레거시로
**import 0건 dead code**. 리네임 대상이 아니라 삭제를 검토할 것.

---

## 검증 방법

1. 각 단계 후 `go build ./...` (6개 모듈) + `go test ./...`
2. `npx tsc --noEmit` + `npm run build`
3. `grep -rn "plugin" --include="*.go" --include="*.ts" --include="*.tsx"` 로 잔존 확인
   (제외 목록에 해당하는 것만 남아야 한다)
4. **커스텀 stage 실동작 검증**: RunnerVersion 재빌드 → realtime 재시작 →
   지오코딩 동작 확인. 코드 생성 계약이 깨지면 이 단계에서 드러난다.
5. DB rename 후 `plugins` 2행이 `stages`로 온전히 옮겨졌는지 확인

## 미결정 사항

- `/plugins` → `/stages/custom` 하위호환 라우트를 남길지 (내부 호출자만이면 불필요)
- 프론트 URL `/plugins` 리다이렉트 제공 여부
- i18n `plugin.*`/`pluginBuilder.*` 네임스페이스를 어떤 이름으로 옮길지
  (`customStage.*`? `stageBuilder.*`?) — `stage.*` 병합은 충돌로 불가
- `pipeline-core/pkg/plugin/` dead code 를 이번에 삭제할지 별건으로 둘지

## 해소된 확인 사항 (재조사 불필요)

- `resource_permissions` 행 0개 → 선례의 `UPDATE resource_permissions` 불필요
- `plugin_stages`·`plugin_binaries` 고아 테이블 → DROP 안전 (테스트 잔재)
- `plugins` 2행 / `plugin_builds` 0행 / `stage_revisions` 2행 / `runner_versions` 7행
- `schema_migrations` 테이블 없음 → SQL 러너 배선 불가(B단계 취소 근거)
