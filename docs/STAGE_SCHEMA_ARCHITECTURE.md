# Stage Schema 아키텍처 (Claude Code 참조용)

> Stage와 GUI를 연결하는 Schema 기반 자동 생성 시스템

## 디렉토리 구조

```
conduix/
├── shared/types/
│   └── stage_schema.go          # 📌 Schema 타입 정의 (핵심)
│
├── pipeline-core/pkg/stream/
│   └── stage_registry.go        # 📌 모든 Stage Schema 등록 (20+ stages)
│
├── control-plane/internal/api/
│   ├── handlers/
│   │   └── stage_handler.go     # API 핸들러
│   └── routes.go                # 라우트 등록 (/stages/*)
│
└── web-ui/src/
    ├── types/
    │   └── stage-schema.ts      # TypeScript 타입 (Go와 동기화)
    │
    ├── services/
    │   └── api.ts               # getStageSchemas() 등 API 함수
    │
    └── components/StageSchemaForm/
        ├── index.ts             # 모듈 export
        ├── useStageSchemas.ts   # React Hook (데이터 패칭)
        ├── StageSchemaForm.tsx  # 메인 폼 생성기
        ├── SchemaField.tsx      # 개별 필드 렌더링 (12+ 타입)
        └── StageSchemaDialog.tsx # Stage 추가/수정 다이얼로그
```

---

## 데이터 흐름

```
┌─────────────────────────────────────────────────────────────────┐
│                         Go Backend                               │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ pipeline-core/pkg/stream/stage_registry.go               │   │
│  │                                                          │   │
│  │  var StageRegistry = &stageRegistry{...}                 │   │
│  │                                                          │   │
│  │  func init() {                                           │   │
│  │      StageRegistry.Register(FilterStageSchema())         │   │
│  │      StageRegistry.Register(RemapStageSchema())          │   │
│  │      StageRegistry.Register(SQLOutputStageSchema())      │   │
│  │      StageRegistry.Register(ContractStageSchema())  // CustomEditor │
│  │      // ... 20+ stages                                   │   │
│  │  }                                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                              ▼                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ control-plane/internal/api/handlers/stage_handler.go     │   │
│  │                                                          │   │
│  │  func (h *StageHandler) GetAllSchemas(c *gin.Context) {  │   │
│  │      schemas := stream.StageRegistry.All()               │   │
│  │      c.JSON(200, schemas)                                │   │
│  │  }                                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                    GET /api/v1/stages/schemas                    │
└──────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                       React Frontend                             │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ components/StageSchemaForm/useStageSchemas.ts            │   │
│  │                                                          │   │
│  │  export function useStageSchemas() {                     │   │
│  │      const [data, setData] = useState<StageSchema[]>([]) │   │
│  │      useEffect(() => {                                   │   │
│  │          api.getStageSchemas().then(setData)             │   │
│  │      }, [])                                              │   │
│  │      return { data, isLoading }                          │   │
│  │  }                                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                              ▼                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ components/StageSchemaForm/StageSchemaForm.tsx           │   │
│  │                                                          │   │
│  │  if (schema.custom_editor) {                             │   │
│  │      // 커스텀 에디터 로드 (lazy import)                  │   │
│  │      const CustomEditor = customEditors[schema.custom_editor] │
│  │      return <CustomEditor value={value} onChange={onChange} /> │
│  │  }                                                       │   │
│  │                                                          │   │
│  │  // 일반 필드는 SchemaField로 자동 렌더링                 │   │
│  │  return schema.fields.map(field =>                       │   │
│  │      <SchemaField key={field.name} field={field} ... />  │   │
│  │  )                                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                              ▼                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ components/StageSchemaForm/SchemaField.tsx               │   │
│  │                                                          │   │
│  │  switch (field.type) {                                   │   │
│  │      case 'string':   return <TextField ... />           │   │
│  │      case 'number':   return <TextField type="number" /> │   │
│  │      case 'boolean':  return <Switch ... />              │   │
│  │      case 'enum':     return <Select ... />              │   │
│  │      case 'array':    return <Autocomplete multiple />   │   │
│  │      case 'json':     return <TextField multiline />     │   │
│  │      case 'secret':   return <TextField type="password" /> │  │
│  │      // ... 12+ 타입 지원                                │   │
│  │  }                                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 핵심 타입 정의

### Go: `shared/types/stage_schema.go`

```go
// StageSchema - Stage 메타데이터
type StageSchema struct {
    Type         string             `json:"type"`           // 고유 식별자
    DisplayName  string             `json:"display_name"`   // UI 표시명
    Description  string             `json:"description"`    // 설명
    Category     StageCategory      `json:"category"`       // transform|validation|output|control
    Icon         string             `json:"icon"`           // Material UI 아이콘명
    Color        string             `json:"color"`          // 테마 색상 (#hex)
    Fields       []StageFieldSchema `json:"fields"`         // 필드 목록
    CustomEditor string             `json:"custom_editor"`  // 커스텀 React 컴포넌트명
}

