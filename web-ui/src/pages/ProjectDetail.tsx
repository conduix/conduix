import { useEffect, useState, useCallback, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  CardHeader,
  Typography,
  Box,
  Button,
  Chip,
  Tabs,
  Tab,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Breadcrumbs,
  Link,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  FormHelperText,
  Avatar,
  IconButton,
  Autocomplete,
} from '@mui/material'
import {
  ArrowBack as ArrowBackIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  Groups as GroupsIcon,
  Settings as SettingsIcon,
  Person as PersonIcon,
  Add as AddIcon,
  Storage as StorageIcon,
  Visibility as VisibilityIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import { ConfirmDialog } from '../components/common/ConfirmDialog'
import debounce from 'lodash/debounce'

interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel(props: TabPanelProps) {
  const { children, value, index, ...other } = props
  return (
    <div role="tabpanel" hidden={value !== index} {...other}>
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  )
}

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
  cluster_id?: string
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
  json_schema?: string
  created_at: string
  updated_at: string
  parent?: DataType
}

export default function ProjectDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { showSuccess, showError } = useSnackbar()
  const [project, setProject] = useState<Project | null>(null)
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [loading, setLoading] = useState(true)
  const [tabValue, setTabValue] = useState(0)
  const [editModalOpen, setEditModalOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    alias: '',
    description: '',
    status: 'active',
    tags: '',
  })

  // Workflow 관련 상태
  const [workflowModalOpen, setWorkflowModalOpen] = useState(false)
  const [editingWorkflow, setEditingWorkflow] = useState<Workflow | null>(null)
  const [workflowForm, setWorkflowForm] = useState({
    name: '',
    slug: '',
    type: 'batch' as 'batch' | 'realtime',
    description: '',
    cluster_id: '', // 실행 대상 cluster (빈 값이면 서버가 default cluster로 폴백)
  })
  const [clusterOptions, setClusterOptions] = useState<Array<{ id: string; name: string }>>([])
  const [workflowSaving, setWorkflowSaving] = useState(false)
  const [deleteWorkflowId, setDeleteWorkflowId] = useState<string | null>(null)

  // 담당자 검색 관련 상태
  const [userOptions, setUserOptions] = useState<UserOption[]>([])
  const [searchingUsers, setSearchingUsers] = useState(false)
  const [selectedOwners, setSelectedOwners] = useState<UserOption[]>([])

  // DataType 관련 상태
  const [dataTypes, setDataTypes] = useState<DataType[]>([])
  const [dataTypeModalOpen, setDataTypeModalOpen] = useState(false)
  const [editingDataType, setEditingDataType] = useState<DataType | null>(null)
  const [dataTypeForm, setDataTypeForm] = useState({
    display_name: '',
    name: '',
    description: '',
    category: 'master',
    parent_id: '',
    key_field: 'id',
    id_fields: [] as string[],
    json_schema: '',
  })
  const [dataTypeSaving, setDataTypeSaving] = useState(false)
  const [selectedParentId, setSelectedParentId] = useState<string | undefined>(undefined)
  const [deleteDataTypeId, setDeleteDataTypeId] = useState<string | null>(null)

  // 사용자 검색 (디바운스) - useMemo로 debounce 함수 생성
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

  const fetchProjectData = useCallback(async () => {
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
      showError(t('project.loadError'))
    } finally {
      setLoading(false)
    }
  }, [id, showError, t])

  useEffect(() => {
    if (id) {
      fetchProjectData()
    }
  }, [id, fetchProjectData])

  const handleEdit = () => {
    if (!project) return
    setFormData({
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
      setSelectedOwners([{
        id: project.owner.id,
        email: project.owner.email,
        name: project.owner.name,
      }])
    } else {
      setSelectedOwners([])
    }
    setUserOptions([])
    setEditModalOpen(true)
  }

  const handleEditSubmit = async () => {
    try {
      const ownerIds = selectedOwners.map(o => o.id)
      const submitData = { ...formData, owner_ids: ownerIds }
      const response = await api.updateProject(id!, submitData)
      if (response.success) {
        showSuccess(t('project.updateSuccess'))
        setEditModalOpen(false)
        fetchProjectData()
      } else {
        showError(response.error || t('project.updateError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('project.updateError'))
    }
  }

  const handleDelete = async () => {
    try {
      const response = await api.deleteProject(id!)
      if (response.success) {
        showSuccess(t('project.deleteSuccess'))
        navigate('/projects')
      } else {
        showError(response.error || t('project.deleteError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('project.deleteError'))
    }
  }

  // 워크플로우 dialog에서 실행 대상 cluster를 고를 수 있도록 목록을 로드한다.
  const loadClusterOptions = async () => {
    try {
      const res = await api.getClusters()
      const list = (res.data || []) as Array<{ id: string; name: string }>
      setClusterOptions(list.map((c) => ({ id: c.id, name: c.name })))
    } catch {
      setClusterOptions([])
    }
  }

  // Workflow CRUD handlers
  const handleCreateWorkflow = () => {
    setEditingWorkflow(null)
    setWorkflowForm({ name: '', slug: '', type: 'batch', description: '', cluster_id: '' })
    loadClusterOptions()
    setWorkflowModalOpen(true)
  }

  const handleEditWorkflow = (workflow: Workflow) => {
    setEditingWorkflow(workflow)
    setWorkflowForm({
      name: workflow.name,
      slug: workflow.slug,
      type: workflow.type,
      description: workflow.description,
      cluster_id: workflow.cluster_id || '',
    })
    loadClusterOptions()
    setWorkflowModalOpen(true)
  }

  const handleWorkflowSubmit = async () => {
    try {
      setWorkflowSaving(true)

      if (editingWorkflow) {
        const response = await api.updateWorkflow(editingWorkflow.id, workflowForm)
        if (response.success) {
          showSuccess(t('workflow.updateSuccess'))
          setWorkflowModalOpen(false)
          fetchProjectData()
        } else {
          showError(response.error || t('workflow.updateError'))
        }
      } else {
        if (!project?.id) {
          showError(t('project.notFound'))
          return
        }
        const response = await api.createWorkflow({
          project_id: project.id,
          ...workflowForm,
        })
        if (response.success) {
          showSuccess(t('workflow.createSuccess'))
          setWorkflowModalOpen(false)
          fetchProjectData()
        } else {
          showError(response.error || t('workflow.createError'))
        }
      }
    } catch (error: unknown) {
      const errorMsg = editingWorkflow ? t('workflow.updateError') : t('workflow.createError')
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || errorMsg)
    } finally {
      setWorkflowSaving(false)
    }
  }

  const handleDeleteWorkflow = async () => {
    if (!deleteWorkflowId) return
    try {
      const response = await api.deleteWorkflow(deleteWorkflowId)
      if (response.success) {
        showSuccess(t('workflow.deleteSuccess'))
        setDeleteWorkflowId(null)
        fetchProjectData()
      } else {
        showError(response.error || t('workflow.deleteError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('workflow.deleteError'))
    }
  }

  // DataType CRUD handlers
  const handleCreateDataType = () => {
    setEditingDataType(null)
    setSelectedParentId(undefined)
    setDataTypeForm({
      display_name: '',
      name: '',
      description: '',
      category: 'master',
      parent_id: '',
      key_field: 'id',
      id_fields: [],
      json_schema: '',
    })
    setDataTypeModalOpen(true)
  }

  const handleEditDataType = (dataType: DataType) => {
    setEditingDataType(dataType)
    setSelectedParentId(dataType.parent_id || undefined)
    let idFieldsArray: string[] = []
    if (dataType.id_fields) {
      try {
        idFieldsArray = JSON.parse(dataType.id_fields)
      } catch {
        idFieldsArray = []
      }
    }
    setDataTypeForm({
      display_name: dataType.display_name,
      name: dataType.name,
      description: dataType.description || '',
      category: dataType.category || 'master',
      parent_id: dataType.parent_id || '',
      key_field: !dataType.parent_id && idFieldsArray.length > 0 ? idFieldsArray[0] : 'id',
      id_fields: dataType.parent_id ? idFieldsArray : [],
      json_schema: dataType.json_schema || '',
    })
    setDataTypeModalOpen(true)
  }

  // 부모 체인을 따라 모든 조상을 찾아 복합키 생성
  const buildCompositeKeyFields = (parentId: string): string[] => {
    const keyFields: string[] = []
    let currentId: string | undefined = parentId

    while (currentId) {
      const dataType = dataTypes.find(dt => dt.id === currentId)
      if (dataType) {
        keyFields.unshift(`${dataType.name}_id`)
        currentId = dataType.parent_id || undefined
      } else {
        break
      }
    }

    keyFields.push('id')
    return keyFields
  }

  const handleParentChange = (parentId: string | undefined) => {
    setSelectedParentId(parentId)
    if (parentId) {
      const keyFields = buildCompositeKeyFields(parentId)
      setDataTypeForm(prev => ({
        ...prev,
        parent_id: parentId,
        id_fields: keyFields,
      }))
    } else {
      setDataTypeForm(prev => ({
        ...prev,
        parent_id: '',
        key_field: 'id',
      }))
    }
  }

  const handleDataTypeSubmit = async () => {
    try {
      setDataTypeSaving(true)

      const idFieldsArray = dataTypeForm.parent_id
        ? dataTypeForm.id_fields.map(f => f.trim()).filter(f => f)
        : dataTypeForm.key_field ? [dataTypeForm.key_field.trim()].filter(f => f) : []

      const submitData = {
        display_name: dataTypeForm.display_name,
        name: dataTypeForm.name,
        description: dataTypeForm.description,
        category: dataTypeForm.category,
        parent_id: dataTypeForm.parent_id || null,
        id_fields: idFieldsArray,
        json_schema: dataTypeForm.json_schema || null,
      }

      if (editingDataType) {
        const response = await api.updateDataType(editingDataType.id, submitData)
        if (response.success) {
          showSuccess(t('dataModel.updateSuccess'))
          setDataTypeModalOpen(false)
          fetchProjectData()
        } else {
          showError(response.error || t('dataModel.updateError'))
        }
      } else {
        if (!project?.id) {
          showError(t('project.notFound'))
          return
        }
        const response = await api.createDataType({
          project_id: project.id,
          ...submitData,
        })
        if (response.success) {
          showSuccess(t('dataModel.createSuccess'))
          setDataTypeModalOpen(false)
          fetchProjectData()
        } else {
          showError(response.error || t('dataModel.createError'))
        }
      }
    } catch (error: unknown) {
      const errorMsg = editingDataType ? t('dataModel.updateError') : t('dataModel.createError')
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || errorMsg)
    } finally {
      setDataTypeSaving(false)
    }
  }

  const handleDeleteDataType = async () => {
    if (!deleteDataTypeId) return
    try {
      const response = await api.deleteDataType(deleteDataTypeId)
      if (response.success) {
        showSuccess(t('dataModel.deleteSuccess'))
        setDeleteDataTypeId(null)
        fetchProjectData()
      } else {
        showError(response.error || t('dataModel.deleteError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('dataModel.deleteError'))
    }
  }

  const getCategoryConfig = (category: string) => {
    const configs: Record<string, { color: 'primary' | 'success' | 'warning' | 'secondary' | 'info' | 'default'; text: string }> = {
      master: { color: 'primary', text: t('dataModel.categories.master') },
      transaction: { color: 'success', text: t('dataModel.categories.transaction') },
      log: { color: 'warning', text: t('dataModel.categories.log') },
      metric: { color: 'secondary', text: t('dataModel.categories.metric') },
      reference: { color: 'info', text: t('dataModel.categories.reference') },
    }
    return configs[category] || { color: 'default' as const, text: category }
  }

  const getStatusConfig = (status: string) => {
    const configs: Record<string, { color: 'success' | 'default' | 'warning'; text: string }> = {
      active: { color: 'success', text: t('status.active') },
      inactive: { color: 'default', text: t('status.inactive') },
      archived: { color: 'warning', text: t('status.archived') },
    }
    return configs[status] || { color: 'default' as const, text: status }
  }

  const getWorkflowStatusConfig = (status: string) => {
    const configs: Record<string, { color: 'default' | 'success' | 'warning' | 'error' | 'primary'; text: string }> = {
      idle: { color: 'default', text: t('pipeline.status.idle') },
      running: { color: 'success', text: t('pipeline.status.running') },
      paused: { color: 'warning', text: t('pipeline.status.paused') },
      stopped: { color: 'default', text: t('pipeline.status.stopped') },
      error: { color: 'error', text: t('pipeline.status.error') },
      completed: { color: 'primary', text: t('pipeline.status.completed') },
    }
    return configs[status] || { color: 'default' as const, text: status }
  }

  // DataType depth calculation
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

  // Sort dataTypes hierarchically
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

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue)
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress />
      </Box>
    )
  }

  if (!project) {
    return (
      <Box sx={{ textAlign: 'center', py: 6 }}>
        <Typography
          sx={{
            color: "text.secondary",
            mb: 2
          }}>{t('project.notFound')}</Typography>
        <Button variant="contained" onClick={() => navigate('/projects')}>
          {t('common.back')}
        </Button>
      </Box>
    );
  }

  const statusConfig = getStatusConfig(project.status)

  return (
    <Box>
      <Breadcrumbs sx={{ mb: 2 }}>
        <Link
          component="button"
          underline="hover"
          color="inherit"
          onClick={() => navigate('/projects')}
        >
          {t('project.title')}
        </Link>
        <Typography sx={{
          color: "text.primary"
        }}>{project.name}</Typography>
      </Breadcrumbs>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/projects')}>
            {t('common.list')}
          </Button>
          <Typography variant="h5">{project.name}</Typography>
          <Chip label={statusConfig.text} color={statusConfig.color} size="small" />
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button startIcon={<EditIcon />} onClick={handleEdit}>
            {t('common.edit')}
          </Button>
          <Button
            color="error"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            {t('common.delete')}
          </Button>
        </Box>
      </Box>
      <Tabs value={tabValue} onChange={handleTabChange} sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tab icon={<SettingsIcon />} iconPosition="start" label={t('project.overview')} />
        <Tab icon={<StorageIcon />} iconPosition="start" label={`${t('dataModel.title')} (${dataTypes.length})`} />
        <Tab icon={<GroupsIcon />} iconPosition="start" label={t('project.workflows', { count: workflows.length })} />
      </Tabs>
      <TabPanel value={tabValue} index={0}>
        <Card>
          <CardContent>
            <TableContainer component={Paper} variant="outlined">
              <Table>
                <TableBody>
                  <TableRow>
                    <TableCell component="th" sx={{ width: 150, bgcolor: 'grey.50' }}>{t('project.name')}</TableCell>
                    <TableCell>{project.name}</TableCell>
                    <TableCell component="th" sx={{ width: 150, bgcolor: 'grey.50' }}>{t('project.alias')}</TableCell>
                    <TableCell><code>{project.alias}</code></TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.status')}</TableCell>
                    <TableCell>
                      <Chip label={statusConfig.text} color={statusConfig.color} size="small" />
                    </TableCell>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.owner')}</TableCell>
                    <TableCell>
                      {project.owners && project.owners.length > 0 ? (
                        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                          {project.owners.map(po => (
                            <Chip
                              key={po.id}
                              avatar={<Avatar src={po.user.avatar_url}><PersonIcon /></Avatar>}
                              label={po.user.name || po.user.email}
                              size="small"
                            />
                          ))}
                        </Box>
                      ) : project.owner ? (
                        <Chip
                          avatar={<Avatar><PersonIcon /></Avatar>}
                          label={project.owner.name || project.owner.email}
                          size="small"
                        />
                      ) : '-'}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.description')}</TableCell>
                    <TableCell colSpan={3}>{project.description || '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.tags')}</TableCell>
                    <TableCell colSpan={3}>
                      {project.tags ? (
                        <Box sx={{ display: 'flex', gap: 0.5 }}>
                          {project.tags.split(',').map((tag, i) => (
                            <Chip key={i} label={tag.trim()} size="small" variant="outlined" />
                          ))}
                        </Box>
                      ) : '-'}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.createdAt')}</TableCell>
                    <TableCell>{new Date(project.created_at).toLocaleString()}</TableCell>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>{t('common.updatedAt')}</TableCell>
                    <TableCell>{new Date(project.updated_at).toLocaleString()}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </TableContainer>
          </CardContent>
        </Card>
      </TabPanel>
      <TabPanel value={tabValue} index={1}>
        <Card>
          <CardHeader
            title={t('dataModel.title')}
            action={
              <Button variant="contained" startIcon={<AddIcon />} onClick={handleCreateDataType}>
                {t('dataModel.new')}
              </Button>
            }
          />
          <CardContent>
            {dataTypes.length > 0 ? (
              <TableContainer component={Paper} variant="outlined">
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>{t('dataModel.name')}</TableCell>
                      <TableCell sx={{ width: 140 }}>{t('dataModel.category')}</TableCell>
                      <TableCell>{t('common.description')}</TableCell>
                      <TableCell sx={{ width: 160 }}>{t('common.updatedAt')}</TableCell>
                      <TableCell sx={{ width: 150 }}>{t('common.actions')}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {hierarchicalDataTypes.map((dataType) => {
                      const depth = getDataTypeDepth(dataType)
                      const hasChildren = dataTypes.some(dt => dt.parent_id === dataType.id)
                      const categoryConfig = getCategoryConfig(dataType.category || 'master')
                      return (
                        <TableRow key={dataType.id}>
                          <TableCell>
                            <Box sx={{ pl: depth * 3 }}>
                              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                                {depth > 0 && (
                                  <Typography
                                    sx={{
                                      color: "text.secondary",
                                      fontSize: 12
                                    }}>--</Typography>
                                )}
                                <Link
                                  component="button"
                                  underline="hover"
                                  onClick={() => navigate(`/data-models/${dataType.id}`)}
                                >
                                  {dataType.display_name}
                                </Link>
                              </Box>
                              <Typography
                                variant="caption"
                                sx={{
                                  color: "text.secondary",
                                  pl: depth > 0 ? 2.5 : 0
                                }}>
                                <code>{dataType.name}</code>
                              </Typography>
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Chip label={categoryConfig.text} color={categoryConfig.color} size="small" />
                          </TableCell>
                          <TableCell sx={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {dataType.description || '-'}
                          </TableCell>
                          <TableCell>{new Date(dataType.updated_at).toLocaleString()}</TableCell>
                          <TableCell>
                            <Box sx={{ display: 'flex', gap: 0.5 }}>
                              <IconButton
                                size="small"
                                onClick={() => navigate(`/data-models/${dataType.id}`)}
                                title={t('dataModel.viewDetail')}
                              >
                                <VisibilityIcon fontSize="small" />
                              </IconButton>
                              <IconButton
                                size="small"
                                onClick={() => handleEditDataType(dataType)}
                                title={t('common.edit')}
                              >
                                <EditIcon fontSize="small" />
                              </IconButton>
                              {!hasChildren && (
                                <IconButton
                                  size="small"
                                  color="error"
                                  onClick={() => setDeleteDataTypeId(dataType.id)}
                                  title={t('common.delete')}
                                >
                                  <DeleteIcon fontSize="small" />
                                </IconButton>
                              )}
                            </Box>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </TableContainer>
            ) : (
              <Box sx={{ textAlign: 'center', py: 6 }}>
                <Typography
                  sx={{
                    color: "text.secondary",
                    mb: 2
                  }}>{t('dataModel.noDataModels')}</Typography>
                <Button variant="contained" onClick={handleCreateDataType}>
                  {t('dataModel.new')}
                </Button>
              </Box>
            )}
          </CardContent>
        </Card>
      </TabPanel>
      <TabPanel value={tabValue} index={2}>
        <Card>
          <CardHeader
            title={t('workflow.title')}
            action={
              <Button variant="contained" startIcon={<AddIcon />} onClick={handleCreateWorkflow}>
                {t('workflow.new')}
              </Button>
            }
          />
          <CardContent>
            {workflows.length > 0 ? (
              <TableContainer component={Paper} variant="outlined">
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>{t('workflow.name')}</TableCell>
                      <TableCell sx={{ width: 100 }}>{t('workflow.type')}</TableCell>
                      <TableCell sx={{ width: 100 }}>{t('common.status')}</TableCell>
                      <TableCell sx={{ width: 80 }}>{t('workflow.enabled')}</TableCell>
                      <TableCell>{t('common.description')}</TableCell>
                      <TableCell sx={{ width: 120 }}>{t('common.actions')}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {workflows.map((workflow) => {
                      const statusConfig = getWorkflowStatusConfig(workflow.status)
                      return (
                        <TableRow key={workflow.id}>
                          <TableCell>
                            <Box>
                              <Link
                                component="button"
                                underline="hover"
                                onClick={() => navigate(`/workflows/${workflow.id}`)}
                              >
                                {workflow.name}
                              </Link>
                              <Typography
                                variant="caption"
                                sx={{
                                  color: "text.secondary",
                                  display: "block"
                                }}>
                                <code>{workflow.slug}</code>
                              </Typography>
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Chip
                              label={workflow.type === 'realtime' ? t('workflow.realtime') : t('workflow.batch')}
                              color={workflow.type === 'realtime' ? 'primary' : 'secondary'}
                              size="small"
                            />
                          </TableCell>
                          <TableCell>
                            <Chip label={statusConfig.text} color={statusConfig.color} size="small" />
                          </TableCell>
                          <TableCell>
                            <Chip
                              label={workflow.schedule_enabled ? t('workflow.on') : t('workflow.off')}
                              color={workflow.schedule_enabled ? 'success' : 'default'}
                              size="small"
                            />
                          </TableCell>
                          <TableCell sx={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {workflow.description || '-'}
                          </TableCell>
                          <TableCell>
                            <Box sx={{ display: 'flex', gap: 0.5 }}>
                              <IconButton size="small" onClick={() => handleEditWorkflow(workflow)}>
                                <EditIcon fontSize="small" />
                              </IconButton>
                              <IconButton
                                size="small"
                                color="error"
                                onClick={() => setDeleteWorkflowId(workflow.id)}
                              >
                                <DeleteIcon fontSize="small" />
                              </IconButton>
                            </Box>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </TableContainer>
            ) : (
              <Box sx={{ textAlign: 'center', py: 6 }}>
                <Typography
                  sx={{
                    color: "text.secondary",
                    mb: 2
                  }}>{t('project.noWorkflows')}</Typography>
                <Button variant="contained" onClick={handleCreateWorkflow}>
                  {t('workflow.new')}
                </Button>
              </Box>
            )}
          </CardContent>
        </Card>
      </TabPanel>
      {/* Edit Project Modal */}
      <Dialog open={editModalOpen} onClose={() => setEditModalOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('project.edit')}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label={t('project.name')}
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              required
              fullWidth
              placeholder={t('project.namePlaceholder')}
            />
            <TextField
              label={t('project.alias')}
              value={formData.alias}
              onChange={(e) => setFormData({ ...formData, alias: e.target.value })}
              required
              fullWidth
              placeholder={t('project.aliasPlaceholder')}
              helperText={t('project.aliasHelp')}
            />
            <TextField
              label={t('common.description')}
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              multiline
              rows={3}
              fullWidth
              placeholder={t('project.descriptionPlaceholder')}
            />
            <Autocomplete
              multiple
              options={[...selectedOwners, ...userOptions.filter(opt => !selectedOwners.some(s => s.id === opt.id))]}
              getOptionLabel={(option) => option.name || option.email}
              value={selectedOwners}
              loading={searchingUsers}
              onInputChange={(_, value) => debouncedSearch(value)}
              onChange={(_, newValue) => setSelectedOwners(newValue)}
              renderOption={(props, option) => (
                <Box component="li" {...props}>
                  <Avatar src={option.avatar_url} sx={{ width: 24, height: 24, mr: 1 }}>
                    <PersonIcon fontSize="small" />
                  </Avatar>
                  <Box>
                    <Typography variant="body2">{option.name || option.email}</Typography>
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{option.email}</Typography>
                  </Box>
                </Box>
              )}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('project.owners')}
                  placeholder={t('project.ownersPlaceholder')}
                />
              )}
            />
            <FormControl fullWidth>
              <InputLabel>{t('common.status')}</InputLabel>
              <Select
                value={formData.status}
                label={t('common.status')}
                onChange={(e) => setFormData({ ...formData, status: e.target.value })}
              >
                <MenuItem value="active">{t('status.active')}</MenuItem>
                <MenuItem value="inactive">{t('status.inactive')}</MenuItem>
                <MenuItem value="archived">{t('status.archived')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('common.tags')}
              value={formData.tags}
              onChange={(e) => setFormData({ ...formData, tags: e.target.value })}
              fullWidth
              placeholder="tag1, tag2, tag3"
              helperText={t('common.tagsHelp')}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditModalOpen(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleEditSubmit}>{t('common.save')}</Button>
        </DialogActions>
      </Dialog>
      {/* Workflow Modal */}
      <Dialog open={workflowModalOpen} onClose={() => setWorkflowModalOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingWorkflow ? t('workflow.edit') : t('workflow.new')}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label={t('workflow.name')}
              value={workflowForm.name}
              onChange={(e) => setWorkflowForm({ ...workflowForm, name: e.target.value })}
              required
              fullWidth
              placeholder={t('workflow.namePlaceholder')}
            />
            <TextField
              label={t('workflow.slug')}
              value={workflowForm.slug}
              onChange={(e) => setWorkflowForm({ ...workflowForm, slug: e.target.value })}
              required
              fullWidth
              placeholder={t('workflow.slugPlaceholder')}
              helperText={t('workflow.slugHelp')}
            />
            <FormControl fullWidth>
              <InputLabel>{t('workflow.type')}</InputLabel>
              <Select
                value={workflowForm.type}
                label={t('workflow.type')}
                onChange={(e) => setWorkflowForm({ ...workflowForm, type: e.target.value as 'batch' | 'realtime' })}
              >
                <MenuItem value="batch">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('workflow.batch')} color="secondary" size="small" />
                    <Typography variant="body2" sx={{
                      color: "text.secondary"
                    }}>{t('workflow.batchDesc')}</Typography>
                  </Box>
                </MenuItem>
                <MenuItem value="realtime">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('workflow.realtime')} color="primary" size="small" />
                    <Typography variant="body2" sx={{
                      color: "text.secondary"
                    }}>{t('workflow.realtimeDesc')}</Typography>
                  </Box>
                </MenuItem>
              </Select>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>{t('workflow.cluster')}</InputLabel>
              <Select
                value={workflowForm.cluster_id}
                label={t('workflow.cluster')}
                onChange={(e) => setWorkflowForm({ ...workflowForm, cluster_id: e.target.value })}
              >
                <MenuItem value="">
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>{t('workflow.clusterDefault')}</Typography>
                </MenuItem>
                {clusterOptions.map((cl) => (
                  <MenuItem key={cl.id} value={cl.id}>{cl.name}</MenuItem>
                ))}
              </Select>
            </FormControl>
            <TextField
              label={t('workflow.description')}
              value={workflowForm.description}
              onChange={(e) => setWorkflowForm({ ...workflowForm, description: e.target.value })}
              multiline
              rows={3}
              fullWidth
              placeholder={t('workflow.descriptionPlaceholder')}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setWorkflowModalOpen(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleWorkflowSubmit} disabled={workflowSaving}>
            {workflowSaving ? <CircularProgress size={20} /> : t('common.save')}
          </Button>
        </DialogActions>
      </Dialog>
      {/* DataType Modal */}
      <Dialog open={dataTypeModalOpen} onClose={() => setDataTypeModalOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{editingDataType ? t('dataModel.edit') : t('dataModel.new')}</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label={t('dataModel.name')}
              value={dataTypeForm.display_name}
              onChange={(e) => setDataTypeForm({ ...dataTypeForm, display_name: e.target.value })}
              required
              fullWidth
              placeholder={t('dataModel.namePlaceholder')}
            />
            <TextField
              label={t('dataModel.slug')}
              value={dataTypeForm.name}
              onChange={(e) => setDataTypeForm({ ...dataTypeForm, name: e.target.value })}
              required
              fullWidth
              placeholder={t('dataModel.slugPlaceholder')}
              helperText={t('dataModel.slugHelp')}
            />
            <FormControl fullWidth>
              <InputLabel>{t('dataModel.category')}</InputLabel>
              <Select
                value={dataTypeForm.category}
                label={t('dataModel.category')}
                onChange={(e) => setDataTypeForm({ ...dataTypeForm, category: e.target.value })}
              >
                <MenuItem value="master">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('dataModel.categories.master')} color="primary" size="small" />
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{t('dataModel.categories.masterDesc')}</Typography>
                  </Box>
                </MenuItem>
                <MenuItem value="transaction">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('dataModel.categories.transaction')} color="success" size="small" />
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{t('dataModel.categories.transactionDesc')}</Typography>
                  </Box>
                </MenuItem>
                <MenuItem value="log">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('dataModel.categories.log')} color="warning" size="small" />
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{t('dataModel.categories.logDesc')}</Typography>
                  </Box>
                </MenuItem>
                <MenuItem value="metric">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('dataModel.categories.metric')} color="secondary" size="small" />
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{t('dataModel.categories.metricDesc')}</Typography>
                  </Box>
                </MenuItem>
                <MenuItem value="reference">
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Chip label={t('dataModel.categories.reference')} color="info" size="small" />
                    <Typography variant="caption" sx={{
                      color: "text.secondary"
                    }}>{t('dataModel.categories.referenceDesc')}</Typography>
                  </Box>
                </MenuItem>
              </Select>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>{t('dataModel.parent')}</InputLabel>
              <Select
                value={dataTypeForm.parent_id}
                label={t('dataModel.parent')}
                onChange={(e) => handleParentChange(e.target.value || undefined)}
                disabled={editingDataType ? dataTypes.some(dt => dt.parent_id === editingDataType.id) : false}
              >
                <MenuItem value="">{t('dataModel.parentPlaceholder')}</MenuItem>
                {dataTypes
                  .filter(dt => dt.id !== editingDataType?.id)
                  .map(dt => (
                    <MenuItem key={dt.id} value={dt.id}>
                      {dt.display_name} <Typography
                      component="span"
                      sx={{
                        color: "text.secondary",
                        ml: 1
                      }}>({dt.name})</Typography>
                    </MenuItem>
                  ))}
              </Select>
              <FormHelperText>
                {editingDataType && dataTypes.some(dt => dt.parent_id === editingDataType.id)
                  ? t('dataModel.parentDisabledHasChildren')
                  : t('dataModel.parentHelp')}
              </FormHelperText>
            </FormControl>
            {selectedParentId ? (
              <Autocomplete
                multiple
                freeSolo
                options={[]}
                value={dataTypeForm.id_fields}
                onChange={(_, newValue) => setDataTypeForm({ ...dataTypeForm, id_fields: newValue as string[] })}
                renderInput={(params) => (
                  <TextField
                    {...params}
                    label={t('dataModel.idFields')}
                    placeholder={t('dataModel.idFieldsPlaceholder')}
                    helperText={t('dataModel.idFieldsHelp')}
                  />
                )}
              />
            ) : (
              <TextField
                label={t('dataModel.keyField')}
                value={dataTypeForm.key_field}
                onChange={(e) => setDataTypeForm({ ...dataTypeForm, key_field: e.target.value })}
                required
                fullWidth
                placeholder={t('dataModel.keyFieldPlaceholder')}
                helperText={t('dataModel.keyFieldHelp')}
              />
            )}
            <TextField
              label={t('common.description')}
              value={dataTypeForm.description}
              onChange={(e) => setDataTypeForm({ ...dataTypeForm, description: e.target.value })}
              multiline
              rows={3}
              fullWidth
            />
            <TextField
              label={t('dataModel.jsonSchema')}
              value={dataTypeForm.json_schema}
              onChange={(e) => setDataTypeForm({ ...dataTypeForm, json_schema: e.target.value })}
              multiline
              rows={10}
              fullWidth
              placeholder={t('dataModel.jsonSchemaPlaceholder')}
              helperText={t('dataModel.jsonSchemaHelp')}
              slotProps={{ input: { sx: { fontFamily: 'monospace' } } }}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDataTypeModalOpen(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleDataTypeSubmit} disabled={dataTypeSaving}>
            {dataTypeSaving ? <CircularProgress size={20} /> : t('common.save')}
          </Button>
        </DialogActions>
      </Dialog>
      {/* Delete Project Confirm Dialog */}
      <ConfirmDialog
        open={deleteDialogOpen}
        title={t('project.deleteConfirm')}
        message={t('project.deleteWarning')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        onConfirm={() => {
          setDeleteDialogOpen(false)
          handleDelete()
        }}
        onCancel={() => setDeleteDialogOpen(false)}
        severity="error"
      />
      {/* Delete Workflow Confirm Dialog */}
      <ConfirmDialog
        open={!!deleteWorkflowId}
        title={t('workflow.deleteConfirm')}
        message={t('workflow.deleteWarning')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        onConfirm={handleDeleteWorkflow}
        onCancel={() => setDeleteWorkflowId(null)}
        severity="error"
      />
      {/* Delete DataType Confirm Dialog */}
      <ConfirmDialog
        open={!!deleteDataTypeId}
        title={t('dataModel.deleteConfirm')}
        message={t('dataModel.deleteWarning')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        onConfirm={handleDeleteDataType}
        onCancel={() => setDeleteDataTypeId(null)}
        severity="error"
      />
    </Box>
  );
}
