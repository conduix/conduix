# 커스텀 stage 의존성 버전 공존 — 구현 계획

> 작성 2026-09-21. 대상: 이 작업을 이어서 구현할 개발자 / Claude Code.
> 배경·근거·한계: [../CUSTOM_STAGE_DEPENDENCY_CONFLICT.md](../CUSTOM_STAGE_DEPENDENCY_CONFLICT.md) §5~§7.
> 결정 기록: [../adr/0005-dependency-version-coexistence.md](../adr/0005-dependency-version-coexistence.md).
> 상태(2026-09-21): **W0~W4 완료(커밋됨, 브랜치 `feat/dep-version-coexistence`). W5~W7 미착수.** 인수인계는 §8.

## 0. 한 문장

레지스트리가 모듈당 여러 버전을 갖고, stage 가 자기 버전을 고정하고, 빌더가 기본과 다른
버전을 `forked/` 경로로 복사·재작성해 **한 바이너리**에 함께 링크한다.

## 1. 불변식 (구현 내내 지킬 것)

1. **바이너리는 하나.** RunnerVersion·streaming 파드·initContainer·rolling 은 건드리지 않는다.
2. **모든 stage 가 기본 버전이면 산출물은 지금과 바이트 단위로 같아야 한다.** fork 경로는
   고정값이 기본과 다를 때만 생긴다. 이것이 회귀 안전선이다.
3. **같은 정책은 한 곳.** "stage 가 쓸 모듈 버전 결정" 은 `dependency.ResolvePins()` 하나가 하고,
   빌더·에디터 테스트·LSP 워크스페이스는 그 결과만 소비한다. 지금은 세 곳이 각자
   레지스트리에서 go.mod 를 생성한다(`runner_builder.go:683`, `stage_import_validation.go:153`,
   `workspace_manager.go:210`).
4. **복사본 내부 import 재작성은 생략 불가.** 생략하면 에러 없이 버전이 섞인다(실험 4).

## 2. 데이터 모델

### 2.1 `allowed_module_versions` (신규) — PK 변경 회피

GORM AutoMigrate(`pkg/database/database.go:65`)는 기존 테이블의 PK 를 바꾸지 못한다.
`allowed_modules` 의 PK(`module_path`)는 유지하고 `version` 컬럼의 의미를 **기본 버전**으로
고정한다. 보유 버전 목록은 새 테이블로 둔다.

```go
// control-plane/pkg/models/models.go — AllowedModule 바로 아래
type AllowedModuleVersion struct {
    ModulePath string         `gorm:"primaryKey;size:255" json:"module_path"`
    Version    string         `gorm:"primaryKey;size:100" json:"version"`
    Status     string         `gorm:"size:50;default:active" json:"status"` // active | retired
    AddedBy    string         `gorm:"size:36" json:"added_by,omitempty"`
    CreatedAt  time.Time      `json:"created_at"`
    DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}
```

`AllowedModule` 에 추가:

```go
SingleVersionOnly bool `gorm:"default:false" json:"single_version_only"` // init 전역 등록 모듈: fork 불가
```

주석 수정: `Version` 의 "module 당 단일 — 충돌 방지의 물리 근거" 를 "기본 버전. 보유 버전은
allowed_module_versions" 로.

**백필**: `Migrate()` 끝에 `allowed_modules` 의 (module_path, version) 을 `allowed_module_versions`
에 `FirstOrCreate` 로 넣는다. 멱등.

### 2.2 `plugins.dep_versions` (신규 컬럼)

```go
DepVersions string `gorm:"type:text" json:"dep_versions,omitempty"` // JSON {module_path: version}. 빈 값 = 레거시(기본 버전 사용)
```

`GoMod` 컬럼은 이 작업에서 제거하지 않는다(W7 후속). 빌더는 이미 무시하고 있다.

### 2.3 `runner_versions.forked_modules` (신규 컬럼, 관측용)

```go
ForkedModules string `gorm:"type:text" json:"forked_modules,omitempty"` // JSON [{module_path, version, fork_path, plugin_ids[]}]
```

