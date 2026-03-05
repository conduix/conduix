/**
 * Stage별 메트릭스 테이블 컴포넌트
 * 각 Stage의 처리량, 에러, 상태를 표시
 */

import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Chip,
  Typography,
  Box,
  LinearProgress,
  Skeleton,
} from '@mui/material'
import type { ActorMetrics, ActorState } from '../../types/graph'

interface StageMetricsTableProps {
  metrics: Record<string, ActorMetrics> | null
  loading?: boolean
}

const stateColors: Record<ActorState, 'success' | 'warning' | 'error' | 'default' | 'info'> = {
  created: 'default',
  starting: 'info',
  running: 'success',
  stopping: 'warning',
  stopped: 'default',
  restarting: 'warning',
  failed: 'error',
}

function formatNumber(value: number): string {
  if (value >= 1000000) {
    return `${(value / 1000000).toFixed(1)}M`
  }
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}K`
  }
  return value.toString()
}

function formatTime(isoString: string): string {
  try {
    const date = new Date(isoString)
    return date.toLocaleTimeString()
  } catch {
    return '-'
  }
}

export function StageMetricsTable({ metrics, loading }: StageMetricsTableProps) {
  if (loading) {
    return (
      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Stage</TableCell>
              <TableCell align="right">Input</TableCell>
              <TableCell align="right">Output</TableCell>
              <TableCell align="right">Errors</TableCell>
              <TableCell align="right">Throughput</TableCell>
              <TableCell>State</TableCell>
              <TableCell>Last Update</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {[1, 2, 3].map((i) => (
              <TableRow key={i}>
                <TableCell>
                  <Skeleton width={100} />
                </TableCell>
                <TableCell>
                  <Skeleton width={60} />
                </TableCell>
                <TableCell>
                  <Skeleton width={60} />
                </TableCell>
                <TableCell>
                  <Skeleton width={40} />
                </TableCell>
                <TableCell>
                  <Skeleton width={80} />
                </TableCell>
                <TableCell>
                  <Skeleton width={60} />
                </TableCell>
                <TableCell>
                  <Skeleton width={80} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    )
  }

  if (!metrics || Object.keys(metrics).length === 0) {
    return (
      <Paper sx={{ p: 4, textAlign: 'center' }}>
        <Typography color="text.secondary">No stage metrics available</Typography>
      </Paper>
    )
  }

  const entries = Object.entries(metrics).sort(([a], [b]) => a.localeCompare(b))

  return (
    <TableContainer component={Paper}>
      <Table size="small">
        <TableHead>
          <TableRow>
            <TableCell>Stage</TableCell>
            <TableCell align="right">Input</TableCell>
            <TableCell align="right">Output</TableCell>
            <TableCell align="right">Errors</TableCell>
            <TableCell align="right">Throughput</TableCell>
            <TableCell>State</TableCell>
            <TableCell>Last Update</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {entries.map(([stageName, stageMetrics]) => {
            const errorRate =
              stageMetrics.input_count > 0
                ? (stageMetrics.error_count / stageMetrics.input_count) * 100
                : 0

            return (
              <TableRow key={stageName} hover>
                <TableCell>
                  <Typography variant="body2" fontWeight={500}>
                    {stageName}
                  </Typography>
                </TableCell>
                <TableCell align="right">
                  <Typography variant="body2">
                    {formatNumber(stageMetrics.input_count)}
                  </Typography>
                </TableCell>
                <TableCell align="right">
                  <Typography variant="body2">
                    {formatNumber(stageMetrics.output_count)}
                  </Typography>
                </TableCell>
                <TableCell align="right">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, justifyContent: 'flex-end' }}>
                    <Typography
                      variant="body2"
                      color={stageMetrics.error_count > 0 ? 'error' : 'text.primary'}
                    >
                      {formatNumber(stageMetrics.error_count)}
                    </Typography>
                    {stageMetrics.error_count > 0 && (
                      <Typography variant="caption" color="error">
                        ({errorRate.toFixed(1)}%)
                      </Typography>
                    )}
                  </Box>
                </TableCell>
                <TableCell align="right">
                  <Box sx={{ minWidth: 100 }}>
                    <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.5 }}>
                      <Typography variant="caption" color="text.secondary">
                        {stageMetrics.throughput_per_sec.toFixed(1)}/s
                      </Typography>
                    </Box>
                    <LinearProgress
                      variant="determinate"
                      value={Math.min(100, stageMetrics.throughput_per_sec / 10)}
                      color={stageMetrics.throughput_per_sec > 100 ? 'success' : 'primary'}
                      sx={{ height: 4, borderRadius: 2 }}
                    />
                  </Box>
                </TableCell>
                <TableCell>
                  <Chip
                    label={stageMetrics.state}
                    size="small"
                    color={stateColors[stageMetrics.state] || 'default'}
                    sx={{ fontSize: '0.7rem' }}
                  />
                </TableCell>
                <TableCell>
                  <Typography variant="caption" color="text.secondary">
                    {formatTime(stageMetrics.last_updated)}
                  </Typography>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </TableContainer>
  )
}

export default StageMetricsTable
