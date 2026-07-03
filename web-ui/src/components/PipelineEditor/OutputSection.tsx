/**
 * Output Section 컴포넌트
 *
 * 파이프라인의 Output(데이터 출력 대상)을 편집합니다.
 * - Output 목록 표시
 * - Output 추가/편집/삭제
 * - Pre-stages 설정 (Output별 전용 변환)
 */

import { useState } from 'react'
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
  Divider,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import AddIcon from '@mui/icons-material/Add'
import EditIcon from '@mui/icons-material/Edit'
import DeleteIcon from '@mui/icons-material/Delete'
import OutputIcon from '@mui/icons-material/Output'
import StorageIcon from '@mui/icons-material/Storage'
import SearchIcon from '@mui/icons-material/Search'
import BoltIcon from '@mui/icons-material/Bolt'
import CloudIcon from '@mui/icons-material/Cloud'
import FolderIcon from '@mui/icons-material/Folder'
import ApiIcon from '@mui/icons-material/Api'
import { useTranslation } from 'react-i18next'
import { usePipelineEditor } from './PipelineEditorContext'
import { ConfirmDialog } from '../common/ConfirmDialog'
import type { Output, OutputType } from '../../types/pipeline'

// Output 타입별 설정
const outputTypeConfig: Record<OutputType, { color: string; icon: React.ReactNode; label: string }> = {
  sql: { color: '#ff9800', icon: <StorageIcon fontSize="small" />, label: 'SQL' },
  elasticsearch: { color: '#00bcd4', icon: <SearchIcon fontSize="small" />, label: 'Elasticsearch' },
  kafka: { color: '#4caf50', icon: <BoltIcon fontSize="small" />, label: 'Kafka' },
  mongodb: { color: '#4db33d', icon: <StorageIcon fontSize="small" />, label: 'MongoDB' },
  s3: { color: '#ff9900', icon: <CloudIcon fontSize="small" />, label: 'S3' },
  rest_api: { color: '#1976d2', icon: <ApiIcon fontSize="small" />, label: 'REST API' },
  file: { color: '#607d8b', icon: <FolderIcon fontSize="small" />, label: 'File' },
}

// Output form 상태 타입
interface OutputFormState {
  name: string
  type: OutputType | ''
  config: Record<string, unknown>
}

const initialOutputForm: OutputFormState = {
  name: '',
  type: '',
  config: {},
}

