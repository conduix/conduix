import { useEffect, useState, useCallback } from 'react'
import {
  Box,
  Typography,
  Card,
  CardContent,
  Grid,
  Chip,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  IconButton,
  Tooltip,
  Alert,
  Collapse,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Extension as PluginIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  CheckCircle as ActiveIcon,
  Category as CategoryIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { useSnackbar } from '../hooks/useSnackbar'
import type { Plugin, PluginCreateRequest, PluginStageCreate } from '../types/plugin'
import { getPlugins, createPlugin, updatePlugin, deletePlugin } from '../services/pluginApi'

interface PluginFormData {
  name: string
  version: string
  image: string
  description: string
  source_repo: string
  stages: PluginStageCreate[]
}

const initialFormData: PluginFormData = {
  name: '',
  version: '',
  image: '',
  description: '',
  source_repo: '',
  stages: [],
}

const initialStage: PluginStageCreate = {
  type: '',
  displayName: '',
  category: 'transform',
  description: '',
  configSchema: { type: 'object', properties: {} },
}

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

export default function PluginsPage() {
  const { t } = useTranslation()
  const { showSuccess, showError } = useSnackbar()
  const [plugins, setPlugins] = useState<Plugin[]>([])
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedPlugin, setSelectedPlugin] = useState<Plugin | null>(null)
  const [formData, setFormData] = useState<PluginFormData>(initialFormData)
  const [expandedPlugin, setExpandedPlugin] = useState<string | null>(null)

  const loadPlugins = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getPlugins()
      setPlugins(data)
    } catch {
      showError(t('plugin.loadError'))
    } finally {
      setLoading(false)
    }
  }, [t, showError])

  useEffect(() => {
    loadPlugins()
  }, [loadPlugins])

  const activeCount = plugins.filter((p) => p.status === 'active').length
  const totalStages = plugins.reduce((sum, p) => sum + (p.stages?.length || 0), 0)

  const handleCreate = () => {
    setSelectedPlugin(null)
    setFormData(initialFormData)
    setDialogOpen(true)
  }

  const handleEdit = (plugin: Plugin) => {
    setSelectedPlugin(plugin)
    setFormData({
      name: plugin.name,
      version: plugin.version,
      image: plugin.image,
      description: plugin.description || '',
      source_repo: plugin.source_repo || '',
      stages:
        plugin.stages?.map((s) => ({
          type: s.stage_type,
          displayName: s.display_name || '',
          category: s.category || 'transform',
          description: s.description || '',
          configSchema: s.config_schema || { type: 'object', properties: {} },
        })) || [],
    })
    setDialogOpen(true)
  }

  const handleDelete = (plugin: Plugin) => {
    setSelectedPlugin(plugin)
    setDeleteDialogOpen(true)
  }

  const confirmDelete = async () => {
    if (!selectedPlugin) return
    try {
      await deletePlugin(selectedPlugin.name)
      showSuccess(t('plugin.deleteSuccess'))
      setDeleteDialogOpen(false)
      loadPlugins()
    } catch {
      showError(t('plugin.deleteError'))
    }
  }

  const handleSubmit = async () => {
    if (!formData.name || !formData.version || !formData.image) return

    const req: PluginCreateRequest = {
      name: formData.name,
      version: formData.version,
      image: formData.image,
      description: formData.description || undefined,
      source_repo: formData.source_repo || undefined,
      stages: formData.stages,
    }

    try {
      if (selectedPlugin) {
        await updatePlugin(selectedPlugin.name, req)
        showSuccess(t('plugin.updateSuccess'))
      } else {
        await createPlugin(req)
        showSuccess(t('plugin.createSuccess'))
      }
      setDialogOpen(false)
      loadPlugins()
    } catch {
      showError(selectedPlugin ? t('plugin.updateError') : t('plugin.createError'))
    }
  }

  const addStage = () => {
    setFormData((prev) => ({
      ...prev,
      stages: [...prev.stages, { ...initialStage }],
    }))
  }

  const removeStage = (index: number) => {
    setFormData((prev) => ({
      ...prev,
      stages: prev.stages.filter((_, i) => i !== index),
    }))
  }

  const updateStage = (index: number, field: keyof PluginStageCreate, value: string) => {
    setFormData((prev) => ({
      ...prev,
      stages: prev.stages.map((s, i) => (i === index ? { ...s, [field]: value } : s)),
    }))
  }

  const statusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'success'
      case 'inactive':
        return 'default'
      case 'deprecated':
        return 'warning'
      default:
        return 'default'
    }
  }

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: t('plugin.name'),
      flex: 1,
      minWidth: 150,
    },
    {
      field: 'version',
      headerName: t('plugin.version'),
      width: 100,
    },
    {
      field: 'image',
      headerName: t('plugin.image'),
      flex: 1.5,
      minWidth: 200,
      renderCell: (params: GridRenderCellParams) => (
        <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: 12 }}>
          {params.value}
        </Typography>
      ),
    },
    {
      field: 'status',
      headerName: t('common.status'),
      width: 100,
      renderCell: (params: GridRenderCellParams) => (
        <Chip
          size="small"
          label={t(`plugin.status.${params.value}`)}
          color={statusColor(params.value) as 'success' | 'default' | 'warning'}
        />
      ),
    },
    {
      field: 'stages',
      headerName: t('plugin.stages'),
      width: 80,
      renderCell: (params: GridRenderCellParams) => (
        <Chip size="small" label={params.value?.length || 0} variant="outlined" />
      ),
    },
    {
      field: 'created_at',
      headerName: t('common.createdAt'),
      width: 160,
      renderCell: (params: GridRenderCellParams) =>
        params.value ? new Date(params.value).toLocaleString() : '-',
    },
    {
      field: 'actions',
      headerName: t('common.actions'),
      width: 120,
      sortable: false,
      renderCell: (params: GridRenderCellParams) => (
        <Box>
          <Tooltip title={t('common.edit')}>
            <IconButton size="small" onClick={() => handleEdit(params.row)}>
              <EditIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title={t('common.delete')}>
            <IconButton size="small" onClick={() => handleDelete(params.row)} color="error">
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>
      ),
    },
  ]

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5">{t('plugin.title')}</Typography>
        <Button variant="contained" startIcon={<AddIcon />} onClick={handleCreate}>
          {t('plugin.register')}
        </Button>
      </Box>

      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={4}>
          <StatCard
            title={t('plugin.totalPlugins')}
            value={plugins.length}
            icon={<PluginIcon />}
            color="#1976d2"
          />
        </Grid>
        <Grid item xs={12} sm={4}>
          <StatCard
            title={t('plugin.activePlugins')}
            value={activeCount}
            icon={<ActiveIcon />}
            color="#2e7d32"
          />
        </Grid>
        <Grid item xs={12} sm={4}>
          <StatCard
            title={t('plugin.totalStages')}
            value={totalStages}
            icon={<CategoryIcon />}
            color="#ed6c02"
          />
        </Grid>
      </Grid>

      <Card>
        <CardContent>
          <DataGrid
            rows={plugins}
            columns={columns}
            loading={loading}
            autoHeight
            pageSizeOptions={[10, 25, 50]}
            initialState={{ pagination: { paginationModel: { pageSize: 10 } } }}
            disableRowSelectionOnClick
            onRowClick={(params) =>
              setExpandedPlugin(expandedPlugin === params.row.id ? null : params.row.id)
            }
            sx={{ border: 'none' }}
          />

          {expandedPlugin && (
            <Collapse in={true}>
              {(() => {
                const plugin = plugins.find((p) => p.id === expandedPlugin)
                if (!plugin?.stages?.length) return null
                return (
                  <Box sx={{ mt: 2, p: 2, bgcolor: 'grey.50', borderRadius: 1 }}>
                    <Typography variant="subtitle2" sx={{ mb: 1 }}>
                      {t('plugin.registeredStages')}
                    </Typography>
                    <List dense>
                      {plugin.stages.map((stage) => (
                        <ListItem key={stage.id}>
                          <ListItemIcon sx={{ minWidth: 36 }}>
                            <CategoryIcon fontSize="small" />
                          </ListItemIcon>
                          <ListItemText
                            primary={stage.display_name || stage.stage_type}
                            secondary={`${stage.stage_type} / ${stage.category || 'transform'}`}
                          />
                          {stage.description && (
                            <Typography variant="caption" color="text.secondary">
                              {stage.description}
                            </Typography>
                          )}
                        </ListItem>
                      ))}
                    </List>
                  </Box>
                )
              })()}
            </Collapse>
          )}
        </CardContent>
      </Card>

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>
          {selectedPlugin ? t('plugin.edit') : t('plugin.register')}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label={t('plugin.name')}
              value={formData.name}
              onChange={(e) => setFormData((prev) => ({ ...prev, name: e.target.value }))}
              required
              fullWidth
              disabled={!!selectedPlugin}
            />
            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label={t('plugin.version')}
                value={formData.version}
                onChange={(e) => setFormData((prev) => ({ ...prev, version: e.target.value }))}
                required
                sx={{ flex: 1 }}
              />
              <TextField
                label={t('plugin.image')}
                value={formData.image}
                onChange={(e) => setFormData((prev) => ({ ...prev, image: e.target.value }))}
                required
                sx={{ flex: 2 }}
                placeholder="myregistry/conduix-plugins:v1.0.0"
              />
            </Box>
            <TextField
              label={t('common.description')}
              value={formData.description}
              onChange={(e) => setFormData((prev) => ({ ...prev, description: e.target.value }))}
              multiline
              rows={2}
              fullWidth
            />
            <TextField
              label={t('plugin.sourceRepo')}
              value={formData.source_repo}
              onChange={(e) => setFormData((prev) => ({ ...prev, source_repo: e.target.value }))}
              fullWidth
              placeholder="https://github.com/mycompany/conduix-plugins"
            />

            {/* Stages Section */}
            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <Typography variant="subtitle1">{t('plugin.stages')}</Typography>
              <Button size="small" startIcon={<AddIcon />} onClick={addStage}>
                {t('plugin.addStage')}
              </Button>
            </Box>

            {formData.stages.map((stage, idx) => (
              <Card key={idx} variant="outlined">
                <CardContent>
                  <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
                    <Typography variant="subtitle2">
                      Stage #{idx + 1}
                    </Typography>
                    <IconButton size="small" onClick={() => removeStage(idx)} color="error">
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Box>
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
                    <Box sx={{ display: 'flex', gap: 2 }}>
                      <TextField
                        label={t('plugin.stageType')}
                        value={stage.type}
                        onChange={(e) => updateStage(idx, 'type', e.target.value)}
                        required
                        size="small"
                        sx={{ flex: 1 }}
                        placeholder="ml-anomaly"
                      />
                      <TextField
                        label={t('plugin.stageDisplayName')}
                        value={stage.displayName}
                        onChange={(e) => updateStage(idx, 'displayName', e.target.value)}
                        required
                        size="small"
                        sx={{ flex: 1 }}
                      />
                      <TextField
                        label={t('plugin.stageCategory')}
                        value={stage.category}
                        onChange={(e) => updateStage(idx, 'category', e.target.value)}
                        size="small"
                        sx={{ flex: 1 }}
                        placeholder="transform"
                      />
                    </Box>
                    <TextField
                      label={t('common.description')}
                      value={stage.description || ''}
                      onChange={(e) => updateStage(idx, 'description', e.target.value)}
                      size="small"
                      fullWidth
                    />
                  </Box>
                </CardContent>
              </Card>
            ))}

            {formData.stages.length === 0 && (
              <Alert severity="info">{t('plugin.noStagesHint')}</Alert>
            )}
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>{t('common.cancel')}</Button>
          <Button
            variant="contained"
            onClick={handleSubmit}
            disabled={!formData.name || !formData.version || !formData.image}
          >
            {selectedPlugin ? t('common.save') : t('common.create')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>{t('plugin.deleteConfirm')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('plugin.deleteMessage', { name: selectedPlugin?.name })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" color="error" onClick={confirmDelete}>
            {t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
