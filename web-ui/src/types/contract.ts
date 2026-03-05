// Data Contract 관련 타입 정의

// 규칙 심각도
export type RuleSeverity = 'error' | 'warning' | 'info'

// 위반 시 처리 방식
export type ViolationAction = 'drop' | 'quarantine' | 'tag' | 'error'

// 비교 연산자
export type ComparisonOperator =
  | '==' | '!='
  | '>' | '>=' | '<' | '<='
  | 'exists' | 'not_exists'
  | 'matches' | 'in' | 'not_in'
  | 'contains' | 'starts_with' | 'ends_with'

// 논리 연산자
export type LogicalOperator = 'AND' | 'OR'

// 단일 조건
export interface RuleCondition {
  id: string
  field: string
  operator: ComparisonOperator
  value?: string | number | boolean | string[]
  // 비교 대상이 필드인 경우 (cross-field validation)
  compareToField?: string
}

// 조건 그룹 (AND/OR로 묶인 조건들)
export interface ConditionGroup {
  id: string
  operator: LogicalOperator
  conditions: (RuleCondition | ConditionGroup)[]
}

// 비즈니스 규칙
export interface BusinessRule {
  id: string
  name: string
  description?: string
  condition: string // 표현식 문자열 (백엔드 호환)
  conditionBuilder?: ConditionGroup // GUI 빌더용 구조화된 조건
  severity: RuleSeverity
  tags?: string[]
}

// 필드 스키마
export interface ContractField {
  name: string
  type: 'string' | 'number' | 'integer' | 'boolean' | 'object' | 'array' | 'any'
  required?: boolean
  description?: string
  pattern?: string
  minLength?: number
  maxLength?: number
  min?: number
  max?: number
  enum?: (string | number | boolean)[]
}

// 계약 스키마
export interface ContractSchema {
  fields: ContractField[]
  strict?: boolean // true면 정의되지 않은 필드 불허
}

// SLA 정의
export interface ContractSLA {
  freshness?: string     // 데이터 신선도 (예: "5m", "1h")
  completeness?: number  // 완전성 비율 (0.0 ~ 1.0)
  accuracy?: number      // 정확도 비율 (0.0 ~ 1.0)
}

// Data Contract 전체 정의
export interface DataContract {
  name: string
  version: string
  description?: string
  owner?: string
  team?: string
  sla?: ContractSLA
  schema?: ContractSchema
  rules: BusinessRule[]
  tags?: Record<string, string>
}

// Circuit Breaker 설정
export interface CircuitBreakerConfig {
  consecutive_failures?: number    // 연속 위반 임계치
  failure_rate_threshold?: number  // 위반율 임계치 (0.0 ~ 1.0)
  window_size?: number             // Sliding Window 크기
  open_timeout?: string            // Open 후 Half-Open 전환 대기 (예: "30s")
  half_open_requests?: number      // Half-Open에서 테스트할 요청 수
}

// DLQ 설정
export interface DLQConfig {
  enabled: boolean
  type: 'kafka' | 'file' | 'http'
  // Kafka DLQ
  brokers?: string[]
  topic?: string
  retention_ms?: number
  // File DLQ
  path?: string
  format?: 'json' | 'jsonl'
  max_size_mb?: number
  max_age_days?: number
  max_backups?: number
  // HTTP DLQ
  url?: string
  headers?: Record<string, string>
}

// Contract Stage 설정
export interface ContractStageConfig {
  contract: DataContract
  action: ViolationAction
  tag_field?: string              // tag 액션용 필드명
  circuit_breaker?: CircuitBreakerConfig
  dlq?: DLQConfig                 // quarantine 액션용
}

// 위반 통계
export interface ContractViolationStats {
  contract_id: string
  total_records: number
  valid_records: number
  invalid_records: number
  warning_records: number
  violations_by_rule: Record<string, number>
  circuit_state: 'closed' | 'open' | 'half_open'
  circuit_failure_rate: number
}

// 연산자 메타데이터 (UI 표시용)
export const OPERATOR_METADATA: Record<ComparisonOperator, {
  label: string
  description: string
  needsValue: boolean
  valueType?: 'text' | 'number' | 'boolean' | 'array' | 'regex' | 'field'
}> = {
  '==': { label: '같음 (==)', description: '값이 같은지 비교', needsValue: true, valueType: 'text' },
  '!=': { label: '다름 (!=)', description: '값이 다른지 비교', needsValue: true, valueType: 'text' },
  '>': { label: '초과 (>)', description: '값보다 큰지 비교', needsValue: true, valueType: 'number' },
  '>=': { label: '이상 (>=)', description: '값 이상인지 비교', needsValue: true, valueType: 'number' },
  '<': { label: '미만 (<)', description: '값보다 작은지 비교', needsValue: true, valueType: 'number' },
  '<=': { label: '이하 (<=)', description: '값 이하인지 비교', needsValue: true, valueType: 'number' },
  'exists': { label: '존재함', description: '필드가 존재하는지 확인', needsValue: false },
  'not_exists': { label: '존재하지 않음', description: '필드가 없는지 확인', needsValue: false },
  'matches': { label: '정규식 일치', description: '정규식 패턴과 일치하는지 확인', needsValue: true, valueType: 'regex' },
  'in': { label: '포함 (in)', description: '값이 목록에 포함되는지 확인', needsValue: true, valueType: 'array' },
  'not_in': { label: '미포함 (not in)', description: '값이 목록에 없는지 확인', needsValue: true, valueType: 'array' },
  'contains': { label: '문자열 포함', description: '문자열을 포함하는지 확인', needsValue: true, valueType: 'text' },
  'starts_with': { label: '시작 문자열', description: '특정 문자열로 시작하는지 확인', needsValue: true, valueType: 'text' },
  'ends_with': { label: '끝 문자열', description: '특정 문자열로 끝나는지 확인', needsValue: true, valueType: 'text' },
}