## 3. 새 패키지 `control-plane/internal/dependency`

세 소비자가 공유하는 정책과 순수 함수. DB 접근은 `Resolver` 하나로 한정한다.

| 파일 | 내용 |
|---|---|
| `pins.go` | `type Pins map[string]string` (module_path → version). `ResolvePins(imports []string, existing Pins, defaults []models.AllowedModule) (Pins, error)`: import 를 `isCoveredByAllowedModule` 규칙으로 모듈에 매핑, 기존 고정값 있으면 유지, 없으면 기본 버전. 매핑 안 되는 외부 import 는 에러(현행 D5 검증과 동일 정책 → `stage_import_validation.go:25` 의 판별 로직을 이 패키지로 이동) |
| `forkpath.go` | `ForkPath(modulePath, version string) string` → `github.com/conduix/forked/<mangled>`. 규칙: `/`·`.` → `_`, 버전의 `.`·`+`·`-` → `_`, 소문자 유지. 예: `github.com/go-resty/resty` + `v0.8.0` → `github.com/conduix/forked/github_com_go-resty_resty_v0_8_0`. 충돌 방지를 위해 (path, version) → mangled 는 단사여야 하며 테스트로 고정 |
| `rewrite.go` | `RewriteModuleTree(dir, oldPath, newPath string) error`: dir 아래 모든 `.go`(`_test.go` 제외)에서 `oldPath` 또는 `oldPath/` 접두 import 를 `newPath` 로 치환(go/parser ImportsOnly → 위치 기반 바이트 치환, 포맷 보존). `go.mod` 의 `module` 줄도 치환. `RewriteStageImports(src string, pins Pins, defaults map[string]string, pkgNameOf func(forkDir string) (string, error)) (string, error)`: 기본과 다른 고정 모듈의 import 를 fork 경로로 바꾸고, 사용자가 alias 를 안 썼으면 복사본의 실제 `package` 이름을 alias 로 붙인다(경로 마지막 요소가 mangled 라 alias 필수) |
| `gomod.go` | `PluginGoMod(name string, pins Pins, defaults map[string]string) string`, `MainRequireBlock(plugins, pins map[pluginID]Pins, forks []Fork) string`, `TestGoMod(pins Pins, sdkPath string) string`, `WorkspaceGoMod(pins Pins, sdkPath string) string` — 현재 `generatePluginGoMod`/`pluginRequireBlock`/`buildTestGoMod`/`generateGoMod` 네 곳의 생성 로직을 여기로 모은다. 기본 버전만 있을 때의 출력은 **현재 출력과 동일**해야 한다(기존 테스트 `TestPluginRequireBlock`, `TestGeneratePluginGoMod` 를 이 패키지로 옮겨 그대로 통과시킨다) |
| `source.go` | `type ModuleSource interface { Dir(ctx, modulePath, version string) (string, error) }`. 기본 구현은 `go mod download -json path@ver` 의 `Dir`(빌더의 `runCommand` env 재사용). 테스트는 testdata 디렉토리를 돌려주는 대역 |

## 4. 작업 단계

의존: W1 → W2 → W3 → W4. W5·W6 는 W2 이후 병행. W7 은 마지막.

### W1. 모델·마이그레이션·레지스트리 API

- `models.go`: §2 의 세 변경. `database.go:65` AutoMigrate 목록에 `AllowedModuleVersion` 추가 + 백필.
- `module_handler.go`:
  - `ListModules`(`:53`): 응답에 `versions []AllowedModuleVersion`, `usage map[version]int`(plugins.dep_versions 스캔) 포함.
  - `CreateModule`(`:63`): 기존과 같이 최신 버전을 기본으로 등록 + `allowed_module_versions` 행 생성.
  - `UpdateModule`(`:112`): 의미를 **기본 버전 변경**으로 명시. 지정 버전이 `allowed_module_versions` 에 없으면 자동 추가. `SingleVersionOnly` 갱신 필드 추가. **기존 stage 는 건드리지 않는다.**
  - 신규 `POST /modules/*module/versions {version?}`(빈 값 → `@latest`), `DELETE /modules/*module/versions/:version`(사용 stage 가 있으면 409 + 목록). `routes.go:416` 에 admin 권한으로 추가.
