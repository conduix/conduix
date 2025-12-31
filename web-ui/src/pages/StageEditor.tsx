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
  validate: { color: 'orange', icon: <CheckCircleOutlined />, label: 'Validate' },
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
      condition: stage.config?.condition || '',
      mappings: stage.config?.mappings ? JSON.stringify(stage.config.mappings, null, 2) : '',
      schema: stage.config?.schema ? JSON.stringify(stage.config.schema, null, 2) : '',
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
                  Filter
                </Space>
              </Select.Option>
              <Select.Option value="remap">
                <Space>
                  <SwapOutlined style={{ color: '#52c41a' }} />
                  Remap
                </Space>
              </Select.Option>
              <Select.Option value="validate">
                <Space>
                  <CheckCircleOutlined style={{ color: '#fa8c16' }} />
                  Validate
                </Space>
              </Select.Option>
              <Select.Option value="sink">
                <Space>
                  <ExportOutlined style={{ color: '#722ed1' }} />
                  Sink
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
