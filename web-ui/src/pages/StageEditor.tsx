import { useEffect, useState, useCallback, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  Button,
  Box,
  Stack,
  CircularProgress,
  Typography,
  Tabs,
  Tab,
  Breadcrumbs,
  Link,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Chip,
  FormHelperText,
} from '@mui/material'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import SaveIcon from '@mui/icons-material/Save'
import AddIcon from '@mui/icons-material/Add'
import EditIcon from '@mui/icons-material/Edit'
import DeleteIcon from '@mui/icons-material/Delete'
import DragHandleIcon from '@mui/icons-material/DragHandle'
import FilterAltIcon from '@mui/icons-material/FilterAlt'
import SwapHorizIcon from '@mui/icons-material/SwapHoriz'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import OutputIcon from '@mui/icons-material/Output'
import RemoveCircleIcon from '@mui/icons-material/RemoveCircle'
import MergeIcon from '@mui/icons-material/Merge'
import CallSplitIcon from '@mui/icons-material/CallSplit'
import LockIcon from '@mui/icons-material/Lock'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import NumbersIcon from '@mui/icons-material/Numbers'
import TextFieldsIcon from '@mui/icons-material/TextFields'
import AccessTimeIcon from '@mui/icons-material/AccessTime'
import SpeedIcon from '@mui/icons-material/Speed'
import ForkRightIcon from '@mui/icons-material/ForkRight'
import StorageIcon from '@mui/icons-material/Storage'
import BlockIcon from '@mui/icons-material/Block'
import ApiIcon from '@mui/icons-material/Api'
import GavelIcon from '@mui/icons-material/Gavel'
import { useTranslation } from 'react-i18next'
import Editor from '@monaco-editor/react'
import yaml from 'js-yaml'
import { api } from '../services/api'
import type { Stage, StageType, OutputType, WorkflowPipeline } from '../types/pipeline'

// Stage 또는 Output 타입 (편집기에서 사용)
type StageOrOutputType = StageType | OutputType
import { ContractStageEditor } from '../components/ContractStageEditor'
import type { DataType, DataTypeField } from '../types/data-type'
import { useSnackbar } from '../hooks/useSnackbar'
import { ConfirmDialog } from '../components/common/ConfirmDialog'
import './StageEditor.css'

/**
 * SQL 식별자 인용 (MySQL 백틱)
 */
const quoteSQLIdentifier = (name: string): string => {
  return '`' + name.replace(/`/g, '``') + '`';
}

/**
 * JSON Schema 타입을 SQL 타입으로 변환
 */
const jsonSchemaTypeToSQL = (type: string, format?: string): string => {
  switch (type) {
    case 'string':
      if (format === 'date-time' || format === 'datetime') return 'TIMESTAMP'
      if (format === 'date') return 'DATE'
      if (format === 'time') return 'TIME'
      if (format === 'uuid') return 'UUID'
      return 'VARCHAR(255)'
    case 'integer':
    case 'int':
      return 'INTEGER'
    case 'number':
    case 'float':
      return 'DECIMAL(18,6)'
    case 'boolean':
    case 'bool':
      return 'BOOLEAN'
    case 'object':
    case 'json':
      return 'JSONB'
    case 'array':
      return 'JSONB'
    case 'datetime':
      return 'TIMESTAMP'
    default:
      return 'TEXT'
  }
}

/**
 * DataTypeField 배열에서 CREATE TABLE 생성
 */
const generateCreateTableFromFields = (tableName: string, fields: DataTypeField[], idFields?: string[]): string => {
  const quotedTable = quoteSQLIdentifier(tableName)
  if (!fields || fields.length === 0) {
    return `-- No fields defined in schema\nCREATE TABLE IF NOT EXISTS ${quotedTable} (\n  id SERIAL PRIMARY KEY\n);`
  }

  const columnDefs = fields.map(field => {
    const sqlType = jsonSchemaTypeToSQL(field.type)
    const nullable = field.required ? ' NOT NULL' : ''
    return `  ${quoteSQLIdentifier(field.name)} ${sqlType}${nullable}`
  })

  // Add primary key constraint if id_fields are defined
  let primaryKeyClause = ''
  if (idFields && idFields.length > 0) {
    const quotedIdFields = idFields.map(f => quoteSQLIdentifier(f))
    primaryKeyClause = `,\n  PRIMARY KEY (${quotedIdFields.join(', ')})`
  }

  return `CREATE TABLE IF NOT EXISTS ${quotedTable} (\n${columnDefs.join(',\n')}${primaryKeyClause}\n);`
}

/**
 * JSON Schema에서 CREATE TABLE 생성
 */
const generateCreateTableFromJsonSchema = (tableName: string, schemaStr: string, idFields?: string[]): string => {
  const quotedTable = quoteSQLIdentifier(tableName)
  try {
    const schema = JSON.parse(schemaStr)
    if (!schema.properties) {
      return `-- Invalid JSON Schema: no properties found\nCREATE TABLE IF NOT EXISTS ${quotedTable} (\n  id SERIAL PRIMARY KEY\n);`
    }

    const columnDefs: string[] = []
    const required = schema.required || []

    for (const [propName, propDef] of Object.entries(schema.properties)) {
      const prop = propDef as { type?: string; format?: string }
      const sqlType = jsonSchemaTypeToSQL(prop.type || 'string', prop.format)
      const nullable = required.includes(propName) ? ' NOT NULL' : ''
      columnDefs.push(`  ${quoteSQLIdentifier(propName)} ${sqlType}${nullable}`)
    }

    // Add primary key constraint if id_fields are defined
    let primaryKeyClause = ''
    if (idFields && idFields.length > 0) {
      const quotedIdFields = idFields.map(f => quoteSQLIdentifier(f))
      primaryKeyClause = `,\n  PRIMARY KEY (${quotedIdFields.join(', ')})`
    }

    return `CREATE TABLE IF NOT EXISTS ${quotedTable} (\n${columnDefs.join(',\n')}${primaryKeyClause}\n);`
  } catch {
    return `-- Failed to parse JSON Schema\nCREATE TABLE IF NOT EXISTS ${quotedTable} (\n  id SERIAL PRIMARY KEY\n);`
  }
}

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

// Stage/Output 타입별 색상 및 아이콘
const stageTypeConfig: Record<StageOrOutputType, { color: string; icon: React.ReactNode; label: string }> = {
  // 변환 Stage
  filter: { color: '#1976d2', icon: <FilterAltIcon fontSize="small" />, label: 'Filter' },
  remap: { color: '#4caf50', icon: <SwapHorizIcon fontSize="small" />, label: 'Remap' },
  drop: { color: '#f44336', icon: <RemoveCircleIcon fontSize="small" />, label: 'Drop' },
  merge: { color: '#00bcd4', icon: <MergeIcon fontSize="small" />, label: 'Merge' },
  split: { color: '#e91e63', icon: <CallSplitIcon fontSize="small" />, label: 'Split' },
  encrypt: { color: '#ffc107', icon: <LockIcon fontSize="small" />, label: 'Encrypt' },
  dedupe: { color: '#ff5722', icon: <ContentCopyIcon fontSize="small" />, label: 'Dedupe' },
  default: { color: '#3f51b5', icon: <TextFieldsIcon fontSize="small" />, label: 'Default' },
  cast: { color: '#cddc39', icon: <NumbersIcon fontSize="small" />, label: 'Cast' },
  timestamp: { color: '#e91e63', icon: <AccessTimeIcon fontSize="small" />, label: 'Timestamp' },
  throttle: { color: '#ff9800', icon: <SpeedIcon fontSize="small" />, label: 'Throttle' },
  validate: { color: '#9e9e9e', icon: <CheckCircleIcon fontSize="small" />, label: 'Validate' },
  contract: { color: '#673ab7', icon: <GavelIcon fontSize="small" />, label: 'Contract' },
  route: { color: '#00bcd4', icon: <ForkRightIcon fontSize="small" />, label: 'Route' },
  delete: { color: '#f44336', icon: <BlockIcon fontSize="small" />, label: 'Delete' },
  // Output Stage (출력)
  sql: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'SQL Output' },
  elasticsearch: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'Elasticsearch Output' },
  kafka: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'Kafka Output' },
  mongodb: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'MongoDB Output' },
  s3: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'S3 Output' },
  rest_api: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'REST API Output' },
  file: { color: '#9c27b0', icon: <OutputIcon fontSize="small" />, label: 'File Output' },
}

// Output 타입별 기본 설정값
const getOutputConfigDefault = (outputType: OutputType): Record<string, unknown> => {
  switch (outputType) {
    case 'sql':
      return {
        connection_string: 'postgres://user:password@localhost:5432/dbname?sslmode=disable',
        table: 'my_table',
        batch_size: 100,
        upsert: true,
        conflict_columns: [],
        create_table: '',
      }
    case 'elasticsearch':
      return {
        addresses: ['http://localhost:9200'],
        index: 'my-index',
        batch_size: 100,
      }
    case 'kafka':
      return {
        brokers: ['localhost:9092'],
        topic: 'my-topic',
      }
    case 'mongodb':
      return {
        uri: 'mongodb://localhost:27017',
        database: 'mydb',
        collection: 'mycollection',
      }
    case 's3':
      return {
        bucket: 'my-bucket',
        region: 'us-east-1',
        prefix: 'data/',
      }
    case 'rest_api':
      return {
        url: 'https://api.example.com/data',
        method: 'POST',
        headers: {},
      }
    case 'file':
      return {
        path: '/data/output',
        format: 'json',
      }
    default:
      return {}
  }
}

