import { useEffect, useState, useCallback, useRef } from 'react'
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
  ToggleButton,
  ToggleButtonGroup,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Extension as PluginIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  CheckCircle as ActiveIcon,
  Category as CategoryIcon,
  Code as CodeIcon,
  Javascript as JsIcon,
  Build as BuildIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { useSnackbar } from '../hooks/useSnackbar'
import type { Plugin, PluginCreateRequest, PluginStageCreate } from '../types/plugin'
import {
  getPlugins,
  createPlugin,
  updatePlugin,
  deletePlugin,
  getRunnerStatus,
  startRunnerBuild,
  getRunnerVersions,
} from '../services/pluginApi'
import type { RunnerStatusResponse, RunnerVersion } from '../services/pluginApi'
import NativeStageEditor from '../components/NativeStageEditor/NativeStageEditor'
import JSScriptStageEditor from '../components/JSScriptStageEditor/JSScriptStageEditor'

type StageMode = 'none' | 'script' | 'native'

interface PluginFormData {
  name: string
  description: string
  source_repo: string
  stageMode: StageMode
  source_code: string
  go_mod: string
  scriptConfig: Record<string, unknown> // JS script stage config (code, timeout)
  stages: PluginStageCreate[]
}

const initialFormData: PluginFormData = {
  name: '',
  description: '',
  source_repo: '',
  stageMode: 'none',
  source_code: '',
  go_mod: '',
  scriptConfig: {},
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
  const [testPassed, setTestPassed] = useState(false)
  const [runnerStatus, setRunnerStatus] = useState<RunnerStatusResponse | null>(null)
  const [buildTriggering, setBuildTriggering] = useState(false)
  const [buildLogOpen, setBuildLogOpen] = useState(false)
  const [buildVersions, setBuildVersions] = useState<RunnerVersion[]>([])
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadRunnerStatus = useCallback(async () => {
    try {
      const status = await getRunnerStatus()
      setRunnerStatus(status)
    } catch {
      // Runner status는 실패해도 무시 (runner 미설정 환경)
    }
  }, [])

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
    loadRunnerStatus()
  }, [loadPlugins, loadRunnerStatus])

  // Runner 빌드 상태 폴링 (needs_build일 때 5초마다)
  useEffect(() => {
    if (runnerStatus?.needs_build) {
      pollRef.current = setInterval(() => {
        loadRunnerStatus()
        loadPlugins() // deployed_hash 갱신 반영
      }, 5000)
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [runnerStatus?.needs_build, loadRunnerStatus, loadPlugins])

  const activeCount = plugins.filter((p) => p.status === 'active').length
  const totalStages = plugins.reduce((sum, p) => sum + (p.stages?.length || 0), 0)

  const handleCreate = () => {
    setSelectedPlugin(null)
    setFormData(initialFormData)
    setTestPassed(false)
    setDialogOpen(true)
  }

  const handleEdit = (plugin: Plugin) => {
    setSelectedPlugin(plugin)

    // plugin type에 따라 stageMode 결정
    let stageMode: StageMode = 'none'
    if (plugin.type === 'native' && plugin.source_code) {
      stageMode = 'native'
    } else if (plugin.type === 'script') {
      stageMode = 'script'
    }

    setFormData({
      name: plugin.name,
      description: plugin.description || '',
      source_repo: plugin.source_repo || '',
      stageMode,
      source_code: plugin.source_code || '',
      go_mod: plugin.go_mod || '',
      scriptConfig: plugin.type === 'script' && plugin.source_code
        ? { code: plugin.source_code }
        : {},
      stages:
        plugin.stages?.map((s) => ({
          type: s.stage_type,
          displayName: s.display_name || '',
          category: s.category || 'transform',
          description: s.description || '',
          configSchema: s.config_schema || { type: 'object', properties: {} },
        })) || [],
    })
    setTestPassed(false)
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

  // Save/Create 시 테스트 필수 여부
  const needsTest = formData.stageMode === 'native' || formData.stageMode === 'script'

  const handleSubmit = async () => {
    if (!formData.name) return

    const req: PluginCreateRequest = {
      name: formData.name,
      description: formData.description || undefined,
      source_repo: formData.source_repo || undefined,
      source_code:
        formData.stageMode === 'native'
          ? formData.source_code || undefined
          : formData.stageMode === 'script'
            ? (formData.scriptConfig.code as string) || undefined
            : undefined,
      go_mod: formData.stageMode === 'native' ? formData.go_mod || undefined : undefined,
      stages: formData.stages,
    }

    try {
      if (selectedPlugin) {
        await updatePlugin(selectedPlugin.name, req)
        const autoBuildMsg = formData.stageMode === 'native' ? ` — ${t('plugin.autoBuildTriggered', 'auto-build triggered')}` : ''
        showSuccess(t('plugin.updateSuccess') + autoBuildMsg)
      } else {
        await createPlugin(req)
        showSuccess(t('plugin.createSuccess'))
      }
      setDialogOpen(false)
      loadPlugins()
      // Native plugin 저장 시 auto-build 트리거됨 → 상태 갱신
      if (formData.stageMode === 'native') {
        setTimeout(loadRunnerStatus, 1000)
      }
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

  const handleViewBuildLogs = async () => {
    try {
      const versions = await getRunnerVersions()
      setBuildVersions(versions)
      setBuildLogOpen(true)
    } catch {
      showError(t('plugin.buildError', 'Failed to load build logs'))
    }
  }

  const handleManualBuild = async () => {
    setBuildTriggering(true)
    try {
      await startRunnerBuild()
      showSuccess(t('plugin.buildStarted', 'Runner build started'))
      setTimeout(loadRunnerStatus, 1000)
    } catch {
      showError(t('plugin.buildError', 'Failed to start build'))
    } finally {
      setBuildTriggering(false)
    }
  }

  const stageTypeLabel = (type: string | undefined) => {
    switch (type) {
      case 'native':
        return 'Native Go'
      case 'script':
        return 'Script JS'
      default:
        return '-'
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
      headerName: t('plugin.type', 'Type'),
      width: 120,
      renderCell: (params: GridRenderCellParams) => (
        <Chip
          size="small"
          icon={
            params.value === 'native'
              ? <CodeIcon sx={{ fontSize: 14 }} />
              : params.value === 'script'
                ? <JsIcon sx={{ fontSize: 14 }} />
                : undefined
          }
          label={stageTypeLabel(params.value)}
          variant="outlined"
          color={params.value === 'native' ? 'primary' : params.value === 'script' ? 'warning' : 'default'}
        />
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
      field: 'deploy_status',
      headerName: t('plugin.deployStatus', 'Deploy'),
      width: 120,
      renderCell: (params: GridRenderCellParams) => {
        const plugin = params.row as Plugin
        if (plugin.type !== 'native' || !plugin.source_hash) return '-'
        if (!plugin.deployed_hash) {
          return <Chip size="small" label={t('plugin.buildNeeded', 'Build needed')} color="warning" variant="outlined" />
        }
        if (plugin.source_hash !== plugin.deployed_hash) {
          return <Chip size="small" label={t('plugin.buildNeeded', 'Build needed')} color="warning" variant="outlined" />
        }
        return <Chip size="small" label={t('plugin.deployed', 'Deployed')} color="success" variant="outlined" />
      },
    },
    {
      field: 'last_test_passed',
      headerName: t('plugin.testStatus', 'Test'),
      width: 100,
      renderCell: (params: GridRenderCellParams) => {
        const plugin = params.row as Plugin
        if (!plugin.type || plugin.type === undefined) return '-'
        if (!plugin.last_test_at) return <Chip size="small" label={t('plugin.untested', 'Untested')} variant="outlined" />
        return plugin.last_test_passed
          ? <Chip size="small" label={t('plugin.testPassed', 'Passed')} color="success" variant="outlined" />
          : <Chip size="small" label={t('plugin.testFailed', 'Failed')} color="error" variant="outlined" />
      },
    },
    {
      field: 'source_repo',
      headerName: t('plugin.sourceRepo'),
      flex: 1,
      minWidth: 150,
      renderCell: (params: GridRenderCellParams) =>
        params.value ? (
          <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.8rem' }}>
            {params.value}
          </Typography>
        ) : (
          '-'
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
      width: 100,
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

      {/* Runner Build Status Banner */}
      {runnerStatus && runnerStatus.needs_build && (
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={
            <Box sx={{ display: 'flex', gap: 1 }}>
              <Button color="warning" size="small" onClick={handleViewBuildLogs}>
                {t('plugin.viewLogs', 'Logs')}
              </Button>
              <Button
                color="warning"
                size="small"
                startIcon={<BuildIcon />}
                onClick={handleManualBuild}
                disabled={buildTriggering}
              >
                {buildTriggering ? t('plugin.buildTriggering', 'Starting...') : t('plugin.buildNow', 'Build Now')}
              </Button>
            </Box>
          }
        >
          <Typography variant="body2">
            {t('plugin.buildNeededBanner', 'Runner build needed — {{count}} plugin(s) have pending changes', {
              count: runnerStatus.plugins.filter((p) => p.needs_build).length,
            })}
          </Typography>
          {runnerStatus.latest_ready_version && (
            <Typography variant="caption" color="text.secondary">
              {t('plugin.lastBuild', 'Latest ready build')}: {runnerStatus.latest_ready_version.id}
            </Typography>
          )}
        </Alert>
      )}

      {runnerStatus && !runnerStatus.needs_build && runnerStatus.plugins.length > 0 && (
        <Alert
          severity="success"
          sx={{ mb: 2 }}
          action={
            <Button color="success" size="small" onClick={handleViewBuildLogs}>
              {t('plugin.viewLogs', 'Logs')}
            </Button>
          }
        >
          <Typography variant="body2">
            {t('plugin.buildUpToDate', 'Runner is up to date — all native plugins deployed')}
          </Typography>
          {runnerStatus.latest_ready_version && (
            <Typography variant="caption" color="text.secondary">
              {t('plugin.lastBuild', 'Latest ready build')}: {runnerStatus.latest_ready_version.id}
              {runnerStatus.latest_ready_version.image_tag && ` (${runnerStatus.latest_ready_version.image_tag})`}
            </Typography>
          )}
        </Alert>
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
                if (!plugin) return null
                return (
                  <Box sx={{ mt: 2, p: 2, bgcolor: 'grey.50', borderRadius: 1 }}>
                    {/* Source Code Preview */}
                    {plugin.source_code && (
                      <Box sx={{ mb: 2 }}>
                        <Typography variant="subtitle2" sx={{ mb: 1 }}>
                          {plugin.type === 'native'
                            ? t('plugin.sourceCodeGo', 'Source Code (Go)')
                            : t('plugin.sourceCodeJs', 'Source Code (JavaScript)')}
                        </Typography>
                        <Box
                          sx={{
                            bgcolor: '#1e1e1e',
                            color: '#d4d4d4',
                            p: 1.5,
                            borderRadius: 1,
                            fontFamily: 'monospace',
                            fontSize: '0.8rem',
                            whiteSpace: 'pre-wrap',
                            maxHeight: 250,
                            overflow: 'auto',
                          }}
                        >
                          {plugin.source_code}
                        </Box>
                        {plugin.source_hash && (
                          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                            Hash: {plugin.source_hash.substring(0, 12)}...
                            {plugin.deployed_hash && plugin.source_hash !== plugin.deployed_hash && (
                              <Chip size="small" label={t('plugin.buildNeeded', 'Build needed')} color="warning" variant="outlined" sx={{ ml: 1 }} />
                            )}
                          </Typography>
                        )}
                      </Box>
                    )}

                    {/* Test History */}
                    {plugin.last_test_at && (
                      <Box sx={{ mb: 2 }}>
                        <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
                          {t('plugin.lastTestResult', 'Last Test Result')}
                        </Typography>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <Chip
                            size="small"
                            label={plugin.last_test_passed ? t('plugin.testPassed', 'Passed') : t('plugin.testFailed', 'Failed')}
                            color={plugin.last_test_passed ? 'success' : 'error'}
                          />
                          <Typography variant="caption" color="text.secondary">
                            {new Date(plugin.last_test_at).toLocaleString()}
                          </Typography>
                        </Box>
                        {plugin.last_test_error && (
                          <Typography variant="caption" color="error" sx={{ mt: 0.5, display: 'block', fontFamily: 'monospace' }}>
                            {plugin.last_test_error}
                          </Typography>
                        )}
                      </Box>
                    )}

                    {/* Stages List */}
                    {plugin.stages && plugin.stages.length > 0 && (
                      <>
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
                      </>
                    )}

                    {/* No content */}
                    {!plugin.source_code && (!plugin.stages || plugin.stages.length === 0) && (
                      <Typography variant="body2" color="text.secondary">
                        {t('plugin.noContent', 'No source code or stages registered.')}
                      </Typography>
                    )}
                  </Box>
                )
              })()}
            </Collapse>
          )}
        </CardContent>
      </Card>

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="lg" fullWidth>
        <DialogTitle>
          {selectedPlugin ? t('plugin.edit') : t('plugin.register')}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            {/* Basic Info */}
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

            {/* Stage Mode Selector */}
            <Box>
              <Typography variant="subtitle1" sx={{ mb: 1 }}>
                {t('plugin.stageCodeType', 'Stage Code Type')}
              </Typography>
              <ToggleButtonGroup
                value={formData.stageMode}
                exclusive
                onChange={(_, value) => {
                  if (value !== null) {
                    setFormData((prev) => ({ ...prev, stageMode: value }))
                    setTestPassed(false)
                  }
                }}
                sx={{ mb: 1 }}
              >
                <ToggleButton value="none" sx={{ px: 3 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <CategoryIcon sx={{ fontSize: 20 }} />
                    <Box sx={{ textAlign: 'left' }}>
                      <Typography variant="body2" sx={{ textTransform: 'none', fontWeight: 600 }}>
                        {t('plugin.modeNone', 'None')}
                      </Typography>
                      <Typography variant="caption" sx={{ textTransform: 'none', display: 'block', color: 'text.secondary' }}>
                        {t('plugin.modeNoneDesc', 'Metadata only')}
                      </Typography>
                    </Box>
                  </Box>
                </ToggleButton>
                <ToggleButton value="script" sx={{ px: 3 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <JsIcon sx={{ fontSize: 20, color: '#f7df1e' }} />
                    <Box sx={{ textAlign: 'left' }}>
                      <Typography variant="body2" sx={{ textTransform: 'none', fontWeight: 600 }}>
                        Script (JavaScript)
                      </Typography>
                      <Typography variant="caption" sx={{ textTransform: 'none', display: 'block', color: 'text.secondary' }}>
                        {t('plugin.modeScriptDesc', 'ES6, instant test')}
                      </Typography>
                    </Box>
                  </Box>
                </ToggleButton>
                <ToggleButton value="native" sx={{ px: 3 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <CodeIcon sx={{ fontSize: 20, color: '#00ADD8' }} />
                    <Box sx={{ textAlign: 'left' }}>
                      <Typography variant="body2" sx={{ textTransform: 'none', fontWeight: 600 }}>
                        Native (Go)
                      </Typography>
                      <Typography variant="caption" sx={{ textTransform: 'none', display: 'block', color: 'text.secondary' }}>
                        {t('plugin.modeNativeDesc', 'Full Go, server build')}
                      </Typography>
                    </Box>
                  </Box>
                </ToggleButton>
              </ToggleButtonGroup>
            </Box>

            {/* Script Stage Editor */}
            {formData.stageMode === 'script' && (
              <Card variant="outlined">
                <CardContent>
                  <JSScriptStageEditor
                    value={formData.scriptConfig}
                    onChange={(val) => {
                      setFormData((prev) => ({ ...prev, scriptConfig: val }))
                    }}
                    onTestResult={setTestPassed}
                    pluginName={selectedPlugin?.name}
                  />
                  {testPassed && (
                    <Alert severity="success" icon={<ActiveIcon />} sx={{ mt: 2 }}>
                      {t('plugin.scriptTestPassed', 'Script test passed — Save enabled')}
                    </Alert>
                  )}
                </CardContent>
              </Card>
            )}

            {/* Native Stage Editor */}
            {formData.stageMode === 'native' && (
              <NativeStageEditor
                sourceCode={formData.source_code}
                goMod={formData.go_mod}
                onSourceChange={(src) => setFormData((prev) => ({ ...prev, source_code: src }))}
                onGoModChange={(mod) => setFormData((prev) => ({ ...prev, go_mod: mod }))}
                onTestPassed={setTestPassed}
                pluginName={selectedPlugin?.name}
              />
            )}

            {/* Stages Metadata Section */}
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
        <DialogActions sx={{ px: 3, pb: 2 }}>
          {needsTest && !testPassed && (
            <Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>
              {t('plugin.nativeTestRequired', 'Test must pass before saving')}
            </Typography>
          )}
          <Button onClick={() => setDialogOpen(false)}>{t('common.cancel')}</Button>
          <Button
            variant="contained"
            onClick={handleSubmit}
            disabled={!formData.name || (needsTest && !testPassed)}
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

      {/* Build Log Dialog */}
      <Dialog open={buildLogOpen} onClose={() => setBuildLogOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{t('plugin.buildHistory', 'Runner Build History')}</DialogTitle>
        <DialogContent>
          {buildVersions.length === 0 ? (
            <Typography color="text.secondary">{t('common.noData')}</Typography>
          ) : (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              {buildVersions.map((v) => (
                <Card key={v.id} variant="outlined">
                  <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                      <Typography variant="subtitle2">{v.id}</Typography>
                      <Chip
                        size="small"
                        label={v.status}
                        color={v.status === 'ready' ? 'success' : v.status === 'failed' ? 'error' : v.status === 'building' ? 'info' : 'default'}
                      />
                      {v.duration_ms && (
                        <Chip size="small" label={`${(v.duration_ms / 1000).toFixed(1)}s`} variant="outlined" />
                      )}
                      {v.started_at && (
                        <Typography variant="caption" color="text.secondary">
                          {new Date(v.started_at).toLocaleString()}
                        </Typography>
                      )}
                    </Box>
                    {v.image_tag && (
                      <Typography variant="caption" sx={{ fontFamily: 'monospace', display: 'block', mb: 0.5 }}>
                        {v.image_tag}
                      </Typography>
                    )}
                    {v.error && (
                      <Alert severity="error" sx={{ mb: 1, py: 0 }}>
                        <Typography variant="caption">{v.error}</Typography>
                      </Alert>
                    )}
                    {v.build_log && (
                      <Box
                        sx={{
                          bgcolor: '#1e1e1e',
                          color: '#d4d4d4',
                          p: 1,
                          borderRadius: 1,
                          fontFamily: 'monospace',
                          fontSize: '0.75rem',
                          whiteSpace: 'pre-wrap',
                          maxHeight: 200,
                          overflow: 'auto',
                        }}
                      >
                        {v.build_log}
                      </Box>
                    )}
                  </CardContent>
                </Card>
              ))}
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setBuildLogOpen(false)}>{t('common.cancel')}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
