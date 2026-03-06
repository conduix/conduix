import {
  Box,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
  Chip,
} from '@mui/material'
import {
  PlayArrow as BatchIcon,
  Stream as StreamIcon,
  Sync as HybridIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'

interface PipelineModeSelectorProps {
  value: 'batch' | 'streaming' | 'hybrid'
  onChange: (mode: 'batch' | 'streaming' | 'hybrid') => void
  disabled?: boolean
}

// Pipeline 실행 모드 선택 컴포넌트 (배치/스트리밍/하이브리드)
export default function PipelineModeSelector({ value, onChange, disabled }: PipelineModeSelectorProps) {
  const { t } = useTranslation()

  const modes = [
    {
      value: 'batch' as const,
      icon: <BatchIcon />,
      label: t('executionMode.batch'),
      description: t('executionMode.batchDesc'),
      k8sResource: 'Job / CronJob',
    },
    {
      value: 'streaming' as const,
      icon: <StreamIcon />,
      label: t('executionMode.streaming'),
      description: t('executionMode.streamingDesc'),
      k8sResource: 'Deployment',
    },
    {
      value: 'hybrid' as const,
      icon: <HybridIcon />,
      label: t('executionMode.hybrid'),
      description: t('executionMode.hybridDesc'),
      k8sResource: 'CronJob + Deployment',
    },
  ]

  return (
    <Box>
      <Typography variant="subtitle2" sx={{ mb: 1 }}>
        {t('executionMode.title')}
      </Typography>
      <ToggleButtonGroup
        value={value}
        exclusive
        onChange={(_, newValue) => {
          if (newValue) onChange(newValue)
        }}
        disabled={disabled}
        fullWidth
        sx={{ mb: 1 }}
      >
        {modes.map((mode) => (
          <ToggleButton key={mode.value} value={mode.value} sx={{ py: 1.5, flexDirection: 'column', gap: 0.5 }}>
            {mode.icon}
            <Typography variant="body2" fontWeight={value === mode.value ? 'bold' : 'normal'}>
              {mode.label}
            </Typography>
          </ToggleButton>
        ))}
      </ToggleButtonGroup>

      {modes
        .filter((m) => m.value === value)
        .map((m) => (
          <Box key={m.value} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="caption" color="text.secondary">
              {m.description}
            </Typography>
            <Chip label={m.k8sResource} size="small" variant="outlined" sx={{ fontFamily: 'monospace', fontSize: 11 }} />
          </Box>
        ))}
    </Box>
  )
}
