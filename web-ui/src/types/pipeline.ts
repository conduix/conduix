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

// 변환 단계 (레거시, Stage로 대체)
export interface TransformStep {
  name: string
  type: string // remap, filter, sample, aggregate
  config: Record<string, unknown>
}

// Stage 타입 정의
export type StageType =
  | 'filter'    // 조건 필터링
  | 'remap'     // 필드 이름 변경
  | 'drop'      // 필드 삭제
  | 'merge'     // 여러 필드를 하나로 합치기
  | 'split'     // 정규식으로 필드 분리
  | 'encrypt'   // 필드 암호화
  | 'dedupe'    // 중복 제거
  | 'default'   // 기본값 설정
  | 'cast'      // 타입 변환
  | 'timestamp' // 타임스탬프 처리
  | 'validate'  // 스키마 검증
  | 'sink'      // 추가 출력

// Stage 인터페이스
export interface Stage {
  id: string           // 프론트엔드용 고유 ID
  name: string
  type: StageType
  config: Record<string, unknown>
}

// Stage 타입별 설정 스키마
export interface FilterStageConfig {
  condition: string    // VRL 조건식
}

export interface RemapStageConfig {
  mappings: Record<string, string>  // source_field -> target_field
  drop_unmapped?: boolean
}

export interface DropStageConfig {
  fields: string[]     // 삭제할 필드 목록
}

export interface MergeStageConfig {
  source_fields: string[]   // 합칠 필드들
  target_field: string      // 결과 필드 이름
  delimiter?: string        // 구분자 (기본: " ")
  template?: string         // 템플릿 (예: "{{first_name}} {{last_name}}")
}

export interface SplitStageConfig {
  source_field: string      // 분리할 필드
  pattern: string           // 정규식 패턴
  target_fields: string[]   // 결과 필드 이름들 (그룹 순서대로)
  keep_original?: boolean   // 원본 필드 유지 여부
}

export interface EncryptStageConfig {
  fields: string[]          // 암호화할 필드들
  method: 'aes256' | 'sha256' | 'sha512' | 'bcrypt' | 'mask'  // 암호화 방식
  key_env?: string          // 암호화 키 환경변수 이름 (AES용)
  mask_char?: string        // 마스킹 문자 (mask용, 기본: "*")
  mask_keep_first?: number  // 앞에서 유지할 문자 수
  mask_keep_last?: number   // 뒤에서 유지할 문자 수
}

export interface DedupeStageConfig {
  key_fields: string[]      // 중복 판단 키 필드들
  strategy: 'keep_first' | 'keep_last' | 'keep_latest'  // 중복 시 유지 전략
  window?: number           // 시간 윈도우 (초, 실시간용)
  timestamp_field?: string  // keep_latest용 타임스탬프 필드
}

export interface DefaultStageConfig {
  defaults: Record<string, unknown>  // 필드별 기본값 { field: default_value }
  only_null?: boolean       // true: null만, false: null과 빈 문자열 모두
}

export interface CastStageConfig {
  casts: Record<string, string>  // 필드별 타입 { field: "int" | "float" | "string" | "bool" | "date" }
  date_format?: string      // 날짜 파싱 포맷 (예: "2006-01-02T15:04:05Z07:00")
  error_action?: 'drop' | 'null' | 'keep'  // 변환 실패 시 처리
}

export interface TimestampStageConfig {
  action: 'add' | 'convert' | 'format'  // 동작 유형
  target_field: string      // 결과 필드
  source_field?: string     // 변환/포맷 시 소스 필드
  timezone?: string         // 타임존 (예: "Asia/Seoul", "UTC")
  input_format?: string     // 입력 포맷 (convert용)
  output_format?: string    // 출력 포맷 (format용)
}

export interface ValidateStageConfig {
  schema: Record<string, unknown>
  drop_on_fail?: boolean
}

export interface SinkStageConfig {
  type: string         // elasticsearch, kafka, etc.
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
  transforms?: TransformStep[]  // 레거시
  stages?: Stage[]              // 새로운 Stage 배열
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
