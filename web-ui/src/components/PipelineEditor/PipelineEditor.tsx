/**
 * Pipeline Editor 메인 컴포넌트
 *
 * GUI/YAML 듀얼 에디터의 통합 컴포넌트
 * - 좌측: Visual Editor (Input, Stages, Outputs)
 * - 우측: YAML Editor
 * - 실시간 양방향 동기화
 */

import { useState } from 'react'
import {
  Box,
  Button,
  Typography,
  Stack,
  CircularProgress,
  Divider,
  IconButton,
  Tooltip,
  useTheme,
  useMediaQuery,
  Tabs,
  Tab,
} from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import SaveIcon from '@mui/icons-material/Save'
import UndoIcon from '@mui/icons-material/Undo'
import VerticalSplitIcon from '@mui/icons-material/VerticalSplit'
import CodeIcon from '@mui/icons-material/Code'
import ViewListIcon from '@mui/icons-material/ViewList'
import { useTranslation } from 'react-i18next'
import { PipelineEditorProvider, usePipelineEditor } from './PipelineEditorContext'
import { InputSection } from './InputSection'
import { StageSection } from './StageSection'
import { OutputSection } from './OutputSection'
import { YamlEditorPane } from './YamlEditorPane'
import type { WorkflowPipeline } from '../../types/pipeline'

// 뷰 모드
type ViewMode = 'split' | 'visual' | 'yaml'

// Props 타입
interface PipelineEditorProps {
  pipeline: WorkflowPipeline
  workflowType: 'batch' | 'realtime'
  onSave: (pipeline: WorkflowPipeline) => Promise<void>
  onCancel: () => void
}

/**
 * 에디터 내부 컴포넌트 (Context 사용)
 */