// Stage form state interface
interface StageFormState {
  name: string
  type: StageOrOutputType | ''
  // Filter
  condition: string
  // Remap
  mappings: string
  // Drop
  drop_fields: string
  // Merge
  merge_source_fields: string
  merge_target_field: string
  merge_delimiter: string
  merge_template: string
  // Split
  split_source_field: string
  split_pattern: string
  split_target_fields: string
  split_keep_original: boolean
  // Encrypt
  encrypt_fields: string
  encrypt_method: string
  encrypt_key_env: string
  encrypt_mask_char: string
  encrypt_mask_keep_first: number
  encrypt_mask_keep_last: number
  // Dedupe
  dedupe_key_fields: string
  dedupe_strategy: string
  dedupe_window: string
  dedupe_timestamp_field: string
  // Default
  default_values: string
  default_only_null: boolean
  // Cast
  cast_mappings: string
  cast_date_format: string
  cast_error_action: string
  // Timestamp
  timestamp_action: string
  timestamp_target_field: string
  timestamp_source_field: string
  timestamp_timezone: string
  timestamp_input_format: string
  timestamp_output_format: string
  // Throttle
  throttle_rate: number | ''
  throttle_interval: string
  throttle_burst: string
  throttle_strategy: string
  throttle_drop_on_limit: boolean
  // Validate
  schema: string
  drop_on_fail: boolean
  // Contract
  contractConfig: Record<string, unknown>
  // SQL Output
  sql_connection_string: string
  sql_table: string
  sql_batch_size: number
  sql_upsert: boolean
  sql_conflict_columns: string
  sql_create_table: string
  // Elasticsearch Output
  es_addresses: string
  es_index: string
  es_batch_size: number
  es_username: string
  es_password: string
  // Kafka Output
  kafka_brokers: string
  kafka_topic: string
  // MongoDB Output
  mongodb_uri: string
  mongodb_database: string
  mongodb_collection: string
  // S3 Output
  s3_bucket: string
  s3_region: string
  s3_prefix: string
  s3_endpoint: string
  // REST API Output
  rest_url: string
  rest_method: string
  rest_headers: string
  // File Output
  file_path: string
  file_format: string
}

const initialStageFormState: StageFormState = {
  name: '',
  type: '',
  condition: '',
  mappings: '',
  drop_fields: '',
  merge_source_fields: '',
  merge_target_field: '',
  merge_delimiter: ' ',
  merge_template: '',
  split_source_field: '',
  split_pattern: '',
  split_target_fields: '',
  split_keep_original: false,
  encrypt_fields: '',
  encrypt_method: 'sha256',
  encrypt_key_env: '',
  encrypt_mask_char: '*',
  encrypt_mask_keep_first: 0,
  encrypt_mask_keep_last: 0,
  dedupe_key_fields: '',
  dedupe_strategy: 'keep_first',
  dedupe_window: '',
  dedupe_timestamp_field: '',
  default_values: '',
  default_only_null: true,
  cast_mappings: '',
  cast_date_format: '',
  cast_error_action: 'null',
  timestamp_action: 'add',
  timestamp_target_field: '',
  timestamp_source_field: '',
  timestamp_timezone: 'UTC',
  timestamp_input_format: '',
  timestamp_output_format: '',
  throttle_rate: 100,
  throttle_interval: 'second',
  throttle_burst: '',
  throttle_strategy: 'token_bucket',
  throttle_drop_on_limit: false,
  schema: '',
  drop_on_fail: false,
  contractConfig: {},
  sql_connection_string: '',
  sql_table: '',
  sql_batch_size: 100,
  sql_upsert: true,
  sql_conflict_columns: '',
  sql_create_table: '',
  es_addresses: '',
  es_index: '',
  es_batch_size: 100,
  es_username: '',
  es_password: '',
  kafka_brokers: '',
  kafka_topic: '',
  mongodb_uri: '',
  mongodb_database: '',
  mongodb_collection: '',
  s3_bucket: '',
  s3_region: '',
  s3_prefix: '',
  s3_endpoint: '',
  rest_url: '',
  rest_method: 'POST',
  rest_headers: '{}',
  file_path: '',
  file_format: 'json',
}

