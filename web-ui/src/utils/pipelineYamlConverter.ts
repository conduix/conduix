/**
 * Pipeline ↔ YAML 변환 유틸리티
 *
 * GUI 에디터와 YAML 에디터 간 양방향 변환을 담당합니다.
 * - pipelineToYaml: Pipeline 객체 → YAML 문자열
 * - yamlToPipeline: YAML 문자열 → Pipeline 객체
 * - validatePipelineYaml: YAML 파싱 결과 유효성 검사
 */

import yaml from 'js-yaml'
import type {
  WorkflowPipeline,
  WorkflowInput,
  Stage,
  Output,
  PipelineBatchConfig,
  StageType,
  OutputType,
} from '../types/pipeline'

// 유효성 검사 결과
export interface ValidationResult {
  valid: boolean
  errors: string[]
  warnings: string[]
}

// Input 타입 목록
const INPUT_TYPES = ['kafka', 'rest_api', 'sql', 'cdc', 'file', 'sql_event', 'kubernetes', 'partitioned_http'] as const
type InputType = (typeof INPUT_TYPES)[number]

// Stage 타입 목록
const STAGE_TYPES: StageType[] = [
  'filter',
  'remap',
  'drop',
  'merge',
  'split',
  'encrypt',
  'dedupe',
  'default',
  'cast',
  'timestamp',
  'throttle',
  'validate',
  'contract',
  'route',
  'delete',
]

// Output 타입 목록
const OUTPUT_TYPES: OutputType[] = [
  'sql',
  'elasticsearch',
  'kafka',
  'mongodb',
  's3',
  'rest_api',
  'file',
]

/**
 * 빈 필드 제거 (YAML 깔끔하게 만들기)
 */
function removeEmptyFields<T extends Record<string, unknown>>(obj: T): Partial<T> {
  const result: Record<string, unknown> = {}

  for (const [key, value] of Object.entries(obj)) {
    if (value === undefined || value === null || value === '') {
      continue
    }
    if (Array.isArray(value)) {
      if (value.length > 0) {
        result[key] = value.map((item) =>
          typeof item === 'object' && item !== null
            ? removeEmptyFields(item as Record<string, unknown>)
            : item
        )
      }
    } else if (typeof value === 'object') {
      const cleaned = removeEmptyFields(value as Record<string, unknown>)
      if (Object.keys(cleaned).length > 0) {
        result[key] = cleaned
      }
    } else {
      result[key] = value
    }
  }

  return result as Partial<T>
}

/**
 * Input을 YAML용 객체로 변환
 */
function inputToYamlObject(input: WorkflowInput): Record<string, unknown> {
  const result: Record<string, unknown> = {
    type: input.type,
    name: input.name,
    config: input.config,
  }

  if (input.rate_limit?.enabled) {
    result.rate_limit = input.rate_limit
  }

  if (input.partition && Object.keys(input.partition).length > 0) {
    result.partition = input.partition
  }

  if (input.pagination && Object.keys(input.pagination).length > 0) {
    result.pagination = input.pagination
  }

  return removeEmptyFields(result)
}

/**
 * Stage를 YAML용 객체로 변환
 */
function stageToYamlObject(stage: Stage): Record<string, unknown> {
  return removeEmptyFields({
    name: stage.name,
    type: stage.type,
    config: stage.config,
  })
}

/**
 * Output을 YAML용 객체로 변환
 */
function outputToYamlObject(output: Output): Record<string, unknown> {
  const result: Record<string, unknown> = {
    name: output.name,
    type: output.type,
    config: output.config,
  }

  if (output.pre_stages && output.pre_stages.length > 0) {
    result.pre_stages = output.pre_stages.map(stageToYamlObject)
  }

  return removeEmptyFields(result)
}

/**
 * Batch 설정을 YAML용 객체로 변환
 */
function batchToYamlObject(batch: PipelineBatchConfig): Record<string, unknown> | undefined {
  if (!batch.enabled) {
    return undefined
  }

  return removeEmptyFields({
    enabled: true,
    output_mode: batch.output_mode,
    size: batch.size,
    workers: batch.workers,
    flush_interval: batch.flush_interval,
  })
}

/**
 * Pipeline → YAML 문자열 변환
 *
 * @param pipeline - WorkflowPipeline 객체
 * @returns YAML 문자열
 */
