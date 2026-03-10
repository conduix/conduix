/**
 * ScriptStageEditor - Starlark Script Stage 전용 에디터
 *
 * Monaco Editor(Python 하이라이팅) + 샘플 데이터 입력 + 테스트 실행 UI
 */
import { useState, useCallback } from 'react'
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
  Collapse,
  IconButton,
  Tooltip,
} from '@mui/material'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ExpandLessIcon from '@mui/icons-material/ExpandLess'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ErrorIcon from '@mui/icons-material/Error'
import RemoveCircleIcon from '@mui/icons-material/RemoveCircle'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import Editor from '@monaco-editor/react'
import { testScript } from '../../services/pluginApi'
import type { TestScriptResponse } from '../../services/pluginApi'

const DEFAULT_CODE = `def process(record):
    """Transform each record. Return dict to pass, None to drop."""
    record["processed"] = True
    return record
`

const DEFAULT_SAMPLE = JSON.stringify(
  { name: 'example', level: 'info', value: 42 },
  null,
  2
)

const BUILTIN_FUNCTIONS = [
  { name: 'hash_sha256(s)', desc: 'SHA-256 hash' },
  { name: 'base64_encode(s)', desc: 'Base64 encode' },
  { name: 'base64_decode(s)', desc: 'Base64 decode' },
  { name: 'json_encode(v)', desc: 'JSON serialize' },
  { name: 'json_decode(s)', desc: 'JSON parse' },
  { name: 'regex_match(pattern, s)', desc: 'Regex match' },
  { name: 'regex_replace(pattern, s, repl)', desc: 'Regex replace' },
  { name: 'timestamp_now()', desc: 'Current UTC time' },
  { name: 'timestamp_parse(s, format)', desc: 'Parse timestamp' },
  { name: 'log(level, message)', desc: 'Log message' },
]

interface ScriptStageEditorProps {
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
  disabled?: boolean
}

