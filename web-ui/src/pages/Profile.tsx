import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Card,
  CardContent,
  Typography,
  Grid,
  Avatar,
  Chip,
  Divider,
  CircularProgress,
  Stack,
} from '@mui/material'
import { DataGrid, GridColDef } from '@mui/x-data-grid'
import {
  Person as UserIcon,
  Email as MailIcon,
  CalendarMonth as CalendarIcon,
  Security as SecurityIcon,
  Api as ApiIcon,
  Dns as ClusterIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import dayjs from 'dayjs'

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
  permissions: string[]
  pipelines: PipelineAccess[]
  workflows: WorkflowAccess[]
  role_info: RoleInfo
}

interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

const roleColors: Record<string, 'error' | 'primary' | 'success'> = {
  admin: 'error',
  operator: 'primary',
  viewer: 'success',
}

export default function ProfilePage() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const { showError } = useSnackbar()

  const fetchProfile = useCallback(async () => {
    try {
      const response = await api.get<ApiResponse<UserProfile>>('/auth/profile')
      if (response.data?.success && response.data.data) {
        setProfile(response.data.data)
      } else {
        showError(t('profile.loadError'))
      }
    } catch (error) {
      showError(t('profile.loadError'))
    } finally {
      setLoading(false)
    }
  }, [showError, t])

  useEffect(() => {
    fetchProfile()
  }, [fetchProfile])

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: 400 }}>
        <CircularProgress />
      </Box>
    )
  }

  if (!profile) {
    return (
      <Box sx={{ textAlign: 'center', py: 8 }}>
        <Typography sx={{
          color: "text.secondary"
        }}>{t('profile.notFound')}</Typography>
      </Box>
    );
  }

  const { user, role_info, pipelines, workflows } = profile

  const pipelineColumns: GridColDef[] = [
    { field: 'name', headerName: t('pipeline.title'), flex: 1 },
    {
      field: 'actions',
      headerName: t('profile.permissions'),
      flex: 1,
      renderCell: (params) => (
        <Stack direction="row" spacing={0.5}>
          {params.value.map((action: string) => (
            <Chip key={action} label={action} size="small" color="primary" variant="outlined" />
          ))}
        </Stack>
      ),
    },
  ]

  const workflowColumns: GridColDef[] = [
    { field: 'name', headerName: t('workflow.title'), flex: 1 },
    {
      field: 'type',
      headerName: t('workflow.type'),
      width: 120,
      renderCell: (params) => (
        <Chip
          label={params.value === 'realtime' ? t('workflow.realtime') : t('workflow.batch')}
          color={params.value === 'realtime' ? 'success' : 'warning'}
          size="small"
        />
      ),
    },
    {
      field: 'actions',
      headerName: t('profile.permissions'),
      flex: 1,
      renderCell: (params) => (
        <Stack direction="row" spacing={0.5}>
          {params.value.map((action: string) => (
            <Chip key={action} label={action} size="small" color="primary" variant="outlined" />
          ))}
        </Stack>
      ),
    },
  ]

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 3 }}>
        <UserIcon />
        <Typography variant="h5">{t('profile.title')}</Typography>
      </Box>
      <Grid container spacing={3}>
        {/* Account Info */}
        <Grid
          size={{
            xs: 12,
            lg: 6
          }}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>{t('profile.accountInfo')}</Typography>

              <Box sx={{ textAlign: 'center', mb: 3 }}>
                <Avatar
                  src={user.avatar_url}
                  sx={{ width: 100, height: 100, mx: 'auto', mb: 2 }}
                >
                  <UserIcon sx={{ fontSize: 50 }} />
                </Avatar>
                <Typography variant="h6">{user.name || user.email}</Typography>
                <Chip
                  label={t(`user.roles.${role_info.display_name}`, role_info.display_name)}
                  color={roleColors[user.role] || 'default'}
                  sx={{ mt: 1 }}
                />
              </Box>

              <Divider sx={{ my: 2 }} />

              <Stack spacing={2}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <MailIcon color="action" />
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>{t('profile.email')}</Typography>
                  <Typography sx={{ ml: 'auto' }}>{user.email}</Typography>
                </Box>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <SecurityIcon color="action" />
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>{t('profile.authProvider')}</Typography>
                  <Chip label={user.provider?.toUpperCase() || 'N/A'} size="small" sx={{ ml: 'auto' }} />
                </Box>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <CalendarIcon color="action" />
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>{t('profile.joinedAt')}</Typography>
                  <Typography sx={{ ml: 'auto' }}>{dayjs(user.created_at).format('YYYY-MM-DD HH:mm')}</Typography>
                </Box>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <CalendarIcon color="action" />
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>{t('profile.lastLogin')}</Typography>
                  <Typography sx={{ ml: 'auto' }}>
                    {user.last_login ? dayjs(user.last_login).format('YYYY-MM-DD HH:mm') : '-'}
                  </Typography>
                </Box>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Role & Permissions */}
        <Grid
          size={{
            xs: 12,
            lg: 6
          }}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>{t('profile.rolePermissions')}</Typography>

              <Box sx={{ mb: 2 }}>
                <Typography
                  variant="body2"
                  sx={{
                    color: "text.secondary",
                    mb: 1
                  }}>
                  {t('profile.role')}
                </Typography>
                <Chip label={t(`user.roles.${role_info.display_name}`, role_info.display_name)} color={roleColors[role_info.role] || 'default'} />
              </Box>

              <Typography
                variant="body2"
                sx={{
                  color: "text.secondary",
                  mb: 2
                }}>
                {t(`user.roleDescriptions.${role_info.description}`, role_info.description)}
              </Typography>

              <Divider sx={{ my: 2 }} />

              <Typography
                variant="body2"
                sx={{
                  color: "text.secondary",
                  mb: 1
                }}>
                {t('profile.permissions')}
              </Typography>
              <Stack direction="row" spacing={1} useFlexGap sx={{
                flexWrap: "wrap"
              }}>
                {role_info.permissions.map((perm) => (
                  <Chip key={perm} label={perm} size="small" color="info" variant="outlined" />
                ))}
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Accessible Workflows */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <ClusterIcon color="action" />
                <Typography variant="h6">{t('profile.accessibleWorkflows')}</Typography>
              </Box>
              {workflows && workflows.length > 0 ? (
                <DataGrid
                  rows={workflows}
                  columns={workflowColumns}
                  autoHeight
                  hideFooter
                  disableRowSelectionOnClick
                />
              ) : (
                <Typography
                  sx={{
                    color: "text.secondary",
                    textAlign: 'center',
                    py: 4
                  }}>
                  {t('profile.noWorkflows')}
                </Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Accessible Pipelines */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <ApiIcon color="action" />
                <Typography variant="h6">{t('profile.accessiblePipelines')}</Typography>
              </Box>
              {pipelines && pipelines.length > 0 ? (
                <DataGrid
                  rows={pipelines}
                  columns={pipelineColumns}
                  autoHeight
                  pageSizeOptions={[10]}
                  initialState={{
                    pagination: { paginationModel: { pageSize: 10 } },
                  }}
                  disableRowSelectionOnClick
                />
              ) : (
                <Typography
                  sx={{
                    color: "text.secondary",
                    textAlign: 'center',
                    py: 4
                  }}>
                  {t('profile.noPipelines')}
                </Typography>
              )}
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}
