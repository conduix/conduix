import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Card,
  CardContent,
  Chip,
  Button,
  TextField,
  Select,
  MenuItem,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Typography,
  Avatar,
  IconButton,
  FormControl,
  InputLabel,
  Tab,
  Tabs,
  InputAdornment,
  Tooltip,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Person as UserOutlined,
  Search as SearchOutlined,
  Edit as EditOutlined,
  Delete as DeleteOutlined,
  Add as PlusOutlined,
  Security as SafetyCertificateOutlined,
} from '@mui/icons-material'
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

const roleColors: Record<string, 'error' | 'primary' | 'success' | 'default'> = {
  admin: 'error',
  operator: 'primary',
  viewer: 'success',
}

const roleDisplayNames: Record<string, string> = {
  admin: '관리자',
  operator: '운영자',
  viewer: '뷰어',
}

// TabPanel component
interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel(props: TabPanelProps) {
  const { children, value, index, ...other } = props
  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      id={`users-tabpanel-${index}`}
      aria-labelledby={`users-tab-${index}`}
      {...other}
    >
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  )
}

export default function UsersPage() {
  const { showSuccess, showError } = useSnackbar()
  const [loading, setLoading] = useState(true)
  const [users, setUsers] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(20)
  const [search, setSearch] = useState('')
  const [roleFilter, setRoleFilter] = useState<string>('')

  const [permissions, setPermissions] = useState<Permission[]>([])
  const [permissionsLoading, setPermissionsLoading] = useState(false)

  const [roleModalVisible, setRoleModalVisible] = useState(false)
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [newRole, setNewRole] = useState('')

  const [permissionModalVisible, setPermissionModalVisible] = useState(false)
  const [permissionForm, setPermissionForm] = useState({
    user_id: '',
    resource_type: '',
    resource_id: '',
    actions: [] as string[],
  })
  const [permissionFormErrors, setPermissionFormErrors] = useState({
    user_id: false,
    resource_type: false,
    resource_id: false,
    actions: false,
  })

  const [activeTab, setActiveTab] = useState(0)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [permissionToDelete, setPermissionToDelete] = useState<string | null>(null)

  const fetchUsers = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      params.append('page', String(page + 1))
      params.append('page_size', String(pageSize))
      if (search) params.append('search', search)
      if (roleFilter) params.append('role', roleFilter)

      const response = await api.get<ApiResponse<UserListResponse>>(`/users?${params}`)
      if (response.data?.success && response.data.data) {
        setUsers(response.data.data.users || [])
        setTotal(response.data.data.total)
      }
    } catch (error) {
      showError('사용자 목록을 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, search, roleFilter, showError])

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

  const fetchPermissions = async () => {
    setPermissionsLoading(true)
    try {
      const response = await api.get<ApiResponse<Permission[]>>('/permissions')
      if (response.data?.success && response.data.data) {
        setPermissions(response.data.data)
      }
    } catch (error) {
      showError('권한 목록을 불러오는데 실패했습니다')
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
        showSuccess('역할이 수정되었습니다')
        setRoleModalVisible(false)
        fetchUsers()
      } else {
        showError(response.data?.error || '역할 수정에 실패했습니다')
      }
    } catch (error) {
      showError('역할 수정에 실패했습니다')
    }
  }

  const validatePermissionForm = (): boolean => {
    const errors = {
      user_id: !permissionForm.user_id,
      resource_type: !permissionForm.resource_type,
      resource_id: !permissionForm.resource_id,
      actions: permissionForm.actions.length === 0,
    }
    setPermissionFormErrors(errors)
    return !Object.values(errors).some(Boolean)
  }

  const handleCreatePermission = async () => {
    if (!validatePermissionForm()) return

    try {
      const payload = {
        ...permissionForm,
        actions: permissionForm.actions.join(','),
      }
      const response = await api.post<ApiResponse<Permission>>('/permissions', payload)
      if (response.data?.success) {
        showSuccess('권한이 생성되었습니다')
        setPermissionModalVisible(false)
        setPermissionForm({ user_id: '', resource_type: '', resource_id: '', actions: [] })
        fetchPermissions()
      } else {
        showError(response.data?.error || '권한 생성에 실패했습니다')
      }
    } catch (error) {
      showError('권한 생성에 실패했습니다')
    }
  }

  const handleDeletePermission = async (id: string) => {
    try {
      const response = await api.delete<ApiResponse<unknown>>(`/permissions/${id}`)
      if (response.data?.success) {
        showSuccess('권한이 삭제되었습니다')
        fetchPermissions()
      } else {
        showError(response.data?.error || '권한 삭제에 실패했습니다')
      }
    } catch (error) {
      showError('권한 삭제에 실패했습니다')
    }
  }

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setActiveTab(newValue)
    if (newValue === 1) {
      fetchPermissions()
    }
  }

  // User columns for DataGrid
  const userColumns: GridColDef[] = [
    {
      field: 'user',
      headerName: '사용자',
      flex: 1,
      minWidth: 250,
      renderCell: (params: GridRenderCellParams<User>) => (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, py: 1 }}>
          <Avatar src={params.row.avatar_url} sx={{ width: 32, height: 32 }}>
            <UserOutlined fontSize="small" />
          </Avatar>
          <Box>
            <Typography variant="body2">{params.row.name || params.row.email}</Typography>
            <Typography variant="caption" color="text.secondary">
              {params.row.email}
            </Typography>
          </Box>
        </Box>
      ),
    },
    {
      field: 'role',
      headerName: '역할',
      width: 120,
      renderCell: (params) => (
        <Chip
          label={roleDisplayNames[params.value] || params.value}
          color={roleColors[params.value] || 'default'}
          size="small"
        />
      ),
    },
    {
      field: 'provider',
      headerName: '인증 제공자',
      width: 130,
      renderCell: (params) => (
        <Chip label={params.value?.toUpperCase() || 'N/A'} size="small" variant="outlined" />
      ),
    },
    {
      field: 'permission_count',
      headerName: '권한 수',
      width: 100,
      valueFormatter: ({ value }) => value || 0,
    },
    {
      field: 'created_at',
      headerName: '가입일',
      width: 120,
      valueFormatter: ({ value }) => dayjs(value).format('YYYY-MM-DD'),
    },
    {
      field: 'last_login',
      headerName: '마지막 로그인',
      width: 150,
      valueFormatter: ({ value }) =>
        value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '-',
    },
    {
      field: 'actions',
      headerName: '작업',
      width: 130,
      sortable: false,
      renderCell: (params: GridRenderCellParams<User>) => (
        <Button
          variant="outlined"
          size="small"
          startIcon={<EditOutlined />}
          onClick={() => {
            setSelectedUser(params.row)
            setNewRole(params.row.role)
            setRoleModalVisible(true)
          }}
        >
          역할 변경
        </Button>
      ),
    },
  ]

  // Permission columns for DataGrid
  const permissionColumns: GridColDef[] = [
    {
      field: 'user',
      headerName: '사용자',
      flex: 1,
      minWidth: 200,
      renderCell: (params: GridRenderCellParams<Permission>) => (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Avatar src={params.row.user?.avatar_url} sx={{ width: 24, height: 24 }}>
            <UserOutlined fontSize="small" />
          </Avatar>
          <Typography variant="body2">
            {params.row.user?.name || params.row.user?.email || params.row.user_id}
          </Typography>
        </Box>
      ),
    },
    {
      field: 'resource_type',
      headerName: '리소스 타입',
      width: 150,
      renderCell: (params) => {
        const typeNames: Record<string, string> = {
          provider: '데이터 제공자',
          group: '파이프라인 그룹',
          pipeline: '파이프라인',
        }
        return <Chip label={typeNames[params.value] || params.value} size="small" />
      },
    },
    {
      field: 'resource_id',
      headerName: '리소스 ID',
      flex: 1,
      minWidth: 200,
    },
    {
      field: 'actions_list',
      headerName: '권한',
      width: 250,
      valueGetter: (params) => params.row.actions,
      renderCell: (params: GridRenderCellParams<Permission>) => (
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
          {params.row.actions.split(',').map((action: string) => (
            <Chip key={action} label={action.trim()} color="primary" size="small" />
          ))}
        </Box>
      ),
    },
    {
      field: 'created_at',
      headerName: '생성일',
      width: 120,
      valueFormatter: ({ value }) => dayjs(value).format('YYYY-MM-DD'),
    },
    {
      field: 'delete_action',
      headerName: '작업',
      width: 80,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Permission>) => (
        <Tooltip title="권한 삭제">
          <IconButton
            color="error"
            size="small"
            onClick={() => {
              setPermissionToDelete(params.row.id)
              setDeleteConfirmOpen(true)
            }}
          >
            <DeleteOutlined />
          </IconButton>
        </Tooltip>
      ),
    },
  ]

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 3 }}>
        <UserOutlined />
        <Typography variant="h5">사용자 관리</Typography>
      </Box>

      <Box sx={{ borderBottom: 1, borderColor: 'divider', mb: 2 }}>
        <Tabs value={activeTab} onChange={handleTabChange}>
          <Tab
            icon={<UserOutlined />}
            iconPosition="start"
            label={`사용자 (${total})`}
          />
          <Tab
            icon={<SafetyCertificateOutlined />}
            iconPosition="start"
            label="리소스 권한"
          />
        </Tabs>
      </Box>

      <TabPanel value={activeTab} index={0}>
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', gap: 2, mb: 2, flexWrap: 'wrap' }}>
              <TextField
                placeholder="이메일 또는 이름 검색"
                size="small"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                sx={{ width: 250 }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <SearchOutlined />
                    </InputAdornment>
                  ),
                }}
              />
              <FormControl size="small" sx={{ width: 150 }}>
                <InputLabel>역할 필터</InputLabel>
                <Select
                  value={roleFilter}
                  onChange={(e) => setRoleFilter(e.target.value)}
                  label="역할 필터"
                >
                  <MenuItem value="">전체</MenuItem>
                  <MenuItem value="admin">관리자</MenuItem>
                  <MenuItem value="operator">운영자</MenuItem>
                  <MenuItem value="viewer">뷰어</MenuItem>
                </Select>
              </FormControl>
            </Box>

            <DataGrid
              rows={users}
              columns={userColumns}
              loading={loading}
              rowCount={total}
              pageSizeOptions={[10, 20, 50]}
              paginationModel={{ page, pageSize }}
              paginationMode="server"
              onPaginationModelChange={(model) => {
                setPage(model.page)
                setPageSize(model.pageSize)
              }}
              autoHeight
              disableRowSelectionOnClick
              getRowHeight={() => 'auto'}
              sx={{
                '& .MuiDataGrid-cell': {
                  py: 1,
                },
              }}
            />
          </CardContent>
        </Card>
      </TabPanel>

      <TabPanel value={activeTab} index={1}>
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: 2 }}>
              <Button
                variant="contained"
                startIcon={<PlusOutlined />}
                onClick={() => setPermissionModalVisible(true)}
              >
                권한 추가
              </Button>
            </Box>

            {permissions.length > 0 ? (
              <DataGrid
                rows={permissions}
                columns={permissionColumns}
                loading={permissionsLoading}
                pageSizeOptions={[10, 20, 50]}
                initialState={{
                  pagination: { paginationModel: { pageSize: 20 } },
                }}
                autoHeight
                disableRowSelectionOnClick
                getRowHeight={() => 'auto'}
                sx={{
                  '& .MuiDataGrid-cell': {
                    py: 1,
                  },
                }}
              />
            ) : (
              <Box sx={{ textAlign: 'center', py: 8, color: 'text.secondary' }}>
                <SafetyCertificateOutlined sx={{ fontSize: 48, mb: 2, opacity: 0.5 }} />
                <Typography>등록된 리소스 권한이 없습니다</Typography>
              </Box>
            )}
          </CardContent>
        </Card>
      </TabPanel>

      {/* 역할 변경 모달 */}
      <Dialog open={roleModalVisible} onClose={() => setRoleModalVisible(false)} maxWidth="sm" fullWidth>
        <DialogTitle>역할 변경</DialogTitle>
        <DialogContent>
          {selectedUser && (
            <Box sx={{ pt: 1 }}>
              <Typography sx={{ mb: 1 }}>
                <strong>사용자:</strong> {selectedUser.name || selectedUser.email}
              </Typography>
              <Typography sx={{ mb: 2, display: 'flex', alignItems: 'center', gap: 1 }}>
                <strong>현재 역할:</strong>
                <Chip
                  label={roleDisplayNames[selectedUser.role]}
                  color={roleColors[selectedUser.role]}
                  size="small"
                />
              </Typography>
              <FormControl fullWidth>
                <InputLabel>새 역할</InputLabel>
                <Select value={newRole} onChange={(e) => setNewRole(e.target.value)} label="새 역할">
                  <MenuItem value="admin">
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Chip label="관리자" color="error" size="small" />
                      <Typography variant="caption">- 모든 권한</Typography>
                    </Box>
                  </MenuItem>
                  <MenuItem value="operator">
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Chip label="운영자" color="primary" size="small" />
                      <Typography variant="caption">- 파이프라인 생성/수정/실행</Typography>
                    </Box>
                  </MenuItem>
                  <MenuItem value="viewer">
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Chip label="뷰어" color="success" size="small" />
                      <Typography variant="caption">- 읽기 전용</Typography>
                    </Box>
                  </MenuItem>
                </Select>
              </FormControl>
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRoleModalVisible(false)}>취소</Button>
          <Button variant="contained" onClick={handleRoleChange}>
            변경
          </Button>
        </DialogActions>
      </Dialog>

      {/* 권한 추가 모달 */}
      <Dialog
        open={permissionModalVisible}
        onClose={() => {
          setPermissionModalVisible(false)
          setPermissionForm({ user_id: '', resource_type: '', resource_id: '', actions: [] })
          setPermissionFormErrors({ user_id: false, resource_type: false, resource_id: false, actions: false })
        }}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>리소스 권한 추가</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: 1 }}>
            <TextField
              label="사용자 ID"
              placeholder="사용자 UUID"
              value={permissionForm.user_id}
              onChange={(e) => setPermissionForm({ ...permissionForm, user_id: e.target.value })}
              error={permissionFormErrors.user_id}
              helperText={permissionFormErrors.user_id ? '사용자 ID를 입력하세요' : ''}
              required
              fullWidth
            />
            <FormControl fullWidth required error={permissionFormErrors.resource_type}>
              <InputLabel>리소스 타입</InputLabel>
              <Select
                value={permissionForm.resource_type}
                onChange={(e) => setPermissionForm({ ...permissionForm, resource_type: e.target.value })}
                label="리소스 타입"
              >
                <MenuItem value="provider">데이터 제공자</MenuItem>
                <MenuItem value="group">파이프라인 그룹</MenuItem>
                <MenuItem value="pipeline">파이프라인</MenuItem>
              </Select>
              {permissionFormErrors.resource_type && (
                <Typography variant="caption" color="error" sx={{ mt: 0.5, ml: 2 }}>
                  리소스 타입을 선택하세요
                </Typography>
              )}
            </FormControl>
            <TextField
              label="리소스 ID"
              placeholder="리소스 UUID"
              value={permissionForm.resource_id}
              onChange={(e) => setPermissionForm({ ...permissionForm, resource_id: e.target.value })}
              error={permissionFormErrors.resource_id}
              helperText={permissionFormErrors.resource_id ? '리소스 ID를 입력하세요' : ''}
              required
              fullWidth
            />
            <FormControl fullWidth required error={permissionFormErrors.actions}>
              <InputLabel>권한</InputLabel>
              <Select
                multiple
                value={permissionForm.actions}
                onChange={(e) =>
                  setPermissionForm({ ...permissionForm, actions: e.target.value as string[] })
                }
                label="권한"
                renderValue={(selected) => (
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                    {selected.map((value) => (
                      <Chip key={value} label={value} size="small" />
                    ))}
                  </Box>
                )}
              >
                <MenuItem value="read">read</MenuItem>
                <MenuItem value="write">write</MenuItem>
                <MenuItem value="execute">execute</MenuItem>
                <MenuItem value="delete">delete</MenuItem>
                <MenuItem value="admin">admin</MenuItem>
              </Select>
              {permissionFormErrors.actions && (
                <Typography variant="caption" color="error" sx={{ mt: 0.5, ml: 2 }}>
                  권한을 선택하세요
                </Typography>
              )}
            </FormControl>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => {
              setPermissionModalVisible(false)
              setPermissionForm({ user_id: '', resource_type: '', resource_id: '', actions: [] })
              setPermissionFormErrors({ user_id: false, resource_type: false, resource_id: false, actions: false })
            }}
          >
            취소
          </Button>
          <Button variant="contained" onClick={handleCreatePermission}>
            추가
          </Button>
        </DialogActions>
      </Dialog>

      {/* 권한 삭제 확인 다이얼로그 */}
      <Dialog open={deleteConfirmOpen} onClose={() => setDeleteConfirmOpen(false)}>
        <DialogTitle>권한 삭제</DialogTitle>
        <DialogContent>
          <Typography>권한을 삭제하시겠습니까?</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteConfirmOpen(false)}>취소</Button>
          <Button
            color="error"
            variant="contained"
            onClick={() => {
              if (permissionToDelete) {
                handleDeletePermission(permissionToDelete)
              }
              setDeleteConfirmOpen(false)
              setPermissionToDelete(null)
            }}
          >
            삭제
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