// StageFieldSchema - 개별 필드 정의
type StageFieldSchema struct {
    Name           string                `json:"name"`           // 필드명
    Type           FieldType             `json:"type"`           // string|number|enum|...
    DisplayName    string                `json:"display_name"`   // UI 표시명
    Description    string                `json:"description"`    // 도움말
    Required       bool                  `json:"required"`       // 필수 여부
    Default        any                   `json:"default"`        // 기본값
    Placeholder    string                `json:"placeholder"`    // 입력 힌트

    // 타입별 옵션
    Options        []FieldOption         `json:"options"`        // enum용 선택지
    Min/Max        *float64              `json:"min/max"`        // 숫자 범위
    Multiline      bool                  `json:"multiline"`      // textarea
    MonoSpace      bool                  `json:"monospace"`      // 고정폭 폰트

    // 조건부 표시
    ShowWhen       *FieldCondition       `json:"show_when"`      // 조건부 표시

    // 커스텀 UI
    CustomEditor   string                `json:"custom_editor"`  // 필드용 커스텀 에디터
    TestConnection *TestConnectionConfig `json:"test_connection"` // 연결 테스트 버튼
}

// FieldType 지원 타입
const (
    FieldTypeString   = "string"
    FieldTypeNumber   = "number"
    FieldTypeInteger  = "integer"
    FieldTypeBoolean  = "boolean"
    FieldTypeEnum     = "enum"
    FieldTypeArray    = "array"
    FieldTypeObject   = "object"
    FieldTypeJSON     = "json"
    FieldTypeCode     = "code"
    FieldTypeKeyValue = "keyvalue"
    FieldTypeDuration = "duration"
    FieldTypeSecret   = "secret"
)

// StageCategory 지원 카테고리
const (
    CategoryTransform  = "transform"   // 변환
    CategoryValidation = "validation"  // 검증
    CategoryOutput     = "output"      // 출력
    CategoryControl    = "control"     // 제어
)
```

### TypeScript: `web-ui/src/types/stage-schema.ts`

```typescript
// Go 타입과 1:1 매핑
export interface StageSchema {
    type: string
    display_name: string
    description?: string
    category: StageCategory
    icon?: string
    color?: string
    fields: StageFieldSchema[]
    custom_editor?: string
}

export interface StageFieldSchema {
    name: string
    type: FieldType
    display_name: string
    required?: boolean
    default?: unknown
    options?: FieldOption[]
    show_when?: FieldCondition
    test_connection?: TestConnectionConfig
    // ...
}

export type FieldType =
    | 'string' | 'number' | 'integer' | 'boolean'
    | 'enum' | 'array' | 'json' | 'code'
    | 'secret' | 'duration' | 'keyvalue'

export type StageCategory = 'transform' | 'validation' | 'output' | 'control'
```

---

## API 엔드포인트

| Method | Path | 설명 | Handler |
|--------|------|------|---------|
| GET | `/api/v1/stages/schemas` | 모든 Schema | `GetAllSchemas` |
| GET | `/api/v1/stages/schemas/:type` | 특정 Schema | `GetSchema` |
| GET | `/api/v1/stages/categories` | 카테고리 목록 | `GetCategories` |
| GET | `/api/v1/stages/categories/:category/schemas` | 카테고리별 | `GetSchemasByCategory` |
| GET | `/api/v1/stages/field-types` | 필드 타입 목록 | `GetFieldTypes` |

---

## 주요 컴포넌트 관계

```
StageSchemaDialog (Stage 추가/수정 다이얼로그)
├── useStageSchemas() → API 호출하여 Schema 목록 가져옴
├── Select (Stage 타입 선택)
└── StageSchemaForm (Schema 기반 폼 생성)
    ├── CustomEditor 체크
    │   └── YES → lazy import 후 커스텀 컴포넌트 렌더링
    │       예: ContractStageEditor, RouteRuleEditor
    └── NO → SchemaField 반복 렌더링
        ├── string  → TextField
        ├── number  → TextField[type=number]
        ├── boolean → Switch
        ├── enum    → Select + MenuItem
        ├── array   → Autocomplete[multiple, freeSolo]
        ├── json    → TextField[multiline, monospace]
        ├── code    → TextField[multiline, monospace]
        ├── secret  → TextField[type=password] + 👁 IconButton
        ├── duration → TextField + 도움말
        └── keyvalue → 동적 Key-Value 테이블