- **검증**: 핸들러 단위 테스트(sqlite 인메모리; 다른 handler 테스트의 DB 대역 방식 확인 후 동일하게). 백필 멱등 테스트.

### W2. `dependency` 패키지 + 세 소비자 전환 (fork 없이)

이 단계가 끝나면 **동작은 지금과 같고 구조만 일원화**된다. fork 는 아직 안 한다.

- §3 패키지 작성. `pins.go`·`gomod.go`·`forkpath.go` 부터.
- `plugin_handler.go` `CreatePlugin`(`:141`)·`updateExistingPlugin`(`:254`)·`UpdatePlugin`(`:319`): `validateStageImports` 호출 지점(`:208`,`:267`,`:358`)을 `dependency.ResolvePins` 로 대체하고 결과를 `plugin.DepVersions` 에 저장. 기존 고정값은 유지, 새 import 만 기본 버전.
- `TestNativePlugin`(`:613`, go.mod 생성 `:670`): `buildTestGoMod(h.db)` → `dependency.TestGoMod(pins, sdkPath)`. 단일 stage 빌드라 fork 불필요, 고정 버전을 원래 경로로 require.
- `workspace_manager.go` `SyncSource`(`:144`)·`generateGoMod`(`:210`): 세션에 pins 를 전달받아 `dependency.WorkspaceGoMod`. `SyncSource` 의 무시되던 `goMod string` 인자를 `pins` 로 교체. LSP proxy(`lsp/proxy.go`)가 stage 저장값에서 pins 를 읽어 넘긴다.
- `runner_builder.go` `buildInTempDir`(`:329`): `generatePluginGoMod`(`:683`)·`pluginRequireBlock`(`:702`)·`appendPluginRequires`(`:721`) 를 `dependency` 호출로 교체. 레거시(빈 `DepVersions`) 플러그인은 빌드 시점 기본 버전으로 pins 를 만들고 **빌드 성공 후 `updateDeployedHashes`(`:466`) 와 함께 저장**한다.
- **검증**: 옮긴 기존 테스트 전부 통과. 기본 버전만 있을 때 go.mod 출력이 바이트 동일한지 golden 테스트. `make test`.

### W3. 빌더 fork 경로

- `buildInTempDir` 에 단계 추가(허용 모듈 조회 `:352` 직후, 플러그인 소스 배치 `:361` 직전):
  1. 모든 plugin 의 pins 에서 `version != default` 인 (module, version) 집합 → `forks`.
  2. `SingleVersionOnly` 모듈이 forks 에 있으면 즉시 실패(어느 stage 가 어느 버전인지 메시지).
  3. 각 fork: `ModuleSource.Dir()` → `copyDir(dir, batchJobDir/forked/<mangled>)`(모듈 캐시는 읽기 전용 → 복사 후 `0644`) → `RewriteModuleTree(copy, module, forkPath)`.
  4. `logBuf` 에 fork 목록 기록. `version.ForkedModules` 채움.
