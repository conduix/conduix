/**
 * 처리량 시계열 차트 컴포넌트
 * Canvas 기반의 간단한 라인 차트
 */

import { useRef, useEffect, useState, useCallback } from 'react'
import { Box, Paper, Typography, useTheme, ToggleButton, ToggleButtonGroup } from '@mui/material'

interface DataPoint {
  timestamp: number
  value: number
}

interface ThroughputChartProps {
  title?: string
  data: DataPoint[]
  color?: string
  height?: number
  maxPoints?: number
  yAxisLabel?: string
}

export function ThroughputChart({
  title = 'Throughput',
  data,
  color,
  height = 200,
  maxPoints = 60,
  yAxisLabel = '/sec',
}: ThroughputChartProps) {
  const theme = useTheme()
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartColor = color || theme.palette.primary.main

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Canvas 크기 설정 (고해상도 대응)
    const dpr = window.devicePixelRatio || 1
    const rect = canvas.getBoundingClientRect()
    canvas.width = rect.width * dpr
    canvas.height = rect.height * dpr
    ctx.scale(dpr, dpr)

    // 데이터 정제 (최근 maxPoints개만)
    const points = data.slice(-maxPoints)

    // 클리어
    ctx.clearRect(0, 0, rect.width, rect.height)

    if (points.length < 2) {
      // 데이터 부족 시 메시지
      ctx.fillStyle = theme.palette.text.secondary
      ctx.font = '14px system-ui'
      ctx.textAlign = 'center'
      ctx.fillText('Waiting for data...', rect.width / 2, rect.height / 2)
      return
    }

    // 마진 설정
    const marginLeft = 50
    const marginRight = 20
    const marginTop = 20
    const marginBottom = 30
    const chartWidth = rect.width - marginLeft - marginRight
    const chartHeight = rect.height - marginTop - marginBottom

    // Y축 범위 계산
    const values = points.map((p) => p.value)
    const maxValue = Math.max(...values, 1)
    const minValue = 0

    // 그리드 그리기
    ctx.strokeStyle = theme.palette.divider
    ctx.lineWidth = 1

    // 수평선 (Y축 그리드)
    const yGridCount = 4
    for (let i = 0; i <= yGridCount; i++) {
      const y = marginTop + (chartHeight / yGridCount) * i
      ctx.beginPath()
      ctx.moveTo(marginLeft, y)
      ctx.lineTo(rect.width - marginRight, y)
      ctx.stroke()

      // Y축 레이블
      const value = maxValue - (maxValue / yGridCount) * i
      ctx.fillStyle = theme.palette.text.secondary
      ctx.font = '11px system-ui'
      ctx.textAlign = 'right'
      ctx.fillText(value.toFixed(0), marginLeft - 5, y + 4)
    }

    // 차트 라인 그리기
    ctx.beginPath()
    ctx.strokeStyle = chartColor
    ctx.lineWidth = 2
    ctx.lineJoin = 'round'
    ctx.lineCap = 'round'

    points.forEach((point, index) => {
      const x = marginLeft + (chartWidth / (points.length - 1)) * index
      const y = marginTop + chartHeight - ((point.value - minValue) / (maxValue - minValue)) * chartHeight

      if (index === 0) {
        ctx.moveTo(x, y)
      } else {
        ctx.lineTo(x, y)
      }
    })
    ctx.stroke()

    // 그라데이션 영역 채우기
    const gradient = ctx.createLinearGradient(0, marginTop, 0, marginTop + chartHeight)
    gradient.addColorStop(0, `${chartColor}40`)
    gradient.addColorStop(1, `${chartColor}05`)

    ctx.lineTo(marginLeft + chartWidth, marginTop + chartHeight)
    ctx.lineTo(marginLeft, marginTop + chartHeight)
    ctx.closePath()
    ctx.fillStyle = gradient
    ctx.fill()

    // X축 레이블 (시간)
    ctx.fillStyle = theme.palette.text.secondary
    ctx.font = '10px system-ui'
    ctx.textAlign = 'center'

    const xLabelCount = Math.min(6, points.length)
    for (let i = 0; i < xLabelCount; i++) {
      const index = Math.floor((points.length - 1) * (i / (xLabelCount - 1)))
      const point = points[index]
      if (!point) continue

      const x = marginLeft + (chartWidth / (points.length - 1)) * index
      const date = new Date(point.timestamp)
      const timeStr = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
      ctx.fillText(timeStr, x, rect.height - 5)
    }
  }, [data, theme, chartColor, maxPoints])

  // 현재 값 계산
  const currentValue = data.length > 0 ? data[data.length - 1].value : 0
  const prevValue = data.length > 1 ? data[data.length - 2].value : currentValue
  const trend = prevValue > 0 ? ((currentValue - prevValue) / prevValue) * 100 : 0

  return (
    <Paper sx={{ p: 2 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
        <Typography variant="subtitle2" color="text.secondary">
          {title}
        </Typography>
        <Box sx={{ textAlign: 'right' }}>
          <Typography variant="h6" sx={{ color: chartColor }}>
            {currentValue.toFixed(1)}
            <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 0.5 }}>
              {yAxisLabel}
            </Typography>
          </Typography>
          {Math.abs(trend) > 0.1 && (
            <Typography
              variant="caption"
              color={trend > 0 ? 'success.main' : 'error.main'}
            >
              {trend > 0 ? '+' : ''}{trend.toFixed(1)}%
            </Typography>
          )}
        </Box>
      </Box>
      <Box sx={{ height }}>
        <canvas
          ref={canvasRef}
          style={{ width: '100%', height: '100%' }}
        />
      </Box>
    </Paper>
  )
}

