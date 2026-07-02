/**
 * YAML Editor Pane 컴포넌트
 *
 * Monaco Editor를 사용한 YAML 에디터 패널
 * - YAML 구문 하이라이팅
 * - 실시간 문법 검증
 * - 포맷팅 기능
 * - 클립보드 복사
 */

import { useState } from 'react'
import {
  Box,
  IconButton,
  Tooltip,
  Alert,
  Typography,
  Stack,
  Chip,
} from '@mui/material'
import FormatAlignLeftIcon from '@mui/icons-material/FormatAlignLeft'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import CheckIcon from '@mui/icons-material/Check'
import ErrorIcon from '@mui/icons-material/Error'
import WarningIcon from '@mui/icons-material/Warning'
import SyncIcon from '@mui/icons-material/Sync'
import Editor from '@monaco-editor/react'
import { useTranslation } from 'react-i18next'
import { usePipelineEditor } from './PipelineEditorContext'

// 동기화 상태별 색상
const syncStatusConfig = {
  synced: { color: 'success.main', label: 'Synced', icon: CheckIcon },
  syncing: { color: 'info.main', label: 'Syncing...', icon: SyncIcon },
  error: { color: 'error.main', label: 'Error', icon: ErrorIcon },
  unsaved: { color: 'warning.main', label: 'Unsaved', icon: WarningIcon },
}

export function YamlEditorPane() {
  const { t } = useTranslation()
  const {
    yamlContent,
    yamlError,
    yamlWarnings,
    syncStatus,
    setYamlContent,
    formatYaml,
  } = usePipelineEditor()

  const [copied, setCopied] = useState(false)

  // 포맷 버튼 핸들러
  const handleFormat = () => {
    formatYaml()
  }

  // 복사 버튼 핸들러
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(yamlContent)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (e) {
      console.error('Failed to copy:', e)
    }
  }

  // YAML 변경 핸들러
  const handleYamlChange = (value: string | undefined) => {
    setYamlContent(value || '')
  }

  const StatusIcon = syncStatusConfig[syncStatus].icon

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* 툴바 */}
      <Box
        sx={{
          p: 1,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <Typography variant="subtitle2" sx={{
          color: "text.secondary"
        }}>
          {t('pipelineEditor.yaml.title')}
        </Typography>

        <Stack direction="row" spacing={0.5} sx={{
          alignItems: "center"
        }}>
          {/* 동기화 상태 */}
          <Chip
            icon={<StatusIcon sx={{ fontSize: 14 }} />}
            label={syncStatusConfig[syncStatus].label}
            size="small"
            sx={{
              bgcolor: `${syncStatusConfig[syncStatus].color}20`,
              color: syncStatusConfig[syncStatus].color,
              '& .MuiChip-icon': {
                color: syncStatusConfig[syncStatus].color,
              },
            }}
          />

          {/* 포맷 버튼 */}
          <Tooltip title={t('pipelineEditor.yaml.format')}>
            <IconButton size="small" onClick={handleFormat}>
              <FormatAlignLeftIcon fontSize="small" />
            </IconButton>
          </Tooltip>

          {/* 복사 버튼 */}
          <Tooltip title={copied ? t('pipelineEditor.yaml.copied') : t('pipelineEditor.yaml.copy')}>
            <IconButton size="small" onClick={handleCopy}>
              {copied ? (
                <CheckIcon fontSize="small" color="success" />
              ) : (
                <ContentCopyIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        </Stack>
      </Box>
      {/* 에러 표시 */}
      {yamlError && (
        <Alert severity="error" sx={{ m: 1, py: 0.5 }} icon={<ErrorIcon fontSize="small" />}>
          <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', fontSize: '0.75rem' }}>
            {yamlError}
          </Typography>
        </Alert>
      )}
      {/* 경고 표시 */}
      {yamlWarnings.length > 0 && !yamlError && (
        <Alert severity="warning" sx={{ m: 1, py: 0.5 }} icon={<WarningIcon fontSize="small" />}>
          <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', fontSize: '0.75rem' }}>
            {yamlWarnings.join('\n')}
          </Typography>
        </Alert>
      )}
      {/* Monaco Editor */}
      <Box sx={{ flex: 1, minHeight: 0 }}>
        <Editor
          height="100%"
          language="yaml"
          theme="vs-dark"
          value={yamlContent}
          onChange={handleYamlChange}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            lineNumbers: 'on',
            tabSize: 2,
            scrollBeyondLastLine: false,
            wordWrap: 'on',
            automaticLayout: true,
            folding: true,
            foldingStrategy: 'indentation',
            renderLineHighlight: 'line',
            cursorBlinking: 'smooth',
            smoothScrolling: true,
            padding: { top: 8, bottom: 8 },
          }}
        />
      </Box>
    </Box>
  );
}
