/**
 * CircuitBreakerConfig - Circuit Breaker 설정 컴포넌트
 */
import {
  Box,
  Stack,
  TextField,
  Typography,
  Card,
  CardContent,
  Slider,
  Alert,
  Chip,
} from '@mui/material'
import { useTranslation } from 'react-i18next'
import type { CircuitBreakerConfig as CBConfig } from '../../types/contract'

interface CircuitBreakerConfigProps {
  value?: CBConfig
  onChange: (value: CBConfig) => void
}

const defaultConfig: CBConfig = {
  consecutive_failures: 50,
  failure_rate_threshold: 0.8,
  window_size: 100,
  open_timeout: '30s',
  half_open_requests: 5,
}

export function CircuitBreakerConfig({ value, onChange }: CircuitBreakerConfigProps) {
  const { t } = useTranslation()
  const config = { ...defaultConfig, ...value }

  const update = (updates: Partial<CBConfig>) => {
    onChange({ ...config, ...updates })
  }

  return (
    <Box>
      <Alert severity="info" sx={{ mb: 3 }}>
        {t('contract.circuitBreakerInfo', 'Circuit Breaker는 연속된 위반이 발생할 때 검증을 일시 중단하여 파이프라인을 보호합니다. 임계치 도달 시 Circuit이 열리고, 일정 시간 후 Half-Open 상태에서 테스트 후 복구됩니다.')}
      </Alert>
      <Stack spacing={3}>
        {/* 연속 실패 임계치 */}
        <Card variant="outlined">
          <CardContent>
            <Typography variant="subtitle2" gutterBottom>
              {t('contract.consecutiveFailures', '연속 위반 임계치')}
            </Typography>
            <Typography
              variant="body2"
              sx={{
                color: "text.secondary",
                mb: 2
              }}>
              {t('contract.consecutiveFailuresDesc', '연속으로 이 횟수만큼 위반이 발생하면 Circuit이 열립니다.')}
            </Typography>
            <Stack direction="row" spacing={2} sx={{
              alignItems: "center"
            }}>
              <Slider
                value={config.consecutive_failures || 50}
                onChange={(_, v) => update({ consecutive_failures: v as number })}
                min={5}
                max={200}
                step={5}
                valueLabelDisplay="auto"
                sx={{ flex: 1 }}
              />
              <TextField
                type="number"
                value={config.consecutive_failures || 50}
                onChange={(e) => update({ consecutive_failures: parseInt(e.target.value) || 50 })}
                size="small"
                sx={{ width: 100 }}
                slotProps={{ htmlInput: { min: 5, max: 500 } }}
              />
              <Typography variant="body2">{t('contract.records', '건')}</Typography>
            </Stack>
          </CardContent>
        </Card>

        {/* 위반율 임계치 */}
        <Card variant="outlined">
          <CardContent>
            <Typography variant="subtitle2" gutterBottom>
              {t('contract.failureRateThreshold', '위반율 임계치')}
            </Typography>
            <Typography
              variant="body2"
              sx={{
                color: "text.secondary",
                mb: 2
              }}>
              {t('contract.failureRateThresholdDesc', 'Sliding Window 내 위반율이 이 비율을 초과하면 Circuit이 열립니다.')}
            </Typography>
            <Stack direction="row" spacing={2} sx={{
              alignItems: "center"
            }}>
              <Slider
                value={(config.failure_rate_threshold || 0.8) * 100}
                onChange={(_, v) => update({ failure_rate_threshold: (v as number) / 100 })}
                min={10}
                max={100}
                step={5}
                valueLabelDisplay="auto"
                valueLabelFormat={(v) => `${v}%`}
                sx={{ flex: 1 }}
              />
              <TextField
                type="number"
                value={Math.round((config.failure_rate_threshold || 0.8) * 100)}
                onChange={(e) => update({ failure_rate_threshold: (parseInt(e.target.value) || 80) / 100 })}
                size="small"
                sx={{ width: 100 }}
                slotProps={{ htmlInput: { min: 10, max: 100 } }}
              />
              <Typography variant="body2">%</Typography>
            </Stack>
          </CardContent>
        </Card>

        {/* Sliding Window 크기 */}
        <Card variant="outlined">
          <CardContent>
            <Typography variant="subtitle2" gutterBottom>
              {t('contract.windowSize', 'Sliding Window 크기')}
            </Typography>
            <Typography
              variant="body2"
              sx={{
                color: "text.secondary",
                mb: 2
              }}>
              {t('contract.windowSizeDesc', '위반율 계산에 사용할 최근 레코드 수입니다.')}
            </Typography>
            <Stack direction="row" spacing={2} sx={{
              alignItems: "center"
            }}>
              <Slider
                value={config.window_size || 100}
                onChange={(_, v) => update({ window_size: v as number })}
                min={10}
                max={1000}
                step={10}
                valueLabelDisplay="auto"
                sx={{ flex: 1 }}
              />
              <TextField
                type="number"
                value={config.window_size || 100}
                onChange={(e) => update({ window_size: parseInt(e.target.value) || 100 })}
                size="small"
                sx={{ width: 100 }}
                slotProps={{ htmlInput: { min: 10, max: 5000 } }}
              />
              <Typography variant="body2">{t('contract.records', '건')}</Typography>
            </Stack>
          </CardContent>
        </Card>

        {/* 복구 설정 */}
        <Card variant="outlined">
          <CardContent>
            <Typography variant="subtitle2" gutterBottom>
              {t('contract.recoverySettings', '복구 설정')}
            </Typography>
            <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
              <TextField
                label={t('contract.openTimeout', 'Open 타임아웃')}
                value={config.open_timeout || '30s'}
                onChange={(e) => update({ open_timeout: e.target.value })}
                size="small"
                sx={{ flex: 1 }}
                helperText={t('contract.openTimeoutHelp', 'Circuit Open 후 Half-Open으로 전환까지 대기 시간 (예: 30s, 1m)')}
              />
              <TextField
                label={t('contract.halfOpenRequests', 'Half-Open 테스트 수')}
                type="number"
                value={config.half_open_requests || 5}
                onChange={(e) => update({ half_open_requests: parseInt(e.target.value) || 5 })}
                size="small"
                sx={{ flex: 1 }}
                helperText={t('contract.halfOpenRequestsHelp', 'Half-Open 상태에서 테스트할 요청 수')}
                slotProps={{ htmlInput: { min: 1, max: 50 } }}
              />
            </Stack>
          </CardContent>
        </Card>

        {/* 상태 다이어그램 */}
        <Card variant="outlined">
          <CardContent>
            <Typography variant="subtitle2" gutterBottom>
              {t('contract.stateDiagram', '상태 전환 다이어그램')}
            </Typography>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mt: 2, flexWrap: 'wrap' }}>
              <Chip label="Closed" color="success" />
              <Typography variant="body2">→</Typography>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>
                ({config.consecutive_failures}{t('contract.consecutiveFailuresShort', '건 연속 위반')} or {Math.round((config.failure_rate_threshold || 0.8) * 100)}% {t('contract.violationRate', '위반율')})
              </Typography>
              <Typography variant="body2">→</Typography>
              <Chip label="Open" color="error" />
              <Typography variant="body2">→</Typography>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>
                ({config.open_timeout} {t('contract.after', '후')})
              </Typography>
              <Typography variant="body2">→</Typography>
              <Chip label="Half-Open" color="warning" />
              <Typography variant="body2">→</Typography>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>
                ({config.half_open_requests}{t('contract.successfulTests', '건 성공 시')})
              </Typography>
              <Typography variant="body2">→</Typography>
              <Chip label="Closed" color="success" />
            </Box>
          </CardContent>
        </Card>
      </Stack>
    </Box>
  );
}

export default CircuitBreakerConfig
