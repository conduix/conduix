import { useEffect, useState, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Card,
  CardContent,
  Button,
  TextField,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Typography,
  Avatar,
  CircularProgress,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  InputAdornment,
  IconButton,
  Tooltip,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Add as PlusOutlined,
  Delete as DeleteOutlined,
  Edit as EditOutlined,
  AccountTree as ProjectOutlined,
  Groups as TeamOutlined,
  Folder as FolderOutlined,
  Person as UserOutlined,
  Search as SearchIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useAuthStore } from '../store/auth'
import { useSnackbar } from '../hooks/useSnackbar'
import debounce from 'lodash/debounce'

// User search result type
interface UserOption {
  id: string
  email: string
  name: string
  avatar_url?: string
}

// Project owner type
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

// Stat card component
interface StatCardProps {
  title: string
  value: number
  icon: React.ReactNode
  color?: string
}

function StatCard({ title, value, icon, color }: StatCardProps) {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Box sx={{ color: color || 'text.secondary' }}>{icon}</Box>
          <Typography variant="body2" color="text.secondary">
            {title}
          </Typography>
        </Box>
        <Typography variant="h4" sx={{ color: color || 'text.primary' }}>
          {value}
        </Typography>
      </CardContent>
    </Card>
  )
}

export default function ProjectsPage() {
  const { t } = useTranslation()
  const { showSuccess, showError } = useSnackbar()
  const { user: currentUser } = useAuthStore()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [modalVisible, setModalVisible] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [pagination, setPagination] = useState({ page: 0, pageSize: 20, total: 0 })

  // Owner search related states
  const [userOptions, setUserOptions] = useState<UserOption[]>([])
  const [searchingUsers, setSearchingUsers] = useState(false)
  const [selectedOwners, setSelectedOwners] = useState<UserOption[]>([])
  const [searchText, setSearchText] = useState('')

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    alias: '',
    description: '',
    status: 'active',
    tags: '',
  })
  const [formErrors, setFormErrors] = useState({
    name: false,
    alias: false,
    aliasPattern: false,
  })

  // Delete confirmation dialog
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [projectToDelete, setProjectToDelete] = useState<string | null>(null)

  const navigate = useNavigate()

  // User search with debounce - useMemo로 debounce 함수 생성
  const debouncedSearch = useMemo(
    () =>
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

  const fetchProjects = useCallback(async () => {
    try {
      setLoading(true)
      const response = await api.getProjects({
        page: pagination.page + 1,
        page_size: pagination.pageSize,
        search: searchText || undefined,
      })
      if (response.success) {
        const data = response.data as ProjectListResponse
        setProjects(data.projects || [])
        setPagination((prev) => ({ ...prev, total: data.total }))
      }
    } catch (error) {
      showError(t('project.loadError'))
    } finally {
      setLoading(false)
    }
  }, [pagination.page, pagination.pageSize, searchText, showError, t])

  useEffect(() => {
    fetchProjects()
  }, [fetchProjects])

  const handleCreate = () => {
    setEditingProject(null)
    setFormData({
      name: '',
      alias: '',
      description: '',
      status: 'active',
      tags: '',
    })
    setFormErrors({ name: false, alias: false, aliasPattern: false })
    // Set current user as default owner
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
    setFormData({
      name: project.name,
      alias: project.alias,
      description: project.description || '',
      status: project.status,
      tags: project.tags || '',
    })
    setFormErrors({ name: false, alias: false, aliasPattern: false })
    // Set existing owners
    if (project.owners && project.owners.length > 0) {
      const owners: UserOption[] = project.owners.map((po) => ({
        id: po.user.id,
        email: po.user.email,
        name: po.user.name,
        avatar_url: po.user.avatar_url,
      }))
      setSelectedOwners(owners)
    } else if (project.owner) {
      // Backward compatibility for single owner
      setSelectedOwners([
        {
          id: project.owner.id,
          email: project.owner.email,
          name: project.owner.name,
        },
      ])
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
        showSuccess(t('project.deleteSuccess'))
        fetchProjects()
      } else {
        showError(response.error || t('project.deleteError'))
      }
    } catch (error: unknown) {
      const err = error as { response?: { data?: { error?: string } } }
      showError(err.response?.data?.error || t('project.deleteError'))
    }
  }

  const validateForm = (): boolean => {
    const errors = {
      name: !formData.name,
      alias: !formData.alias,
      aliasPattern: !!formData.alias && !/^[a-z0-9-]+$/.test(formData.alias),
    }
    setFormErrors(errors)
    return !Object.values(errors).some(Boolean)
  }

  const handleSubmit = async () => {
    if (!validateForm()) return

    try {
      // Add owner IDs
      const ownerIds = selectedOwners.map((o) => o.id)
      const submitData = { ...formData, owner_ids: ownerIds }

      if (editingProject) {
        const response = await api.updateProject(editingProject.id, submitData)
        if (response.success) {
          showSuccess(t('project.updateSuccess'))
          setModalVisible(false)
          fetchProjects()
        } else {
          showError(response.error || t('project.updateError'))
        }
      } else {
        const response = await api.createProject(submitData)
        if (response.success) {
          showSuccess(t('project.createSuccess'))
          setModalVisible(false)
          fetchProjects()
        } else {
          showError(response.error || t('project.createError'))
        }
      }
    } catch (error: unknown) {
      const err = error as { response?: { data?: { error?: string } } }
      showError(err.response?.data?.error || t('common.save') + ' failed')
    }
  }

  const handleSearch = (value: string) => {
    setSearchText(value)
    setPagination((prev) => ({ ...prev, page: 0 }))
  }

  const getStatusConfig = (status: string): { color: 'success' | 'default' | 'warning'; text: string } => {
    const configs: Record<string, { color: 'success' | 'default' | 'warning'; text: string }> = {
      active: { color: 'success', text: t('status.active') },
      inactive: { color: 'default', text: t('status.inactive') },
      archived: { color: 'warning', text: t('status.archived') },
    }
    return configs[status] || { color: 'default', text: status }
  }

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: t('project.name'),
      flex: 1,
      minWidth: 200,
      renderCell: (params: GridRenderCellParams<Project>) => (
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            cursor: 'pointer',
            '&:hover': { color: 'primary.main' },
          }}
          onClick={() => navigate(`/projects/${params.row.alias || params.row.id}`)}
        >
          <ProjectOutlined fontSize="small" />
          <Typography variant="body2">{params.value}</Typography>
        </Box>
      ),
    },
    {
      field: 'alias',
      headerName: t('project.alias'),
      width: 150,
      renderCell: (params) => (
        <Typography
          variant="body2"
          sx={{
            fontFamily: 'monospace',
            backgroundColor: 'action.hover',
            px: 1,
            py: 0.5,
            borderRadius: 1,
          }}
        >
          {params.value}
        </Typography>
      ),
    },
    {
      field: 'description',
      headerName: t('common.description'),
      flex: 1,
      minWidth: 200,
    },
    {
      field: 'group_count',
      headerName: t('project.groupCount'),
      width: 100,
      align: 'center',
      headerAlign: 'center',
      valueFormatter: ({ value }) => value || 0,
    },
    {
      field: 'status',
      headerName: t('common.status'),
      width: 100,
      renderCell: (params) => {
        const config = getStatusConfig(params.value)
        return <Chip label={config.text} color={config.color} size="small" />
      },
    },
    {
      field: 'created_at',
      headerName: t('common.createdAt'),
      width: 120,
      valueFormatter: ({ value }) => new Date(value).toLocaleDateString(),
    },
    {
      field: 'actions',
      headerName: t('common.actions'),
      width: 100,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Project>) => (
        <Box sx={{ display: 'flex', gap: 0.5 }}>
          <Tooltip title={t('common.edit')}>
            <IconButton size="small" onClick={() => handleEdit(params.row)}>
              <EditOutlined fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title={t('common.delete')}>
            <IconButton
              size="small"
              color="error"
              onClick={() => {
                setProjectToDelete(params.row.id)
                setDeleteDialogOpen(true)
              }}
            >
              <DeleteOutlined fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>
      ),
    },
  ]

  // Calculate statistics
  const activeCount = projects.filter((p) => p.status === 'active').length
  const totalGroups = projects.reduce((sum, p) => sum + (p.group_count || 0), 0)

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5">{t('project.title')}</Typography>
        <Box sx={{ display: 'flex', gap: 2 }}>
          <TextField
            placeholder={t('project.searchPlaceholder')}
            size="small"
            sx={{ width: 250 }}
            onChange={(e) => handleSearch(e.target.value)}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon />
                </InputAdornment>
              ),
            }}
          />
          <Button variant="contained" startIcon={<PlusOutlined />} onClick={handleCreate}>
            {t('project.new')}
          </Button>
        </Box>
      </Box>

      <Box sx={{ display: 'flex', gap: 2, mb: 2, flexWrap: 'wrap' }}>
        <Box sx={{ flex: '1 1 300px', minWidth: 250 }}>
          <StatCard title={t('project.totalProjects')} value={pagination.total} icon={<FolderOutlined />} />
        </Box>
        <Box sx={{ flex: '1 1 300px', minWidth: 250 }}>
          <StatCard
            title={t('project.activeProjects')}
            value={activeCount}
            icon={<ProjectOutlined />}
            color="#4caf50"
          />
        </Box>
        <Box sx={{ flex: '1 1 300px', minWidth: 250 }}>
          <StatCard title={t('project.totalGroups')} value={totalGroups} icon={<TeamOutlined />} />
        </Box>
      </Box>

      <DataGrid
        rows={projects}
        columns={columns}
        loading={loading}
        rowCount={pagination.total}
        pageSizeOptions={[10, 20, 50]}
        paginationModel={{ page: pagination.page, pageSize: pagination.pageSize }}
        paginationMode="server"
        onPaginationModelChange={(model) => {
          setPagination((prev) => ({ ...prev, page: model.page, pageSize: model.pageSize }))
        }}
        autoHeight
        disableRowSelectionOnClick
        sx={{
          '& .MuiDataGrid-cell': {
            py: 1,
          },
        }}
      />

      {/* Create/Edit Modal */}
      <Dialog
        open={modalVisible}
        onClose={() => setModalVisible(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{editingProject ? t('project.edit') : t('project.new')}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: 1 }}>
            <TextField
              label={t('project.name')}
              placeholder={t('project.namePlaceholder')}
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              error={formErrors.name}
              helperText={formErrors.name ? t('project.nameRequired') : ''}
              required
              fullWidth
            />

            <TextField
              label={t('project.alias')}
              placeholder={t('project.aliasPlaceholder')}
              value={formData.alias}
              onChange={(e) => setFormData({ ...formData, alias: e.target.value })}
              error={formErrors.alias || formErrors.aliasPattern}
              helperText={
                formErrors.alias
                  ? t('project.aliasRequired')
                  : formErrors.aliasPattern
                    ? t('project.aliasPattern')
                    : t('project.aliasHelp')
              }
              required
              fullWidth
            />

            <TextField
              label={t('common.description')}
              placeholder={t('project.descriptionPlaceholder')}
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              multiline
              rows={3}
              fullWidth
            />

            {/* Owner selection */}
            <FormControl fullWidth>
              <InputLabel>{t('project.owners')}</InputLabel>
              <Select
                multiple
                value={selectedOwners.map((o) => o.id)}
                onChange={(e) => {
                  const values = e.target.value as string[]
                  const newOwners = values.map((id) => {
                    const existing = selectedOwners.find((o) => o.id === id)
                    if (existing) return existing
                    const fromOptions = userOptions.find((o) => o.id === id)
                    return fromOptions || { id, email: '', name: '' }
                  })
                  setSelectedOwners(newOwners)
                }}
                label={t('project.owners')}
                onOpen={() => {}}
                renderValue={(selected) => (
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                    {selectedOwners
                      .filter((o) => selected.includes(o.id))
                      .map((owner) => (
                        <Chip
                          key={owner.id}
                          avatar={<Avatar src={owner.avatar_url} sx={{ width: 20, height: 20 }}><UserOutlined fontSize="small" /></Avatar>}
                          label={owner.name || owner.email}
                          size="small"
                        />
                      ))}
                  </Box>
                )}
                MenuProps={{
                  PaperProps: {
                    sx: { maxHeight: 300 },
                  },
                }}
              >
                {/* Search input at top */}
                <Box sx={{ px: 2, py: 1, position: 'sticky', top: 0, bgcolor: 'background.paper', zIndex: 1 }}>
                  <TextField
                    size="small"
                    placeholder={t('project.ownersPlaceholder')}
                    fullWidth
                    onChange={(e) => handleUserSearch(e.target.value)}
                    onClick={(e) => e.stopPropagation()}
                    onKeyDown={(e) => e.stopPropagation()}
                    InputProps={{
                      endAdornment: searchingUsers ? (
                        <InputAdornment position="end">
                          <CircularProgress size={16} />
                        </InputAdornment>
                      ) : null,
                    }}
                  />
                </Box>
                {/* Currently selected owners */}
                {selectedOwners.map((owner) => (
                  <MenuItem key={owner.id} value={owner.id}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Avatar src={owner.avatar_url} sx={{ width: 24, height: 24 }}>
                        <UserOutlined fontSize="small" />
                      </Avatar>
                      <Box>
                        <Typography variant="body2">{owner.name || owner.email}</Typography>
                        <Typography variant="caption" color="text.secondary">
                          {owner.email}
                        </Typography>
                      </Box>
                    </Box>
                  </MenuItem>
                ))}
                {/* Search results (not already selected) */}
                {userOptions
                  .filter((opt) => !selectedOwners.some((s) => s.id === opt.id))
                  .map((user) => (
                    <MenuItem key={user.id} value={user.id}>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <Avatar src={user.avatar_url} sx={{ width: 24, height: 24 }}>
                          <UserOutlined fontSize="small" />
                        </Avatar>
                        <Box>
                          <Typography variant="body2">{user.name || user.email}</Typography>
                          <Typography variant="caption" color="text.secondary">
                            {user.email}
                          </Typography>
                        </Box>
                      </Box>
                    </MenuItem>
                  ))}
              </Select>
            </FormControl>

            {editingProject && (
              <FormControl fullWidth>
                <InputLabel>{t('common.status')}</InputLabel>
                <Select
                  value={formData.status}
                  onChange={(e) => setFormData({ ...formData, status: e.target.value })}
                  label={t('common.status')}
                >
                  <MenuItem value="active">{t('status.active')}</MenuItem>
                  <MenuItem value="inactive">{t('status.inactive')}</MenuItem>
                  <MenuItem value="archived">{t('status.archived')}</MenuItem>
                </Select>
              </FormControl>
            )}

            <TextField
              label={t('common.tags')}
              placeholder="tag1, tag2, tag3"
              value={formData.tags}
              onChange={(e) => setFormData({ ...formData, tags: e.target.value })}
              helperText={t('common.tagsHelp')}
              fullWidth
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setModalVisible(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleSubmit}>
            {t('common.save')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>{t('project.deleteConfirm')}</DialogTitle>
        <DialogContent>
          <Typography>{t('project.deleteWarning')}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>{t('common.cancel')}</Button>
          <Button
            color="error"
            variant="contained"
            onClick={() => {
              if (projectToDelete) {
                handleDelete(projectToDelete)
              }
              setDeleteDialogOpen(false)
              setProjectToDelete(null)
            }}
          >
            {t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
