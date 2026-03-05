/**
 * 파이프라인 모니터링 메인 컴포넌트
 * 메트릭스 카드, Stage 테이블, 처리량 차트를 통합
 */

import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Box,
  Typography,
  IconButton,
  Tooltip,
  Paper,
  Chip,
  ToggleButton,
  ToggleButtonGroup,
  Divider,
} from '@mui/material'
import {
  Refresh as RefreshIcon,
  Pause as PauseIcon,
  PlayArrow as PlayIcon,
  FullscreenExit,
  Fullscreen,
} from '@mui/icons-material'
import { usePipelineMetrics } from '../../hooks/usePipelineMetrics'
import { MetricsGrid, type PipelineMetrics } from './MetricsCard'
import { StageMetricsTable } from './StageMetricsTable'
import { ThroughputChart, MultiSeriesChart } from './ThroughputChart'
import type { ActorMetrics } from '../../types/graph'

interface DataPoint {
  timestamp: number
  value: number
}

interface PipelineMonitorProps {
  pipelineId: string
  pipelineName?: string
  refreshInterval?: number
}

export function PipelineMonitor({
  pipelineId,
  pipelineName,
  refreshInterval = 5000,
}: PipelineMonitorProps) {
  const [isPaused, setIsPaused] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [viewMode, setViewMode] = useState<'overview' | 'stages' | 'charts'>('overview')

  // 메트릭스 히스토리 (차트용)
  const [throughputHistory, setThroughputHistory] = useState<DataPoint[]>([])
  const [stageHistories, setStageHistories] = useState<Record<string, DataPoint[]>>({})

  // 집계 메트릭스
  const [aggregatedMetrics, setAggregatedMetrics] = useState<PipelineMetrics | null>(null)

  const containerRef = useRef<HTMLDivElement>(null)

  const { metrics, loading, error, refetch } = usePipelineMetrics(
    pipelineId,
    isPaused ? 0 : refreshInterval
  )

  // 메트릭스 업데이트 시 히스토리에 추가
  useEffect(() => {
    if (!metrics) return

    const now = Date.now()

    // 전체 처리량 계산
    const totalThroughput = Object.values(metrics).reduce(
      (sum, m) => sum + m.throughput_per_sec,
      0
    )

    setThroughputHistory((prev) => {
      const newHistory = [...prev, { timestamp: now, value: totalThroughput }]
      // 최근 120개 (10분) 유지
      return newHistory.slice(-120)
    })

    // Stage별 히스토리 업데이트
    setStageHistories((prev) => {
      const newHistories: Record<string, DataPoint[]> = { ...prev }

      for (const [stageName, stageMetrics] of Object.entries(metrics)) {
        const stageHistory = prev[stageName] || []
        newHistories[stageName] = [
          ...stageHistory,
          { timestamp: now, value: stageMetrics.throughput_per_sec },
        ].slice(-120)
      }

      return newHistories
    })

    // 집계 메트릭스 계산
    const metricsValues = Object.values(metrics)
    if (metricsValues.length > 0) {
      const totalInput = metricsValues.reduce((sum, m) => sum + m.input_count, 0)
      const totalOutput = metricsValues.reduce((sum, m) => sum + m.output_count, 0)
      const totalErrors = metricsValues.reduce((sum, m) => sum + m.error_count, 0)

      setAggregatedMetrics({
        throughput_per_sec: totalThroughput / metricsValues.length,
        input_count: totalInput,
        output_count: totalOutput,
        error_count: totalErrors,
        error_rate: totalInput > 0 ? (totalErrors / totalInput) * 100 : 0,
        avg_latency_ms: 0, // TODO: 실제 지연시간 측정 필요
        buffer_size: 0,
        buffer_capacity: 1000,
      })
    }
  }, [metrics])

  const handleTogglePause = useCallback(() => {
    setIsPaused((prev) => !prev)
  }, [])

  const handleToggleFullscreen = useCallback(() => {
    if (!containerRef.current) return

    if (!isFullscreen) {
      containerRef.current.requestFullscreen?.()
    } else {
      document.exitFullscreen?.()
    }
    setIsFullscreen((prev) => !prev)
  }, [isFullscreen])

  const handleViewModeChange = useCallback(
    (_: React.MouseEvent<HTMLElement>, newMode: 'overview' | 'stages' | 'charts' | null) => {
      if (newMode) {
        setViewMode(newMode)
      }
    },
    []
  )

  // 상태 색상 결정
  const getStatusColor = (metrics: Record<string, ActorMetrics> | null): 'success' | 'warning' | 'error' | 'default' => {
    if (!metrics) return 'default'

    const states = Object.values(metrics).map((m) => m.state)

    if (states.some((s) => s === 'failed')) return 'error'
    if (states.some((s) => s === 'restarting' || s === 'stopping')) return 'warning'
    if (states.every((s) => s === 'running')) return 'success'
    return 'default'
  }

  return (
    <Box ref={containerRef} sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 헤더 */}
      <Paper sx={{ px: 2, py: 1, mb: 2 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <Typography variant="h6">
              {pipelineName || `Pipeline ${pipelineId.slice(0, 8)}`}
            </Typography>
            <Chip
              label={error ? 'Error' : isPaused ? 'Paused' : 'Live'}
              size="small"
              color={error ? 'error' : isPaused ? 'default' : getStatusColor(metrics)}
              sx={{ animation: !isPaused && !error ? 'pulse 2s infinite' : 'none' }}
            />
          </Box>

          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <ToggleButtonGroup
              size="small"
              value={viewMode}
              exclusive
              onChange={handleViewModeChange}
            >
              <ToggleButton value="overview">Overview</ToggleButton>
              <ToggleButton value="stages">Stages</ToggleButton>
              <ToggleButton value="charts">Charts</ToggleButton>
            </ToggleButtonGroup>

            <Divider orientation="vertical" flexItem sx={{ mx: 1 }} />

            <Tooltip title={isPaused ? 'Resume' : 'Pause'}>
              <IconButton size="small" onClick={handleTogglePause}>
                {isPaused ? <PlayIcon /> : <PauseIcon />}
              </IconButton>
            </Tooltip>

            <Tooltip title="Refresh">
              <IconButton size="small" onClick={refetch} disabled={loading}>
                <RefreshIcon />
              </IconButton>
            </Tooltip>

            <Tooltip title={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}>
              <IconButton size="small" onClick={handleToggleFullscreen}>
                {isFullscreen ? <FullscreenExit /> : <Fullscreen />}
              </IconButton>
            </Tooltip>
          </Box>
        </Box>
      </Paper>

      {/* 에러 표시 */}
      {error && (
        <Paper sx={{ p: 2, mb: 2, bgcolor: 'error.main', color: 'error.contrastText' }}>
          <Typography>Failed to load metrics: {error.message}</Typography>
        </Paper>
      )}

      {/* 메인 콘텐츠 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {viewMode === 'overview' && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {/* 메트릭스 카드 */}
            <MetricsGrid metrics={aggregatedMetrics} loading={loading} />

            {/* 처리량 차트 */}
            <ThroughputChart
              title="Total Throughput"
              data={throughputHistory}
              height={200}
            />

            {/* Stage 테이블 */}
            <Box>
              <Typography variant="subtitle1" sx={{ mb: 1 }}>
                Stage Metrics
              </Typography>
              <StageMetricsTable metrics={metrics} loading={loading} />
            </Box>
          </Box>
        )}

        {viewMode === 'stages' && (
          <Box>
            <Typography variant="subtitle1" sx={{ mb: 2 }}>
              Detailed Stage Metrics
            </Typography>
            <StageMetricsTable metrics={metrics} loading={loading} />
          </Box>
        )}

        {viewMode === 'charts' && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <ThroughputChart
              title="Total Throughput"
              data={throughputHistory}
              height={250}
            />

            <MultiSeriesChart
              data={stageHistories}
              height={400}
            />
          </Box>
        )}
      </Box>

      {/* 스타일 */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.6; }
        }
      `}</style>
    </Box>
  )
}

export default PipelineMonitor
