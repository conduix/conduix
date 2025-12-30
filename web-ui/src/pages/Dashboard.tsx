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

interface PipelineStats {
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
  const [pipelineStats, setPipelineStats] = useState<PipelineStats>({
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
  const [recentPipelines, setRecentPipelines] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchData()
  }, [])

  const fetchData = async () => {
    try {
      setLoading(true)

      // Fetch pipeline list
      const pipelinesRes = await api.getPipelines()
      if (pipelinesRes.success) {
        const pipelines = pipelinesRes.data.items || []
        setPipelineStats({
          total: pipelinesRes.data.total || 0,
          running: pipelines.filter((p: any) => p.status === 'running').length,
          stopped: pipelines.filter((p: any) => p.status === 'stopped').length,
          failed: pipelines.filter((p: any) => p.status === 'failed').length,
        })
        setRecentPipelines(pipelines.slice(0, 5))
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

  const pipelineColumns = [
    {
      title: t('pipeline.name'),
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => {
        const color =
          status === 'running'
            ? 'green'
            : status === 'failed'
            ? 'red'
            : 'default'
        return <Tag color={color}>{status || 'stopped'}</Tag>
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
              title={t('dashboard.totalPipelines')}
              value={pipelineStats.total}
              prefix={<BranchesOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title={t('dashboard.runningPipelines')}
              value={pipelineStats.running}
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
          <Card title={t('dashboard.recentPipelines')} loading={loading}>
            <Table
              dataSource={recentPipelines}
              columns={pipelineColumns}
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