export function pipelineToYaml(pipeline: WorkflowPipeline): string {
  const yamlObj: Record<string, unknown> = {}

  // 기본 정보
  if (pipeline.name) {
    yamlObj.name = pipeline.name
  }
  if (pipeline.description) {
    yamlObj.description = pipeline.description
  }

  // Input (source 대신 input 사용)
  const input = pipeline.input || pipeline.source
  if (input && input.type) {
    yamlObj.input = inputToYamlObject(input)
  }

  // Stages
  if (pipeline.stages && pipeline.stages.length > 0) {
    yamlObj.stages = pipeline.stages.map(stageToYamlObject)
  }

  // Outputs
  if (pipeline.outputs && pipeline.outputs.length > 0) {
    yamlObj.outputs = pipeline.outputs.map(outputToYamlObject)
  }

  // Batch 설정
  if (pipeline.batch) {
    const batchYaml = batchToYamlObject(pipeline.batch)
    if (batchYaml) {
      yamlObj.batch = batchYaml
    }
  }

  return yaml.dump(yamlObj, {
    indent: 2,
    lineWidth: 120,
    noRefs: true,
    sortKeys: false,
  })
}

/**
 * YAML 객체에서 Input 파싱
 */
function parseInput(inputObj: unknown): WorkflowInput | undefined {
  if (!inputObj || typeof inputObj !== 'object') {
    return undefined
  }

  const obj = inputObj as Record<string, unknown>

  if (!obj.type || typeof obj.type !== 'string') {
    return undefined
  }

  return {
    type: obj.type,
    name: (obj.name as string) || '',
    config: (obj.config as Record<string, unknown>) || {},
    rate_limit: obj.rate_limit as WorkflowInput['rate_limit'],
    partition: obj.partition as Record<string, unknown> | undefined,
    pagination: obj.pagination as Record<string, unknown> | undefined,
  }
}

/**
 * YAML 객체에서 Stage 파싱
 */
function parseStage(stageObj: unknown, index: number): Stage | undefined {
  if (!stageObj || typeof stageObj !== 'object') {
    return undefined
  }

  const obj = stageObj as Record<string, unknown>

  if (!obj.type || typeof obj.type !== 'string') {
    return undefined
  }

  return {
    id: (obj.id as string) || crypto.randomUUID(),
    name: (obj.name as string) || `stage_${index + 1}`,
    type: obj.type as StageType,
    config: (obj.config as Record<string, unknown>) || {},
  }
}

/**
 * YAML 객체에서 Output 파싱
 */
function parseOutput(outputObj: unknown, index: number): Output | undefined {
  if (!outputObj || typeof outputObj !== 'object') {
    return undefined
  }

  const obj = outputObj as Record<string, unknown>

  if (!obj.type || typeof obj.type !== 'string') {
    return undefined
  }

  const preStages = Array.isArray(obj.pre_stages)
    ? (obj.pre_stages
        .map((ps, i) => parseStage(ps, i))
        .filter((s): s is Stage => s !== undefined))
    : undefined

  return {
    id: (obj.id as string) || crypto.randomUUID(),
    name: (obj.name as string) || `output_${index + 1}`,
    type: obj.type as OutputType,
    config: (obj.config as Record<string, unknown>) || {},
    pre_stages: preStages,
  }
}

/**
 * YAML 객체에서 Batch 설정 파싱
 */
function parseBatch(batchObj: unknown): PipelineBatchConfig | undefined {
  if (!batchObj || typeof batchObj !== 'object') {
    return undefined
  }

  const obj = batchObj as Record<string, unknown>

  return {
    enabled: obj.enabled !== false,
    output_mode: (obj.output_mode as 'bulk' | 'individual') || 'bulk',
    size: (obj.size as number) || 100,
    workers: (obj.workers as number) || 20,
    flush_interval: (obj.flush_interval as string) || '5s',
  }
}

/**
 * YAML 문자열 → Pipeline 객체 변환
 *
 * @param yamlContent - YAML 문자열
 * @param existingPipeline - 기존 파이프라인 (ID 보존용)
 * @returns WorkflowPipeline 객체
 * @throws YAML 파싱 오류
 */
export function yamlToPipeline(
  yamlContent: string,
  existingPipeline?: WorkflowPipeline
): WorkflowPipeline {
  const parsed = yaml.load(yamlContent)

  if (!parsed || typeof parsed !== 'object') {
    throw new Error('Invalid YAML: expected an object')
  }

  const obj = parsed as Record<string, unknown>

  // 기존 파이프라인 ID 보존
  const result: WorkflowPipeline = {
    id: existingPipeline?.id || crypto.randomUUID(),
    name: (obj.name as string) || existingPipeline?.name || '',
    description: obj.description as string | undefined,
    priority: existingPipeline?.priority || 0,
  }

  // Input 파싱 (input 또는 source)
  const inputData = obj.input || obj.source
  if (inputData) {
    const input = parseInput(inputData)
    if (input) {
      result.input = input
      result.source = input // 하위 호환성
    }
  }

  // Stages 파싱
  if (Array.isArray(obj.stages)) {
    result.stages = obj.stages
      .map((s, i) => parseStage(s, i))
      .filter((s): s is Stage => s !== undefined)
  }

  // Outputs 파싱
  if (Array.isArray(obj.outputs)) {
    result.outputs = obj.outputs
      .map((o, i) => parseOutput(o, i))
      .filter((o): o is Output => o !== undefined)
  }

  // Batch 설정 파싱
  if (obj.batch) {
    result.batch = parseBatch(obj.batch)
  }

  // 기존 파이프라인의 추가 필드 보존
  if (existingPipeline) {
    result.depends_on = existingPipeline.depends_on
    result.weight = existingPipeline.weight
    result.realtime_mode = existingPipeline.realtime_mode
    result.parent_pipeline_id = existingPipeline.parent_pipeline_id
    result.target_data_type_id = existingPipeline.target_data_type_id
    result.expansion_mode = existingPipeline.expansion_mode
    result.parameter_bindings = existingPipeline.parameter_bindings
    result.target_models = existingPipeline.target_models
    result.discriminator_field = existingPipeline.discriminator_field
  }

  return result
}