export default function StageEditorPage() {
  const { t } = useTranslation()
  const { projectAlias, workflowId, pipelineId } = useParams<{
    projectAlias: string
    workflowId: string
    pipelineId: string
  }>()
  const navigate = useNavigate()
  const { showSuccess, showError, showWarning } = useSnackbar()

  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [pipeline, setPipeline] = useState<WorkflowPipeline | null>(null)
  const [stages, setStages] = useState<Stage[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testingConnection, setTestingConnection] = useState(false)

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
  const [stageForm, setStageForm] = useState<StageFormState>(initialStageFormState)
  const [stageFormErrors, setStageFormErrors] = useState<Record<string, string>>({})

  // Delete confirmation
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [stageToDelete, setStageToDelete] = useState<string | null>(null)

  // Available fields for Contract stage (from source config or previous stages)
  const availableFields = useMemo(() => {
    const fields: string[] = []
    // Add common fields
    fields.push('id', 'timestamp', 'type', 'source')
    // Add fields from pipeline source if available
    if (pipeline?.source?.config) {
      const sourceConfig = pipeline.source.config
      // Add fields from source schema or known patterns
      if (sourceConfig.fields && Array.isArray(sourceConfig.fields)) {
        fields.push(...(sourceConfig.fields as string[]))
      }
    }
    // Remove duplicates
    return [...new Set(fields)]
  }, [pipeline])

  // Update stage form field
  const updateStageFormField = useCallback(<K extends keyof StageFormState>(field: K, value: StageFormState[K]) => {
    setStageForm(prev => ({ ...prev, [field]: value }))
    if (stageFormErrors[field]) {
      setStageFormErrors(prev => {
        const newErrors = { ...prev }
        delete newErrors[field]
        return newErrors
      })
    }
  }, [stageFormErrors])

  // 기본 예시 Stage 생성
  const createDefaultStages = useCallback((): Stage[] => [
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
  ], [])

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

  const fetchData = useCallback(async () => {
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
    } catch {
      showError(t('workflow.loadError'))
    } finally {
      setLoading(false)
    }
  }, [workflowId, pipelineId, createDefaultStages, syncStagesToYaml, showError, t])

  useEffect(() => {
    if (workflowId && pipelineId) {
      fetchData()
    }
  }, [workflowId, pipelineId, fetchData])

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
  const handleTabChange = (_event: React.SyntheticEvent, newValue: string) => {
    if (newValue === 'yaml' && editMode === 'visual') {
      syncStagesToYaml(stages)
    } else if (newValue === 'visual' && editMode === 'yaml') {
      if (!syncYamlToStages()) {
        showWarning(t('stage.yamlParseError'))
        return
      }
    }
    setEditMode(newValue as 'visual' | 'yaml')
  }

  // 저장
  const handleSave = async () => {
    if (editMode === 'yaml') {
      if (!syncYamlToStages()) {
        showError(t('stage.yamlParseError'))
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
        showSuccess(t('stage.saveSuccess'))
      } else {
        showError(res.error || t('stage.saveError'))
      }
    } catch {
      showError(t('stage.saveError'))
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
    setStageForm({
      ...initialStageFormState,
      type: 'filter',
    })
    setStageFormErrors({})
    setStageModalVisible(true)
  }

  // Output 타입인지 확인
  const isOutputType = (t: StageOrOutputType): t is OutputType => {
    return ['sql', 'elasticsearch', 'kafka', 'mongodb', 's3', 'rest_api', 'file'].includes(t)
  }

  // 타입 변경 시 기본값 설정
  const handleStageTypeChange = (type: StageOrOutputType) => {
    updateStageFormField('type', type)

    // Output 타입인 경우 기본값 설정
    const defaultConfig = isOutputType(type) ? getOutputConfigDefault(type) : {}

    switch (type) {
      case 'sql':
        setStageForm(prev => ({
          ...prev,
          type,
          sql_connection_string: defaultConfig.connection_string as string,
          sql_table: defaultConfig.table as string,
          sql_batch_size: defaultConfig.batch_size as number,
          sql_upsert: defaultConfig.upsert as boolean,
          sql_conflict_columns: (defaultConfig.conflict_columns as string[])?.join(', ') || '',
          sql_create_table: defaultConfig.create_table as string || '',
        }))
        break
      case 'elasticsearch':
        setStageForm(prev => ({
          ...prev,
          type,
          es_addresses: (defaultConfig.addresses as string[])?.join(', ') || '',
          es_index: defaultConfig.index as string || '',
          es_batch_size: defaultConfig.batch_size as number || 100,
          es_username: '',
          es_password: '',
        }))
        break
      case 'kafka':
        setStageForm(prev => ({
          ...prev,
          type,
          kafka_brokers: (defaultConfig.brokers as string[])?.join(', ') || '',
          kafka_topic: defaultConfig.topic as string || '',
        }))
        break
      case 'mongodb':
        setStageForm(prev => ({
          ...prev,
          type,
          mongodb_uri: defaultConfig.uri as string || '',
          mongodb_database: defaultConfig.database as string || '',
          mongodb_collection: defaultConfig.collection as string || '',
        }))
        break
      case 's3':
        setStageForm(prev => ({
          ...prev,
          type,
          s3_bucket: defaultConfig.bucket as string || '',
          s3_region: defaultConfig.region as string || '',
          s3_prefix: defaultConfig.prefix as string || '',
          s3_endpoint: '',
        }))
        break
      case 'rest_api':
        setStageForm(prev => ({
          ...prev,
          type,
          rest_url: defaultConfig.url as string || '',
          rest_method: defaultConfig.method as string || 'POST',
          rest_headers: defaultConfig.headers ? JSON.stringify(defaultConfig.headers, null, 2) : '{}',
        }))
        break
      case 'file':
        setStageForm(prev => ({
          ...prev,
          type,
          file_path: defaultConfig.path as string || '',
          file_format: defaultConfig.format as string || 'json',
        }))
        break
      default:
        updateStageFormField('type', type)
    }
  }

  const handleEditStage = (stage: Stage) => {
    setEditingStage(stage)
    setStageFormErrors({})

    const newForm: StageFormState = {
      ...initialStageFormState,
      name: stage.name,
      type: stage.type,
      // filter
      condition: stage.config?.condition as string || '',
      // remap
      mappings: stage.config?.mappings ? JSON.stringify(stage.config.mappings, null, 2) : '',
      // drop
      drop_fields: (stage.config?.fields as string[])?.join(', ') || '',
      // merge
      merge_source_fields: (stage.config?.source_fields as string[])?.join(', ') || '',
      merge_target_field: stage.config?.target_field as string || '',
      merge_delimiter: stage.config?.delimiter as string || ' ',
      merge_template: stage.config?.template as string || '',
      // split
      split_source_field: stage.config?.source_field as string || '',
      split_pattern: stage.config?.pattern as string || '',
      split_target_fields: (stage.config?.target_fields as string[])?.join(', ') || '',
      split_keep_original: stage.config?.keep_original as boolean || false,
      // encrypt
      encrypt_fields: (stage.config?.fields as string[])?.join(', ') || '',
      encrypt_method: stage.config?.method as string || 'sha256',
      encrypt_key_env: stage.config?.key_env as string || '',
      encrypt_mask_char: stage.config?.mask_char as string || '*',
      encrypt_mask_keep_first: stage.config?.mask_keep_first as number || 0,
      encrypt_mask_keep_last: stage.config?.mask_keep_last as number || 0,
      // dedupe
      dedupe_key_fields: (stage.config?.key_fields as string[])?.join(', ') || '',
      dedupe_strategy: stage.config?.strategy as string || 'keep_first',
      dedupe_window: stage.config?.window?.toString() || '',
      dedupe_timestamp_field: stage.config?.timestamp_field as string || '',
      // default
      default_values: stage.config?.defaults ? JSON.stringify(stage.config.defaults, null, 2) : '',
      default_only_null: (stage.config?.only_null as boolean) ?? true,
      // cast
      cast_mappings: stage.config?.casts ? JSON.stringify(stage.config.casts, null, 2) : '',
      cast_date_format: stage.config?.date_format as string || '',
      cast_error_action: stage.config?.error_action as string || 'null',
      // timestamp
      timestamp_action: stage.config?.action as string || 'add',
      timestamp_target_field: stage.config?.target_field as string || '',
      timestamp_source_field: stage.config?.source_field as string || '',
      timestamp_timezone: stage.config?.timezone as string || 'UTC',
      timestamp_input_format: stage.config?.input_format as string || '',
      timestamp_output_format: stage.config?.output_format as string || '',
      // throttle
      throttle_rate: stage.config?.rate as number || 100,
      throttle_interval: stage.config?.interval as string || 'second',
      throttle_burst: stage.config?.burst?.toString() || '',
      throttle_strategy: stage.config?.strategy as string || 'token_bucket',
      throttle_drop_on_limit: (stage.config?.drop_on_limit as boolean) ?? false,
      // validate
      schema: stage.config?.schema ? JSON.stringify(stage.config.schema, null, 2) : '',
      drop_on_fail: stage.config?.drop_on_fail as boolean || false,
      // contract
      contractConfig: stage.type === 'contract' ? (stage.config || {}) : {},
      // SQL output
      sql_connection_string: stage.config?.connection_string as string || '',
      sql_table: stage.config?.table as string || '',
      sql_batch_size: stage.config?.batch_size as number || 100,
      sql_upsert: (stage.config?.upsert as boolean) ?? true,
      sql_conflict_columns: (stage.config?.conflict_columns as string[])?.join(', ') || '',
      sql_create_table: stage.config?.create_table as string || '',
      // Elasticsearch output
      es_addresses: (stage.config?.addresses as string[])?.join(', ') || '',
      es_index: stage.config?.index as string || '',
      es_batch_size: stage.config?.batch_size as number || 100,
      es_username: stage.config?.username as string || '',
      es_password: stage.config?.password as string || '',
      // Kafka output
      kafka_brokers: (stage.config?.brokers as string[])?.join(', ') || '',
      kafka_topic: stage.config?.topic as string || '',
      // MongoDB output
      mongodb_uri: stage.config?.uri as string || '',
      mongodb_database: stage.config?.database as string || '',
      mongodb_collection: stage.config?.collection as string || '',
      // S3 output
      s3_bucket: stage.config?.bucket as string || '',
      s3_region: stage.config?.region as string || '',
      s3_prefix: stage.config?.prefix as string || '',
      s3_endpoint: stage.config?.endpoint as string || '',
      // REST API output
      rest_url: stage.config?.url as string || '',
      rest_method: stage.config?.method as string || 'POST',
      rest_headers: stage.config?.headers ? JSON.stringify(stage.config.headers, null, 2) : '{}',
      // File output
      file_path: stage.config?.path as string || '',
      file_format: stage.config?.format as string || 'json',
    }

    setStageForm(newForm)
    setStageModalVisible(true)
  }

  const handleDeleteStage = (id: string) => {
    setStageToDelete(id)
    setDeleteDialogOpen(true)
  }

  const confirmDeleteStage = () => {
    if (stageToDelete) {
      setStages(stages.filter(s => s.id !== stageToDelete))
    }
    setDeleteDialogOpen(false)
    setStageToDelete(null)
  }

  // Test DB Connection
  const handleTestDBConnection = async () => {
    if (!stageForm.sql_connection_string) {
      showWarning(t('stage.testConnectionNoString'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testDBConnection(stageForm.sql_connection_string)
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency})`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Test Elasticsearch Connection
  const handleTestElasticsearch = async () => {
    if (!stageForm.es_addresses) {
      showWarning(t('stage.testConnectionNoAddress'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testElasticsearch({
        addresses: stageForm.es_addresses.split(',').map((s: string) => s.trim()).filter(Boolean),
        username: stageForm.es_username,
        password: stageForm.es_password,
      })
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency}) - ${result.message}`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Test Kafka Connection
  const handleTestKafka = async () => {
    if (!stageForm.kafka_brokers) {
      showWarning(t('stage.testConnectionNoBroker'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testKafka({
        brokers: stageForm.kafka_brokers.split(',').map((s: string) => s.trim()).filter(Boolean),
        topic: stageForm.kafka_topic,
      })
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency}) - ${result.message}`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Test MongoDB Connection
  const handleTestMongoDB = async () => {
    if (!stageForm.mongodb_uri) {
      showWarning(t('stage.testConnectionNoUri'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testMongoDB({
        uri: stageForm.mongodb_uri,
        database: stageForm.mongodb_database,
        collection: stageForm.mongodb_collection,
      })
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency}) - ${result.message}`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Test S3 Connection
  const handleTestS3 = async () => {
    if (!stageForm.s3_bucket || !stageForm.s3_region) {
      showWarning(t('stage.testConnectionNoS3'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testS3({
        bucket: stageForm.s3_bucket,
        region: stageForm.s3_region,
        endpoint: stageForm.s3_endpoint,
      })
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency}) - ${result.message}`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Test REST API Connection
  const handleTestRESTAPI = async () => {
    if (!stageForm.rest_url) {
      showWarning(t('stage.testConnectionNoUrl'))
      return
    }

    setTestingConnection(true)
    try {
      const result = await api.testRESTAPI({
        url: stageForm.rest_url,
        method: stageForm.rest_method || 'GET',
      })
      if (result.success) {
        showSuccess(`${t('stage.testConnectionSuccess')} (${result.latency}) - ${result.message}`)
      } else {
        showError(`${t('stage.testConnectionFailed')}: ${result.error}`)
      }
    } catch {
      showError(t('stage.testConnectionError'))
    } finally {
      setTestingConnection(false)
    }
  }

  // Generate CREATE TABLE from target data model schema
  const handleGenerateCreateTableForFields = async () => {
    if (!pipeline?.target_data_type_id) {
      showWarning(t('stage.noTargetDataModel'))
      return
    }

    try {
      const res = await api.getDataType(pipeline.target_data_type_id)
      if (res.success && res.data) {
        const dataType = res.data as DataType
        const tableName = dataType.name || 'my_table'
        let createTableSQL = ''

        if (dataType.schema?.fields && dataType.schema.fields.length > 0) {
          createTableSQL = generateCreateTableFromFields(tableName, dataType.schema.fields, dataType.id_fields)
        } else if (dataType.schema?.definition) {
          createTableSQL = generateCreateTableFromJsonSchema(tableName, dataType.schema.definition, dataType.id_fields)
        } else {
          showWarning(t('stage.noSchemaDefinition'))
          return
        }

        // Update individual fields
        setStageForm(prev => ({
          ...prev,
          sql_table: tableName,
          sql_create_table: createTableSQL,
          sql_conflict_columns: dataType.id_fields?.join(', ') || '',
        }))
        showSuccess(t('stage.createTableGenerated'))
      }
    } catch (error) {
      console.error('Failed to fetch data type:', error)
      showError(t('stage.createTableError'))
    }
  }

  const validateStageForm = (): boolean => {
    const errors: Record<string, string> = {}

    if (!stageForm.name) {
      errors.name = t('stage.nameRequired')
    }

    if (!stageForm.type) {
      errors.type = t('stage.typeRequired')
    }

    // Type-specific validation
    switch (stageForm.type) {
      case 'drop':
        if (!stageForm.drop_fields) {
          errors.drop_fields = t('stage.dropFieldsRequired')
        }
        break
      case 'merge':
        if (!stageForm.merge_source_fields) {
          errors.merge_source_fields = t('stage.mergeSourceFieldsRequired')
        }
        if (!stageForm.merge_target_field) {
          errors.merge_target_field = t('stage.mergeTargetFieldRequired')
        }
        break
      case 'split':
        if (!stageForm.split_source_field) {
          errors.split_source_field = t('stage.splitSourceFieldRequired')
        }
        if (!stageForm.split_pattern) {
          errors.split_pattern = t('stage.splitPatternRequired')
        }
        if (!stageForm.split_target_fields) {
          errors.split_target_fields = t('stage.splitTargetFieldsRequired')
        }
        break
      case 'encrypt':
        if (!stageForm.encrypt_fields) {
          errors.encrypt_fields = t('stage.encryptFieldsRequired')
        }
        break
      case 'dedupe':
        if (!stageForm.dedupe_key_fields) {
          errors.dedupe_key_fields = t('stage.dedupeKeyFieldsRequired')
        }
        break
      case 'default':
        if (!stageForm.default_values) {
          errors.default_values = t('stage.defaultValuesRequired')
        }
        break
      case 'cast':
        if (!stageForm.cast_mappings) {
          errors.cast_mappings = t('stage.castMappingsRequired')
        }
        break
      case 'timestamp':
        if (!stageForm.timestamp_target_field) {
          errors.timestamp_target_field = t('stage.timestampTargetFieldRequired')
        }
        break
      case 'throttle':
        if (!stageForm.throttle_rate) {
          errors.throttle_rate = t('stage.throttleRateRequired')
        }
        break
      case 'sql':
        if (!stageForm.sql_connection_string) {
          errors.sql_connection_string = t('stage.sqlConnectionStringRequired')
        }
        if (!stageForm.sql_table) {
          errors.sql_table = t('stage.sqlTableRequired')
        }
        break
      case 'elasticsearch':
        if (!stageForm.es_addresses) {
          errors.es_addresses = t('stage.esAddressesRequired')
        }
        if (!stageForm.es_index) {
          errors.es_index = t('stage.esIndexRequired')
        }
        break
      case 'kafka':
        if (!stageForm.kafka_brokers) {
          errors.kafka_brokers = t('stage.kafkaBrokersRequired')
        }
        if (!stageForm.kafka_topic) {
          errors.kafka_topic = t('stage.kafkaTopicRequired')
        }
        break
      case 'mongodb':
        if (!stageForm.mongodb_uri) {
          errors.mongodb_uri = t('stage.mongodbUriRequired')
        }
        if (!stageForm.mongodb_database) {
          errors.mongodb_database = t('stage.mongodbDatabaseRequired')
        }
        if (!stageForm.mongodb_collection) {
          errors.mongodb_collection = t('stage.mongodbCollectionRequired')
        }
        break
      case 's3':
        if (!stageForm.s3_bucket) {
          errors.s3_bucket = t('stage.s3BucketRequired')
        }
        if (!stageForm.s3_region) {
          errors.s3_region = t('stage.s3RegionRequired')
        }
        break
      case 'rest_api':
        if (!stageForm.rest_url) {
          errors.rest_url = t('stage.restUrlRequired')
        }
        break
      case 'file':
        if (!stageForm.file_path) {
          errors.file_path = t('stage.filePathRequired')
        }
        break
    }

    setStageFormErrors(errors)
    return Object.keys(errors).length === 0
  }

  const handleStageSubmit = () => {
    if (!validateStageForm()) {
      return
    }

    let config: Record<string, unknown> = {}
    switch (stageForm.type) {
      case 'filter':
        config = { condition: stageForm.condition || '' }
        break
      case 'remap':
        try {
          config = { mappings: stageForm.mappings ? JSON.parse(stageForm.mappings) : {} }
        } catch {
          config = { mappings: {} }
        }
        break
      case 'drop':
        config = {
          fields: stageForm.drop_fields
            ? stageForm.drop_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
        }
        break
      case 'merge':
        config = {
          source_fields: stageForm.merge_source_fields
            ? stageForm.merge_source_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          target_field: stageForm.merge_target_field || '',
          delimiter: stageForm.merge_delimiter || ' ',
          template: stageForm.merge_template || undefined,
        }
        break
      case 'split':
        config = {
          source_field: stageForm.split_source_field || '',
          pattern: stageForm.split_pattern || '',
          target_fields: stageForm.split_target_fields
            ? stageForm.split_target_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          keep_original: stageForm.split_keep_original || false,
        }
        break
      case 'encrypt':
        config = {
          fields: stageForm.encrypt_fields
            ? stageForm.encrypt_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          method: stageForm.encrypt_method || 'sha256',
          key_env: stageForm.encrypt_key_env || undefined,
          mask_char: stageForm.encrypt_mask_char || '*',
          mask_keep_first: stageForm.encrypt_mask_keep_first || 0,
          mask_keep_last: stageForm.encrypt_mask_keep_last || 0,
        }
        break
      case 'dedupe':
        config = {
          key_fields: stageForm.dedupe_key_fields
            ? stageForm.dedupe_key_fields.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          strategy: stageForm.dedupe_strategy || 'keep_first',
          window: stageForm.dedupe_window ? Number(stageForm.dedupe_window) : undefined,
          timestamp_field: stageForm.dedupe_timestamp_field || undefined,
        }
        break
      case 'default':
        try {
          config = {
            defaults: stageForm.default_values ? JSON.parse(stageForm.default_values) : {},
            only_null: stageForm.default_only_null ?? true,
          }
        } catch {
          config = { defaults: {} }
        }
        break
      case 'cast':
        try {
          config = {
            casts: stageForm.cast_mappings ? JSON.parse(stageForm.cast_mappings) : {},
            date_format: stageForm.cast_date_format || undefined,
            error_action: stageForm.cast_error_action || 'null',
          }
        } catch {
          config = { casts: {} }
        }
        break
      case 'timestamp':
        config = {
          action: stageForm.timestamp_action || 'add',
          target_field: stageForm.timestamp_target_field || '',
          source_field: stageForm.timestamp_source_field || undefined,
          timezone: stageForm.timestamp_timezone || 'UTC',
          input_format: stageForm.timestamp_input_format || undefined,
          output_format: stageForm.timestamp_output_format || undefined,
        }
        break
      case 'throttle':
        config = {
          rate: stageForm.throttle_rate ? Number(stageForm.throttle_rate) : 100,
          interval: stageForm.throttle_interval || 'second',
          burst: stageForm.throttle_burst ? Number(stageForm.throttle_burst) : undefined,
          strategy: stageForm.throttle_strategy || 'token_bucket',
          drop_on_limit: stageForm.throttle_drop_on_limit || false,
        }
        break
      case 'validate':
        try {
          config = {
            schema: stageForm.schema ? JSON.parse(stageForm.schema) : {},
            drop_on_fail: stageForm.drop_on_fail || false,
          }
        } catch {
          config = { schema: {} }
        }
        break
      case 'contract':
        // Contract stage config는 contractConfig state에서 직접 사용
        config = stageForm.contractConfig || {
          contract: { name: '', version: '1.0.0', rules: [] },
          action: 'drop',
        }
        break
      // SQL Output stage
      case 'sql':
        config = {
          connection_string: stageForm.sql_connection_string || '',
          table: stageForm.sql_table || '',
          batch_size: stageForm.sql_batch_size ? Number(stageForm.sql_batch_size) : 100,
          upsert: stageForm.sql_upsert ?? true,
          conflict_columns: stageForm.sql_conflict_columns
            ? stageForm.sql_conflict_columns.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          create_table: stageForm.sql_create_table || '',
        }
        break
      // Elasticsearch Output stage
      case 'elasticsearch':
        config = {
          addresses: stageForm.es_addresses
            ? stageForm.es_addresses.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          index: stageForm.es_index || '',
          batch_size: stageForm.es_batch_size ? Number(stageForm.es_batch_size) : 100,
          username: stageForm.es_username || undefined,
          password: stageForm.es_password || undefined,
        }
        break
      // Kafka Output stage
      case 'kafka':
        config = {
          brokers: stageForm.kafka_brokers
            ? stageForm.kafka_brokers.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
          topic: stageForm.kafka_topic || '',
        }
        break
      // MongoDB Output stage
      case 'mongodb':
        config = {
          uri: stageForm.mongodb_uri || '',
          database: stageForm.mongodb_database || '',
          collection: stageForm.mongodb_collection || '',
        }
        break
      // S3 Output stage
      case 's3':
        config = {
          bucket: stageForm.s3_bucket || '',
          region: stageForm.s3_region || '',
          prefix: stageForm.s3_prefix || '',
          endpoint: stageForm.s3_endpoint || undefined,
        }
        break
      // REST API Output stage
      case 'rest_api':
        try {
          config = {
            url: stageForm.rest_url || '',
            method: stageForm.rest_method || 'POST',
            headers: stageForm.rest_headers ? JSON.parse(stageForm.rest_headers) : {},
          }
        } catch {
          config = {
            url: stageForm.rest_url || '',
            method: stageForm.rest_method || 'POST',
            headers: {},
          }
        }
        break
      // File Output stage
      case 'file':
        config = {
          path: stageForm.file_path || '',
          format: stageForm.file_format || 'json',
        }
        break
    }

    const stageData: Stage = {
      id: editingStage?.id || crypto.randomUUID(),
      name: stageForm.name,
      type: stageForm.type as StageType,
      config,
    }

    if (editingStage) {
      setStages(stages.map(s => (s.id === editingStage.id ? stageData : s)))
    } else {
      setStages([...stages, stageData])
    }

    setStageModalVisible(false)
  }

  // Stage 설명 렌더링
  const renderStageDescription = (stage: Stage) => {
    switch (stage.type) {
      case 'filter':
        return stage.config?.condition ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {String(stage.config.condition)}
          </Typography>
        ) : null;
      case 'remap': {
        const mappings = stage.config?.mappings as Record<string, string> | undefined
        if (mappings) {
          const entries = Object.entries(mappings).slice(0, 3)
          return (
            <Typography
              variant="body2"
              noWrap
              sx={{
                color: "text.secondary",
                maxWidth: 300
              }}>
              {entries.map(([k, v]) => `${k} -> ${v}`).join(', ')}
              {Object.keys(mappings).length > 3 ? '...' : ''}
            </Typography>
          );
        }
        return null
      }
      case 'drop': {
        const fields = stage.config?.fields as string[] | undefined
        return fields?.length ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {fields.slice(0, 5).join(', ')}{fields.length > 5 ? '...' : ''}
          </Typography>
        ) : null;
      }
      case 'merge': {
        const sourceFields = stage.config?.source_fields as string[] | undefined
        const targetField = stage.config?.target_field as string | undefined
        return sourceFields?.length && targetField ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {sourceFields.join(' + ')} → {targetField}
          </Typography>
        ) : null;
      }
      case 'split': {
        const sourceField = stage.config?.source_field as string | undefined
        const targetFields = stage.config?.target_fields as string[] | undefined
        return sourceField && targetFields?.length ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {sourceField} → {targetFields.join(', ')}
          </Typography>
        ) : null;
      }
      case 'encrypt': {
        const encFields = stage.config?.fields as string[] | undefined
        const method = stage.config?.method as string | undefined
        return encFields?.length ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {encFields.slice(0, 3).join(', ')}{encFields.length > 3 ? '...' : ''} ({method})
          </Typography>
        ) : null;
      }
      case 'dedupe': {
        const keyFields = stage.config?.key_fields as string[] | undefined
        const strategy = stage.config?.strategy as string | undefined
        return keyFields?.length ? (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {keyFields.join(', ')} ({strategy})
          </Typography>
        ) : null;
      }
      case 'default': {
        const defaults = stage.config?.defaults as Record<string, unknown> | undefined
        if (defaults) {
          const keys = Object.keys(defaults).slice(0, 3)
          return (
            <Typography
              variant="body2"
              noWrap
              sx={{
                color: "text.secondary",
                maxWidth: 300
              }}>
              {keys.join(', ')}{Object.keys(defaults).length > 3 ? '...' : ''}
            </Typography>
          );
        }
        return null
      }
      case 'cast': {
        const casts = stage.config?.casts as Record<string, string> | undefined
        if (casts) {
          const entries = Object.entries(casts).slice(0, 3)
          return (
            <Typography
              variant="body2"
              noWrap
              sx={{
                color: "text.secondary",
                maxWidth: 300
              }}>
              {entries.map(([k, v]) => `${k}:${v}`).join(', ')}{Object.keys(casts).length > 3 ? '...' : ''}
            </Typography>
          );
        }
        return null
      }
      case 'timestamp': {
        const action = stage.config?.action as string | undefined
        const targetField = stage.config?.target_field as string | undefined
        return (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {action}→ {targetField}
          </Typography>
        );
      }
      case 'throttle': {
        const rate = stage.config?.rate as number | undefined
        const interval = stage.config?.interval as string | undefined
        const strategy = stage.config?.strategy as string | undefined
        return (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {rate}/{interval}({strategy || 'token_bucket'})
                      </Typography>
        );
      }
      case 'validate':
        return (
          <Typography variant="body2" sx={{
            color: "text.secondary"
          }}>Schema validation</Typography>
        );
      case 'contract': {
        const contractName = (stage.config?.contract as { name?: string })?.name
        const action = stage.config?.action as string
        const rulesCount = ((stage.config?.contract as { rules?: unknown[] })?.rules || []).length
        return (
          <Typography
            variant="body2"
            noWrap
            sx={{
              color: "text.secondary",
              maxWidth: 300
            }}>
            {contractName || 'Contract'}({rulesCount}rules, {action || 'drop'})
                      </Typography>
        );
      }
      // Output stages
      case 'sql':
      case 'elasticsearch':
      case 'kafka':
      case 'mongodb':
      case 's3':
      case 'rest_api':
      case 'file':
        return (
          <Typography variant="body2" sx={{
            color: "text.secondary"
          }}>
            {stage.type}output
                      </Typography>
        );
      default:
        return null
    }
  }

  // 타입별 폼 필드 렌더링
  const renderTypeSpecificFields = () => {
    switch (stageForm.type) {
      case 'filter':
        return (
          <TextField
            label={t('stage.condition')}
            value={stageForm.condition}
            onChange={(e) => updateStageFormField('condition', e.target.value)}
            helperText={t('stage.conditionHelp')}
            placeholder='.status == "active" && .age >= 18'
            multiline
            rows={3}
            fullWidth
            sx={{ fontFamily: 'monospace' }}
          />
        )
      case 'remap':
        return (
          <TextField
            label={t('stage.mappings')}
            value={stageForm.mappings}
            onChange={(e) => updateStageFormField('mappings', e.target.value)}
            helperText={t('stage.mappingsHelp')}
            placeholder='{"old_field": "new_field", "id": "board_id"}'
            multiline
            rows={5}
            fullWidth
            sx={{ fontFamily: 'monospace' }}
          />
        )
      case 'drop':
        return (
          <TextField
            label={t('stage.dropFields')}
            value={stageForm.drop_fields}
            onChange={(e) => updateStageFormField('drop_fields', e.target.value)}
            error={!!stageFormErrors.drop_fields}
            helperText={stageFormErrors.drop_fields || t('stage.dropFieldsHelp')}
            placeholder="password, secret_key, internal_id"
            required
            fullWidth
            sx={{ fontFamily: 'monospace' }}
          />
        )
      case 'merge':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.mergeSourceFields')}
              value={stageForm.merge_source_fields}
              onChange={(e) => updateStageFormField('merge_source_fields', e.target.value)}
              error={!!stageFormErrors.merge_source_fields}
              helperText={stageFormErrors.merge_source_fields || t('stage.mergeSourceFieldsHelp')}
              placeholder="first_name, last_name"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.mergeTargetField')}
              value={stageForm.merge_target_field}
              onChange={(e) => updateStageFormField('merge_target_field', e.target.value)}
              error={!!stageFormErrors.merge_target_field}
              helperText={stageFormErrors.merge_target_field}
              placeholder="full_name"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.mergeDelimiter')}
              value={stageForm.merge_delimiter}
              onChange={(e) => updateStageFormField('merge_delimiter', e.target.value)}
              helperText={t('stage.mergeDelimiterHelp')}
              placeholder=" "
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.mergeTemplate')}
              value={stageForm.merge_template}
              onChange={(e) => updateStageFormField('merge_template', e.target.value)}
              helperText={t('stage.mergeTemplateHelp')}
              placeholder="{{first_name}} {{last_name}}"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      case 'split':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.splitSourceField')}
              value={stageForm.split_source_field}
              onChange={(e) => updateStageFormField('split_source_field', e.target.value)}
              error={!!stageFormErrors.split_source_field}
              helperText={stageFormErrors.split_source_field}
              placeholder="full_name"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.splitPattern')}
              value={stageForm.split_pattern}
              onChange={(e) => updateStageFormField('split_pattern', e.target.value)}
              error={!!stageFormErrors.split_pattern}
              helperText={stageFormErrors.split_pattern || t('stage.splitPatternHelp')}
              placeholder="^(\w+)\s+(\w+)$"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.splitTargetFields')}
              value={stageForm.split_target_fields}
              onChange={(e) => updateStageFormField('split_target_fields', e.target.value)}
              error={!!stageFormErrors.split_target_fields}
              helperText={stageFormErrors.split_target_fields || t('stage.splitTargetFieldsHelp')}
              placeholder="first_name, last_name"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth>
              <InputLabel>{t('stage.splitKeepOriginal')}</InputLabel>
              <Select
                value={stageForm.split_keep_original}
                onChange={(e) => updateStageFormField('split_keep_original', e.target.value as boolean)}
                label={t('stage.splitKeepOriginal')}
              >
                <MenuItem value={true as unknown as string}>{t('common.yes')}</MenuItem>
                <MenuItem value={false as unknown as string}>{t('common.no')}</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )
      case 'encrypt':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.encryptFields')}
              value={stageForm.encrypt_fields}
              onChange={(e) => updateStageFormField('encrypt_fields', e.target.value)}
              error={!!stageFormErrors.encrypt_fields}
              helperText={stageFormErrors.encrypt_fields || t('stage.encryptFieldsHelp')}
              placeholder="password, ssn, credit_card"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth required>
              <InputLabel>{t('stage.encryptMethod')}</InputLabel>
              <Select
                value={stageForm.encrypt_method}
                onChange={(e) => updateStageFormField('encrypt_method', e.target.value)}
                label={t('stage.encryptMethod')}
              >
                <MenuItem value="sha256">SHA-256 (Hash)</MenuItem>
                <MenuItem value="sha512">SHA-512 (Hash)</MenuItem>
                <MenuItem value="aes256">AES-256 (Encryption)</MenuItem>
                <MenuItem value="bcrypt">bcrypt (Password)</MenuItem>
                <MenuItem value="mask">Mask (****)</MenuItem>
              </Select>
            </FormControl>
            {stageForm.encrypt_method === 'aes256' && (
              <TextField
                label={t('stage.encryptKeyEnv')}
                value={stageForm.encrypt_key_env}
                onChange={(e) => updateStageFormField('encrypt_key_env', e.target.value)}
                helperText={t('stage.encryptKeyEnvHelp')}
                placeholder="ENCRYPTION_KEY"
                fullWidth
                sx={{ fontFamily: 'monospace' }}
              />
            )}
            {stageForm.encrypt_method === 'mask' && (
              <Stack direction="row" spacing={2}>
                <TextField
                  label={t('stage.encryptMaskChar')}
                  value={stageForm.encrypt_mask_char}
                  onChange={(e) => updateStageFormField('encrypt_mask_char', e.target.value)}
                  placeholder="*"
                  slotProps={{ htmlInput: { maxLength: 1 } }}
                  sx={{ width: 60 }}
                />
                <TextField
                  label={t('stage.encryptMaskKeepFirst')}
                  type="number"
                  value={stageForm.encrypt_mask_keep_first}
                  onChange={(e) => updateStageFormField('encrypt_mask_keep_first', Number(e.target.value))}
                  placeholder="0"
                  slotProps={{ htmlInput: { min: 0 } }}
                  sx={{ width: 100 }}
                />
                <TextField
                  label={t('stage.encryptMaskKeepLast')}
                  type="number"
                  value={stageForm.encrypt_mask_keep_last}
                  onChange={(e) => updateStageFormField('encrypt_mask_keep_last', Number(e.target.value))}
                  placeholder="0"
                  slotProps={{ htmlInput: { min: 0 } }}
                  sx={{ width: 100 }}
                />
              </Stack>
            )}
          </Stack>
        )
      case 'dedupe':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.dedupeKeyFields')}
              value={stageForm.dedupe_key_fields}
              onChange={(e) => updateStageFormField('dedupe_key_fields', e.target.value)}
              error={!!stageFormErrors.dedupe_key_fields}
              helperText={stageFormErrors.dedupe_key_fields || t('stage.dedupeKeyFieldsHelp')}
              placeholder="order_id, product_id"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth required>
              <InputLabel>{t('stage.dedupeStrategy')}</InputLabel>
              <Select
                value={stageForm.dedupe_strategy}
                onChange={(e) => updateStageFormField('dedupe_strategy', e.target.value)}
                label={t('stage.dedupeStrategy')}
              >
                <MenuItem value="keep_first">{t('stage.dedupeKeepFirst')}</MenuItem>
                <MenuItem value="keep_last">{t('stage.dedupeKeepLast')}</MenuItem>
                <MenuItem value="keep_latest">{t('stage.dedupeKeepLatest')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('stage.dedupeWindow')}
              type="number"
              value={stageForm.dedupe_window}
              onChange={(e) => updateStageFormField('dedupe_window', e.target.value)}
              helperText={t('stage.dedupeWindowHelp')}
              placeholder="300"
              slotProps={{ htmlInput: { min: 0 } }}
              sx={{ width: 150 }}
            />
            <TextField
              label={t('stage.dedupeTimestampField')}
              value={stageForm.dedupe_timestamp_field}
              onChange={(e) => updateStageFormField('dedupe_timestamp_field', e.target.value)}
              helperText={t('stage.dedupeTimestampFieldHelp')}
              placeholder="updated_at"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      case 'default':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.defaultValues')}
              value={stageForm.default_values}
              onChange={(e) => updateStageFormField('default_values', e.target.value)}
              error={!!stageFormErrors.default_values}
              helperText={stageFormErrors.default_values || t('stage.defaultValuesHelp')}
              placeholder='{"status": "pending", "count": 0, "tags": []}'
              multiline
              rows={5}
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth>
              <InputLabel>{t('stage.defaultOnlyNull')}</InputLabel>
              <Select
                value={stageForm.default_only_null}
                onChange={(e) => updateStageFormField('default_only_null', e.target.value as boolean)}
                label={t('stage.defaultOnlyNull')}
              >
                <MenuItem value={true as unknown as string}>{t('stage.defaultOnlyNullYes')}</MenuItem>
                <MenuItem value={false as unknown as string}>{t('stage.defaultOnlyNullNo')}</MenuItem>
              </Select>
              <FormHelperText>{t('stage.defaultOnlyNullHelp')}</FormHelperText>
            </FormControl>
          </Stack>
        )
      case 'cast':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.castMappings')}
              value={stageForm.cast_mappings}
              onChange={(e) => updateStageFormField('cast_mappings', e.target.value)}
              error={!!stageFormErrors.cast_mappings}
              helperText={stageFormErrors.cast_mappings || t('stage.castMappingsHelp')}
              placeholder='{"age": "int", "price": "float", "is_active": "bool", "created_at": "date"}'
              multiline
              rows={5}
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.castDateFormat')}
              value={stageForm.cast_date_format}
              onChange={(e) => updateStageFormField('cast_date_format', e.target.value)}
              helperText={t('stage.castDateFormatHelp')}
              placeholder="2006-01-02T15:04:05Z07:00"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth>
              <InputLabel>{t('stage.castErrorAction')}</InputLabel>
              <Select
                value={stageForm.cast_error_action}
                onChange={(e) => updateStageFormField('cast_error_action', e.target.value)}
                label={t('stage.castErrorAction')}
              >
                <MenuItem value="null">{t('stage.castErrorNull')}</MenuItem>
                <MenuItem value="drop">{t('stage.castErrorDrop')}</MenuItem>
                <MenuItem value="keep">{t('stage.castErrorKeep')}</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )
      case 'timestamp':
        return (
          <Stack spacing={2}>
            <FormControl fullWidth required>
              <InputLabel>{t('stage.timestampAction')}</InputLabel>
              <Select
                value={stageForm.timestamp_action}
                onChange={(e) => updateStageFormField('timestamp_action', e.target.value)}
                label={t('stage.timestampAction')}
              >
                <MenuItem value="add">{t('stage.timestampAdd')}</MenuItem>
                <MenuItem value="convert">{t('stage.timestampConvert')}</MenuItem>
                <MenuItem value="format">{t('stage.timestampFormat')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('stage.timestampTargetField')}
              value={stageForm.timestamp_target_field}
              onChange={(e) => updateStageFormField('timestamp_target_field', e.target.value)}
              error={!!stageFormErrors.timestamp_target_field}
              helperText={stageFormErrors.timestamp_target_field}
              placeholder="processed_at"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            {stageForm.timestamp_action !== 'add' && (
              <TextField
                label={t('stage.timestampSourceField')}
                value={stageForm.timestamp_source_field}
                onChange={(e) => updateStageFormField('timestamp_source_field', e.target.value)}
                placeholder="created_at"
                fullWidth
                sx={{ fontFamily: 'monospace' }}
              />
            )}
            <FormControl fullWidth>
              <InputLabel>{t('stage.timestampTimezone')}</InputLabel>
              <Select
                value={stageForm.timestamp_timezone}
                onChange={(e) => updateStageFormField('timestamp_timezone', e.target.value)}
                label={t('stage.timestampTimezone')}
              >
                <MenuItem value="UTC">UTC</MenuItem>
                <MenuItem value="Asia/Seoul">Asia/Seoul</MenuItem>
                <MenuItem value="Asia/Tokyo">Asia/Tokyo</MenuItem>
                <MenuItem value="America/New_York">America/New_York</MenuItem>
                <MenuItem value="America/Los_Angeles">America/Los_Angeles</MenuItem>
                <MenuItem value="Europe/London">Europe/London</MenuItem>
              </Select>
            </FormControl>
            {stageForm.timestamp_action === 'convert' && (
              <TextField
                label={t('stage.timestampInputFormat')}
                value={stageForm.timestamp_input_format}
                onChange={(e) => updateStageFormField('timestamp_input_format', e.target.value)}
                helperText={t('stage.timestampInputFormatHelp')}
                placeholder="2006-01-02 15:04:05"
                fullWidth
                sx={{ fontFamily: 'monospace' }}
              />
            )}
            {stageForm.timestamp_action === 'format' && (
              <TextField
                label={t('stage.timestampOutputFormat')}
                value={stageForm.timestamp_output_format}
                onChange={(e) => updateStageFormField('timestamp_output_format', e.target.value)}
                helperText={t('stage.timestampOutputFormatHelp')}
                placeholder="2006-01-02"
                fullWidth
                sx={{ fontFamily: 'monospace' }}
              />
            )}
          </Stack>
        )
      case 'throttle':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.throttleRate')}
              type="number"
              value={stageForm.throttle_rate}
              onChange={(e) => updateStageFormField('throttle_rate', e.target.value ? Number(e.target.value) : '')}
              error={!!stageFormErrors.throttle_rate}
              helperText={stageFormErrors.throttle_rate || t('stage.throttleRateHelp')}
              placeholder="100"
              slotProps={{ htmlInput: { min: 1 } }}
              required
              sx={{ width: 150 }}
            />
            <FormControl sx={{ width: 150 }} required>
              <InputLabel>{t('stage.throttleInterval')}</InputLabel>
              <Select
                value={stageForm.throttle_interval}
                onChange={(e) => updateStageFormField('throttle_interval', e.target.value)}
                label={t('stage.throttleInterval')}
              >
                <MenuItem value="second">{t('stage.throttlePerSecond')}</MenuItem>
                <MenuItem value="minute">{t('stage.throttlePerMinute')}</MenuItem>
                <MenuItem value="hour">{t('stage.throttlePerHour')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              label={t('stage.throttleBurst')}
              type="number"
              value={stageForm.throttle_burst}
              onChange={(e) => updateStageFormField('throttle_burst', e.target.value)}
              helperText={t('stage.throttleBurstHelp')}
              placeholder="10"
              slotProps={{ htmlInput: { min: 0 } }}
              sx={{ width: 150 }}
            />
            <FormControl fullWidth>
              <InputLabel>{t('stage.throttleStrategy')}</InputLabel>
              <Select
                value={stageForm.throttle_strategy}
                onChange={(e) => updateStageFormField('throttle_strategy', e.target.value)}
                label={t('stage.throttleStrategy')}
              >
                <MenuItem value="token_bucket">{t('stage.throttleTokenBucket')}</MenuItem>
                <MenuItem value="sliding_window">{t('stage.throttleSlidingWindow')}</MenuItem>
                <MenuItem value="fixed_window">{t('stage.throttleFixedWindow')}</MenuItem>
              </Select>
              <FormHelperText>{t('stage.throttleStrategyHelp')}</FormHelperText>
            </FormControl>
            <FormControl fullWidth>
              <InputLabel>{t('stage.throttleDropOnLimit')}</InputLabel>
              <Select
                value={stageForm.throttle_drop_on_limit}
                onChange={(e) => updateStageFormField('throttle_drop_on_limit', e.target.value as boolean)}
                label={t('stage.throttleDropOnLimit')}
              >
                <MenuItem value={false as unknown as string}>{t('stage.throttleWait')}</MenuItem>
                <MenuItem value={true as unknown as string}>{t('stage.throttleDrop')}</MenuItem>
              </Select>
              <FormHelperText>{t('stage.throttleDropOnLimitHelp')}</FormHelperText>
            </FormControl>
          </Stack>
        )
      case 'validate':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.schema')}
              value={stageForm.schema}
              onChange={(e) => updateStageFormField('schema', e.target.value)}
              helperText={t('stage.schemaHelp')}
              placeholder='{"type": "object", "required": ["id", "name"]}'
              multiline
              rows={5}
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth>
              <InputLabel>{t('stage.dropOnFail')}</InputLabel>
              <Select
                value={stageForm.drop_on_fail}
                onChange={(e) => updateStageFormField('drop_on_fail', e.target.value as boolean)}
                label={t('stage.dropOnFail')}
              >
                <MenuItem value={true as unknown as string}>{t('common.yes')}</MenuItem>
                <MenuItem value={false as unknown as string}>{t('common.no')}</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )
      case 'contract':
        return (
          <ContractStageEditor
            value={stageForm.contractConfig || {}}
            onChange={(contractConfig) => updateStageFormField('contractConfig', contractConfig)}
            availableFields={availableFields}
          />
        )
      // SQL Output stage
      case 'sql':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.sqlConnectionString')}
              value={stageForm.sql_connection_string}
              onChange={(e) => updateStageFormField('sql_connection_string', e.target.value)}
              error={!!stageFormErrors.sql_connection_string}
              helperText={stageFormErrors.sql_connection_string || t('stage.sqlConnectionStringHelp')}
              placeholder="postgres://user:password@localhost:5432/dbname?sslmode=disable"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestDBConnection}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.sqlTable')}
              value={stageForm.sql_table}
              onChange={(e) => updateStageFormField('sql_table', e.target.value)}
              error={!!stageFormErrors.sql_table}
              helperText={stageFormErrors.sql_table}
              placeholder="my_table"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.sqlBatchSize')}
              type="number"
              value={stageForm.sql_batch_size}
              onChange={(e) => updateStageFormField('sql_batch_size', Number(e.target.value))}
              helperText={t('stage.sqlBatchSizeHelp')}
              slotProps={{ htmlInput: { min: 1, max: 10000 } }}
              sx={{ width: 150 }}
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('stage.sqlUpsert')}</InputLabel>
              <Select
                value={stageForm.sql_upsert}
                onChange={(e) => updateStageFormField('sql_upsert', e.target.value as boolean)}
                label={t('stage.sqlUpsert')}
              >
                <MenuItem value={true as unknown as string}>{t('common.yes')}</MenuItem>
                <MenuItem value={false as unknown as string}>{t('common.no')}</MenuItem>
              </Select>
              <FormHelperText>{t('stage.sqlUpsertHelp')}</FormHelperText>
            </FormControl>
            <TextField
              label={t('stage.sqlConflictColumns')}
              value={stageForm.sql_conflict_columns}
              onChange={(e) => updateStageFormField('sql_conflict_columns', e.target.value)}
              helperText={t('stage.sqlConflictColumnsHelp')}
              placeholder="id, external_id"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.sqlCreateTable')}
              value={stageForm.sql_create_table}
              onChange={(e) => updateStageFormField('sql_create_table', e.target.value)}
              helperText={t('stage.sqlCreateTableHelp')}
              placeholder="CREATE TABLE IF NOT EXISTS my_table (...);"
              multiline
              rows={6}
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            {pipeline?.target_data_type_id && (
              <Button
                variant="outlined"
                startIcon={<StorageIcon />}
                onClick={handleGenerateCreateTableForFields}
              >
                {t('stage.generateCreateTable')}
              </Button>
            )}
          </Stack>
        )
      // Elasticsearch Output stage
      case 'elasticsearch':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.esAddresses')}
              value={stageForm.es_addresses}
              onChange={(e) => updateStageFormField('es_addresses', e.target.value)}
              error={!!stageFormErrors.es_addresses}
              helperText={stageFormErrors.es_addresses || t('stage.esAddressesHelp')}
              placeholder="http://localhost:9200, http://localhost:9201"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestElasticsearch}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.esIndex')}
              value={stageForm.es_index}
              onChange={(e) => updateStageFormField('es_index', e.target.value)}
              error={!!stageFormErrors.es_index}
              helperText={stageFormErrors.es_index}
              placeholder="my-index"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.esBatchSize')}
              type="number"
              value={stageForm.es_batch_size}
              onChange={(e) => updateStageFormField('es_batch_size', Number(e.target.value))}
              slotProps={{ htmlInput: { min: 1, max: 10000 } }}
              sx={{ width: 150 }}
            />
            <TextField
              label={t('stage.esUsername')}
              value={stageForm.es_username}
              onChange={(e) => updateStageFormField('es_username', e.target.value)}
              placeholder="elastic"
              fullWidth
            />
            <TextField
              label={t('stage.esPassword')}
              type="password"
              value={stageForm.es_password}
              onChange={(e) => updateStageFormField('es_password', e.target.value)}
              placeholder="password"
              fullWidth
            />
          </Stack>
        )
      // Kafka Output stage
      case 'kafka':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.kafkaBrokers')}
              value={stageForm.kafka_brokers}
              onChange={(e) => updateStageFormField('kafka_brokers', e.target.value)}
              error={!!stageFormErrors.kafka_brokers}
              helperText={stageFormErrors.kafka_brokers || t('stage.kafkaBrokersHelp')}
              placeholder="localhost:9092, localhost:9093"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestKafka}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.kafkaTopic')}
              value={stageForm.kafka_topic}
              onChange={(e) => updateStageFormField('kafka_topic', e.target.value)}
              error={!!stageFormErrors.kafka_topic}
              helperText={stageFormErrors.kafka_topic}
              placeholder="my-topic"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      // MongoDB Output stage
      case 'mongodb':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.mongodbUri')}
              value={stageForm.mongodb_uri}
              onChange={(e) => updateStageFormField('mongodb_uri', e.target.value)}
              error={!!stageFormErrors.mongodb_uri}
              helperText={stageFormErrors.mongodb_uri || t('stage.mongodbUriHelp')}
              placeholder="mongodb://localhost:27017"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestMongoDB}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.mongodbDatabase')}
              value={stageForm.mongodb_database}
              onChange={(e) => updateStageFormField('mongodb_database', e.target.value)}
              error={!!stageFormErrors.mongodb_database}
              helperText={stageFormErrors.mongodb_database}
              placeholder="mydb"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.mongodbCollection')}
              value={stageForm.mongodb_collection}
              onChange={(e) => updateStageFormField('mongodb_collection', e.target.value)}
              error={!!stageFormErrors.mongodb_collection}
              helperText={stageFormErrors.mongodb_collection}
              placeholder="mycollection"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      // S3 Output stage
      case 's3':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.s3Bucket')}
              value={stageForm.s3_bucket}
              onChange={(e) => updateStageFormField('s3_bucket', e.target.value)}
              error={!!stageFormErrors.s3_bucket}
              helperText={stageFormErrors.s3_bucket}
              placeholder="my-bucket"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl fullWidth required error={!!stageFormErrors.s3_region}>
              <InputLabel>{t('stage.s3Region')}</InputLabel>
              <Select
                value={stageForm.s3_region}
                onChange={(e) => updateStageFormField('s3_region', e.target.value)}
                label={t('stage.s3Region')}
              >
                <MenuItem value="us-east-1">us-east-1</MenuItem>
                <MenuItem value="us-west-2">us-west-2</MenuItem>
                <MenuItem value="eu-west-1">eu-west-1</MenuItem>
                <MenuItem value="ap-northeast-1">ap-northeast-1</MenuItem>
                <MenuItem value="ap-northeast-2">ap-northeast-2</MenuItem>
                <MenuItem value="ap-southeast-1">ap-southeast-1</MenuItem>
              </Select>
              {stageFormErrors.s3_region && <FormHelperText>{stageFormErrors.s3_region}</FormHelperText>}
            </FormControl>
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestS3}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.s3Prefix')}
              value={stageForm.s3_prefix}
              onChange={(e) => updateStageFormField('s3_prefix', e.target.value)}
              helperText={t('stage.s3PrefixHelp')}
              placeholder="data/"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <TextField
              label={t('stage.s3Endpoint')}
              value={stageForm.s3_endpoint}
              onChange={(e) => updateStageFormField('s3_endpoint', e.target.value)}
              helperText={t('stage.s3EndpointHelp')}
              placeholder="https://s3.custom.endpoint.com"
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      // REST API Output stage
      case 'rest_api':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.restUrl')}
              value={stageForm.rest_url}
              onChange={(e) => updateStageFormField('rest_url', e.target.value)}
              error={!!stageFormErrors.rest_url}
              helperText={stageFormErrors.rest_url}
              placeholder="https://api.example.com/data"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl sx={{ width: 120 }}>
              <InputLabel>{t('stage.restMethod')}</InputLabel>
              <Select
                value={stageForm.rest_method}
                onChange={(e) => updateStageFormField('rest_method', e.target.value)}
                label={t('stage.restMethod')}
              >
                <MenuItem value="GET">GET</MenuItem>
                <MenuItem value="POST">POST</MenuItem>
                <MenuItem value="PUT">PUT</MenuItem>
                <MenuItem value="PATCH">PATCH</MenuItem>
              </Select>
            </FormControl>
            <Button
              variant="outlined"
              startIcon={<ApiIcon />}
              onClick={handleTestRESTAPI}
              disabled={testingConnection}
            >
              {testingConnection ? <CircularProgress size={20} /> : t('stage.testConnection')}
            </Button>
            <TextField
              label={t('stage.restHeaders')}
              value={stageForm.rest_headers}
              onChange={(e) => updateStageFormField('rest_headers', e.target.value)}
              helperText={t('stage.restHeadersHelp')}
              placeholder='{"Authorization": "Bearer token", "Content-Type": "application/json"}'
              multiline
              rows={3}
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
          </Stack>
        )
      // File Output stage
      case 'file':
        return (
          <Stack spacing={2}>
            <TextField
              label={t('stage.filePath')}
              value={stageForm.file_path}
              onChange={(e) => updateStageFormField('file_path', e.target.value)}
              error={!!stageFormErrors.file_path}
              helperText={stageFormErrors.file_path}
              placeholder="/data/output"
              required
              fullWidth
              sx={{ fontFamily: 'monospace' }}
            />
            <FormControl sx={{ width: 150 }}>
              <InputLabel>{t('stage.fileFormat')}</InputLabel>
              <Select
                value={stageForm.file_format}
                onChange={(e) => updateStageFormField('file_format', e.target.value)}
                label={t('stage.fileFormat')}
              >
                <MenuItem value="json">JSON</MenuItem>
                <MenuItem value="csv">CSV</MenuItem>
                <MenuItem value="parquet">Parquet</MenuItem>
              </Select>
            </FormControl>
          </Stack>
        )
      default:
        return null
    }
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: 400 }}>
        <CircularProgress size={40} />
      </Box>
    )
  }

  return (
    <Box className="stage-editor-page">
      {/* Header */}
      <Box className="stage-editor-header">
        <Box className="stage-editor-header-left">
          <Breadcrumbs>
            <Typography sx={{
              color: "text.secondary"
            }}>{workflow?.project?.name || projectAlias}</Typography>
            <Link
              component="button"
              underline="hover"
              color="inherit"
              onClick={() => navigate(`/workflows/${workflowId}`)}
            >
              {workflow?.name}
            </Link>
            <Typography sx={{
              color: "text.secondary"
            }}>{pipeline?.name}</Typography>
            <Typography sx={{
              color: "text.primary"
            }}>{t('stage.title')}</Typography>
          </Breadcrumbs>
          <Typography variant="h5" sx={{ mt: 1 }}>
            {t('stage.title')} - {pipeline?.name}
          </Typography>
        </Box>
        <Stack direction="row" spacing={1}>
          <Button
            startIcon={<ArrowBackIcon />}
            onClick={() => navigate(`/workflows/${workflowId}`)}
          >
            {t('common.back')}
          </Button>
          <Button
            variant="contained"
            startIcon={<SaveIcon />}
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? <CircularProgress size={20} /> : t('common.save')}
          </Button>
        </Stack>
      </Box>
      {/* Content */}
      <Card className="stage-editor-content">
        <CardContent>
          <Tabs value={editMode} onChange={handleTabChange} sx={{ mb: 2 }}>
            <Tab label={t('stage.visual')} value="visual" />
            <Tab label={t('stage.yaml')} value="yaml" />
          </Tabs>

          {editMode === 'visual' ? (
            <Box className="stage-list">
              {stages.length === 0 ? (
                <Box sx={{ textAlign: 'center', py: 5 }}>
                  <Typography
                    sx={{
                      color: "text.secondary",
                      mb: 2
                    }}>{t('stage.noStages')}</Typography>
                  <Button variant="contained" startIcon={<AddIcon />} onClick={handleAddStage}>
                    {t('stage.add')}
                  </Button>
                </Box>
              ) : (
                <>
                  {stages.map((stage, index) => (
                    <Box
                      key={stage.id}
                      className={`stage-card ${draggedId === stage.id ? 'dragging' : ''} ${dropTargetId === stage.id ? 'drop-target' : ''}`}
                      draggable
                      onDragStart={e => handleDragStart(e, stage.id)}
                      onDragOver={e => handleDragOver(e, stage.id)}
                      onDragLeave={handleDragLeave}
                      onDrop={e => handleDrop(e, stage.id)}
                      onDragEnd={handleDragEnd}
                    >
                      <Box className="stage-card-drag-handle">
                        <DragHandleIcon />
                      </Box>
                      <Box className="stage-card-index">{index + 1}</Box>
                      <Box className="stage-card-content">
                        <Box className="stage-card-header">
                          <Typography variant="subtitle2" className="stage-card-name">{stage.name}</Typography>
                          <Chip
                            icon={stageTypeConfig[stage.type]?.icon as React.ReactElement}
                            label={stageTypeConfig[stage.type]?.label}
                            size="small"
                            sx={{ bgcolor: stageTypeConfig[stage.type]?.color, color: 'white' }}
                          />
                        </Box>
                        <Box className="stage-card-description">
                          {renderStageDescription(stage)}
                        </Box>
                      </Box>
                      <Box className="stage-card-actions">
                        <Button
                          size="small"
                          onClick={() => handleEditStage(stage)}
                        >
                          <EditIcon fontSize="small" />
                        </Button>
                        <Button
                          size="small"
                          color="error"
                          onClick={() => handleDeleteStage(stage.id)}
                        >
                          <DeleteIcon fontSize="small" />
                        </Button>
                      </Box>
                    </Box>
                  ))}
                  <Button
                    variant="outlined"
                    startIcon={<AddIcon />}
                    onClick={handleAddStage}
                    fullWidth
                    sx={{ mt: 2 }}
                  >
                    {t('stage.add')}
                  </Button>
                </>
              )}
            </Box>
          ) : (
            <Box className="yaml-editor-container">
              {yamlError && (
                <Box className="yaml-error" sx={{ mb: 1, p: 1, bgcolor: 'error.light', borderRadius: 1 }}>
                  <Typography color="error">{yamlError}</Typography>
                </Box>
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
            </Box>
          )}
        </CardContent>
      </Card>
      {/* Stage Modal */}
      <Dialog
        open={stageModalVisible}
        onClose={() => setStageModalVisible(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>
          {editingStage ? t('stage.edit') : t('stage.add')}
        </DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label={t('stage.name')}
              value={stageForm.name}
              onChange={(e) => updateStageFormField('name', e.target.value)}
              error={!!stageFormErrors.name}
              helperText={stageFormErrors.name}
              placeholder="filter-active"
              required
              fullWidth
            />

            <FormControl fullWidth required error={!!stageFormErrors.type}>
              <InputLabel>{t('stage.type')}</InputLabel>
              <Select
                value={stageForm.type}
                onChange={(e) => handleStageTypeChange(e.target.value as StageType)}
                label={t('stage.type')}
              >
                <MenuItem value="filter">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <FilterAltIcon sx={{ color: '#1976d2' }} />
                    <span>{t('stage.types.filter')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="remap">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <SwapHorizIcon sx={{ color: '#4caf50' }} />
                    <span>{t('stage.types.remap')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="drop">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <RemoveCircleIcon sx={{ color: '#f44336' }} />
                    <span>{t('stage.types.drop')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="merge">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <MergeIcon sx={{ color: '#00bcd4' }} />
                    <span>{t('stage.types.merge')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="split">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <CallSplitIcon sx={{ color: '#e91e63' }} />
                    <span>{t('stage.types.split')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="encrypt">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <LockIcon sx={{ color: '#ffc107' }} />
                    <span>{t('stage.types.encrypt')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="dedupe">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <ContentCopyIcon sx={{ color: '#ff5722' }} />
                    <span>{t('stage.types.dedupe')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="default">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <TextFieldsIcon sx={{ color: '#3f51b5' }} />
                    <span>{t('stage.types.default')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="cast">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <NumbersIcon sx={{ color: '#cddc39' }} />
                    <span>{t('stage.types.cast')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="timestamp">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <AccessTimeIcon sx={{ color: '#e91e63' }} />
                    <span>{t('stage.types.timestamp')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="throttle">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <SpeedIcon sx={{ color: '#ff9800' }} />
                    <span>{t('stage.types.throttle')}</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="validate">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <CheckCircleIcon sx={{ color: '#9e9e9e' }} />
                    <span>{t('stage.types.validate')}</span>
                  </Stack>
                </MenuItem>
                {/* Output Stage Types */}
                <MenuItem value="sql">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>SQL Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="elasticsearch">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>Elasticsearch Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="kafka">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>Kafka Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="mongodb">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>MongoDB Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="s3">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>S3 Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="rest_api">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>REST API Output</span>
                  </Stack>
                </MenuItem>
                <MenuItem value="file">
                  <Stack direction="row" spacing={1} sx={{
                    alignItems: "center"
                  }}>
                    <OutputIcon sx={{ color: '#9c27b0' }} />
                    <span>File Output</span>
                  </Stack>
                </MenuItem>
              </Select>
              {stageFormErrors.type && <FormHelperText>{stageFormErrors.type}</FormHelperText>}
            </FormControl>

            {renderTypeSpecificFields()}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setStageModalVisible(false)}>{t('common.cancel')}</Button>
          <Button variant="contained" onClick={handleStageSubmit}>{t('common.save')}</Button>
        </DialogActions>
      </Dialog>
      {/* Delete Confirmation Dialog */}
      <ConfirmDialog
        open={deleteDialogOpen}
        title={t('stage.deleteConfirm')}
        message={t('stage.deleteConfirmMessage')}
        confirmText={t('common.yes')}
        cancelText={t('common.no')}
        onConfirm={confirmDeleteStage}
        onCancel={() => {
          setDeleteDialogOpen(false)
          setStageToDelete(null)
        }}
        severity="error"
      />
    </Box>
  );
}
