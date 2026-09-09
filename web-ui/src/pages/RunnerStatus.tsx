import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material'
import { Refresh as RefreshIcon } from '@mui/icons-material'
import { useTranslation } from 'react-i18next'

import {
  getRunnerDeployments,
  getRunnerVersions,
  type RunnerDeploymentStatus,
  type RunnerVersion,
} from '../services/pluginApi'

// 빌드가 진행 중이거나 배포 미확정인 실행이 있으면 짧게 폴링한다.
// 정적인 상태에서 계속 두드리면 낭비이므로, 움직이는 것이 있을 때만 돈다.
const POLL_MS = 5000

function statusColor(s: RunnerVersion['status']) {
  switch (s) {
    case 'ready':
      return 'success' as const
    case 'failed':
      return 'error' as const
    case 'building':
    case 'pending':
      return 'info' as const
    default:
      return 'default' as const
  }
}

function durationText(ms?: number) {
  if (!ms || ms <= 0) return '-'
  const s = Math.round(ms / 1000)
  return s >= 60 ? `${Math.floor(s / 60)}m ${s % 60}s` : `${s}s`
}

/**
 * Runner 빌드·배포 현황 화면.
 *
 * 왜 필요한가: native stage 워크플로우는 GHCR 이미지가 아니라 DB 에 저장된 바이너리로
 * 실행된다. 그래서 "빌드는 최신인데 노드는 옛 바이너리로 돈다" 는 상태가 생기고,
 * 이 화면이 없으면 kubectl 로 initContainer 명령을 뜯어야 확인할 수 있었다(실측).
 */
