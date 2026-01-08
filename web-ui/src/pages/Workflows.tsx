import { useEffect, useState } from 'react'
import { Table, Tag, Button, Space, Card, Typography } from 'antd'
import { EyeOutlined, PlayCircleOutlined, PauseCircleOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'

const { Title } = Typography

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
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchWorkflows()
  }, [])

  const fetchWorkflows = async () => {
    try {
      setLoading(true)
      const response = await api.getWorkflows()
      if (response.success) {
        // Backend returns array directly, not wrapped in { items: [...] }
        const data = response.data
        setWorkflows(Array.isArray(data) ? data : (data.items || []))
      }
    } catch (error) {
      console.error('Failed to fetch workflows:', error)
    } finally {
      setLoading(false)
    }
  }

  const handleStart = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await api.startWorkflow(id)
      fetchWorkflows()
    } catch (error) {
      console.error('Failed to start workflow:', error)
    }
  }

  const handleStop = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await api.stopWorkflow(id)
      fetchWorkflows()
    } catch (error) {
      console.error('Failed to stop workflow:', error)
    }
  }

  const columns = [
    {
      title: t('workflow.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: Workflow) => (
        <a onClick={() => navigate(`/workflows/${record.id}`)}>{name}</a>
      ),
    },
    {
      title: t('workflow.project'),
      dataIndex: ['project', 'name'],
      key: 'project',
      render: (name: string, record: Workflow) => (
        <a onClick={() => navigate(`/projects/${record.project?.alias || record.project_id}`)}>
          {name || '-'}
        </a>
      ),
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
        const colorMap: Record<string, string> = {
          running: 'green',
          idle: 'default',
          stopped: 'default',
          error: 'red',
          paused: 'orange',
        }
        return <Tag color={colorMap[status] || 'default'}>{status}</Tag>
      },
    },
    {
      title: t('common.createdAt'),
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => new Date(date).toLocaleDateString(),
    },
    {
      title: t('common.actions'),
      key: 'actions',
      render: (_: unknown, record: Workflow) => (
        <Space>
          <Button
            type="text"
            icon={<EyeOutlined />}
            onClick={() => navigate(`/workflows/${record.id}`)}
          />
          {record.status === 'running' ? (
            <Button
              type="text"
              icon={<PauseCircleOutlined />}
              onClick={(e) => handleStop(record.id, e)}
            />
          ) : (
            <Button
              type="text"
              icon={<PlayCircleOutlined />}
              onClick={(e) => handleStart(record.id, e)}
            />
          )}
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Title level={4} style={{ margin: 0 }}>{t('workflow.title')}</Title>
      </div>

      <Card>
        <Table
          dataSource={workflows}
          columns={columns}
          rowKey="id"
          loading={loading}
          pagination={{ pageSize: 20 }}
        />
      </Card>
    </div>
  )
}
