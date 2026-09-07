import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Alert,
  Box,
  Chip,
  CircularProgress,
  FormControl,
  IconButton,
  MenuItem,
  Paper,
  Select,
  Snackbar,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'

import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import type { ExecutionListData, WorkflowExecution } from '../types/execution'

// 서버 페이징(limit/offset)을 쓴다 — 실행 이력은 계속 쌓이므로 전량 로드 후 자르면 안 된다.
const ROWS_PER_PAGE_OPTIONS = [10, 25, 50, 100]

const STATUS_FILTERS = ['running', 'completed', 'failed', 'error', 'stopped'] as const

function statusColor(status: string): 'success' | 'error' | 'info' | 'default' {
  switch (status) {
    case 'completed':
      return 'success'
    case 'failed':
    case 'error':
      return 'error'
    case 'running':
      return 'info'
    default:
      return 'default'
  }
}

function formatDuration(exec: WorkflowExecution): string {
  const ms =
    exec.duration_ms ??
    (exec.completed_at
      ? new Date(exec.completed_at).getTime() - new Date(exec.started_at).getTime()
      : 0)
  if (!ms || ms < 0) return '-'
  if (ms < 1000) return `${ms}ms`
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(1)}s`
  const min = Math.floor(sec / 60)
  return `${min}m ${Math.round(sec % 60)}s`
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString()
}

export default function HistoryPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { open, message, severity, showError, closeSnackbar } = useSnackbar()

  const [executions, setExecutions] = useState<WorkflowExecution[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [rowsPerPage, setRowsPerPage] = useState(25)
  const [status, setStatus] = useState('')
  const [loading, setLoading] = useState(true)

  const fetchExecutions = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.getAllExecutions({
        status: status || undefined,
        limit: rowsPerPage,
        offset: page * rowsPerPage,
      })
      if (res.success) {
        const data = res.data as ExecutionListData
        setExecutions(data.executions || [])
        setTotal(data.total || 0)
      } else {
        showError(t('history.loadError'))
      }
    } catch {
      showError(t('history.loadError'))
    } finally {
      setLoading(false)
    }
  }, [page, rowsPerPage, status, showError, t])

  useEffect(() => {
    fetchExecutions()
  }, [fetchExecutions])

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {t('history.title')}
        </Typography>

        <FormControl size="small" sx={{ minWidth: 160 }}>
          <Select
            displayEmpty
            value={status}
            onChange={(e) => {
              setStatus(e.target.value)
              setPage(0) // 필터가 바뀌면 총 건수가 달라져 현재 페이지가 범위를 벗어날 수 있다
            }}
          >
            <MenuItem value="">{t('history.allStatuses')}</MenuItem>
            {STATUS_FILTERS.map((s) => (
              <MenuItem key={s} value={s}>
                {t(`status.${s}`, s)}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <Tooltip title={t('common.refresh')}>
          <IconButton onClick={fetchExecutions} disabled={loading}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {loading && executions.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : (
        <TableContainer component={Paper} sx={{ overflowX: 'auto' }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('history.workflow')}</TableCell>
                <TableCell>{t('common.status')}</TableCell>
                <TableCell>{t('workflow.startedAt')}</TableCell>
                <TableCell>{t('workflow.duration')}</TableCell>
                <TableCell align="right">{t('workflow.totalRecords')}</TableCell>
                <TableCell align="right">{t('workflow.failedRecords')}</TableCell>
                <TableCell>{t('history.triggeredBy')}</TableCell>
                <TableCell align="right">{t('common.actions')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {executions.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} align="center" sx={{ py: 4, color: 'text.secondary' }}>
                    {t('common.noData')}
                  </TableCell>
                </TableRow>
              ) : (
                executions.map((exec) => (
                  <TableRow key={exec.id} hover>
                    <TableCell>
                      <Typography variant="body2">
                        {exec.workflow?.name || exec.workflow_id}
                      </Typography>
                      {exec.parent_execution_id && (
                        <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                          {t('history.subExecution')}
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        label={t(`status.${exec.status}`, exec.status)}
                        color={statusColor(exec.status)}
                      />
                    </TableCell>
                    <TableCell>{formatDateTime(exec.started_at)}</TableCell>
                    <TableCell>{formatDuration(exec)}</TableCell>
                    <TableCell align="right">{exec.total_records.toLocaleString()}</TableCell>
                    <TableCell align="right">
                      {exec.failed_records > 0 ? (
                        <Typography variant="body2" color="error">
                          {exec.failed_records.toLocaleString()}
                        </Typography>
                      ) : (
                        '0'
                      )}
                    </TableCell>
                    <TableCell>{exec.triggered_by || '-'}</TableCell>
                    <TableCell align="right">
                      <Tooltip title={t('history.openWorkflow')}>
                        <IconButton
                          size="small"
                          onClick={() => navigate(`/workflows/${exec.workflow_id}`)}
                        >
                          <OpenInNewIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
          <TablePagination
            component="div"
            count={total}
            page={page}
            onPageChange={(_, p) => setPage(p)}
            rowsPerPage={rowsPerPage}
            rowsPerPageOptions={ROWS_PER_PAGE_OPTIONS}
            onRowsPerPageChange={(e) => {
              setRowsPerPage(parseInt(e.target.value, 10))
              setPage(0)
            }}
          />
        </TableContainer>
      )}

      {executions.some((e) => e.error_message) && (
        <Alert severity="warning" sx={{ mt: 2 }}>
          {t('history.hasErrors')}
        </Alert>
      )}

      <Snackbar open={open} autoHideDuration={5000} onClose={closeSnackbar}>
        <Alert severity={severity} onClose={closeSnackbar}>
          {message}
        </Alert>
      </Snackbar>
    </Box>
  )
}
