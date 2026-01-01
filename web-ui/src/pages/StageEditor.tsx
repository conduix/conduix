import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  Button,
  Space,
  message,
  Spin,
  Typography,
  Tabs,
  Breadcrumb,
  Modal,
  Form,
  Input,
  Select,
  Popconfirm,
  Tag,
  Empty,
} from 'antd'
import {
  ArrowLeftOutlined,
  SaveOutlined,
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  HolderOutlined,
  FilterOutlined,
  SwapOutlined,
  CheckCircleOutlined,
  ExportOutlined,
  MinusCircleOutlined,
  MergeCellsOutlined,
  SplitCellsOutlined,
  LockOutlined,
  CopyOutlined,
  FieldNumberOutlined,
  FieldStringOutlined,
  ClockCircleOutlined,
  DashboardOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import Editor from '@monaco-editor/react'
import yaml from 'js-yaml'
import { api } from '../services/api'
import type { Stage, StageType, WorkflowPipeline } from '../types/pipeline'
import './StageEditor.css'

const { Title, Text } = Typography

interface Workflow {
  id: string
  name: string
  slug: string
  project_id: string
  pipelines?: WorkflowPipeline[]
  project?: {
    id: string
    name: string
    alias: string
  }
}

// Stage 타입별 색상 및 아이콘
const stageTypeConfig: Record<StageType, { color: string; icon: React.ReactNode; label: string }> = {
  filter: { color: 'blue', icon: <FilterOutlined />, label: 'Filter' },
  remap: { color: 'green', icon: <SwapOutlined />, label: 'Remap' },
  drop: { color: 'red', icon: <MinusCircleOutlined />, label: 'Drop' },
  merge: { color: 'cyan', icon: <MergeCellsOutlined />, label: 'Merge' },
  split: { color: 'magenta', icon: <SplitCellsOutlined />, label: 'Split' },
  encrypt: { color: 'gold', icon: <LockOutlined />, label: 'Encrypt' },
  dedupe: { color: 'volcano', icon: <CopyOutlined />, label: 'Dedupe' },
  default: { color: 'geekblue', icon: <FieldStringOutlined />, label: 'Default' },
  cast: { color: 'lime', icon: <FieldNumberOutlined />, label: 'Cast' },
  timestamp: { color: 'pink', icon: <ClockCircleOutlined />, label: 'Timestamp' },
  throttle: { color: 'orange', icon: <DashboardOutlined />, label: 'Throttle' },
  validate: { color: 'default', icon: <CheckCircleOutlined />, label: 'Validate' },
  sink: { color: 'purple', icon: <ExportOutlined />, label: 'Sink' },
}

export default function StageEditorPage() {
  const { t } = useTranslation()
  const { projectAlias, workflowId, pipelineId } = useParams<{
    projectAlias: string
    workflowId: string
    pipelineId: string
  }>()
  const navigate = useNavigate()

  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [pipeline, setPipeline] = useState<WorkflowPipeline | null>(null)
  const [stages, setStages] = useState<Stage[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  // Edit mode
  const [editMode, setEditMode] = useState<'visual' | 'yaml'>('visual')
  const [yamlContent, setYamlContent] = useState<string>('')
  const [yamlError, setYamlError] = useState<string | null>(null)

  // Drag & Drop
  const [draggedId, setDraggedId] = useState<string | null>(null)
  const [dropTargetId, setDropTargetId] = useState<string | null>(null)

  // Stage modal
  const [stageModalVisible, setStageModalVisible] = useState(false)
  const [editingStage, setEditingStage] = useState<Stage | null>(null)
  const [stageForm] = Form.useForm()
  const watchedStageType = Form.useWatch('type', stageForm)

  useEffect(() => {
    if (workflowId && pipelineId) {
      fetchData()
    }
  }, [workflowId, pipelineId])

  // 기본 예시 Stage 생성
  const createDefaultStages = (): Stage[] => [
    {
      id: crypto.randomUUID(),
      name: 'filter-example',
      type: 'filter',
      config: { condition: '.status == "active"' },
    },
    {
      id: crypto.randomUUID(),
      name: 'remap-fields',
      type: 'remap',
      config: { mappings: { id: 'record_id', name: 'title' } },
    },
  ]

  const fetchData = async () => {
    try {
      setLoading(true)
      const res = await api.getWorkflow(workflowId!)
      if (res.success && res.data) {
        setWorkflow(res.data)

        // Find pipeline
        const pipelines = res.data.pipelines || []
        const found = pipelines.find((p: WorkflowPipeline) => p.id === pipelineId)
        if (found) {
          setPipeline(found)
          // Stage가 없으면 기본 예시 Stage 사용
          const initialStages = found.stages && found.stages.length > 0
            ? found.stages
            : createDefaultStages()
          setStages(initialStages)
          syncStagesToYaml(initialStages)
        }
      }
    } catch (error) {
      message.error(t('workflow.loadError'))
    } finally {
      setLoading(false)
    }
  }

  // YAML 동기화
  const syncStagesToYaml = useCallback((stageList: Stage[]) => {
    try {
      const yamlStr = yaml.dump(stageList, { indent: 2, lineWidth: -1 })
      setYamlContent(yamlStr)
      setYamlError(null)
    } catch (error) {
      console.error('Failed to convert stages to YAML:', error)
    }
  }, [])

  const syncYamlToStages = useCallback(() => {
    try {
      const parsed = yaml.load(yamlContent) as Stage[] | null
      if (parsed && Array.isArray(parsed)) {
        // ID가 없으면 생성
        const withIds = parsed.map(s => ({
          ...s,
          id: s.id || crypto.randomUUID(),
        }))
        setStages(withIds)
        setYamlError(null)
        return true
      } else if (parsed === null || yamlContent.trim() === '') {
        setStages([])
        setYamlError(null)
        return true
      }
      setYamlError('Invalid YAML format: expected array of stages')
      return false
    } catch (error) {
      setYamlError(`YAML Parse Error: ${(error as Error).message}`)
      return false
    }
  }, [yamlContent])

  // 탭 변경 시 동기화
  const handleTabChange = (key: string) => {
    if (key === 'yaml' && editMode === 'visual') {
      syncStagesToYaml(stages)
    } else if (key === 'visual' && editMode === 'yaml') {
      if (!syncYamlToStages()) {
        message.warning(t('stage.yamlParseError'))
        return
      }
    }
    setEditMode(key as 'visual' | 'yaml')
  }

  // 저장
  const handleSave = async () => {
    if (editMode === 'yaml') {
      if (!syncYamlToStages()) {
        message.error(t('stage.yamlParseError'))
        return
      }
    }

    try {
      setSaving(true)

      // workflow의 pipelines 업데이트
      const updatedPipelines = (workflow?.pipelines || []).map(p =>
        p.id === pipelineId ? { ...p, stages } : p
      )

      const res = await api.updateWorkflow(workflowId!, {
        pipelines: updatedPipelines,
      })

      if (res.success) {
        message.success(t('stage.saveSuccess'))
      } else {
        message.error(res.error || t('stage.saveError'))
      }
    } catch (error) {
      message.error(t('stage.saveError'))
    } finally {
      setSaving(false)
    }
  }

  // Drag & Drop 핸들러
  const handleDragStart = (e: React.DragEvent, id: string) => {
    setDraggedId(id)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }

  const handleDragOver = (e: React.DragEvent, targetId: string) => {
    e.preventDefault()
    if (draggedId && draggedId !== targetId) {
      setDropTargetId(targetId)
    }
  }

  const handleDragLeave = () => {
    setDropTargetId(null)
  }

  const handleDrop = (e: React.DragEvent, targetId: string) => {
    e.preventDefault()
    if (!draggedId || draggedId === targetId) {
      setDraggedId(null)
      setDropTargetId(null)
      return
    }

    const dragIndex = stages.findIndex(s => s.id === draggedId)
    const dropIndex = stages.findIndex(s => s.id === targetId)

    if (dragIndex !== -1 && dropIndex !== -1) {
      const newStages = [...stages]
      const [removed] = newStages.splice(dragIndex, 1)
      newStages.splice(dropIndex, 0, removed)
      setStages(newStages)
    }

    setDraggedId(null)
    setDropTargetId(null)
  }

  const handleDragEnd = () => {
    setDraggedId(null)
    setDropTargetId(null)
  }

  // Stage CRUD
  const handleAddStage = () => {
    setEditingStage(null)
    stageForm.resetFields()
    stageForm.setFieldsValue({
      type: 'filter',
      config: {},
    })
    setStageModalVisible(true)
  }

  const handleEditStage = (stage: Stage) => {
    setEditingStage(stage)
    stageForm.setFieldsValue({
      name: stage.name,
      type: stage.type,
      // filter
      condition: stage.config?.condition || '',
      // remap
      mappings: stage.config?.mappings ? JSON.stringify(stage.config.mappings, null, 2) : '',
      // drop
      drop_fields: (stage.config?.fields as string[])?.join(', ') || '',
      // merge
      merge_source_fields: (stage.config?.source_fields as string[])?.join(', ') || '',
      merge_target_field: stage.config?.target_field || '',
      merge_delimiter: stage.config?.delimiter || ' ',
      merge_template: stage.config?.template || '',
      // split
      split_source_field: stage.config?.source_field || '',
      split_pattern: stage.config?.pattern || '',
      split_target_fields: (stage.config?.target_fields as string[])?.join(', ') || '',
      split_keep_original: stage.config?.keep_original || false,
      // encrypt
      encrypt_fields: (stage.config?.fields as string[])?.join(', ') || '',
      encrypt_method: stage.config?.method || 'sha256',
      encrypt_key_env: stage.config?.key_env || '',
      encrypt_mask_char: stage.config?.mask_char || '*',
      encrypt_mask_keep_first: stage.config?.mask_keep_first || 0,
      encrypt_mask_keep_last: stage.config?.mask_keep_last || 0,
      // dedupe
      dedupe_key_fields: (stage.config?.key_fields as string[])?.join(', ') || '',
      dedupe_strategy: stage.config?.strategy || 'keep_first',
      dedupe_window: stage.config?.window || '',
      dedupe_timestamp_field: stage.config?.timestamp_field || '',
      // default
      default_values: stage.config?.defaults ? JSON.stringify(stage.config.defaults, null, 2) : '',
      default_only_null: stage.config?.only_null ?? true,
      // cast
      cast_mappings: stage.config?.casts ? JSON.stringify(stage.config.casts, null, 2) : '',
      cast_date_format: stage.config?.date_format || '',
      cast_error_action: stage.config?.error_action || 'null',
      // timestamp
      timestamp_action: stage.config?.action || 'add',
      timestamp_target_field: stage.config?.target_field || '',
      timestamp_source_field: stage.config?.source_field || '',
      timestamp_timezone: stage.config?.timezone || 'UTC',
      timestamp_input_format: stage.config?.input_format || '',
      timestamp_output_format: stage.config?.output_format || '',
      // throttle
      throttle_rate: stage.config?.rate || 100,
      throttle_interval: stage.config?.interval || 'second',
      throttle_burst: stage.config?.burst || '',
      throttle_strategy: stage.config?.strategy || 'token_bucket',
      throttle_drop_on_limit: stage.config?.drop_on_limit ?? false,
      // validate
      schema: stage.config?.schema ? JSON.stringify(stage.config.schema, null, 2) : '',
      drop_on_fail: stage.config?.drop_on_fail || false,
      // sink
      sink_type: stage.config?.type || '',
      sink_config: stage.config?.config ? JSON.stringify(stage.config.config, null, 2) : '',
    })
    setStageModalVisible(true)
  }

  const handleDeleteStage = (id: string) => {
    setStages(stages.filter(s => s.id !== id))
  }

  const handleStageSubmit = async () => {
    try {
      const values = await stageForm.validateFields()

      let config: Record<string, unknown> = {}
      switch (values.type) {
        case 'filter':
          config = { condition: values.condition || '' }
          break
        case 'remap':
          try {
            config = { mappings: values.mappings ? JSON.parse(values.mappings) : {} }
          } catch {
            config = { mappings: {} }
          }
          break
        case 'drop':
          config = {
            fields: values.drop_fields
              ? values.drop_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
              : [],
          }
          break
        case 'merge':
          config = {
            source_fields: values.merge_source_fields
              ? values.merge_source_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
              : [],
            target_field: values.merge_target_field || '',
            delimiter: values.merge_delimiter || ' ',
            template: values.merge_template || undefined,
          }
          break
        case 'split':
          config = {
            source_field: values.split_source_field || '',
            pattern: values.split_pattern || '',
            target_fields: values.split_target_fields
              ? values.split_target_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
              : [],
            keep_original: values.split_keep_original || false,
          }
          break
        case 'encrypt':
          config = {
            fields: values.encrypt_fields
              ? values.encrypt_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
              : [],
            method: values.encrypt_method || 'sha256',
            key_env: values.encrypt_key_env || undefined,
            mask_char: values.encrypt_mask_char || '*',
            mask_keep_first: values.encrypt_mask_keep_first || 0,
            mask_keep_last: values.encrypt_mask_keep_last || 0,
          }
          break
        case 'dedupe':
          config = {
            key_fields: values.dedupe_key_fields
              ? values.dedupe_key_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
              : [],
            strategy: values.dedupe_strategy || 'keep_first',
            window: values.dedupe_window ? Number(values.dedupe_window) : undefined,
            timestamp_field: values.dedupe_timestamp_field || undefined,
          }
          break
        case 'default':
          try {
            config = {
              defaults: values.default_values ? JSON.parse(values.default_values) : {},
              only_null: values.default_only_null ?? true,
            }
          } catch {
            config = { defaults: {} }
          }
          break
        case 'cast':
          try {
            config = {
              casts: values.cast_mappings ? JSON.parse(values.cast_mappings) : {},
              date_format: values.cast_date_format || undefined,
              error_action: values.cast_error_action || 'null',
            }
          } catch {
            config = { casts: {} }
          }
          break
        case 'timestamp':
          config = {
            action: values.timestamp_action || 'add',
            target_field: values.timestamp_target_field || '',
            source_field: values.timestamp_source_field || undefined,
            timezone: values.timestamp_timezone || 'UTC',
            input_format: values.timestamp_input_format || undefined,
            output_format: values.timestamp_output_format || undefined,
          }
          break
        case 'throttle':
          config = {
            rate: values.throttle_rate ? Number(values.throttle_rate) : 100,
            interval: values.throttle_interval || 'second',
            burst: values.throttle_burst ? Number(values.throttle_burst) : undefined,
            strategy: values.throttle_strategy || 'token_bucket',
            drop_on_limit: values.throttle_drop_on_limit || false,
          }
          break
        case 'validate':
          try {
            config = {
              schema: values.schema ? JSON.parse(values.schema) : {},
              drop_on_fail: values.drop_on_fail || false,
            }
          } catch {
            config = { schema: {} }
          }
          break
        case 'sink':
          try {
            config = {
              type: values.sink_type || '',
              config: values.sink_config ? JSON.parse(values.sink_config) : {},
            }
          } catch {
            config = { type: values.sink_type || '', config: {} }
          }
          break
      }

      const stageData: Stage = {
        id: editingStage?.id || crypto.randomUUID(),
        name: values.name,
        type: values.type,
        config,
      }

      if (editingStage) {
        setStages(stages.map(s => (s.id === editingStage.id ? stageData : s)))
      } else {
        setStages([...stages, stageData])
      }

      setStageModalVisible(false)
    } catch (error) {
      // Validation error
    }
  }

  // Stage 설명 렌더링
  const renderStageDescription = (stage: Stage) => {
    switch (stage.type) {
      case 'filter':
        return stage.config?.condition ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {String(stage.config.condition)}
          </Text>
        ) : null
      case 'remap': {
        const mappings = stage.config?.mappings as Record<string, string> | undefined
        if (mappings) {
          const entries = Object.entries(mappings).slice(0, 3)
          return (
            <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
              {entries.map(([k, v]) => `${k} → ${v}`).join(', ')}
              {Object.keys(mappings).length > 3 ? '...' : ''}
            </Text>
          )
        }
        return null
      }
      case 'drop': {
        const fields = stage.config?.fields as string[] | undefined
        return fields?.length ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {fields.slice(0, 5).join(', ')}{fields.length > 5 ? '...' : ''}
          </Text>
        ) : null
      }
      case 'merge': {
        const sourceFields = stage.config?.source_fields as string[] | undefined
        const targetField = stage.config?.target_field as string | undefined
        return sourceFields?.length && targetField ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {sourceFields.join(' + ')} → {targetField}
          </Text>
        ) : null
      }
      case 'split': {
        const sourceField = stage.config?.source_field as string | undefined
        const targetFields = stage.config?.target_fields as string[] | undefined
        return sourceField && targetFields?.length ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {sourceField} → {targetFields.join(', ')}
          </Text>
        ) : null
      }
      case 'encrypt': {
        const encFields = stage.config?.fields as string[] | undefined
        const method = stage.config?.method as string | undefined
        return encFields?.length ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {encFields.slice(0, 3).join(', ')}{encFields.length > 3 ? '...' : ''} ({method})
          </Text>
        ) : null
      }
      case 'dedupe': {
        const keyFields = stage.config?.key_fields as string[] | undefined
        const strategy = stage.config?.strategy as string | undefined
        return keyFields?.length ? (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {keyFields.join(', ')} ({strategy})
          </Text>
        ) : null
      }
      case 'default': {
        const defaults = stage.config?.defaults as Record<string, unknown> | undefined
        if (defaults) {
          const keys = Object.keys(defaults).slice(0, 3)
          return (
            <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
              {keys.join(', ')}{Object.keys(defaults).length > 3 ? '...' : ''}
            </Text>
          )
        }
        return null
      }
      case 'cast': {
        const casts = stage.config?.casts as Record<string, string> | undefined
        if (casts) {
          const entries = Object.entries(casts).slice(0, 3)
          return (
            <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
              {entries.map(([k, v]) => `${k}:${v}`).join(', ')}{Object.keys(casts).length > 3 ? '...' : ''}
            </Text>
          )
        }
        return null
      }
      case 'timestamp': {
        const action = stage.config?.action as string | undefined
        const targetField = stage.config?.target_field as string | undefined
        return (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {action} → {targetField}
          </Text>
        )
      }
      case 'throttle': {
        const rate = stage.config?.rate as number | undefined
        const interval = stage.config?.interval as string | undefined
        const strategy = stage.config?.strategy as string | undefined
        return (
          <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
            {rate}/{interval} ({strategy || 'token_bucket'})
          </Text>
        )
      }
      case 'validate':
        return <Text type="secondary">Schema validation</Text>
      case 'sink':
        return (
          <Text type="secondary">
            {String(stage.config?.type || 'unknown')}
          </Text>
        )
      default:
        return null
    }
  }

  // 타입별 폼 필드 렌더링
  const renderTypeSpecificFields = () => {
    switch (watchedStageType) {
      case 'filter':
        return (
          <Form.Item
            name="condition"
            label={t('stage.condition')}
            extra={t('stage.conditionHelp')}
          >
            <Input.TextArea
              rows={3}
              placeholder='.status == "active" && .age >= 18'
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
        )
      case 'remap':
        return (
          <Form.Item
            name="mappings"
            label={t('stage.mappings')}
            extra={t('stage.mappingsHelp')}
          >
            <Input.TextArea
              rows={5}
              placeholder='{"old_field": "new_field", "id": "board_id"}'
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
        )
      case 'drop':
        return (
          <Form.Item
            name="drop_fields"
            label={t('stage.dropFields')}
            extra={t('stage.dropFieldsHelp')}
            rules={[{ required: true, message: t('stage.dropFieldsRequired') }]}
          >
            <Input
              placeholder="password, secret_key, internal_id"
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
        )
      case 'merge':
        return (
          <>
            <Form.Item
              name="merge_source_fields"
              label={t('stage.mergeSourceFields')}
              extra={t('stage.mergeSourceFieldsHelp')}
              rules={[{ required: true, message: t('stage.mergeSourceFieldsRequired') }]}
            >
              <Input
                placeholder="first_name, last_name"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="merge_target_field"
              label={t('stage.mergeTargetField')}
              rules={[{ required: true, message: t('stage.mergeTargetFieldRequired') }]}
            >
              <Input
                placeholder="full_name"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="merge_delimiter"
              label={t('stage.mergeDelimiter')}
              extra={t('stage.mergeDelimiterHelp')}
            >
              <Input placeholder=" " style={{ fontFamily: 'monospace' }} />
            </Form.Item>
            <Form.Item
              name="merge_template"
              label={t('stage.mergeTemplate')}
              extra={t('stage.mergeTemplateHelp')}
            >
              <Input
                placeholder="{{first_name}} {{last_name}}"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
          </>
        )
      case 'split':
        return (
          <>
            <Form.Item
              name="split_source_field"
              label={t('stage.splitSourceField')}
              rules={[{ required: true, message: t('stage.splitSourceFieldRequired') }]}
            >
              <Input
                placeholder="full_name"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="split_pattern"
              label={t('stage.splitPattern')}
              extra={t('stage.splitPatternHelp')}
              rules={[{ required: true, message: t('stage.splitPatternRequired') }]}
            >
              <Input
                placeholder="^(\w+)\s+(\w+)$"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="split_target_fields"
              label={t('stage.splitTargetFields')}
              extra={t('stage.splitTargetFieldsHelp')}
              rules={[{ required: true, message: t('stage.splitTargetFieldsRequired') }]}
            >
              <Input
                placeholder="first_name, last_name"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="split_keep_original"
              label={t('stage.splitKeepOriginal')}
            >
              <Select>
                <Select.Option value={true}>{t('common.yes')}</Select.Option>
                <Select.Option value={false}>{t('common.no')}</Select.Option>
              </Select>
            </Form.Item>
          </>
        )
      case 'encrypt':
        return (
          <>
            <Form.Item
              name="encrypt_fields"
              label={t('stage.encryptFields')}
              extra={t('stage.encryptFieldsHelp')}
              rules={[{ required: true, message: t('stage.encryptFieldsRequired') }]}
            >
              <Input
                placeholder="password, ssn, credit_card"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="encrypt_method"
              label={t('stage.encryptMethod')}
              rules={[{ required: true }]}
            >
              <Select>
                <Select.Option value="sha256">SHA-256 (Hash)</Select.Option>
                <Select.Option value="sha512">SHA-512 (Hash)</Select.Option>
                <Select.Option value="aes256">AES-256 (Encryption)</Select.Option>
                <Select.Option value="bcrypt">bcrypt (Password)</Select.Option>
                <Select.Option value="mask">Mask (****)</Select.Option>
              </Select>
            </Form.Item>
            {watchedStageType === 'encrypt' && stageForm.getFieldValue('encrypt_method') === 'aes256' && (
              <Form.Item
                name="encrypt_key_env"
                label={t('stage.encryptKeyEnv')}
                extra={t('stage.encryptKeyEnvHelp')}
              >
                <Input placeholder="ENCRYPTION_KEY" style={{ fontFamily: 'monospace' }} />
              </Form.Item>
            )}
            {watchedStageType === 'encrypt' && stageForm.getFieldValue('encrypt_method') === 'mask' && (
              <>
                <Form.Item
                  name="encrypt_mask_char"
                  label={t('stage.encryptMaskChar')}
                >
                  <Input placeholder="*" maxLength={1} style={{ width: 60 }} />
                </Form.Item>
                <Form.Item
                  name="encrypt_mask_keep_first"
                  label={t('stage.encryptMaskKeepFirst')}
                >
                  <Input type="number" min={0} placeholder="0" style={{ width: 100 }} />
                </Form.Item>
                <Form.Item
                  name="encrypt_mask_keep_last"
                  label={t('stage.encryptMaskKeepLast')}
                >
                  <Input type="number" min={0} placeholder="0" style={{ width: 100 }} />
                </Form.Item>
              </>
            )}
          </>
        )
      case 'dedupe':
        return (
          <>
            <Form.Item
              name="dedupe_key_fields"
              label={t('stage.dedupeKeyFields')}
              extra={t('stage.dedupeKeyFieldsHelp')}
              rules={[{ required: true, message: t('stage.dedupeKeyFieldsRequired') }]}
            >
              <Input
                placeholder="order_id, product_id"
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="dedupe_strategy"
              label={t('stage.dedupeStrategy')}
              rules={[{ required: true }]}
            >
              <Select>
                <Select.Option value="keep_first">{t('stage.dedupeKeepFirst')}</Select.Option>
                <Select.Option value="keep_last">{t('stage.dedupeKeepLast')}</Select.Option>
                <Select.Option value="keep_latest">{t('stage.dedupeKeepLatest')}</Select.Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="dedupe_window"
              label={t('stage.dedupeWindow')}
              extra={t('stage.dedupeWindowHelp')}
            >
              <Input type="number" min={0} placeholder="300" suffix="sec" style={{ width: 150 }} />
            </Form.Item>
            <Form.Item
              name="dedupe_timestamp_field"
              label={t('stage.dedupeTimestampField')}
              extra={t('stage.dedupeTimestampFieldHelp')}
            >
              <Input placeholder="updated_at" style={{ fontFamily: 'monospace' }} />
            </Form.Item>
          </>
        )
      case 'default':
        return (
          <>
            <Form.Item
              name="default_values"
              label={t('stage.defaultValues')}
              extra={t('stage.defaultValuesHelp')}
              rules={[{ required: true, message: t('stage.defaultValuesRequired') }]}
            >
              <Input.TextArea
                rows={5}
                placeholder='{"status": "pending", "count": 0, "tags": []}'
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="default_only_null"
              label={t('stage.defaultOnlyNull')}
              extra={t('stage.defaultOnlyNullHelp')}
            >
              <Select>
                <Select.Option value={true}>{t('stage.defaultOnlyNullYes')}</Select.Option>
                <Select.Option value={false}>{t('stage.defaultOnlyNullNo')}</Select.Option>
              </Select>
            </Form.Item>
          </>
        )
      case 'cast':
        return (
          <>
            <Form.Item
              name="cast_mappings"
              label={t('stage.castMappings')}
              extra={t('stage.castMappingsHelp')}
              rules={[{ required: true, message: t('stage.castMappingsRequired') }]}
            >
              <Input.TextArea
                rows={5}
                placeholder='{"age": "int", "price": "float", "is_active": "bool", "created_at": "date"}'
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item
              name="cast_date_format"
              label={t('stage.castDateFormat')}
              extra={t('stage.castDateFormatHelp')}
            >
              <Input placeholder="2006-01-02T15:04:05Z07:00" style={{ fontFamily: 'monospace' }} />
            </Form.Item>
            <Form.Item
              name="cast_error_action"
              label={t('stage.castErrorAction')}
            >
              <Select>
                <Select.Option value="null">{t('stage.castErrorNull')}</Select.Option>
                <Select.Option value="drop">{t('stage.castErrorDrop')}</Select.Option>
                <Select.Option value="keep">{t('stage.castErrorKeep')}</Select.Option>
              </Select>
            </Form.Item>
          </>
        )
      case 'timestamp':
        return (
          <>
            <Form.Item
              name="timestamp_action"
              label={t('stage.timestampAction')}
              rules={[{ required: true }]}
            >
              <Select>
                <Select.Option value="add">{t('stage.timestampAdd')}</Select.Option>
                <Select.Option value="convert">{t('stage.timestampConvert')}</Select.Option>
                <Select.Option value="format">{t('stage.timestampFormat')}</Select.Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="timestamp_target_field"
              label={t('stage.timestampTargetField')}
              rules={[{ required: true, message: t('stage.timestampTargetFieldRequired') }]}
            >
              <Input placeholder="processed_at" style={{ fontFamily: 'monospace' }} />
            </Form.Item>
            {watchedStageType === 'timestamp' && stageForm.getFieldValue('timestamp_action') !== 'add' && (
              <Form.Item
                name="timestamp_source_field"
                label={t('stage.timestampSourceField')}
              >
                <Input placeholder="created_at" style={{ fontFamily: 'monospace' }} />
              </Form.Item>
            )}
            <Form.Item
              name="timestamp_timezone"
              label={t('stage.timestampTimezone')}
            >
              <Select showSearch placeholder="UTC">
                <Select.Option value="UTC">UTC</Select.Option>
                <Select.Option value="Asia/Seoul">Asia/Seoul</Select.Option>
                <Select.Option value="Asia/Tokyo">Asia/Tokyo</Select.Option>
                <Select.Option value="America/New_York">America/New_York</Select.Option>
                <Select.Option value="America/Los_Angeles">America/Los_Angeles</Select.Option>
                <Select.Option value="Europe/London">Europe/London</Select.Option>
              </Select>
            </Form.Item>
            {watchedStageType === 'timestamp' && stageForm.getFieldValue('timestamp_action') === 'convert' && (
              <Form.Item
                name="timestamp_input_format"
                label={t('stage.timestampInputFormat')}
                extra={t('stage.timestampInputFormatHelp')}
              >
                <Input placeholder="2006-01-02 15:04:05" style={{ fontFamily: 'monospace' }} />
              </Form.Item>
            )}
            {watchedStageType === 'timestamp' && stageForm.getFieldValue('timestamp_action') === 'format' && (
              <Form.Item
                name="timestamp_output_format"
                label={t('stage.timestampOutputFormat')}
                extra={t('stage.timestampOutputFormatHelp')}
              >
                <Input placeholder="2006-01-02" style={{ fontFamily: 'monospace' }} />
              </Form.Item>
            )}
          </>
        )
      case 'throttle':
        return (
          <>
            <Form.Item
              name="throttle_rate"
              label={t('stage.throttleRate')}
              extra={t('stage.throttleRateHelp')}
              rules={[{ required: true, message: t('stage.throttleRateRequired') }]}
            >
              <Input type="number" min={1} placeholder="100" style={{ width: 150 }} />
            </Form.Item>
            <Form.Item
              name="throttle_interval"
              label={t('stage.throttleInterval')}
              rules={[{ required: true }]}
            >
              <Select style={{ width: 150 }}>
                <Select.Option value="second">{t('stage.throttlePerSecond')}</Select.Option>
                <Select.Option value="minute">{t('stage.throttlePerMinute')}</Select.Option>
                <Select.Option value="hour">{t('stage.throttlePerHour')}</Select.Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="throttle_burst"
              label={t('stage.throttleBurst')}
              extra={t('stage.throttleBurstHelp')}
            >
              <Input type="number" min={0} placeholder="10" style={{ width: 150 }} />
            </Form.Item>
            <Form.Item
              name="throttle_strategy"
              label={t('stage.throttleStrategy')}
              extra={t('stage.throttleStrategyHelp')}
            >
              <Select>
                <Select.Option value="token_bucket">{t('stage.throttleTokenBucket')}</Select.Option>
                <Select.Option value="sliding_window">{t('stage.throttleSlidingWindow')}</Select.Option>
                <Select.Option value="fixed_window">{t('stage.throttleFixedWindow')}</Select.Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="throttle_drop_on_limit"
              label={t('stage.throttleDropOnLimit')}
              extra={t('stage.throttleDropOnLimitHelp')}
            >
              <Select>
                <Select.Option value={false}>{t('stage.throttleWait')}</Select.Option>
                <Select.Option value={true}>{t('stage.throttleDrop')}</Select.Option>
              </Select>
            </Form.Item>
          </>
        )
      case 'validate':
        return (
          <>
            <Form.Item
              name="schema"
              label={t('stage.schema')}
              extra={t('stage.schemaHelp')}
            >
              <Input.TextArea
                rows={5}
                placeholder='{"type": "object", "required": ["id", "name"]}'
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
            <Form.Item name="drop_on_fail" valuePropName="checked">
              <Select placeholder={t('stage.dropOnFail')}>
                <Select.Option value={true}>{t('common.yes')}</Select.Option>
                <Select.Option value={false}>{t('common.no')}</Select.Option>
              </Select>
            </Form.Item>
          </>
        )
      case 'sink':
        return (
          <>
            <Form.Item
              name="sink_type"
              label={t('stage.sinkType')}
              rules={[{ required: true }]}
            >
              <Select placeholder={t('stage.sinkTypePlaceholder')}>
                <Select.Option value="elasticsearch">Elasticsearch</Select.Option>
                <Select.Option value="kafka">Kafka</Select.Option>
                <Select.Option value="mongodb">MongoDB</Select.Option>
                <Select.Option value="s3">S3</Select.Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="sink_config"
              label={t('stage.sinkConfig')}
            >
              <Input.TextArea
                rows={5}
                placeholder='{"index": "my-index", "batch_size": 100}'
                style={{ fontFamily: 'monospace' }}
              />
            </Form.Item>
          </>
        )
      default:
        return null
    }
  }

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 100 }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="stage-editor-page">
      {/* Header */}
      <div className="stage-editor-header">
        <div className="stage-editor-header-left">
          <Breadcrumb
            items={[
              { title: workflow?.project?.name || projectAlias },
              {
                title: workflow?.name,
                onClick: () => navigate(`/workflows/${workflowId}`),
              },
              { title: pipeline?.name },
              { title: t('stage.title') },
            ]}
          />
          <Title level={4} style={{ marginTop: 8, marginBottom: 0 }}>
            {t('stage.title')} - {pipeline?.name}
          </Title>
        </div>
        <Space>
          <Button
            icon={<ArrowLeftOutlined />}
            onClick={() => navigate(`/workflows/${workflowId}`)}
          >
            {t('common.back')}
          </Button>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            loading={saving}
            onClick={handleSave}
          >
            {t('common.save')}
          </Button>
        </Space>
      </div>

      {/* Content */}
      <Card className="stage-editor-content">
        <Tabs
          activeKey={editMode}
          onChange={handleTabChange}
          items={[
            {
              key: 'visual',
              label: t('stage.visual'),
              children: (
                <div className="stage-list">
                  {stages.length === 0 ? (
                    <Empty description={t('stage.noStages')}>
                      <Button type="primary" icon={<PlusOutlined />} onClick={handleAddStage}>
                        {t('stage.add')}
                      </Button>
                    </Empty>
                  ) : (
                    <>
                      {stages.map((stage, index) => (
                        <div
                          key={stage.id}
                          className={`stage-card ${draggedId === stage.id ? 'dragging' : ''} ${dropTargetId === stage.id ? 'drop-target' : ''}`}
                          draggable
                          onDragStart={e => handleDragStart(e, stage.id)}
                          onDragOver={e => handleDragOver(e, stage.id)}
                          onDragLeave={handleDragLeave}
                          onDrop={e => handleDrop(e, stage.id)}
                          onDragEnd={handleDragEnd}
                        >
                          <div className="stage-card-drag-handle">
                            <HolderOutlined />
                          </div>
                          <div className="stage-card-index">{index + 1}</div>
                          <div className="stage-card-content">
                            <div className="stage-card-header">
                              <span className="stage-card-name">{stage.name}</span>
                              <Tag color={stageTypeConfig[stage.type]?.color}>
                                {stageTypeConfig[stage.type]?.icon} {stageTypeConfig[stage.type]?.label}
                              </Tag>
                            </div>
                            <div className="stage-card-description">
                              {renderStageDescription(stage)}
                            </div>
                          </div>
                          <div className="stage-card-actions">
                            <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => handleEditStage(stage)}
                            />
                            <Popconfirm
                              title={t('stage.deleteConfirm')}
                              onConfirm={() => handleDeleteStage(stage.id)}
                              okText={t('common.yes')}
                              cancelText={t('common.no')}
                            >
                              <Button
                                type="text"
                                size="small"
                                danger
                                icon={<DeleteOutlined />}
                              />
                            </Popconfirm>
                          </div>
                        </div>
                      ))}
                      <Button
                        type="dashed"
                        icon={<PlusOutlined />}
                        onClick={handleAddStage}
                        block
                        style={{ marginTop: 16 }}
                      >
                        {t('stage.add')}
                      </Button>
                    </>
                  )}
                </div>
              ),
            },
            {
              key: 'yaml',
              label: t('stage.yaml'),
              children: (
                <div className="yaml-editor-container">
                  {yamlError && (
                    <div className="yaml-error">{yamlError}</div>
                  )}
                  <Editor
                    height="500px"
                    language="yaml"
                    theme="vs-dark"
                    value={yamlContent}
                    onChange={value => setYamlContent(value || '')}
                    options={{
                      minimap: { enabled: false },
                      fontSize: 14,
                      lineNumbers: 'on',
                      tabSize: 2,
                      scrollBeyondLastLine: false,
                      automaticLayout: true,
                    }}
                  />
                </div>
              ),
            },
          ]}
        />
      </Card>

      {/* Stage Modal */}
      <Modal
        title={editingStage ? t('stage.edit') : t('stage.add')}
        open={stageModalVisible}
        onOk={handleStageSubmit}
        onCancel={() => setStageModalVisible(false)}
        width={600}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
      >
        <Form form={stageForm} layout="vertical">
          <Form.Item
            name="name"
            label={t('stage.name')}
            rules={[{ required: true, message: t('stage.nameRequired') }]}
          >
            <Input placeholder="filter-active" />
          </Form.Item>

          <Form.Item
            name="type"
            label={t('stage.type')}
            rules={[{ required: true, message: t('stage.typeRequired') }]}
          >
            <Select placeholder={t('stage.typePlaceholder')}>
              <Select.Option value="filter">
                <Space>
                  <FilterOutlined style={{ color: '#1890ff' }} />
                  {t('stage.types.filter')}
                </Space>
              </Select.Option>
              <Select.Option value="remap">
                <Space>
                  <SwapOutlined style={{ color: '#52c41a' }} />
                  {t('stage.types.remap')}
                </Space>
              </Select.Option>
              <Select.Option value="drop">
                <Space>
                  <MinusCircleOutlined style={{ color: '#ff4d4f' }} />
                  {t('stage.types.drop')}
                </Space>
              </Select.Option>
              <Select.Option value="merge">
                <Space>
                  <MergeCellsOutlined style={{ color: '#13c2c2' }} />
                  {t('stage.types.merge')}
                </Space>
              </Select.Option>
              <Select.Option value="split">
                <Space>
                  <SplitCellsOutlined style={{ color: '#eb2f96' }} />
                  {t('stage.types.split')}
                </Space>
              </Select.Option>
              <Select.Option value="encrypt">
                <Space>
                  <LockOutlined style={{ color: '#faad14' }} />
                  {t('stage.types.encrypt')}
                </Space>
              </Select.Option>
              <Select.Option value="dedupe">
                <Space>
                  <CopyOutlined style={{ color: '#fa541c' }} />
                  {t('stage.types.dedupe')}
                </Space>
              </Select.Option>
              <Select.Option value="default">
                <Space>
                  <FieldStringOutlined style={{ color: '#2f54eb' }} />
                  {t('stage.types.default')}
                </Space>
              </Select.Option>
              <Select.Option value="cast">
                <Space>
                  <FieldNumberOutlined style={{ color: '#a0d911' }} />
                  {t('stage.types.cast')}
                </Space>
              </Select.Option>
              <Select.Option value="timestamp">
                <Space>
                  <ClockCircleOutlined style={{ color: '#eb2f96' }} />
                  {t('stage.types.timestamp')}
                </Space>
              </Select.Option>
              <Select.Option value="throttle">
                <Space>
                  <DashboardOutlined style={{ color: '#fa8c16' }} />
                  {t('stage.types.throttle')}
                </Space>
              </Select.Option>
              <Select.Option value="validate">
                <Space>
                  <CheckCircleOutlined style={{ color: '#fa8c16' }} />
                  {t('stage.types.validate')}
                </Space>
              </Select.Option>
              <Select.Option value="sink">
                <Space>
                  <ExportOutlined style={{ color: '#722ed1' }} />
                  {t('stage.types.sink')}
                </Space>
              </Select.Option>
            </Select>
          </Form.Item>

          {renderTypeSpecificFields()}
        </Form>
      </Modal>
    </div>
  )
}
