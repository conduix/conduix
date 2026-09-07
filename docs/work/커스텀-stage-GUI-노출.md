# 작업 지시 — 커스텀 stage 를 GUI 에서 선택·설정 가능하게

작성 2026-09-07 / 상태: 미착수

## 한 줄 요약

**커스텀 stage 는 실행은 되지만 GUI 에 존재 자체가 보이지 않는다.**
빌트인 stage 와 동일하게 목록에 뜨고, 설정 폼이 자동 생성되고, 소스를 열람할 수 있어야 한다.

## 재현

1. 커스텀 stage 를 등록한다 (`POST /api/v1/plugins`, name=`geocode_kakao`, source_code 포함)
2. 빌드·배포까지 정상 완료 → 파이프라인 실행 시 **동작함**
3. GUI 워크플로 화면(`/workflows/<id>`)을 연다
4. **stage type 이 표시되지 않고, 무슨 동작을 하는지 알 수 있는 정보가 없다**
5. 새 파이프라인을 만들 때 **stage 목록에서 고를 수 없다**

실제 사례: `deploy/e2e/plugins/geocode_kakao/` (527행, 주소→좌표 변환)

---

## 원인 — 4개 계층이 모두 비어 있다

### ① DB 모델에 스키마 컬럼이 없다

`control-plane/pkg/models/models.go:598`

```go
type Plugin struct {
    Name        string  // geocode_kakao
    Type        string  // native | script
    SourceCode  string  // Go 소스 (mediumtext)
    Description string  // 설명 한 줄
    ...
    // ← 설정 필드 스키마를 담을 컬럼이 없다
}
```

빌트인은 `types.StageSchema{Type, DisplayName, Category, Icon, Fields[]...}` 를 코드로 갖는데,
플러그인은 이에 대응하는 저장소가 없다.

### ② plugin-sdk 에 스키마 선언 수단이 없다

`plugin-sdk/native_stage.go`

```go
type NativeStage interface {
    Init(config map[string]any) error
    Process(record map[string]any) (map[string]any, error)
    ProcessBatch(records []map[string]any) ([]map[string]any, error)
    Close() error
}
```

플러그인 작성자가 "내 stage 는 이런 설정 필드를 받는다" 를 선언할 방법이 없다.

### ③ 목록 API 가 DB 를 보지 않는다

`control-plane/internal/api/handlers/stage_handler.go`

```go
func (h *StageHandler) ListAllStages(c *gin.Context) {
    builtinSchemas := stream.StageRegistry.All()   // ← 빌트인만
    ...
    response := AllStagesResponse{
        Builtin: builtinStages,                     // ← Custom 필드 자체가 없다
    }
}
```

라우트 주석(`routes.go:377`)에는 **"빌트인 + 플러그인 Stage 목록"** 이라고 적혀 있으나
구현은 빌트인만 한다. `plugins` 테이블을 조회하는 코드가 없다.

### ④ 개별 스키마 API 도 마찬가지

```go
func (h *StageHandler) GetStageSchema(c *gin.Context) {
    // 1. 빌트인 Stage에서 찾기
    if schema, ok := stream.StageRegistry.Get(stageType); ok { ... }
    // 찾지 못함
    middleware.ErrorResponseWithCode(c, http.StatusNotFound, ...)
}
```

`// 1.` 이라는 번호가 붙어 있는 것으로 보아 **2번(플러그인 조회)을 넣으려다 만 흔적**이다.
커스텀 stage type 으로 호출하면 404.

---

## 설계 결정이 필요한 지점

**스키마를 어디서 얻을 것인가** — 두 방식이 있고, 병행을 권한다.

| 방식 | 장점 | 단점 |
|---|---|---|
| A. 등록 API 로 받기 | 단순. 기존 플러그인도 스키마만 추가하면 됨 | 소스와 스키마가 어긋날 수 있다 (코드 고치고 스키마 안 고침) |
| B. 소스에서 추출 | 항상 일치 | 추출 파이프라인 필요 |

