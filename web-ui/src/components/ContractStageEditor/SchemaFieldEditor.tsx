/**
 * SchemaFieldEditor - 스키마 필드 편집 컴포넌트
 */
import {
  Stack,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  IconButton,
  Checkbox,
  FormControlLabel,
  Collapse,
  Paper,
  Chip,
  Box,
} from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ExpandLessIcon from '@mui/icons-material/ExpandLess'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ContractField } from '../../types/contract'

interface SchemaFieldEditorProps {
  field: ContractField
  onChange: (field: ContractField) => void
  onDelete: () => void
}

const fieldTypes = [
  { value: 'string', label: 'String (문자열)' },
  { value: 'number', label: 'Number (숫자)' },
  { value: 'integer', label: 'Integer (정수)' },
  { value: 'boolean', label: 'Boolean (불리언)' },
  { value: 'object', label: 'Object (객체)' },
  { value: 'array', label: 'Array (배열)' },
  { value: 'any', label: 'Any (모든 타입)' },
]

export function SchemaFieldEditor({ field, onChange, onDelete }: SchemaFieldEditorProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)

  const update = (updates: Partial<ContractField>) => {
    onChange({ ...field, ...updates })
  }

  const hasConstraints = field.pattern || field.minLength || field.maxLength || field.min !== undefined || field.max !== undefined || field.enum

  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Stack direction="row" spacing={2} alignItems="center">
        {/* 필드명 */}
        <TextField
          label={t('contract.fieldName', '필드명')}
          value={field.name}
          onChange={(e) => update({ name: e.target.value })}
          size="small"
          sx={{ flex: 1 }}
        />

        {/* 타입 */}
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>{t('contract.fieldType', '타입')}</InputLabel>
          <Select
            value={field.type}
            label={t('contract.fieldType', '타입')}
            onChange={(e) => update({ type: e.target.value as ContractField['type'] })}
          >
            {fieldTypes.map(ft => (
              <MenuItem key={ft.value} value={ft.value}>{ft.label}</MenuItem>
            ))}
          </Select>
        </FormControl>

        {/* 필수 여부 */}
        <FormControlLabel
          control={
            <Checkbox
              checked={field.required || false}
              onChange={(e) => update({ required: e.target.checked })}
              size="small"
            />
          }
          label={t('contract.required', '필수')}
        />

        {/* 제약조건 토글 */}
        <IconButton size="small" onClick={() => setExpanded(!expanded)}>
          {expanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        </IconButton>

        {/* 제약조건 있음 표시 */}
        {hasConstraints && !expanded && (
          <Chip label={t('contract.hasConstraints', '제약조건')} size="small" color="info" variant="outlined" />
        )}

        {/* 삭제 */}
        <IconButton size="small" color="error" onClick={onDelete}>
          <DeleteIcon fontSize="small" />
        </IconButton>
      </Stack>

      {/* 확장된 제약조건 영역 */}
      <Collapse in={expanded}>
        <Box sx={{ mt: 2, pl: 2 }}>
          {/* 설명 */}
          <TextField
            label={t('contract.fieldDescription', '설명')}
            value={field.description || ''}
            onChange={(e) => update({ description: e.target.value })}
            size="small"
            fullWidth
            sx={{ mb: 2 }}
          />

          {/* 문자열 제약조건 */}
          {field.type === 'string' && (
            <Stack spacing={2}>
              <TextField
                label={t('contract.pattern', '정규식 패턴')}
                value={field.pattern || ''}
                onChange={(e) => update({ pattern: e.target.value || undefined })}
                size="small"
                fullWidth
                placeholder="^[a-zA-Z0-9]+$"
                helperText={t('contract.patternHelp', '정규식 패턴 (예: 이메일, 전화번호 형식)')}
              />
              <Stack direction="row" spacing={2}>
                <TextField
                  label={t('contract.minLength', '최소 길이')}
                  type="number"
                  value={field.minLength ?? ''}
                  onChange={(e) => update({ minLength: e.target.value ? parseInt(e.target.value) : undefined })}
                  size="small"
                  sx={{ flex: 1 }}
                />
                <TextField
                  label={t('contract.maxLength', '최대 길이')}
                  type="number"
                  value={field.maxLength ?? ''}
                  onChange={(e) => update({ maxLength: e.target.value ? parseInt(e.target.value) : undefined })}
                  size="small"
                  sx={{ flex: 1 }}
                />
              </Stack>
            </Stack>
          )}

          {/* 숫자 제약조건 */}
          {(field.type === 'number' || field.type === 'integer') && (
            <Stack direction="row" spacing={2}>
              <TextField
                label={t('contract.min', '최소값')}
                type="number"
                value={field.min ?? ''}
                onChange={(e) => update({ min: e.target.value ? parseFloat(e.target.value) : undefined })}
                size="small"
                sx={{ flex: 1 }}
              />
              <TextField
                label={t('contract.max', '최대값')}
                type="number"
                value={field.max ?? ''}
                onChange={(e) => update({ max: e.target.value ? parseFloat(e.target.value) : undefined })}
                size="small"
                sx={{ flex: 1 }}
              />
            </Stack>
          )}

          {/* Enum 값 */}
          <TextField
            label={t('contract.enum', '허용 값 목록 (Enum)')}
            value={field.enum?.join(', ') || ''}
            onChange={(e) => {
              const values = e.target.value ? e.target.value.split(',').map(v => v.trim()).filter(Boolean) : undefined
              update({ enum: values })
            }}
            size="small"
            fullWidth
            sx={{ mt: 2 }}
            placeholder="value1, value2, value3"
            helperText={t('contract.enumHelp', '쉼표로 구분된 허용 값 목록')}
          />
        </Box>
      </Collapse>
    </Paper>
  )
}

export default SchemaFieldEditor
