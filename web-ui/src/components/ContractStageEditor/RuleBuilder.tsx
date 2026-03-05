/**
 * RuleBuilder - 비즈니스 규칙 빌더 컴포넌트
 * AND/OR 조합으로 복합 조건을 GUI로 구성
 */
import { useState, useCallback, useEffect } from 'react'
import {
  Box,
  Typography,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Button,
  IconButton,
  Stack,
  Chip,
  ToggleButton,
  ToggleButtonGroup,
  Autocomplete,
  Paper,
  Divider,
  Tooltip,
  Alert,
  Collapse,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteIcon from '@mui/icons-material/Delete'
import CodeIcon from '@mui/icons-material/Code'
import BuildIcon from '@mui/icons-material/Build'
import CompareArrowsIcon from '@mui/icons-material/CompareArrows'
import { useTranslation } from 'react-i18next'
import type {
  BusinessRule,
  RuleCondition,
  ConditionGroup,
  ComparisonOperator,
  LogicalOperator,
  RuleSeverity,
  OPERATOR_METADATA,
} from '../../types/contract'
import { conditionGroupToExpression, expressionToConditionGroup } from '../../types/contract'

interface RuleBuilderProps {
  rule: BusinessRule
  onChange: (updates: Partial<BusinessRule>) => void
  onDelete: () => void
  availableFields?: string[]
}

// 연산자 메타데이터
const operatorMeta: typeof OPERATOR_METADATA = {
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

export function RuleBuilder({ rule, onChange, onDelete, availableFields = [] }: RuleBuilderProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<'builder' | 'expression'>('builder')
  const [conditionGroup, setConditionGroup] = useState<ConditionGroup | null>(
    rule.conditionBuilder || expressionToConditionGroup(rule.condition) || {
      id: crypto.randomUUID(),
      operator: 'AND',
      conditions: [],
    }
  )

  // 표현식이 변경되면 빌더 동기화 시도
  useEffect(() => {
    if (mode === 'expression' && rule.condition) {
      const parsed = expressionToConditionGroup(rule.condition)
      if (parsed) {
        setConditionGroup(parsed)
      }
    }
  }, [rule.condition, mode])

  // 그룹 논리 연산자 변경
  const updateGroupOperator = useCallback((op: LogicalOperator) => {
    if (conditionGroup) {
      const newGroup = { ...conditionGroup, operator: op }
      setConditionGroup(newGroup)
      if (newGroup.conditions.length > 0) {
        onChange({ condition: conditionGroupToExpression(newGroup), conditionBuilder: newGroup })
      }
    }
  }, [conditionGroup, onChange])

  // 조건 추가
  const addCondition = useCallback(() => {
    if (!conditionGroup) return
    const newCondition: RuleCondition = {
      id: crypto.randomUUID(),
      field: availableFields[0] || '',
      operator: '==',
      value: '',
    }
    const newGroup = {
      ...conditionGroup,
      conditions: [...conditionGroup.conditions, newCondition],
    }
    setConditionGroup(newGroup)
    onChange({ condition: conditionGroupToExpression(newGroup), conditionBuilder: newGroup })
  }, [conditionGroup, availableFields, onChange])

  // 조건 업데이트
  const updateCondition = useCallback((condId: string, updates: Partial<RuleCondition>) => {
    if (!conditionGroup) return
    const updateInGroup = (group: ConditionGroup): ConditionGroup => ({
      ...group,
      conditions: group.conditions.map(cond => {
        if ('conditions' in cond) {
          return updateInGroup(cond as ConditionGroup)
        }
        if ((cond as RuleCondition).id === condId) {
          return { ...cond, ...updates } as RuleCondition
        }
        return cond
      }),
    })
    const newGroup = updateInGroup(conditionGroup)
    setConditionGroup(newGroup)
    if (newGroup.conditions.length > 0) {
      onChange({ condition: conditionGroupToExpression(newGroup), conditionBuilder: newGroup })
    }
  }, [conditionGroup, onChange])

  // 조건 삭제
  const deleteCondition = useCallback((condId: string) => {
    if (!conditionGroup) return
    const deleteInGroup = (group: ConditionGroup): ConditionGroup => ({
      ...group,
      conditions: group.conditions
        .filter(cond => {
          if ('conditions' in cond) return true
          return (cond as RuleCondition).id !== condId
        })
        .map(cond => {
          if ('conditions' in cond) {
            return deleteInGroup(cond as ConditionGroup)
          }
          return cond
        }),
    })
    const newGroup = deleteInGroup(conditionGroup)
    setConditionGroup(newGroup)
    onChange({
      condition: newGroup.conditions.length > 0 ? conditionGroupToExpression(newGroup) : '',
      conditionBuilder: newGroup,
    })
  }, [conditionGroup, onChange])

  // 조건 렌더링
  const renderCondition = (condition: RuleCondition, index: number, total: number) => {
    const opMeta = operatorMeta[condition.operator]
    const isFieldComparison = !!condition.compareToField

    return (
      <Paper key={condition.id} variant="outlined" sx={{ p: 2 }}>
        <Stack spacing={2}>
          <Stack direction="row" spacing={1} alignItems="center">
            {/* 필드 선택 */}
            <Autocomplete
              freeSolo
              options={availableFields}
              value={condition.field}
              onChange={(_, v) => updateCondition(condition.id, { field: v || '' })}
              onInputChange={(_, v) => updateCondition(condition.id, { field: v })}
              size="small"
              sx={{ minWidth: 150 }}
              renderInput={(params) => (
                <TextField {...params} label={t('contract.field', '필드')} />
              )}
            />

            {/* 연산자 선택 */}
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <InputLabel>{t('contract.operator', '연산자')}</InputLabel>
              <Select
                value={condition.operator}
                label={t('contract.operator', '연산자')}
                onChange={(e) => updateCondition(condition.id, {
                  operator: e.target.value as ComparisonOperator,
                  // 값이 필요 없는 연산자로 변경 시 값 초기화
                  value: operatorMeta[e.target.value as ComparisonOperator].needsValue ? condition.value : undefined,
                })}
              >
                {Object.entries(operatorMeta).map(([op, meta]) => (
                  <MenuItem key={op} value={op}>
                    <Tooltip title={meta.description}>
                      <span>{meta.label}</span>
                    </Tooltip>
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            {/* 필드 vs 값 토글 (비교 연산자인 경우만) */}
            {opMeta?.needsValue && ['==', '!=', '>', '>=', '<', '<='].includes(condition.operator) && (
              <Tooltip title={t('contract.compareToField', '다른 필드와 비교')}>
                <ToggleButton
                  value="field"
                  selected={isFieldComparison}
                  onChange={() => {
                    if (isFieldComparison) {
                      updateCondition(condition.id, { compareToField: undefined, value: '' })
                    } else {
                      updateCondition(condition.id, { compareToField: availableFields[0] || '', value: undefined })
                    }
                  }}
                  size="small"
                >
                  <CompareArrowsIcon fontSize="small" />
                </ToggleButton>
              </Tooltip>
            )}

            {/* 값 입력 */}
            {opMeta?.needsValue && (
              isFieldComparison ? (
                <Autocomplete
                  freeSolo
                  options={availableFields.filter(f => f !== condition.field)}
                  value={condition.compareToField || ''}
                  onChange={(_, v) => updateCondition(condition.id, { compareToField: v || '' })}
                  onInputChange={(_, v) => updateCondition(condition.id, { compareToField: v })}
                  size="small"
                  sx={{ minWidth: 150 }}
                  renderInput={(params) => (
                    <TextField {...params} label={t('contract.compareField', '비교 필드')} />
                  )}
                />
              ) : opMeta.valueType === 'array' ? (
                <TextField
                  label={t('contract.values', '값 목록')}
                  value={Array.isArray(condition.value) ? condition.value.join(', ') : condition.value || ''}
                  onChange={(e) => {
                    const values = e.target.value.split(',').map(v => v.trim()).filter(Boolean)
                    updateCondition(condition.id, { value: values })
                  }}
                  size="small"
                  sx={{ flex: 1 }}
                  placeholder="value1, value2, value3"
                  helperText={t('contract.arrayHelp', '쉼표로 구분')}
                />
              ) : opMeta.valueType === 'number' ? (
                <TextField
                  label={t('contract.value', '값')}
                  type="number"
                  value={condition.value || ''}
                  onChange={(e) => updateCondition(condition.id, { value: parseFloat(e.target.value) || 0 })}
                  size="small"
                  sx={{ minWidth: 120 }}
                />
              ) : (
                <TextField
                  label={opMeta.valueType === 'regex' ? t('contract.pattern', '패턴') : t('contract.value', '값')}
                  value={condition.value || ''}
                  onChange={(e) => updateCondition(condition.id, { value: e.target.value })}
                  size="small"
                  sx={{ flex: 1 }}
                  placeholder={opMeta.valueType === 'regex' ? '^[a-z]+$' : ''}
                />
              )
            )}

            {/* 삭제 버튼 */}
            <IconButton size="small" color="error" onClick={() => deleteCondition(condition.id)}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Stack>

          {/* AND/OR 구분선 */}
          {index < total - 1 && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Divider sx={{ flex: 1 }} />
              <Chip
                label={conditionGroup?.operator || 'AND'}
                size="small"
                color="primary"
                variant="outlined"
              />
              <Divider sx={{ flex: 1 }} />
            </Box>
          )}
        </Stack>
      </Paper>
    )
  }

  return (
    <Box>
      {/* 규칙 기본 정보 */}
      <Stack direction="row" spacing={2} sx={{ mb: 2 }}>
        <TextField
          label={t('contract.ruleName', '규칙 이름')}
          value={rule.name}
          onChange={(e) => onChange({ name: e.target.value })}
          size="small"
          sx={{ flex: 1 }}
          placeholder="valid_date_range"
        />
        <FormControl size="small" sx={{ minWidth: 120 }}>
          <InputLabel>{t('contract.severity', '심각도')}</InputLabel>
          <Select
            value={rule.severity}
            label={t('contract.severity', '심각도')}
            onChange={(e) => onChange({ severity: e.target.value as RuleSeverity })}
          >
            <MenuItem value="error">
              <Chip label="Error" size="small" color="error" />
            </MenuItem>
            <MenuItem value="warning">
              <Chip label="Warning" size="small" color="warning" />
            </MenuItem>
            <MenuItem value="info">
              <Chip label="Info" size="small" color="info" />
            </MenuItem>
          </Select>
        </FormControl>
        <IconButton color="error" onClick={onDelete}>
          <DeleteIcon />
        </IconButton>
      </Stack>

      <TextField
        label={t('contract.ruleDescription', '규칙 설명')}
        value={rule.description || ''}
        onChange={(e) => onChange({ description: e.target.value })}
        size="small"
        fullWidth
        sx={{ mb: 2 }}
        placeholder={t('contract.ruleDescriptionPlaceholder', '이 규칙이 검증하는 내용을 설명하세요')}
      />

      {/* 모드 전환 */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="subtitle2">{t('contract.condition', '조건')}</Typography>
        <ToggleButtonGroup
          value={mode}
          exclusive
          onChange={(_, v) => v && setMode(v)}
          size="small"
        >
          <ToggleButton value="builder">
            <Tooltip title={t('contract.builderMode', '빌더 모드')}>
              <BuildIcon fontSize="small" />
            </Tooltip>
          </ToggleButton>
          <ToggleButton value="expression">
            <Tooltip title={t('contract.expressionMode', '표현식 모드')}>
              <CodeIcon fontSize="small" />
            </Tooltip>
          </ToggleButton>
        </ToggleButtonGroup>
      </Box>

      {/* 빌더 모드 */}
      <Collapse in={mode === 'builder'}>
        <Box sx={{ mb: 2 }}>
          <Stack direction="row" spacing={2} alignItems="center" sx={{ mb: 2 }}>
            <Typography variant="body2" color="text.secondary">
              {t('contract.logicalOperator', '조건 결합 방식')}:
            </Typography>
            <ToggleButtonGroup
              value={conditionGroup?.operator || 'AND'}
              exclusive
              onChange={(_, v) => v && updateGroupOperator(v)}
              size="small"
            >
              <ToggleButton value="AND">
                <Tooltip title={t('contract.andDescription', '모든 조건이 참이어야 함')}>
                  <span>AND</span>
                </Tooltip>
              </ToggleButton>
              <ToggleButton value="OR">
                <Tooltip title={t('contract.orDescription', '하나 이상의 조건이 참이면 됨')}>
                  <span>OR</span>
                </Tooltip>
              </ToggleButton>
            </ToggleButtonGroup>
          </Stack>

          {/* 조건 목록 */}
          <Stack spacing={1}>
            {conditionGroup?.conditions.map((cond, index) => {
              if ('conditions' in cond) {
                // 중첩 그룹 (현재는 미지원 - 향후 확장)
                return null
              }
              return renderCondition(cond as RuleCondition, index, conditionGroup.conditions.length)
            })}
          </Stack>

          <Button startIcon={<AddIcon />} onClick={addCondition} sx={{ mt: 2 }}>
            {t('contract.addCondition', '조건 추가')}
          </Button>
        </Box>
      </Collapse>

      {/* 표현식 모드 */}
      <Collapse in={mode === 'expression'}>
        <Box>
          <TextField
            label={t('contract.expression', '표현식')}
            value={rule.condition}
            onChange={(e) => onChange({ condition: e.target.value })}
            size="small"
            fullWidth
            multiline
            rows={3}
            placeholder='amount > 0 AND status == "active"'
            helperText={t('contract.expressionHelp', '예: field == "value" AND other_field > 100')}
          />
          {rule.condition && !expressionToConditionGroup(rule.condition) && (
            <Alert severity="warning" sx={{ mt: 1 }}>
              {t('contract.complexExpression', '복잡한 표현식은 빌더 모드에서 편집할 수 없습니다.')}
            </Alert>
          )}
        </Box>
      </Collapse>

      {/* 미리보기 */}
      {rule.condition && (
        <Paper variant="outlined" sx={{ p: 1, mt: 2, bgcolor: 'grey.50' }}>
          <Typography variant="caption" color="text.secondary">
            {t('contract.preview', '표현식 미리보기')}:
          </Typography>
          <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
            {rule.condition}
          </Typography>
        </Paper>
      )}
    </Box>
  )
}

export default RuleBuilder
