import React, { useState, useCallback } from 'react';
import {
  Filter,
  FilterNode,
  Condition,
  ConditionGroup,
  LogicalOperator,
  Operator,
  OPERATORS,
  FieldMeta,
  FilterBuilderProps,
  createCondition,
  createGroup,
  createEmptyFilter,
} from '../../types/filter';

// ============ 서브 컴포넌트 ============

interface ConditionRowProps {
  condition: Condition;
  fields: FieldMeta[];
  onChange: (updates: Partial<Condition>) => void;
  onDelete: () => void;
  readonly?: boolean;
  isDragging?: boolean;
  onDragStart?: () => void;
  onDragEnd?: () => void;
}

/** 단일 조건 행 */
const ConditionRow: React.FC<ConditionRowProps> = ({
  condition,
  fields,
  onChange,
  onDelete,
  readonly,
  isDragging,
  onDragStart,
  onDragEnd,
}) => {
  const operator = OPERATORS.find((op) => op.value === condition.op);

  return (
    <div
      className={`condition-row ${isDragging ? 'dragging' : ''}`}
      draggable={!readonly}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
    >
      {/* 드래그 핸들 */}
      {!readonly && (
        <span className="drag-handle" title="드래그하여 이동">
          ⋮⋮
        </span>
      )}

      {/* 필드 선택 */}
      <select
        className="field-select"
        value={condition.field}
        onChange={(e) => onChange({ field: e.target.value })}
        disabled={readonly}
      >
        <option value="">필드 선택...</option>
        {fields.map((field) => (
          <option key={field.path} value={field.path}>
            {field.label || field.path}
          </option>
        ))}
        {/* 직접 입력 옵션 */}
        {condition.field && !fields.find((f) => f.path === condition.field) && (
          <option value={condition.field}>{condition.field}</option>
        )}
      </select>

      {/* 연산자 선택 */}
      <select
        className="operator-select"
        value={condition.op}
        onChange={(e) => onChange({ op: e.target.value as Operator })}
        disabled={readonly}
      >
        {OPERATORS.map((op) => (
          <option key={op.value} value={op.value} title={op.description}>
            {op.label}
          </option>
        ))}
      </select>

      {/* 값 입력 (필요한 경우) */}
      {operator?.needsValue && (
        <input
          className="value-input"
          type={operator.valueType === 'number' ? 'number' : 'text'}
          value={String(condition.value ?? '')}
          onChange={(e) => {
            const val = operator.valueType === 'number'
              ? Number(e.target.value)
              : e.target.value;
            onChange({ value: val });
          }}
          placeholder="값 입력..."
          disabled={readonly}
        />
      )}

      {/* 삭제 버튼 */}
      {!readonly && (
        <button className="delete-btn" onClick={onDelete} title="조건 삭제">
          ✕
        </button>
      )}
    </div>
  );
};

interface ConditionGroupViewProps {
  group: ConditionGroup;
  fields: FieldMeta[];
  depth: number;
  onUpdate: (updates: Partial<ConditionGroup>) => void;
  onUpdateChild: (childId: string, updates: FilterNode) => void;
  onDeleteChild: (childId: string) => void;
  onAddCondition: () => void;
  onAddGroup: () => void;
  onDelete?: () => void;
  readonly?: boolean;
  draggedId: string | null;
  onDragStart: (id: string) => void;
  onDragEnd: () => void;
  onDrop: (targetId: string, position: 'before' | 'after' | 'inside') => void;
}

