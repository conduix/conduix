import { useState, useCallback, useRef, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Box,
  Card,
  CardContent,
  Button,
  Typography,
  TextField,
  Stack,
  Chip,
  Alert,
  LinearProgress,
  IconButton,
  Tooltip,
  Paper,
} from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import BuildIcon from '@mui/icons-material/Build'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ErrorIcon from '@mui/icons-material/Error'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import Editor from '@monaco-editor/react'
import { useTranslation } from 'react-i18next'
import { useSnackbar } from '../hooks/useSnackbar'
import { buildPlugin, getBuild, validateSource } from '../services/pluginApi'
import type { PluginBuild } from '../types/plugin'

const DEFAULT_SOURCE = `package main

import (
\tsdk "github.com/conduix/conduix/plugin-sdk"
)

type MyTransform struct {
\t// Add your fields here
}

func (t *MyTransform) Init(config map[string]any) error {
\treturn nil
}

func (t *MyTransform) ProcessBatch(records []*sdk.Record) ([]*sdk.Record, error) {
\tfor _, r := range records {
\t\t// Transform each record
\t\t_ = r
\t}
\treturn records, nil
}

func (t *MyTransform) Close() error {
\treturn nil
}

func main() {
\tsdk.Serve(&MyTransform{})
}
`

const DEFAULT_GOMOD = `module conduix-plugin-my-plugin

go 1.26

require github.com/conduix/conduix/plugin-sdk v0.0.0
`