// 조건 빌더 → 표현식 문자열 변환
export function conditionGroupToExpression(group: ConditionGroup): string {
  const parts = group.conditions.map(cond => {
    if ('operator' in cond && 'conditions' in cond) {
      // 중첩 그룹
      return `(${conditionGroupToExpression(cond as ConditionGroup)})`
    } else {
      // 단일 조건
      const c = cond as RuleCondition
      return conditionToExpression(c)
    }
  })
  return parts.join(` ${group.operator} `)
}

function conditionToExpression(cond: RuleCondition): string {
  const { field, operator, value, compareToField } = cond

  // 필드 vs 필드 비교
  if (compareToField) {
    return `${field} ${operator} ${compareToField}`
  }

  switch (operator) {
    case 'exists':
      return `${field} exists`
    case 'not_exists':
      return `${field} not_exists`
    case 'matches':
      return `${field} matches "${value}"`
    case 'in':
    case 'not_in': {
      const arr = Array.isArray(value) ? value : [value]
      return `${field} ${operator} [${arr.map(v => typeof v === 'string' ? `"${v}"` : v).join(', ')}]`
    }
    case 'contains':
      return `${field} contains "${value}"`
    case 'starts_with':
      return `${field} starts_with "${value}"`
    case 'ends_with':
      return `${field} ends_with "${value}"`
    default: {
      // 숫자/문자열/불리언 비교
      const formattedValue = typeof value === 'string' ? `"${value}"` : value
      return `${field} ${operator} ${formattedValue}`
    }
  }
}

// 표현식 문자열 → 조건 빌더 변환 (단순 파싱, 복잡한 경우 수동 편집 필요)
export function expressionToConditionGroup(expr: string): ConditionGroup | null {
  // 기본적인 AND/OR 분리만 지원
  // 복잡한 표현식은 null 반환 (수동 편집 모드)
  try {
    const trimmed = expr.trim()
    if (!trimmed) return null

    // AND로 분리된 조건들 처리
    if (trimmed.includes(' AND ')) {
      const parts = trimmed.split(' AND ').map(p => p.trim())
      return {
        id: crypto.randomUUID(),
        operator: 'AND',
        conditions: parts.map(parseSimpleCondition).filter(Boolean) as RuleCondition[]
      }
    }

    // OR로 분리된 조건들 처리
    if (trimmed.includes(' OR ')) {
      const parts = trimmed.split(' OR ').map(p => p.trim())
      return {
        id: crypto.randomUUID(),
        operator: 'OR',
        conditions: parts.map(parseSimpleCondition).filter(Boolean) as RuleCondition[]
      }
    }

    // 단일 조건
    const singleCond = parseSimpleCondition(trimmed)
    if (singleCond) {
      return {
        id: crypto.randomUUID(),
        operator: 'AND',
        conditions: [singleCond]
      }
    }

    return null
  } catch {
    return null
  }
}

function parseSimpleCondition(expr: string): RuleCondition | null {
  // 괄호 제거
  expr = expr.replace(/^\(|\)$/g, '').trim()

  // exists / not_exists
  const existsMatch = expr.match(/^(\w+)\s+(exists|not_exists)$/)
  if (existsMatch) {
    return {
      id: crypto.randomUUID(),
      field: existsMatch[1],
      operator: existsMatch[2] as ComparisonOperator
    }
  }

  // 비교 연산자
  const compMatch = expr.match(/^(\w+)\s*(==|!=|>=|<=|>|<)\s*(.+)$/)
  if (compMatch) {
    let value: string | number | boolean = compMatch[3].trim()
    // 따옴표 제거
    if (value.startsWith('"') && value.endsWith('"')) {
      value = value.slice(1, -1)
    } else if (!isNaN(Number(value))) {
      value = Number(value)
    } else if (value === 'true' || value === 'false') {
      value = value === 'true'
    }

    return {
      id: crypto.randomUUID(),
      field: compMatch[1],
      operator: compMatch[2] as ComparisonOperator,
      value
    }
  }

  // matches
  const matchesMatch = expr.match(/^(\w+)\s+matches\s+"(.+)"$/)
  if (matchesMatch) {
    return {
      id: crypto.randomUUID(),
      field: matchesMatch[1],
      operator: 'matches',
      value: matchesMatch[2]
    }
  }

  // in / not_in
  const inMatch = expr.match(/^(\w+)\s+(in|not_in)\s+\[(.+)\]$/)
  if (inMatch) {
    const values = inMatch[3].split(',').map(v => {
      v = v.trim()
      if (v.startsWith('"') && v.endsWith('"')) return v.slice(1, -1)
      return v
    })
    return {
      id: crypto.randomUUID(),
      field: inMatch[1],
      operator: inMatch[2] as ComparisonOperator,
      value: values as string[]
    }
  }

  return null
}
