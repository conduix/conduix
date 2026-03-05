import { useEffect, useState, useCallback } from 'react'
import {
  Box,
  Typography,
  Card,
  CardContent,
  Grid,
  Chip,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  IconButton,
  Tooltip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  SelectChangeEvent,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Dns as ClusterIcon,
  Add as AddIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  CheckCircle as CheckCircleIcon,
  Cancel as CloseCircleIcon,
  Cloud as CloudIcon,
  Computer as DesktopIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import { useAuthStore } from '../store/auth'

interface Cluster {
  id: string
  name: string
  description: string
  api_server_url: string
  region: string
  status: string
  agent_count: number
  online_agent_count: number
  desired_agents: number
  agent_config?: string
  created_at: string
  updated_at: string
}

interface ClusterFormData {
  name: string
  description: string
  api_server_url: string
  region: string
  status: string
  desired_agents: number
  node_selector: string // JSON 문자열로 관리
}

const initialFormData: ClusterFormData = {
  name: '',
  description: '',
  api_server_url: '',
  region: '',
  status: 'active',
  desired_agents: 1,
  node_selector: '',
}

interface StatCardProps {
  title: string
  value: number
  icon: React.ReactNode
  color?: string
}

function StatCard({ title, value, icon, color }: StatCardProps) {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Box sx={{ color: color || 'text.secondary' }}>{icon}</Box>
          <Typography variant="body2" color="text.secondary">
            {title}
          </Typography>
        </Box>
        <Typography variant="h4" sx={{ color: color || 'text.primary' }}>
          {value}
        </Typography>
      </CardContent>
    </Card>
  )
}

