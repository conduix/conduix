/**
 * SchemaField - Schema 기반 개별 필드 렌더링 컴포넌트
 */
import { useMemo, useState } from 'react'
import {
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
  InputAdornment,
  IconButton,
  Button,
  Chip,
  Stack,
  Box,
  Typography,
  FormHelperText,
  Autocomplete,
  type AutocompleteRenderInputParams,
} from '@mui/material'
import Visibility from '@mui/icons-material/Visibility'
import VisibilityOff from '@mui/icons-material/VisibilityOff'
import LinkIcon from '@mui/icons-material/Link'
import { useTranslation } from 'react-i18next'
import type { StageFieldSchema, FieldCondition } from '../../types/stage-schema'
import { api } from '../../services/api'

interface SchemaFieldProps {
  field: StageFieldSchema
  value: unknown
  onChange: (value: unknown) => void
  allValues: Record<string, unknown>  // 조건부 표시용
  error?: string
  disabled?: boolean
}

export function SchemaField({
  field,
  value,
  onChange,
  allValues,
  error,
  disabled,
}: SchemaFieldProps) {
  const { t } = useTranslation()
  const [showSecret, setShowSecret] = useState(false)
  const [testLoading, setTestLoading] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)

  // 조건부 표시 확인
  const isVisible = useMemo(() => {
    if (!field.show_when) return true
    return checkCondition(field.show_when, allValues)
  }, [field.show_when, allValues])

  if (!isVisible) return null

  // 연결 테스트 실행
  const handleTestConnection = async () => {
    if (!field.test_connection) return

    setTestLoading(true)
    setTestResult(null)

    try {
      // 테스트에 필요한 필드들 수집
      const testData: Record<string, unknown> = {}
      for (const fieldName of field.test_connection.fields) {
        testData[fieldName] = allValues[fieldName]
      }

      const response = await api.post(field.test_connection.endpoint, testData)
      const result = response.data as { success: boolean; message?: string; error?: string }
      setTestResult({
        success: result.success,
        message: result.success ? (result.message || t('common.success')) : (result.error || t('common.failed')),
      })
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : t('common.error'),
      })
    } finally {
      setTestLoading(false)
    }
  }

  // 필드 타입별 렌더링
  switch (field.type) {
    case 'string':
      return (
        <Box>
          <TextField
            label={field.display_name}
            value={value ?? field.default ?? ''}
            onChange={(e) => onChange(e.target.value)}
            required={field.required}
            disabled={disabled}
            error={!!error}
            helperText={error || field.description}
            placeholder={field.placeholder}
            fullWidth
            size="small"
            multiline={field.multiline}
            rows={field.rows || 3}
            slotProps={{ input: {
              style: field.monospace ? { fontFamily: 'monospace' } : undefined,
            } }}
          />
          {field.test_connection && (
            <Stack
              direction="row"
              spacing={1}
              sx={{
                alignItems: "center",
                mt: 1
              }}>
              <Button
                size="small"
                variant="outlined"
                onClick={handleTestConnection}
                disabled={testLoading || disabled}
                startIcon={<LinkIcon />}
              >
                {field.test_connection.label || t('common.testConnection')}
              </Button>
              {testResult && (
                <Typography
                  variant="body2"
                  color={testResult.success ? 'success.main' : 'error.main'}
                >
                  {testResult.message}
                </Typography>
              )}
            </Stack>
          )}
        </Box>
      );

    case 'secret':
      return (
        <Box>
          <TextField
            label={field.display_name}
            type={showSecret ? 'text' : 'password'}
            value={value ?? ''}
            onChange={(e) => onChange(e.target.value)}
            required={field.required}
            disabled={disabled}
            error={!!error}
            helperText={error || field.description}
            placeholder={field.placeholder}
            fullWidth
            size="small"
            slotProps={{ input: {
              style: field.monospace ? { fontFamily: 'monospace' } : undefined,
              endAdornment: (
                <InputAdornment position="end">
                  <IconButton
                    onClick={() => setShowSecret(!showSecret)}
                    edge="end"
                    size="small"
                  >
                    {showSecret ? <VisibilityOff /> : <Visibility />}
                  </IconButton>
                </InputAdornment>
              ),
            } }}
          />
          {field.test_connection && (
            <Stack
              direction="row"
              spacing={1}
              sx={{
                alignItems: "center",
                mt: 1
              }}>
              <Button
                size="small"
                variant="outlined"
                onClick={handleTestConnection}
                disabled={testLoading || disabled}
                startIcon={<LinkIcon />}
              >
                {field.test_connection.label || t('common.testConnection')}
              </Button>
              {testResult && (
                <Typography
                  variant="body2"
                  color={testResult.success ? 'success.main' : 'error.main'}
                >
                  {testResult.message}
                </Typography>
              )}
            </Stack>
          )}
        </Box>
      );

    case 'number':
    case 'integer':
      return (
        <TextField
          label={field.display_name}
          type="number"
          value={value ?? field.default ?? ''}
          onChange={(e) => {
            const val = e.target.value
            if (val === '') {
              onChange(undefined)
            } else {
              onChange(field.type === 'integer' ? parseInt(val) : parseFloat(val))
            }
          }}
          required={field.required}
          disabled={disabled}
          error={!!error}
          helperText={error || field.description}
          placeholder={field.placeholder}
          fullWidth
          size="small"
          slotProps={{ htmlInput: {
            min: field.min,
            max: field.max,
            step: field.type === 'integer' ? 1 : 'any',
          } }}
        />
      )

    case 'boolean':
      return (
        <FormControlLabel
          control={
            <Switch
              checked={Boolean(value ?? field.default ?? false)}
              onChange={(e) => onChange(e.target.checked)}
              disabled={disabled}
              size="small"
            />
          }
          label={
            <Box>
              <Typography variant="body2">{field.display_name}</Typography>
              {field.description && (
                <Typography variant="caption" sx={{
                  color: "text.secondary"
                }}>
                  {field.description}
                </Typography>
              )}
            </Box>
          }
        />
      );

    case 'enum':
      return (
        <FormControl fullWidth size="small" error={!!error} required={field.required}>
          <InputLabel>{field.display_name}</InputLabel>
          <Select
            value={value ?? field.default ?? ''}
            label={field.display_name}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
          >
            {field.options?.map((opt) => (
              <MenuItem key={opt.value} value={opt.value}>
                {opt.label}
                {opt.description && (
                  <Typography
                    variant="caption"
                    sx={{
                      color: "text.secondary",
                      ml: 1
                    }}>
                    - {opt.description}
                  </Typography>
                )}
              </MenuItem>
            ))}
          </Select>
          {(error || field.description) && (
            <FormHelperText>{error || field.description}</FormHelperText>
          )}
        </FormControl>
      );

    case 'array':
      return (
        <ArrayField
          field={field}
          value={value as string[] | undefined}
          onChange={onChange}
          error={error}
          disabled={disabled}
        />
      )

    case 'json':
    case 'code':
      return (
        <TextField
          label={field.display_name}
          value={typeof value === 'object' ? JSON.stringify(value, null, 2) : (value ?? field.default ?? '')}
          onChange={(e) => {
            const val = e.target.value
            if (field.type === 'json') {
              try {
                onChange(JSON.parse(val))
              } catch {
                onChange(val) // 파싱 실패시 문자열로 저장
              }
            } else {
              onChange(val)
            }
          }}
          required={field.required}
          disabled={disabled}
          error={!!error}
          helperText={error || field.description}
          placeholder={field.placeholder}
          fullWidth
          size="small"
          multiline
          rows={field.rows || 5}
          slotProps={{ input: {
            style: { fontFamily: 'monospace' },
          } }}
        />
      )

    case 'duration':
      return (
        <TextField
          label={field.display_name}
          value={value ?? field.default ?? ''}
          onChange={(e) => onChange(e.target.value)}
          required={field.required}
          disabled={disabled}
          error={!!error}
          helperText={error || field.description || t('stage.durationHelp', '예: 30s, 5m, 1h')}
          placeholder={field.placeholder || '30s'}
          fullWidth
          size="small"
          slotProps={{ input: {
            style: { fontFamily: 'monospace' },
          } }}
        />
      )

    case 'keyvalue':
      return (
        <KeyValueField
          field={field}
          value={value as Record<string, string> | undefined}
          onChange={onChange}
          error={error}
          disabled={disabled}
        />
      )

    default:
      return (
        <TextField
          label={field.display_name}
          value={String(value ?? field.default ?? '')}
          onChange={(e) => onChange(e.target.value)}
          required={field.required}
          disabled={disabled}
          error={!!error}
          helperText={error || field.description}
          fullWidth
          size="small"
        />
      )
  }
}

