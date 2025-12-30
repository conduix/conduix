// Pipeline related types

// 확장 모드: 자식 파이프라인 실행 방식
export type ExpansionMode = 'none' | 'for_each_record'

// 파라미터 바인딩: 부모 출력 → 자식 입력 매핑
export interface ParameterBinding {
  parent_field: string // 부모 출력 필드 (예: "id", "board_id")
  child_param: string  // 자식 파라미터 이름 (예: "board_id")
}

// 소스 설정
export interface WorkflowSource {
  type: string // kafka, cdc, rest_api, sql, file, sql_event
  name: string
  config: Record<string, unknown>
}

// 변환 단계
export interface TransformStep {
  name: string
  type: string // remap, filter, sample, aggregate
  config: Record<string, unknown>
}

// 싱크 설정
export interface WorkflowSink {
  type: string // elasticsearch, kafka, sql, mongodb, s3, rest_api
  name: string
  config: Record<string, unknown>
  condition?: string
}

// 워크플로우 내 파이프라인 정의
export interface WorkflowPipeline {
  id: string
  name: string
  description?: string
  priority: number
  depends_on?: string[]
  source: WorkflowSource
  transforms?: TransformStep[]
  sinks: WorkflowSink[]
  weight?: number

  // 계층형 파이프라인 필드
  parent_pipeline_id?: string | null     // 부모 파이프라인 ID
  target_data_type_id?: string | null    // 확장용 DataType ID
  expansion_mode?: ExpansionMode         // 자식 파이프라인 확장 모드
  parameter_bindings?: ParameterBinding[] // 부모→자식 파라미터 매핑
}

// 워크플로우
export interface Workflow {
  id: string
  project_id: string
  provider_id: string
  name: string
  slug: string
  description?: string
  type: 'batch' | 'realtime'
  execution_mode: 'parallel' | 'sequential' | 'dag'
  status: string
  schedule_enabled: boolean
  schedule_type?: string
  schedule_cron?: string
  schedule_interval?: string
  schedule_timezone?: string
  pipelines_config?: string // JSON string of WorkflowPipeline[]
  pipelines?: WorkflowPipeline[] // Parsed pipelines
  created_at: string
  updated_at: string
}
