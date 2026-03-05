/**
 * DLQConfigEditor - Dead Letter Queue 설정 컴포넌트
 */
import {
  Box,
  Stack,
  TextField,
  Typography,
  Card,
  CardContent,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
  Alert,
  Collapse,
  Divider,
} from '@mui/material'
import { useTranslation } from 'react-i18next'
import type { DLQConfig } from '../../types/contract'

interface DLQConfigEditorProps {
  value?: DLQConfig
  onChange: (value: DLQConfig) => void
}

const defaultConfig: DLQConfig = {
  enabled: true,
  type: 'file',
}

export function DLQConfigEditor({ value, onChange }: DLQConfigEditorProps) {
  const { t } = useTranslation()
  const config = { ...defaultConfig, ...value }

  const update = (updates: Partial<DLQConfig>) => {
    onChange({ ...config, ...updates })
  }

  return (
    <Box>
      <Alert severity="info" sx={{ mb: 3 }}>
        {t('contract.dlqInfo', 'DLQ(Dead Letter Queue)는 계약 위반 레코드를 별도로 저장하여 분석하고 재처리할 수 있도록 합니다.')}
      </Alert>

      {/* DLQ 활성화 */}
      <FormControlLabel
        control={
          <Switch
            checked={config.enabled}
            onChange={(e) => update({ enabled: e.target.checked })}
          />
        }
        label={t('contract.dlqEnabled', 'DLQ 활성화')}
        sx={{ mb: 2 }}
      />

      <Collapse in={config.enabled}>
        <Stack spacing={3}>
          {/* DLQ 타입 선택 */}
          <FormControl fullWidth>
            <InputLabel>{t('contract.dlqType', 'DLQ 타입')}</InputLabel>
            <Select
              value={config.type}
              label={t('contract.dlqType', 'DLQ 타입')}
              onChange={(e) => update({ type: e.target.value as DLQConfig['type'] })}
            >
              <MenuItem value="file">{t('contract.dlqTypeFile', '파일 (File)')}</MenuItem>
              <MenuItem value="kafka">{t('contract.dlqTypeKafka', 'Kafka')}</MenuItem>
              <MenuItem value="http">{t('contract.dlqTypeHttp', 'HTTP Webhook')}</MenuItem>
            </Select>
          </FormControl>

          {/* File DLQ 설정 */}
          {config.type === 'file' && (
            <Card variant="outlined">
              <CardContent>
                <Typography variant="subtitle2" gutterBottom>
                  {t('contract.fileDlqSettings', '파일 DLQ 설정')}
                </Typography>
                <Stack spacing={2} sx={{ mt: 2 }}>
                  <TextField
                    label={t('contract.dlqPath', '저장 경로')}
                    value={config.path || ''}
                    onChange={(e) => update({ path: e.target.value })}
                    size="small"
                    fullWidth
                    placeholder="/var/log/conduix/dlq"
                    helperText={t('contract.dlqPathHelp', '위반 레코드가 저장될 디렉토리 경로')}
                  />
                  <FormControl size="small">
                    <InputLabel>{t('contract.dlqFormat', '포맷')}</InputLabel>
                    <Select
                      value={config.format || 'jsonl'}
                      label={t('contract.dlqFormat', '포맷')}
                      onChange={(e) => update({ format: e.target.value as 'json' | 'jsonl' })}
                    >
                      <MenuItem value="jsonl">JSONL (한 줄당 하나의 JSON)</MenuItem>
                      <MenuItem value="json">JSON Array</MenuItem>
                    </Select>
                  </FormControl>
                  <Divider />
                  <Typography variant="subtitle2">
                    {t('contract.retentionSettings', '보관 설정')}
                  </Typography>
                  <Stack direction="row" spacing={2}>
                    <TextField
                      label={t('contract.maxSizeMB', '최대 파일 크기 (MB)')}
                      type="number"
                      value={config.max_size_mb || ''}
                      onChange={(e) => update({ max_size_mb: e.target.value ? parseInt(e.target.value) : undefined })}
                      size="small"
                      sx={{ flex: 1 }}
                      placeholder="100"
                      helperText={t('contract.maxSizeMBHelp', '초과 시 새 파일로 rotation')}
                    />
                    <TextField
                      label={t('contract.maxAgeDays', '최대 보관 기간 (일)')}
                      type="number"
                      value={config.max_age_days || ''}
                      onChange={(e) => update({ max_age_days: e.target.value ? parseInt(e.target.value) : undefined })}
                      size="small"
                      sx={{ flex: 1 }}
                      placeholder="30"
                      helperText={t('contract.maxAgeDaysHelp', '오래된 파일 자동 삭제')}
                    />
                    <TextField
                      label={t('contract.maxBackups', '최대 백업 수')}
                      type="number"
                      value={config.max_backups || ''}
                      onChange={(e) => update({ max_backups: e.target.value ? parseInt(e.target.value) : undefined })}
                      size="small"
                      sx={{ flex: 1 }}
                      placeholder="10"
                      helperText={t('contract.maxBackupsHelp', '유지할 백업 파일 개수')}
                    />
                  </Stack>
                </Stack>
              </CardContent>
            </Card>
          )}

          {/* Kafka DLQ 설정 */}
          {config.type === 'kafka' && (
            <Card variant="outlined">
              <CardContent>
                <Typography variant="subtitle2" gutterBottom>
                  {t('contract.kafkaDlqSettings', 'Kafka DLQ 설정')}
                </Typography>
                <Stack spacing={2} sx={{ mt: 2 }}>
                  <TextField
                    label={t('contract.kafkaBrokers', 'Kafka Brokers')}
                    value={config.brokers?.join(', ') || ''}
                    onChange={(e) => update({ brokers: e.target.value.split(',').map(b => b.trim()).filter(Boolean) })}
                    size="small"
                    fullWidth
                    placeholder="localhost:9092, localhost:9093"
                    helperText={t('contract.kafkaBrokersHelp', '쉼표로 구분된 Kafka broker 주소')}
                  />
                  <TextField
                    label={t('contract.kafkaTopic', 'Topic 이름')}
                    value={config.topic || ''}
                    onChange={(e) => update({ topic: e.target.value })}
                    size="small"
                    fullWidth
                    placeholder="conduix-dlq"
                  />
                  <TextField
                    label={t('contract.kafkaRetentionMs', 'Retention (ms)')}
                    type="number"
                    value={config.retention_ms || ''}
                    onChange={(e) => update({ retention_ms: e.target.value ? parseInt(e.target.value) : undefined })}
                    size="small"
                    fullWidth
                    placeholder="604800000"
                    helperText={t('contract.kafkaRetentionMsHelp', '메시지 보관 기간 (밀리초, 기본: 7일 = 604800000)')}
                  />
                </Stack>
              </CardContent>
            </Card>
          )}

          {/* HTTP DLQ 설정 */}
          {config.type === 'http' && (
            <Card variant="outlined">
              <CardContent>
                <Typography variant="subtitle2" gutterBottom>
                  {t('contract.httpDlqSettings', 'HTTP Webhook DLQ 설정')}
                </Typography>
                <Stack spacing={2} sx={{ mt: 2 }}>
                  <TextField
                    label={t('contract.httpUrl', 'Webhook URL')}
                    value={config.url || ''}
                    onChange={(e) => update({ url: e.target.value })}
                    size="small"
                    fullWidth
                    placeholder="https://your-service.com/dlq/webhook"
                    helperText={t('contract.httpUrlHelp', '위반 레코드가 POST될 엔드포인트')}
                  />
                  <TextField
                    label={t('contract.httpHeaders', 'Headers (JSON)')}
                    value={config.headers ? JSON.stringify(config.headers, null, 2) : ''}
                    onChange={(e) => {
                      try {
                        const headers = e.target.value ? JSON.parse(e.target.value) : undefined
                        update({ headers })
                      } catch {
                        // 유효하지 않은 JSON은 무시
                      }
                    }}
                    size="small"
                    fullWidth
                    multiline
                    rows={3}
                    placeholder='{"Authorization": "Bearer xxx", "Content-Type": "application/json"}'
                    helperText={t('contract.httpHeadersHelp', 'HTTP 요청에 포함할 헤더 (JSON 형식)')}
                  />
                </Stack>
              </CardContent>
            </Card>
          )}
        </Stack>
      </Collapse>
    </Box>
  )
}

export default DLQConfigEditor
