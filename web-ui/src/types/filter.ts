// 필터 빌더 타입 정의 (GUI 드래그앤드롭 + YAML 에디터 호환)

/** 비교 연산자 */
export type Operator =
  | 'eq'         // ==
  | 'neq'        // !=
  | 'gt'         // >
  | 'gte'        // >=
  | 'lt'         // <
  | 'lte'        // <=
  | 'contains'   // 문자열 포함
  | 'startswith' // 문자열 시작
  | 'endswith'   // 문자열 끝
  | 'regex'      // 정규식
  | 'exists'     // 필드 존재
  | 'notexists'  // 필드 미존재
  | 'in'         // 값 목록 포함
  | 'notin'      // 값 목록 미포함
  | 'null'       // null 체크
  | 'notnull';   // not null 체크

/** 논리 연산자 */
export type LogicalOperator = 'and' | 'or';

/** 연산자 메타데이터 (UI 표시용) */
export interface OperatorMeta {
  value: Operator;
  label: string;
  description: string;
  needsValue: boolean;
  valueType?: 'string' | 'number' | 'boolean' | 'array' | 'regex';
}

/** 연산자 목록 */
export const OPERATORS: OperatorMeta[] = [
  { value: 'eq', label: '같음 (==)', description: '값이 동일한 경우', needsValue: true, valueType: 'string' },
  { value: 'neq', label: '다름 (!=)', description: '값이 다른 경우', needsValue: true, valueType: 'string' },
  { value: 'gt', label: '초과 (>)', description: '값보다 큰 경우', needsValue: true, valueType: 'number' },
  { value: 'gte', label: '이상 (>=)', description: '값 이상인 경우', needsValue: true, valueType: 'number' },
  { value: 'lt', label: '미만 (<)', description: '값보다 작은 경우', needsValue: true, valueType: 'number' },
  { value: 'lte', label: '이하 (<=)', description: '값 이하인 경우', needsValue: true, valueType: 'number' },
  { value: 'contains', label: '포함', description: '문자열에 값이 포함된 경우', needsValue: true, valueType: 'string' },
  { value: 'startswith', label: '시작', description: '문자열이 값으로 시작하는 경우', needsValue: true, valueType: 'string' },
  { value: 'endswith', label: '끝', description: '문자열이 값으로 끝나는 경우', needsValue: true, valueType: 'string' },
  { value: 'regex', label: '정규식', description: '정규식 패턴과 일치하는 경우', needsValue: true, valueType: 'regex' },
  { value: 'exists', label: '존재함', description: '필드가 존재하는 경우', needsValue: false },
  { value: 'notexists', label: '존재안함', description: '필드가 존재하지 않는 경우', needsValue: false },
  { value: 'in', label: '목록 포함', description: '값이 목록에 포함된 경우', needsValue: true, valueType: 'array' },
  { value: 'notin', label: '목록 미포함', description: '값이 목록에 포함되지 않은 경우', needsValue: true, valueType: 'array' },
  { value: 'null', label: 'NULL', description: '값이 null인 경우', needsValue: false },
  { value: 'notnull', label: 'NOT NULL', description: '값이 null이 아닌 경우', needsValue: false },
];

/** 단일 조건 */
export interface Condition {
  id: string;           // 드래그앤드롭용 고유 ID
  field: string;        // 필드 경로 (예: "user.name")
  op: Operator;         // 연산자
  value?: unknown;      // 비교값
}

/** 조건 그룹 (AND/OR) */
export interface ConditionGroup {
  id: string;                        // 드래그앤드롭용 고유 ID
  operator: LogicalOperator;         // and, or
  conditions: FilterNode[];          // 조건 목록
}

/** 필터 노드 (조건 또는 그룹) */
export interface FilterNode {
  type: 'condition' | 'group';
  condition?: Condition;
  group?: ConditionGroup;
}

/** 전체 필터 정의 */
export interface Filter {
  version?: string;
  root?: FilterNode;
  expression?: string;   // 문자열 표현식 (하위 호환)
}

/** 필터 빌더 상태 */
export interface FilterBuilderState {
  filter: Filter;
  mode: 'visual' | 'yaml';           // 현재 편집 모드
  selectedNodeId: string | null;     // 선택된 노드 ID
  draggedNodeId: string | null;      // 드래그 중인 노드 ID
  dropTargetId: string | null;       // 드롭 대상 노드 ID
  errors: ValidationError[];         // 유효성 검사 오류
}

/** 유효성 검사 오류 */
export interface ValidationError {
  nodeId: string;
  message: string;
}

/** 필터 빌더 액션 */
export type FilterBuilderAction =
  | { type: 'SET_MODE'; mode: 'visual' | 'yaml' }
  | { type: 'SET_FILTER'; filter: Filter }
  | { type: 'ADD_CONDITION'; parentId: string | null; condition: Condition }
  | { type: 'ADD_GROUP'; parentId: string | null; operator: LogicalOperator }
  | { type: 'UPDATE_CONDITION'; id: string; updates: Partial<Condition> }
  | { type: 'UPDATE_GROUP'; id: string; operator: LogicalOperator }
  | { type: 'DELETE_NODE'; id: string }
  | { type: 'MOVE_NODE'; nodeId: string; targetId: string; position: 'before' | 'after' | 'inside' }
  | { type: 'SELECT_NODE'; id: string | null }
  | { type: 'SET_DRAG'; nodeId: string | null }
  | { type: 'SET_DROP_TARGET'; targetId: string | null }
  | { type: 'VALIDATE' }
  | { type: 'CLEAR_ERRORS' };

/** 필드 메타데이터 (자동완성용) */
export interface FieldMeta {
  path: string;           // 필드 경로
  label: string;          // 표시 이름
  type: 'string' | 'number' | 'boolean' | 'object' | 'array';
  description?: string;
  examples?: unknown[];
}

/** 필터 빌더 Props */
export interface FilterBuilderProps {
  /** 초기 필터 값 */
  value?: Filter;
  /** 필터 변경 콜백 */
  onChange?: (filter: Filter) => void;
  /** 사용 가능한 필드 목록 (자동완성용) */
  availableFields?: FieldMeta[];
  /** 읽기 전용 모드 */
  readonly?: boolean;
  /** 초기 편집 모드 */
  defaultMode?: 'visual' | 'yaml';
  /** YAML 모드에서 표현식 형태로 표시할지 여부 */
  yamlAsExpression?: boolean;
}

// ============ 유틸리티 함수 타입 ============

/** ID 생성 함수 */
export function generateId(): string {
  return Math.random().toString(36).substring(2, 10);
}

/** 새 조건 생성 */
export function createCondition(
  field: string = '',
  op: Operator = 'eq',
  value?: unknown
): FilterNode {
  return {
    type: 'condition',
    condition: {
      id: generateId(),
      field,
      op,
      value,
    },
  };
}

/** 새 그룹 생성 */
export function createGroup(
  operator: LogicalOperator = 'and',
  conditions: FilterNode[] = []
): FilterNode {
  return {
    type: 'group',
    group: {
      id: generateId(),
      operator,
      conditions,
    },
  };
}

/** 빈 필터 생성 */
export function createEmptyFilter(): Filter {
  return {
    version: '1',
    root: createGroup('and', []),
  };
}

/** 필터가 비어있는지 확인 */
export function isFilterEmpty(filter: Filter): boolean {
  if (filter.expression) {
    return filter.expression.trim() === '';
  }
  if (!filter.root) {
    return true;
  }
  if (filter.root.type === 'group') {
    return filter.root.group?.conditions.length === 0;
  }
  return false;
}
