/**
 * NativeStageEditor - Go Native Stage 코드 에디터
 *
 * Monaco Editor(Go 하이라이팅) + go.mod 탭 + Config/SampleData/Result 패널
 * 서버 사이드 go build + 실행 테스트, 테스트 성공 시에만 Save 활성화
 * gopls LSP 연동: 자동완성, hover, 진단 (WebSocket 연결 가능 시)
 */
import { useState, useCallback, useEffect, useRef } from 'react'
import {
  Box,
  Stack,
  Typography,
  TextField,
  Button,
  Alert,
  Chip,
  Paper,
  Divider,
  Tab,
  Tabs,
  CircularProgress,
} from '@mui/material'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ErrorIcon from '@mui/icons-material/Error'
import WarningIcon from '@mui/icons-material/Warning'
import BuildIcon from '@mui/icons-material/Build'
import SmartToyIcon from '@mui/icons-material/SmartToy'
import Editor, { type OnMount, type Monaco } from '@monaco-editor/react'
import type { editor as monacoEditor, IDisposable, IPosition } from 'monaco-editor'
import { useTranslation } from 'react-i18next'
import { testNativePlugin } from '../../services/pluginApi'
import type { TestNativePluginResponse } from '../../types/plugin'
import { LSPClient, lspKindToMonaco, lspSeverityToMonaco } from '../../services/lspClient'
import type { LSPDiagnostic } from '../../services/lspClient'

