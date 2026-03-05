/**
 * Pipeline GUI/YAML 양방향 동기화 훅
 *
 * GUI 변경 → YAML 반영 (즉시)
 * YAML 변경 → GUI 반영 (debounce 300ms)
 *
 * 동기화 충돌 방지를 위해 syncLock 사용
 */

import { useState, useCallback, useRef, useMemo, useEffect } from 'react'
import type { WorkflowPipeline, WorkflowInput, Stage, Output, PipelineBatchConfig } from '../types/pipeline'
import {
  pipelineToYaml,
  yamlToPipeline,
  validateYamlString,
  type ValidationResult,
} from '../utils/pipelineYamlConverter'

// 동기화 상태
export type SyncStatus = 'synced' | 'syncing' | 'error' | 'unsaved'

// 훅 반환 타입
export interface UsePipelineSyncReturn {
  // 상태
  pipeline: WorkflowPipeline
  yamlContent: string
  yamlError: string | null
  yamlWarnings: string[]
  syncStatus: SyncStatus
  isDirty: boolean

  // GUI 업데이트 (즉시 반영)
  updatePipeline: (updates: Partial<WorkflowPipeline>) => void
  updateInput: (input: Partial<WorkflowInput>) => void
  addStage: (stage: Stage) => void
  updateStage: (id: string, updates: Partial<Stage>) => void
  removeStage: (id: string) => void
  reorderStages: (fromIndex: number, toIndex: number) => void
  addOutput: (output: Output) => void
  updateOutput: (id: string, updates: Partial<Output>) => void
  removeOutput: (id: string) => void
  updateBatch: (batch: Partial<PipelineBatchConfig>) => void

  // YAML 업데이트 (debounce)
  setYamlContent: (yaml: string) => void

  // 유틸리티
  formatYaml: () => void
  resetToInitial: () => void
  markAsSaved: () => void
}

// debounce 유틸리티
function debounce(
  fn: (yaml: string) => void,
  delay: number
): (yaml: string) => void {
  let timeoutId: ReturnType<typeof setTimeout>

  return (yaml: string) => {
    clearTimeout(timeoutId)
    timeoutId = setTimeout(() => fn(yaml), delay)
  }
}

// shallow merge with type safety
function shallowMerge<T>(target: T, updates: Partial<T>): T {
  return { ...target, ...updates }
}

/**
 * Pipeline GUI/YAML 동기화 훅
 *
 * @param initialPipeline - 초기 파이프라인 상태
 * @returns 동기화된 상태와 업데이트 함수들
 */
