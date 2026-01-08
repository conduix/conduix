import { useEffect, useState } from 'react'
import { Row, Col, Card, Statistic, Table, Tag, Typography } from 'antd'
import {
  BranchesOutlined,
  PlayCircleOutlined,
  ClusterOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'

const { Title } = Typography

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
  const [recentWorkflows, setRecentWorkflows] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchData()
  }, [])

  const fetchData = async () => {
    try {
      setLoading(true)

      // Fetch workflow list
      const workflowsRes = await api.getWorkflows()
      if (workflowsRes.success) {
        // Backend returns array directly, not wrapped in { items: [...] }
        const data = workflowsRes.data
        const workflows = Array.isArray(data) ? data : (data.items || [])
        setWorkflowStats({
          total: workflows.length,
          running: workflows.filter((w: any) => w.status === 'running').length,
          stopped: workflows.filter((w: any) => w.status === 'stopped' || w.status === 'idle').length,
          failed: workflows.filter((w: any) => w.status === 'error').length,
        })
        setRecentWorkflows(workflows.slice(0, 5))
      }

      // Fetch agent list
      try {
        const agentsRes = await api.getAgents()
        if (agentsRes.success) {
          const agents = agentsRes.data || []
          setAgentStats({
            total: agents.length,
            online: agents.filter((a: any) => a.status === 'online').length,
            offline: agents.filter((a: any) => a.status === 'offline').length,
          })
        }
      } catch {
        // Agent API may not exist
      }
    } catch (error) {
      console.error('Failed to fetch dashboard data:', error)
    } finally {
      setLoading(false)
    }
  }

  const workflowColumns = [
    {
      title: t('workflow.name'),
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: t('workflow.type'),
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => (
        <Tag color={type === 'realtime' ? 'blue' : 'orange'}>{type}</Tag>
      ),
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => {
        const color =
          status === 'running'
            ? 'green'
            : status === 'error'
            ? 'red'
            : 'default'
        return <Tag color={color}>{status || 'idle'}</Tag>
      },
    },
    {
      title: t('common.createdAt'),
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => new Date(date).toLocaleDateString(),
    },
  ]

  return (
    <div>
      <Title level={4}>{t('dashboard.title')}</Title>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title={t('dashboard.totalWorkflows')}
              value={workflowStats.total}
              prefix={<BranchesOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title={t('dashboard.runningWorkflows')}
              value={workflowStats.running}
              prefix={<PlayCircleOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title={t('dashboard.onlineAgents')}
              value={agentStats.online}
              suffix={`/ ${agentStats.total}`}
              prefix={<ClusterOutlined />}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title={t('dashboard.throughput')}
              value={0}
              prefix={<ThunderboltOutlined />}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <Card title={t('dashboard.recentWorkflows')} loading={loading}>
            <Table
              dataSource={recentWorkflows}
              columns={workflowColumns}
              rowKey="id"
              pagination={false}
              size="small"
            />
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title={t('dashboard.systemStatus')}>
            <div style={{ padding: '20px 0', textAlign: 'center', color: '#999' }}>
              {t('dashboard.systemMonitorPlaceholder')}
            </div>
          </Card>
        </Col>
      </Row>
    </div>
  )
}
