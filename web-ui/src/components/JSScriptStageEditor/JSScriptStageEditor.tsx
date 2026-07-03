/**
 * JSScriptStageEditor - JavaScript (goja) Script Stage 전용 에디터
 *
 * Monaco Editor(JavaScript 하이라이팅) + 샘플 데이터 입력 + 테스트 실행 UI
 * goja 지원: ES5.1 완전 + ES6 부분 (let/const, arrow fn, template literal, destructuring, etc.)
 * JS 표준 내장 객체 사용 가능: JSON, RegExp, Date, Math, String, Array, Object
 * Go 등록 함수: console.log()만. hash/base64 등은 Go builtin stage 사용.
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
  IconButton,
} from '@mui/material'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ErrorIcon from '@mui/icons-material/Error'
import RemoveCircleIcon from '@mui/icons-material/RemoveCircle'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import Editor from '@monaco-editor/react'
import { testScript } from '../../services/pluginApi'
import type { TestScriptResponse } from '../../services/pluginApi'

const DEFAULT_CODE = `function process(record) {
    // Transform each record. Return object to pass, null to drop.
    record.processed = true;
    return record;
}
`

const DEFAULT_SAMPLE = JSON.stringify(
  { name: 'example', level: 'info', value: 42 },
  null,
  2
)

interface JSScriptStageEditorProps {
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
  disabled?: boolean
  /** 테스트 실행 결과를 부모에게 알려주는 콜백 (success: true/false) */
  onTestResult?: (success: boolean) => void
  /** 기존 플러그인이면 테스트 결과 DB 기록 */
  pluginName?: string
}

export default function JSScriptStageEditor({
  value,
  onChange,
  disabled,
  onTestResult,
  pluginName,
}: JSScriptStageEditorProps) {
  const code = (value.code as string) || DEFAULT_CODE
  const timeout = (value.timeout as string) || '1s'

  const [sampleData, setSampleData] = useState(DEFAULT_SAMPLE)
  const [testResult, setTestResult] = useState<TestScriptResponse | null>(null)
  const [testing, setTesting] = useState(false)

  const handleCodeChange = useCallback(
    (newCode: string | undefined) => {
      onChange({ ...value, code: newCode || '' })
      // 코드 변경 시 테스트 결과 무효화
      setTestResult(null)
      onTestResult?.(false)
    },
    [value, onChange, onTestResult]
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
        plugin_name: pluginName,
      })
      setTestResult(result)
      onTestResult?.(result.success)
    } catch (err) {
      const failResult = {
        success: false,
        dropped: false,
        error: err instanceof Error ? err.message : 'Test failed',
        elapsed: '0s',
      }
      setTestResult(failResult)
      onTestResult?.(false)
    } finally {
      setTesting(false)
    }
  }, [code, timeout, sampleData, onTestResult, pluginName])

  const copyOutput = useCallback(() => {
    if (testResult?.output) {
      navigator.clipboard.writeText(JSON.stringify(testResult.output, null, 2))
    }
  }, [testResult])

  return (
    <Stack spacing={2}>
      {/* Info */}
      <Alert severity="info" sx={{ '& .MuiAlert-message': { fontSize: '0.8rem' } }}>
        JavaScript (ES6) — JSON, RegExp, Date, Math 등 표준 내장 객체 사용 가능.
        import/require, fetch, setTimeout 불가. hash/base64는 별도 Stage 사용.
      </Alert>
      {/* Code Editor */}
      <Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          JavaScript Code <Chip label="required" size="small" color="error" variant="outlined" sx={{ ml: 1 }} />
        </Typography>

        <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
          <Editor
            height="300px"
            language="javascript"
            theme="vs-dark"
            value={code}
            onChange={handleCodeChange}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              lineNumbers: 'on',
              scrollBeyondLastLine: false,
              wordWrap: 'on',
              tabSize: 2,
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
            <Typography
              variant="caption"
              sx={{
                color: "text.secondary",
                mb: 0.5,
                display: 'block'
              }}>
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
            <Typography
              variant="caption"
              sx={{
                color: "text.secondary",
                mb: 0.5,
                display: 'block'
              }}>
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
                    <Stack
                      direction="row"
                      sx={{
                        alignItems: "center",
                        gap: 1
                      }}>
                      <RemoveCircleIcon color="warning" fontSize="small" />
                      <span>Record dropped (null returned)</span>
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
  );
}
