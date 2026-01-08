import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Table,
  Button,
  Space,
  Tag,
  message,
  Popconfirm,
  Typography,
  Card,
  Row,
  Col,
  Statistic,
  Select,
  Badge,
  Input,
} from 'antd'
import {
  DeleteOutlined,
  EyeOutlined,
  DatabaseOutlined,
  AppstoreOutlined,
  CheckCircleOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'

const { Title } = Typography
const { Search } = Input

interface DataType {
  id: string
  project_id: string
  parent_id?: string | null
  name: string
  display_name: string
  description?: string
  category?: string
  id_fields?: string
  json_schema?: string
  schema?: {
    type?: string
    definition?: string
    fields?: Array<{ name: string; type: string; required?: boolean; description?: string }>
  }
  created_at: string
  updated_at: string
  project?: {
    id: string
    name: string
    alias: string
  }
  parent?: DataType
}

interface Project {
  id: string
  name: string
  alias: string
}

export default function DataModelsPage() {
  const { t } = useTranslation()
  const [dataModels, setDataModels] = useState<DataType[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 })
  const [filters, setFilters] = useState<{
    project_id?: string
    category?: string
    search?: string
  }>({})
  const navigate = useNavigate()

  useEffect(() => {
    fetchDataModels()
    fetchProjects()
  }, [pagination.current, pagination.pageSize, filters])

  const fetchDataModels = async () => {
    try {
      setLoading(true)
      const response = await api.getDataTypes({
        project_id: filters.project_id,
        category: filters.category,
      })
      if (response.success) {
        let items = response.data || []
        // 검색어 필터링 (클라이언트 사이드)
        if (filters.search) {
          const searchLower = filters.search.toLowerCase()
          items = items.filter((item: DataType) =>
            item.name.toLowerCase().includes(searchLower) ||
            item.display_name.toLowerCase().includes(searchLower) ||
            (item.description || '').toLowerCase().includes(searchLower)
          )
        }
        setDataModels(items)
        setPagination(prev => ({ ...prev, total: items.length }))
      }
    } catch (error) {
      message.error(t('dataModel.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const fetchProjects = async () => {
    try {
      const response = await api.getProjects({ page: 1, page_size: 100 })
      if (response.success) {
        setProjects(response.data?.projects || [])
      }
    } catch (error) {
      console.error('Failed to load projects:', error)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      const response = await api.deleteDataType(id)
      if (response.success) {
        message.success(t('dataModel.deleteSuccess'))
        fetchDataModels()
      } else {
        message.error(response.error || t('dataModel.deleteError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('dataModel.deleteError'))
    }
  }

  const handleSearch = (value: string) => {
    setFilters(prev => ({ ...prev, search: value }))
    setPagination(prev => ({ ...prev, current: 1 }))
  }

  const handleProjectChange = (value: string | undefined) => {
    setFilters(prev => ({ ...prev, project_id: value }))
    setPagination(prev => ({ ...prev, current: 1 }))
  }

  const handleCategoryChange = (value: string | undefined) => {
    setFilters(prev => ({ ...prev, category: value }))
    setPagination(prev => ({ ...prev, current: 1 }))
  }

  const getCategoryConfig = (category: string | undefined) => {
    const configs: Record<string, { color: string; text: string }> = {
      master: { color: 'blue', text: t('dataModel.categories.master') },
      transaction: { color: 'green', text: t('dataModel.categories.transaction') },
      log: { color: 'orange', text: t('dataModel.categories.log') },
      metric: { color: 'purple', text: t('dataModel.categories.metric') },
      reference: { color: 'cyan', text: t('dataModel.categories.reference') },
    }
    return configs[category || ''] || { color: 'default', text: category || '-' }
  }

  const hasSchema = (dataModel: DataType): boolean => {
    return !!(dataModel.json_schema || (dataModel.schema?.fields && dataModel.schema.fields.length > 0))
  }

  const columns = [
    {
      title: t('dataModel.name'),
      dataIndex: 'display_name',
      key: 'display_name',
      render: (displayName: string, record: DataType) => (
        <a onClick={() => navigate(`/data-models/${record.id}`)}>
          <Space>
            <DatabaseOutlined />
            <span>{displayName}</span>
            <code style={{ fontSize: 12, color: '#999' }}>{record.name}</code>
          </Space>
        </a>
      ),
    },
    {
      title: t('dataModel.category'),
      dataIndex: 'category',
      key: 'category',
      width: 130,
      render: (category: string) => {
        const config = getCategoryConfig(category)
        return <Tag color={config.color}>{config.text}</Tag>
      },
    },
    {
      title: t('dataModel.project'),
      dataIndex: 'project',
      key: 'project',
      width: 150,
      render: (project: Project | undefined) => project?.name || '-',
    },
    {
      title: t('dataModel.schemaStatus'),
      key: 'schema',
      width: 120,
      align: 'center' as const,
      render: (_: unknown, record: DataType) => (
        hasSchema(record) ? (
          <Badge status="success" text={t('dataModel.schemaDefined')} />
        ) : (
          <Badge status="default" text={t('dataModel.schemaUndefined')} />
        )
      ),
    },
    {
      title: t('common.updatedAt'),
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 120,
      render: (date: string) => new Date(date).toLocaleDateString(),
    },
    {
      title: t('common.actions'),
      key: 'action',
      width: 100,
      render: (_: unknown, record: DataType) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EyeOutlined />}
            onClick={() => navigate(`/data-models/${record.id}`)}
            title={t('dataModel.viewDetail')}
          />
          <Popconfirm
            title={t('dataModel.deleteConfirm')}
            description={t('dataModel.deleteWarning')}
            onConfirm={() => handleDelete(record.id)}
            okText={t('common.delete')}
            cancelText={t('common.cancel')}
          >
            <Button type="text" danger icon={<DeleteOutlined />} title={t('common.delete')} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  // Calculate statistics
  const totalCount = dataModels.length
  const schemaDefinedCount = dataModels.filter(dm => hasSchema(dm)).length
  const categoryStats = dataModels.reduce((acc, dm) => {
    const cat = dm.category || 'unknown'
    acc[cat] = (acc[cat] || 0) + 1
    return acc
  }, {} as Record<string, number>)

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Title level={4} style={{ margin: 0 }}>{t('dataModel.list')}</Title>
        <Space>
          <Select
            placeholder={t('dataModel.allProjects')}
            allowClear
            style={{ width: 180 }}
            onChange={handleProjectChange}
            value={filters.project_id}
          >
            {projects.map(p => (
              <Select.Option key={p.id} value={p.id}>{p.name}</Select.Option>
            ))}
          </Select>
          <Select
            placeholder={t('dataModel.category')}
            allowClear
            style={{ width: 150 }}
            onChange={handleCategoryChange}
            value={filters.category}
          >
            <Select.Option value="master">{t('dataModel.categories.master')}</Select.Option>
            <Select.Option value="transaction">{t('dataModel.categories.transaction')}</Select.Option>
            <Select.Option value="log">{t('dataModel.categories.log')}</Select.Option>
            <Select.Option value="metric">{t('dataModel.categories.metric')}</Select.Option>
            <Select.Option value="reference">{t('dataModel.categories.reference')}</Select.Option>
          </Select>
          <Search
            placeholder={t('dataModel.searchPlaceholder')}
            onSearch={handleSearch}
            style={{ width: 200 }}
            allowClear
          />
        </Space>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('dataModel.totalDataModels')}
              value={totalCount}
              prefix={<DatabaseOutlined />}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('dataModel.schemaDefined')}
              value={schemaDefinedCount}
              prefix={<CheckCircleOutlined />}
              valueStyle={{ color: '#52c41a' }}
              suffix={`/ ${totalCount}`}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('dataModel.byCategory')}
              value={Object.keys(categoryStats).length}
              prefix={<AppstoreOutlined />}
            />
          </Card>
        </Col>
      </Row>

      <Table
        dataSource={dataModels}
        columns={columns}
        rowKey="id"
        loading={loading}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          total: pagination.total,
          showSizeChanger: true,
          showTotal: (total) => t('common.total', { count: total }),
          onChange: (page, pageSize) => {
            setPagination(prev => ({ ...prev, current: page, pageSize }))
          },
        }}
      />
    </div>
  )
}
