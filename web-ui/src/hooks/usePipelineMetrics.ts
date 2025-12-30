import { useState, useEffect, useRef, useCallback } from 'react'
import { api } from '../services/api'
import type { ActorMetricsMap } from '../types/graph'

interface UsePipelineMetricsResult {
  metrics: ActorMetricsMap | null
  loading: boolean
  error: Error | null
  refetch: () => Promise<void>
}

export function usePipelineMetrics(
  pipelineId: string,
  interval: number = 5000
): UsePipelineMetricsResult {
  const [metrics, setMetrics] = useState<ActorMetricsMap | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchMetrics = useCallback(async () => {
    if (!pipelineId) return

    try {
      const response = await api.getPipelineActorMetrics(pipelineId)
      if (response.success) {
        setMetrics(response.data || {})
        setError(null)
      }
    } catch (err) {
      setError(err as Error)
    } finally {
      setLoading(false)
    }
  }, [pipelineId])

  useEffect(() => {
    // 초기 조회
    fetchMetrics()

    // 폴링 설정
    if (interval > 0) {
      intervalRef.current = setInterval(fetchMetrics, interval)
    }

    // Cleanup
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
      }
    }
  }, [fetchMetrics, interval])

  return { metrics, loading, error, refetch: fetchMetrics }
}
