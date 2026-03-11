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
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tab,
  Tabs,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Extension as PluginIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  CheckCircle as ActiveIcon,
  Category as CategoryIcon,
  Build as BuildIcon,
  Warning as WarningIcon,
  History as HistoryIcon,
  Replay as ReplayIcon,
  Description as LogIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { useSnackbar } from '../hooks/useSnackbar'
import type { Plugin, PluginCreateRequest, PluginStageCreate, RunnerStatusResponse, RunnerVersion, StageRevision } from '../types/plugin'
import { getPlugins, createPlugin, updatePlugin, deletePlugin, getRunnerStatus, startRunnerBuild, getRunnerVersions, rebuildRunnerVersion, getPluginRevisions } from '../services/pluginApi'

interface PluginFormData {
  name: string
  description: string
  source_repo: string
  stages: PluginStageCreate[]
}

const initialFormData: PluginFormData = {
  name: '',
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
  const [runnerStatus, setRunnerStatus] = useState<RunnerStatusResponse | null>(null)
  const [building, setBuilding] = useState(false)
  const [runnerVersions, setRunnerVersions] = useState<RunnerVersion[]>([])
  const [revisions, setRevisions] = useState<StageRevision[]>([])
  const [selectedRevisionPlugin, setSelectedRevisionPlugin] = useState<string | null>(null)
  const [buildLogDialog, setBuildLogDialog] = useState<{ open: boolean; log: string; title: string }>({ open: false, log: '', title: '' })
  const [bottomTab, setBottomTab] = useState(0)

  const loadPlugins = useCallback(async () => {
    setLoading(true)
    try {
      const [data, status, versions] = await Promise.all([
        getPlugins(),
        getRunnerStatus().catch(() => null),
        getRunnerVersions().catch(() => []),
      ])
      setPlugins(data)
      if (status) setRunnerStatus(status)
      setRunnerVersions(versions)
    } catch {
      showError(t('plugin.loadError'))
    } finally {
      setLoading(false)
    }
  }, [t, showError])

  const handleRunnerBuild = async () => {
    setBuilding(true)
    try {
      await startRunnerBuild()
      showSuccess('Runner build started')
      setTimeout(() => loadPlugins(), 3000)
    } catch {
      showError('Failed to start runner build')
    } finally {
      setBuilding(false)
    }
  }

  const handleRebuild = async (versionId: string) => {
    try {
      await rebuildRunnerVersion(versionId)
      showSuccess('Rebuild started')
      setTimeout(() => loadPlugins(), 3000)
    } catch {
      showError('Failed to start rebuild')
    }
  }

  const loadRevisions = async (pluginName: string) => {
    try {
      const data = await getPluginRevisions(pluginName)
      setRevisions(data)
      setSelectedRevisionPlugin(pluginName)
    } catch {
      showError('Failed to load revisions')
    }
  }

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
    if (!formData.name) return

    const req: PluginCreateRequest = {
      name: formData.name,
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
      field: 'type',
      headerName: 'Type',
      width: 90,
      renderCell: (params: GridRenderCellParams) => (
        <Chip
          size="small"
          label={params.value || 'native'}
          color={params.value === 'script' ? 'info' : 'default'}
          variant="outlined"
        />
      ),
    },
    {
      field: 'deploy_status',
      headerName: 'Deploy',
      width: 100,
      renderCell: (params: GridRenderCellParams) => {
        const plugin = params.row as Plugin
        if (plugin.type === 'script') {
          return <Chip size="small" label="Instant" color="success" variant="outlined" />
        }
        const needsBuild = plugin.source_hash && plugin.source_hash !== plugin.deployed_hash
        return needsBuild
          ? <Chip size="small" label="Build needed" color="warning" variant="outlined" />
          : <Chip size="small" label="Deployed" color="success" variant="outlined" />
      },
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
          {params.row.type === 'native' && (
            <Tooltip title="Revision History">
              <IconButton size="small" onClick={(e) => { e.stopPropagation(); loadRevisions(params.row.name); setBottomTab(1) }}>
                <HistoryIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
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

      {/* Runner Status Panel */}
      {runnerStatus && runnerStatus.plugins.length > 0 && (
        <Card sx={{ mb: 3 }}>
          <CardContent>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <BuildIcon fontSize="small" />
                <Typography variant="subtitle2">Runner Image</Typography>
                {runnerStatus.latest_ready_version && (
                  <Chip
                    size="small"
                    label={runnerStatus.latest_ready_version.id}
                    variant="outlined"
                    sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }}
                  />
                )}
              </Box>
              <Button
                variant={runnerStatus.needs_build ? 'contained' : 'outlined'}
                size="small"
                startIcon={runnerStatus.needs_build ? <WarningIcon /> : <BuildIcon />}
                onClick={handleRunnerBuild}
                disabled={building}
                color={runnerStatus.needs_build ? 'warning' : 'primary'}
              >
                {building ? 'Building...' : runnerStatus.needs_build ? 'Build Required' : 'Rebuild'}
              </Button>
            </Box>
            {runnerStatus.needs_build && (
              <Alert severity="warning" sx={{ mt: 1 }} icon={<WarningIcon />}>
                {runnerStatus.plugins.filter(p => p.needs_build).map(p => p.name).join(', ')} — source modified, rebuild needed
              </Alert>
            )}
            {!runnerStatus.needs_build && runnerStatus.latest_ready_version && (
              <Alert severity="success" sx={{ mt: 1 }}>
                All native stages deployed ({runnerStatus.latest_ready_version.id})
                {runnerStatus.latest_ready_version.revision_seq != null && (
                  <> &mdash; revision seq #{runnerStatus.latest_ready_version.revision_seq}</>
                )}
              </Alert>
            )}
          </CardContent>
        </Card>
      )}

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

      {/* Build History + Revision History Tabs */}
      <Card sx={{ mt: 3 }}>
        <Tabs value={bottomTab} onChange={(_, v) => setBottomTab(v)} sx={{ borderBottom: 1, borderColor: 'divider', px: 2 }}>
          <Tab icon={<BuildIcon fontSize="small" />} iconPosition="start" label="Build History" />
          <Tab icon={<HistoryIcon fontSize="small" />} iconPosition="start" label="Revision History" />
        </Tabs>
        <CardContent sx={{ p: 0 }}>
          {/* Build History Tab */}
          {bottomTab === 0 && (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>#</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Revision Seq</TableCell>
                    <TableCell>Trigger</TableCell>
                    <TableCell>Duration</TableCell>
                    <TableCell>Created</TableCell>
                    <TableCell align="right">Actions</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {runnerVersions.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={7} align="center" sx={{ py: 3, color: 'text.secondary' }}>
                        No build history
                      </TableCell>
                    </TableRow>
                  ) : (
                    runnerVersions.map((v) => (
                      <TableRow key={v.id} hover>
                        <TableCell>
                          <Chip size="small" label={`#${v.build_number}`} variant="outlined" sx={{ fontFamily: 'monospace' }} />
                        </TableCell>
                        <TableCell>
                          <Chip
                            size="small"
                            label={v.status}
                            color={v.status === 'ready' ? 'success' : v.status === 'failed' ? 'error' : v.status === 'building' ? 'warning' : 'default'}
                          />
                        </TableCell>
                        <TableCell>
                          {v.revision_seq ? (
                            <Chip size="small" label={`seq #${v.revision_seq}`} variant="outlined" sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }} />
                          ) : '-'}
                        </TableCell>
                        <TableCell>
                          <Chip
                            size="small"
                            label={v.trigger || 'manual'}
                            variant="outlined"
                            color={v.trigger === 'rebuild' ? 'info' : 'default'}
                          />
                        </TableCell>
                        <TableCell>
                          {v.duration_ms ? `${(v.duration_ms / 1000).toFixed(1)}s` : '-'}
                        </TableCell>
                        <TableCell sx={{ fontSize: '0.8rem' }}>
                          {new Date(v.created_at).toLocaleString()}
                        </TableCell>
                        <TableCell align="right">
                          {v.build_log && (
                            <Tooltip title="Build Log">
                              <IconButton size="small" onClick={() => setBuildLogDialog({ open: true, log: v.build_log || '', title: `Build #${v.build_number} Log` })}>
                                <LogIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                          <Tooltip title="Rebuild">
                            <IconButton size="small" onClick={() => handleRebuild(v.id)} disabled={building}>
                              <ReplayIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </TableContainer>
          )}

          {/* Revision History Tab */}
          {bottomTab === 1 && (
            <Box>
              {!selectedRevisionPlugin && (
                <Box sx={{ p: 3, textAlign: 'center', color: 'text.secondary' }}>
                  <HistoryIcon sx={{ fontSize: 40, mb: 1, opacity: 0.5 }} />
                  <Typography>Click the history button on a native plugin to view its revision history</Typography>
                </Box>
              )}
              {selectedRevisionPlugin && (
                <>
                  <Box sx={{ px: 2, pt: 2, display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography variant="subtitle2">Revisions for</Typography>
                    <Chip size="small" label={selectedRevisionPlugin} color="primary" />
                  </Box>
                  <TableContainer>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>Seq</TableCell>
                          <TableCell>Action</TableCell>
                          <TableCell>Hash</TableCell>
                          <TableCell>Diff</TableCell>
                          <TableCell>Message</TableCell>
                          <TableCell>Created</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {revisions.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={6} align="center" sx={{ py: 3, color: 'text.secondary' }}>
                              No revisions
                            </TableCell>
                          </TableRow>
                        ) : (
                          revisions.map((r) => (
                            <TableRow key={r.id} hover>
                              <TableCell>
                                <Chip size="small" label={`#${r.seq}`} variant="outlined" sx={{ fontFamily: 'monospace' }} />
                              </TableCell>
                              <TableCell>
                                <Chip
                                  size="small"
                                  label={r.action}
                                  color={r.action === 'create' ? 'success' : r.action === 'delete' ? 'error' : 'warning'}
                                  variant="outlined"
                                />
                              </TableCell>
                              <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }}>
                                {r.source_hash ? r.source_hash.substring(0, 8) + '...' : '-'}
                              </TableCell>
                              <TableCell sx={{ fontSize: '0.8rem' }}>
                                {r.diff_summary || '-'}
                              </TableCell>
                              <TableCell sx={{ fontSize: '0.8rem', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {r.message || '-'}
                              </TableCell>
                              <TableCell sx={{ fontSize: '0.8rem' }}>
                                {new Date(r.created_at).toLocaleString()}
                              </TableCell>
                            </TableRow>
                          ))
                        )}
                      </TableBody>
                    </Table>
                  </TableContainer>
                </>
              )}
            </Box>
          )}
        </CardContent>
      </Card>

      {/* Build Log Dialog */}
      <Dialog open={buildLogDialog.open} onClose={() => setBuildLogDialog({ open: false, log: '', title: '' })} maxWidth="md" fullWidth>
        <DialogTitle>{buildLogDialog.title}</DialogTitle>
        <DialogContent>
          <Box
            component="pre"
            sx={{
              bgcolor: '#1e1e1e',
              color: '#d4d4d4',
              p: 2,
              borderRadius: 1,
              overflow: 'auto',
              maxHeight: 500,
              fontSize: '0.8rem',
              fontFamily: 'monospace',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
            }}
          >
            {buildLogDialog.log || 'No log available'}
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setBuildLogDialog({ open: false, log: '', title: '' })}>Close</Button>
        </DialogActions>
      </Dialog>

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
            disabled={!formData.name}
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
