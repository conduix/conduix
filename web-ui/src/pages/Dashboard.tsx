import { useEffect, useState } from 'react'
import {
  Grid,
  Card,
  CardContent,
  Typography,
  Box,
  Chip,
  Skeleton,
} from '@mui/material'
import { DataGrid, GridColDef } from '@mui/x-data-grid'
import {
  AccountTree as BranchesIcon,
  PlayCircle as PlayCircleIcon,
  Dns as ClusterIcon,
  Bolt as BoltIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'

interface Workflow {
  id: string
  name: string
  status: string
  type: 'batch' | 'realtime'
  updated_at: string
  project?: { name: string }
}

interface WorkflowStats {
  total: number
  running: number
  stopped: number
  failed: number
}

interface AgentStats {
  total: number
  online: number
  offline: number
}

interface StatCardProps {
  title: string
  value: number
  suffix?: string
  icon: React.ReactNode
  color?: string
  loading?: boolean
}

function StatCard({ title, value, suffix, icon, color, loading }: StatCardProps) {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Box sx={{ color: color || 'text.secondary' }}>{icon}</Box>
          <Typography variant="body2" color="text.secondary">
            {title}
          </Typography>
        </Box>
        {loading ? (
          <Skeleton width={80} height={40} />
        ) : (
          <Typography variant="h4" sx={{ color: color || 'text.primary' }}>
            {value}
            {suffix && (
              <Typography component="span" variant="body1" color="text.secondary">
                {suffix}
              </Typography>
            )}
          </Typography>
        )}
      </CardContent>
    </Card>
  )
}

export default function DashboardPage() {
  const { t } = useTranslation()
  const [workflowStats, setWorkflowStats] = useState<WorkflowStats>({
    total: 0,
    running: 0,
    stopped: 0,
    failed: 0,
  })
  const [agentStats, setAgentStats] = useState<AgentStats>({
    total: 0,
    online: 0,
    offline: 0,
  })
  const [recentWorkflows, setRecentWorkflows] = useState<Workflow[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchData()
  }, [])

  const fetchData = async () => {
    try {
      setLoading(true)

      const workflowsRes = await api.getWorkflows()
      if (workflowsRes.success) {
        const data = workflowsRes.data
        const workflows: Workflow[] = Array.isArray(data) ? data : (data.items || [])
        setWorkflowStats({
          total: workflows.length,
          running: workflows.filter((w) => w.status === 'running').length,
          stopped: workflows.filter((w) => w.status === 'stopped' || w.status === 'idle').length,
          failed: workflows.filter((w) => w.status === 'error').length,
        })
        setRecentWorkflows(workflows.slice(0, 5))
      }

      try {
        const agentsRes = await api.getAgents()
        if (agentsRes.success) {
          const agents: Array<{ status: string }> = agentsRes.data || []
          setAgentStats({
            total: agents.length,
            online: agents.filter((a) => a.status === 'online').length,
            offline: agents.filter((a) => a.status === 'offline').length,
          })
        }
      } catch {
        // Agent API may not exist
      }
    } catch {
      // Failed to fetch dashboard data
    } finally {
      setLoading(false)
    }
  }

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: t('workflow.name'),
      flex: 1,
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
        const status = params.value || 'idle'
        const colorMap: Record<string, 'success' | 'error' | 'default'> = {
          running: 'success',
          error: 'error',
        }
        return (
          <Chip
            label={status}
            color={colorMap[status] || 'default'}
            size="small"
          />
        )
      },
    },
    {
      field: 'created_at',
      headerName: t('common.createdAt'),
      width: 120,
      valueFormatter: ({ value }) => new Date(value).toLocaleDateString(),
    },
  ]

  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 3 }}>
        {t('dashboard.title')}
      </Typography>

      <Grid container spacing={2}>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title={t('dashboard.totalWorkflows')}
            value={workflowStats.total}
            icon={<BranchesIcon />}
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title={t('dashboard.runningWorkflows')}
            value={workflowStats.running}
            icon={<PlayCircleIcon />}
            color="#4caf50"
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title={t('dashboard.onlineAgents')}
            value={agentStats.online}
            suffix={` / ${agentStats.total}`}
            icon={<ClusterIcon />}
            color="#2196f3"
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title={t('dashboard.throughput')}
            value={0}
            icon={<BoltIcon />}
            loading={loading}
          />
        </Grid>
      </Grid>

      <Grid container spacing={2} sx={{ mt: 2 }}>
        <Grid item xs={12} lg={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>
                {t('dashboard.recentWorkflows')}
              </Typography>
              <DataGrid
                rows={recentWorkflows}
                columns={columns}
                loading={loading}
                autoHeight
                hideFooter
                disableRowSelectionOnClick
                sx={{ border: 0 }}
              />
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} lg={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>
                {t('dashboard.systemStatus')}
              </Typography>
              <Box sx={{ py: 4, textAlign: 'center', color: 'text.secondary' }}>
                {t('dashboard.systemMonitorPlaceholder')}
              </Box>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  )
}
