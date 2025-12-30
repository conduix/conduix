import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, Descriptions, Button, Space, Tag, Table, Tabs, message, Typography, Spin } from 'antd'
import { ArrowLeftOutlined, ApartmentOutlined } from '@ant-design/icons'
import { api } from '../services/api'
import { PipelineGraph } from '../components/PipelineGraph'

const { Title } = Typography

export default function PipelineDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [pipeline, setPipeline] = useState<any>(null)
  const [history, setHistory] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (id) {
      fetchPipeline()
      fetchHistory()
    }
  }, [id])

  const fetchPipeline = async () => {
    try {
      const response = await api.getPipeline(id!)
      if (response.success) {
        setPipeline(response.data)
      }
    } catch (error) {
      message.error('파이프라인 정보를 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }

  const fetchHistory = async () => {
    try {
      const response = await api.getPipelineHistory(id!)
      if (response.success) {
        setHistory(response.data || [])
      }
    } catch (error) {
      // 히스토리가 없을 수 있음
    }
  }

  // NOTE: 개별 파이프라인 실행 제어(start/stop/pause)는 지원하지 않음
  // 파이프라인 실행 제어는 PipelineGroup 단위로만 가능

  const historyColumns = [
    {
      title: '실행 ID',
      dataIndex: 'id',
      key: 'id',
      ellipsis: true,
    },
    {
      title: '상태',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => {
        const colors: Record<string, string> = {
          running: 'green',
          completed: 'blue',
          failed: 'red',
          paused: 'orange',
        }
        return <Tag color={colors[status] || 'default'}>{status}</Tag>
      },
    },
    {
      title: '처리량',
      dataIndex: 'processed_count',
      key: 'processed_count',
      render: (count: number) => count?.toLocaleString() || 0,
    },
    {
      title: '에러',
      dataIndex: 'error_count',
      key: 'error_count',
      render: (count: number) => count?.toLocaleString() || 0,
    },
    {
      title: '시작 시간',
      dataIndex: 'started_at',
      key: 'started_at',
      render: (date: string) => date ? new Date(date).toLocaleString() : '-',
    },
  ]

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 50 }}>
        <Spin size="large" />
      </div>
    )
  }

  if (!pipeline) {
    return <div>파이프라인을 찾을 수 없습니다.</div>
  }

  const tabItems = [
    {
      key: 'info',
      label: '정보',
      children: (
        <Descriptions bordered column={2}>
          <Descriptions.Item label="ID">{pipeline.id}</Descriptions.Item>
          <Descriptions.Item label="이름">{pipeline.name}</Descriptions.Item>
          <Descriptions.Item label="설명" span={2}>
            {pipeline.description || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="생성일">
            {new Date(pipeline.created_at).toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="수정일">
            {new Date(pipeline.updated_at).toLocaleString()}
          </Descriptions.Item>
        </Descriptions>
      ),
    },
    {
      key: 'config',
      label: '설정',
      children: (
        <pre
          style={{
            background: '#f5f5f5',
            padding: 16,
            borderRadius: 8,
            overflow: 'auto',
            maxHeight: 500,
          }}
        >
          {pipeline.config_yaml}
        </pre>
      ),
    },
    {
      key: 'history',
      label: '실행 히스토리',
      children: (
        <Table dataSource={history} columns={historyColumns} rowKey="id" />
      ),
    },
    {
      key: 'graph',
      label: (
        <span>
          <ApartmentOutlined /> Pipeline Graph
        </span>
      ),
      children: (
        <PipelineGraph
          pipelineId={id!}
          readonly={false}
          pollingInterval={5000}
        />
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/pipelines')}>
            목록
          </Button>
          <Title level={4} style={{ margin: 0 }}>{pipeline.name}</Title>
        </Space>
      </div>

      <Card>
        <Tabs items={tabItems} />
      </Card>
    </div>
  )
}
