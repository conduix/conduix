/**
 * 파이프라인 메트릭스 카드 컴포넌트
 * 처리량, 에러율, 지연시간 등 핵심 지표 표시
 */

import { Card, CardContent, Box, Typography, Skeleton, Chip } from '@mui/material'
import {
  TrendingUp as ThroughputIcon,
  Error as ErrorIcon,
  Timer as LatencyIcon,
  Storage as BufferIcon,
} from '@mui/icons-material'

export interface PipelineMetrics {
  throughput_per_sec: number
  input_count: number
  output_count: number
  error_count: number
  error_rate: number
  avg_latency_ms: number
  buffer_size: number
  buffer_capacity: number
}

interface MetricsCardProps {
  title: string
  value: number | string
  unit?: string
  icon: React.ReactNode
  color?: string
  trend?: number
  loading?: boolean
}

export function MetricsCard({
  title,
  value,
  unit,
  icon,
  color = 'text.primary',
  trend,
  loading,
}: MetricsCardProps) {
  return (
    <Card sx={{ height: '100%' }}>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Box sx={{ color }}>{icon}</Box>
          <Typography variant="body2" sx={{
            color: "text.secondary"
          }}>
            {title}
          </Typography>
        </Box>

        {loading ? (
          <Skeleton width={100} height={48} />
        ) : (
          <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5 }}>
            <Typography variant="h4" sx={{ color, fontWeight: 600 }}>
              {typeof value === 'number' ? value.toLocaleString() : value}
            </Typography>
            {unit && (
              <Typography variant="body2" sx={{
                color: "text.secondary"
              }}>
                {unit}
              </Typography>
            )}
          </Box>
        )}

        {trend !== undefined && (
          <Chip
            size="small"
            label={`${trend > 0 ? '+' : ''}${trend.toFixed(1)}%`}
            color={trend > 0 ? 'success' : trend < 0 ? 'error' : 'default'}
            sx={{ mt: 1, height: 20, fontSize: '0.7rem' }}
          />
        )}
      </CardContent>
    </Card>
  );
}

interface MetricsGridProps {
  metrics: PipelineMetrics | null
  loading?: boolean
}

export function MetricsGrid({ metrics, loading }: MetricsGridProps) {
  const errorRate = metrics
    ? metrics.input_count > 0
      ? (metrics.error_count / metrics.input_count) * 100
      : 0
    : 0

  const bufferUsage = metrics
    ? metrics.buffer_capacity > 0
      ? (metrics.buffer_size / metrics.buffer_capacity) * 100
      : 0
    : 0

  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
        gap: 2,
      }}
    >
      <MetricsCard
        title="Throughput"
        value={metrics?.throughput_per_sec ?? 0}
        unit="/sec"
        icon={<ThroughputIcon />}
        color="#4caf50"
        loading={loading}
      />

      <MetricsCard
        title="Error Rate"
        value={errorRate.toFixed(2)}
        unit="%"
        icon={<ErrorIcon />}
        color={errorRate > 5 ? '#f44336' : errorRate > 1 ? '#ff9800' : '#4caf50'}
        loading={loading}
      />

      <MetricsCard
        title="Avg Latency"
        value={metrics?.avg_latency_ms ?? 0}
        unit="ms"
        icon={<LatencyIcon />}
        color={
          (metrics?.avg_latency_ms ?? 0) > 1000
            ? '#f44336'
            : (metrics?.avg_latency_ms ?? 0) > 500
            ? '#ff9800'
            : '#4caf50'
        }
        loading={loading}
      />

      <MetricsCard
        title="Buffer Usage"
        value={bufferUsage.toFixed(0)}
        unit="%"
        icon={<BufferIcon />}
        color={bufferUsage > 80 ? '#f44336' : bufferUsage > 50 ? '#ff9800' : '#4caf50'}
        loading={loading}
      />
    </Box>
  )
}

export default MetricsCard