- 플러그인 소스 배치(`:367`): `p.SourceCode` 를 그대로 쓰지 말고 `RewriteStageImports` 결과를 쓴다. `pkgNameOf` 는 fork 디렉토리의 첫 `.go` 파일 `package` 절을 읽는다.
- go.mod: `MainRequireBlock` 이 fork 마다 `require <forkPath> v0.0.0` + `replace <forkPath> => ./forked/<mangled>` 를 낸다. 플러그인 go.mod 도 fork 경로를 require(replace 는 메인만).
- `CombinedSourceHash`(`:574`): **pins 를 해시에 포함**한다(정렬된 `module@version` 목록). 이것으로 §3 의 "PUT 후 빌드 스킵" 도 해소된다 — 기본 버전 변경은 새 stage 에만 영향이라 해시가 바뀌지 않지만, stage 가 버전을 올리면 바뀐다. 리졸버 `coreChangedSince`(`runner_resolver.go:212`)는 같은 함수를 쓰므로 자동 정합.
- **검증**:
  - 단위: `TestForkPath_Injective`, `TestRewriteModuleTree_SelfImportsAndGoMod`(testdata 에 다중 패키지 모듈 두 벌), `TestRewriteStageImports_AddsAlias/KeepsUserAlias/LeavesDefaultUntouched`, `TestMainRequireBlock_WithForks`.
  - 통합(`//go:build integration`, 실제 `go build`, 네트워크 불필요): testdata 의 `foo_a`/`foo_b` 를 `ModuleSource` 대역으로 주입해 두 stage 가 각기 다른 버전을 호출하고 출력이 갈리는지 확인 — CONFLICT.md §5 실험 2~5 를 자동화한 것.

### W4. init 자가점검

- `pipeline-runner/cmd/runner/main.go:23`: `main()` 첫 줄에서 `os.Getenv("CONDUIX_INIT_CHECK") == "1"` 이면 `slog.Info("init check ok")` 후 `os.Exit(0)`. 모든 `init()` 은 이 시점에 이미 실행됐으므로 중복 등록 panic 은 그 전에 터진다.
- `runner_builder.go` `go build` 성공 직후(`:414` 이후): `forks` 가 비어 있으면 건너뛴다(불변식 2). 있으면 실행:
  - `config.Platform` 이 호스트와 같으면 빌드된 바이너리를 `CONDUIX_INIT_CHECK=1` 로 실행.
  - 다르면(로컬 darwin 빌더 등) 호스트 타깃으로 `-o init-check-bin` 한 번 더 빌드해 실행. `RunnerBuilderConfig.InitCheck` (`auto|off`, 기본 `auto`) 로 끌 수 있게.
  - 비정상 종료 시 stderr 의 panic 첫 줄 + fork 목록을 `version.Error` 에 담아 실패. 메시지에 "해당 모듈을 single_version_only 로 표시하거나 stage 버전을 맞추세요" 안내.
- **검증**: runner 단위 테스트(env 설정 시 exit 0). 빌더 통합 테스트에 `database/sql` 드라이버 이름을 두 번 `Register` 하는 fixture 를 fork 로 넣어 실패 메시지가 나오는지.

### W5. stage 버전 올리기 API

- `POST /plugins/:id/upgrade-deps`(operator 이상): 대상 모듈 목록(생략 시 전부)을 **기본 버전으로 바꾼 pins** 로 `TestNativePlugin` 의 임시 빌드 절차(`:613`~, sample_data 없이 컴파일만)를 돌리고, 성공하면 `DepVersions` 갱신 + StageRevision(`createRevision`, `:793`) 기록. 실패하면 400 + 컴파일 에러 원문, 저장값 유지.
- `POST /modules/*module/upgrade-all`(admin): 그 모듈을 비기본 버전으로 고정한 모든 stage 에 위를 순차 실행. 결과 표(성공/실패/에러)를 반환. **실패한 stage 는 그대로 둔다.**
- 플러그인 응답(`ListPlugins`/`GetPlugin`)에 `pinned_behind: [{module_path, pinned, default}]` 추가.
- **검증**: 핸들러 테스트(빌드 대역 주입).

### W6. web-ui

- `services/moduleApi.ts`: `versions`, `usage`, `single_version_only`, 버전 추가/retire/upgrade-all 호출 추가.
- `NativeStageEditor.tsx:116~147` 모듈 패널: 모듈별 "기본 vX" 와 이 stage 의 고정 "vY" 를 나란히 표시. 다르면 배지 + "기본 버전으로 시도" 버튼(W5 API). 결과 에러 원문 표시.
- `Plugins.tsx` 목록: `pinned_behind` 가 있으면 카운트 배지.
- 관리 화면(모듈 레지스트리): 버전 목록·사용 수·기본 지정·retire·`single_version_only` 토글·upgrade-all.
- i18n(en/ko) 키 추가.
- **검증**: 컴포넌트 테스트 + 브라우저 확인(로컬 K8s, `make e2e-up` 은 필요할 때만).

