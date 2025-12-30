import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Table,
  Button,
  Space,
  Tag,
  Modal,
  Form,
  Input,
  Select,
  message,
  Popconfirm,
  Typography,
  Card,
  Row,
  Col,
  Statistic,
  Avatar,
  Spin,
} from 'antd'
import {
  PlusOutlined,
  DeleteOutlined,
  EditOutlined,
  ProjectOutlined,
  TeamOutlined,
  FolderOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useAuthStore } from '../store/auth'
import debounce from 'lodash/debounce'

const { Title } = Typography
const { Search } = Input

// 사용자 검색 결과 타입
interface UserOption {
  id: string
  email: string
  name: string
  avatar_url?: string
}

// 프로젝트 담당자 타입
interface ProjectOwner {
  id: string
  user_id: string
  role: string
  user: {
    id: string
    name: string
    email: string
    avatar_url?: string
  }
}

interface Project {
  id: string
  name: string
  alias: string
  description: string
  status: string
  owner_id: string
  owner?: {
    id: string
    name: string
    email: string
  }
  owners?: ProjectOwner[]
  metadata: string
  tags: string
  created_by: string
  created_at: string
  updated_at: string
  group_count?: number
}

interface ProjectListResponse {
  projects: Project[]
  total: number
  page: number
  page_size: number
  total_pages: number
}

