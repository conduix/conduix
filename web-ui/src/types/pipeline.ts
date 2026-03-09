// Pipeline related types

// 확장 모드: 자식 파이프라인 실행 방식
export type ExpansionMode = 'none' | 'for_each_record'

// 파라미터 바인딩: 부모 출력 → 자식 입력 매핑
export interface ParameterBinding {
  parent_field: string // 부모 출력 필드 (예: "id", "board_id")
  child_param: string  // 자식 파라미터 이름 (예: "board_id")
}

// Rate Limit 설정
export interface RateLimitConfig {
  enabled: boolean
  rate: number              // 단위 시간당 처리량
  interval: 'second' | 'minute' | 'hour'  // 단위 시간
  burst?: number            // 버스트 허용량 (토큰 버킷)
  strategy?: 'token_bucket' | 'sliding_window' | 'fixed_window'  // 알고리즘
}

// REST API Pagination 타입
export type PaginationType =
  | 'none'            // 페이지네이션 없음
  | 'page_increment'  // 방식1: 호출자가 page 파라미터 증가, 데이터 없으면 종료
  | 'next_url'        // 방식2: 응답에서 다음 URL 추출, URL 없으면 종료
  | 'next_offset'     // 방식3: 응답에서 offset 추출하여 URL 생성, offset 없으면 종료
  | 'page_with_count' // 방식4: page 증가 + currentCount < perPage 면 종료

// 방식1: Page Increment 설정
export interface PageIncrementConfig {
  type: 'page_increment'
  param_name: string        // URL 파라미터명 (예: "page")
  start_value: number       // 시작값 (예: 1)
}

// 방식2: Next URL 설정
export interface NextUrlConfig {
  type: 'next_url'
  url_path: string          // 응답 JSON에서 다음 URL 경로 (예: "nextUrl", "links.next")
}

// 방식3: Next Offset 설정
export interface NextOffsetConfig {
  type: 'next_offset'
  offset_param: string      // URL 파라미터명 (예: "offset")
  offset_path: string       // 응답 JSON에서 다음 offset 경로 (예: "nextOffset")
}

// 방식4: Page with Count 설정
export interface PageWithCountConfig {
  type: 'page_with_count'
  page_param: string        // URL 파라미터명 (예: "page")
  start_value: number       // 시작값 (예: 1)
  current_count_path: string // 응답 JSON에서 현재 개수 경로 (예: "currentCount")
  per_page_path: string     // 응답 JSON에서 페이지당 개수 경로 (예: "perPage")
}

// Pagination 설정 Union 타입
export type PaginationConfig =
  | PageIncrementConfig
  | NextUrlConfig
  | NextOffsetConfig
  | PageWithCountConfig

// Input 설정 (데이터 입력 소스)
// Input 타입: kafka, cdc, rest_api, sql, file, sql_event
export interface WorkflowInput {
  type: string // kafka, cdc, rest_api, sql, file, sql_event, partitioned_http
  name: string
  config: Record<string, unknown>
  rate_limit?: RateLimitConfig  // 입력 레벨 rate limiting
  partition?: Record<string, unknown>   // 파티션 디스커버리 설정
  pagination?: Record<string, unknown>  // 페이지네이션 설정
}

// WorkflowSource는 WorkflowInput의 별칭 (하위 호환성)
// @deprecated WorkflowInput을 사용하세요
export type WorkflowSource = WorkflowInput

// 변환 단계 (레거시, Stage로 대체)
export interface TransformStep {
  name: string
  type: string // remap, filter, sample, aggregate
  config: Record<string, unknown>
}

// 실시간 파이프라인 모드
export type RealtimePipelineMode = 'raw' | 'cdc'

// 실시간 파이프라인 Target Model 매핑
export interface TargetModelMapping {
  model_id: string              // Data Model ID
  discriminator_value?: string  // discriminator_field 값 (라우팅용)
}

// Stage 타입 정의 (데이터 변환/처리)
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
  | 'throttle'  // 처리량 제한
  | 'validate'  // 스키마 검증
  | 'contract'  // Data Contract 검증 (비즈니스 규칙)
  | 'route'     // 이벤트 라우팅 (CDC용)
  | 'delete'    // 삭제 처리 (CDC용)

// Output 타입 정의 (데이터 출력/저장)
export type OutputType =
  | 'sql'           // SQL 데이터베이스
  | 'elasticsearch' // Elasticsearch
  | 'kafka'         // Kafka
  | 'mongodb'       // MongoDB
  | 's3'            // S3
  | 'rest_api'      // REST API
  | 'file'          // 파일

// Stage 인터페이스 (레거시 호환: Output도 stages 배열에 포함될 수 있음)
export interface Stage {
  id: string           // 프론트엔드용 고유 ID
  name: string
  type: StageType | OutputType  // 레거시 호환: Output 타입도 허용
  config: Record<string, unknown>
}

