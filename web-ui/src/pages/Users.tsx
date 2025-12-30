import { useState, useEffect } from 'react'
import {
  Card,
  Table,
  Tag,
  Button,
  Space,
  Input,
  Select,
  Modal,
  Form,
  message,
  Typography,
  Avatar,
  Popconfirm,
  Tabs,
  Empty,
} from 'antd'
import {
  UserOutlined,
  SearchOutlined,
  EditOutlined,
  DeleteOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { api } from '../services/api'
import dayjs from 'dayjs'

const { Title, Text } = Typography
const { Option } = Select

interface User {
  id: string
  email: string
  name: string
  provider: string
  role: string
  avatar_url: string
  created_at: string
  last_login: string | null
  permission_count: number
}

interface Permission {
  id: string
  resource_type: string
  resource_id: string
  user_id: string
  actions: string
  created_at: string
  user?: User
}

interface UserListResponse {
  users: User[]
  total: number
  page: number
  page_size: number
  total_pages: number
}

interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
  message?: string
}

const roleColors: Record<string, string> = {
  admin: 'red',
  operator: 'blue',
  viewer: 'green',
}

const roleDisplayNames: Record<string, string> = {
  admin: '관리자',
  operator: '운영자',
  viewer: '뷰어',
}

export default function UsersPage() {
  const [loading, setLoading] = useState(true)
  const [users, setUsers] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [search, setSearch] = useState('')
  const [roleFilter, setRoleFilter] = useState<string | undefined>(undefined)

  const [permissions, setPermissions] = useState<Permission[]>([])
  const [permissionsLoading, setPermissionsLoading] = useState(false)

  const [roleModalVisible, setRoleModalVisible] = useState(false)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [newRole, setNewRole] = useState('')

  const [permissionModalVisible, setPermissionModalVisible] = useState(false)
  const [permissionForm] = Form.useForm()

  useEffect(() => {
    fetchUsers()
  }, [page, pageSize, search, roleFilter])

  const fetchUsers = async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      params.append('page', String(page))
      params.append('page_size', String(pageSize))
      if (search) params.append('search', search)
      if (roleFilter) params.append('role', roleFilter)

      const response = await api.get<ApiResponse<UserListResponse>>(`/users?${params}`)
      if (response.data?.success && response.data.data) {
        setUsers(response.data.data.users || [])
        setTotal(response.data.data.total)
      }
    } catch (error) {
      message.error('사용자 목록을 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }

  const fetchPermissions = async () => {
    setPermissionsLoading(true)
    try {
      const response = await api.get<ApiResponse<Permission[]>>('/permissions')
      if (response.data?.success && response.data.data) {
        setPermissions(response.data.data)
      }
    } catch (error) {
      message.error('권한 목록을 불러오는데 실패했습니다')
    } finally {
      setPermissionsLoading(false)
    }
  }

  const handleRoleChange = async () => {
    if (!selectedUser || !newRole) return

    try {
      const response = await api.put<ApiResponse<User>>(`/users/${selectedUser.id}/role`, {
        role: newRole,
      })
      if (response.data?.success) {
        message.success('역할이 수정되었습니다')
        setRoleModalVisible(false)
        fetchUsers()
      } else {
        message.error(response.data?.error || '역할 수정에 실패했습니다')
      }
    } catch (error) {
      message.error('역할 수정에 실패했습니다')
    }
  }

  const handleCreatePermission = async (values: any) => {
    try {
      // actions 배열을 콤마로 연결된 문자열로 변환
      const payload = {
        ...values,
        actions: Array.isArray(values.actions) ? values.actions.join(',') : values.actions,
      }
      const response = await api.post<ApiResponse<Permission>>('/permissions', payload)
      if (response.data?.success) {
        message.success('권한이 생성되었습니다')
        setPermissionModalVisible(false)
        permissionForm.resetFields()
        fetchPermissions()
      } else {
        message.error(response.data?.error || '권한 생성에 실패했습니다')
      }
    } catch (error) {
      message.error('권한 생성에 실패했습니다')
    }
  }

  const handleDeletePermission = async (id: string) => {
    try {
      const response = await api.delete<ApiResponse<any>>(`/permissions/${id}`)
      if (response.data?.success) {
        message.success('권한이 삭제되었습니다')
        fetchPermissions()
      } else {
        message.error(response.data?.error || '권한 삭제에 실패했습니다')
      }
    } catch (error) {
      message.error('권한 삭제에 실패했습니다')
    }
  }

  const userColumns = [
    {
      title: '사용자',
      key: 'user',
      render: (_: any, record: User) => (
        <Space>
          <Avatar src={record.avatar_url} icon={<UserOutlined />} />
          <div>
            <div>{record.name || record.email}</div>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.email}
            </Text>
          </div>
        </Space>
      ),
    },
    {
      title: '역할',
      dataIndex: 'role',
      key: 'role',
      render: (role: string) => (
        <Tag color={roleColors[role] || 'default'}>
          {roleDisplayNames[role] || role}
        </Tag>
      ),
    },
    {
      title: '인증 제공자',
      dataIndex: 'provider',
      key: 'provider',
      render: (provider: string) => (
        <Tag>{provider?.toUpperCase() || 'N/A'}</Tag>
      ),
    },
    {
      title: '권한 수',
      dataIndex: 'permission_count',
      key: 'permission_count',
      render: (count: number) => count || 0,
    },
    {
      title: '가입일',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => dayjs(date).format('YYYY-MM-DD'),
    },
    {
      title: '마지막 로그인',
      dataIndex: 'last_login',
      key: 'last_login',
      render: (date: string | null) =>
        date ? dayjs(date).format('YYYY-MM-DD HH:mm') : '-',
    },
    {
      title: '작업',
      key: 'actions',
      render: (_: any, record: User) => (
        <Button
          icon={<EditOutlined />}
          size="small"
          onClick={() => {
            setSelectedUser(record)
            setNewRole(record.role)
            setRoleModalVisible(true)
          }}
        >
          역할 변경
        </Button>
      ),
    },
  ]

  const permissionColumns = [
    {
      title: '사용자',
      key: 'user',
      render: (_: any, record: Permission) => (
        <Space>
          <Avatar
            src={record.user?.avatar_url}
            icon={<UserOutlined />}
            size="small"
          />
          <span>{record.user?.name || record.user?.email || record.user_id}</span>
        </Space>
      ),
    },
    {
      title: '리소스 타입',
      dataIndex: 'resource_type',
      key: 'resource_type',
      render: (type: string) => {
        const typeNames: Record<string, string> = {
          provider: '데이터 제공자',
          group: '파이프라인 그룹',
          pipeline: '파이프라인',
        }
        return <Tag>{typeNames[type] || type}</Tag>
      },
    },
    {
      title: '리소스 ID',
      dataIndex: 'resource_id',
      key: 'resource_id',
      ellipsis: true,
    },
    {
      title: '권한',
      dataIndex: 'actions',
      key: 'actions',
      render: (actions: string) => (
        <Space wrap>
          {actions.split(',').map((action) => (
            <Tag key={action} color="blue">
              {action.trim()}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '생성일',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => dayjs(date).format('YYYY-MM-DD'),
    },
    {
      title: '작업',
      key: 'actions',
      render: (_: any, record: Permission) => (
        <Popconfirm
          title="권한을 삭제하시겠습니까?"
          onConfirm={() => handleDeletePermission(record.id)}
          okText="삭제"
          cancelText="취소"
        >
          <Button danger icon={<DeleteOutlined />} size="small" />
        </Popconfirm>
      ),
    },
  ]

  const tabItems = [
    {
      key: 'users',
      label: (
        <span>
          <UserOutlined /> 사용자 ({total})
        </span>
      ),
      children: (
        <Card>
          <Space style={{ marginBottom: 16 }} wrap>
            <Input
              placeholder="이메일 또는 이름 검색"
              prefix={<SearchOutlined />}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              style={{ width: 250 }}
              allowClear
            />
            <Select
              placeholder="역할 필터"
              value={roleFilter}
              onChange={setRoleFilter}
              style={{ width: 150 }}
              allowClear
            >
              <Option value="admin">관리자</Option>
              <Option value="operator">운영자</Option>
              <Option value="viewer">뷰어</Option>
            </Select>
          </Space>

          <Table
            dataSource={users}
            columns={userColumns}
            rowKey="id"
            loading={loading}
            pagination={{
              current: page,
              pageSize,
              total,
              showSizeChanger: true,
              showTotal: (t) => `총 ${t}명`,
              onChange: (p, ps) => {
                setPage(p)
                setPageSize(ps)
              },
            }}
          />
        </Card>
      ),
    },
    {
      key: 'permissions',
      label: (
        <span>
          <SafetyCertificateOutlined /> 리소스 권한
        </span>
      ),
      children: (
        <Card
          extra={
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setPermissionModalVisible(true)}
            >
              권한 추가
            </Button>
          }
        >
          {permissions.length > 0 ? (
            <Table
              dataSource={permissions}
              columns={permissionColumns}
              rowKey="id"
              loading={permissionsLoading}
              pagination={{ pageSize: 20 }}
            />
          ) : (
            <Empty description="등록된 리소스 권한이 없습니다" />
          )}
        </Card>
      ),
    },
  ]

  return (
    <div style={{ padding: '24px' }}>
      <Title level={2}>
        <UserOutlined /> 사용자 관리
      </Title>

      <Tabs
        items={tabItems}
        onChange={(key) => {
          if (key === 'permissions') {
            fetchPermissions()
          }
        }}
      />

      {/* 역할 변경 모달 */}
      <Modal
        title="역할 변경"
        open={roleModalVisible}
        onOk={handleRoleChange}
        onCancel={() => setRoleModalVisible(false)}
        okText="변경"
        cancelText="취소"
      >
        {selectedUser && (
          <div>
            <p>
              <strong>사용자:</strong> {selectedUser.name || selectedUser.email}
            </p>
            <p>
              <strong>현재 역할:</strong>{' '}
              <Tag color={roleColors[selectedUser.role]}>
                {roleDisplayNames[selectedUser.role]}
              </Tag>
            </p>
            <div style={{ marginTop: 16 }}>
              <strong>새 역할:</strong>
              <Select
                value={newRole}
                onChange={setNewRole}
                style={{ width: '100%', marginTop: 8 }}
              >
                <Option value="admin">
                  <Tag color="red">관리자</Tag> - 모든 권한
                </Option>
                <Option value="operator">
                  <Tag color="blue">운영자</Tag> - 파이프라인 생성/수정/실행
                </Option>
                <Option value="viewer">
                  <Tag color="green">뷰어</Tag> - 읽기 전용
                </Option>
              </Select>
            </div>
          </div>
        )}
      </Modal>

      {/* 권한 추가 모달 */}
      <Modal
        title="리소스 권한 추가"
        open={permissionModalVisible}
        onOk={() => permissionForm.submit()}
        onCancel={() => {
          setPermissionModalVisible(false)
          permissionForm.resetFields()
        }}
        okText="추가"
        cancelText="취소"
      >
        <Form
          form={permissionForm}
          layout="vertical"
          onFinish={handleCreatePermission}
        >
          <Form.Item
            name="user_id"
            label="사용자 ID"
            rules={[{ required: true, message: '사용자 ID를 입력하세요' }]}
          >
            <Input placeholder="사용자 UUID" />
          </Form.Item>
          <Form.Item
            name="resource_type"
            label="리소스 타입"
            rules={[{ required: true, message: '리소스 타입을 선택하세요' }]}
          >
            <Select placeholder="리소스 타입 선택">
              <Option value="provider">데이터 제공자</Option>
              <Option value="group">파이프라인 그룹</Option>
              <Option value="pipeline">파이프라인</Option>
            </Select>
          </Form.Item>
          <Form.Item
            name="resource_id"
            label="리소스 ID"
            rules={[{ required: true, message: '리소스 ID를 입력하세요' }]}
          >
            <Input placeholder="리소스 UUID" />
          </Form.Item>
          <Form.Item
            name="actions"
            label="권한"
            rules={[{ required: true, message: '권한을 입력하세요' }]}
          >
            <Select mode="multiple" placeholder="권한 선택">
              <Option value="read">read</Option>
              <Option value="write">write</Option>
              <Option value="execute">execute</Option>
              <Option value="delete">delete</Option>
              <Option value="admin">admin</Option>
            </Select>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
