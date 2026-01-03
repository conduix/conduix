import { useState, useEffect, useRef, ReactNode } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Card, Button, message, Space, Typography, Spin, Divider } from 'antd'
import { GithubOutlined, GoogleOutlined, GitlabOutlined, LoginOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '../store/auth'
import { api } from '../services/api'

const { Text } = Typography

// Provider icons and colors (ID-based lookup)
const PROVIDER_CONFIG: Record<string, { icon: ReactNode; color: string }> = {
  github: { icon: <GithubOutlined />, color: '#24292e' },
  google: { icon: <GoogleOutlined />, color: '#4285f4' },
  naver: { icon: <span style={{ fontSize: 14, fontWeight: 'bold' }}>N</span>, color: '#03C75A' },
  kakao: { icon: <span style={{ fontSize: 14, fontWeight: 'bold' }}>K</span>, color: '#FEE500' },
  gitlab: { icon: <GitlabOutlined />, color: '#FC6D26' },
}

// Default fallback for unknown providers
const DEFAULT_PROVIDER_CONFIG = { icon: <LoginOutlined />, color: '#666666' }

// Get provider icon and color (with fallback)
const getProviderConfig = (id: string) => PROVIDER_CONFIG[id] || DEFAULT_PROVIDER_CONFIG

interface Provider {
  id: string
  name: string
  enabled: boolean
}

interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

interface LoginResponse {
  auth_url: string
  state: string
  provider: string
}

interface UserResponse {
  id: string
  email: string
  name: string
  role: string
}

export default function LoginPage() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState<string | null>(null)
  const [providers, setProviders] = useState<Provider[]>([])
  const [loadingProviders, setLoadingProviders] = useState(true)
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const setAuth = useAuthStore((state) => state.setAuth)
  const callbackProcessed = useRef(false)

  // Fetch configured OAuth2 providers from server
  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await api.get<ApiResponse<Provider[]>>('/auth/providers')
        if (response.data?.success && response.data.data) {
          setProviders(response.data.data)
        }
      } catch (error) {
        console.error('Failed to fetch providers:', error)
      } finally {
        setLoadingProviders(false)
      }
    }
    fetchProviders()
  }, [])

  // Handle OAuth2 callback (if token in URL)
  useEffect(() => {
    const token = searchParams.get('token')
    if (token && !callbackProcessed.current) {
      callbackProcessed.current = true
      handleOAuthCallback(token)
    }
  }, [searchParams])

  const handleOAuthCallback = async (token: string) => {
    try {
      const response = await api.get<ApiResponse<UserResponse>>('/auth/me', {
        headers: { Authorization: `Bearer ${token}` }
      })
      if (response.data?.success && response.data.data) {
        setAuth(token, response.data.data)
        message.success(t('auth.loginSuccess'))
        navigate('/dashboard')
      }
    } catch (error) {
      message.error(t('auth.loginError'))
    }
  }

  const handleLogin = async (providerId: string) => {
    setLoading(providerId)
    try {
      const response = await api.post<ApiResponse<LoginResponse>>('/auth/login', { provider: providerId })
      if (response.data?.success && response.data.data?.auth_url) {
        window.location.href = response.data.data.auth_url
      } else {
        message.error(response.data?.error || t('auth.loginError'))
        setLoading(null)
      }
    } catch (error: unknown) {
      const errorMsg = (error as { response?: { data?: { error?: string } } }).response?.data?.error || t('auth.loginError')
      message.error(errorMsg)
      setLoading(null)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      }}
    >
      <Card
        style={{ width: 600, textAlign: 'center' }}
        styles={{ body: { padding: '40px' } }}
      >
        <img
          src="/logo-with-title.png"
          alt="Conduix Logo"
          style={{
            width: 1000,
            maxWidth: '100%',
            marginBottom: 32,
            objectFit: 'contain',
          }}
        />

        {loadingProviders ? (
          <Spin tip={t('auth.loadingOptions')} />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {providers.map((provider) => {
              const config = getProviderConfig(provider.id)
              return (
                <Button
                  key={provider.id}
                  type="primary"
                  icon={config.icon}
                  size="large"
                  block
                  loading={loading === provider.id}
                  onClick={() => handleLogin(provider.id)}
                  style={{
                    backgroundColor: config.color,
                    borderColor: config.color,
                  }}
                >
                  {t('auth.loginWith', { provider: provider.name })}
                </Button>
              )
            })}

            {providers.length === 0 && (
              <>
                <Divider style={{ margin: '8px 0' }} />
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {t('auth.noProviders')}
                  <br />
                  {t('auth.configureProviders')}
                </Text>
              </>
            )}
          </Space>
        )}

        <div style={{ marginTop: 24 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('auth.termsAgreement')}
          </Text>
        </div>
      </Card>
    </div>
  )
}