**권고: 둘 다 지원하되 B 우선.**
`plugin-sdk` 에 선택적 인터페이스를 두고, 구현했으면 그것을 정본으로 쓰고
없으면 등록 API 로 받은 값을 쓴다. **기존 플러그인이 깨지지 않는다.**

```go
// plugin-sdk — 선택적 구현
type SchemaProvider interface {
    Schema() types.StageSchema
}
```

> 이 결정은 구현팀이 판단할 사항이다. A 만으로도 목적은 달성된다.

---

## 작업 항목

### 1. DB — `plugins` 테이블에 `config_schema` 추가

**마이그레이션**: `control-plane/pkg/database/migrations/000007_add_plugin_config_schema.{up,down}.sql`
(현재 최신은 `000006_add_pipeline_links`)

```sql
-- up
ALTER TABLE plugins ADD COLUMN config_schema JSON NULL
  COMMENT 'types.StageSchema 직렬화 — GUI 설정 폼 생성용';

-- down
ALTER TABLE plugins DROP COLUMN config_schema;
```

**모델**: `control-plane/pkg/models/models.go` 의 `Plugin` 에 필드 추가

```go
ConfigSchema datatypes.JSON `gorm:"type:json" json:"config_schema,omitempty"`
```

`NULL` 허용 — 기존 플러그인은 스키마 없이도 목록에는 떠야 한다(타입·설명만 표시).

### 2. 등록 API — 스키마 수신

`control-plane/internal/api/handlers/plugin_handler.go`

- `CreatePluginRequest` / `UpdatePluginRequest` 에 `ConfigSchema` 추가
- 저장 전 **검증**: `types.StageSchema` 로 언마샬되는지, `Fields[].Type` 이
  알려진 `FieldType`(string/number/integer/boolean/enum/array/object/json/code/keyvalue/duration/secret)인지
- 검증 실패 시 400 — 잘못된 스키마가 저장되면 GUI 폼이 깨진다

### 3. 목록 API — 플러그인 포함

`stage_handler.go` 의 `ListAllStages`

```go
type AllStagesResponse struct {
    Builtin []BuiltinStageInfo `json:"builtin"`
    Custom  []CustomStageInfo  `json:"custom"`   // 신규
}
```

- `plugins` 테이블에서 `status='active'` 인 것을 조회해 `Custom` 에 담는다
- stage type = 플러그인 `Name` (등록 시 이 이름이 곧 stage type 이 된다)
- `config_schema` 가 없어도 목록에는 포함한다 — 타입·설명은 보여야 한다

**주의**: 빌트인과 커스텀의 **이름 충돌**을 어떻게 다룰지 정할 것.
빌트인 우선을 권하며, 충돌 시 등록 단계에서 거부하는 편이 낫다.

### 4. 개별 스키마 API — 2번 분기 추가

`stage_handler.go` 의 `GetStageSchema`

```go
// 1. 빌트인 (기존)
if schema, ok := stream.StageRegistry.Get(stageType); ok { ... }

// 2. 커스텀 플러그인 (신규)
//    - config_schema 있으면 → 빌트인과 동일한 형태로 반환
//    - 없으면 → type/display_name/description 만 담아 반환 (404 아님)
```

`convertFieldsToJSONSchema()`(`stage_handler.go:130`)를 그대로 재사용하면
빌트인과 동일한 응답 형태가 나온다.

### 5. GUI 가 쓰는 엔드포인트 확인 — **여기가 함정**

`web-ui/src/services/api.ts:733`

```ts
async getStageSchemas() {
  const response = await this.client.get('/stages/schemas')   // ← 이것
}
```

그런데 `routes.go:379` 는 이 경로를 **"빌트인만 (하위 호환성)"** 이라고 명시한다.

```go
stages.GET("", ListAllStages)               // 빌트인 + 플러그인 (주석만, 구현 안 됨)
stages.GET("/schemas", GetAllSchemas)       // 빌트인만 (하위 호환성)  ← GUI 가 쓰는 것
```