```

---

## 커스텀 에디터 등록

### 1. Go에서 CustomEditor 지정

```go
// pipeline-core/pkg/stream/stage_registry.go
func ContractStageSchema() types.StageSchema {
    return types.StageSchema{
        Type:         "contract",
        CustomEditor: "ContractStageEditor",  // 👈 React 컴포넌트명
        Fields:       []types.StageFieldSchema{},
    }
}
```

### 2. React에서 컴포넌트 매핑

```typescript
// web-ui/src/components/StageSchemaForm/StageSchemaForm.tsx
const customEditors: Record<string, LazyExoticComponent<...>> = {
    ContractStageEditor: lazy(() => import('../ContractStageEditor/ContractStageEditor')),
    RouteRuleEditor: lazy(() => import('../RouteRuleEditor/RouteRuleEditor')),
    // 새 커스텀 에디터 추가 시 여기에 등록
}
```

### 3. 커스텀 에디터 인터페이스

```typescript
interface CustomEditorProps {
    value: Record<string, unknown>   // Stage config
    onChange: (value: Record<string, unknown>) => void
    disabled?: boolean
}
```

---

## 새 Stage 추가 체크리스트

### 일반 Stage (Schema 기반 폼)

1. [ ] `pipeline-core/pkg/stream/stage_registry.go`에 Schema 함수 추가
2. [ ] `init()` 함수에서 `StageRegistry.Register()` 호출
3. [ ] 서버 재시작 → 자동으로 GUI 생성됨

### 커스텀 UI가 필요한 Stage

1. [ ] Schema에서 `CustomEditor: "MyCustomEditor"` 지정
2. [ ] `web-ui/src/components/MyCustomEditor/` 디렉토리 생성
3. [ ] React 컴포넌트 구현 (CustomEditorProps 인터페이스 준수)
4. [ ] `StageSchemaForm.tsx`의 `customEditors` 맵에 등록

---

## 조건부 필드 (`ShowWhen`) 처리

### Go 정의
```go
{
    Name:     "mask_char",
    ShowWhen: &types.FieldCondition{
        Field:    "method",
        Operator: "eq",
        Value:    "mask",
    },
}
```

### React 처리 (`SchemaField.tsx`)
```typescript
function checkCondition(condition: FieldCondition, values: Record<string, unknown>): boolean {
    const fieldValue = values[condition.field]

    switch (condition.operator) {
        case 'eq':  return fieldValue === condition.value
        case 'neq': return fieldValue !== condition.value
        case 'in':  return Array.isArray(condition.value) && condition.value.includes(fieldValue)
        case 'exists': return fieldValue !== undefined && fieldValue !== null
        default: return true
    }
}

// 사용
const isVisible = useMemo(() => {
    if (!field.show_when) return true
    return checkCondition(field.show_when, allValues)
}, [field.show_when, allValues])

if (!isVisible) return null
```

---

## 연결 테스트 버튼

### Go 정의
```go
{
    Name: "connection_string",
    TestConnection: &types.TestConnectionConfig{
        Endpoint: "/utils/test-db-connection",
        Fields:   []string{"connection_string"},
        Label:    "연결 테스트",
    },
}
```

### React 처리 (`SchemaField.tsx`)
```typescript
const handleTestConnection = async () => {
    const testData: Record<string, unknown> = {}
    for (const fieldName of field.test_connection.fields) {
        testData[fieldName] = allValues[fieldName]  // 필요한 필드들 수집
    }

    const response = await api.post(field.test_connection.endpoint, testData)
    setTestResult({
        success: response.data.success,
        message: response.data.message || response.data.error,
    })
}
```

---

## 관련 파일 빠른 참조

| 파일 | 목적 | 수정 시점 |
|------|------|----------|
| `shared/types/stage_schema.go` | 타입 정의 | 새 필드 타입 추가 시 |
| `pipeline-core/pkg/stream/stage_registry.go` | Schema 등록 | 새 Stage 추가 시 |
| `control-plane/internal/api/handlers/stage_handler.go` | API 핸들러 | 새 API 추가 시 |
| `web-ui/src/types/stage-schema.ts` | TS 타입 | Go 타입 변경 시 동기화 |
| `web-ui/src/components/StageSchemaForm/SchemaField.tsx` | 필드 렌더링 | 새 필드 타입 추가 시 |
| `web-ui/src/components/StageSchemaForm/StageSchemaForm.tsx` | 폼 생성 | 새 커스텀 에디터 등록 시 |
