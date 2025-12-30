import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Table,
  Button,
  Space,
  Tag,
  Modal,
  Form,
  Input,
  message,
  Popconfirm,
  Typography,
} from 'antd'
import {
  PlusOutlined,
  DeleteOutlined,
  EditOutlined,
} from '@ant-design/icons'
import { api } from '../services/api'

const { Title } = Typography
const { TextArea } = Input

interface Pipeline {
  id: string
  name: string
  description: string
  config_yaml: string
  created_at: string
  updated_at: string
  status?: string
}

const defaultConfigYaml = `version: "1.0"
name: "new-pipeline"
type: flat

sources:
  demo_source:
    type: demo
    interval: 1s

transforms:
  parse:
    type: remap
    inputs:
      - demo_source

sinks:
  console:
    type: console
    inputs:
      - parse

checkpoint:
  enabled: true
  storage: redis
  interval: 10s
`

export default function PipelinesPage() {
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [loading, setLoading] = useState(true)
  const [modalVisible, setModalVisible] = useState(false)
  const [editingPipeline, setEditingPipeline] = useState<Pipeline | null>(null)
  const [form] = Form.useForm()
  const navigate = useNavigate()

  useEffect(() => {
    fetchPipelines()
  }, [])

  const fetchPipelines = async () => {
    try {
      setLoading(true)
      const response = await api.getPipelines()
      if (response.success) {
        setPipelines(response.data.items || [])
      }
    } catch (error) {
      message.error('파이프라인 목록을 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }

  const handleCreate = () => {
    setEditingPipeline(null)
    form.resetFields()
    form.setFieldsValue({ config_yaml: defaultConfigYaml })
    setModalVisible(true)
  }

  const handleEdit = (pipeline: Pipeline) => {
    setEditingPipeline(pipeline)
    form.setFieldsValue(pipeline)
    setModalVisible(true)
  }

  const handleDelete = async (id: string) => {
    try {
      await api.deletePipeline(id)
      message.success('파이프라인이 삭제되었습니다')
      fetchPipelines()
    } catch (error) {
      message.error('파이프라인 삭제에 실패했습니다')
    }
  }

  // NOTE: 개별 파이프라인 실행 제어(start/stop/pause)는 지원하지 않음
  // 파이프라인 실행 제어는 PipelineGroup 단위로만 가능

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields()

      if (editingPipeline) {
        await api.updatePipeline(editingPipeline.id, values)
        message.success('파이프라인이 수정되었습니다')
      } else {
        await api.createPipeline(values)
        message.success('파이프라인이 생성되었습니다')
      }

      setModalVisible(false)
      fetchPipelines()
    } catch (error) {
      message.error('저장에 실패했습니다')
    }
  }

  const columns = [
    {
      title: '이름',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: Pipeline) => (
        <a onClick={() => navigate(`/pipelines/${record.id}`)}>{name}</a>
      ),
    },
    {
      title: '설명',
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
    },
    {
      title: '상태',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => {
        const statusConfig: Record<string, { color: string; text: string }> = {
          running: { color: 'green', text: '실행 중' },
          paused: { color: 'orange', text: '일시중지' },
          stopped: { color: 'default', text: '중지됨' },
          failed: { color: 'red', text: '실패' },
          pending: { color: 'blue', text: '대기 중' },
        }
        const config = statusConfig[status || 'stopped'] || statusConfig.stopped
        return <Tag color={config.color}>{config.text}</Tag>
      },
    },
    {
      title: '생성일',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => new Date(date).toLocaleDateString(),
    },
    {
      title: '액션',
      key: 'action',
      render: (_: any, record: Pipeline) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
            title="수정"
          />
          <Popconfirm
            title="정말 삭제하시겠습니까?"
            onConfirm={() => handleDelete(record.id)}
            okText="삭제"
            cancelText="취소"
          >
            <Button type="text" danger icon={<DeleteOutlined />} title="삭제" />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Title level={4} style={{ margin: 0 }}>파이프라인</Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>
          새 파이프라인
        </Button>
      </div>

      <Table
        dataSource={pipelines}
        columns={columns}
        rowKey="id"
        loading={loading}
      />

      <Modal
        title={editingPipeline ? '파이프라인 수정' : '새 파이프라인'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={800}
        okText="저장"
        cancelText="취소"
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label="이름"
            rules={[{ required: true, message: '이름을 입력하세요' }]}
          >
            <Input />
          </Form.Item>

          <Form.Item name="description" label="설명">
            <Input.TextArea rows={2} />
          </Form.Item>

          <Form.Item
            name="config_yaml"
            label="설정 (YAML)"
            rules={[{ required: true, message: 'YAML 설정을 입력하세요' }]}
          >
            <TextArea
              rows={15}
              style={{ fontFamily: 'monospace' }}
              placeholder="파이프라인 설정을 YAML 형식으로 입력하세요"
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