/** 조건 그룹 뷰 */
const ConditionGroupView: React.FC<ConditionGroupViewProps> = ({
  group,
  fields,
  depth,
  onUpdate,
  onUpdateChild,
  onDeleteChild,
  onAddCondition,
  onAddGroup,
  onDelete,
  readonly,
  draggedId,
  onDragStart,
  onDragEnd,
  onDrop,
}) => {
  const [dropPosition, setDropPosition] = useState<'before' | 'after' | 'inside' | null>(null);

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    const rect = e.currentTarget.getBoundingClientRect();
    const y = e.clientY - rect.top;
    const height = rect.height;

    if (y < height * 0.25) {
      setDropPosition('before');
    } else if (y > height * 0.75) {
      setDropPosition('after');
    } else {
      setDropPosition('inside');
    }
  };

  const handleDragLeave = () => {
    setDropPosition(null);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    if (dropPosition && draggedId !== group.id) {
      onDrop(group.id, dropPosition);
    }
    setDropPosition(null);
  };

  return (
    <div
      className={`condition-group depth-${depth} ${dropPosition ? `drop-${dropPosition}` : ''}`}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* 그룹 헤더 */}
      <div className="group-header">
        <select
          className="logical-operator"
          value={group.operator}
          onChange={(e) => onUpdate({ operator: e.target.value as LogicalOperator })}
          disabled={readonly}
        >
          <option value="and">AND (모두 일치)</option>
          <option value="or">OR (하나 이상 일치)</option>
        </select>

        {!readonly && (
          <div className="group-actions">
            <button onClick={onAddCondition} title="조건 추가">
              + 조건
            </button>
            <button onClick={onAddGroup} title="그룹 추가">
              + 그룹
            </button>
            {depth > 0 && onDelete && (
              <button className="delete-btn" onClick={onDelete} title="그룹 삭제">
                ✕
              </button>
            )}
          </div>
        )}
      </div>

      {/* 조건 목록 */}
      <div className="group-conditions">
        {group.conditions.length === 0 ? (
          <div className="empty-hint">조건을 추가하세요</div>
        ) : (
          group.conditions.map((node, index) => (
            <div key={node.condition?.id || node.group?.id} className="condition-item">
              {index > 0 && (
                <div className="logical-separator">
                  {group.operator === 'and' ? 'AND' : 'OR'}
                </div>
              )}

              {node.type === 'condition' && node.condition && (
                <ConditionRow
                  condition={node.condition}
                  fields={fields}
                  onChange={(updates) =>
                    onUpdateChild(node.condition!.id, {
                      ...node,
                      condition: { ...node.condition!, ...updates },
                    })
                  }
                  onDelete={() => onDeleteChild(node.condition!.id)}
                  readonly={readonly}
                  isDragging={draggedId === node.condition.id}
                  onDragStart={() => onDragStart(node.condition!.id)}
                  onDragEnd={onDragEnd}
                />
              )}

              {node.type === 'group' && node.group && (
                <ConditionGroupView
                  group={node.group}
                  fields={fields}
                  depth={depth + 1}
                  onUpdate={(updates) =>
                    onUpdateChild(node.group!.id, {
                      ...node,
                      group: { ...node.group!, ...updates },
                    })
                  }
                  onUpdateChild={onUpdateChild}
                  onDeleteChild={onDeleteChild}
                  onAddCondition={() => {
                    const newCondition = createCondition();
                    onUpdateChild(node.group!.id, {
                      ...node,
                      group: {
                        ...node.group!,
                        conditions: [...node.group!.conditions, newCondition],
                      },
                    });
                  }}
                  onAddGroup={() => {
                    const newGroup = createGroup('and', []);
                    onUpdateChild(node.group!.id, {
                      ...node,
                      group: {
                        ...node.group!,
                        conditions: [...node.group!.conditions, newGroup],
                      },
                    });
                  }}
                  onDelete={() => onDeleteChild(node.group!.id)}
                  readonly={readonly}
                  draggedId={draggedId}
                  onDragStart={onDragStart}
                  onDragEnd={onDragEnd}
                  onDrop={onDrop}
                />
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
};

// ============ YAML 에디터 ============

interface YamlEditorProps {
  value: string;
  onChange: (value: string) => void;
  readonly?: boolean;
  error?: string;
}

const YamlEditor: React.FC<YamlEditorProps> = ({ value, onChange, readonly, error }) => {
  return (
    <div className="yaml-editor">
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="필터 표현식을 입력하세요...
예시:
.status == 'active' && .age >= 18
.category == 'premium' || .vip == true"
        disabled={readonly}
        className={error ? 'has-error' : ''}
      />
      {error && <div className="error-message">{error}</div>}
    </div>
  );
};

// ============ 메인 컴포넌트 ============

/** 필터 빌더 메인 컴포넌트 */
export const FilterBuilder: React.FC<FilterBuilderProps> = ({
  value,
  onChange,
  availableFields = [],
  readonly = false,
  defaultMode = 'visual',
  yamlAsExpression: _yamlAsExpression = true,
}) => {
  // 상태
  const [mode, setMode] = useState<'visual' | 'yaml'>(defaultMode);
  const [filter, setFilter] = useState<Filter>(value || createEmptyFilter());
  const [yamlValue, setYamlValue] = useState<string>(value?.expression || '');
  const [yamlError, setYamlError] = useState<string | undefined>();
  const [draggedId, setDraggedId] = useState<string | null>(null);

  // 필터 변경 핸들러
  const handleFilterChange = useCallback(
    (newFilter: Filter) => {
      setFilter(newFilter);
      onChange?.(newFilter);
    },
    [onChange]
  );

  // 모드 전환
  const handleModeChange = useCallback(
    (newMode: 'visual' | 'yaml') => {
      if (newMode === 'yaml' && mode === 'visual') {
        // Visual → YAML: 구조화된 필터를 표현식으로 변환
        // TODO: 실제 변환 로직 (API 호출 또는 클라이언트 변환)
        const expr = filter.expression || '';
        setYamlValue(expr);
      } else if (newMode === 'visual' && mode === 'yaml') {
        // YAML → Visual: 표현식을 구조화된 필터로 변환
        // TODO: 실제 변환 로직 (API 호출 또는 클라이언트 변환)
        handleFilterChange({
          ...filter,
          expression: yamlValue,
        });
      }
      setMode(newMode);
    },
    [mode, filter, yamlValue, handleFilterChange]
  );

  // YAML 값 변경
  const handleYamlChange = useCallback(
    (value: string) => {
      setYamlValue(value);
      setYamlError(undefined);

      // 간단한 유효성 검사
      try {
        // TODO: 표현식 파싱 및 검증
        handleFilterChange({
          ...filter,
          expression: value,
        });
      } catch (e) {
        setYamlError((e as Error).message);
      }
    },
    [filter, handleFilterChange]
  );

  // 루트 그룹 업데이트
  const handleRootUpdate = useCallback(
    (updates: Partial<ConditionGroup>) => {
      if (filter.root?.type === 'group' && filter.root.group) {
        handleFilterChange({
          ...filter,
          root: {
            ...filter.root,
            group: { ...filter.root.group, ...updates },
          },
        });
      }
    },
    [filter, handleFilterChange]
  );

  // 자식 노드 업데이트 (재귀)
  const handleUpdateChild = useCallback(
    (childId: string, updates: FilterNode) => {
      const updateNode = (node: FilterNode): FilterNode => {
        if (node.type === 'condition' && node.condition?.id === childId) {
          return updates;
        }
        if (node.type === 'group' && node.group) {
          if (node.group.id === childId) {
            return updates;
          }
          return {
            ...node,
            group: {
              ...node.group,
              conditions: node.group.conditions.map(updateNode),
            },
          };
        }
        return node;
      };

      if (filter.root) {
        handleFilterChange({
          ...filter,
          root: updateNode(filter.root),
        });
      }
    },
    [filter, handleFilterChange]
  );

  // 자식 노드 삭제 (재귀)
  const handleDeleteChild = useCallback(
    (childId: string) => {
      const deleteFromNode = (node: FilterNode): FilterNode | null => {
        if (node.type === 'condition' && node.condition?.id === childId) {
          return null;
        }
        if (node.type === 'group' && node.group) {
          if (node.group.id === childId) {
            return null;
          }
          const newConditions = node.group.conditions
            .map(deleteFromNode)
            .filter((n): n is FilterNode => n !== null);
          return {
            ...node,
            group: { ...node.group, conditions: newConditions },
          };
        }
        return node;
      };

      if (filter.root) {
        const newRoot = deleteFromNode(filter.root);
        handleFilterChange({
          ...filter,
          root: newRoot || createGroup('and', []),
        });
      }
    },
    [filter, handleFilterChange]
  );

  // 조건 추가
  const handleAddCondition = useCallback(() => {
    if (filter.root?.type === 'group' && filter.root.group) {
      handleFilterChange({
        ...filter,
        root: {
          ...filter.root,
          group: {
            ...filter.root.group,
            conditions: [...filter.root.group.conditions, createCondition()],
          },
        },
      });
    }
  }, [filter, handleFilterChange]);

  // 그룹 추가
  const handleAddGroup = useCallback(() => {
    if (filter.root?.type === 'group' && filter.root.group) {
      handleFilterChange({
        ...filter,
        root: {
          ...filter.root,
          group: {
            ...filter.root.group,
            conditions: [...filter.root.group.conditions, createGroup('and', [])],
          },
        },
      });
    }
  }, [filter, handleFilterChange]);

  // 드래그앤드롭 핸들러
  const handleDrop = useCallback(
    (targetId: string, position: 'before' | 'after' | 'inside') => {
      if (!draggedId || draggedId === targetId) return;
      // TODO: 노드 이동 로직 구현
      console.log(`Move ${draggedId} to ${position} ${targetId}`);
      setDraggedId(null);
    },
    [draggedId]
  );

  return (
    <div className="filter-builder">
      {/* 모드 전환 탭 */}
      <div className="mode-tabs">
        <button
          className={`mode-tab ${mode === 'visual' ? 'active' : ''}`}
          onClick={() => handleModeChange('visual')}
        >
          🎨 비주얼 에디터
        </button>
        <button
          className={`mode-tab ${mode === 'yaml' ? 'active' : ''}`}
          onClick={() => handleModeChange('yaml')}
        >
          📝 YAML 에디터
        </button>
      </div>

      {/* 에디터 영역 */}
      <div className="editor-area">
        {mode === 'visual' && filter.root?.type === 'group' && filter.root.group && (
          <ConditionGroupView
            group={filter.root.group}
            fields={availableFields}
            depth={0}
            onUpdate={handleRootUpdate}
            onUpdateChild={handleUpdateChild}
            onDeleteChild={handleDeleteChild}
            onAddCondition={handleAddCondition}
            onAddGroup={handleAddGroup}
            readonly={readonly}
            draggedId={draggedId}
            onDragStart={setDraggedId}
            onDragEnd={() => setDraggedId(null)}
            onDrop={handleDrop}
          />
        )}

        {mode === 'yaml' && (
          <YamlEditor
            value={yamlValue}
            onChange={handleYamlChange}
            readonly={readonly}
            error={yamlError}
          />
        )}
      </div>
    </div>
  );
};

export default FilterBuilder;
