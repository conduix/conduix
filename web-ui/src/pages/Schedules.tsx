import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Alert,
  Box,
  Chip,
  CircularProgress,
  FormControlLabel,
  IconButton,
  Paper,
  Snackbar,
  Switch,
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
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'

import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import type { WorkflowSchedule } from '../types/execution'

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString()
}

// 스케줄 표현식: cron 이면 cron 식, interval 이면 주기, manual/미설정이면 없음.
function scheduleExpression(s: WorkflowSchedule): string {
  if (s.type === 'cron') return s.cron || '-'
  if (s.type === 'interval') return s.interval || '-'
  return '-'
}

export default function SchedulesPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { open, message, severity, showSuccess, showError, closeSnackbar } = useSnackbar()

  const [schedules, setSchedules] = useState<WorkflowSchedule[]>([])
  const [loading, setLoading] = useState(true)
  const [onlyEnabled, setOnlyEnabled] = useState(false)
  // 토글·즉시실행 중인 workflow_id — 연타로 중복 요청이 나가지 않게 버튼만 잠근다.
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set())

  const fetchSchedules = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.getSchedules(onlyEnabled ? { enabled: true } : undefined)
      if (res.success) {
        setSchedules((res.data as WorkflowSchedule[]) || [])
      } else {
        showError(t('schedules.loadError'))
      }
    } catch {
      showError(t('schedules.loadError'))
    } finally {
      setLoading(false)
    }
  }, [onlyEnabled, showError, t])

  useEffect(() => {
    fetchSchedules()
  }, [fetchSchedules])

  const withBusy = async (workflowID: string, fn: () => Promise<void>) => {
    setBusyIds((prev) => new Set(prev).add(workflowID))
    try {
      await fn()
    } finally {
      setBusyIds((prev) => {
        const next = new Set(prev)
        next.delete(workflowID)
        return next
      })
    }
  }

  const handleToggle = (s: WorkflowSchedule) =>
    withBusy(s.workflow_id, async () => {
      try {
        const res = await api.setWorkflowScheduleEnabled(s.workflow_id, !s.enabled)
        if (res.success) {
          showSuccess(t('schedules.toggleSuccess'))
          await fetchSchedules()
        } else {
          // cron 미설정 상태에서 enable 하면 백엔드가 거절한다 — 그 이유를 그대로 보여준다.
          showError(res.error?.message || t('schedules.toggleError'))
        }
      } catch {
        showError(t('schedules.toggleError'))
      }
    })

  const handleTriggerNow = (s: WorkflowSchedule) =>
    withBusy(s.workflow_id, async () => {
      try {
        const res = await api.triggerWorkflowNow(s.workflow_id)
        if (res.success) {
          showSuccess(t('schedules.triggerSuccess'))
        } else {
          showError(res.error?.message || t('schedules.triggerError'))
        }
      } catch {
        showError(t('schedules.triggerError'))
      }
    })

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {t('schedules.title')}
        </Typography>

        <FormControlLabel
          control={
            <Switch
              size="small"
              checked={onlyEnabled}
              onChange={(e) => setOnlyEnabled(e.target.checked)}
            />
          }
          label={t('schedules.onlyEnabled')}
        />

        <Tooltip title={t('common.refresh')}>
          <IconButton onClick={fetchSchedules} disabled={loading}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      <Alert severity="info" sx={{ mb: 2 }}>
        {t('schedules.batchOnlyNotice')}
      </Alert>

      {loading && schedules.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : (
        <TableContainer component={Paper} sx={{ overflowX: 'auto' }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('schedules.workflow')}</TableCell>
                <TableCell>{t('schedules.type')}</TableCell>
                <TableCell>{t('schedules.expression')}</TableCell>
                <TableCell>{t('schedules.timezone')}</TableCell>
                <TableCell>{t('schedule.lastRun')}</TableCell>
                <TableCell>{t('schedule.nextRun')}</TableCell>
                <TableCell>{t('schedule.enabled')}</TableCell>
                <TableCell align="right">{t('common.actions')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {schedules.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} align="center" sx={{ py: 4, color: 'text.secondary' }}>
                    {t('common.noData')}
                  </TableCell>
                </TableRow>
              ) : (
                schedules.map((s) => {
                  const busy = busyIds.has(s.workflow_id)
                  return (
                    <TableRow key={s.workflow_id} hover>
                      <TableCell>{s.workflow_name || s.workflow_id}</TableCell>
                      <TableCell>
                        <Chip
                          size="small"
                          variant="outlined"
                          label={s.type ? t(`schedules.type_${s.type}`, s.type) : t('schedules.unset')}
                        />
                      </TableCell>
                      <TableCell>
                        <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                          {scheduleExpression(s)}
                        </Typography>
                      </TableCell>
                      <TableCell>{s.timezone || '-'}</TableCell>
                      <TableCell>{formatDateTime(s.last_run_at)}</TableCell>
                      <TableCell>{formatDateTime(s.next_run_at)}</TableCell>
                      <TableCell>
                        <Switch
                          size="small"
                          checked={s.enabled}
                          disabled={busy}
                          onChange={() => handleToggle(s)}
                        />
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title={t('schedules.triggerNow')}>
                          <IconButton
                            size="small"
                            disabled={busy}
                            onClick={() => handleTriggerNow(s)}
                          >
                            <PlayArrowIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title={t('schedules.openWorkflow')}>
                          <IconButton
                            size="small"
                            onClick={() => navigate(`/workflows/${s.workflow_id}`)}
                          >
                            <OpenInNewIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <Snackbar open={open} autoHideDuration={5000} onClose={closeSnackbar}>
        <Alert severity={severity} onClose={closeSnackbar}>
          {message}
        </Alert>
      </Snackbar>
    </Box>
  )
}
