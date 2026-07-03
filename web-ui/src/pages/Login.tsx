import { useState, useEffect, useRef, ReactNode, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Card,
  CardContent,
  Button,
  Box,
  Typography,
  CircularProgress,
  Divider,
  Stack,
  Alert,
} from '@mui/material'
import {
  GitHub as GitHubIcon,
  Google as GoogleIcon,
  Login as LoginIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '../store/auth'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'

// Provider icons and colors (ID-based lookup)
const PROVIDER_CONFIG: Record<string, { icon: ReactNode; color: string }> = {
  github: { icon: <GitHubIcon />, color: '#24292e' },
  google: { icon: <GoogleIcon />, color: '#4285f4' },
  naver: { icon: <Typography sx={{ fontSize: 14, fontWeight: 'bold' }}>N</Typography>, color: '#03C75A' },
  kakao: { icon: <Typography sx={{ fontSize: 14, fontWeight: 'bold' }}>K</Typography>, color: '#FEE500' },
  gitlab: { icon: <LoginIcon />, color: '#FC6D26' },
}

// Default fallback for unknown providers
const DEFAULT_PROVIDER_CONFIG = { icon: <LoginIcon />, color: '#666666' }

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
  const { showSuccess, showError } = useSnackbar()

  // Fetch configured OAuth2 providers from server
  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await api.get<ApiResponse<Provider[]>>('/auth/providers')
        if (response.data?.success && response.data.data) {
          const sorted = [...response.data.data].sort((a, b) => a.name.localeCompare(b.name))
          setProviders(sorted)
        }
      } catch (error) {
        console.error('Failed to fetch providers:', error)
      } finally {
        setLoadingProviders(false)
      }
    }
    fetchProviders()
  }, [])

  const handleOAuthCallback = useCallback(async (token: string) => {
    try {
      const response = await api.get<ApiResponse<UserResponse>>('/auth/me', {
        headers: { Authorization: `Bearer ${token}` }
      })
      if (response.data?.success && response.data.data) {
        setAuth(token, response.data.data)
        showSuccess(t('auth.loginSuccess'))
        navigate('/dashboard')
      }
    } catch (error) {
      showError(t('auth.loginError'))
    }
  }, [setAuth, showSuccess, showError, t, navigate])

  // Handle OAuth2 callback (if token in URL)
  useEffect(() => {
    const token = searchParams.get('token')
    if (token && !callbackProcessed.current) {
      callbackProcessed.current = true
      handleOAuthCallback(token)
    }
  }, [searchParams, handleOAuthCallback])

  const handleLogin = async (providerId: string) => {
    setLoading(providerId)
    try {
      const response = await api.post<ApiResponse<LoginResponse>>('/auth/login', { provider: providerId })
      if (response.data?.success && response.data.data?.auth_url) {
        window.location.href = response.data.data.auth_url
      } else {
        showError(response.data?.error || t('auth.loginError'))
        setLoading(null)
      }
    } catch (error: unknown) {
      const errorMsg = (error as { response?: { data?: { error?: string } } }).response?.data?.error || t('auth.loginError')
      showError(errorMsg)
      setLoading(null)
    }
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      }}
    >
      <Card sx={{ width: 600, textAlign: 'center' }}>
        <CardContent sx={{ p: 5 }}>
          <img
            src="/logo-title-nobg.png"
            alt="Conduix Logo"
            style={{
              width: 1000,
              maxWidth: '100%',
              marginBottom: 32,
              objectFit: 'contain',
            }}
          />

          {loadingProviders ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 1 }}>
              <CircularProgress size={20} />
              <Typography>{t('auth.loadingOptions')}</Typography>
            </Box>
          ) : (
            <Stack spacing={2}>
              {providers.map((provider) => {
                const config = getProviderConfig(provider.id)
                return (
                  <Button
                    key={provider.id}
                    variant="contained"
                    startIcon={config.icon}
                    size="large"
                    fullWidth
                    disabled={loading === provider.id}
                    onClick={() => handleLogin(provider.id)}
                    sx={{
                      backgroundColor: config.color,
                      '&:hover': {
                        backgroundColor: config.color,
                        filter: 'brightness(0.9)',
                      },
                    }}
                  >
                    {loading === provider.id ? (
                      <CircularProgress size={24} color="inherit" />
                    ) : (
                      t('auth.loginWith', { provider: provider.name })
                    )}
                  </Button>
                )
              })}

              {providers.length === 0 && (
                <>
                  <Divider sx={{ my: 1 }} />
                  <Alert severity="info" sx={{ textAlign: 'left' }}>
                    {t('auth.noProviders')}
                    <br />
                    {t('auth.configureProviders')}
                  </Alert>
                </>
              )}
            </Stack>
          )}

          <Typography
            variant="caption"
            sx={{
              color: "text.secondary",
              mt: 3,
              display: 'block'
            }}>
            {t('auth.termsAgreement')}
          </Typography>
        </CardContent>
      </Card>
    </Box>
  );
}