### W7. 문서·정리

- `ARCHITECTURE.md` §3 "Native tier" 한 줄에 "stage 별 의존성 버전 고정, 비기본 버전은 fork 링크" 추가.
- `archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md` 상단에 "D4 는 ADR-0005 로 부분 대체됨" 배너.
- ADR-0005 상태 `Proposed` → `Accepted`.
- `plugins.go_mod` 컬럼 제거 여부 결정(별도).
- 이 문서를 `archive/` 로 이동.

## 5. e2e 시나리오 (로컬 K8s)

1. 모듈 `github.com/google/uuid` 기본 `v1.6.0`, 버전 목록에 `v1.3.0` 추가.
2. stage A: `uuid.NewString()` 사용, `dep_versions={uuid: v1.3.0}` 으로 저장(API 로 직접 지정).
   stage B: 같은 코드, 기본(v1.6.0).
3. 빌드 → `runner_versions.forked_modules` 에 uuid v1.3.0 한 건, 빌드 로그에 fork 1개.
4. 워크플로 하나에 A·B 를 모두 넣고 실행 → 두 stage 모두 정상 출력.
5. 다중 패키지 검증: `github.com/go-resty/resty/v2` 를 `v2.7.0` / 최신 두 버전으로 같은 절차. 같은 메이저 경로에서의 마이너 공존 확인.
6. `POST /plugins/A/upgrade-deps` → 성공 시 `dep_versions` 가 v1.6.0 으로, 다음 빌드에서 fork 0개.
7. 음성 케이스: `single_version_only=true` 모듈에 비기본 고정 시 저장 400.

## 6. 되돌리기

- W2 까지는 동작 변경이 없다(불변식 2).
- W3 이후 문제가 나면 모든 stage 의 `dep_versions` 를 기본 버전으로 맞추는 SQL 한 번으로 fork 가 0 이 되어 현행 동작으로 돌아간다. 코드 롤백 없이 데이터만으로 복귀 가능.

## 7. 규모 (추정)

| 단계 | 규모 |
|---|---|
| W1 | 소 — 모델 3개 필드·테이블 1개·핸들러 2개 추가 |
| W2 | 중 — 네 곳의 go.mod 생성 일원화. 테스트 이동 포함 |
| W3 | 중 — 복사·재작성·해시. 통합 테스트 fixture 작성이 절반 |
| W4 | 소 |
| W5 | 소~중 — 기존 임시 빌드 절차 재사용 |
| W6 | 중 — 화면 3곳 |
| W7 | 소 |

## 8. 인수인계 (다음 세션 시작점)

### 8.1 완료된 것 (W1, 테스트 통과)

| 파일 | 변경 | 목적 |
|---|---|---|
| `control-plane/pkg/models/models.go` | `Plugin.DepVersions`(JSON, stage 별 고정 버전), `AllowedModule.SingleVersionOnly`, 신규 `AllowedModuleVersion`(복합 PK, **soft-delete 없음**) | 데이터 모델. `Version` 은 "기본 버전" 으로 의미 변경(주석 반영). `RunnerVersion.ForkedModules` 는 **아직 추가 안 함**(W3 에서) |
| `control-plane/pkg/database/database.go` | AutoMigrate 목록에 `AllowedModuleVersion`, `BackfillModuleVersions()` 멱등 백필 | 기존 모듈의 기본 버전을 보유 버전 행으로 이관 |
| `control-plane/internal/api/handlers/module_handler.go` | `ListModules` 응답을 `ModuleView{AllowedModule, Versions[], Usage{version→stage수}}` 로. `CreateModule`/`UpdateModule` 이 보유 버전 행을 함께 보장. `UpdateModule` 에 `single_version_only` 필드. 신규 `AddModuleVersion`(POST /module-versions), `RetireModuleVersion`(DELETE /module-versions?module_path=&version=) | 다중 버전 레지스트리 API. 폐기는 기본 버전·고정 stage 존재 시 거부(409 에 stage 이름 목록) |
| `control-plane/internal/api/routes.go` | `/module-versions` 그룹(admin) | `/modules/*module` catch-all 뒤에 세그먼트를 못 붙여 별도 그룹 |
| `control-plane/internal/api/handlers/module_handler_test.go` | sqlite + httptest GOPROXY 대역. 백필 멱등, 생성/기본변경/버전추가/폐기 가드/재추가/usage 집계 6종 | |

