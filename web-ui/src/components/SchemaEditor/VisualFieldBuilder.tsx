import { useState } from 'react'
import {
  Card,
  Button,
  Space,
  Input,
  Select,
  Checkbox,
  Popconfirm,
  Empty,
  Dropdown,
  message,
} from 'antd'
import {
  PlusOutlined,
  DeleteOutlined,
  HolderOutlined,
  KeyOutlined,
  DownOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

export interface DataTypeField {
  name: string
  type: string
  required?: boolean
  description?: string
}

interface VisualFieldBuilderProps {
  fields: DataTypeField[]
  onChange: (fields: DataTypeField[]) => void
  idFields?: string[]
}

const FIELD_TYPES = [
  { value: 'string', label: 'String' },
  { value: 'integer', label: 'Integer' },
  { value: 'number', label: 'Number' },
  { value: 'boolean', label: 'Boolean' },
  { value: 'datetime', label: 'DateTime' },
  { value: 'date', label: 'Date' },
  { value: 'json', label: 'JSON' },
  { value: 'array', label: 'Array' },
  { value: 'text', label: 'Text' },
  { value: 'uuid', label: 'UUID' },
]

export default function VisualFieldBuilder({ fields, onChange, idFields = [] }: VisualFieldBuilderProps) {
  const { t } = useTranslation()
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)

  const handleAddField = () => {
    const newField: DataTypeField = {
      name: '',
      type: 'string',
      required: false,
      description: '',
    }
    onChange([...fields, newField])
  }

  const handleAddIdField = () => {
    const newField: DataTypeField = {
      name: 'id',
      type: 'integer',
      required: true,
      description: 'Primary key',
    }
    // Check if id field already exists
    if (fields.some(f => f.name === 'id')) {
      message.warning(t('dataModel.schema.duplicateFieldName'))
      return
    }
    onChange([newField, ...fields])
  }

  const handleAddTimestampFields = () => {
    const newFields: DataTypeField[] = []
    if (!fields.some(f => f.name === 'created_at')) {
      newFields.push({
        name: 'created_at',
        type: 'datetime',
        required: false,
        description: 'Record creation timestamp',
      })
    }
    if (!fields.some(f => f.name === 'updated_at')) {
      newFields.push({
        name: 'updated_at',
        type: 'datetime',
        required: false,
        description: 'Record last update timestamp',
      })
    }
    if (newFields.length === 0) {
      message.warning(t('dataModel.schema.duplicateFieldName'))
      return
    }
    onChange([...fields, ...newFields])
  }

  const handleFieldChange = (index: number, field: Partial<DataTypeField>) => {
    const newFields = [...fields]
    newFields[index] = { ...newFields[index], ...field }
    onChange(newFields)
  }

  const handleDeleteField = (index: number) => {
    const newFields = fields.filter((_, i) => i !== index)
    onChange(newFields)
  }

  const handleDragStart = (e: React.DragEvent, index: number) => {
    setDraggedIndex(index)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', index.toString())
  }

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragOverIndex(index)
  }

  const handleDragLeave = () => {
    setDragOverIndex(null)
  }

  const handleDrop = (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault()
    if (draggedIndex === null || draggedIndex === targetIndex) {
      setDraggedIndex(null)
      setDragOverIndex(null)
      return
    }

    const newFields = [...fields]
    const [removed] = newFields.splice(draggedIndex, 1)
    newFields.splice(targetIndex, 0, removed)
    onChange(newFields)

    setDraggedIndex(null)
    setDragOverIndex(null)
  }

  const handleDragEnd = () => {
    setDraggedIndex(null)
    setDragOverIndex(null)
  }

  const quickAddItems = [
    {
      key: 'id',
      label: t('dataModel.quickAdd.idField'),
      onClick: handleAddIdField,
    },
    {
      key: 'timestamps',
      label: t('dataModel.quickAdd.timestamps'),
      onClick: handleAddTimestampFields,
    },
  ]

  const isKeyField = (fieldName: string) => idFields.includes(fieldName)

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAddField}>
            {t('dataModel.schema.addField')}
          </Button>
          <Dropdown menu={{ items: quickAddItems }} placement="bottomLeft">
            <Button>
              {t('dataModel.quickAdd.title')} <DownOutlined />
            </Button>
          </Dropdown>
        </Space>
      </div>

      {fields.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t('dataModel.schema.noFields')}
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {fields.map((field, index) => (
            <Card
              key={index}
              size="small"
              draggable
              onDragStart={(e) => handleDragStart(e, index)}
              onDragOver={(e) => handleDragOver(e, index)}
              onDragLeave={handleDragLeave}
              onDrop={(e) => handleDrop(e, index)}
              onDragEnd={handleDragEnd}
              style={{
                cursor: 'grab',
                opacity: draggedIndex === index ? 0.5 : 1,
                borderColor: dragOverIndex === index ? '#1890ff' : undefined,
                borderStyle: dragOverIndex === index ? 'dashed' : undefined,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <HolderOutlined style={{ color: '#999', cursor: 'grab' }} />

                <Input
                  placeholder={t('dataModel.schema.fieldNamePlaceholder')}
                  value={field.name}
                  onChange={(e) => handleFieldChange(index, { name: e.target.value })}
                  style={{ width: 150 }}
                  status={!field.name ? 'error' : undefined}
                  addonAfter={isKeyField(field.name) ? <KeyOutlined style={{ color: '#faad14' }} /> : null}
                />

                <Select
                  value={field.type}
                  onChange={(value) => handleFieldChange(index, { type: value })}
                  style={{ width: 130 }}
                  options={FIELD_TYPES.map(ft => ({
                    value: ft.value,
                    label: t(`dataModel.schema.fieldTypes.${ft.value}`, ft.label),
                  }))}
                />

                <Checkbox
                  checked={field.required}
                  onChange={(e) => handleFieldChange(index, { required: e.target.checked })}
                >
                  {t('dataModel.schema.fieldRequired')}
                </Checkbox>

                <Input
                  placeholder={t('dataModel.schema.fieldDescriptionPlaceholder')}
                  value={field.description}
                  onChange={(e) => handleFieldChange(index, { description: e.target.value })}
                  style={{ flex: 1, minWidth: 150 }}
                />

                <Popconfirm
                  title={t('dataModel.schema.deleteFieldConfirm')}
                  onConfirm={() => handleDeleteField(index)}
                  okText={t('common.delete')}
                  cancelText={t('common.cancel')}
                >
                  <Button type="text" danger icon={<DeleteOutlined />} />
                </Popconfirm>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