// Output 인터페이스
// Output 전용 변환 단계(pre_stages)를 포함할 수 있음
export interface Output {
  id: string                     // 프론트엔드용 고유 ID
  name: string
  type: OutputType
  pre_stages?: Stage[]           // Output 전용 변환 단계 (공통 Stage 이후 적용)
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

export interface ThrottleStageConfig {
  rate: number              // 처리량 (records per interval)
  interval: 'second' | 'minute' | 'hour'  // 단위 시간
  burst?: number            // 버스트 허용량 (초과 허용 수)
  strategy?: 'token_bucket' | 'sliding_window' | 'fixed_window'  // 알고리즘
  drop_on_limit?: boolean   // true: 초과 시 드랍, false: 대기
}

export interface ValidateStageConfig {
  schema: Record<string, unknown>
  drop_on_fail?: boolean
}

export interface OutputStageConfig {
  type: string         // elasticsearch, kafka, etc.
  config: Record<string, unknown>
}

// Route Stage: 이벤트 타입별 라우팅 (CDC용)
export interface RouteStageConfig {
  field: string                    // 라우팅 기준 필드 (예: "_op")
  routes: RouteDefinition[]        // 라우팅 규칙
}

export interface RouteDefinition {
  match: string[]                  // 매칭할 값들 (예: ["insert", "update"])
  target: string                   // 대상 스테이지/플로우 이름
}

// Delete Stage: 삭제 이벤트 처리 (CDC용)
export interface DeleteStageConfig {
  mode: 'physical' | 'logical'     // 삭제 방식
  logical?: {
    field: string                  // 삭제 마커 필드 (예: "deleted_at")
    value: string                  // 값 (예: "$now", "true")
  }
}

// Output 처리 모드
export type OutputMode = 'bulk' | 'individual'

// 배치 처리 설정
// Stage는 항상 병렬 처리, Output만 bulk/individual 선택
export interface PipelineBatchConfig {
  enabled: boolean
  output_mode?: OutputMode      // Output 처리 모드: bulk (배치 전송) 또는 individual (개별 전송)
  size?: number                 // 배치 크기 - 한 번에 처리할 레코드 수 (기본: 100)
  workers?: number              // Stage 병렬 워커 수 (기본: size와 동일, 최대 100)
  flush_interval?: string       // 시간 기반 플러시 주기 (기본: 5s)
}

// 워크플로우 내 파이프라인 정의
// 데이터 흐름: Input → [공통 Stage] → [Output별 PreStages] → Output
export interface WorkflowPipeline {
  id: string
  name: string
  description?: string
  priority: number
  depends_on?: string[]
  input?: WorkflowInput           // 데이터 입력 소스 (권장)
  transforms?: TransformStep[]    // 레거시
  stages?: Stage[]                // 공통 데이터 변환/처리 단계
  outputs?: Output[]              // 데이터 출력 대상 (각각 pre_stages 포함 가능)
  weight?: number

  // 실시간 파이프라인 모드 (realtime workflow에서만 사용)
  realtime_mode?: RealtimePipelineMode   // raw: 순수 데이터, cdc: Change Data Capture

  // 계층형 파이프라인 필드 (Batch용)
  parent_pipeline_id?: string | null     // 부모 파이프라인 ID
  target_data_type_id?: string | null    // 단일 DataType ID (Batch용)
  expansion_mode?: ExpansionMode         // 자식 파이프라인 확장 모드
  parameter_bindings?: ParameterBinding[] // 부모→자식 파라미터 매핑

  // 실시간 파이프라인 Target Model 필드
  target_models?: TargetModelMapping[]   // 다중 Target Model (Realtime용)
  discriminator_field?: string           // 모델 구분 필드 (예: "_table")

  // 배치 처리 설정 (Batch 워크플로우에서만 사용)
  batch?: PipelineBatchConfig

  // @deprecated source는 input으로 대체됨 (하위 호환성)
  source?: WorkflowSource
}

// 헬퍼 함수: Input 또는 Source 반환 (하위 호환성)
export function getInput(pipeline: WorkflowPipeline): WorkflowInput {
  // input이 있으면 우선 사용
  if (pipeline.input && pipeline.input.type) {
    return pipeline.input
  }
  // source가 있으면 source 반환 (하위 호환성)
  if (pipeline.source && pipeline.source.type) {
    return pipeline.source
  }
  // 둘 다 없으면 빈 Input 반환
  return { type: '', name: '', config: {} }
}

// 헬퍼 함수: Input 설정 (source도 함께 설정하여 하위 호환성 유지)
export function setInput(pipeline: WorkflowPipeline, input: WorkflowInput): void {
  pipeline.input = input
  pipeline.source = input  // 하위 호환성
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