**계획 대비 달라진 점**
- 버전 API 경로: 계획의 `POST /modules/*module/versions` 는 Gin catch-all 제약으로 불가 → `/module-versions` + body/query.
- `AllowedModuleVersion` 에 `DeletedAt` 을 두지 않음(폐기 후 재추가 시 복합 PK 충돌 방지). 폐기 = 물리 삭제.
- `SingleVersionOnly` 가드를 `AddModuleVersion` 에도 둠(기본 외 버전 추가 자체를 거부). stage 저장 시 가드는 W2 `ResolvePins` 에 구현됨(비기본 고정이면 400).

### 8.1b W2 완료분 (테스트 통과, lint 0)

| 파일 | 변경 | 계획 대비 |
|---|---|---|
| 신규 `control-plane/internal/dependency/{pins,forkpath,gomod}.go` | `Pins`/`ResolvePins`/`ParseImports`/`OwningModule`/`Defaults`/`Fingerprint`, `ForkPath`·`ForkDirName`, `PluginGoMod`·`MainRequireBlock`·`TestGoMod`·`WorkspaceGoMod`·`CollectForks` | `rewrite.go`·`source.go` 는 W3 에서(아직 fork 를 만들지 않으므로) |
| `handlers/stage_import_validation.go` | `validateStageImports`/`parseImportPaths`/`isStdlibImport`/`isCoveredByAllowedModule`/`buildTestGoMod` 삭제 → `resolveStagePins`·`resolveAndEncodePins`·`pinsByPluginName`·`activeAllowedModules` 래퍼만 | 검증(D5)과 버전 확정이 같은 규칙이라 한 함수로 합침 |
| `handlers/plugin_handler.go` | Create/upsert/Update 세 지점이 pins 를 확정해 `DepVersions` 저장. `TestNativePlugin` 은 `dependency.TestGoMod(pins, sdkPath)` + `plugin_name` 으로 기존 고정값 조회 | |
| `lsp/{proxy,workspace_manager}.go`, `handlers/lsp_handler.go`, `web-ui/src/services/lspClient.ts` | `GetOrCreate`/`SyncSource` 가 pins 를 받고 `dependency.WorkspaceGoMod` 사용. WebSocket 쿼리에 `plugin_name` 추가 | **계획과 다름**: web-ui 는 `/lsp/sync` 를 호출하지 않고 WebSocket 만 쓴다. workspace go.mod 를 만드는 실질 경로는 `GetOrCreate` 뿐이라 거기에 pins 를 넣었다 |
| `builder/deps.go`(신규) | `resolvedDeps`(Names/Pins/Defaults/Forks/Backfill), `resolveDeps`, `saveBackfilledPins` | 레거시(빈 `DepVersions`)는 빌드 시 기본 버전으로 pins 생성 → **빌드 성공 후** 저장 |
| `builder/runner_builder.go` | `generatePluginGoMod`/`pluginRequireBlock` 삭제, `appendPluginRequires` 는 `dependency.MainRequireBlock` 호출. `CombinedSourceHash(hashes, coreHash, depsFingerprint)` 3-인자화 + `PluginDepFingerprint` | `resolveDeps` 는 **빌드 스킵 판정 뒤**에 둔다(스킵 경로에서 전 stage 파싱 낭비 회피) |
| `services/runner_resolver.go` | `coreChangedSince` 도 같은 `PluginDepFingerprint` 사용 | 빌더·리졸버 해시 정합 유지 |
| 테스트 | `dependency/dependency_test.go` 18종(golden·단사성·정책), `builder/deps_test.go` 6종 | `TestPluginRequireBlock`·`TestGeneratePluginGoMod`·`stage_import_validation_test.go` 는 `dependency` 로 이동 |

