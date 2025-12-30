// 삭제 모드
export type DeleteMode = 'physical' | 'soft' | 'ignore'

// 논리 삭제 필드 타입
export type SoftDeleteFieldType = 'timestamp' | 'boolean' | 'status' | 'custom'

// 삭제 감지 방법
export type DeleteDetectionMethod = 'null_body' | 'event_type' | 'flag_field'

// 논리 삭제 설정
export interface SoftDeleteConfig {
  field_type: SoftDeleteFieldType
  field_name: string
  delete_value?: string
  active_value?: string
}

// 삭제 감지 설정
export interface DeleteDetectionConfig {
  method: DeleteDetectionMethod
  flag_field?: string
  flag_value?: string
  event_type_field?: string
  delete_event_type?: string
}

// 삭제 전략
export interface DeleteStrategy {
  mode: DeleteMode
  soft_delete?: SoftDeleteConfig
  detection?: DeleteDetectionConfig
}

// 데이터 유형 필드
export interface DataTypeField {
  name: string
  type: string // string, int, float, bool, datetime, json
  required?: boolean
  description?: string
}

// 데이터 유형 스키마
export interface DataTypeSchema {
  type: 'json_schema' | 'avro' | 'infer'
  definition?: string
  fields?: DataTypeField[]
}

// 데이터 유형 저장소 설정
export interface DataTypeStorage {
  type: string // elasticsearch, postgresql, s3, etc.
  config?: Record<string, unknown>
}

// 사전작업 타입
export type PreworkType = 'sql' | 'http' | 'elasticsearch' | 's3' | 'script'

// 사전작업 실행 시점
export type PreworkExecutionPhase = 'data_type' | 'pipeline' | 'manual'

// 사전작업 상태
export type PreworkStatus = 'pending' | 'running' | 'completed' | 'failed'

// 데이터 유형 사전작업
export interface DataTypePrework {
  id: string
  data_type_id: string
  name: string
  description?: string
  type: PreworkType
  phase: PreworkExecutionPhase
  order: number
  config: Record<string, unknown>
  status: PreworkStatus
  executed_at?: string
  executed_by?: string
  error_msg?: string
  created_at: string
  updated_at: string
}

// 데이터 유형
export interface DataType {
  id: string
  name: string
  display_name: string
  description?: string
  category?: string
  delete_strategy?: DeleteStrategy
  id_fields?: string[]
  schema?: DataTypeSchema
  storage?: DataTypeStorage
  preworks?: DataTypePrework[]
  created_by?: string
  created_at: string
  updated_at: string
}

// 삭제 전략 프리셋
export interface DeleteStrategyPreset {
  id: string
  name: string
  display_name: string
  description?: string
  strategy: DeleteStrategy
  is_default?: boolean
  is_system?: boolean
  created_at: string
}

// 데이터 유형 카테고리
export interface DataTypeCategory {
  id: string
  name: string
  description: string
}

// 데이터 유형 생성 요청
export interface CreateDataTypeRequest {
  name: string
  display_name: string
  description?: string
  category?: string
  delete_strategy?: DeleteStrategy
  id_fields?: string[]
  schema?: DataTypeSchema
  storage?: DataTypeStorage
  preworks?: CreatePreworkRequest[]
}

// 사전작업 생성 요청
export interface CreatePreworkRequest {
  name: string
  description?: string
  type: PreworkType
  phase: PreworkExecutionPhase
  order?: number
  config: Record<string, unknown>
}

// SQL 사전작업 설정
export interface PreworkSQLConfig {
  connection_id: string
  statements: string[]
  use_transaction?: boolean
  rollback_on_error?: boolean
}

// HTTP 사전작업 설정
export interface PreworkHTTPConfig {
  method: string
  url: string
  headers?: Record<string, string>
  body?: string
  expected_status?: number[]
}

// Elasticsearch 사전작업 설정
export interface PreworkElasticsearchConfig {
  cluster_id: string
  action: 'create_index' | 'put_mapping' | 'create_template'
  index_name?: string
  body?: string
}

// 기본 삭제 전략 프리셋 ID
export const DEFAULT_DELETE_STRATEGY_PRESETS = {
  PHYSICAL: 'physical',
  SOFT_TIMESTAMP: 'soft_timestamp',
  SOFT_BOOLEAN: 'soft_boolean',
  SOFT_STATUS: 'soft_status',
  CDC_SOFT_TIMESTAMP: 'cdc_soft_timestamp',
  IGNORE: 'ignore',
} as const

// 카테고리 ID
export const DATA_TYPE_CATEGORIES = {
  MASTER: 'master',
  TRANSACTION: 'transaction',
  LOG: 'log',
  METRIC: 'metric',
  REFERENCE: 'reference',
} as const