function PipelineEditorInner() {
  const { t } = useTranslation()
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))

  const {
    pipeline,
    yamlError,
    isDirty,
    syncStatus,
    onSave,
    onCancel,
    resetToInitial,
  } = usePipelineEditor()

  const [saving, setSaving] = useState(false)
  const [viewMode, setViewMode] = useState<ViewMode>(isMobile ? 'visual' : 'split')

  // 저장 핸들러
  const handleSave = async () => {
    try {
      setSaving(true)
      await onSave()
    } catch (e) {
      console.error('Failed to save pipeline:', e)
    } finally {
      setSaving(false)
    }
  }

  // 리셋 핸들러
  const handleReset = () => {
    resetToInitial()
  }

  // 뷰 모드 변경 핸들러 (모바일용 탭)
  const handleViewModeTabChange = (_: React.SyntheticEvent, newValue: ViewMode) => {
    setViewMode(newValue)
  }

  // Visual 에디터 렌더링
  const renderVisualEditor = () => (
    <Box
      sx={{
        flex: 1,
        overflow: 'auto',
        p: 2,
        bgcolor: 'background.default',
      }}
    >
      <Stack spacing={1}>
        <InputSection />
        <StageSection />
        <OutputSection />
      </Stack>
    </Box>
  )

  // YAML 에디터 렌더링
  const renderYamlEditor = () => (
    <Box
      sx={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        bgcolor: 'grey.900',
        overflow: 'hidden',
      }}
    >
      <YamlEditorPane />
    </Box>
  )

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        bgcolor: 'background.paper',
      }}
    >
      {/* 헤더 */}
      <Box
        sx={{
          p: 2,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: 1,
        }}
      >
        <Stack direction="row" spacing={2} sx={{
          alignItems: "center"
        }}>
          <IconButton onClick={onCancel} size="small">
            <ArrowBackIcon />
          </IconButton>
          <Typography variant="h6" noWrap>
            {pipeline.name || t('pipelineEditor.newPipeline')}
          </Typography>
          {isDirty && (
            <Typography variant="caption" sx={{
              color: "warning.main"
            }}>
              ({t('pipelineEditor.unsaved')})
            </Typography>
          )}
        </Stack>

        <Stack direction="row" spacing={1} sx={{
          alignItems: "center"
        }}>
          {/* 뷰 모드 토글 (데스크탑) */}
          {!isMobile && (
            <Stack direction="row" spacing={0.5}>
              <Tooltip title={t('pipelineEditor.viewSplit')}>
                <IconButton
                  size="small"
                  onClick={() => setViewMode('split')}
                  color={viewMode === 'split' ? 'primary' : 'default'}
                >
                  <VerticalSplitIcon />
                </IconButton>
              </Tooltip>
              <Tooltip title={t('pipelineEditor.viewVisual')}>
                <IconButton
                  size="small"
                  onClick={() => setViewMode('visual')}
                  color={viewMode === 'visual' ? 'primary' : 'default'}
                >
                  <ViewListIcon />
                </IconButton>
              </Tooltip>
              <Tooltip title={t('pipelineEditor.viewYaml')}>
                <IconButton
                  size="small"
                  onClick={() => setViewMode('yaml')}
                  color={viewMode === 'yaml' ? 'primary' : 'default'}
                >
                  <CodeIcon />
                </IconButton>
              </Tooltip>
            </Stack>
          )}

          <Divider orientation="vertical" flexItem />

          {/* 리셋 버튼 */}
          <Tooltip title={t('pipelineEditor.reset')}>
            <span>
              <IconButton size="small" onClick={handleReset} disabled={!isDirty}>
                <UndoIcon />
              </IconButton>
            </span>
          </Tooltip>

          {/* 취소 버튼 */}
          <Button onClick={onCancel} color="inherit">
            {t('common.cancel')}
          </Button>

          {/* 저장 버튼 */}
          <Button
            variant="contained"
            startIcon={saving ? <CircularProgress size={16} color="inherit" /> : <SaveIcon />}
            onClick={handleSave}
            disabled={saving || syncStatus === 'error' || !!yamlError}
          >
            {t('common.save')}
          </Button>
        </Stack>
      </Box>
      {/* 모바일 탭 */}
      {isMobile && (
        <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tabs value={viewMode} onChange={handleViewModeTabChange} variant="fullWidth">
            <Tab icon={<ViewListIcon />} label={t('pipelineEditor.visual')} value="visual" />
            <Tab icon={<CodeIcon />} label={t('pipelineEditor.yaml.title')} value="yaml" />
          </Tabs>
        </Box>
      )}
      {/* 에디터 본문 */}
      <Box
        sx={{
          flex: 1,
          display: 'flex',
          overflow: 'hidden',
          minHeight: 0,
        }}
      >
        {viewMode === 'split' && (
          <>
            {/* 좌측: Visual Editor */}
            <Box
              sx={{
                width: '50%',
                borderRight: 1,
                borderColor: 'divider',
                display: 'flex',
                flexDirection: 'column',
                overflow: 'hidden',
              }}
            >
              {renderVisualEditor()}
            </Box>

            {/* 우측: YAML Editor */}
            <Box
              sx={{
                width: '50%',
                display: 'flex',
                flexDirection: 'column',
                overflow: 'hidden',
              }}
            >
              {renderYamlEditor()}
            </Box>
          </>
        )}

        {viewMode === 'visual' && renderVisualEditor()}

        {viewMode === 'yaml' && renderYamlEditor()}
      </Box>
    </Box>
  );
}

/**
 * Pipeline Editor 메인 컴포넌트
 *
 * Provider로 감싸서 Context 제공
 */
export function PipelineEditor({
  pipeline,
  workflowType,
  onSave,
  onCancel,
}: PipelineEditorProps) {
  return (
    <PipelineEditorProvider
      initialPipeline={pipeline}
      workflowType={workflowType}
      onSave={onSave}
      onCancel={onCancel}
    >
      <PipelineEditorInner />
    </PipelineEditorProvider>
  )
}

export default PipelineEditor