/**
 * 파싱된 YAML 유효성 검사
 *
 * @param parsed - 파싱된 객체
 * @returns ValidationResult
 */
export function validatePipelineYaml(parsed: unknown): ValidationResult {
  const errors: string[] = []
  const warnings: string[] = []

  if (!parsed || typeof parsed !== 'object') {
    return { valid: false, errors: ['YAML must be an object'], warnings: [] }
  }

  const obj = parsed as Record<string, unknown>

  // Input 검증
  const inputData = obj.input || obj.source
  if (inputData && typeof inputData === 'object') {
    const input = inputData as Record<string, unknown>

    if (!input.type) {
      errors.push('Input type is required')
    } else if (!INPUT_TYPES.includes(input.type as InputType)) {
      warnings.push(`Unknown input type: ${input.type}`)
    }

    if (!input.name) {
      warnings.push('Input name is recommended')
    }
  }

  // Stages 검증
  if (obj.stages) {
    if (!Array.isArray(obj.stages)) {
      errors.push('Stages must be an array')
    } else {
      obj.stages.forEach((stage, index) => {
        if (!stage || typeof stage !== 'object') {
          errors.push(`Stage ${index + 1}: must be an object`)
          return
        }

        const s = stage as Record<string, unknown>
        if (!s.type) {
          errors.push(`Stage ${index + 1}: type is required`)
        } else if (!STAGE_TYPES.includes(s.type as StageType)) {
          warnings.push(`Stage ${index + 1}: unknown type '${s.type}'`)
        }
      })
    }
  }

  // Outputs 검증
  if (obj.outputs) {
    if (!Array.isArray(obj.outputs)) {
      errors.push('Outputs must be an array')
    } else {
      obj.outputs.forEach((output, index) => {
        if (!output || typeof output !== 'object') {
          errors.push(`Output ${index + 1}: must be an object`)
          return
        }

        const o = output as Record<string, unknown>
        if (!o.type) {
          errors.push(`Output ${index + 1}: type is required`)
        } else if (!OUTPUT_TYPES.includes(o.type as OutputType)) {
          warnings.push(`Output ${index + 1}: unknown type '${o.type}'`)
        }

        // Pre-stages 검증
        if (o.pre_stages && !Array.isArray(o.pre_stages)) {
          errors.push(`Output ${index + 1}: pre_stages must be an array`)
        }
      })
    }
  }

  // Batch 검증
  if (obj.batch && typeof obj.batch === 'object') {
    const batch = obj.batch as Record<string, unknown>

    if (batch.output_mode && !['bulk', 'individual'].includes(batch.output_mode as string)) {
      errors.push("Batch output_mode must be 'bulk' or 'individual'")
    }

    if (batch.size && (typeof batch.size !== 'number' || batch.size < 1)) {
      errors.push('Batch size must be a positive number')
    }

    if (batch.workers && (typeof batch.workers !== 'number' || batch.workers < 1)) {
      errors.push('Batch workers must be a positive number')
    }
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings,
  }
}

/**
 * YAML 문자열 유효성 검사 (파싱 + 구조 검증)
 *
 * @param yamlContent - YAML 문자열
 * @returns ValidationResult
 */
export function validateYamlString(yamlContent: string): ValidationResult {
  try {
    const parsed = yaml.load(yamlContent)
    return validatePipelineYaml(parsed)
  } catch (e) {
    return {
      valid: false,
      errors: [(e as Error).message],
      warnings: [],
    }
  }
}

/**
 * YAML 포맷팅 (정렬 및 정리)
 *
 * @param yamlContent - YAML 문자열
 * @returns 포맷된 YAML 문자열
 */
export function formatYaml(yamlContent: string): string {
  try {
    const parsed = yaml.load(yamlContent)
    return yaml.dump(parsed, {
      indent: 2,
      lineWidth: 120,
      noRefs: true,
      sortKeys: false,
    })
  } catch {
    // 파싱 실패 시 원본 반환
    return yamlContent
  }
}
