import { useState, useEffect } from 'react'
import {
  Card,
  Descriptions,
  Tag,
  Table,
  Spin,
  message,
  Avatar,
  Typography,
  Space,
  Divider,
  Empty,
  Row,
  Col,
} from 'antd'
import {
  UserOutlined,
  MailOutlined,
  CalendarOutlined,
  SafetyCertificateOutlined,
  ApiOutlined,
  ClusterOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import dayjs from 'dayjs'

const { Title, Text } = Typography

interface User {
  id: string
  email: string
  name: string
  provider: string
  role: string
  avatar_url: string
  created_at: string
  last_login: string | null
}

interface RoleInfo {
  role: string
  display_name: string
  description: string
  permissions: string[]
}

interface PipelineAccess {
  id: string
  name: string
  actions: string[]
}

interface WorkflowAccess {
  id: string
  name: string
  type: string
  actions: string[]
}

interface UserProfile {
  user: User
  permissions: any[]
  pipelines: PipelineAccess[]
  workflows: WorkflowAccess[]
  role_info: RoleInfo
}

interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

const roleColors: Record<string, string> = {
  admin: 'red',
  operator: 'blue',
  viewer: 'green',
}

export default function ProfilePage() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [profile, setProfile] = useState<UserProfile | null>(null)

  useEffect(() => {
    fetchProfile()
  }, [])

  const fetchProfile = async () => {
    try {
      const response = await api.get<ApiResponse<UserProfile>>('/auth/profile')
      if (response.data?.success && response.data.data) {
        setProfile(response.data.data)
      } else {
        message.error(t('profile.loadError'))
      }
    } catch (error) {
      message.error(t('profile.loadError'))
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: '100px' }}>
        <Spin size="large" tip={t('common.loading')} />
      </div>
    )
  }

  if (!profile) {
    return <Empty description={t('profile.notFound')} />
  }

  const { user, role_info, pipelines, workflows } = profile

  const pipelineColumns = [
    {
      title: t('pipeline.title'),
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: t('profile.permissions'),
      dataIndex: 'actions',
      key: 'actions',
      render: (actions: string[]) => (
        <Space>
          {actions.map((action) => (
            <Tag key={action} color="blue">
              {action}
            </Tag>
          ))}
        </Space>
      ),
    },
  ]

  const workflowColumns = [
    {
      title: t('workflow.title'),
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: t('workflow.type'),
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => (
        <Tag color={type === 'realtime' ? 'green' : 'orange'}>
          {type === 'realtime' ? t('workflow.realtime') : t('workflow.batch')}
        </Tag>
      ),
    },
    {
      title: t('profile.permissions'),
      dataIndex: 'actions',
      key: 'actions',
      render: (actions: string[]) => (
        <Space>
          {actions.map((action) => (
            <Tag key={action} color="blue">
              {action}
            </Tag>
          ))}
        </Space>
      ),
    },
  ]

  return (
    <div style={{ padding: '24px' }}>
      <Title level={2}>
        <UserOutlined /> {t('profile.title')}
      </Title>

      <Row gutter={[24, 24]}>
        {/* Account Info */}
        <Col xs={24} lg={12}>
          <Card title={t('profile.accountInfo')} style={{ height: '100%' }}>
            <div style={{ textAlign: 'center', marginBottom: 24 }}>
              <Avatar
                size={100}
                src={user.avatar_url}
                icon={<UserOutlined />}
              />
              <Title level={4} style={{ marginTop: 16, marginBottom: 4 }}>
                {user.name || user.email}
              </Title>
              <Tag color={roleColors[user.role] || 'default'} style={{ fontSize: 14 }}>
                {role_info.display_name}
              </Tag>
            </div>

            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label={<><MailOutlined /> {t('profile.email')}</>}>
                {user.email}
              </Descriptions.Item>
              <Descriptions.Item label={<><SafetyCertificateOutlined /> {t('profile.authProvider')}</>}>
                <Tag>{user.provider?.toUpperCase() || 'N/A'}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label={<><CalendarOutlined /> {t('profile.joinedAt')}</>}>
                {dayjs(user.created_at).format('YYYY-MM-DD HH:mm')}
              </Descriptions.Item>
              <Descriptions.Item label={<><CalendarOutlined /> {t('profile.lastLogin')}</>}>
                {user.last_login
                  ? dayjs(user.last_login).format('YYYY-MM-DD HH:mm')
                  : '-'}
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>

        {/* Role & Permissions */}
        <Col xs={24} lg={12}>
          <Card title={t('profile.rolePermissions')} style={{ height: '100%' }}>
            <div style={{ marginBottom: 16 }}>
              <Text strong>{t('profile.role')}: </Text>
              <Tag color={roleColors[role_info.role] || 'default'} style={{ fontSize: 14 }}>
                {role_info.display_name}
              </Tag>
            </div>
            <div style={{ marginBottom: 16 }}>
              <Text type="secondary">{role_info.description}</Text>
            </div>

            <Divider orientation="left" plain>
              {t('profile.permissions')}
            </Divider>
            <Space wrap>
              {role_info.permissions.map((perm) => (
                <Tag key={perm} color="geekblue">
                  {perm}
                </Tag>
              ))}
            </Space>
          </Card>
        </Col>

        {/* Accessible Workflows */}
        <Col xs={24}>
          <Card
            title={
              <>
                <ClusterOutlined /> {t('profile.accessibleWorkflows')}
              </>
            }
          >
            {workflows && workflows.length > 0 ? (
              <Table
                dataSource={workflows}
                columns={workflowColumns}
                rowKey="id"
                pagination={false}
                size="small"
              />
            ) : (
              <Empty description={t('profile.noWorkflows')} />
            )}
          </Card>
        </Col>

        {/* Accessible Pipelines */}
        <Col xs={24}>
          <Card
            title={
              <>
                <ApiOutlined /> {t('profile.accessiblePipelines')}
              </>
            }
          >
            {pipelines && pipelines.length > 0 ? (
              <Table
                dataSource={pipelines}
                columns={pipelineColumns}
                rowKey="id"
                pagination={{ pageSize: 10 }}
                size="small"
              />
            ) : (
              <Empty description={t('profile.noPipelines')} />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
