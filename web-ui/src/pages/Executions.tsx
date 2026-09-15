import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'

import { api } from '../services/api'

// 실행 중인 파이프라인이 "어느 파드에서, 몇 개가, 얼마나 바쁘게" 도는지 보여준다.
//
// 이 화면이 없던 동안 /agents 는 실행 개수(숫자)만, /runner 는 바이너리 버전만 보여줬다.
// 어느 파드가 도는지·바쁜지는 볼 수 없었고, 작업이 없을 때 "없다"는 것도 알 수 없었다.

export interface ExecutionRow {
  id: string
  workflow_id: string
  workflow?: { name?: string; type?: string }
  status: string
  started_at?: string
  completed_at?: string
  agent_id?: string
  // delegated_to 는 위임 생성한 K8s 리소스명이다(realtime 은 상주 파드, batch 는 Job).
  delegated_to?: string
  total_records?: number
  failed_records?: number
  error_message?: string
}

const RUNNING = 'running'

// 목록 API 는 { executions, total, limit, offset } 을 준다.
// 한때 여기서 items 를 읽어, 실행이 있어도 화면이 늘 비어 있었다(실측).
// 응답 모양이 바뀌면 화면이 조용히 빈 채로 남으므로 이 변환만 따로 테스트한다.
export function parseExecutionList(data: unknown): ExecutionRow[] {
  if (Array.isArray(data)) return data as ExecutionRow[]
  const list = (data as { executions?: unknown })?.executions
  return Array.isArray(list) ? (list as ExecutionRow[]) : []
}

export default function ExecutionsPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [rows, setRows] = useState<ExecutionRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchExecutions = useCallback(async () => {
    try {
      const res = await api.getAllExecutions({ status: RUNNING, limit: 200 })
      setRows(parseExecutionList(res?.data))
      setError(null)
    } catch (err: unknown) {
      // 조용히 실패하면 "실행 없음" 과 "조회 실패" 가 화면에서 구분되지 않는다.
      const e = err as { response?: { data?: { error?: { message?: string } } } }
      setError(e.response?.data?.error?.message || t('executions.loadError'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    fetchExecutions()
    // 실행 중 화면이므로 주기 갱신한다. 5초는 파드 상태 보고 주기(30초)보다 짧아
    // 중앙에 반영된 변화를 놓치지 않는다.
    const timer = setInterval(fetchExecutions, 5000)
    return () => clearInterval(timer)
  }, [fetchExecutions])

  // 파드별로 묶는다 — "파드 하나에 실행 몇 개" 가 이 화면의 핵심 질문이다.
  const byPod = useMemo(() => {
    const groups = new Map<string, ExecutionRow[]>()
    for (const r of rows) {
      const key = r.delegated_to || t('executions.inProcess')
      groups.set(key, [...(groups.get(key) ?? []), r])
    }
    return groups
  }, [rows, t])

  const realtimeCount = rows.filter((r) => r.workflow?.type === 'realtime').length
  const batchCount = rows.filter((r) => r.workflow?.type === 'batch').length

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Typography variant="h5">{t('executions.title')}</Typography>
        <Button startIcon={<RefreshIcon />} onClick={fetchExecutions} disabled={loading}>
          {t('common.refresh')}
        </Button>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Box sx={{ display: 'flex', gap: 1, mb: 2, flexWrap: 'wrap' }}>
        <Chip label={t('executions.podCount', { count: byPod.size })} />
        <Chip label={t('executions.realtimeCount', { count: realtimeCount })} color="primary" variant="outlined" />
        <Chip label={t('executions.batchCount', { count: batchCount })} color="secondary" variant="outlined" />
      </Box>

      {loading && rows.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : rows.length === 0 ? (
        // "작업이 없으면 없다는 상태" 를 명시한다 — 빈 화면은 장애와 구분되지 않는다.
        <Card>
          <CardContent sx={{ textAlign: 'center', py: 6 }}>
            <Typography color="text.secondary">{t('executions.empty')}</Typography>
          </CardContent>
        </Card>
      ) : (
        [...byPod.entries()].map(([pod, items]) => (
          <Card key={pod} sx={{ mb: 2 }}>
            <CardHeader
              title={
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography sx={{ fontFamily: 'monospace' }}>{pod}</Typography>
                  <Chip size="small" label={t('executions.execCount', { count: items.length })} />
                </Box>
              }
              subheader={items[0]?.agent_id ? `agent: ${items[0].agent_id}` : undefined}
              titleTypographyProps={{ variant: 'subtitle1' }}
            />
            <CardContent sx={{ pt: 0 }}>
              <TableContainer sx={{ overflowX: 'auto' }}>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>{t('executions.workflow')}</TableCell>
                      <TableCell>{t('executions.type')}</TableCell>
                      <TableCell align="right">{t('executions.records')}</TableCell>
                      <TableCell align="right">{t('executions.failed')}</TableCell>
                      <TableCell>{t('executions.startedAt')}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {items.map((r) => (
                      <TableRow
                        key={r.id}
                        hover
                        sx={{ cursor: 'pointer' }}
                        onClick={() => navigate(`/workflows/${r.workflow_id}`)}
                      >
                        <TableCell>
                          <Tooltip title={r.id}>
                            <span>{r.workflow?.name || r.workflow_id}</span>
                          </Tooltip>
                        </TableCell>
                        <TableCell>
                          <Chip
                            size="small"
                            label={r.workflow?.type || '-'}
                            color={r.workflow?.type === 'realtime' ? 'primary' : 'secondary'}
                            variant="outlined"
                          />
                        </TableCell>
                        <TableCell align="right">{(r.total_records ?? 0).toLocaleString()}</TableCell>
                        <TableCell align="right">
                          {r.failed_records ? (
                            <Typography component="span" color="error">
                              {r.failed_records.toLocaleString()}
                            </Typography>
                          ) : (
                            0
                          )}
                        </TableCell>
                        <TableCell sx={{ whiteSpace: 'nowrap' }}>
                          {r.started_at ? new Date(r.started_at).toLocaleString() : '-'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </CardContent>
          </Card>
        ))
      )}
    </Box>
  )
}
