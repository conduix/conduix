import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  Descriptions,
  Tag,
  Button,
  Space,
  message,
  Spin,
  Typography,
  Tabs,
  Breadcrumb,
  Form,
  Input,
  Select,
} from 'antd'
import {
  ArrowLeftOutlined,
  SaveOutlined,
  DatabaseOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { SchemaEditor, DataTypeSchema, DataTypeField } from '../components/SchemaEditor'

const { Title } = Typography

interface DataType {
  id: string
  project_id: string
  parent_id?: string | null
  name: string
  display_name: string
  description?: string
  category?: string
  id_fields?: string | string[]
  json_schema?: string
  schema?: DataTypeSchema
  created_at: string
  updated_at: string
  project?: {
    id: string
    name: string
    alias: string
  }
  parent?: DataType
}

export default function DataModelDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [dataModel, setDataModel] = useState<DataType | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  // Schema state
  const [schema, setSchema] = useState<DataTypeSchema>({})
  const [jsonSchema, setJsonSchema] = useState<string>('')
  const [hasChanges, setHasChanges] = useState(false)

  useEffect(() => {
    if (id) {
      fetchDataModel()
    }
  }, [id])

  const fetchDataModel = async () => {
    try {
      setLoading(true)
      const response = await api.getDataType(id!)
      if (response.success) {
        const data = response.data as DataType
        setDataModel(data)

        // Initialize form
        form.setFieldsValue({
          name: data.name,
          display_name: data.display_name,
          description: data.description,
          category: data.category,
        })

        // Initialize schema
        if (data.schema) {
          setSchema(data.schema)
        }
        if (data.json_schema) {
          setJsonSchema(data.json_schema)
          // If we have json_schema but no schema.fields, parse it
          if (!data.schema?.fields || data.schema.fields.length === 0) {
            try {
              const parsed = JSON.parse(data.json_schema)
              const fields: DataTypeField[] = []
              const required = parsed.required || []
              if (parsed.properties) {
                for (const [name, prop] of Object.entries(parsed.properties)) {
                  const typedProp = prop as Record<string, unknown>
                  fields.push({
                    name,
                    type: mapJsonSchemaTypeToFieldType(
                      typedProp.type as string,
                      typedProp.format as string | undefined
                    ),
                    required: required.includes(name),
                    description: typedProp.description as string | undefined,
                  })
                }
              }
              setSchema({ type: 'json_schema', fields, definition: data.json_schema })
            } catch {
              // Keep empty schema if parse fails
            }
          }
        }

        setHasChanges(false)
      }
    } catch (error) {
      message.error(t('dataModel.loadError'))
    } finally {
      setLoading(false)
    }
  }

  const handleSchemaChange = useCallback((newSchema: DataTypeSchema, newJsonSchema: string) => {
    setSchema(newSchema)
    setJsonSchema(newJsonSchema)
    setHasChanges(true)
  }, [])

  // Auto-save when schema is generated from sample data
  const handleSchemaGenerateComplete = useCallback(async (newSchema: DataTypeSchema, newJsonSchema: string) => {
    try {
      const updateData = {
        schema: newSchema,
        json_schema: newJsonSchema,
      }
      const response = await api.updateDataType(id!, updateData)
      if (response.success) {
        message.success(t('dataModel.schemaAutoSaved'))
        setHasChanges(false)
      } else {
        message.error(response.error || t('dataModel.updateError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('dataModel.updateError'))
    }
  }, [id, t])

  const handleSave = async () => {
    try {
      setSaving(true)
      const values = await form.validateFields()

      const updateData = {
        ...values,
        schema: schema,
        json_schema: jsonSchema,
      }

      const response = await api.updateDataType(id!, updateData)
      if (response.success) {
        message.success(t('dataModel.updateSuccess'))
        setHasChanges(false)
        fetchDataModel() // Refresh data
      } else {
        message.error(response.error || t('dataModel.updateError'))
      }
    } catch (error: any) {
      message.error(error.response?.data?.error || t('dataModel.updateError'))
    } finally {
      setSaving(false)
    }
  }

  const handleFormChange = () => {
    setHasChanges(true)
  }

  const getCategoryConfig = (category: string | undefined) => {
    const configs: Record<string, { color: string; text: string }> = {
      master: { color: 'blue', text: t('dataModel.categories.master') },
      transaction: { color: 'green', text: t('dataModel.categories.transaction') },
      log: { color: 'orange', text: t('dataModel.categories.log') },
      metric: { color: 'purple', text: t('dataModel.categories.metric') },
      reference: { color: 'cyan', text: t('dataModel.categories.reference') },
    }
    return configs[category || ''] || { color: 'default', text: category || '-' }
  }

  const getIdFields = (): string[] => {
    if (!dataModel?.id_fields) return []
    if (Array.isArray(dataModel.id_fields)) return dataModel.id_fields
    try {
      return JSON.parse(dataModel.id_fields)
    } catch {
      return dataModel.id_fields.split(',').map(s => s.trim())
    }
  }

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: '50px' }}>
        <Spin size="large" />
      </div>
    )
  }

  if (!dataModel) {
    return (
      <div style={{ textAlign: 'center', padding: '50px' }}>
        <Typography.Text type="secondary">{t('dataModel.notFound')}</Typography.Text>
      </div>
    )
  }

  const categoryConfig = getCategoryConfig(dataModel.category)

  const tabItems = [
    {
      key: 'overview',
      label: t('dataModel.overview'),
      children: (
        <Card>
          <Form
            form={form}
            layout="vertical"
            onValuesChange={handleFormChange}
          >
            <Form.Item
              name="display_name"
              label={t('dataModel.name')}
              rules={[{ required: true, message: t('dataModel.nameRequired') }]}
            >
              <Input placeholder={t('dataModel.namePlaceholder')} />
            </Form.Item>

            <Form.Item
              name="name"
              label={t('dataModel.slug')}
              rules={[
                { required: true, message: t('dataModel.slugRequired') },
                { pattern: /^[a-z0-9_-]+$/, message: t('dataModel.slugPattern') },
              ]}
              extra={t('dataModel.slugHelp')}
            >
              <Input placeholder={t('dataModel.slugPlaceholder')} />
            </Form.Item>

            <Form.Item
              name="category"
              label={t('dataModel.category')}
            >
              <Select placeholder={t('dataModel.category')} allowClear>
                <Select.Option value="master">{t('dataModel.categories.master')}</Select.Option>
                <Select.Option value="transaction">{t('dataModel.categories.transaction')}</Select.Option>
                <Select.Option value="log">{t('dataModel.categories.log')}</Select.Option>
                <Select.Option value="metric">{t('dataModel.categories.metric')}</Select.Option>
                <Select.Option value="reference">{t('dataModel.categories.reference')}</Select.Option>
              </Select>
            </Form.Item>

            <Form.Item
              name="description"
              label={t('common.description')}
            >
              <Input.TextArea rows={3} />
            </Form.Item>
          </Form>

          <Descriptions column={2} style={{ marginTop: 24 }}>
            <Descriptions.Item label={t('dataModel.project')}>
              {dataModel.project?.name || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('dataModel.parent')}>
              {dataModel.parent?.display_name || t('dataModel.parentPlaceholder')}
            </Descriptions.Item>
            <Descriptions.Item label={t('dataModel.idFields')}>
              {getIdFields().length > 0 ? (
                <Space>
                  {getIdFields().map(field => (
                    <Tag key={field}>{field}</Tag>
                  ))}
                </Space>
              ) : '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('common.createdAt')}>
              {new Date(dataModel.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label={t('common.updatedAt')}>
              {new Date(dataModel.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        </Card>
      ),
    },
    {
      key: 'schema',
      label: t('dataModel.schemaTab'),
      children: (
        <Card>
          <SchemaEditor
            schema={schema}
            jsonSchema={jsonSchema}
            onChange={handleSchemaChange}
            onGenerateComplete={handleSchemaGenerateComplete}
            idFields={getIdFields()}
          />
        </Card>
      ),
    },
  ]

  return (
    <div>
      <Breadcrumb
        style={{ marginBottom: 16 }}
        items={[
          { title: <a onClick={() => navigate('/data-models')}>{t('dataModel.list')}</a> },
          { title: dataModel.display_name },
        ]}
      />

      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/data-models')}>
            {t('common.back')}
          </Button>
          <Title level={4} style={{ margin: 0 }}>
            <DatabaseOutlined style={{ marginRight: 8 }} />
            {dataModel.display_name}
          </Title>
          <Tag color={categoryConfig.color}>{categoryConfig.text}</Tag>
        </Space>
        <Button
          type="primary"
          icon={<SaveOutlined />}
          onClick={handleSave}
          loading={saving}
          disabled={!hasChanges}
        >
          {t('common.save')}
        </Button>
      </div>

      <Tabs defaultActiveKey="overview" items={tabItems} />
    </div>
  )
}

function mapJsonSchemaTypeToFieldType(type: string, format?: string): string {
  if (type === 'string') {
    if (format === 'date-time') return 'datetime'
    if (format === 'date') return 'date'
    if (format === 'uuid') return 'uuid'
    return 'string'
  }
  if (type === 'integer') return 'integer'
  if (type === 'number') return 'number'
  if (type === 'boolean') return 'boolean'
  if (type === 'object') return 'json'
  if (type === 'array') return 'array'
  return 'string'
}
