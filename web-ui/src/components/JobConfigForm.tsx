import React from 'react'
import {
  Card,
  CardContent,
  FormControlLabel,
  Switch,
  TextField,
  Typography,
  Box,
  Grid,
  Tooltip,
  Divider
} from '@mui/material'
import HelpOutlineIcon from '@mui/icons-material/HelpOutlineOutlined'
import { useTranslation } from 'react-i18next'
import type { JobConfig } from '../services/api'

interface JobConfigFormProps {
  value?: JobConfig | null
  onChange?: (value: JobConfig | null) => void
  disabled?: boolean
}

// JobConfig 폼 컴포넌트
// Batch 워크플로우에서 Kubernetes Job 설정을 위한 폼
export function JobConfigForm({ value, onChange, disabled }: JobConfigFormProps) {
  const { t } = useTranslation()

  const isEnabled = value !== null && value !== undefined

  const handleEnableChange = (_event: React.ChangeEvent<HTMLInputElement>, enabled: boolean) => {
    if (enabled) {
      // 기본값으로 JobConfig 생성
      onChange?.({
        cpu: '500m',
        memory: '512Mi',
        cpu_limit: '1000m',
        memory_limit: '1Gi',
        timeout_seconds: 3600,
        backoff_limit: 3,
        ttl_after_finished: 300,
      })
    } else {
      onChange?.(null)
    }
  }

  const handleFieldChange = (field: keyof JobConfig, fieldValue: string | number | undefined) => {
    if (!value) return
    onChange?.({
      ...value,
      [field]: fieldValue,
    })
  }

  const LabelWithHelp = ({ label, help }: { label: string; help: string }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
      <Typography variant="body2">{label}</Typography>
      <Tooltip title={help} arrow>
        <HelpOutlineIcon sx={{ fontSize: 14, color: 'text.secondary' }} />
      </Tooltip>
    </Box>
  )

  return (
    <Card variant="outlined" sx={{ mt: 2 }}>
      <CardContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <FormControlLabel
            control={
              <Switch
                checked={isEnabled}
                onChange={handleEnableChange}
                disabled={disabled}
              />
            }
            label={
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Typography sx={{
                  fontWeight: "medium"
                }}>{t('workflow.jobConfigEnabled')}</Typography>
                <Tooltip title={t('workflow.jobConfigEnabledDesc')} arrow>
                  <HelpOutlineIcon sx={{ fontSize: 16, color: 'text.secondary' }} />
                </Tooltip>
              </Box>
            }
          />

          {isEnabled && (
            <>
              <Divider />

              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {t('workflow.jobResourceSettings')}
              </Typography>

              <Grid container spacing={2}>
                <Grid size={6}>
                  <TextField
                    fullWidth
                    size="small"
                    label={<LabelWithHelp label={t('workflow.jobCpu')} help={t('workflow.jobCpuHelp')} />}
                    value={value?.cpu || ''}
                    onChange={(e) => handleFieldChange('cpu', e.target.value)}
                    placeholder={t('workflow.jobCpuPlaceholder')}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={6}>
                  <TextField
                    fullWidth
                    size="small"
                    label={<LabelWithHelp label={t('workflow.jobMemory')} help={t('workflow.jobMemoryHelp')} />}
                    value={value?.memory || ''}
                    onChange={(e) => handleFieldChange('memory', e.target.value)}
                    placeholder={t('workflow.jobMemoryPlaceholder')}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={6}>
                  <TextField
                    fullWidth
                    size="small"
                    label={<LabelWithHelp label={t('workflow.jobCpuLimit')} help={t('workflow.jobCpuLimitHelp')} />}
                    value={value?.cpu_limit || ''}
                    onChange={(e) => handleFieldChange('cpu_limit', e.target.value)}
                    placeholder="1000m"
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={6}>
                  <TextField
                    fullWidth
                    size="small"
                    label={<LabelWithHelp label={t('workflow.jobMemoryLimit')} help={t('workflow.jobMemoryLimitHelp')} />}
                    value={value?.memory_limit || ''}
                    onChange={(e) => handleFieldChange('memory_limit', e.target.value)}
                    placeholder="1Gi"
                    disabled={disabled}
                  />
                </Grid>
              </Grid>

              <Divider />

              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {t('workflow.jobBehaviorSettings')}
              </Typography>

              <Grid container spacing={2}>
                <Grid size={4}>
                  <TextField
                    fullWidth
                    size="small"
                    type="number"
                    label={<LabelWithHelp label={t('workflow.jobTimeout')} help={t('workflow.jobTimeoutHelp')} />}
                    value={value?.timeout_seconds || ''}
                    onChange={(e) => handleFieldChange('timeout_seconds', e.target.value ? parseInt(e.target.value) : undefined)}
                    slotProps={{ htmlInput: { min: 60, max: 86400 } }}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={4}>
                  <TextField
                    fullWidth
                    size="small"
                    type="number"
                    label={<LabelWithHelp label={t('workflow.jobBackoffLimit')} help={t('workflow.jobBackoffLimitHelp')} />}
                    value={value?.backoff_limit ?? ''}
                    onChange={(e) => handleFieldChange('backoff_limit', e.target.value ? parseInt(e.target.value) : undefined)}
                    slotProps={{ htmlInput: { min: 0, max: 10 } }}
                    disabled={disabled}
                  />
                </Grid>
                <Grid size={4}>
                  <TextField
                    fullWidth
                    size="small"
                    type="number"
                    label={<LabelWithHelp label={t('workflow.jobTtlAfterFinished')} help={t('workflow.jobTtlAfterFinishedHelp')} />}
                    value={value?.ttl_after_finished ?? ''}
                    onChange={(e) => handleFieldChange('ttl_after_finished', e.target.value ? parseInt(e.target.value) : undefined)}
                    slotProps={{ htmlInput: { min: 0, max: 3600 } }}
                    disabled={disabled}
                  />
                </Grid>
              </Grid>
            </>
          )}
        </Box>
      </CardContent>
    </Card>
  );
}

export default JobConfigForm