**3·4번만 고치면 GUI 는 여전히 커스텀을 못 본다.**
`GetAllSchemas` 도 함께 고치거나, GUI 를 `/stages` 로 옮겨야 한다.
어느 쪽이든 **양쪽을 함께 바꿔야 효과가 난다.**

### 6. web-ui 타입·렌더링

`web-ui/src/types/plugin.ts`

```ts
export interface StageListResponse {
  builtin: StageInfo[]
  custom: StageInfo[]      // 신규
}
```

- stage 선택 목록에 커스텀 섹션 추가 (빌트인과 구분해 표시하는 편이 낫다)
- `StageSchemaForm` 은 **수정 불필요** — 같은 `StageSchema` 형태로 오면 그대로 렌더된다
- `config_schema` 가 없는 플러그인은 폼 대신 "설정 스키마 미등록" 안내 + 원시 JSON 편집기로 폴백

### 7. 워크플로 상세 — 소스 열람

이미 `SourceCode` 가 DB 에 있다. 워크플로 화면에서 커스텀 stage 를 눌렀을 때
소스를 볼 수 있게 한다(읽기 전용). 무슨 동작을 하는지 확인할 수단이 이것뿐인 경우가 있다.

---

## 완료 기준

- [ ] 커스텀 stage 가 GUI stage 목록에 뜬다
- [ ] 새 파이프라인 작성 시 커스텀 stage 를 **선택**할 수 있다
- [ ] 선택하면 `config_schema` 기반 **설정 폼이 자동 생성**된다
- [ ] 워크플로 상세에서 stage type 과 설명이 보인다
- [ ] 워크플로 상세에서 소스를 열람할 수 있다
- [ ] `config_schema` 없는 기존 플러그인도 **목록에는 뜨고**, 폼은 JSON 폴백으로 동작한다
- [ ] `geocode_kakao` 로 위 항목을 실제 확인

## 검증 대상

`deploy/e2e/plugins/geocode_kakao/` — 이 stage 로 끝까지 확인할 것.
설정 필드는 다음과 같다(`stage.go:112` 의 `Init` 에서 확인):

| 필드 | 타입 | 기본값 | 비고 |
|---|---|---|---|
| `address_field` | string | — | **필수**. 없으면 Init 에러 |
| `lotno_field` | string | `""` | 지번주소 폴백 필드 |
| `api_key` | **secret** | `""` | 비우면 캐시 전용 모드(API 미호출) |
| `api_base_url` | string | `https://dapi.kakao.com` | |
| `max_retries` | integer | 3 | |
| `daily_quota` | integer | 100000 | 일일 호출 상한 |
| `rps` | number | 20 | 0 이하면 20 으로 보정 |
| `memory_cache_size` | integer | 50000 | |
| `cache_dsn` | **secret** | `""` | 비우면 영속 캐시 미사용 |
| `cache_driver` | string | `mysql` | `cache_dsn` 있을 때만 |
| `cache_table` | string | `geocode_cache` | `cache_dsn` 있을 때만 |

출력 필드는 고정이다(설정 불가): `lat`, `lon`, `geo_status`, `geo_addr`,
`geo_source`, `geo_match`.

이 stage 가 좋은 검증 대상인 이유:
- **필수 필드**(`address_field`) — 미입력 시 GUI 가 막아주는지
- **secret 2개**(`api_key`, `cache_dsn`) — 마스킹되는지
- **조건부 필드**(`cache_driver`/`cache_table` 은 `cache_dsn` 이 있을 때만 의미) —
  `StageFieldSchema.ShowWhen` 으로 표현되는지
- **기본값이 있는 숫자 필드** — 폼에 기본값이 채워지는지

---

## 참고

- Stage Schema 전체 구조: `docs/STAGE_SCHEMA_ARCHITECTURE.md`
- 타입 정의: `shared/types/stage_schema.go`
- 빌트인 등록 예시: `pipeline-core/pkg/stream/stage_registry.go`
- 플러그인 계약(struct 이름은 반드시 `Stage`): `control-plane/internal/builder/runner_builder.go:544`