**W2 에서 내린 판단 (W3 이 이어받을 전제)**
- `ForkDirName` 은 mangle 만으로 단사가 아니다(`resty/v2` 와 `resty_v2` 충돌 — 테스트로 잡힘). 원본 (path, version) sha256 앞 10자를 접미사로 붙여 단사성을 해시가 보장한다.
- 재빌드 판정 지문(`dependency.Fingerprint`)은 **기본 버전을 참조하지 않는다.** 빌더와 리졸버가 DB 의 `dep_versions` 만으로 같은 값을 얻어야 하기 때문. 고정값이 하나도 없으면 빈 문자열이라 기존 ready 버전이 무효화되지 않는다(불변식 2).
- go.mod 의 require 목록이 "허용 모듈 전부" 에서 "그 stage 가 실제 import 하는 모듈" 로 좁아졌다. `go mod tidy` 가 어차피 정리하므로 최종 산출물은 같지만, tidy 이전 텍스트는 다르다.
- 해소 불가한 레거시 stage(retire 된 모듈 import 등)는 **빌드를 세우지 않고** 경고 후 고정 버전 없이 진행한다. stage 하나가 전체 배포를 막지 않게 하기 위함.

### 8.1c W3 완료분 (테스트 통과, lint 0)

| 파일 | 변경 |
|---|---|
| 신규 `dependency/rewrite.go` | `RewriteModuleTree`(복사본 자기참조 import + go.mod module 줄), `RewriteStageImports`(stage import → fork 경로 + alias 자동), `PackageNameInDir`. 치환은 go/parser 위치 기반 바이트 교체라 **파일 서식이 보존**된다 |
| 신규 `dependency/source.go` | `ModuleSource` 인터페이스 + `GoModDownloadSource`(`go mod download -json` 의 Dir) |
| `models.go` | `RunnerVersion.ForkedModules`(JSON) + `RunnerVersionMetaColumns` 에 등록(가벼운 컬럼이라 목록 조회 포함) |
| `builder/deps.go` | `materializeForks`(캐시 Dir → copyDir → `makeTreeWritable` → `RewriteModuleTree`), `stageSourceFor`(stage import 재작성), `resolveDeps` 에 `SingleVersionOnly` fork 거부 |
| `builder/runner_builder.go` | `buildInTempDir` 에 fork 단계 삽입(플러그인 소스 배치 직전), `version.ForkedModules` 기록, `RunnerBuilder.moduleSource` 이음매 추가 |
| `builder/testdata/foo_v1,foo_v2` + `fork_integration_test.go` | `//go:build integration`. 두 버전을 fork 로 함께 링크해 **실제 go build + 실행**, 출력이 `old\|new` 인지 확인 |

**W3 에서 확인한 것**
- 통합 테스트는 진짜로 함정을 잡는다: `RewriteModuleTree` 호출을 빼고 돌리면 실패한다(검증함).
- `go mod download <path>@<ver>` 는 모듈 컨텍스트 안에서 실행해도 그 go.mod 를 수정하지 않는다(실측). 그래서 fork 단계를 require 추가·tidy **앞**에 둘 수 있다.
- `-x` 플래그는 뺐다. `runCommand` 가 stdout/stderr 를 한 버퍼에 합치는데 `-x` 는 `#` 로 시작하는 줄을 대량으로 내보내 JSON 파싱을 불필요하게 위태롭게 한다.
- fork 디렉토리 이름은 `dependency.ForkDirName` 이 유일하게 만든다(빌더·재작성·go.mod 세 곳이 같은 함수를 부른다).

**실행 검증은 아직 안 했다** — 단위·통합 테스트까지다. 로컬 K8s e2e(§5)는 W4 이후.

