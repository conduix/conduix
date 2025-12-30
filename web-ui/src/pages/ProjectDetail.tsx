import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  Descriptions,
  Tag,
  Button,
  Space,
  message,
  Spin,
  Typography,
  Tabs,
  Table,
  Empty,
  Breadcrumb,
  Modal,
  Form,
  Input,
  Select,
  Popconfirm,
  Avatar,
} from 'antd'
import {
  ArrowLeftOutlined,
  EditOutlined,
  DeleteOutlined,
  TeamOutlined,
  SettingOutlined,
  UserOutlined,
  PlusOutlined,
  DatabaseOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import debounce from 'lodash/debounce'

const { Title } = Typography

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
}

interface Workflow {
  id: string
  name: string
  slug: string
  description: string
  type: 'batch' | 'realtime'
  status: string
  schedule_enabled: boolean
  provider_id: string
  provider?: {
    id: string
    name: string
  }
  created_at: string
  updated_at: string
}

interface DataType {
  id: string
  project_id: string
  parent_id?: string | null
  name: string
  display_name: string
  description?: string
  category?: string
  id_fields?: string
  created_at: string
  updated_at: string
  parent?: DataType
}

export default function ProjectDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [project, setProject] = useState<Project | null>(null)
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [loading, setLoading] = useState(true)
  const [editModalVisible, setEditModalVisible] = useState(false)
  const [form] = Form.useForm()

  // Workflow 관련 상태
  const [workflowModalVisible, setWorkflowModalVisible] = useState(false)
  const [editingWorkflow, setEditingWorkflow] = useState<Workflow | null>(null)
  const [workflowForm] = Form.useForm()
  const [workflowSaving, setWorkflowSaving] = useState(false)

  // 담당자 검색 관련 상태
  const [userOptions, setUserOptions] = useState<UserOption[]>([])
  const [searchingUsers, setSearchingUsers] = useState(false)
  const [selectedOwners, setSelectedOwners] = useState<UserOption[]>([])

  // DataType 관련 상태
  const [dataTypes, setDataTypes] = useState<DataType[]>([])
  const [dataTypeModalVisible, setDataTypeModalVisible] = useState(false)
  const [editingDataType, setEditingDataType] = useState<DataType | null>(null)
  const [dataTypeForm] = Form.useForm()
  const [dataTypeSaving, setDataTypeSaving] = useState(false)
  const [selectedParentId, setSelectedParentId] = useState<string | undefined>(undefined)

  // 사용자 검색 (디바운스)
  const debouncedSearch = useCallback(
    debounce(async (query: string) => {
      if (!query || query.length < 2) {
        setUserOptions([])
        setSearchingUsers(false)
        return
      }
      setSearchingUsers(true)
      try {
        const response = await api.searchUsers(query)
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
    debouncedSearch(value)
  }

  useEffect(() => {
    if (id) {
      fetchProjectData()
    }
  }, [id])

  const fetchProjectData = async () => {
    try {
      setLoading(true)
      const [projectRes, workflowsRes, dataTypesRes] = await Promise.all([
        api.getProject(id!),
        api.getProjectWorkflows(id!),
        api.getProjectDataTypes(id!),
      ])

      if (projectRes.success) {
        setProject(projectRes.data)
      }
      if (workflowsRes.success) {
        setWorkflows(workflowsRes.data || [])
      }
      if (dataTypesRes.success) {
        setDataTypes(dataTypesRes.data || [])
      }
    } catch (error) {
      message.error(t('project.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const handleEdit = () => {
    if (!project) return
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
    setEditModalVisible(true)
  }

  const handleEditSubmit = async () => {
    try {
      const values = await form.validateFields()
      // 담당자 ID 목록 추가
      const ownerIds = selectedOwners.map(o => o.id)
      const submitData = { ...values, owner_ids: ownerIds }
      const response = await api.updateProject(id!, submitData)
      if (response.success) {
        message.success(t('project.updateSuccess'))
        setEditModalVisible(false)
        fetchProjectData()
      } else {
        message.error(response.error || t('project.updateError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('project.updateError'))
    }
  }

  const handleDelete = async () => {
    try {
      const response = await api.deleteProject(id!)
      if (response.success) {
        message.success(t('project.deleteSuccess'))
        navigate('/projects')
      } else {
        message.error(response.error || t('project.deleteError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('project.deleteError'))
    }
  }

  // Workflow CRUD handlers
  const handleCreateWorkflow = () => {
    setEditingWorkflow(null)
    workflowForm.resetFields()
    workflowForm.setFieldsValue({ type: 'batch' })
    setWorkflowModalVisible(true)
  }

  const handleEditWorkflow = (workflow: Workflow) => {
    setEditingWorkflow(workflow)
    workflowForm.setFieldsValue({
      name: workflow.name,
      slug: workflow.slug,
      type: workflow.type,
      description: workflow.description,
    })
    setWorkflowModalVisible(true)
  }

  const handleWorkflowSubmit = async () => {
    try {
      const values = await workflowForm.validateFields()
      setWorkflowSaving(true)

      if (editingWorkflow) {
        // 수정
        const response = await api.updateWorkflow(editingWorkflow.id, values)
        if (response.success) {
          message.success(t('workflow.updateSuccess'))
          setWorkflowModalVisible(false)
          fetchProjectData()
        } else {
          message.error(response.error || t('workflow.updateError'))
        }
      } else {
        // 생성 - project.id (UUID) 사용
        if (!project?.id) {
          message.error(t('project.notFound'))
          return
        }
        const response = await api.createWorkflow({
          project_id: project.id,
          ...values,
        })
        if (response.success) {
          message.success(t('workflow.createSuccess'))
          setWorkflowModalVisible(false)
          fetchProjectData()
        } else {
          message.error(response.error || t('workflow.createError'))
        }
      }
    } catch (error: any) {
      const errorMsg = editingWorkflow ? t('workflow.updateError') : t('workflow.createError')
      message.error(error.response?.data?.error || errorMsg)
    } finally {
      setWorkflowSaving(false)
    }
  }

  const handleDeleteWorkflow = async (workflowId: string) => {
    try {
      const response = await api.deleteWorkflow(workflowId)
      if (response.success) {
        message.success(t('workflow.deleteSuccess'))
        fetchProjectData()
      } else {
        message.error(response.error || t('workflow.deleteError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('workflow.deleteError'))
    }
  }

  // DataType CRUD handlers
  const handleCreateDataType = () => {
    setEditingDataType(null)
    setSelectedParentId(undefined)
    dataTypeForm.resetFields()
    dataTypeForm.setFieldsValue({ category: 'master', key_field: 'id' })
    setDataTypeModalVisible(true)
  }

  const handleEditDataType = (dataType: DataType) => {
    setEditingDataType(dataType)
    setSelectedParentId(dataType.parent_id || undefined)
    // id_fields는 JSON 문자열이므로 파싱 필요
    let idFieldsArray: string[] = []
    if (dataType.id_fields) {
      try {
        idFieldsArray = JSON.parse(dataType.id_fields)
      } catch {
        idFieldsArray = []
      }
    }
    // 부모가 없으면 key_field 하나, 있으면 id_fields 복합키
    const formValues: Record<string, unknown> = {
      display_name: dataType.display_name,
      name: dataType.name,
      description: dataType.description,
      category: dataType.category || 'master',
      parent_id: dataType.parent_id || undefined,
    }
    if (dataType.parent_id) {
      formValues.id_fields = idFieldsArray
    } else {
      formValues.key_field = idFieldsArray.length > 0 ? idFieldsArray[0] : 'id'
    }
    dataTypeForm.setFieldsValue(formValues)
    setDataTypeModalVisible(true)
  }

  // 부모 체인을 따라 모든 조상을 찾아 복합키 생성
  const buildCompositeKeyFields = (parentId: string): string[] => {
    const keyFields: string[] = []
    let currentId: string | undefined = parentId

    // 조상 체인 탐색 (부모 → 조부모 → ...)
    while (currentId) {
      const dataType = dataTypes.find(dt => dt.id === currentId)
      if (dataType) {
        keyFields.unshift(`${dataType.name}_id`) // 앞에 추가 (root부터 순서대로)
        currentId = dataType.parent_id || undefined
      } else {
        break
      }
    }

    keyFields.push('id') // 마지막에 자기 자신의 id
    return keyFields
  }

  const handleParentChange = (parentId: string | undefined) => {
    setSelectedParentId(parentId)
    if (parentId) {
      // 부모 선택 시 복합키 기본값 설정 (조상 체인 전체 포함)
      const keyFields = buildCompositeKeyFields(parentId)
      dataTypeForm.setFieldsValue({
        id_fields: keyFields,
      })
    } else {
      // 부모 해제 시 단일 키 필드로 전환
      dataTypeForm.setFieldsValue({
        key_field: 'id',
      })
    }
  }

  const handleDataTypeSubmit = async () => {
    try {
      const values = await dataTypeForm.validateFields()
      setDataTypeSaving(true)

      // parent_id가 빈 문자열이면 null로 변환
      // key_field 또는 id_fields를 id_fields 배열로 통합 (공백 제거)
      const idFieldsArray = values.parent_id
        ? (values.id_fields || []).map((f: string) => f.trim()).filter((f: string) => f)
        : values.key_field ? [values.key_field.trim()].filter((f: string) => f) : []

      const submitData = {
        display_name: values.display_name,
        name: values.name,
        description: values.description,
        category: values.category,
        parent_id: values.parent_id || null,
        id_fields: idFieldsArray,
      }

      if (editingDataType) {
        // 수정
        const response = await api.updateDataType(editingDataType.id, submitData)
        if (response.success) {
          message.success(t('dataType.updateSuccess'))
          setDataTypeModalVisible(false)
          fetchProjectData()
        } else {
          message.error(response.error || t('dataType.updateError'))
        }
      } else {
        // 생성 - project.id (UUID) 사용
        if (!project?.id) {
          message.error(t('project.notFound'))
          return
        }
        const response = await api.createDataType({
          project_id: project.id,
          ...submitData,
        })
        if (response.success) {
          message.success(t('dataType.createSuccess'))
          setDataTypeModalVisible(false)
          fetchProjectData()
        } else {
          message.error(response.error || t('dataType.createError'))
        }
      }
    } catch (error: any) {
      const errorMsg = editingDataType ? t('dataType.updateError') : t('dataType.createError')
      message.error(error.response?.data?.error || errorMsg)
    } finally {
      setDataTypeSaving(false)
    }
  }

  const handleDeleteDataType = async (dataTypeId: string) => {
    try {
      const response = await api.deleteDataType(dataTypeId)
      if (response.success) {
        message.success(t('dataType.deleteSuccess'))
        fetchProjectData()
      } else {
        message.error(response.error || t('dataType.deleteError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('dataType.deleteError'))
    }
  }

  const getCategoryConfig = (category: string) => {
    const configs: Record<string, { color: string; text: string }> = {
      master: { color: 'blue', text: t('dataType.categories.master') },
      transaction: { color: 'green', text: t('dataType.categories.transaction') },
      log: { color: 'orange', text: t('dataType.categories.log') },
      metric: { color: 'purple', text: t('dataType.categories.metric') },
      reference: { color: 'cyan', text: t('dataType.categories.reference') },
    }
    return configs[category] || { color: 'default', text: category }
  }

  const getStatusConfig = (status: string) => {
    const configs: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: t('status.active') },
      inactive: { color: 'default', text: t('status.inactive') },
      archived: { color: 'orange', text: t('status.archived') },
    }
    return configs[status] || { color: 'default', text: status }
  }

  const getWorkflowStatusConfig = (status: string) => {
    const configs: Record<string, { color: string; text: string }> = {
      idle: { color: 'default', text: t('pipeline.status.idle') },
      running: { color: 'green', text: t('pipeline.status.running') },
      paused: { color: 'orange', text: t('pipeline.status.paused') },
      stopped: { color: 'default', text: t('pipeline.status.stopped') },
      error: { color: 'red', text: t('pipeline.status.error') },
      completed: { color: 'blue', text: t('pipeline.status.completed') },
    }
    return configs[status] || { color: 'default', text: status }
  }

  const workflowColumns = [
    {
      title: t('workflow.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: Workflow) => (
        <div>
          <a onClick={() => navigate(`/workflows/${record.id}`)}>{name}</a>
          <div style={{ fontSize: 12, color: '#999' }}>
            <code>{record.slug}</code>
          </div>
        </div>
      ),
    },
    {
      title: t('workflow.type'),
      dataIndex: 'type',
      key: 'type',
      width: 100,
      render: (type: string) => (
        <Tag color={type === 'realtime' ? 'blue' : 'purple'}>
          {type === 'realtime' ? t('workflow.realtime') : t('workflow.batch')}
        </Tag>
      ),
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: string) => {
        const config = getWorkflowStatusConfig(status)
        return <Tag color={config.color}>{config.text}</Tag>
      },
    },
    {
      title: t('workflow.enabled'),
      dataIndex: 'schedule_enabled',
      key: 'schedule_enabled',
      width: 80,
      render: (enabled: boolean) => (
        <Tag color={enabled ? 'green' : 'default'}>
          {enabled ? t('workflow.on') : t('workflow.off')}
        </Tag>
      ),
    },
    {
      title: t('common.description'),
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
      render: (desc: string) => desc || '-',
    },
    {
      title: t('common.actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, record: Workflow) => (
        <Space>
          <Button
            size="small"
            icon={<EditOutlined />}
            onClick={() => handleEditWorkflow(record)}
          />
          <Popconfirm
            title={t('workflow.deleteConfirm')}
            description={t('workflow.deleteWarning')}
            onConfirm={() => handleDeleteWorkflow(record.id)}
            okText={t('common.delete')}
            cancelText={t('common.cancel')}
          >
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  // DataType의 depth 계산 (부모 체인 따라가며)
  const getDataTypeDepth = (dt: DataType): number => {
    let depth = 0
    let currentParentId = dt.parent_id
    while (currentParentId) {
      depth++
      const parent = dataTypes.find(p => p.id === currentParentId)
      currentParentId = parent?.parent_id || null
    }
    return depth
  }

  // DataType을 계층 구조로 정렬
  const getHierarchicalDataTypes = (): DataType[] => {
    const result: DataType[] = []
    const addWithChildren = (parentId: string | null) => {
      const children = dataTypes
        .filter(dt => (dt.parent_id || null) === parentId)
        .sort((a, b) => a.display_name.localeCompare(b.display_name))
      for (const child of children) {
        result.push(child)
        addWithChildren(child.id)
      }
    }
    addWithChildren(null)
    return result
  }

  const hierarchicalDataTypes = getHierarchicalDataTypes()

  const dataTypeColumns = [
    {
      title: t('dataType.name'),
      dataIndex: 'display_name',
      key: 'display_name',
      render: (displayName: string, record: DataType) => {
        const depth = getDataTypeDepth(record)
        return (
          <div style={{ paddingLeft: depth * 24 }}>
            <Space size={4}>
              {depth > 0 && (
                <span style={{ color: '#999', fontSize: 12 }}>└─</span>
              )}
              <span>{displayName}</span>
            </Space>
            <div style={{ fontSize: 12, color: '#999', paddingLeft: depth > 0 ? 20 : 0 }}>
              <code>{record.name}</code>
            </div>
          </div>
        )
      },
    },
    {
      title: t('dataType.category'),
      dataIndex: 'category',
      key: 'category',
      width: 140,
      render: (category: string) => {
        const config = getCategoryConfig(category || 'master')
        return <Tag color={config.color}>{config.text}</Tag>
      },
    },
    {
      title: t('common.description'),
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
      render: (desc: string) => desc || '-',
    },
    {
      title: t('common.updatedAt'),
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      render: (date: string) => new Date(date).toLocaleString(),
    },
    {
      title: t('common.actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, record: DataType) => {
        // 자식이 있으면 삭제 불가
        const hasChildren = dataTypes.some(dt => dt.parent_id === record.id)
        return (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => handleEditDataType(record)}
            />
            {!hasChildren && (
              <Popconfirm
                title={t('dataType.deleteConfirm')}
                description={t('dataType.deleteWarning')}
                onConfirm={() => handleDeleteDataType(record.id)}
                okText={t('common.delete')}
                cancelText={t('common.cancel')}
              >
                <Button size="small" danger icon={<DeleteOutlined />} />
              </Popconfirm>
            )}
          </Space>
        )
      },
    },
  ]

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: 400 }}>
        <Spin size="large" />
      </div>
    )
  }

  if (!project) {
    return (
      <Empty description={t('project.notFound')}>
        <Button type="primary" onClick={() => navigate('/projects')}>
          {t('common.back')}
        </Button>
      </Empty>
    )
  }

  const statusConfig = getStatusConfig(project.status)

  return (
    <div>
      <Breadcrumb
        style={{ marginBottom: 16 }}
        items={[
          { title: <a onClick={() => navigate('/projects')}>{t('project.title')}</a> },
          { title: project.name },
        ]}
      />

      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/projects')}>
            {t('common.list')}
          </Button>
          <Title level={4} style={{ margin: 0 }}>
            {project.name}
          </Title>
          <Tag color={statusConfig.color}>{statusConfig.text}</Tag>
        </Space>
        <Space>
          <Button icon={<EditOutlined />} onClick={handleEdit}>
            {t('common.edit')}
          </Button>
          <Popconfirm
            title={t('project.deleteConfirm')}
            description={t('project.deleteWarning')}
            onConfirm={handleDelete}
            okText={t('common.delete')}
            cancelText={t('common.cancel')}
          >
            <Button danger icon={<DeleteOutlined />}>
              {t('common.delete')}
            </Button>
          </Popconfirm>
        </Space>
      </div>

      <Tabs
        defaultActiveKey="overview"
        items={[
          {
            key: 'overview',
            label: (
              <span>
                <SettingOutlined />
                {t('project.overview')}
              </span>
            ),
            children: (
              <Card>
                <Descriptions bordered column={2}>
                  <Descriptions.Item label={t('project.name')}>
                    {project.name}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('project.alias')}>
                    <code>{project.alias}</code>
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.status')}>
                    <Tag color={statusConfig.color}>{statusConfig.text}</Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.owner')}>
                    {project.owners && project.owners.length > 0 ? (
                      <Space wrap size={8}>
                        {project.owners.map(po => (
                          <Tag
                            key={po.id}
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: 6,
                              padding: '4px 8px',
                              margin: 0,
                            }}
                          >
                            <Avatar size={20} src={po.user.avatar_url} icon={<UserOutlined />} />
                            <span>{po.user.name || po.user.email}</span>
                          </Tag>
                        ))}
                      </Space>
                    ) : project.owner ? (
                      <Tag
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: 6,
                          padding: '4px 8px',
                          margin: 0,
                        }}
                      >
                        <Avatar size={20} icon={<UserOutlined />} />
                        <span>{project.owner.name || project.owner.email}</span>
                      </Tag>
                    ) : (
                      '-'
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.description')} span={2}>
                    {project.description || '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.tags')} span={2}>
                    {project.tags ? (
                      <Space>
                        {project.tags.split(',').map((tag, i) => (
                          <Tag key={i}>{tag.trim()}</Tag>
                        ))}
                      </Space>
                    ) : (
                      '-'
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.createdAt')}>
                    {new Date(project.created_at).toLocaleString()}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('common.updatedAt')}>
                    {new Date(project.updated_at).toLocaleString()}
                  </Descriptions.Item>
                </Descriptions>
              </Card>
            ),
          },
          {
            key: 'dataTypes',
            label: (
              <span>
                <DatabaseOutlined />
                {t('dataType.title')} ({dataTypes.length})
              </span>
            ),
            children: (
              <Card
                title={t('dataType.title')}
                extra={
                  <Button type="primary" icon={<PlusOutlined />} onClick={handleCreateDataType}>
                    {t('dataType.new')}
                  </Button>
                }
              >
                {dataTypes.length > 0 ? (
                  <Table
                    dataSource={hierarchicalDataTypes}
                    columns={dataTypeColumns}
                    rowKey="id"
                    pagination={false}
                  />
                ) : (
                  <Empty description={t('dataType.noDataTypes')}>
                    <Button type="primary" onClick={handleCreateDataType}>
                      {t('dataType.new')}
                    </Button>
                  </Empty>
                )}
              </Card>
            ),
          },
          {
            key: 'workflows',
            label: (
              <span>
                <TeamOutlined />
                {t('project.workflows', { count: workflows.length })}
              </span>
            ),
            children: (
              <Card
                title={t('workflow.title')}
                extra={
                  <Button type="primary" icon={<PlusOutlined />} onClick={handleCreateWorkflow}>
                    {t('workflow.new')}
                  </Button>
                }
              >
                {workflows.length > 0 ? (
                  <Table
                    dataSource={workflows}
                    columns={workflowColumns}
                    rowKey="id"
                    pagination={false}
                  />
                ) : (
                  <Empty description={t('project.noWorkflows')}>
                    <Button type="primary" onClick={handleCreateWorkflow}>
                      {t('workflow.new')}
                    </Button>
                  </Empty>
                )}
              </Card>
            ),
          },
        ]}
      />

      <Modal
        title={t('project.edit')}
        open={editModalVisible}
        onOk={handleEditSubmit}
        onCancel={() => setEditModalVisible(false)}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label={t('project.name')}
            rules={[{ required: true, message: t('project.nameRequired') }]}
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

          <Form.Item name="status" label={t('common.status')}>
            <Select>
              <Select.Option value="active">{t('status.active')}</Select.Option>
              <Select.Option value="inactive">{t('status.inactive')}</Select.Option>
              <Select.Option value="archived">{t('status.archived')}</Select.Option>
            </Select>
          </Form.Item>

          <Form.Item
            name="tags"
            label={t('common.tags')}
            extra={t('common.tagsHelp')}
          >
            <Input placeholder="tag1, tag2, tag3" />
          </Form.Item>
        </Form>
      </Modal>

      {/* Workflow Create/Edit Modal */}
      <Modal
        title={editingWorkflow ? t('workflow.edit') : t('workflow.new')}
        open={workflowModalVisible}
        onOk={handleWorkflowSubmit}
        onCancel={() => setWorkflowModalVisible(false)}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        confirmLoading={workflowSaving}
      >
        <Form form={workflowForm} layout="vertical">
          <Form.Item
            name="name"
            label={t('workflow.name')}
            rules={[{ required: true, message: t('workflow.nameRequired') }]}
          >
            <Input placeholder={t('workflow.namePlaceholder')} />
          </Form.Item>

          <Form.Item
            name="slug"
            label={t('workflow.slug')}
            rules={[
              { required: true, message: t('workflow.slugRequired') },
              { pattern: /^[a-z0-9-]+$/, message: t('workflow.slugPattern') },
            ]}
            extra={t('workflow.slugHelp')}
          >
            <Input placeholder={t('workflow.slugPlaceholder')} />
          </Form.Item>

          <Form.Item
            name="type"
            label={t('workflow.type')}
            rules={[{ required: true, message: t('workflow.typeRequired') }]}
          >
            <Select>
              <Select.Option value="batch">
                <Tag color="purple">{t('workflow.batch')}</Tag>
                <span style={{ marginLeft: 8, color: '#666' }}>{t('workflow.batchDesc')}</span>
              </Select.Option>
              <Select.Option value="realtime">
                <Tag color="blue">{t('workflow.realtime')}</Tag>
                <span style={{ marginLeft: 8, color: '#666' }}>{t('workflow.realtimeDesc')}</span>
              </Select.Option>
            </Select>
          </Form.Item>

          <Form.Item
            name="description"
            label={t('workflow.description')}
          >
            <Input.TextArea rows={3} placeholder={t('workflow.descriptionPlaceholder')} />
          </Form.Item>
        </Form>
      </Modal>

      {/* DataType Create/Edit Modal */}
      <Modal
        title={editingDataType ? t('dataType.edit') : t('dataType.new')}
        open={dataTypeModalVisible}
        onOk={handleDataTypeSubmit}
        onCancel={() => setDataTypeModalVisible(false)}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        confirmLoading={dataTypeSaving}
      >
        <Form form={dataTypeForm} layout="vertical">
          <Form.Item
            name="display_name"
            label={t('dataType.name')}
            rules={[{ required: true, message: t('dataType.nameRequired') }]}
          >
            <Input placeholder={t('dataType.namePlaceholder')} />
          </Form.Item>

          <Form.Item
            name="name"
            label={t('dataType.slug')}
            rules={[
              { required: true, message: t('dataType.slugRequired') },
              { pattern: /^[a-z0-9_-]+$/, message: t('dataType.slugPattern') },
            ]}
            extra={t('dataType.slugHelp')}
          >
            <Input placeholder={t('dataType.slugPlaceholder')} />
          </Form.Item>

          <Form.Item name="category" label={t('dataType.category')}>
            <Select>
              <Select.Option value="master">
                <div>
                  <Tag color="blue">{t('dataType.categories.master')}</Tag>
                  <span style={{ marginLeft: 8, color: '#666', fontSize: 12 }}>{t('dataType.categories.masterDesc')}</span>
                </div>
              </Select.Option>
              <Select.Option value="transaction">
                <div>
                  <Tag color="green">{t('dataType.categories.transaction')}</Tag>
                  <span style={{ marginLeft: 8, color: '#666', fontSize: 12 }}>{t('dataType.categories.transactionDesc')}</span>
                </div>
              </Select.Option>
              <Select.Option value="log">
                <div>
                  <Tag color="orange">{t('dataType.categories.log')}</Tag>
                  <span style={{ marginLeft: 8, color: '#666', fontSize: 12 }}>{t('dataType.categories.logDesc')}</span>
                </div>
              </Select.Option>
              <Select.Option value="metric">
                <div>
                  <Tag color="purple">{t('dataType.categories.metric')}</Tag>
                  <span style={{ marginLeft: 8, color: '#666', fontSize: 12 }}>{t('dataType.categories.metricDesc')}</span>
                </div>
              </Select.Option>
              <Select.Option value="reference">
                <div>
                  <Tag color="cyan">{t('dataType.categories.reference')}</Tag>
                  <span style={{ marginLeft: 8, color: '#666', fontSize: 12 }}>{t('dataType.categories.referenceDesc')}</span>
                </div>
              </Select.Option>
            </Select>
          </Form.Item>

          <Form.Item
            name="parent_id"
            label={t('dataType.parent')}
            extra={
              editingDataType && dataTypes.some(dt => dt.parent_id === editingDataType.id)
                ? t('dataType.parentDisabledHasChildren')
                : t('dataType.parentHelp')
            }
          >
            <Select
              allowClear
              placeholder={t('dataType.parentPlaceholder')}
              onChange={handleParentChange}
              disabled={editingDataType ? dataTypes.some(dt => dt.parent_id === editingDataType.id) : false}
            >
              {dataTypes
                .filter(dt => dt.id !== editingDataType?.id)
                .map(dt => (
                  <Select.Option key={dt.id} value={dt.id}>
                    <span>{dt.display_name}</span>
                    <span style={{ marginLeft: 8, color: '#999' }}>({dt.name})</span>
                  </Select.Option>
                ))}
            </Select>
          </Form.Item>

          {selectedParentId ? (
            <Form.Item
              name="id_fields"
              label={t('dataType.idFields')}
              extra={t('dataType.idFieldsHelp')}
              rules={[{ required: true, message: t('dataType.idFieldsRequired') }]}
            >
              <Select
                mode="tags"
                placeholder={t('dataType.idFieldsPlaceholder')}
                tokenSeparators={[',']}
              />
            </Form.Item>
          ) : (
            <Form.Item
              name="key_field"
              label={t('dataType.keyField')}
              extra={t('dataType.keyFieldHelp')}
              rules={[
                { required: true, message: t('dataType.keyFieldRequired') },
                { whitespace: true, message: t('dataType.keyFieldRequired') },
              ]}
            >
              <Input placeholder={t('dataType.keyFieldPlaceholder')} />
            </Form.Item>
          )}

          <Form.Item name="description" label={t('common.description')}>
            <Input.TextArea rows={3} placeholder={t('project.descriptionPlaceholder')} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
