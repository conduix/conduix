/**
 * Pipeline Editor Context
 *
 * GUI/YAML 듀얼 에디터의 상태 관리를 담당합니다.
 * usePipelineSync 훅을 래핑하여 컴포넌트 트리에 공유합니다.
 */

import { createContext, useContext, useCallback, type ReactNode } from 'react'
import { usePipelineSync, type SyncStatus } from '../../hooks/usePipelineSync'
import type {
  WorkflowPipeline,
  WorkflowInput,
  Stage,
  Output,
  PipelineBatchConfig,
} from '../../types/pipeline'

// Context 값 타입
export interface PipelineEditorContextValue {
  // 상태
  pipeline: WorkflowPipeline
  yamlContent: string
  yamlError: string | null
  yamlWarnings: string[]
  syncStatus: SyncStatus
  isDirty: boolean
  workflowType: 'batch' | 'realtime'

  // Input 관련
  updateInput: (input: Partial<WorkflowInput>) => void

  // Stage 관련
  addStage: (stage: Stage) => void
  updateStage: (id: string, updates: Partial<Stage>) => void
  removeStage: (id: string) => void
  reorderStages: (fromIndex: number, toIndex: number) => void

  // Output 관련
  addOutput: (output: Output) => void
  updateOutput: (id: string, updates: Partial<Output>) => void
  removeOutput: (id: string) => void

  // Batch 설정
  updateBatch: (batch: Partial<PipelineBatchConfig>) => void

  // YAML 관련
  setYamlContent: (yaml: string) => void
  formatYaml: () => void

  // 저장/리셋
  onSave: () => Promise<void>
  onCancel: () => void
  resetToInitial: () => void
  markAsSaved: () => void
}

// Context 생성
const PipelineEditorContext = createContext<PipelineEditorContextValue | null>(null)

// Props 타입
interface PipelineEditorProviderProps {
  children: ReactNode
  initialPipeline: WorkflowPipeline
  workflowType: 'batch' | 'realtime'
  onSave: (pipeline: WorkflowPipeline) => Promise<void>
  onCancel: () => void
}

/**
 * Pipeline Editor Provider
 *
 * 파이프라인 에디터의 상태를 관리하고 하위 컴포넌트에 제공합니다.
 */
export function PipelineEditorProvider({
  children,
  initialPipeline,
  workflowType,
  onSave,
  onCancel,
}: PipelineEditorProviderProps) {
  const sync = usePipelineSync(initialPipeline)

  // 저장 핸들러
  const handleSave = useCallback(async () => {
    if (sync.yamlError) {
      throw new Error('YAML has errors')
    }

    await onSave(sync.pipeline)
    sync.markAsSaved()
  }, [sync, onSave])

  // Context 값
  const value: PipelineEditorContextValue = {
    // 상태
    pipeline: sync.pipeline,
    yamlContent: sync.yamlContent,
    yamlError: sync.yamlError,
    yamlWarnings: sync.yamlWarnings,
    syncStatus: sync.syncStatus,
    isDirty: sync.isDirty,
    workflowType,

    // Input 관련
    updateInput: sync.updateInput,

    // Stage 관련
    addStage: sync.addStage,
    updateStage: sync.updateStage,
    removeStage: sync.removeStage,
    reorderStages: sync.reorderStages,

    // Output 관련
    addOutput: sync.addOutput,
    updateOutput: sync.updateOutput,
    removeOutput: sync.removeOutput,

    // Batch 설정
    updateBatch: sync.updateBatch,

    // YAML 관련
    setYamlContent: sync.setYamlContent,
    formatYaml: sync.formatYaml,

    // 저장/리셋
    onSave: handleSave,
    onCancel,
    resetToInitial: sync.resetToInitial,
    markAsSaved: sync.markAsSaved,
  }

  return (
    <PipelineEditorContext.Provider value={value}>
      {children}
    </PipelineEditorContext.Provider>
  )
}

/**
 * Pipeline Editor Context 사용 훅
 *
 * @throws Provider 외부에서 사용 시 에러
 */
export function usePipelineEditor(): PipelineEditorContextValue {
  const context = useContext(PipelineEditorContext)

  if (!context) {
    throw new Error('usePipelineEditor must be used within PipelineEditorProvider')
  }

  return context
}
