/**
 * Stage Section 컴포넌트
 *
 * 파이프라인의 Stage(데이터 변환 단계)를 편집합니다.
 * - Stage 목록 표시 (드래그 앤 드롭)
 * - Stage 추가/편집/삭제
 * - Stage 타입별 설정 폼
 */

import { useState, useCallback } from 'react'
import {
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Typography,
  Box,
  Button,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Stack,
  Chip,
  Paper,
  FormControlLabel,
  Switch,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import AddIcon from '@mui/icons-material/Add'
import EditIcon from '@mui/icons-material/Edit'
import DeleteIcon from '@mui/icons-material/Delete'
import DragHandleIcon from '@mui/icons-material/DragHandle'
import FilterAltIcon from '@mui/icons-material/FilterAlt'
import SwapHorizIcon from '@mui/icons-material/SwapHoriz'
import RemoveCircleIcon from '@mui/icons-material/RemoveCircle'
import MergeIcon from '@mui/icons-material/Merge'
import CallSplitIcon from '@mui/icons-material/CallSplit'
import LockIcon from '@mui/icons-material/Lock'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import NumbersIcon from '@mui/icons-material/Numbers'
import TextFieldsIcon from '@mui/icons-material/TextFields'
import AccessTimeIcon from '@mui/icons-material/AccessTime'
import SpeedIcon from '@mui/icons-material/Speed'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import GavelIcon from '@mui/icons-material/Gavel'
import ForkRightIcon from '@mui/icons-material/ForkRight'
import BlockIcon from '@mui/icons-material/Block'
import { useTranslation } from 'react-i18next'
import { usePipelineEditor } from './PipelineEditorContext'
import { ConfirmDialog } from '../common/ConfirmDialog'
import type { Stage, StageType } from '../../types/pipeline'

// Stage 타입별 설정
const stageTypeConfig: Record<StageType, { color: string; icon: React.ReactNode; label: string }> = {
  filter: { color: '#1976d2', icon: <FilterAltIcon fontSize="small" />, label: 'Filter' },
  remap: { color: '#4caf50', icon: <SwapHorizIcon fontSize="small" />, label: 'Remap' },
  drop: { color: '#f44336', icon: <RemoveCircleIcon fontSize="small" />, label: 'Drop' },
  merge: { color: '#00bcd4', icon: <MergeIcon fontSize="small" />, label: 'Merge' },
  split: { color: '#e91e63', icon: <CallSplitIcon fontSize="small" />, label: 'Split' },
  encrypt: { color: '#ffc107', icon: <LockIcon fontSize="small" />, label: 'Encrypt' },
  dedupe: { color: '#ff5722', icon: <ContentCopyIcon fontSize="small" />, label: 'Dedupe' },
  default: { color: '#3f51b5', icon: <TextFieldsIcon fontSize="small" />, label: 'Default' },
  cast: { color: '#cddc39', icon: <NumbersIcon fontSize="small" />, label: 'Cast' },
  timestamp: { color: '#e91e63', icon: <AccessTimeIcon fontSize="small" />, label: 'Timestamp' },
  throttle: { color: '#ff9800', icon: <SpeedIcon fontSize="small" />, label: 'Throttle' },
  validate: { color: '#9e9e9e', icon: <CheckCircleIcon fontSize="small" />, label: 'Validate' },
  contract: { color: '#673ab7', icon: <GavelIcon fontSize="small" />, label: 'Contract' },
  route: { color: '#00bcd4', icon: <ForkRightIcon fontSize="small" />, label: 'Route' },
  delete: { color: '#f44336', icon: <BlockIcon fontSize="small" />, label: 'Delete' },
}

// Stage form 상태 타입
interface StageFormState {
  name: string
  type: StageType | ''
  config: Record<string, unknown>
}

const initialStageForm: StageFormState = {
  name: '',
  type: '',
  config: {},
}

export function StageSection() {
  const { t } = useTranslation()
  const { pipeline, addStage, updateStage, removeStage, reorderStages } = usePipelineEditor()
  const [expanded, setExpanded] = useState(true)

  // 모달 상태
  const [modalOpen, setModalOpen] = useState(false)
  const [editingStage, setEditingStage] = useState<Stage | null>(null)
  const [stageForm, setStageForm] = useState<StageFormState>(initialStageForm)

  // 삭제 확인 다이얼로그
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [stageToDelete, setStageToDelete] = useState<string | null>(null)

  // 드래그 상태
  const [draggedId, setDraggedId] = useState<string | null>(null)
  const [dropTargetId, setDropTargetId] = useState<string | null>(null)

  const stages = pipeline.stages || []

  // Stage 추가 버튼 핸들러
  const handleAddStage = () => {
    setEditingStage(null)
    setStageForm({
      name: `stage_${stages.length + 1}`,
      type: '',
      config: {},
    })
    setModalOpen(true)
  }

  // Stage 편집 버튼 핸들러
  const handleEditStage = (stage: Stage) => {
    setEditingStage(stage)
    setStageForm({
      name: stage.name,
      type: stage.type as StageType,
      config: { ...stage.config },
    })
    setModalOpen(true)
  }

  // Stage 삭제 버튼 핸들러
  const handleDeleteClick = (id: string) => {
    setStageToDelete(id)
    setDeleteDialogOpen(true)
  }

  // Stage 삭제 확인 핸들러
  const handleDeleteConfirm = () => {
    if (stageToDelete) {
      removeStage(stageToDelete)
      setStageToDelete(null)
    }
    setDeleteDialogOpen(false)
  }

  // Stage 저장 핸들러
  const handleSaveStage = () => {
    if (!stageForm.name || !stageForm.type) return

    if (editingStage) {
      updateStage(editingStage.id, {
        name: stageForm.name,
        type: stageForm.type,
        config: stageForm.config,
      })
    } else {
      addStage({
        id: crypto.randomUUID(),
        name: stageForm.name,
        type: stageForm.type,
        config: stageForm.config,
      })
    }

    setModalOpen(false)
    setStageForm(initialStageForm)
    setEditingStage(null)
  }

  // Stage 타입 변경 핸들러
  const handleTypeChange = (type: StageType | '') => {
    const defaultConfigs: Record<StageType, Record<string, unknown>> = {
      filter: { condition: '' },
      remap: { mappings: {} },
      drop: { fields: [] },
      merge: { source_fields: [], target_field: '', delimiter: ' ' },
      split: { source_field: '', pattern: '', target_fields: [] },
      encrypt: { fields: [], method: 'sha256' },
      dedupe: { key_fields: [], strategy: 'keep_first' },
      default: { defaults: {}, only_null: true },
      cast: { casts: {}, error_action: 'null' },
      timestamp: { action: 'add', target_field: '', timezone: 'UTC' },
      throttle: { rate: 100, interval: 'second', drop_on_limit: false },
      validate: { schema: {}, drop_on_fail: false },
      contract: { rules: [], on_violation: 'reject' },
      route: { field: '', routes: [] },
      delete: { mode: 'physical' },
    }

    setStageForm((prev) => ({
      ...prev,
      type,
      config: type ? defaultConfigs[type] || {} : {},
    }))
  }

  // Config 필드 업데이트
  const handleConfigChange = (field: string, value: unknown) => {
    setStageForm((prev) => ({
      ...prev,
      config: { ...prev.config, [field]: value },
    }))
  }

  // 드래그 시작
  const handleDragStart = (e: React.DragEvent, id: string) => {
    setDraggedId(id)
    e.dataTransfer.effectAllowed = 'move'
  }

  // 드래그 오버
  const handleDragOver = useCallback((e: React.DragEvent, id: string) => {
    e.preventDefault()
    setDropTargetId(id)
  }, [])

  // 드롭
  const handleDrop = useCallback(
    (e: React.DragEvent, targetId: string) => {
      e.preventDefault()
      if (draggedId && draggedId !== targetId) {
        const fromIndex = stages.findIndex((s) => s.id === draggedId)
        const toIndex = stages.findIndex((s) => s.id === targetId)
        if (fromIndex !== -1 && toIndex !== -1) {
          reorderStages(fromIndex, toIndex)
        }
      }
      setDraggedId(null)
      setDropTargetId(null)
    },
    [draggedId, stages, reorderStages]
  )

  // 드래그 종료
  const handleDragEnd = () => {
    setDraggedId(null)
    setDropTargetId(null)
  }

  // Stage 타입별 설정 필드 렌더링
  const renderConfigFields = () => {
    const config = stageForm.config

    switch (stageForm.type) {
      case 'filter':
        return (
          <TextField
            label={t('pipelineEditor.stage.condition')}
            value={config.condition || ''}
            onChange={(e) => handleConfigChange('condition', e.target.value)}
            placeholder='.status == "active"'
            helperText={t('pipelineEditor.stage.conditionHelp')}
            fullWidth
            required
          />
        )

      case 'remap':
        return (
          <TextField
            label={t('pipelineEditor.stage.mappings')}
            value={
              typeof config.mappings === 'string'
                ? config.mappings
                : JSON.stringify(config.mappings || {}, null, 2)
            }
            onChange={(e) => {
              try {
                handleConfigChange('mappings', JSON.parse(e.target.value))
              } catch {
                handleConfigChange('mappings', e.target.value)
              }
            }}
            placeholder='{"old_name": "new_name"}'
            helperText={t('pipelineEditor.stage.mappingsHelp')}
            multiline
            rows={3}
            fullWidth
            required
          />
        )

      case 'drop':
        return (
          <TextField
            label={t('pipelineEditor.stage.fields')}
            value={
              Array.isArray(config.fields) ? (config.fields as string[]).join(', ') : config.fields || ''
            }
            onChange={(e) => handleConfigChange('fields', e.target.value.split(',').map((s) => s.trim()))}
            placeholder="field1, field2, field3"
            helperText={t('pipelineEditor.stage.fieldsHelp')}
            fullWidth
            required
          />
        )

      case 'merge':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.sourceFields')}
              value={
                Array.isArray(config.source_fields)
                  ? (config.source_fields as string[]).join(', ')
                  : config.source_fields || ''
              }
              onChange={(e) =>
                handleConfigChange('source_fields', e.target.value.split(',').map((s) => s.trim()))
              }
              placeholder="first_name, last_name"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.stage.targetField')}
              value={config.target_field || ''}
              onChange={(e) => handleConfigChange('target_field', e.target.value)}
              placeholder="full_name"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.stage.delimiter')}
              value={config.delimiter || ' '}
              onChange={(e) => handleConfigChange('delimiter', e.target.value)}
              placeholder=" "
              sx={{ width: 100 }}
            />
          </Stack>
        )

      case 'encrypt':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.fields')}
              value={
                Array.isArray(config.fields) ? (config.fields as string[]).join(', ') : config.fields || ''
              }
              onChange={(e) => handleConfigChange('fields', e.target.value.split(',').map((s) => s.trim()))}
              placeholder="email, phone, ssn"
              fullWidth
              required
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.stage.method')}</InputLabel>
              <Select
                value={config.method || 'sha256'}
                onChange={(e) => handleConfigChange('method', e.target.value)}
                label={t('pipelineEditor.stage.method')}
              >
                <MenuItem value="sha256">SHA-256</MenuItem>
                <MenuItem value="sha512">SHA-512</MenuItem>
                <MenuItem value="aes256">AES-256</MenuItem>
                <MenuItem value="bcrypt">BCrypt</MenuItem>
                <MenuItem value="mask">Mask</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      case 'dedupe':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.keyFields')}
              value={
                Array.isArray(config.key_fields)
                  ? (config.key_fields as string[]).join(', ')
                  : config.key_fields || ''
              }
              onChange={(e) =>
                handleConfigChange('key_fields', e.target.value.split(',').map((s) => s.trim()))
              }
              placeholder="id, timestamp"
              fullWidth
              required
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.stage.strategy')}</InputLabel>
              <Select
                value={config.strategy || 'keep_first'}
                onChange={(e) => handleConfigChange('strategy', e.target.value)}
                label={t('pipelineEditor.stage.strategy')}
              >
                <MenuItem value="keep_first">{t('pipelineEditor.stage.keepFirst')}</MenuItem>
                <MenuItem value="keep_last">{t('pipelineEditor.stage.keepLast')}</MenuItem>
                <MenuItem value="keep_latest">{t('pipelineEditor.stage.keepLatest')}</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      case 'cast':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.casts')}
              value={
                typeof config.casts === 'string'
                  ? config.casts
                  : JSON.stringify(config.casts || {}, null, 2)
              }
              onChange={(e) => {
                try {
                  handleConfigChange('casts', JSON.parse(e.target.value))
                } catch {
                  handleConfigChange('casts', e.target.value)
                }
              }}
              placeholder='{"age": "int", "price": "float"}'
              helperText={t('pipelineEditor.stage.castsHelp')}
              multiline
              rows={2}
              fullWidth
              required
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.stage.errorAction')}</InputLabel>
              <Select
                value={config.error_action || 'null'}
                onChange={(e) => handleConfigChange('error_action', e.target.value)}
                label={t('pipelineEditor.stage.errorAction')}
              >
                <MenuItem value="null">{t('pipelineEditor.stage.setNull')}</MenuItem>
                <MenuItem value="drop">{t('pipelineEditor.stage.dropRecord')}</MenuItem>
                <MenuItem value="keep">{t('pipelineEditor.stage.keepOriginal')}</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      case 'timestamp':
        return (
          <Stack spacing={2}>
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.stage.action')}</InputLabel>
              <Select
                value={config.action || 'add'}
                onChange={(e) => handleConfigChange('action', e.target.value)}
                label={t('pipelineEditor.stage.action')}
              >
                <MenuItem value="add">{t('pipelineEditor.stage.addTimestamp')}</MenuItem>
                <MenuItem value="convert">{t('pipelineEditor.stage.convert')}</MenuItem>
                <MenuItem value="format">{t('pipelineEditor.stage.format')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('pipelineEditor.stage.targetField')}
              value={config.target_field || ''}
              onChange={(e) => handleConfigChange('target_field', e.target.value)}
              placeholder="created_at"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.stage.timezone')}
              value={config.timezone || 'UTC'}
              onChange={(e) => handleConfigChange('timezone', e.target.value)}
              placeholder="UTC"
              sx={{ width: 150 }}
            />
          </Stack>
        )

      case 'throttle':
        return (
          <Stack spacing={2}>
            <Stack direction="row" spacing={2}>
              <TextField
                label={t('pipelineEditor.stage.rate')}
                type="number"
                value={config.rate || 100}
                onChange={(e) => handleConfigChange('rate', Number(e.target.value))}
                inputProps={{ min: 1 }}
                sx={{ width: 120 }}
                required
              />
              <FormControl sx={{ width: 120 }}>
                <InputLabel>{t('pipelineEditor.stage.interval')}</InputLabel>
                <Select
                  value={config.interval || 'second'}
                  onChange={(e) => handleConfigChange('interval', e.target.value)}
                  label={t('pipelineEditor.stage.interval')}
                >
                  <MenuItem value="second">{t('pipelineEditor.input.perSecond')}</MenuItem>
                  <MenuItem value="minute">{t('pipelineEditor.input.perMinute')}</MenuItem>
                  <MenuItem value="hour">{t('pipelineEditor.input.perHour')}</MenuItem>
                </Select>
              </FormControl>
            </Stack>
            <FormControlLabel
              control={
                <Switch
                  checked={config.drop_on_limit === true}
                  onChange={(e) => handleConfigChange('drop_on_limit', e.target.checked)}
                />
              }
              label={t('pipelineEditor.stage.dropOnLimit')}
            />
          </Stack>
        )

      case 'validate':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.schema')}
              value={
                typeof config.schema === 'string'
                  ? config.schema
                  : JSON.stringify(config.schema || {}, null, 2)
              }
              onChange={(e) => {
                try {
                  handleConfigChange('schema', JSON.parse(e.target.value))
                } catch {
                  handleConfigChange('schema', e.target.value)
                }
              }}
              placeholder='{"type": "object", "properties": {...}}'
              multiline
              rows={4}
              fullWidth
            />
            <FormControlLabel
              control={
                <Switch
                  checked={config.drop_on_fail === true}
                  onChange={(e) => handleConfigChange('drop_on_fail', e.target.checked)}
                />
              }
              label={t('pipelineEditor.stage.dropOnFail')}
            />
          </Stack>
        )

      case 'default':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.stage.defaults')}
              value={
                typeof config.defaults === 'string'
                  ? config.defaults
                  : JSON.stringify(config.defaults || {}, null, 2)
              }
              onChange={(e) => {
                try {
                  handleConfigChange('defaults', JSON.parse(e.target.value))
                } catch {
                  handleConfigChange('defaults', e.target.value)
                }
              }}
              placeholder='{"status": "unknown", "count": 0}'
              multiline
              rows={2}
              fullWidth
            />
            <FormControlLabel
              control={
                <Switch
                  checked={config.only_null !== false}
                  onChange={(e) => handleConfigChange('only_null', e.target.checked)}
                />
              }
              label={t('pipelineEditor.stage.onlyNull')}
            />
          </Stack>
        )

      default:
        return (
          <Typography color="text.secondary">
            {stageForm.type
              ? t('pipelineEditor.stage.configNotSupported')
              : t('pipelineEditor.stage.selectType')}
          </Typography>
        )
    }
  }

  return (
    <>
      <Accordion expanded={expanded} onChange={(_, isExpanded) => setExpanded(isExpanded)}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="subtitle1" fontWeight="medium">
              {t('pipelineEditor.stage.title')}
            </Typography>
            <Chip label={stages.length} size="small" color="primary" variant="outlined" />
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Stack spacing={1}>
            {/* Stage 목록 */}
            {stages.map((stage, index) => (
              <Paper
                key={stage.id}
                elevation={draggedId === stage.id ? 4 : 1}
                sx={{
                  p: 1.5,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                  cursor: 'grab',
                  opacity: draggedId === stage.id ? 0.5 : 1,
                  bgcolor: dropTargetId === stage.id ? 'action.hover' : 'background.paper',
                  transition: 'all 0.2s',
                }}
                draggable
                onDragStart={(e) => handleDragStart(e, stage.id)}
                onDragOver={(e) => handleDragOver(e, stage.id)}
                onDrop={(e) => handleDrop(e, stage.id)}
                onDragEnd={handleDragEnd}
              >
                <DragHandleIcon sx={{ color: 'text.secondary' }} />
                <Typography variant="body2" color="text.secondary" sx={{ width: 24 }}>
                  {index + 1}
                </Typography>
                {stageTypeConfig[stage.type as StageType] && (
                  <Chip
                    icon={stageTypeConfig[stage.type as StageType].icon as React.ReactElement}
                    label={stageTypeConfig[stage.type as StageType].label}
                    size="small"
                    sx={{
                      bgcolor: stageTypeConfig[stage.type as StageType].color,
                      color: 'white',
                    }}
                  />
                )}
                <Typography variant="body2" sx={{ flex: 1 }}>
                  {stage.name}
                </Typography>
                <IconButton size="small" onClick={() => handleEditStage(stage)}>
                  <EditIcon fontSize="small" />
                </IconButton>
                <IconButton size="small" onClick={() => handleDeleteClick(stage.id)} color="error">
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </Paper>
            ))}

            {/* Stage 추가 버튼 */}
            <Button startIcon={<AddIcon />} onClick={handleAddStage} variant="outlined" fullWidth>
              {t('pipelineEditor.stage.add')}
            </Button>
          </Stack>
        </AccordionDetails>
      </Accordion>

      {/* Stage 편집 모달 */}
      <Dialog open={modalOpen} onClose={() => setModalOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingStage ? t('pipelineEditor.stage.edit') : t('pipelineEditor.stage.add')}
        </DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label={t('pipelineEditor.stage.name')}
              value={stageForm.name}
              onChange={(e) => setStageForm((prev) => ({ ...prev, name: e.target.value }))}
              fullWidth
              required
            />
            <FormControl fullWidth required>
              <InputLabel>{t('pipelineEditor.stage.type')}</InputLabel>
              <Select
                value={stageForm.type}
                onChange={(e) => handleTypeChange(e.target.value as StageType | '')}
                label={t('pipelineEditor.stage.type')}
              >
                {(Object.keys(stageTypeConfig) as StageType[]).map((type) => {
                  const config = stageTypeConfig[type]
                  return (
                    <MenuItem key={type} value={type}>
                      <Stack direction="row" spacing={1} alignItems="center">
                        <Chip
                          icon={config.icon as React.ReactElement}
                          label={config.label}
                          size="small"
                          sx={{ bgcolor: config.color, color: 'white' }}
                        />
                      </Stack>
                    </MenuItem>
                  )
                })}
              </Select>
            </FormControl>

            {/* 타입별 설정 */}
            {stageForm.type && (
              <Box sx={{ mt: 2 }}>
                <Typography variant="subtitle2" gutterBottom>
                  {t('pipelineEditor.stage.configuration')}
                </Typography>
                {renderConfigFields()}
              </Box>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button>
          <Button
            onClick={handleSaveStage}
            variant="contained"
            disabled={!stageForm.name || !stageForm.type}
          >
            {t('common.save')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 삭제 확인 다이얼로그 */}
      <ConfirmDialog
        open={deleteDialogOpen}
        title={t('pipelineEditor.stage.deleteConfirmTitle')}
        message={t('pipelineEditor.stage.deleteConfirmMessage')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteDialogOpen(false)}
        severity="error"
      />
    </>
  )
}