// pluginNameToStructName converts plugin name (e.g. "ip-filter") to PascalCase struct name (e.g. "IpFilterStage")
function pluginNameToStructName(name?: string): string {
  if (!name) return 'MyStage'
  const pascal = name
    .split(/[-_\s]+/)
    .map(w => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
    .join('')
  // Go 식별자는 숫자로 시작 불가
  const safe = /^\d/.test(pascal) ? 'S' + pascal : pascal
  return safe.endsWith('Stage') ? safe : safe + 'Stage'
}

function generateDefaultSource(structName: string): string {
  return `package main

import (
	sdk "github.com/conduix/conduix/plugin-sdk"
)

// ${structName} 커스텀 Stage
type ${structName} struct {
	sdk.BaseNativeStage
	// config fields
}

func (s *${structName}) Init(config map[string]any) error {
	return nil
}

func (s *${structName}) Process(record map[string]any) (map[string]any, error) {
	// Transform each record. Return nil to drop.
	record["processed"] = true
	return record, nil
}

// Stage 반드시 이 변수를 선언해야 합니다
var Stage sdk.NativeStage = &${structName}{}
`
}

const DEFAULT_GO_MOD = `module conduix-plugin-test

go 1.26

require github.com/conduix/conduix/plugin-sdk v0.0.0
`

const DEFAULT_CONFIG = JSON.stringify({}, null, 2)

const DEFAULT_SAMPLE = JSON.stringify(
  [{ name: 'example', level: 'info', value: 42 }],
  null,
  2
)

interface NativeStageEditorProps {
  sourceCode: string
  goMod: string
  onSourceChange: (source: string) => void
  onGoModChange: (goMod: string) => void
  onTestPassed: (passed: boolean) => void
  disabled?: boolean
  pluginName?: string // 기존 플러그인이면 테스트 결과 DB 기록
}

export default function NativeStageEditor({
  sourceCode,
  goMod,
  onSourceChange,
  onGoModChange,
  onTestPassed,
  disabled,
  pluginName,
}: NativeStageEditorProps) {
  const { t } = useTranslation()
  const structName = pluginNameToStructName(pluginName)
  const code = sourceCode || generateDefaultSource(structName)
  const mod = goMod || DEFAULT_GO_MOD

  const [editorTab, setEditorTab] = useState(0) // 0: main.go, 1: go.mod
  const [configData, setConfigData] = useState(DEFAULT_CONFIG)
  const [sampleData, setSampleData] = useState(DEFAULT_SAMPLE)
  const [testResult, setTestResult] = useState<TestNativePluginResponse | null>(null)
  const [testing, setTesting] = useState(false)
  const [lspConnected, setLspConnected] = useState(false)
  const lspClientRef = useRef<LSPClient | null>(null)
  const editorRef = useRef<monacoEditor.IStandaloneCodeEditor | null>(null)
  const monacoRef = useRef<Monaco | null>(null)
  const disposablesRef = useRef<IDisposable[]>([])

  // LSP 세션 ID (컴포넌트 마운트당 고유)
  const sessionIdRef = useRef(`lsp-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`)

  // LSP 연결 (gopls 사용 가능 시)
  useEffect(() => {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${wsProtocol}//${window.location.host}/api/v1/lsp/go`

    const client = new LSPClient(wsUrl, sessionIdRef.current)
    lspClientRef.current = client

    client.connect()
      .then(async () => {
        setLspConnected(true)
        // 문서 열기
        const docUri = `file:///workspace/main.go`
        await client.openDocument(docUri, code)
      })
      .catch(() => {
        // gopls 미설치 또는 서버 미지원 — 기본 모드로 동작
        setLspConnected(false)
      })

    return () => {
      client.disconnect()
      lspClientRef.current = null
      setLspConnected(false)
      disposablesRef.current.forEach((d) => d.dispose())
      disposablesRef.current = []
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Monaco Editor 마운트 시 LSP 프로바이더 등록
  const handleEditorMount: OnMount = useCallback((editor: monacoEditor.IStandaloneCodeEditor, monaco: Monaco) => {
    editorRef.current = editor
    monacoRef.current = monaco

    // 이전 disposable 정리
    disposablesRef.current.forEach((d) => d.dispose())
    disposablesRef.current = []

    // CompletionItemProvider 등록
    const completionDisposable = monaco.languages.registerCompletionItemProvider('go', {
      triggerCharacters: ['.', '('],
      provideCompletionItems: async (model: monacoEditor.ITextModel, position: IPosition) => {
        const client = lspClientRef.current
        if (!client?.isConnected) return { suggestions: [] }

        try {
          const items = await client.completion(position.lineNumber - 1, position.column - 1)
          const word = model.getWordUntilPosition(position)
          const range = new monaco.Range(
            position.lineNumber,
            word.startColumn,
            position.lineNumber,
            word.endColumn
          )

          return {
            suggestions: items.map((item) => ({
              label: item.label,
              kind: lspKindToMonaco(item.kind),
              detail: item.detail || '',
              documentation: typeof item.documentation === 'string'
                ? item.documentation
                : item.documentation?.value || '',
              insertText: item.textEdit?.newText || item.insertText || item.label,
              range,
              sortText: item.sortText,
              filterText: item.filterText,
            })),
          }
        } catch {
          return { suggestions: [] }
        }
      },
    })
    disposablesRef.current.push(completionDisposable)

    // HoverProvider 등록
    const hoverDisposable = monaco.languages.registerHoverProvider('go', {
      provideHover: async (_model: monacoEditor.ITextModel, position: IPosition) => {
        const client = lspClientRef.current
        if (!client?.isConnected) return null

        try {
          const result = await client.hover(position.lineNumber - 1, position.column - 1)
          if (!result) return null

          let value = ''
          if (typeof result.contents === 'string') {
            value = result.contents
          } else if (Array.isArray(result.contents)) {
            value = result.contents
              .map((c) => (typeof c === 'string' ? c : c.value))
              .join('\n\n')
          } else {
            value = result.contents.value
          }

          return {
            contents: [{ value: `\`\`\`go\n${value}\n\`\`\`` }],
          }
        } catch {
          return null
        }
      },
    })
    disposablesRef.current.push(hoverDisposable)

    // Diagnostics 핸들러 등록
    const client = lspClientRef.current
    if (client) {
      client.setDiagnosticsHandler((diagnostics: LSPDiagnostic[]) => {
        const model = editor.getModel()
        if (!model) return

        const markers = diagnostics.map((d) => ({
          severity: lspSeverityToMonaco(d.severity),
          startLineNumber: d.range.start.line + 1,
          startColumn: d.range.start.character + 1,
          endLineNumber: d.range.end.line + 1,
          endColumn: d.range.end.character + 1,
          message: d.message,
          source: d.source || 'gopls',
        }))
        monaco.editor.setModelMarkers(model, 'gopls', markers)
      })
    }
  }, [])

  const handleCodeChange = useCallback(
    (value: string | undefined) => {
      const newCode = value || ''
      onSourceChange(newCode)
      onTestPassed(false) // 코드 변경 시 테스트 무효화
      setTestResult(null)
      // LSP didChange 전송
      lspClientRef.current?.changeDocument(newCode)
    },
    [onSourceChange, onTestPassed]
  )

  const handleGoModChange = useCallback(
    (value: string | undefined) => {
      onGoModChange(value || '')
      onTestPassed(false)
      setTestResult(null)
    },
    [onGoModChange, onTestPassed]
  )

  const handleTest = useCallback(async () => {
    setTesting(true)
    setTestResult(null)

    try {
      let parsedConfig: Record<string, unknown> = {}
      try {
        parsedConfig = JSON.parse(configData)
      } catch {
        setTestResult({
          success: false,
          build_error: 'Invalid Config JSON',
        })
        setTesting(false)
        return
      }

      let parsedSample: Record<string, unknown>[]
      try {
        const parsed = JSON.parse(sampleData)
        parsedSample = Array.isArray(parsed) ? parsed : [parsed]
      } catch {
        setTestResult({
          success: false,
          build_error: 'Invalid Sample Data JSON',
        })
        setTesting(false)
        return
      }

      const result = await testNativePlugin({
        source_code: code,
        go_mod: mod,
        config: parsedConfig,
        sample_data: parsedSample,
        plugin_name: pluginName,
      })

      setTestResult(result)
      onTestPassed(result.success)
    } catch (err) {
      setTestResult({
        success: false,
        build_error: `Request failed: ${err instanceof Error ? err.message : String(err)}`,
      })
      onTestPassed(false)
    } finally {
      setTesting(false)
    }
  }, [code, mod, configData, sampleData, onTestPassed, pluginName])

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* Editor Section */}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Box sx={{ borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center' }}>
          <Tabs
            value={editorTab}
            onChange={(_, v) => setEditorTab(v)}
            sx={{ minHeight: 36, '& .MuiTab-root': { minHeight: 36, py: 0 }, flex: 1 }}
          >
            <Tab label="main.go" sx={{ textTransform: 'none', fontFamily: 'monospace' }} />
            <Tab label="go.mod" sx={{ textTransform: 'none', fontFamily: 'monospace' }} />
          </Tabs>
          {lspConnected && (
            <Chip
              icon={<SmartToyIcon sx={{ fontSize: 14 }} />}
              label="gopls"
              size="small"
              color="success"
              variant="outlined"
              sx={{ mr: 1, fontSize: '0.7rem', height: 22 }}
            />
          )}
        </Box>

        {editorTab === 0 && (
          <Editor
            height="350px"
            language="go"
            value={code}
            onChange={handleCodeChange}
            onMount={handleEditorMount}
            theme="vs-dark"
            options={{
              fontSize: 13,
              minimap: { enabled: false },
              scrollBeyondLastLine: false,
              readOnly: disabled,
              tabSize: 4,
              insertSpaces: false,
              automaticLayout: true,
              quickSuggestions: true,
              suggestOnTriggerCharacters: true,
            }}
          />
        )}

        {editorTab === 1 && (
          <Editor
            height="150px"
            language="go"
            value={mod}
            onChange={handleGoModChange}
            theme="vs-dark"
            options={{
              fontSize: 13,
              minimap: { enabled: false },
              scrollBeyondLastLine: false,
              readOnly: disabled,
              tabSize: 4,
              automaticLayout: true,
            }}
          />
        )}
      </Paper>

      {/* Test Section */}
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1.5 }}>
          <Typography variant="subtitle2">
            <BuildIcon sx={{ fontSize: 16, mr: 0.5, verticalAlign: 'text-bottom' }} />
            {t('plugin.nativeTest', 'Build & Test')}
          </Typography>
          <Button
            size="small"
            variant="contained"
            startIcon={testing ? <CircularProgress size={16} /> : <PlayArrowIcon />}
            onClick={handleTest}
            disabled={testing || disabled || !code.trim()}
          >
            {testing ? t('plugin.nativeTesting', 'Testing...') : t('plugin.nativeRunTest', 'Test')}
          </Button>
        </Stack>

        <Stack direction="row" spacing={2} sx={{ mb: 1.5 }}>
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>
              Config (JSON)
            </Typography>
            <TextField
              value={configData}
              onChange={(e) => setConfigData(e.target.value)}
              multiline
              rows={3}
              fullWidth
              size="small"
              sx={{ '& textarea': { fontFamily: 'monospace', fontSize: 12 } }}
            />
          </Box>
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>
              Sample Data (JSON)
            </Typography>
            <TextField
              value={sampleData}
              onChange={(e) => setSampleData(e.target.value)}
              multiline
              rows={3}
              fullWidth
              size="small"
              sx={{ '& textarea': { fontFamily: 'monospace', fontSize: 12 } }}
            />
          </Box>
        </Stack>

        <Divider sx={{ my: 1.5 }} />

        {/* Result */}
        {testResult && (
          <Box>
            {/* Security Check */}
            {testResult.security_check && !testResult.security_check.passed && (
              <Alert severity="error" sx={{ mb: 1 }}>
                <Typography variant="subtitle2">Security Check Failed</Typography>
                {testResult.security_check.errors?.map((e, i) => (
                  <Typography key={i} variant="body2" sx={{ fontFamily: 'monospace', fontSize: 12 }}>
                    {e}
                  </Typography>
                ))}
              </Alert>
            )}

            {testResult.security_check?.warnings?.map((w, i) => (
              <Alert key={i} severity="warning" icon={<WarningIcon />} sx={{ mb: 1 }}>
                {w}
              </Alert>
            ))}

            {/* Build Error */}
            {testResult.build_error && (
              <Alert severity="error" sx={{ mb: 1 }}>
                <Typography variant="subtitle2">Build Error</Typography>
                <Typography
                  variant="body2"
                  component="pre"
                  sx={{ fontFamily: 'monospace', fontSize: 12, whiteSpace: 'pre-wrap', mt: 0.5 }}
                >
                  {testResult.build_error}
                </Typography>
              </Alert>
            )}

            {/* Exec Error */}
            {testResult.exec_error && (
              <Alert severity="error" sx={{ mb: 1 }}>
                <Typography variant="subtitle2">Execution Error</Typography>
                <Typography
                  variant="body2"
                  component="pre"
                  sx={{ fontFamily: 'monospace', fontSize: 12, whiteSpace: 'pre-wrap', mt: 0.5 }}
                >
                  {testResult.exec_error}
                </Typography>
              </Alert>
            )}

            {/* Success */}
            {testResult.success && (
              <Alert severity="success" icon={<CheckCircleIcon />} sx={{ mb: 1 }}>
                <Stack direction="row" spacing={2} alignItems="center">
                  <Typography variant="subtitle2">
                    {t('plugin.nativeTestPassed', 'Test Passed')}
                  </Typography>
                  {testResult.build_elapsed && (
                    <Chip size="small" label={`Build: ${testResult.build_elapsed}`} variant="outlined" />
                  )}
                  {testResult.exec_elapsed && (
                    <Chip size="small" label={`Exec: ${testResult.exec_elapsed}`} variant="outlined" />
                  )}
                </Stack>
              </Alert>
            )}

            {/* Output */}
            {testResult.exec_output && testResult.exec_output.length > 0 && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  Output ({testResult.exec_output.length} records)
                </Typography>
                <Paper
                  variant="outlined"
                  sx={{
                    p: 1,
                    mt: 0.5,
                    maxHeight: 200,
                    overflow: 'auto',
                    bgcolor: '#1e1e1e',
                  }}
                >
                  <Typography
                    variant="body2"
                    component="pre"
                    sx={{ fontFamily: 'monospace', fontSize: 12, color: '#d4d4d4', m: 0 }}
                  >
                    {JSON.stringify(testResult.exec_output, null, 2)}
                  </Typography>
                </Paper>
              </Box>
            )}

            {/* Failed status chip */}
            {!testResult.success && (
              <Chip
                icon={<ErrorIcon />}
                label={t('plugin.nativeTestFailed', 'Test Failed')}
                color="error"
                size="small"
                sx={{ mt: 1 }}
              />
            )}
          </Box>
        )}

        {!testResult && !testing && (
          <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center', py: 1 }}>
            {t('plugin.nativeTestHint', 'Click Test to build and run the stage with sample data')}
          </Typography>
        )}
      </Paper>
    </Box>
  )
}