export default function RunnerStatusPage() {
  const { t } = useTranslation()
  const [versions, setVersions] = useState<RunnerVersion[]>([])
  const [deployments, setDeployments] = useState<RunnerDeploymentStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expandedLog, setExpandedLog] = useState<string | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const load = useCallback(async () => {
    // 한쪽이 실패해도 나머지는 보여준다 — 전체 화면을 비우면 원인 파악이 어려워진다.
    const [vRes, dRes] = await Promise.allSettled([getRunnerVersions(), getRunnerDeployments()])
    if (vRes.status === 'fulfilled') setVersions(vRes.value)
    if (dRes.status === 'fulfilled') setDeployments(dRes.value)
    setError(
      vRes.status === 'rejected' || dRes.status === 'rejected'
        ? t('runner.loadPartialError', 'Some data could not be loaded')
        : null,
    )
    setLoading(false)
  }, [t])

  useEffect(() => {
    load()
  }, [load])

  const moving = Boolean(deployments?.building_version) || (deployments?.deploying_count ?? 0) > 0
  useEffect(() => {
    if (moving) {
      pollRef.current = setInterval(load, POLL_MS)
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [moving, load])

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress />
      </Box>
    )
  }

  const latest = deployments?.latest_ready_version
  const staleCount = deployments?.stale_count ?? 0
  const deployingCount = deployments?.deploying_count ?? 0

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Typography variant="h5">{t('runner.title', 'Runner Builds & Deployments')}</Typography>
        <Button startIcon={<RefreshIcon />} onClick={load}>
          {t('common.refresh', 'Refresh')}
        </Button>
      </Box>

      {error && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {deployments?.building_version && (
        <Alert severity="info" icon={<CircularProgress size={18} />} sx={{ mb: 2 }}>
          {t('runner.buildingNow', 'Build in progress: {{id}}', { id: deployments.building_version })}
        </Alert>
      )}

      {staleCount > 0 && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          {t(
            'runner.staleRunning',
            '{{count}} running execution(s) use an older runner than the latest build ({{latest}}). Restart them to pick up the new binary.',
            { count: staleCount, latest: latest || '-' },
          )}
        </Alert>
      )}

      {deployingCount > 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {t('runner.deployingNow', '{{count}} execution(s) are still being placed on a node.', {
            count: deployingCount,
          })}
        </Alert>
      )}

      {/* 노드 배포 현황 — 어느 노드에서 어떤 버전이 도는지 */}
      <Typography variant="h6" sx={{ mb: 1 }}>
        {t('runner.deployments', 'Deployed on nodes')}
        {latest && (
          <Typography component="span" variant="body2" sx={{ ml: 1, color: 'text.secondary' }}>
            {t('runner.latestIs', 'latest: {{id}} (#{{no}})', {
              id: latest,
              no: deployments?.latest_ready_build_number ?? '-',
            })}
          </Typography>
        )}
      </Typography>
      <TableContainer component={Paper} sx={{ mb: 4, overflowX: 'auto' }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>{t('runner.workflow', 'Workflow')}</TableCell>
              <TableCell>{t('runner.node', 'Node')}</TableCell>
              <TableCell>{t('runner.version', 'Runner version')}</TableCell>
              <TableCell>{t('runner.state', 'State')}</TableCell>
              <TableCell>{t('runner.startedAt', 'Started')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {(deployments?.deployments.length ?? 0) === 0 ? (
              <TableRow>
                <TableCell colSpan={5}>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                    {t('runner.noRunning', 'No running executions')}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              deployments?.deployments.map((d) => (
                <TableRow key={d.execution_id}>
                  <TableCell>
                    <Typography variant="body2">{d.workflow_name || d.workflow_id}</Typography>
                    <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                      {d.workflow_type}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    {d.agent_id ? (
                      <Typography variant="body2" noWrap title={d.agent_id} sx={{ maxWidth: 220 }}>
                        {d.agent_id}
                      </Typography>
                    ) : (
                      '-'
                    )}
                  </TableCell>
                  <TableCell>
                    {d.runner_version_id ? (
                      <Chip
                        size="small"
                        variant="outlined"
                        color={d.stale ? 'warning' : 'default'}
                        label={`${d.runner_version_id}${d.runner_build_number ? ` (#${d.runner_build_number})` : ''}`}
                      />
                    ) : (
                      <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                        {t('runner.noNativeStage', 'no native stage')}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell>
                    {d.deploying ? (
                      <Chip size="small" color="info" label={t('runner.deploying', 'deploying')} />
                    ) : d.stale ? (
                      <Chip size="small" color="warning" label={t('runner.stale', 'outdated')} />
                    ) : (
                      <Chip size="small" color="success" label={t('runner.upToDate', 'up to date')} />
                    )}
                  </TableCell>
                  <TableCell>
                    {d.started_at ? new Date(d.started_at).toLocaleString() : '-'}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>

      {/* 빌드 히스토리 — 실패 로그까지 여기서 본다 */}
      <Typography variant="h6" sx={{ mb: 1 }}>
        {t('runner.history', 'Build history')}
      </Typography>
      <TableContainer component={Paper} sx={{ overflowX: 'auto' }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>#</TableCell>
              <TableCell>{t('runner.versionId', 'Version')}</TableCell>
              <TableCell>{t('runner.status', 'Status')}</TableCell>
              <TableCell>{t('runner.duration', 'Duration')}</TableCell>
              <TableCell>{t('runner.finishedAt', 'Finished')}</TableCell>
              <TableCell>{t('runner.log', 'Log')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {versions.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6}>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                    {t('runner.noBuilds', 'No builds yet')}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              versions.map((v) => (
                // Fragment 로 본문 행 + 로그 행을 묶는다. key 는 Fragment 에 준다 —
                // 자식에만 주면 React 가 리스트 항목을 식별하지 못한다.
                <Fragment key={v.id}>
                  <TableRow>
                    <TableCell>{v.build_number}</TableCell>
                    <TableCell>
                      <Typography variant="body2">
                        {v.id}
                        {v.id === latest && (
                          <Chip
                            size="small"
                            color="success"
                            variant="outlined"
                            label={t('runner.current', 'current')}
                            sx={{ ml: 1 }}
                          />
                        )}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Chip size="small" color={statusColor(v.status)} label={v.status} />
                    </TableCell>
                    <TableCell>{durationText(v.duration_ms)}</TableCell>
                    <TableCell>
                      {v.finished_at ? new Date(v.finished_at).toLocaleString() : '-'}
                    </TableCell>
                    <TableCell>
                      {v.build_log || v.error ? (
                        <Button
                          size="small"
                          onClick={() => setExpandedLog(expandedLog === v.id ? null : v.id)}
                        >
                          {expandedLog === v.id
                            ? t('common.hide', 'Hide')
                            : t('runner.viewLog', 'View')}
                        </Button>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                  </TableRow>
                  {expandedLog === v.id && (
                    <TableRow>
                      <TableCell colSpan={6} sx={{ bgcolor: 'action.hover' }}>
                        {v.error && (
                          <Alert severity="error" sx={{ mb: 1 }}>
                            {v.error}
                          </Alert>
                        )}
                        <Box
                          component="pre"
                          sx={{
                            m: 0,
                            maxHeight: 320,
                            overflow: 'auto',
                            fontSize: 12,
                            whiteSpace: 'pre-wrap',
                          }}
                        >
                          {v.build_log || t('runner.noLog', '(no log)')}
                        </Box>
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  )
}
