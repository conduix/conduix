import { useEffect, useState } from 'react'
import {
  Box,
  Typography,
  Card,
  CardContent,
  Chip,
  IconButton,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Visibility as EyeIcon,
  PlayCircle as PlayIcon,
  PauseCircle as PauseIcon,
  UploadFile as UploadIcon,
} from '@mui/icons-material'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'

interface Workflow {
  id: string
  name: string
  slug: string
  type: 'batch' | 'realtime'
  status: string
  project_id: string
  project?: {
    id: string
    name: string
    alias: string
  }
  created_at: string
  updated_at: string
}

export default function WorkflowsPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { showSuccess, showError } = useSnackbar()
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [loading, setLoading] = useState(true)
  const [importOpen, setImportOpen] = useState(false)
  const [importYaml, setImportYaml] = useState('')
  const [importProjectId, setImportProjectId] = useState('')
  const [importing, setImporting] = useState(false)

  useEffect(() => {
    fetchWorkflows()
  }, [])

  const fetchWorkflows = async () => {
    try {
      setLoading(true)
      const response = await api.getWorkflows()
      if (response.success) {
        const data = response.data
        setWorkflows(Array.isArray(data) ? data : (data.items || []))
      }
    } catch (error: unknown) {
      // 조용히 실패하면 "워크플로우가 없음" 과 "조회 실패" 가 화면에서 구분되지 않는다.
      const err = error as { response?: { data?: { error?: { message?: string } } } }
      showError(err.response?.data?.error?.message || t('workflow.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const handleStart = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const res = await api.startWorkflow(id)
      // 서버가 빌드를 걸고 실행을 예약한 경우 status=building 으로 온다.
      // 이걸 구분하지 않으면 "실행됨" 으로 보이고 사용자는 왜 안 도는지 알 수 없다.
      if (res.success && res.data?.status === 'building') {
        showSuccess(res.message || t('workflow.buildStarted'))
      } else if (res.success) {
        showSuccess(t('workflow.startSuccess'))
      } else {
        showError(res.error?.message || t('workflow.startError'))
      }
      fetchWorkflows()
    } catch (error: unknown) {
      // console.error 만 하면 사용자는 버튼을 눌러도 아무 반응이 없다고 느낀다 —
      // 실패 사유가 서버 응답에 들어 있는데도 화면에 전달되지 않았다.
      const err = error as { response?: { data?: { error?: { message?: string } } } }
      showError(err.response?.data?.error?.message || t('workflow.startError'))
    }
  }

  const handleStop = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const res = await api.stopWorkflow(id)
      if (res.success) {
        showSuccess(t('workflow.stopSuccess'))
      } else {
        showError(res.error?.message || t('workflow.stopError'))
      }
      fetchWorkflows()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { error?: { message?: string } } } }
      showError(err.response?.data?.error?.message || t('workflow.stopError'))
    }
  }

  const handleImport = async () => {
    if (!importYaml.trim()) {
      showError(t('workflow.importEmptyError'))
      return
    }
    try {
      setImporting(true)
      // project_id는 YAML에 있거나 여기서 지정(override). 지정 시 쿼리로 전달.
      const res = await api.importWorkflowYAML(importYaml, importProjectId.trim() || undefined)
      if (res.success && res.data?.id) {
        showSuccess(t('workflow.importSuccess'))
        setImportOpen(false)
        setImportYaml('')
        setImportProjectId('')
        navigate(`/workflows/${res.data.id}`)
      } else {
        showError(res.error?.message || t('workflow.importError'))
      }
    } catch (error: unknown) {
      const err = error as { response?: { data?: { error?: { message?: string } } } }
      showError(err.response?.data?.error?.message || t('workflow.importError'))
    } finally {
      setImporting(false)
    }
  }

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: t('workflow.name'),
      flex: 1,
      renderCell: (params: GridRenderCellParams<Workflow>) => (
        <Typography
          component="a"
          sx={{ cursor: 'pointer', color: 'primary.main', textDecoration: 'none' }}
          onClick={() => navigate(`/workflows/${params.row.id}`)}
        >
          {params.value}
        </Typography>
      ),
    },
    {
      field: 'project',
      headerName: t('workflow.project'),
      width: 150,
      valueGetter: (_value, row) => row.project?.name || "-",
      renderCell: (params: GridRenderCellParams<Workflow>) => (
        <Typography
          component="a"
          sx={{ cursor: 'pointer', color: 'primary.main', textDecoration: 'none' }}
          onClick={() => navigate(`/projects/${params.row.project?.alias || params.row.project_id}`)}
        >
          {params.row.project?.name || '-'}
        </Typography>
      ),
    },
    {
      field: 'type',
      headerName: t('workflow.type'),
      width: 120,
      renderCell: (params) => (
        <Chip
          label={params.value}
          color={params.value === 'realtime' ? 'primary' : 'warning'}
          size="small"
        />
      ),
    },
    {
      field: 'status',
      headerName: t('common.status'),
      width: 120,
      renderCell: (params) => {
        const colorMap: Record<string, 'success' | 'error' | 'warning' | 'default'> = {
          running: 'success',
          idle: 'default',
          stopped: 'default',
          error: 'error',
          paused: 'warning',
        }
        return (
          <Chip
            label={params.value || 'idle'}
            color={colorMap[params.value] || 'default'}
            size="small"
          />
        )
      },
    },
    {
      field: 'created_at',
      headerName: t('common.createdAt'),
      width: 120,
      valueFormatter: (value) => new Date(value).toLocaleDateString(),
    },
    {
      field: 'actions',
      headerName: t('common.actions'),
      width: 120,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Workflow>) => (
        <Box>
          <IconButton
            size="small"
            onClick={() => navigate(`/workflows/${params.row.id}`)}
          >
            <EyeIcon fontSize="small" />
          </IconButton>
          {params.row.status === 'running' ? (
            <IconButton
              size="small"
              onClick={(e) => handleStop(params.row.id, e)}
            >
              <PauseIcon fontSize="small" />
            </IconButton>
          ) : (
            <IconButton
              size="small"
              onClick={(e) => handleStart(params.row.id, e)}
            >
              <PlayIcon fontSize="small" />
            </IconButton>
          )}
        </Box>
      ),
    },
  ]

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5">{t('workflow.title')}</Typography>
        <Button variant="outlined" startIcon={<UploadIcon />} onClick={() => setImportOpen(true)}>
          {t('workflow.import')}
        </Button>
      </Box>

      <Dialog open={importOpen} onClose={() => setImportOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{t('workflow.import')}</DialogTitle>
        <DialogContent>
          <TextField
            label={t('workflow.importProjectIdLabel')}
            helperText={t('workflow.importProjectIdHelp')}
            value={importProjectId}
            onChange={(e) => setImportProjectId(e.target.value)}
            fullWidth
            margin="normal"
            size="small"
          />
          <TextField
            label="YAML"
            value={importYaml}
            onChange={(e) => setImportYaml(e.target.value)}
            fullWidth
            multiline
            minRows={12}
            margin="normal"
            slotProps={{ input: { style: { fontFamily: 'monospace', fontSize: 13 } } }}
            placeholder={'name: my-workflow\ntype: realtime\npipelines: [...]'}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setImportOpen(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleImport} disabled={importing}>
            {t('workflow.import')}
          </Button>
        </DialogActions>
      </Dialog>

      <Card>
        <CardContent>
          <DataGrid
            rows={workflows}
            columns={columns}
            loading={loading}
            autoHeight
            pageSizeOptions={[20, 50, 100]}
            initialState={{
              pagination: { paginationModel: { pageSize: 20 } },
            }}
            disableRowSelectionOnClick
          />
        </CardContent>
      </Card>
    </Box>
  )
}
