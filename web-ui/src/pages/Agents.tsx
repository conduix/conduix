import { useEffect, useState, useCallback } from 'react'
import {
  Box,
  Typography,
  Card,
  CardContent,
  Grid,
  Chip,
  LinearProgress,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  SelectChangeEvent,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Dns as ClusterIcon,
  CheckCircle as CheckCircleIcon,
  Cancel as CloseCircleIcon,
  Computer as DesktopIcon,
  Storage as HddIcon,
  Cloud as CloudIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'

interface Cluster {
  id: string
  name: string
  description: string
  status: string
  agent_count: number
  online_agent_count: number
}

interface PipelineStat {
  pipeline_id: string
  status: string
  processed_count: number
  error_count: number
}

interface RunningExecution {
  execution_id: string
  workflow_id: string
  started_at: string
}

interface Agent {
  id: string
  hostname: string
  ip_address: string
  status: string
  last_heartbeat: string | null
  registered_at: string
  version: string
  labels: string
  cluster_id: string
  cluster_name: string
  cpu_usage: number
  memory_usage: number
  disk_usage: number
  pipelines: string[]
  pipeline_stats: PipelineStat[]
  running_execs: RunningExecution[]
  uptime: string
}

function ResourceProgress({ label, value }: { label: string; value: number }) {
  const getColor = (v: number): 'success' | 'warning' | 'error' => {
    if (v >= 90) return 'error'
    if (v >= 70) return 'warning'
    return 'success'
  }

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <Typography variant="caption" color="text.secondary" sx={{ width: 40 }}>
        {label}
      </Typography>
      <Box sx={{ flex: 1 }}>
        <LinearProgress
          variant="determinate"
          value={Math.round(value || 0)}
          color={getColor(value || 0)}
          sx={{ height: 8, borderRadius: 4 }}
        />
      </Box>
      <Typography variant="caption" sx={{ width: 40, textAlign: 'right' }}>
        {Math.round(value || 0)}%
      </Typography>
    </Box>
  )
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

export default function AgentsPage() {
  const { t } = useTranslation()
  const [agents, setAgents] = useState<Agent[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [selectedCluster, setSelectedCluster] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const { showError } = useSnackbar()

  const fetchClusters = useCallback(async () => {
    try {
      const response = await api.getClusters()
      if (response.success) {
        setClusters(response.data || [])
      }
    } catch {
      // 클러스터 로드 실패는 무시 (필터 없이 동작)
    }
  }, [])

  const fetchAgents = useCallback(async () => {
    try {
      const params = selectedCluster ? { cluster_id: selectedCluster } : undefined
      const response = await api.getAgents(params)
      if (response.success) {
        setAgents(response.data || [])
      }
    } catch {
      showError(t('agent.loadError'))
    } finally {
      setLoading(false)
    }
  }, [showError, t, selectedCluster])

  useEffect(() => {
    fetchClusters()
  }, [fetchClusters])

  useEffect(() => {
    fetchAgents()
    const interval = setInterval(fetchAgents, 30000)
    return () => clearInterval(interval)
  }, [fetchAgents])

  const handleClusterChange = (event: SelectChangeEvent) => {
    setSelectedCluster(event.target.value)
    setLoading(true)
  }

  const formatLastHeartbeat = (heartbeat: string | null) => {
    if (!heartbeat) return '-'
    const date = new Date(heartbeat)
    const now = new Date()
    const diffSeconds = Math.floor((now.getTime() - date.getTime()) / 1000)

    if (diffSeconds < 60) return `${diffSeconds}${t('agent.secondsAgo')}`
    if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}${t('agent.minutesAgo')}`
    return date.toLocaleString()
  }

  const onlineCount = agents.filter(a => a.status === 'online').length
  const offlineCount = agents.filter(a => a.status === 'offline').length
  const totalRunningWorkflows = agents.reduce((sum, a) => sum + (a.running_execs?.length || 0), 0)

  const columns: GridColDef[] = [
    {
      field: 'hostname',
      headerName: t('agent.hostname'),
      flex: 1,
      renderCell: (params: GridRenderCellParams<Agent>) => (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <DesktopIcon fontSize="small" />
          <span>{params.value}</span>
          {params.row.version && (
            <Chip label={`v${params.row.version}`} size="small" color="primary" variant="outlined" />
          )}
        </Box>
      ),
    },
    {
      field: 'cluster_name',
      headerName: t('agent.cluster'),
      width: 150,
      renderCell: (params: GridRenderCellParams<Agent>) => {
        if (!params.row.cluster_name) {
          return <Typography variant="body2" color="text.secondary">-</Typography>
        }
        return (
          <Chip
            icon={<ClusterIcon />}
            label={params.row.cluster_name}
            size="small"
            variant="outlined"
          />
        )
      },
    },
    {
      field: 'status',
      headerName: t('agent.status'),
      width: 120,
      renderCell: (params) => {
        const isOnline = params.value === 'online'
        return (
          <Chip
            icon={isOnline ? <CheckCircleIcon /> : <CloseCircleIcon />}
            label={isOnline ? t('agent.online') : t('agent.offline')}
            color={isOnline ? 'success' : 'error'}
            size="small"
          />
        )
      },
    },
    {
      field: 'resources',
      headerName: t('agent.resources'),
      width: 250,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Agent>) => (
        <Box sx={{ width: '100%', py: 1 }}>
          <ResourceProgress label="CPU" value={params.row.cpu_usage} />
          <ResourceProgress label="MEM" value={params.row.memory_usage} />
          <ResourceProgress label="DISK" value={params.row.disk_usage} />
        </Box>
      ),
    },
    {
      field: 'running',
      headerName: t('agent.runningWorkflows'),
      width: 150,
      valueGetter: (params) => params.row.running_execs?.length || 0,
      renderCell: (params: GridRenderCellParams<Agent>) => {
        const count = params.row.running_execs?.length || 0
        return (
          <Chip
            label={`${count} ${t('agent.workflows')}`}
            color={count > 0 ? 'primary' : 'default'}
            size="small"
          />
        )
      },
    },
    {
      field: 'uptime',
      headerName: t('agent.uptime'),
      width: 100,
      valueFormatter: ({ value }) => value || '-',
    },
    {
      field: 'last_heartbeat',
      headerName: t('agent.lastHeartbeat'),
      width: 150,
      valueFormatter: ({ value }) => formatLastHeartbeat(value),
    },
  ]

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <DesktopIcon />
          <Typography variant="h5">{t('agent.title')}</Typography>
        </Box>
        <FormControl size="small" sx={{ minWidth: 200 }}>
          <InputLabel>{t('agent.filterByCluster')}</InputLabel>
          <Select
            value={selectedCluster}
            label={t('agent.filterByCluster')}
            onChange={handleClusterChange}
          >
            <MenuItem value="">{t('agent.allClusters')}</MenuItem>
            {clusters.map((cluster) => (
              <MenuItem key={cluster.id} value={cluster.id}>
                {cluster.name} ({cluster.online_agent_count}/{cluster.agent_count})
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      <Grid container spacing={2} sx={{ mb: 2 }}>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('agent.totalAgents')}
            value={agents.length}
            icon={<CloudIcon />}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('agent.onlineAgents')}
            value={onlineCount}
            icon={<CheckCircleIcon />}
            color="#4caf50"
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('agent.offlineAgents')}
            value={offlineCount}
            icon={<CloseCircleIcon />}
            color={offlineCount > 0 ? '#f44336' : undefined}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title={t('agent.runningWorkflows')}
            value={totalRunningWorkflows}
            icon={<HddIcon />}
          />
        </Grid>
      </Grid>

      <DataGrid
        rows={agents}
        columns={columns}
        loading={loading}
        autoHeight
        getRowHeight={() => 'auto'}
        disableRowSelectionOnClick
        sx={{
          '& .MuiDataGrid-cell': {
            py: 1,
          },
        }}
      />
    </Box>
  )
}