### 8.1d W4 완료분 (테스트 통과, lint 0)

| 파일 | 변경 |
|---|---|
| `pipeline-runner/cmd/runner/main.go` | `CONDUIX_INIT_CHECK=1` 이면 설정 로드 전에 `os.Exit(0)`. 이 시점엔 `registry_custom.go` 포함 모든 `init()` 이 끝나 있어 중복 등록은 그 전에 panic 으로 터진다 |
| `pipeline-runner/cmd/runner/init_check_test.go` | 점검 플래그면 설정 없이 exit 0, 플래그 없으면 여전히 설정 요구. 러너 빌드는 `sync.Once` 로 한 벌만(30초 → 15초) |
| `builder/deps.go` | `runInitCheck`: fork 가 있을 때만 실행. 배포 타깃 ≠ 호스트면 호스트 타깃으로 한 벌 더 빌드해 실행, **그 재빌드 실패는 점검 생략**(배포 산출물은 이미 성공했으므로 막지 않는다). 실패 메시지에 fork 목록 + panic 첫 줄 + 안내 |
| `builder/runner_builder.go` | `RunnerBuilderConfig.InitCheck`(`auto`/`off`), go build 성공 직후 호출 |
| `builder/fork_integration_test.go` | 같은 드라이버 이름을 두 패키지가 `sql.Register` 하는 최소 재현으로, **빌드는 성공하고 init 에서 panic** 하는 것을 확인 |

### 8.2 남은 작업 — 무엇을 왜 고치는가

아래는 §3·§4 의 요약이다. 상세 지점(파일:라인)은 해당 절을 본다.

| 순서 | 파일 / 신규 | 할 일 | 목적 |
|---|---|---|---|
| W5-1 | `handlers/plugin_handler.go` 신규 `UpgradeDeps` (POST /plugins/:id/upgrade-deps) | 기본 버전 pins 로 `TestNativePlugin` 의 임시 빌드(컴파일만) → 성공 시 `DepVersions` 갱신 + `createRevision` | stage 소유자의 명시적 수렴 경로 |
| W5-2 | `handlers/module_handler.go` 신규 `UpgradeAll` (POST /module-versions/upgrade-all) | 그 모듈 비기본 고정 stage 전부에 W5-1 순차 실행, 결과 표 반환, 실패는 그대로 둠 | admin 일괄 수렴 |
| W5-3 | `handlers/plugin_handler.go` `ListPlugins`/`GetPlugin` | 응답에 `pinned_behind: [{module_path, pinned, default}]` | UI 배지 재료 |
| W6-1 | `web-ui/src/services/moduleApi.ts` | `ModuleView` 타입(versions/usage/single_version_only), addVersion/retireVersion/upgradeAll/upgradeDeps 호출 | |
| W6-2 | `web-ui/src/components/NativeStageEditor/NativeStageEditor.tsx:116~147` | 모듈 패널에 "기본 vX / 이 stage vY", 다르면 배지 + "기본 버전으로 시도" 버튼 | |
| W6-3 | `web-ui/src/pages/Plugins.tsx` | `pinned_behind` 카운트 배지 | |
| W6-4 | 관리 화면(신규 또는 Plugins.tsx 탭) | 버전 목록·사용 수·기본 지정·폐기·`single_version_only` 토글·upgrade-all | |
| W6-5 | `web-ui/src/i18n/{en,ko}` | 키 추가 | |
| W7 | `ARCHITECTURE.md` §3, `archive/CUSTOM_STAGE_DEPENDENCY_REGISTRY.md` 배너, ADR-0005 → Accepted, 이 문서 → archive | 문서 정합 |

### 8.3 다음 세션 첫 명령

```bash
git checkout feat/dep-version-coexistence
cd control-plane && go test ./... -count=1                          # W1~W3 회귀
go test -tags integration ./internal/builder/       # fork 공존 + init 자가점검
# W5-1 시작: handlers/plugin_handler.go 에 POST /plugins/:id/upgrade-deps
```