// 배열 필드 컴포넌트
function ArrayField({
  field,
  value,
  onChange,
  error,
  disabled,
}: {
  field: StageFieldSchema
  value: string[] | undefined
  onChange: (value: unknown) => void
  error?: string
  disabled?: boolean
}) {
  const currentValue: string[] = Array.isArray(value) ? value.map(String) : []

  return (
    <Autocomplete
      multiple
      freeSolo
      options={[] as string[]}
      value={currentValue}
      onChange={(_, newValue) => onChange(newValue as string[])}
      disabled={disabled}
      renderValue={(value, getItemProps) =>
        (value as readonly string[]).map((option, index) => {
          const { key, ...itemProps } = getItemProps({ index })
          return <Chip label={String(option)} {...itemProps} key={key} size="small" />
        })
      }
      renderInput={(params: AutocompleteRenderInputParams) => (
        <TextField
          {...params}
          label={field.display_name}
          placeholder={field.placeholder || '값을 입력하고 Enter'}
          error={!!error}
          helperText={error || field.description}
          size="small"
          required={field.required}
        />
      )}
    />
  )
}

// 키-값 필드 컴포넌트
function KeyValueField({
  field,
  value,
  onChange,
  error,
  disabled,
}: {
  field: StageFieldSchema
  value: Record<string, string> | undefined
  onChange: (value: unknown) => void
  error?: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const currentValue = value || {}
  const entries = Object.entries(currentValue)

  const handleAdd = () => {
    onChange({ ...currentValue, '': '' })
  }

  const handleKeyChange = (oldKey: string, newKey: string) => {
    const newValue = { ...currentValue }
    const val = newValue[oldKey]
    delete newValue[oldKey]
    newValue[newKey] = val
    onChange(newValue)
  }

  const handleValueChange = (key: string, val: string) => {
    onChange({ ...currentValue, [key]: val })
  }

  const handleRemove = (key: string) => {
    const newValue = { ...currentValue }
    delete newValue[key]
    onChange(newValue)
  }

  return (
    <Box>
      <Typography variant="body2" gutterBottom>
        {field.display_name}
        {field.required && ' *'}
      </Typography>
      {field.description && (
        <Typography variant="caption" gutterBottom sx={{
          color: "text.secondary"
        }}>
          {field.description}
        </Typography>
      )}
      <Stack spacing={1} sx={{ mt: 1 }}>
        {entries.map(([key, val], index) => (
          <Stack direction="row" spacing={1} key={index}>
            <TextField
              placeholder="Key"
              value={key}
              onChange={(e) => handleKeyChange(key, e.target.value)}
              size="small"
              disabled={disabled}
              sx={{ flex: 1 }}
            />
            <TextField
              placeholder="Value"
              value={val}
              onChange={(e) => handleValueChange(key, e.target.value)}
              size="small"
              disabled={disabled}
              sx={{ flex: 1 }}
            />
            <IconButton
              size="small"
              onClick={() => handleRemove(key)}
              disabled={disabled}
            >
              ✕
            </IconButton>
          </Stack>
        ))}
        <Button
          size="small"
          onClick={handleAdd}
          disabled={disabled}
        >
          + {t('common.add')}
        </Button>
      </Stack>
      {error && (
        <FormHelperText error>{error}</FormHelperText>
      )}
    </Box>
  );
}

// 조건 확인 함수
function checkCondition(condition: FieldCondition, values: Record<string, unknown>): boolean {
  const fieldValue = values[condition.field]

  switch (condition.operator) {
    case 'eq':
      return fieldValue === condition.value
    case 'neq':
      return fieldValue !== condition.value
    case 'in':
      if (Array.isArray(condition.value)) {
        return condition.value.includes(fieldValue as string)
      }
      return false
    case 'exists':
      return fieldValue !== undefined && fieldValue !== null && fieldValue !== ''
    default:
      return true
  }
}
