# Stage Schema 사용자 가이드

> Stage를 쉽게 추가하고 관리하기 위한 Schema 기반 시스템

## 개요

Conduix의 Stage Schema 시스템은 **Go 코드에서 정의한 Stage 메타데이터를 기반으로 GUI를 자동 생성**합니다.

### 장점

| 기존 방식 | Schema 기반 방식 |
|-----------|------------------|
| Go 코드 + React 폼 각각 수정 | Go Schema만 정의하면 GUI 자동 생성 |
| Stage마다 수백 줄의 폼 코드 | 10~30줄의 Schema 정의 |
| 타입 불일치 위험 | 단일 소스 (Single Source of Truth) |

---

## 새 Stage 추가하기

### 1단계: Go에서 Schema 정의

`pipeline-core/pkg/stream/stage_registry.go`에 Schema 함수 추가:

```go
func MyNewStageSchema() types.StageSchema {
    return types.StageSchema{
        Type:        "my_new_stage",      // 고유 타입명
        DisplayName: "My New Stage",      // UI 표시명
        Description: "이 Stage는 ...",    // 설명
        Category:    types.CategoryTransform,  // 카테고리
        Icon:        "AutoFixHigh",       // Material UI 아이콘
        Color:       "#2196f3",           // 테마 색상
        Fields: []types.StageFieldSchema{
            {
                Name:        "input_field",
                Type:        types.FieldTypeString,
                DisplayName: "입력 필드",
                Description: "처리할 필드명",
                Required:    true,
                Placeholder: "field_name",
            },
            {
                Name:        "mode",
                Type:        types.FieldTypeEnum,
                DisplayName: "모드",
                Default:     "fast",
                Options: []types.FieldOption{
                    {Value: "fast", Label: "빠른 처리"},
                    {Value: "accurate", Label: "정확한 처리"},
                },
            },
        },
    }
}
```

### 2단계: Registry에 등록

같은 파일의 `init()` 함수에 추가:

```go
func init() {
    // ... 기존 Stage들 ...
    StageRegistry.Register(MyNewStageSchema())
}
```

### 3단계: 완료!

서버 재시작 후 Web UI에서 새 Stage를 사용할 수 있습니다.

---

## 필드 타입 가이드

### 기본 타입

| 타입 | 설명 | GUI 렌더링 |
|------|------|-----------|
| `string` | 텍스트 | 텍스트 입력 |
| `number` | 숫자 (소수점) | 숫자 입력 |
| `integer` | 정수 | 숫자 입력 (step=1) |
| `boolean` | 참/거짓 | 스위치 |
| `enum` | 선택지 | 드롭다운 |

### 고급 타입

| 타입 | 설명 | GUI 렌더링 |
|------|------|-----------|
| `array` | 배열 | 태그 입력 (Chip) |
| `json` | JSON 객체 | 코드 에디터 |
| `code` | 코드/표현식 | 코드 에디터 |
| `secret` | 비밀번호 | 마스킹 입력 + 👁 토글 |
| `duration` | 시간 (30s, 5m) | 텍스트 입력 |
| `keyvalue` | 키-값 쌍 | 동적 테이블 |

---

## 고급 기능

### 조건부 필드 표시

다른 필드 값에 따라 필드 표시/숨김:

```go
{
    Name:        "key_env",
    Type:        types.FieldTypeString,
    DisplayName: "암호화 키 환경변수",
    // method가 "aes256"일 때만 표시
    ShowWhen: &types.FieldCondition{
        Field:    "method",
        Operator: "eq",
        Value:    "aes256",
    },
}
```

**지원 연산자:**
- `eq`: 같음
- `neq`: 다름
- `in`: 배열에 포함
- `exists`: 값 존재

### 연결 테스트 버튼

DB, Elasticsearch 등 연결 테스트 UI 자동 생성:

```go
{
    Name:        "connection_string",
    Type:        types.FieldTypeSecret,
    DisplayName: "연결 문자열",
    TestConnection: &types.TestConnectionConfig{
        Endpoint: "/utils/test-db-connection",
        Fields:   []string{"connection_string"},
        Label:    "연결 테스트",
    },
}
```

### 커스텀 에디터

복잡한 UI가 필요한 경우 React 컴포넌트 지정:

```go
func ContractStageSchema() types.StageSchema {
    return types.StageSchema{
        Type:         "contract",
        DisplayName:  "Contract",
        Category:     types.CategoryValidation,
        CustomEditor: "ContractStageEditor",  // React 컴포넌트명
        Fields:       []types.StageFieldSchema{},  // 빈 배열 (커스텀 에디터가 담당)
    }
}
```

---

## 카테고리

| 카테고리 | 설명 | 예시 Stage |
|----------|------|-----------|
| `transform` | 데이터 변환 | filter, remap, drop, merge |
| `validation` | 데이터 검증 | validate, contract |
| `output` | 데이터 출력 | sql, elasticsearch, kafka |
| `control` | 흐름 제어 | throttle, sample, route |

---

## 예제: SQL Output Stage

```go
func SQLOutputStageSchema() types.StageSchema {
    return types.StageSchema{
        Type:        "sql",
        DisplayName: "SQL Output",
        Description: "SQL 데이터베이스 출력",
        Category:    types.CategoryOutput,
        Icon:        "Storage",
        Color:       "#9c27b0",
        Fields: []types.StageFieldSchema{
            {
                Name:        "connection_string",
                Type:        types.FieldTypeSecret,
                DisplayName: "연결 문자열",
                Required:    true,
                Placeholder: "postgres://user:pass@localhost:5432/db",
                MonoSpace:   true,
                TestConnection: &types.TestConnectionConfig{
                    Endpoint: "/utils/test-db-connection",
                    Fields:   []string{"connection_string"},
                    Label:    "연결 테스트",
                },
            },
            {
                Name:        "table",
                Type:        types.FieldTypeString,
                DisplayName: "테이블",
                Required:    true,
            },
            {
                Name:        "batch_size",
                Type:        types.FieldTypeInteger,
                DisplayName: "배치 크기",
                Default:     100,
                Min:         pointerFloat64(1),
                Max:         pointerFloat64(10000),
            },
            {
                Name:        "upsert",
                Type:        types.FieldTypeBoolean,
                DisplayName: "Upsert 사용",
                Description: "중복 키 시 업데이트",
                Default:     true,
            },
            {
                Name:        "conflict_columns",
                Type:        types.FieldTypeArray,
                DisplayName: "충돌 컬럼",
                ShowWhen:    &types.FieldCondition{Field: "upsert", Operator: "eq", Value: true},
            },
        },
    }
}
```

---

## FAQ

### Q: 기존 Stage도 Schema로 전환해야 하나요?

A: 권장하지만 필수는 아닙니다. 기존 하드코딩된 폼도 계속 동작합니다. 새 Stage부터 Schema 방식을 사용하세요.

### Q: CustomEditor는 언제 사용하나요?

A: AND/OR 조건 빌더, 드래그앤드롭 UI 등 **표준 폼으로 표현하기 어려운 복잡한 UI**가 필요할 때 사용합니다.

### Q: 필드 유효성 검사는 어떻게 하나요?

A:
- `Required: true` - 필수 필드
- `Min/Max` - 숫자 범위
- `MinLength/MaxLength` - 문자열 길이
- `Pattern` - 정규식 패턴

### Q: 다국어 지원은?

A: 현재 Schema의 DisplayName/Description은 한국어로 작성합니다. 추후 i18n 키 기반으로 확장 가능합니다.