export default function ClustersPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.user)
  const isAdmin = user?.role === 'admin'

  const [clusters, setClusters] = useState<Cluster[]>([])
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [editingCluster, setEditingCluster] = useState<Cluster | null>(null)
  const [deletingCluster, setDeletingCluster] = useState<Cluster | null>(null)
  const [formData, setFormData] = useState<ClusterFormData>(initialFormData)
  const [submitting, setSubmitting] = useState(false)
  const { showSuccess, showError } = useSnackbar()

  const fetchClusters = useCallback(async () => {
    try {
      const response = await api.getClusters()
      if (response.success) {
        setClusters(response.data || [])
      }
    } catch {
      showError(t('cluster.loadError'))
    } finally {
      setLoading(false)
    }
  }, [showError, t])

  useEffect(() => {
    fetchClusters()
  }, [fetchClusters])

  const handleOpenDialog = (cluster?: Cluster) => {
    if (cluster) {
      setEditingCluster(cluster)
      // agent_config에서 node_selector 추출
      let nodeSelector = ''
      if (cluster.agent_config) {
        try {
          const config = JSON.parse(cluster.agent_config)
          if (config.node_selector) {
            nodeSelector = JSON.stringify(config.node_selector, null, 2)
          }
        } catch {
          // ignore
        }
      }
      setFormData({
        name: cluster.name,
        description: cluster.description || '',
        api_server_url: cluster.api_server_url || '',
        region: cluster.region || '',
        status: cluster.status,
        desired_agents: cluster.desired_agents || 1,
        node_selector: nodeSelector,
      })
    } else {
      setEditingCluster(null)
      setFormData(initialFormData)
    }
    setDialogOpen(true)
  }

  const handleCloseDialog = () => {
    setDialogOpen(false)
    setEditingCluster(null)
    setFormData(initialFormData)
  }

  const handleSubmit = async () => {
    if (!formData.name.trim()) {
      showError(t('cluster.nameRequired'))
      return
    }

    // node_selector JSON 유효성 검사
    let nodeSelector: Record<string, string> | undefined
    if (formData.node_selector.trim()) {
      try {
        nodeSelector = JSON.parse(formData.node_selector)
      } catch {
        showError(t('cluster.invalidNodeSelector'))
        return
      }
    }

    setSubmitting(true)
    try {
      const baseData = {
        name: formData.name,
        description: formData.description,
        api_server_url: formData.api_server_url,
        region: formData.region,
        status: formData.status,
      }

      if (editingCluster) {
        // 클러스터 정보 업데이트
        await api.updateCluster(editingCluster.id, baseData)
        // Agent 설정 업데이트
        await api.updateClusterAgentConfig(editingCluster.id, {
          desired_agents: formData.desired_agents,
          agent_config: nodeSelector ? { node_selector: nodeSelector } : undefined,
        })
        showSuccess(t('cluster.updateSuccess'))
      } else {
        // 새 클러스터 생성
        const response = await api.createCluster(baseData)
        // 생성 후 Agent 설정 업데이트
        if (response.data?.id) {
          await api.updateClusterAgentConfig(response.data.id, {
            desired_agents: formData.desired_agents,
            agent_config: nodeSelector ? { node_selector: nodeSelector } : undefined,
          })
        }
        showSuccess(t('cluster.createSuccess'))
      }
      handleCloseDialog()
      fetchClusters()
    } catch {
      showError(editingCluster ? t('cluster.updateError') : t('cluster.createError'))
    } finally {
      setSubmitting(false)
    }
  }

  const handleOpenDeleteDialog = (cluster: Cluster) => {
    setDeletingCluster(cluster)
    setDeleteDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!deletingCluster) return

    setSubmitting(true)
    try {
      await api.deleteCluster(deletingCluster.id)
      showSuccess(t('cluster.deleteSuccess'))
      setDeleteDialogOpen(false)
      setDeletingCluster(null)
      fetchClusters()
    } catch {
      showError(t('cluster.deleteError'))
    } finally {
      setSubmitting(false)
    }
  }

  const handleStatusChange = (event: SelectChangeEvent) => {
    setFormData({ ...formData, status: event.target.value })
  }

  // 통계 계산
  const totalClusters = clusters.length
  const activeClusters = clusters.filter((c) => c.status === 'active').length
  const totalAgents = clusters.reduce((sum, c) => sum + c.agent_count, 0)
  const onlineAgents = clusters.reduce((sum, c) => sum + c.online_agent_count, 0)

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: t('cluster.name'),
      flex: 1,
      renderCell: (params: GridRenderCellParams<Cluster>) => (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <ClusterIcon fontSize="small" />
          <span>{params.value}</span>
        </Box>
      ),
    },
    {
      field: 'region',
      headerName: t('cluster.region'),
      width: 120,
      renderCell: (params) => params.value || '-',
    },
    {
      field: 'status',
      headerName: t('cluster.status'),
      width: 120,
      renderCell: (params) => {
        const isActive = params.value === 'active'
        return (
          <Chip
            icon={isActive ? <CheckCircleIcon /> : <CloseCircleIcon />}
            label={isActive ? t('cluster.active') : t('cluster.inactive')}
            color={isActive ? 'success' : 'default'}
            size="small"
          />
        )
      },
    },
    {
      field: 'agent_count',
      headerName: t('cluster.agents'),
      width: 180,
      renderCell: (params: GridRenderCellParams<Cluster>) => {
        const online = params.row.online_agent_count
        const current = params.row.agent_count
        const desired = params.row.desired_agents || 1
        return (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <DesktopIcon fontSize="small" color={online > 0 ? 'success' : 'disabled'} />
            <Typography variant="body2">
              {online}/{current}
              {current !== desired && (
                <Typography component="span" variant="body2" color="text.secondary">
                  {' '}(→{desired})
                </Typography>
              )}
            </Typography>
          </Box>
        )
      },
    },
    {
      field: 'description',
      headerName: t('cluster.description'),
      flex: 1,
      renderCell: (params) => (
        <Typography variant="body2" color="text.secondary" noWrap>
          {params.value || '-'}
        </Typography>
      ),
    },
    {
      field: 'actions',
      headerName: t('common.actions'),
      width: 120,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Cluster>) => (
        <Box>
          {isAdmin && (
            <>
              <Tooltip title={t('common.edit')}>
                <IconButton size="small" onClick={() => handleOpenDialog(params.row)}>
                  <EditIcon fontSize="small" />
                </IconButton>
              </Tooltip>
              <Tooltip title={t('common.delete')}>
                <IconButton
                  size="small"
                  color="error"
                  onClick={() => handleOpenDeleteDialog(params.row)}
                  disabled={params.row.agent_count > 0}
                >
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </>
          )}
        </Box>
      ),
    },
  ]

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <ClusterIcon />
          <Typography variant="h5">{t('cluster.title')}</Typography>
        </Box>
        {isAdmin && (
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={() => handleOpenDialog()}
          >
            {t('cluster.create')}
          </Button>
        )}
      </Box>

      <Grid container spacing={2} sx={{ mb: 2 }}>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('cluster.totalClusters')}
            value={totalClusters}
            icon={<ClusterIcon />}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('cluster.activeClusters')}
            value={activeClusters}
            icon={<CheckCircleIcon />}
            color="#4caf50"
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('cluster.totalAgents')}
            value={totalAgents}
            icon={<CloudIcon />}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('cluster.onlineAgents')}
            value={onlineAgents}
            icon={<DesktopIcon />}
            color={onlineAgents > 0 ? '#4caf50' : undefined}
          />
        </Grid>
      </Grid>

      <DataGrid
        rows={clusters}
        columns={columns}
        loading={loading}
        autoHeight
        disableRowSelectionOnClick
      />

      {/* 생성/수정 다이얼로그 */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingCluster ? t('cluster.edit') : t('cluster.create')}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label={t('cluster.name')}
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              fullWidth
              required
            />
            <TextField
              label={t('cluster.description')}
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              fullWidth
              multiline
              rows={2}
            />
            <TextField
              label={t('cluster.apiServerUrl')}
              value={formData.api_server_url}
              onChange={(e) => setFormData({ ...formData, api_server_url: e.target.value })}
              fullWidth
              placeholder="https://k8s-api.example.com:6443"
            />
            <TextField
              label={t('cluster.region')}
              value={formData.region}
              onChange={(e) => setFormData({ ...formData, region: e.target.value })}
              fullWidth
              placeholder="kr-central-1"
            />
            {editingCluster && (
              <FormControl fullWidth>
                <InputLabel>{t('cluster.status')}</InputLabel>
                <Select
                  value={formData.status}
                  label={t('cluster.status')}
                  onChange={handleStatusChange}
                >
                  <MenuItem value="active">{t('cluster.active')}</MenuItem>
                  <MenuItem value="inactive">{t('cluster.inactive')}</MenuItem>
                </Select>
              </FormControl>
            )}

            {/* Agent 배포 설정 */}
            <Typography variant="subtitle2" sx={{ mt: 2 }}>
              {t('cluster.agentSettings')}
            </Typography>
            <TextField
              label={t('cluster.desiredAgents')}
              type="number"
              value={formData.desired_agents}
              onChange={(e) => setFormData({ ...formData, desired_agents: parseInt(e.target.value) || 1 })}
              fullWidth
              inputProps={{ min: 0, max: 100 }}
              helperText={t('cluster.desiredAgentsHelp')}
            />
            <TextField
              label={t('cluster.nodeSelector')}
              value={formData.node_selector}
              onChange={(e) => setFormData({ ...formData, node_selector: e.target.value })}
              fullWidth
              multiline
              rows={3}
              placeholder='{"node-type": "pipeline", "zone": "a"}'
              helperText={t('cluster.nodeSelectorHelp')}
              InputProps={{
                style: { fontFamily: 'monospace', fontSize: '0.875rem' },
              }}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>{t('common.cancel')}</Button>
          <Button onClick={handleSubmit} variant="contained" disabled={submitting}>
            {editingCluster ? t('common.save') : t('common.create')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 삭제 확인 다이얼로그 */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>{t('cluster.deleteConfirm')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('cluster.deleteMessage', { name: deletingCluster?.name })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>{t('common.cancel')}</Button>
          <Button onClick={handleDelete} color="error" variant="contained" disabled={submitting}>
            {t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
