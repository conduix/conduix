import { useEffect, useState } from 'react'
import {
  Table,
  Tag,
  Typography,
  Card,
  Row,
  Col,
  Statistic,
  Progress,
  Space,
  Badge,
  Tooltip,
  Collapse,
  List,
  message,
} from 'antd'
import {
  ClusterOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  DesktopOutlined,
  HddOutlined,
  CloudServerOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api } from '../services/api'

const { Title, Text } = Typography

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
  cpu_usage: number
  memory_usage: number
  disk_usage: number
  pipelines: string[]
  pipeline_stats: PipelineStat[]
  running_execs: RunningExecution[]
  uptime: string
}

export default function AgentsPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchAgents()
    // 30초마다 자동 갱신
    const interval = setInterval(fetchAgents, 30000)
    return () => clearInterval(interval)
  }, [])

  const fetchAgents = async () => {
    try {
      const response = await api.getAgents()
      if (response.success) {
        setAgents(response.data || [])
      }
    } catch (error) {
      message.error(t('agent.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'online':
        return <Badge status="success" text={t('agent.online')} />
      case 'offline':
        return <Badge status="error" text={t('agent.offline')} />
      default:
        return <Badge status="default" text={t('agent.unknown')} />
    }
  }

  const getProgressColor = (value: number) => {
    if (value >= 90) return '#ff4d4f'
    if (value >= 70) return '#faad14'
    return '#52c41a'
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

  const columns = [
    {
      title: t('agent.hostname'),
      dataIndex: 'hostname',
      key: 'hostname',
      render: (hostname: string, record: Agent) => (
        <Space>
          <DesktopOutlined />
          <span>{hostname}</span>
          {record.version && (
            <Tag color="blue" style={{ fontSize: 10 }}>v{record.version}</Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('agent.status'),
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (status: string) => getStatusBadge(status),
    },
    {
      title: t('agent.resources'),
      key: 'resources',
      width: 300,
      render: (_: unknown, record: Agent) => (
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Tooltip title="CPU">
              <Text type="secondary" style={{ width: 60 }}>CPU</Text>
            </Tooltip>
            <Progress
              percent={Math.round(record.cpu_usage || 0)}
              size="small"
              strokeColor={getProgressColor(record.cpu_usage || 0)}
              style={{ flex: 1, margin: 0 }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Tooltip title="Memory">
              <Text type="secondary" style={{ width: 60 }}>MEM</Text>
            </Tooltip>
            <Progress
              percent={Math.round(record.memory_usage || 0)}
              size="small"
              strokeColor={getProgressColor(record.memory_usage || 0)}
              style={{ flex: 1, margin: 0 }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Tooltip title="Disk">
              <Text type="secondary" style={{ width: 60 }}>DISK</Text>
            </Tooltip>
            <Progress
              percent={Math.round(record.disk_usage || 0)}
              size="small"
              strokeColor={getProgressColor(record.disk_usage || 0)}
              style={{ flex: 1, margin: 0 }}
            />
          </div>
        </Space>
      ),
    },
    {
      title: t('agent.runningWorkflows'),
      key: 'running',
      width: 150,
      render: (_: unknown, record: Agent) => {
        const count = record.running_execs?.length || 0
        return (
          <Tag color={count > 0 ? 'processing' : 'default'}>
            {count} {t('agent.workflows')}
          </Tag>
        )
      },
    },
    {
      title: t('agent.uptime'),
      dataIndex: 'uptime',
      key: 'uptime',
      width: 100,
      render: (uptime: string) => uptime || '-',
    },
    {
      title: t('agent.lastHeartbeat'),
      dataIndex: 'last_heartbeat',
      key: 'last_heartbeat',
      width: 150,
      render: (heartbeat: string | null) => formatLastHeartbeat(heartbeat),
    },
  ]

  const expandedRowRender = (record: Agent) => {
    return (
      <div style={{ padding: '0 16px' }}>
        <Collapse ghost>
          {record.running_execs && record.running_execs.length > 0 && (
            <Collapse.Panel header={t('agent.runningExecutions')} key="executions">
              <List
                size="small"
                dataSource={record.running_execs}
                renderItem={(exec) => (
                  <List.Item>
                    <Space>
                      <Tag color="processing">{t('agent.running')}</Tag>
                      <a onClick={() => navigate(`/workflows/${exec.workflow_id}`)}>
                        Workflow: {exec.workflow_id.substring(0, 8)}...
                      </a>
                      <Text type="secondary">
                        {t('agent.startedAt')}: {new Date(exec.started_at).toLocaleString()}
                      </Text>
                    </Space>
                  </List.Item>
                )}
              />
            </Collapse.Panel>
          )}
          {record.pipeline_stats && record.pipeline_stats.length > 0 && (
            <Collapse.Panel header={t('agent.pipelineStats')} key="pipelines">
              <List
                size="small"
                dataSource={record.pipeline_stats}
                renderItem={(stat) => (
                  <List.Item>
                    <Space>
                      <Tag color={stat.status === 'running' ? 'processing' : 'default'}>
                        {stat.status}
                      </Tag>
                      <Text>Pipeline: {stat.pipeline_id.substring(0, 8)}...</Text>
                      <Text type="secondary">
                        {t('agent.processed')}: {stat.processed_count.toLocaleString()}
                      </Text>
                      {stat.error_count > 0 && (
                        <Text type="danger">
                          {t('agent.errors')}: {stat.error_count}
                        </Text>
                      )}
                    </Space>
                  </List.Item>
                )}
              />
            </Collapse.Panel>
          )}
          <Collapse.Panel header={t('agent.details')} key="details">
            <Space direction="vertical">
              <Text><strong>ID:</strong> {record.id}</Text>
              <Text><strong>IP:</strong> {record.ip_address || '-'}</Text>
              <Text><strong>{t('agent.registeredAt')}:</strong> {new Date(record.registered_at).toLocaleString()}</Text>
            </Space>
          </Collapse.Panel>
        </Collapse>
      </div>
    )
  }

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Title level={4} style={{ margin: 0 }}>
          <ClusterOutlined style={{ marginRight: 8 }} />
          {t('agent.title')}
        </Title>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card size="small">
            <Statistic
              title={t('agent.totalAgents')}
              value={agents.length}
              prefix={<CloudServerOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic
              title={t('agent.onlineAgents')}
              value={onlineCount}
              prefix={<CheckCircleOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic
              title={t('agent.offlineAgents')}
              value={offlineCount}
              prefix={<CloseCircleOutlined />}
              valueStyle={{ color: offlineCount > 0 ? '#ff4d4f' : undefined }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic
              title={t('agent.runningWorkflows')}
              value={totalRunningWorkflows}
              prefix={<HddOutlined />}
            />
          </Card>
        </Col>
      </Row>

      <Table
        dataSource={agents}
        columns={columns}
        rowKey="id"
        loading={loading}
        expandable={{
          expandedRowRender,
          rowExpandable: (record) =>
            (record.running_execs?.length > 0) ||
            (record.pipeline_stats?.length > 0) ||
            true,
        }}
        pagination={false}
      />
    </div>
  )
}