export function OutputSection() {
  const { t } = useTranslation()
  const { pipeline, addOutput, updateOutput, removeOutput } = usePipelineEditor()
  const [expanded, setExpanded] = useState(true)

  // 모달 상태
  const [modalOpen, setModalOpen] = useState(false)
  const [editingOutput, setEditingOutput] = useState<Output | null>(null)
  const [outputForm, setOutputForm] = useState<OutputFormState>(initialOutputForm)

  // 삭제 확인 다이얼로그
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [outputToDelete, setOutputToDelete] = useState<string | null>(null)

  const outputs = pipeline.outputs || []

  // Output 추가 버튼 핸들러
  const handleAddOutput = () => {
    setEditingOutput(null)
    setOutputForm({
      name: `output_${outputs.length + 1}`,
      type: '',
      config: {},
    })
    setModalOpen(true)
  }

  // Output 편집 버튼 핸들러
  const handleEditOutput = (output: Output) => {
    setEditingOutput(output)
    setOutputForm({
      name: output.name,
      type: output.type,
      config: { ...output.config },
    })
    setModalOpen(true)
  }

  // Output 삭제 버튼 핸들러
  const handleDeleteClick = (id: string) => {
    setOutputToDelete(id)
    setDeleteDialogOpen(true)
  }

  // Output 삭제 확인 핸들러
  const handleDeleteConfirm = () => {
    if (outputToDelete) {
      removeOutput(outputToDelete)
      setOutputToDelete(null)
    }
    setDeleteDialogOpen(false)
  }

  // Output 저장 핸들러
  const handleSaveOutput = () => {
    if (!outputForm.name || !outputForm.type) return

    if (editingOutput) {
      updateOutput(editingOutput.id, {
        name: outputForm.name,
        type: outputForm.type,
        config: outputForm.config,
      })
    } else {
      addOutput({
        id: crypto.randomUUID(),
        name: outputForm.name,
        type: outputForm.type,
        config: outputForm.config,
      })
    }

    setModalOpen(false)
    setOutputForm(initialOutputForm)
    setEditingOutput(null)
  }

  // Output 타입 변경 핸들러
  const handleTypeChange = (type: OutputType | '') => {
    const defaultConfigs: Record<OutputType, Record<string, unknown>> = {
      sql: {
        connection_string: '',
        table: '',
        batch_size: 100,
        upsert: true,
        conflict_columns: [],
      },
      elasticsearch: {
        addresses: ['http://localhost:9200'],
        index: '',
        batch_size: 100,
      },
      kafka: {
        brokers: ['localhost:9092'],
        topic: '',
      },
      mongodb: {
        uri: '',
        database: '',
        collection: '',
      },
      s3: {
        bucket: '',
        region: 'us-east-1',
        prefix: '',
      },
      rest_api: {
        url: '',
        method: 'POST',
        headers: {},
      },
      file: {
        path: '',
        format: 'json',
      },
    }

    setOutputForm((prev) => ({
      ...prev,
      type,
      config: type ? defaultConfigs[type] || {} : {},
    }))
  }

  // Config 필드 업데이트
  const handleConfigChange = (field: string, value: unknown) => {
    setOutputForm((prev) => ({
      ...prev,
      config: { ...prev.config, [field]: value },
    }))
  }

  // Output 타입별 설정 필드 렌더링
  const renderConfigFields = () => {
    const config = outputForm.config

    switch (outputForm.type) {
      case 'sql':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.connectionString')}
              type="password"
              value={config.connection_string || ''}
              onChange={(e) => handleConfigChange('connection_string', e.target.value)}
              placeholder="postgres://user:pass@host:5432/db"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.table')}
              value={config.table || ''}
              onChange={(e) => handleConfigChange('table', e.target.value)}
              placeholder="my_table"
              fullWidth
              required
            />
            <Stack direction="row" spacing={2}>
              <TextField
                label={t('pipelineEditor.output.batchSize')}
                type="number"
                value={config.batch_size || 100}
                onChange={(e) => handleConfigChange('batch_size', Number(e.target.value))}
                slotProps={{ htmlInput: { min: 1, max: 10000 } }}
                sx={{ width: 120 }}
              />
              <FormControlLabel
                control={
                  <Switch
                    checked={config.upsert !== false}
                    onChange={(e) => handleConfigChange('upsert', e.target.checked)}
                  />
                }
                label={t('pipelineEditor.output.upsert')}
              />
            </Stack>
            <TextField
              label={t('pipelineEditor.output.conflictColumns')}
              value={
                Array.isArray(config.conflict_columns)
                  ? (config.conflict_columns as string[]).join(', ')
                  : config.conflict_columns || ''
              }
              onChange={(e) =>
                handleConfigChange('conflict_columns', e.target.value.split(',').map((s) => s.trim()))
              }
              placeholder="id"
              helperText={t('pipelineEditor.output.conflictColumnsHelp')}
              fullWidth
            />
          </Stack>
        )

      case 'elasticsearch':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.addresses')}
              value={
                Array.isArray(config.addresses)
                  ? (config.addresses as string[]).join(', ')
                  : config.addresses || ''
              }
              onChange={(e) =>
                handleConfigChange('addresses', e.target.value.split(',').map((s) => s.trim()))
              }
              placeholder="http://localhost:9200"
              helperText={t('pipelineEditor.output.addressesHelp')}
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.index')}
              value={config.index || ''}
              onChange={(e) => handleConfigChange('index', e.target.value)}
              placeholder="my-index"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.batchSize')}
              type="number"
              value={config.batch_size || 100}
              onChange={(e) => handleConfigChange('batch_size', Number(e.target.value))}
              slotProps={{ htmlInput: { min: 1, max: 10000 } }}
              sx={{ width: 120 }}
            />
            <Divider>{t('pipelineEditor.output.authentication')}</Divider>
            <Stack direction="row" spacing={2}>
              <TextField
                label={t('pipelineEditor.output.username')}
                value={config.username || ''}
                onChange={(e) => handleConfigChange('username', e.target.value)}
                sx={{ flex: 1 }}
              />
              <TextField
                label={t('pipelineEditor.output.password')}
                type="password"
                value={config.password || ''}
                onChange={(e) => handleConfigChange('password', e.target.value)}
                sx={{ flex: 1 }}
              />
            </Stack>
          </Stack>
        )

      case 'kafka':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.brokers')}
              value={
                Array.isArray(config.brokers)
                  ? (config.brokers as string[]).join(', ')
                  : config.brokers || ''
              }
              onChange={(e) =>
                handleConfigChange('brokers', e.target.value.split(',').map((s) => s.trim()))
              }
              placeholder="localhost:9092"
              helperText={t('pipelineEditor.output.brokersHelp')}
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.topic')}
              value={config.topic || ''}
              onChange={(e) => handleConfigChange('topic', e.target.value)}
              placeholder="output-topic"
              fullWidth
              required
            />
          </Stack>
        )

      case 'mongodb':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.uri')}
              type="password"
              value={config.uri || ''}
              onChange={(e) => handleConfigChange('uri', e.target.value)}
              placeholder="mongodb://localhost:27017"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.database')}
              value={config.database || ''}
              onChange={(e) => handleConfigChange('database', e.target.value)}
              placeholder="mydb"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.collection')}
              value={config.collection || ''}
              onChange={(e) => handleConfigChange('collection', e.target.value)}
              placeholder="mycollection"
              fullWidth
              required
            />
          </Stack>
        )

      case 's3':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.bucket')}
              value={config.bucket || ''}
              onChange={(e) => handleConfigChange('bucket', e.target.value)}
              placeholder="my-bucket"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.output.region')}
              value={config.region || 'us-east-1'}
              onChange={(e) => handleConfigChange('region', e.target.value)}
              placeholder="us-east-1"
              sx={{ width: 150 }}
            />
            <TextField
              label={t('pipelineEditor.output.prefix')}
              value={config.prefix || ''}
              onChange={(e) => handleConfigChange('prefix', e.target.value)}
              placeholder="data/"
              fullWidth
            />
            <TextField
              label={t('pipelineEditor.output.endpoint')}
              value={config.endpoint || ''}
              onChange={(e) => handleConfigChange('endpoint', e.target.value)}
              placeholder="https://s3.amazonaws.com"
              helperText={t('pipelineEditor.output.endpointHelp')}
              fullWidth
            />
          </Stack>
        )

      case 'rest_api':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.url')}
              value={config.url || ''}
              onChange={(e) => handleConfigChange('url', e.target.value)}
              placeholder="https://api.example.com/data"
              fullWidth
              required
            />
            <FormControl sx={{ width: 120 }}>
              <InputLabel>{t('pipelineEditor.output.method')}</InputLabel>
              <Select
                value={config.method || 'POST'}
                onChange={(e) => handleConfigChange('method', e.target.value)}
                label={t('pipelineEditor.output.method')}
              >
                <MenuItem value="POST">POST</MenuItem>
                <MenuItem value="PUT">PUT</MenuItem>
                <MenuItem value="PATCH">PATCH</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('pipelineEditor.output.headers')}
              value={
                typeof config.headers === 'string'
                  ? config.headers
                  : JSON.stringify(config.headers || {}, null, 2)
              }
              onChange={(e) => {
                try {
                  handleConfigChange('headers', JSON.parse(e.target.value))
                } catch {
                  handleConfigChange('headers', e.target.value)
                }
              }}
              placeholder='{"Authorization": "Bearer token"}'
              multiline
              rows={2}
              fullWidth
            />
          </Stack>
        )

      case 'file':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.output.path')}
              value={config.path || ''}
              onChange={(e) => handleConfigChange('path', e.target.value)}
              placeholder="/data/output"
              fullWidth
              required
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.output.format')}</InputLabel>
              <Select
                value={config.format || 'json'}
                onChange={(e) => handleConfigChange('format', e.target.value)}
                label={t('pipelineEditor.output.format')}
              >
                <MenuItem value="json">JSON</MenuItem>
                <MenuItem value="csv">CSV</MenuItem>
                <MenuItem value="parquet">Parquet</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      default:
        return (
          <Typography sx={{
            color: "text.secondary"
          }}>
            {outputForm.type
              ? t('pipelineEditor.output.configNotSupported')
              : t('pipelineEditor.output.selectType')}
          </Typography>
        );
    }
  }

  return (
    <>
      <Accordion expanded={expanded} onChange={(_, isExpanded) => setExpanded(isExpanded)}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="subtitle1" sx={{
              fontWeight: "medium"
            }}>
              {t('pipelineEditor.output.title')}
            </Typography>
            <Chip label={outputs.length} size="small" color="secondary" variant="outlined" />
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Stack spacing={1}>
            {/* Output 목록 */}
            {outputs.map((output) => (
              <Paper
                key={output.id}
                elevation={1}
                sx={{
                  p: 1.5,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                }}
              >
                <OutputIcon sx={{ color: 'text.secondary' }} />
                {outputTypeConfig[output.type] && (
                  <Chip
                    icon={outputTypeConfig[output.type].icon as React.ReactElement}
                    label={outputTypeConfig[output.type].label}
                    size="small"
                    sx={{
                      bgcolor: outputTypeConfig[output.type].color,
                      color: 'white',
                    }}
                  />
                )}
                <Typography variant="body2" sx={{ flex: 1 }}>
                  {output.name}
                </Typography>
                {output.pre_stages && output.pre_stages.length > 0 && (
                  <Chip
                    label={`${output.pre_stages.length} pre-stages`}
                    size="small"
                    variant="outlined"
                  />
                )}
                <IconButton size="small" onClick={() => handleEditOutput(output)}>
                  <EditIcon fontSize="small" />
                </IconButton>
                <IconButton size="small" onClick={() => handleDeleteClick(output.id)} color="error">
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </Paper>
            ))}

            {/* Output 추가 버튼 */}
            <Button startIcon={<AddIcon />} onClick={handleAddOutput} variant="outlined" fullWidth>
              {t('pipelineEditor.output.add')}
            </Button>
          </Stack>
        </AccordionDetails>
      </Accordion>
      {/* Output 편집 모달 */}
      <Dialog open={modalOpen} onClose={() => setModalOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingOutput ? t('pipelineEditor.output.edit') : t('pipelineEditor.output.add')}
        </DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label={t('pipelineEditor.output.name')}
              value={outputForm.name}
              onChange={(e) => setOutputForm((prev) => ({ ...prev, name: e.target.value }))}
              fullWidth
              required
            />
            <FormControl fullWidth required>
              <InputLabel>{t('pipelineEditor.output.type')}</InputLabel>
              <Select
                value={outputForm.type}
                onChange={(e) => handleTypeChange(e.target.value as OutputType | '')}
                label={t('pipelineEditor.output.type')}
              >
                {(Object.keys(outputTypeConfig) as OutputType[]).map((type) => {
                  const config = outputTypeConfig[type]
                  return (
                    <MenuItem key={type} value={type}>
                      <Stack direction="row" spacing={1} sx={{
                        alignItems: "center"
                      }}>
                        <Chip
                          icon={config.icon as React.ReactElement}
                          label={config.label}
                          size="small"
                          sx={{ bgcolor: config.color, color: 'white' }}
                        />
                      </Stack>
                    </MenuItem>
                  );
                })}
              </Select>
            </FormControl>

            {/* 타입별 설정 */}
            {outputForm.type && (
              <Box sx={{ mt: 2 }}>
                <Typography variant="subtitle2" gutterBottom>
                  {t('pipelineEditor.output.configuration')}
                </Typography>
                {renderConfigFields()}
              </Box>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button>
          <Button
            onClick={handleSaveOutput}
            variant="contained"
            disabled={!outputForm.name || !outputForm.type}
          >
            {t('common.save')}
          </Button>
        </DialogActions>
      </Dialog>
      {/* 삭제 확인 다이얼로그 */}
      <ConfirmDialog
        open={deleteDialogOpen}
        title={t('pipelineEditor.output.deleteConfirmTitle')}
        message={t('pipelineEditor.output.deleteConfirmMessage')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteDialogOpen(false)}
        severity="error"
      />
    </>
  );
}