export default function PluginBuilderPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { showSuccess, showError, showInfo } = useSnackbar()
  const [searchParams] = useSearchParams()

  const [pluginName, setPluginName] = useState(searchParams.get('name') || '')
  const [version, setVersion] = useState(searchParams.get('version') || 'v1.0.0')
  const [sourceCode, setSourceCode] = useState(DEFAULT_SOURCE)
  const [goMod, setGoMod] = useState(DEFAULT_GOMOD)
  const [activeTab, setActiveTab] = useState<'source' | 'gomod'>('source')

  // Validation
  const [validating, setValidating] = useState(false)
  const [validationResult, setValidationResult] = useState<{ valid: boolean; imports?: string[]; error?: string } | null>(null)

  // Build
  const [building, setBuilding] = useState(false)
  const [currentBuild, setCurrentBuild] = useState<PluginBuild | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Cleanup poll on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  const handleValidate = useCallback(async () => {
    setValidating(true)
    setValidationResult(null)
    try {
      const result = await validateSource(sourceCode)
      setValidationResult({ valid: result.valid, imports: result.imports })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      setValidationResult({ valid: false, error: msg })
    } finally {
      setValidating(false)
    }
  }, [sourceCode])

  const handleBuild = useCallback(async () => {
    if (!pluginName.trim()) {
      showError(t('pluginBuilder.nameRequired'))
      return
    }

    setBuilding(true)
    setCurrentBuild(null)

    try {
      const build = await buildPlugin({
        name: pluginName,
        version,
        source_code: sourceCode,
        go_mod: goMod || undefined,
      })

      setCurrentBuild(build)
      showInfo(t('pluginBuilder.buildStarted'))

      // Poll for build status
      pollRef.current = setInterval(async () => {
        try {
          const updated = await getBuild(build.id)
          setCurrentBuild(updated)

          if (updated.status === 'success' || updated.status === 'failed') {
            if (pollRef.current) clearInterval(pollRef.current)
            pollRef.current = null
            setBuilding(false)

            if (updated.status === 'success') {
              showSuccess(t('pluginBuilder.buildSuccess'))
            } else {
              showError(t('pluginBuilder.buildFailed'))
            }
          }
        } catch {
          if (pollRef.current) clearInterval(pollRef.current)
          pollRef.current = null
          setBuilding(false)
        }
      }, 2000)
    } catch (err: unknown) {
      setBuilding(false)
      const msg = err instanceof Error ? err.message : String(err)
      showError(msg)
    }
  }, [pluginName, version, sourceCode, goMod, showError, showInfo, showSuccess, t])

  const copyBuildLog = useCallback(() => {
    if (currentBuild?.build_log) {
      navigator.clipboard.writeText(currentBuild.build_log)
      showSuccess('Copied to clipboard')
    }
  }, [currentBuild, showSuccess])

  return (
    <Box>
      {/* Header */}
      <Stack direction="row" alignItems="center" spacing={2} sx={{ mb: 3 }}>
        <IconButton onClick={() => navigate('/plugins')}>
          <ArrowBackIcon />
        </IconButton>
        <BuildIcon color="primary" sx={{ fontSize: 28 }} />
        <Typography variant="h5">{t('pluginBuilder.title')}</Typography>
      </Stack>

      {/* Plugin metadata */}
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <Stack direction="row" spacing={2}>
            <TextField
              label={t('plugin.name')}
              value={pluginName}
              onChange={(e) => setPluginName(e.target.value)}
              size="small"
              required
              sx={{ minWidth: 250 }}
            />
            <TextField
              label={t('plugin.version')}
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              size="small"
              sx={{ minWidth: 150 }}
            />
            <Button
              variant="outlined"
              size="small"
              onClick={handleValidate}
              disabled={validating || !sourceCode.trim()}
              startIcon={<CheckCircleIcon />}
            >
              {t('pluginBuilder.validate')}
            </Button>
            <Button
              variant="contained"
              onClick={handleBuild}
              disabled={building || !pluginName.trim()}
              startIcon={<PlayArrowIcon />}
            >
              {building ? t('pluginBuilder.building') : t('pluginBuilder.buildAndRegister')}
            </Button>
          </Stack>

          {/* Validation result */}
          {validationResult && (
            <Box sx={{ mt: 2 }}>
              {validationResult.valid ? (
                <Alert severity="success" sx={{ py: 0 }}>
                  {t('pluginBuilder.validationPassed')}
                  {validationResult.imports && validationResult.imports.length > 0 && (
                    <Box sx={{ mt: 0.5 }}>
                      {validationResult.imports.map((imp) => (
                        <Chip key={imp} label={imp} size="small" sx={{ mr: 0.5, mb: 0.5 }} />
                      ))}
                    </Box>
                  )}
                </Alert>
              ) : (
                <Alert severity="error" sx={{ py: 0 }}>
                  {validationResult.error || t('pluginBuilder.validationFailed')}
                </Alert>
              )}
            </Box>
          )}
        </CardContent>
      </Card>

      {/* Editor */}
      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ p: 0, '&:last-child': { pb: 0 } }}>
          {/* Tab bar */}
          <Stack direction="row" spacing={1} sx={{ px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
            <Chip
              label="main.go"
              onClick={() => setActiveTab('source')}
              color={activeTab === 'source' ? 'primary' : 'default'}
              variant={activeTab === 'source' ? 'filled' : 'outlined'}
              size="small"
            />
            <Chip
              label="go.mod"
              onClick={() => setActiveTab('gomod')}
              color={activeTab === 'gomod' ? 'primary' : 'default'}
              variant={activeTab === 'gomod' ? 'filled' : 'outlined'}
              size="small"
            />
          </Stack>

          {/* Monaco Editor */}
          <Box sx={{ height: 500 }}>
            {activeTab === 'source' ? (
              <Editor
                height="100%"
                language="go"
                theme="vs-dark"
                value={sourceCode}
                onChange={(val) => setSourceCode(val || '')}
                options={{
                  minimap: { enabled: false },
                  fontSize: 13,
                  lineNumbers: 'on',
                  tabSize: 4,
                  scrollBeyondLastLine: false,
                  wordWrap: 'off',
                  automaticLayout: true,
                  folding: true,
                  renderLineHighlight: 'line',
                  cursorBlinking: 'smooth',
                  smoothScrolling: true,
                  padding: { top: 8, bottom: 8 },
                }}
              />
            ) : (
              <Editor
                height="100%"
                language="go"
                theme="vs-dark"
                value={goMod}
                onChange={(val) => setGoMod(val || '')}
                options={{
                  minimap: { enabled: false },
                  fontSize: 13,
                  lineNumbers: 'on',
                  tabSize: 4,
                  scrollBeyondLastLine: false,
                  automaticLayout: true,
                  padding: { top: 8, bottom: 8 },
                }}
              />
            )}
          </Box>
        </CardContent>
      </Card>

      {/* Build progress & log */}
      {(building || currentBuild) && (
        <Card>
          <CardContent>
            <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
              <Typography variant="subtitle1">{t('pluginBuilder.buildLog')}</Typography>
              {currentBuild && (
                <Chip
                  label={currentBuild.status}
                  size="small"
                  color={
                    currentBuild.status === 'success' ? 'success'
                    : currentBuild.status === 'failed' ? 'error'
                    : currentBuild.status === 'building' ? 'warning'
                    : 'default'
                  }
                  icon={currentBuild.status === 'success' ? <CheckCircleIcon /> : currentBuild.status === 'failed' ? <ErrorIcon /> : undefined}
                />
              )}
              {currentBuild?.duration_ms && (
                <Typography variant="caption" color="text.secondary">
                  {(currentBuild.duration_ms / 1000).toFixed(1)}s
                </Typography>
              )}
              {currentBuild?.build_log && (
                <Tooltip title="Copy log">
                  <IconButton size="small" onClick={copyBuildLog}>
                    <ContentCopyIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              )}
            </Stack>

            {building && <LinearProgress sx={{ mb: 1 }} />}

            {currentBuild?.error && (
              <Alert severity="error" sx={{ mb: 1 }}>
                {currentBuild.error}
              </Alert>
            )}

            {currentBuild?.build_log && (
              <Paper
                sx={{
                  p: 1.5,
                  bgcolor: '#1e1e1e',
                  color: '#d4d4d4',
                  fontFamily: 'monospace',
                  fontSize: 12,
                  maxHeight: 300,
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                }}
              >
                {currentBuild.build_log}
              </Paper>
            )}
          </CardContent>
        </Card>
      )}
    </Box>
  )
}