export default function ScriptStageEditor({
  value,
  onChange,
  disabled,
}: ScriptStageEditorProps) {
  const code = (value.code as string) || DEFAULT_CODE
  const timeout = (value.timeout as string) || '1s'

  const [sampleData, setSampleData] = useState(DEFAULT_SAMPLE)
  const [testResult, setTestResult] = useState<TestScriptResponse | null>(null)
  const [testing, setTesting] = useState(false)
  const [showBuiltins, setShowBuiltins] = useState(false)

  const handleCodeChange = useCallback(
    (newCode: string | undefined) => {
      onChange({ ...value, code: newCode || '' })
    },
    [value, onChange]
  )

  const handleTimeoutChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange({ ...value, timeout: e.target.value })
    },
    [value, onChange]
  )

  const handleTest = useCallback(async () => {
    setTesting(true)
    setTestResult(null)
    try {
      let parsedData: Record<string, unknown>
      try {
        parsedData = JSON.parse(sampleData)
      } catch {
        setTestResult({
          success: false,
          dropped: false,
          error: 'Invalid JSON in sample data',
          elapsed: '0s',
        })
        return
      }

      const result = await testScript({
        code,
        timeout: timeout || '1s',
        sample_data: parsedData,
      })
      setTestResult(result)
    } catch (err) {
      setTestResult({
        success: false,
        dropped: false,
        error: err instanceof Error ? err.message : 'Test failed',
        elapsed: '0s',
      })
    } finally {
      setTesting(false)
    }
  }, [code, timeout, sampleData])

  const copyOutput = useCallback(() => {
    if (testResult?.output) {
      navigator.clipboard.writeText(JSON.stringify(testResult.output, null, 2))
    }
  }, [testResult])

  return (
    <Stack spacing={2}>
      {/* Code Editor */}
      <Box>
        <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 1 }}>
          <Typography variant="subtitle2">
            Starlark Code <Chip label="required" size="small" color="error" variant="outlined" sx={{ ml: 1 }} />
          </Typography>
          <Button
            size="small"
            onClick={() => setShowBuiltins(!showBuiltins)}
            endIcon={showBuiltins ? <ExpandLessIcon /> : <ExpandMoreIcon />}
          >
            Built-in Functions
          </Button>
        </Stack>

        <Collapse in={showBuiltins}>
          <Paper variant="outlined" sx={{ p: 1.5, mb: 1, bgcolor: 'action.hover' }}>
            <Stack direction="row" flexWrap="wrap" gap={0.5}>
              {BUILTIN_FUNCTIONS.map((fn) => (
                <Tooltip key={fn.name} title={fn.desc} arrow>
                  <Chip
                    label={fn.name}
                    size="small"
                    variant="outlined"
                    sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }}
                  />
                </Tooltip>
              ))}
            </Stack>
          </Paper>
        </Collapse>

        <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
          <Editor
            height="300px"
            language="python"
            theme="vs-dark"
            value={code}
            onChange={handleCodeChange}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              lineNumbers: 'on',
              scrollBeyondLastLine: false,
              wordWrap: 'on',
              tabSize: 4,
              insertSpaces: true,
              readOnly: disabled,
              automaticLayout: true,
            }}
          />
        </Paper>
      </Box>

      {/* Timeout */}
      <TextField
        label="Timeout"
        value={timeout}
        onChange={handleTimeoutChange}
        size="small"
        helperText="Per-record timeout (e.g. 1s, 500ms, 2s)"
        disabled={disabled}
        sx={{ maxWidth: 200 }}
      />

      <Divider />

      {/* Test Section */}
      <Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Test
        </Typography>

        <Stack direction={{ xs: 'column', md: 'row' }} spacing={2}>
          {/* Sample Data Input */}
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>
              Sample Data (JSON)
            </Typography>
            <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
              <Editor
                height="150px"
                language="json"
                theme="vs-dark"
                value={sampleData}
                onChange={(v) => setSampleData(v || '{}')}
                options={{
                  minimap: { enabled: false },
                  fontSize: 12,
                  lineNumbers: 'off',
                  scrollBeyondLastLine: false,
                  wordWrap: 'on',
                  tabSize: 2,
                  readOnly: disabled,
                  automaticLayout: true,
                }}
              />
            </Paper>
          </Box>

          {/* Result Output */}
          <Box sx={{ flex: 1 }}>
            <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>
              Result
              {testResult && (
                <Chip
                  label={testResult.elapsed}
                  size="small"
                  variant="outlined"
                  sx={{ ml: 1, fontSize: '0.7rem' }}
                />
              )}
              {testResult?.output && (
                <IconButton size="small" onClick={copyOutput} sx={{ ml: 0.5 }}>
                  <ContentCopyIcon sx={{ fontSize: 14 }} />
                </IconButton>
              )}
            </Typography>
            <Paper
              variant="outlined"
              sx={{
                height: 150,
                overflow: 'auto',
                bgcolor: 'grey.900',
                p: 1.5,
                fontFamily: 'monospace',
                fontSize: '0.8rem',
                color: 'grey.300',
                whiteSpace: 'pre-wrap',
              }}
            >
              {testResult ? (
                testResult.success ? (
                  testResult.dropped ? (
                    <Stack direction="row" alignItems="center" gap={1}>
                      <RemoveCircleIcon color="warning" fontSize="small" />
                      <span>Record dropped (None returned)</span>
                    </Stack>
                  ) : (
                    JSON.stringify(testResult.output, null, 2)
                  )
                ) : (
                  <Box sx={{ color: 'error.light' }}>{testResult.error}</Box>
                )
              ) : (
                <Box sx={{ color: 'grey.600' }}>Click &quot;Run Test&quot; to see results</Box>
              )}
            </Paper>
          </Box>
        </Stack>

        <Button
          variant="contained"
          startIcon={<PlayArrowIcon />}
          onClick={handleTest}
          disabled={disabled || testing || !code.trim()}
          sx={{ mt: 1.5 }}
          size="small"
        >
          {testing ? 'Running...' : 'Run Test'}
        </Button>

        {testResult && (
          <Alert
            severity={testResult.success ? 'success' : 'error'}
            icon={testResult.success ? <CheckCircleIcon /> : <ErrorIcon />}
            sx={{ mt: 1 }}
          >
            {testResult.success
              ? testResult.dropped
                ? 'Script executed — record dropped'
                : 'Script executed successfully'
              : `Error: ${testResult.error}`}
          </Alert>
        )}
      </Box>
    </Stack>
  )
}
