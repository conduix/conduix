/**
 * StageSchemaDialog - Stage 추가/수정 다이얼로그 (Schema 기반)
 *
 * Stage Schema를 기반으로 폼을 자동 생성하여
 * 새로운 Stage를 쉽게 추가할 수 있게 합니다.
 */
import { useState, useMemo, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Stack,
  Box,
  Typography,
  Chip,
  CircularProgress,
  Alert,
  ListSubheader,
} from '@mui/material'
import { useTranslation } from 'react-i18next'
import type { StageSchema, StageCategory } from '../../types/stage-schema'
import type { Stage, StageType } from '../../types/pipeline'
import { StageSchemaForm } from './StageSchemaForm'
import { useStageSchemas, useStageCategories } from './useStageSchemas'

// 카테고리별 색상
const categoryColors: Record<StageCategory, string> = {
  transform: '#1976d2',
  validation: '#673ab7',
  output: '#9c27b0',
  control: '#ff9800',
}

// 카테고리별 라벨
const categoryLabels: Record<StageCategory, string> = {
  transform: 'Transform',
  validation: 'Validation',
  output: 'Output',
  control: 'Control',
}

interface StageSchemaDialogProps {
  open: boolean
  onClose: () => void
  onSubmit: (stage: Stage) => void
  editingStage?: Stage | null  // 수정 시 기존 Stage
}

export function StageSchemaDialog({
  open,
  onClose,
  onSubmit,
  editingStage,
}: StageSchemaDialogProps) {
  const { t } = useTranslation()
  const { data: schemas = [], isLoading: schemasLoading } = useStageSchemas()
  useStageCategories() // Load categories for potential future use

  // 폼 상태
  const [name, setName] = useState('')
  const [selectedType, setSelectedType] = useState<string>('')
  const [config, setConfig] = useState<Record<string, unknown>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})

  // 선택된 타입의 스키마
  const selectedSchema = useMemo(() => {
    return schemas.find((s: StageSchema) => s.type === selectedType)
  }, [schemas, selectedType])

  // 카테고리별로 그룹화된 스키마
  const schemasByCategory = useMemo(() => {
    const grouped: Record<StageCategory, StageSchema[]> = {
      transform: [],
      validation: [],
      output: [],
      control: [],
    }
    for (const schema of schemas) {
      const cat = schema.category as StageCategory
      if (grouped[cat]) {
        grouped[cat].push(schema)
      }
    }
    return grouped
  }, [schemas])

  // 수정 모드 시 초기값 설정
  useEffect(() => {
    if (editingStage) {
      setName(editingStage.name)
      setSelectedType(editingStage.type)
      setConfig(editingStage.config || {})
    } else {
      setName('')
      setSelectedType('')
      setConfig({})
    }
    setErrors({})
  }, [editingStage, open])

  // 타입 변경 시 config 초기화
  const handleTypeChange = (type: string) => {
    setSelectedType(type)
    // 수정 모드가 아니면 config 초기화
    if (!editingStage || editingStage.type !== type) {
      // 기본값으로 초기화
      const schema = schemas.find((s: StageSchema) => s.type === type)
      if (schema) {
        const defaultConfig: Record<string, unknown> = {}
        for (const field of schema.fields) {
          if (field.default !== undefined) {
            defaultConfig[field.name] = field.default
          }
        }
        setConfig(defaultConfig)
      } else {
        setConfig({})
      }
    }
    setErrors({})
  }

  // 유효성 검사
  const validate = (): boolean => {
    const newErrors: Record<string, string> = {}

    if (!name.trim()) {
      newErrors.name = t('stage.nameRequired', 'Stage 이름을 입력하세요')
    }

    if (!selectedType) {
      newErrors.type = t('stage.typeRequired', 'Stage 타입을 선택하세요')
    }

    // 필수 필드 검사
    if (selectedSchema) {
      for (const field of selectedSchema.fields) {
        if (field.required) {
          const value = config[field.name]
          if (value === undefined || value === null || value === '') {
            newErrors[field.name] = t('common.required', '필수 항목입니다')
          }
        }
      }
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  // 제출
  const handleSubmit = () => {
    if (!validate()) return

    const stage: Stage = {
      id: editingStage?.id || crypto.randomUUID(),
      name: name.trim(),
      type: selectedType as StageType,
      config,
    }

    onSubmit(stage)
    onClose()
  }

  if (schemasLoading) {
    return (
      <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
        <DialogContent>
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
            <CircularProgress />
          </Box>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        {editingStage ? t('stage.editStage', 'Stage 수정') : t('stage.addStage', 'Stage 추가')}
      </DialogTitle>

      <DialogContent>
        <Stack spacing={3} sx={{ mt: 1 }}>
          {/* Stage 이름 */}
          <TextField
            label={t('stage.name', 'Stage 이름')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={!!errors.name}
            helperText={errors.name}
            required
            fullWidth
            size="small"
            autoFocus={!editingStage}
          />

          {/* Stage 타입 선택 */}
          <FormControl fullWidth size="small" error={!!errors.type} required>
            <InputLabel>{t('stage.type', 'Stage 타입')}</InputLabel>
            <Select
              value={selectedType}
              label={t('stage.type', 'Stage 타입')}
              onChange={(e) => handleTypeChange(e.target.value)}
              disabled={!!editingStage}  // 수정 시 타입 변경 불가
            >
              {/* 카테고리별 그룹 */}
              {(['transform', 'validation', 'output', 'control'] as StageCategory[]).map((category) => {
                const categorySchemas = schemasByCategory[category]
                if (!categorySchemas || categorySchemas.length === 0) return null
                return [
                  <ListSubheader
                    key={`header-${category}`}
                    sx={{ backgroundColor: 'background.paper' }}
                  >
                    <Chip
                      label={categoryLabels[category]}
                      size="small"
                      sx={{
                        backgroundColor: categoryColors[category],
                        color: 'white',
                        fontWeight: 'bold',
                      }}
                    />
                  </ListSubheader>,
                  ...categorySchemas.map((schema) => (
                    <MenuItem key={schema.type} value={schema.type}>
                      <Stack direction="row" spacing={1} alignItems="center">
                        <Typography>{schema.display_name}</Typography>
                        {schema.description && (
                          <Typography variant="caption" color="text.secondary">
                            - {schema.description}
                          </Typography>
                        )}
                      </Stack>
                    </MenuItem>
                  )),
                ]
              })}
            </Select>
            {errors.type && (
              <Typography variant="caption" color="error" sx={{ mt: 0.5 }}>
                {errors.type}
              </Typography>
            )}
          </FormControl>

          {/* 선택된 Stage의 설명 */}
          {selectedSchema?.description && (
            <Alert severity="info" icon={false}>
              <Typography variant="body2">{selectedSchema.description}</Typography>
            </Alert>
          )}

          {/* Stage 설정 폼 (Schema 기반) */}
          {selectedSchema && (
            <Box sx={{ pt: 1 }}>
              <Typography variant="subtitle2" gutterBottom>
                {t('stage.configuration', '설정')}
              </Typography>
              <StageSchemaForm
                schema={selectedSchema}
                value={config}
                onChange={setConfig}
                errors={errors}
              />
            </Box>
          )}
        </Stack>
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose}>
          {t('common.cancel', '취소')}
        </Button>
        <Button onClick={handleSubmit} variant="contained" disabled={!selectedType}>
          {editingStage ? t('common.save', '저장') : t('common.add', '추가')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

export default StageSchemaDialog