export default function ProjectsPage() {
  const { t } = useTranslation()
  const { user: currentUser } = useAuthStore()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [modalVisible, setModalVisible] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 })

  // 담당자 검색 관련 상태
  const [userOptions, setUserOptions] = useState<UserOption[]>([])
  const [searchingUsers, setSearchingUsers] = useState(false)
  const [selectedOwners, setSelectedOwners] = useState<UserOption[]>([])
  const [searchText, setSearchText] = useState('')
  const [form] = Form.useForm()
  const navigate = useNavigate()

  // 사용자 검색 (디바운스)
  const debouncedSearch = useCallback(
    debounce(async (query: string) => {
      console.log('Debounced search called with:', query)
      if (!query || query.length < 2) {
        setUserOptions([])
        setSearchingUsers(false)
        return
      }
      setSearchingUsers(true)
      try {
        console.log('Calling API searchUsers...')
        const response = await api.searchUsers(query)
        console.log('API response:', response)
        if (response.success) {
          setUserOptions(response.data || [])
        }
      } catch (error) {
        console.error('User search failed:', error)
      } finally {
        setSearchingUsers(false)
      }
    }, 300),
    []
  )

  const handleUserSearch = (value: string) => {
    console.log('handleUserSearch called with:', value)
    debouncedSearch(value)
  }

  useEffect(() => {
    fetchProjects()
  }, [pagination.current, pagination.pageSize, searchText])

  const fetchProjects = async () => {
    try {
      setLoading(true)
      const response = await api.getProjects({
        page: pagination.current,
        page_size: pagination.pageSize,
        search: searchText || undefined,
      })
      if (response.success) {
        const data = response.data as ProjectListResponse
        setProjects(data.projects || [])
        setPagination(prev => ({ ...prev, total: data.total }))
      }
    } catch (error) {
      message.error(t('project.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const handleCreate = () => {
    setEditingProject(null)
    form.resetFields()
    // 현재 사용자를 기본 담당자로 설정
    if (currentUser) {
      const defaultOwner: UserOption = {
        id: currentUser.id,
        email: currentUser.email,
        name: currentUser.name || currentUser.email,
        avatar_url: currentUser.avatarUrl,
      }
      setSelectedOwners([defaultOwner])
    } else {
      setSelectedOwners([])
    }
    setUserOptions([])
    setModalVisible(true)
  }

  const handleEdit = (project: Project) => {
    setEditingProject(project)
    form.setFieldsValue({
      name: project.name,
      alias: project.alias,
      description: project.description,
      status: project.status,
      tags: project.tags,
    })
    // 기존 담당자 목록 설정
    if (project.owners && project.owners.length > 0) {
      const owners: UserOption[] = project.owners.map(po => ({
        id: po.user.id,
        email: po.user.email,
        name: po.user.name,
        avatar_url: po.user.avatar_url,
      }))
      setSelectedOwners(owners)
    } else if (project.owner) {
      // 기존 단일 owner 호환
      setSelectedOwners([{
        id: project.owner.id,
        email: project.owner.email,
        name: project.owner.name,
      }])
    } else {
      setSelectedOwners([])
    }
    setUserOptions([])
    setModalVisible(true)
  }

  const handleDelete = async (id: string) => {
    try {
      const response = await api.deleteProject(id)
      if (response.success) {
        message.success(t('project.deleteSuccess'))
        fetchProjects()
      } else {
        message.error(response.error || t('project.deleteError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('project.deleteError'))
    }
  }

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields()
      // 담당자 ID 목록 추가
      const ownerIds = selectedOwners.map(o => o.id)
      const submitData = { ...values, owner_ids: ownerIds }

      if (editingProject) {
        const response = await api.updateProject(editingProject.id, submitData)
        if (response.success) {
          message.success(t('project.updateSuccess'))
          setModalVisible(false)
          fetchProjects()
        } else {
          message.error(response.error || t('project.updateError'))
        }
      } else {
        const response = await api.createProject(submitData)
        if (response.success) {
          message.success(t('project.createSuccess'))
          setModalVisible(false)
          fetchProjects()
        } else {
          message.error(response.error || t('project.createError'))
        }
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('common.save') + ' failed')
    }
  }

  const handleSearch = (value: string) => {
    setSearchText(value)
    setPagination(prev => ({ ...prev, current: 1 }))
  }

  const getStatusConfig = (status: string) => {
    const configs: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: t('status.active') },
      inactive: { color: 'default', text: t('status.inactive') },
      archived: { color: 'orange', text: t('status.archived') },
    }
    return configs[status] || { color: 'default', text: status }
  }

  const columns = [
    {
      title: t('project.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: Project) => (
        <a onClick={() => navigate(`/projects/${record.alias || record.id}`)}>
          <Space>
            <ProjectOutlined />
            {name}
          </Space>
        </a>
      ),
    },
    {
      title: t('project.alias'),
      dataIndex: 'alias',
      key: 'alias',
      render: (alias: string) => <code>{alias}</code>,
    },
    {
      title: t('common.description'),
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
    },
    {
      title: t('project.groupCount'),
      dataIndex: 'group_count',
      key: 'group_count',
      width: 100,
      align: 'center' as const,
      render: (count: number) => count || 0,
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: string) => {
        const config = getStatusConfig(status)
        return <Tag color={config.color}>{config.text}</Tag>
      },
    },
    {
      title: t('common.createdAt'),
      dataIndex: 'created_at',
      key: 'created_at',
      width: 120,
      render: (date: string) => new Date(date).toLocaleDateString(),
    },
    {
      title: t('common.actions'),
      key: 'action',
      width: 100,
      render: (_: unknown, record: Project) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
            title={t('common.edit')}
          />
          <Popconfirm
            title={t('project.deleteConfirm')}
            description={t('project.deleteWarning')}
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
  const activeCount = projects.filter(p => p.status === 'active').length
  const totalGroups = projects.reduce((sum, p) => sum + (p.group_count || 0), 0)

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Title level={4} style={{ margin: 0 }}>{t('project.title')}</Title>
        <Space>
          <Search
            placeholder={t('project.searchPlaceholder')}
            onSearch={handleSearch}
            style={{ width: 250 }}
            allowClear
          />
          <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>
            {t('project.new')}
          </Button>
        </Space>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('project.totalProjects')}
              value={pagination.total}
              prefix={<FolderOutlined />}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('project.activeProjects')}
              value={activeCount}
              prefix={<ProjectOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic
              title={t('project.totalGroups')}
              value={totalGroups}
              prefix={<TeamOutlined />}
            />
          </Card>
        </Col>
      </Row>

      <Table
        dataSource={projects}
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

      <Modal
        title={editingProject ? t('project.edit') : t('project.new')}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={600}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label={t('project.name')}
            rules={[
              { required: true, message: t('project.nameRequired') },
            ]}
          >
            <Input placeholder={t('project.namePlaceholder')} />
          </Form.Item>

          <Form.Item
            name="alias"
            label={t('project.alias')}
            rules={[
              { required: true, message: t('project.aliasRequired') },
              { pattern: /^[a-z0-9-]+$/, message: t('project.aliasPattern') },
            ]}
            extra={t('project.aliasHelp')}
          >
            <Input placeholder={t('project.aliasPlaceholder')} />
          </Form.Item>

          <Form.Item name="description" label={t('common.description')}>
            <Input.TextArea rows={3} placeholder={t('project.descriptionPlaceholder')} />
          </Form.Item>

          <Form.Item label={t('project.owners')}>
            <Select
              mode="multiple"
              showSearch
              placeholder={t('project.ownersPlaceholder')}
              value={selectedOwners.map(o => o.id)}
              onChange={(values: string[]) => {
                // 선택된 사용자 ID로 selectedOwners 업데이트
                const newOwners = values.map(id => {
                  const existing = selectedOwners.find(o => o.id === id)
                  if (existing) return existing
                  const fromOptions = userOptions.find(o => o.id === id)
                  return fromOptions || { id, email: '', name: '' }
                })
                setSelectedOwners(newOwners)
              }}
              onSearch={handleUserSearch}
              filterOption={false}
              notFoundContent={searchingUsers ? <Spin size="small" /> : null}
              optionLabelProp="label"
            >
              {/* 현재 선택된 담당자들 */}
              {selectedOwners.map(owner => (
                <Select.Option key={owner.id} value={owner.id} label={owner.name || owner.email}>
                  <Space>
                    <Avatar size="small" src={owner.avatar_url} icon={<UserOutlined />} />
                    <span>{owner.name || owner.email}</span>
                    <span style={{ color: '#999' }}>{owner.email}</span>
                  </Space>
                </Select.Option>
              ))}
              {/* 검색 결과 (선택되지 않은 것만) */}
              {userOptions
                .filter(opt => !selectedOwners.some(s => s.id === opt.id))
                .map(user => (
                  <Select.Option key={user.id} value={user.id} label={user.name || user.email}>
                    <Space>
                      <Avatar size="small" src={user.avatar_url} icon={<UserOutlined />} />
                      <span>{user.name || user.email}</span>
                      <span style={{ color: '#999' }}>{user.email}</span>
                    </Space>
                  </Select.Option>
                ))}
            </Select>
          </Form.Item>

          {editingProject && (
            <Form.Item name="status" label={t('common.status')}>
              <Select>
                <Select.Option value="active">{t('status.active')}</Select.Option>
                <Select.Option value="inactive">{t('status.inactive')}</Select.Option>
                <Select.Option value="archived">{t('status.archived')}</Select.Option>
              </Select>
            </Form.Item>
          )}

          <Form.Item
            name="tags"
            label={t('common.tags')}
            extra={t('common.tagsHelp')}
          >
            <Input placeholder="tag1, tag2, tag3" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