export function usePipelineSync(initialPipeline: WorkflowPipeline): UsePipelineSyncReturn {
  // 상태
  const [pipeline, setPipeline] = useState<WorkflowPipeline>(initialPipeline)
  const [yamlContent, setYamlContentState] = useState<string>(() =>
    pipelineToYaml(initialPipeline)
  )
  const [yamlError, setYamlError] = useState<string | null>(null)
  const [yamlWarnings, setYamlWarnings] = useState<string[]>([])
  const [syncStatus, setSyncStatus] = useState<SyncStatus>('synced')
  const [isDirty, setIsDirty] = useState(false)

  // 동기화 잠금 (충돌 방지)
  const syncLockRef = useRef(false)
  const initialPipelineRef = useRef(initialPipeline)

  // 초기 파이프라인 변경 시 리셋
  useEffect(() => {
    if (initialPipeline.id !== initialPipelineRef.current.id) {
      initialPipelineRef.current = initialPipeline
      setPipeline(initialPipeline)
      setYamlContentState(pipelineToYaml(initialPipeline))
      setYamlError(null)
      setYamlWarnings([])
      setSyncStatus('synced')
      setIsDirty(false)
    }
  }, [initialPipeline])

  /**
   * GUI → YAML 동기화 (즉시)
   */
  const syncToYaml = useCallback((newPipeline: WorkflowPipeline) => {
    if (syncLockRef.current) return

    try {
      const newYaml = pipelineToYaml(newPipeline)
      setYamlContentState(newYaml)
      setYamlError(null)
      setYamlWarnings([])
      setSyncStatus('unsaved')
      setIsDirty(true)
    } catch (e) {
      console.error('Failed to convert pipeline to YAML:', e)
    }
  }, [])

  /**
   * YAML → GUI 동기화 (내부 함수)
   */
  const syncFromYamlInternal = useCallback(
    (yaml: string) => {
      if (syncLockRef.current) return

      setSyncStatus('syncing')

      // 유효성 검사
      const validation: ValidationResult = validateYamlString(yaml)

      if (!validation.valid) {
        setYamlError(validation.errors.join('\n'))
        setYamlWarnings(validation.warnings)
        setSyncStatus('error')
        return
      }

      try {
        syncLockRef.current = true
        const newPipeline = yamlToPipeline(yaml, pipeline)
        setPipeline(newPipeline)
        setYamlError(null)
        setYamlWarnings(validation.warnings)
        setSyncStatus('unsaved')
        setIsDirty(true)
      } catch (e) {
        setYamlError((e as Error).message)
        setSyncStatus('error')
      } finally {
        syncLockRef.current = false
      }
    },
    [pipeline]
  )

  /**
   * YAML → GUI 동기화 (debounce 300ms)
   */
  const debouncedSyncFromYaml = useMemo(
    () => debounce(syncFromYamlInternal, 300),
    [syncFromYamlInternal]
  )

  /**
   * Pipeline 직접 업데이트
   */
  const updatePipeline = useCallback(
    (updates: Partial<WorkflowPipeline>) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newPipeline = shallowMerge(prev, updates)
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Input 업데이트
   */
  const updateInput = useCallback(
    (inputUpdates: Partial<WorkflowInput>) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const currentInput = prev.input || { type: '', name: '', config: {} }
        const newInput = shallowMerge(currentInput, inputUpdates)
        const newPipeline = { ...prev, input: newInput, source: newInput }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Stage 추가
   */
  const addStage = useCallback(
    (stage: Stage) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newStage = { ...stage, id: stage.id || crypto.randomUUID() }
        const newStages = [...(prev.stages || []), newStage]
        const newPipeline = { ...prev, stages: newStages }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Stage 업데이트
   */
  const updateStage = useCallback(
    (id: string, updates: Partial<Stage>) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newStages = (prev.stages || []).map((s) =>
          s.id === id ? shallowMerge(s, updates) : s
        )
        const newPipeline = { ...prev, stages: newStages }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Stage 삭제
   */
  const removeStage = useCallback(
    (id: string) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newStages = (prev.stages || []).filter((s) => s.id !== id)
        const newPipeline = { ...prev, stages: newStages }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Stage 순서 변경
   */
  const reorderStages = useCallback(
    (fromIndex: number, toIndex: number) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const stages = [...(prev.stages || [])]
        const [removed] = stages.splice(fromIndex, 1)
        stages.splice(toIndex, 0, removed)
        const newPipeline = { ...prev, stages }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Output 추가
   */
  const addOutput = useCallback(
    (output: Output) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newOutput = { ...output, id: output.id || crypto.randomUUID() }
        const newOutputs = [...(prev.outputs || []), newOutput]
        const newPipeline = { ...prev, outputs: newOutputs }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Output 업데이트
   */
  const updateOutput = useCallback(
    (id: string, updates: Partial<Output>) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newOutputs = (prev.outputs || []).map((o) =>
          o.id === id ? shallowMerge(o, updates) : o
        )
        const newPipeline = { ...prev, outputs: newOutputs }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Output 삭제
   */
  const removeOutput = useCallback(
    (id: string) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const newOutputs = (prev.outputs || []).filter((o) => o.id !== id)
        const newPipeline = { ...prev, outputs: newOutputs }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * Batch 설정 업데이트
   */
  const updateBatch = useCallback(
    (batchUpdates: Partial<PipelineBatchConfig>) => {
      if (syncLockRef.current) return

      syncLockRef.current = true
      setPipeline((prev) => {
        const currentBatch = prev.batch || {
          enabled: false,
          output_mode: 'bulk' as const,
          size: 100,
          workers: 20,
          flush_interval: '5s',
        }
        const newBatch = { ...currentBatch, ...batchUpdates }
        const newPipeline = { ...prev, batch: newBatch }
        syncToYaml(newPipeline)
        return newPipeline
      })
      syncLockRef.current = false
    },
    [syncToYaml]
  )

  /**
   * YAML 직접 업데이트
   */
  const setYamlContent = useCallback(
    (yaml: string) => {
      setYamlContentState(yaml)
      debouncedSyncFromYaml(yaml)
    },
    [debouncedSyncFromYaml]
  )

  /**
   * YAML 포맷팅
   */
  const formatYaml = useCallback(() => {
    try {
      const newYaml = pipelineToYaml(pipeline)
      setYamlContentState(newYaml)
    } catch (e) {
      console.error('Failed to format YAML:', e)
    }
  }, [pipeline])

  /**
   * 초기 상태로 리셋
   */
  const resetToInitial = useCallback(() => {
    setPipeline(initialPipelineRef.current)
    setYamlContentState(pipelineToYaml(initialPipelineRef.current))
    setYamlError(null)
    setYamlWarnings([])
    setSyncStatus('synced')
    setIsDirty(false)
  }, [])

  /**
   * 저장 완료 표시
   */
  const markAsSaved = useCallback(() => {
    initialPipelineRef.current = pipeline
    setSyncStatus('synced')
    setIsDirty(false)
  }, [pipeline])

  return {
    // 상태
    pipeline,
    yamlContent,
    yamlError,
    yamlWarnings,
    syncStatus,
    isDirty,

    // GUI 업데이트
    updatePipeline,
    updateInput,
    addStage,
    updateStage,
    removeStage,
    reorderStages,
    addOutput,
    updateOutput,
    removeOutput,
    updateBatch,

    // YAML 업데이트
    setYamlContent,

    // 유틸리티
    formatYaml,
    resetToInitial,
    markAsSaved,
  }
}
