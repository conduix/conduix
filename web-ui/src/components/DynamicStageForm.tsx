import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Slider,
  Switch,
  FormControlLabel,
  Typography,
  Chip,
  CircularProgress,
  Alert,
} from '@mui/material'
import { useTranslation } from 'react-i18next'
import { getStageSchema } from '../services/pluginApi'
import type { StageSchemaResponse } from '../types/plugin'

interface DynamicStageFormProps {
  stageType: string
  config: Record<string, unknown>
  onChange: (config: Record<string, unknown>) => void
}

interface JsonSchemaProperty {
  type?: string
  title?: string
  description?: string
  default?: unknown
  enum?: string[]
  enumNames?: string[]
  minimum?: number
  maximum?: number
  minItems?: number
  items?: { type?: string }
  'ui:widget'?: string
}

interface JsonSchema {
  type?: string
  properties?: Record<string, JsonSchemaProperty>
  required?: string[]
}

// JSON Schema 기반 동적 폼 렌더링 컴포넌트
export default function DynamicStageForm({ stageType, config, onChange }: DynamicStageFormProps) {
  const { t } = useTranslation()
  const [schema, setSchema] = useState<StageSchemaResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadSchema = useCallback(async () => {
    if (!stageType) return
    setLoading(true)
    setError(null)
    try {
      const data = await getStageSchema(stageType)
      setSchema(data)
    } catch {
      setError(t('plugin.schemaLoadError'))
    } finally {
      setLoading(false)
    }
  }, [stageType, t])

  useEffect(() => {
    loadSchema()
  }, [loadSchema])

  const handleChange = (field: string, value: unknown) => {
    onChange({ ...config, [field]: value })
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
        <CircularProgress size={24} />
      </Box>
    )
  }

  if (error) {
    return <Alert severity="warning">{error}</Alert>
  }

  if (!schema?.configSchema) {
    return <Alert severity="info">{t('plugin.noSchema')}</Alert>
  }

  const configSchema = schema.configSchema as JsonSchema
  const properties = configSchema.properties || {}
  const required = configSchema.required || []

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {schema.pluginImage && (
        <Chip
          size="small"
          label={`Plugin: ${schema.pluginImage}`}
          variant="outlined"
          color="primary"
          sx={{ alignSelf: 'flex-start' }}
        />
      )}

      {Object.entries(properties).map(([fieldName, prop]) => {
        const isRequired = required.includes(fieldName)
        const currentValue = config[fieldName] ?? prop.default
        const uiWidget = prop['ui:widget']

        // Slider widget
        if (uiWidget === 'slider' && prop.type === 'number') {
          return (
            <Box key={fieldName}>
              <Typography variant="body2" gutterBottom>
                {prop.title || fieldName}
                {isRequired && ' *'}
              </Typography>
              {prop.description && (
                <Typography variant="caption" color="text.secondary" gutterBottom>
                  {prop.description}
                </Typography>
              )}
              <Slider
                value={(currentValue as number) ?? prop.minimum ?? 0}
                min={prop.minimum ?? 0}
                max={prop.maximum ?? 100}
                step={(prop.maximum ?? 100) <= 1 ? 0.01 : 1}
                valueLabelDisplay="auto"
                onChange={(_, val) => handleChange(fieldName, val)}
              />
            </Box>
          )
        }

        // Enum / Select
        if (prop.enum) {
          return (
            <FormControl key={fieldName} fullWidth size="small" required={isRequired}>
              <InputLabel>{prop.title || fieldName}</InputLabel>
              <Select
                value={(currentValue as string) ?? ''}
                label={prop.title || fieldName}
                onChange={(e) => handleChange(fieldName, e.target.value)}
              >
                {prop.enum.map((val, i) => (
                  <MenuItem key={val} value={val}>
                    {prop.enumNames?.[i] || val}
                  </MenuItem>
                ))}
              </Select>
              {prop.description && (
                <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>
                  {prop.description}
                </Typography>
              )}
            </FormControl>
          )
        }

        // Boolean
        if (prop.type === 'boolean') {
          return (
            <FormControlLabel
              key={fieldName}
              control={
                <Switch
                  checked={!!currentValue}
                  onChange={(e) => handleChange(fieldName, e.target.checked)}
                />
              }
              label={
                <Box>
                  <Typography variant="body2">{prop.title || fieldName}</Typography>
                  {prop.description && (
                    <Typography variant="caption" color="text.secondary">
                      {prop.description}
                    </Typography>
                  )}
                </Box>
              }
            />
          )
        }

        // Number
        if (prop.type === 'number' || prop.type === 'integer') {
          return (
            <TextField
              key={fieldName}
              label={prop.title || fieldName}
              type="number"
              value={currentValue ?? ''}
              onChange={(e) => {
                const val = e.target.value === '' ? undefined : Number(e.target.value)
                handleChange(fieldName, val)
              }}
              required={isRequired}
              size="small"
              fullWidth
              helperText={prop.description}
              inputProps={{
                min: prop.minimum,
                max: prop.maximum,
              }}
            />
          )
        }

        // Array of strings
        if (prop.type === 'array' && prop.items?.type === 'string') {
          const arrayValue = (currentValue as string[]) || []
          return (
            <Box key={fieldName}>
              <TextField
                label={prop.title || fieldName}
                value={arrayValue.join(', ')}
                onChange={(e) => {
                  const vals = e.target.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean)
                  handleChange(fieldName, vals)
                }}
                required={isRequired}
                size="small"
                fullWidth
                helperText={prop.description || t('plugin.arrayFieldHelp')}
              />
            </Box>
          )
        }

        // Default: string text field
        return (
          <TextField
            key={fieldName}
            label={prop.title || fieldName}
            value={(currentValue as string) ?? ''}
            onChange={(e) => handleChange(fieldName, e.target.value)}
            required={isRequired}
            size="small"
            fullWidth
            helperText={prop.description}
            multiline={uiWidget === 'textarea'}
            rows={uiWidget === 'textarea' ? 3 : undefined}
          />
        )
      })}
    </Box>
  )
}