interface MultiSeriesChartProps {
  data: Record<string, DataPoint[]>
  height?: number
  maxPoints?: number
}

export function MultiSeriesChart({ data, height = 250, maxPoints = 60 }: MultiSeriesChartProps) {
  const [selectedSeries, setSelectedSeries] = useState<string[]>([])
  const seriesNames = Object.keys(data)

  useEffect(() => {
    // 기본적으로 모든 시리즈 선택
    if (selectedSeries.length === 0 && seriesNames.length > 0) {
      setSelectedSeries(seriesNames.slice(0, 5)) // 최대 5개
    }
  }, [seriesNames, selectedSeries])

  const handleSeriesChange = useCallback(
    (_: React.MouseEvent<HTMLElement>, newSeries: string[]) => {
      setSelectedSeries(newSeries)
    },
    []
  )

  const colors = ['#1890ff', '#52c41a', '#722ed1', '#faad14', '#f5222d', '#13c2c2', '#eb2f96']

  return (
    <Paper sx={{ p: 2 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="subtitle2" color="text.secondary">
          Stage Throughput
        </Typography>
        <ToggleButtonGroup
          size="small"
          value={selectedSeries}
          onChange={handleSeriesChange}
          sx={{ flexWrap: 'wrap', gap: 0.5 }}
        >
          {seriesNames.map((name, i) => (
            <ToggleButton
              key={name}
              value={name}
              sx={{
                px: 1,
                py: 0.25,
                fontSize: '0.7rem',
                borderColor: colors[i % colors.length],
                '&.Mui-selected': {
                  bgcolor: `${colors[i % colors.length]}20`,
                  color: colors[i % colors.length],
                },
              }}
            >
              {name}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
      </Box>

      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 2 }}>
        {selectedSeries.map((seriesName, i) => (
          <ThroughputChart
            key={seriesName}
            title={seriesName}
            data={data[seriesName] || []}
            color={colors[i % colors.length]}
            height={height / 2}
            maxPoints={maxPoints}
          />
        ))}
      </Box>
    </Paper>
  )
}

export default ThroughputChart
