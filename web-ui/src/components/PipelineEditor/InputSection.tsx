/**
 * Input Section 컴포넌트
 *
 * 파이프라인의 Input(데이터 소스) 설정을 편집합니다.
 * - Input 타입 선택 (kafka, rest_api, sql, cdc, file 등)
 * - 타입별 설정 폼
 * - Rate Limit 설정
 * - Batch 설정 (batch workflow만)
 */

import { useState } from 'react'
import {
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Typography,
  Box,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Switch,
  FormControlLabel,
  Stack,
  Chip,
  Divider,
  FormHelperText,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ApiIcon from '@mui/icons-material/Api'
import StorageIcon from '@mui/icons-material/Storage'
import CloudIcon from '@mui/icons-material/Cloud'
import InsertDriveFileIcon from '@mui/icons-material/InsertDriveFile'
import BoltIcon from '@mui/icons-material/Bolt'
import SyncIcon from '@mui/icons-material/Sync'
import { useTranslation } from 'react-i18next'
import { usePipelineEditor } from './PipelineEditorContext'
import type { RateLimitConfig } from '../../types/pipeline'

// Input 타입 정의
type InputType = 'rest_api' | 'kafka' | 'cdc' | 'sql' | 'file' | 'sql_event' | 'kubernetes'

// Input 타입별 설정
const inputTypeConfig: Record<InputType, { color: string; icon: React.ReactNode; label: string }> = {
  rest_api: { color: '#1976d2', icon: <ApiIcon fontSize="small" />, label: 'REST API' },
  kafka: { color: '#4caf50', icon: <BoltIcon fontSize="small" />, label: 'Kafka' },
  cdc: { color: '#9c27b0', icon: <SyncIcon fontSize="small" />, label: 'CDC' },
  sql: { color: '#ff9800', icon: <StorageIcon fontSize="small" />, label: 'SQL' },
  file: { color: '#00bcd4', icon: <InsertDriveFileIcon fontSize="small" />, label: 'File' },
  sql_event: { color: '#e91e63', icon: <CloudIcon fontSize="small" />, label: 'SQL Event' },
  kubernetes: { color: '#3f51b5', icon: <CloudIcon fontSize="small" />, label: 'Kubernetes Logs' },
}

// 워크플로우 타입별 사용 가능한 Input 타입
const inputTypesByWorkflow: Record<'batch' | 'realtime', InputType[]> = {
  batch: ['rest_api', 'sql', 'file'],
  realtime: ['rest_api', 'kafka', 'cdc', 'sql_event', 'kubernetes'],
}

export function InputSection() {
  const { t } = useTranslation()
  const { pipeline, updateInput, updateBatch, workflowType } = usePipelineEditor()
  const [expanded, setExpanded] = useState(true)

  const input = pipeline.input || pipeline.source || { type: '', name: '', config: {} }
  const availableTypes = inputTypesByWorkflow[workflowType]

  // Input 타입 변경 핸들러
  const handleTypeChange = (newType: string) => {
    const defaultConfigs: Record<string, Record<string, unknown>> = {
      rest_api: { url: '', method: 'GET', headers: {} },
      kafka: { brokers: '', topic: '', group_id: '', offset: 'latest' },
      cdc: { connection_string: '', database: '', table: '', slot_name: '' },
      sql: { connection_string: '', query: '', fetch_size: 1000 },
      file: { path: '', format: 'json', delimiter: '' },
      sql_event: { connection_string: '', query: '', poll_interval: '1m', watermark_column: '' },
      kubernetes: { namespace: '', pod_selector: '', container_name: '', follow: true, tail_lines: 100 },
    }

    updateInput({
      type: newType,
      name: input.name || `${newType}_source`,
      config: defaultConfigs[newType] || {},
    })
  }

  // Config 필드 업데이트 핸들러
  const handleConfigChange = (field: string, value: unknown) => {
    updateInput({
      config: { ...input.config, [field]: value },
    })
  }

  // Rate Limit 업데이트 핸들러
  const handleRateLimitChange = (field: keyof RateLimitConfig, value: unknown) => {
    const currentRateLimit = input.rate_limit || {
      enabled: false,
      rate: 100,
      interval: 'second' as const,
    }
    updateInput({
      rate_limit: { ...currentRateLimit, [field]: value },
    })
  }

  // Input 타입별 설정 필드 렌더링
  const renderConfigFields = () => {
    const config = input.config || {}

    switch (input.type) {
      case 'rest_api':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.url')}
              value={config.url || ''}
              onChange={(e) => handleConfigChange('url', e.target.value)}
              placeholder="https://api.example.com/data"
              fullWidth
              required
            />
            <FormControl sx={{ width: 120 }}>
              <InputLabel>{t('pipelineEditor.input.method')}</InputLabel>
              <Select
                value={config.method || 'GET'}
                onChange={(e) => handleConfigChange('method', e.target.value)}
                label={t('pipelineEditor.input.method')}
              >
                <MenuItem value="GET">GET</MenuItem>
                <MenuItem value="POST">POST</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('pipelineEditor.input.headers')}
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

      case 'kafka':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.brokers')}
              value={config.brokers || ''}
              onChange={(e) => handleConfigChange('brokers', e.target.value)}
              placeholder="localhost:9092,localhost:9093"
              helperText={t('pipelineEditor.input.brokersHelp')}
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.topic')}
              value={config.topic || ''}
              onChange={(e) => handleConfigChange('topic', e.target.value)}
              placeholder="my-topic"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.groupId')}
              value={config.group_id || ''}
              onChange={(e) => handleConfigChange('group_id', e.target.value)}
              placeholder="consumer-group-1"
              fullWidth
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.input.offset')}</InputLabel>
              <Select
                value={config.offset || 'latest'}
                onChange={(e) => handleConfigChange('offset', e.target.value)}
                label={t('pipelineEditor.input.offset')}
              >
                <MenuItem value="latest">Latest</MenuItem>
                <MenuItem value="earliest">Earliest</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      case 'sql':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.connectionString')}
              type="password"
              value={config.connection_string || ''}
              onChange={(e) => handleConfigChange('connection_string', e.target.value)}
              placeholder="postgres://user:pass@host:5432/db"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.query')}
              value={config.query || ''}
              onChange={(e) => handleConfigChange('query', e.target.value)}
              placeholder="SELECT * FROM users WHERE updated_at > :last_sync"
              multiline
              rows={3}
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.fetchSize')}
              type="number"
              value={config.fetch_size || 1000}
              onChange={(e) => handleConfigChange('fetch_size', Number(e.target.value))}
              inputProps={{ min: 100, max: 100000 }}
              sx={{ width: 150 }}
            />
          </Stack>
        )

      case 'cdc':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.connectionString')}
              type="password"
              value={config.connection_string || ''}
              onChange={(e) => handleConfigChange('connection_string', e.target.value)}
              placeholder="postgres://user:pass@host:5432/db"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.database')}
              value={config.database || ''}
              onChange={(e) => handleConfigChange('database', e.target.value)}
              placeholder="mydb"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.table')}
              value={config.table || ''}
              onChange={(e) => handleConfigChange('table', e.target.value)}
              placeholder="users"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.slotName')}
              value={config.slot_name || ''}
              onChange={(e) => handleConfigChange('slot_name', e.target.value)}
              placeholder="cdc_slot"
              fullWidth
            />
          </Stack>
        )

      case 'file':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.path')}
              value={config.path || ''}
              onChange={(e) => handleConfigChange('path', e.target.value)}
              placeholder="/data/input/*.json"
              fullWidth
              required
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('pipelineEditor.input.format')}</InputLabel>
              <Select
                value={config.format || 'json'}
                onChange={(e) => handleConfigChange('format', e.target.value)}
                label={t('pipelineEditor.input.format')}
              >
                <MenuItem value="json">JSON</MenuItem>
                <MenuItem value="csv">CSV</MenuItem>
                <MenuItem value="parquet">Parquet</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )

      case 'sql_event':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.connectionString')}
              type="password"
              value={config.connection_string || ''}
              onChange={(e) => handleConfigChange('connection_string', e.target.value)}
              placeholder="postgres://user:pass@host:5432/db"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.query')}
              value={config.query || ''}
              onChange={(e) => handleConfigChange('query', e.target.value)}
              placeholder="SELECT * FROM events WHERE created_at > :watermark"
              multiline
              rows={3}
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.pollInterval')}
              value={config.poll_interval || '1m'}
              onChange={(e) => handleConfigChange('poll_interval', e.target.value)}
              placeholder="1m"
              sx={{ width: 120 }}
            />
            <TextField
              label={t('pipelineEditor.input.watermarkColumn')}
              value={config.watermark_column || ''}
              onChange={(e) => handleConfigChange('watermark_column', e.target.value)}
              placeholder="created_at"
              fullWidth
            />
          </Stack>
        )

      case 'kubernetes':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('pipelineEditor.input.namespace')}
              value={config.namespace || ''}
              onChange={(e) => handleConfigChange('namespace', e.target.value)}
              placeholder="default"
              fullWidth
            />
            <TextField
              label={t('pipelineEditor.input.podSelector')}
              value={config.pod_selector || ''}
              onChange={(e) => handleConfigChange('pod_selector', e.target.value)}
              placeholder="app=nginx,env=production"
              fullWidth
              required
            />
            <TextField
              label={t('pipelineEditor.input.containerName')}
              value={config.container_name || ''}
              onChange={(e) => handleConfigChange('container_name', e.target.value)}
              placeholder="nginx"
              fullWidth
            />
            <FormControlLabel
              control={
                <Switch
                  checked={config.follow !== false}
                  onChange={(e) => handleConfigChange('follow', e.target.checked)}
                />
              }
              label={t('pipelineEditor.input.follow')}
            />
            <TextField
              label={t('pipelineEditor.input.tailLines')}
              type="number"
              value={config.tail_lines || 100}
              onChange={(e) => handleConfigChange('tail_lines', Number(e.target.value))}
              inputProps={{ min: 1, max: 10000 }}
              sx={{ width: 120 }}
            />
          </Stack>
        )

      default:
        return (
          <Typography color="text.secondary">
            {t('pipelineEditor.input.selectType')}
          </Typography>
        )
    }
  }

  // Rate Limit 설정 렌더링
  const renderRateLimitSection = () => {
    const rateLimit = input.rate_limit || { enabled: false, rate: 100, interval: 'second' as const }

    return (
      <>
        <Divider sx={{ my: 2 }}>{t('pipelineEditor.input.rateLimit')}</Divider>
        <FormControlLabel
          control={
            <Switch
              checked={rateLimit.enabled || false}
              onChange={(e) => handleRateLimitChange('enabled', e.target.checked)}
            />
          }
          label={t('pipelineEditor.input.rateLimitEnabled')}
        />
        {rateLimit.enabled && (
          <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
            <TextField
              label={t('pipelineEditor.input.rate')}
              type="number"
              value={rateLimit.rate || 100}
              onChange={(e) => handleRateLimitChange('rate', Number(e.target.value))}
              inputProps={{ min: 1 }}
              sx={{ width: 120 }}
            />
            <FormControl sx={{ width: 120 }}>
              <InputLabel>{t('pipelineEditor.input.interval')}</InputLabel>
              <Select
                value={rateLimit.interval || 'second'}
                onChange={(e) => handleRateLimitChange('interval', e.target.value)}
                label={t('pipelineEditor.input.interval')}
              >
                <MenuItem value="second">{t('pipelineEditor.input.perSecond')}</MenuItem>
                <MenuItem value="minute">{t('pipelineEditor.input.perMinute')}</MenuItem>
                <MenuItem value="hour">{t('pipelineEditor.input.perHour')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('pipelineEditor.input.burst')}
              type="number"
              value={rateLimit.burst || ''}
              onChange={(e) =>
                handleRateLimitChange('burst', e.target.value ? Number(e.target.value) : undefined)
              }
              inputProps={{ min: 1 }}
              sx={{ width: 100 }}
            />
          </Stack>
        )}
      </>
    )
  }

  // Batch 설정 렌더링 (batch workflow만)
  const renderBatchSection = () => {
    if (workflowType !== 'batch') return null

    const batch = pipeline.batch || { enabled: false, output_mode: 'bulk', size: 100, workers: 20 }

    return (
      <>
        <Divider sx={{ my: 2 }}>{t('pipelineEditor.input.batch')}</Divider>
        <FormControlLabel
          control={
            <Switch
              checked={batch.enabled || false}
              onChange={(e) => updateBatch({ enabled: e.target.checked })}
            />
          }
          label={t('pipelineEditor.input.batchEnabled')}
        />
        <FormHelperText sx={{ mt: -1, mb: 1 }}>
          {t('pipelineEditor.input.batchEnabledHelp')}
        </FormHelperText>

        {batch.enabled && (
          <Box sx={{ mt: 2 }}>
            <Stack spacing={2}>
              <FormControl sx={{ width: 200 }}>
                <InputLabel>{t('pipelineEditor.input.outputMode')}</InputLabel>
                <Select
                  value={batch.output_mode || 'bulk'}
                  onChange={(e) => updateBatch({ output_mode: e.target.value as 'bulk' | 'individual' })}
                  label={t('pipelineEditor.input.outputMode')}
                >
                  <MenuItem value="bulk">{t('pipelineEditor.input.bulkMode')}</MenuItem>
                  <MenuItem value="individual">{t('pipelineEditor.input.individualMode')}</MenuItem>
                </Select>
                <FormHelperText>{t('pipelineEditor.input.outputModeHelp')}</FormHelperText>
              </FormControl>

              <Stack direction="row" spacing={2}>
                <TextField
                  label={t('pipelineEditor.input.batchSize')}
                  type="number"
                  value={batch.size || 100}
                  onChange={(e) => updateBatch({ size: Number(e.target.value) })}
                  inputProps={{ min: 1, max: 10000 }}
                  sx={{ width: 120 }}
                />
                <TextField
                  label={t('pipelineEditor.input.workers')}
                  type="number"
                  value={batch.workers || 20}
                  onChange={(e) => updateBatch({ workers: Number(e.target.value) })}
                  inputProps={{ min: 1, max: 100 }}
                  sx={{ width: 120 }}
                />
                <TextField
                  label={t('pipelineEditor.input.flushInterval')}
                  value={batch.flush_interval || '5s'}
                  onChange={(e) => updateBatch({ flush_interval: e.target.value })}
                  placeholder="5s"
                  sx={{ width: 100 }}
                />
              </Stack>
            </Stack>
          </Box>
        )}
      </>
    )
  }

  return (
    <Accordion expanded={expanded} onChange={(_, isExpanded) => setExpanded(isExpanded)}>
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="subtitle1" fontWeight="medium">
            {t('pipelineEditor.input.title')}
          </Typography>
          {input.type && inputTypeConfig[input.type as InputType] && (
            <Chip
              icon={inputTypeConfig[input.type as InputType].icon as React.ReactElement}
              label={inputTypeConfig[input.type as InputType].label}
              size="small"
              sx={{
                bgcolor: inputTypeConfig[input.type as InputType].color,
                color: 'white',
              }}
            />
          )}
        </Box>
      </AccordionSummary>
      <AccordionDetails>
        <Stack spacing={2}>
          {/* Input 타입 선택 */}
          <FormControl fullWidth required>
            <InputLabel>{t('pipelineEditor.input.type')}</InputLabel>
            <Select
              value={input.type || ''}
              onChange={(e) => handleTypeChange(e.target.value)}
              label={t('pipelineEditor.input.type')}
            >
              {availableTypes.map((type) => {
                const config = inputTypeConfig[type]
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

          {/* Input 이름 */}
          <TextField
            label={t('pipelineEditor.input.name')}
            value={input.name || ''}
            onChange={(e) => updateInput({ name: e.target.value })}
            placeholder="api_source"
            fullWidth
            required
          />

          {/* 타입별 설정 필드 */}
          <Divider>{t('pipelineEditor.input.configuration')}</Divider>
          {renderConfigFields()}

          {/* Rate Limit 설정 */}
          {renderRateLimitSection()}

          {/* Batch 설정 */}
          {renderBatchSection()}
        </Stack>
      </AccordionDetails>
    </Accordion>
  )
}
