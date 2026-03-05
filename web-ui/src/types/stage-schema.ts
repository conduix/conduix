// Stage Schema 타입 정의
// Go의 shared/types/stage_schema.go와 동기화

// Stage 카테고리
export type StageCategory = 'transform' | 'validation' | 'output' | 'control'

// 필드 타입
export type FieldType =
  | 'string'
  | 'number'
  | 'integer'
  | 'boolean'
  | 'enum'
  | 'array'
  | 'object'
  | 'json'
  | 'code'
  | 'keyvalue'
  | 'duration'
  | 'secret'

// 필드 옵션 (enum용)
export interface FieldOption {
  value: string
  label: string
  description?: string
}

// 조건부 표시 조건
export interface FieldCondition {
  field: string
  operator: 'eq' | 'neq' | 'in' | 'exists'
  value: unknown
}

// 연결 테스트 설정
export interface TestConnectionConfig {
  endpoint: string
  fields: string[]
  label?: string
  method?: string
}

// 필드 스키마
export interface StageFieldSchema {
  name: string
  type: FieldType
  display_name: string
  description?: string
  required?: boolean
  default?: unknown
  placeholder?: string

  // 타입별 옵션
  options?: FieldOption[]      // enum용
  min?: number                 // number 최소값
  max?: number                 // number 최대값
  min_length?: number          // string 최소 길이
  max_length?: number          // string 최대 길이
  pattern?: string             // 정규식 패턴
  multiline?: boolean          // textarea 사용
  rows?: number                // textarea 행 수
  monospace?: boolean          // 고정폭 폰트

  // 조건부 표시
  show_when?: FieldCondition

  // 중첩 필드 (object 타입용)
  fields?: StageFieldSchema[]

  // 배열 아이템 스키마 (array 타입용)
  item_schema?: StageFieldSchema

  // 커스텀 에디터
  custom_editor?: string

  // 연결 테스트 버튼
  test_connection?: TestConnectionConfig
}

// Stage 스키마
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

// 카테고리 메타데이터
export interface CategoryInfo {
  value: StageCategory
  label: string
  description: string
}

// 필드 타입 메타데이터
export interface FieldTypeInfo {
  value: FieldType
  label: string
  description: string
}
