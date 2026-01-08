import { useState, useEffect, useCallback } from 'react'
import { Tabs, message, Alert } from 'antd'
import Editor from '@monaco-editor/react'
import { useTranslation } from 'react-i18next'
import VisualFieldBuilder, { DataTypeField } from './VisualFieldBuilder'

export interface DataTypeSchema {
  type?: 'json_schema' | 'avro' | 'infer'
  definition?: string
  fields?: DataTypeField[]
}

interface SchemaEditorProps {
  schema?: DataTypeSchema
  jsonSchema?: string
  onChange: (schema: DataTypeSchema, jsonSchema: string) => void
  idFields?: string[]
}

// Convert fields array to JSON Schema
function fieldsToJsonSchema(fields: DataTypeField[]): string {
  const properties: Record<string, unknown> = {}
  const required: string[] = []

  for (const field of fields) {
    const prop: Record<string, unknown> = {
      type: mapFieldTypeToJsonSchemaType(field.type),
    }
    if (field.description) {
      prop.description = field.description
    }
    // Add format for specific types
    if (field.type === 'datetime') {
      prop.format = 'date-time'
    } else if (field.type === 'date') {
      prop.format = 'date'
    } else if (field.type === 'uuid') {
      prop.format = 'uuid'
    }
    properties[field.name] = prop
    if (field.required) {
      required.push(field.name)
    }
  }

  const schema: Record<string, unknown> = {
    $schema: 'http://json-schema.org/draft-07/schema#',
    type: 'object',
    properties,
  }
  if (required.length > 0) {
    schema.required = required
  }

  return JSON.stringify(schema, null, 2)
}

// Convert JSON Schema to fields array
function jsonSchemaToFields(jsonStr: string): DataTypeField[] {
  try {
    const schema = JSON.parse(jsonStr)
    const fields: DataTypeField[] = []
    const requiredFields = schema.required || []

    if (schema.properties) {
      for (const [name, prop] of Object.entries(schema.properties)) {
        const typedProp = prop as Record<string, unknown>
        fields.push({
          name,
          type: mapJsonSchemaTypeToFieldType(
            typedProp.type as string,
            typedProp.format as string | undefined
          ),
          required: requiredFields.includes(name),
          description: typedProp.description as string | undefined,
        })
      }
    }

    return fields
  } catch {
    return []
  }
}

function mapFieldTypeToJsonSchemaType(type: string): string {
  const mapping: Record<string, string> = {
    string: 'string',
    text: 'string',
    integer: 'integer',
    number: 'number',
    boolean: 'boolean',
    datetime: 'string',
    date: 'string',
    json: 'object',
    array: 'array',
    uuid: 'string',
  }
  return mapping[type] || 'string'
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

export default function SchemaEditor({ schema, jsonSchema, onChange, idFields = [] }: SchemaEditorProps) {
  const { t } = useTranslation()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [fields, setFields] = useState<DataTypeField[]>(schema?.fields || [])
  const [jsonContent, setJsonContent] = useState<string>(jsonSchema || '')
  const [jsonError, setJsonError] = useState<string | null>(null)

  // Initialize from props
  useEffect(() => {
    if (schema?.fields && schema.fields.length > 0) {
      setFields(schema.fields)
      if (!jsonContent) {
        setJsonContent(fieldsToJsonSchema(schema.fields))
      }
    } else if (jsonSchema) {
      setJsonContent(jsonSchema)
      const parsedFields = jsonSchemaToFields(jsonSchema)
      if (parsedFields.length > 0) {
        setFields(parsedFields)
      }
    }
  }, [])

  const handleTabChange = (key: string) => {
    if (key === 'json' && editMode === 'visual') {
      // Sync visual to JSON
      const newJson = fieldsToJsonSchema(fields)
      setJsonContent(newJson)
      setJsonError(null)
    } else if (key === 'visual' && editMode === 'json') {
      // Sync JSON to visual
      if (jsonContent.trim()) {
        try {
          JSON.parse(jsonContent)
          const parsedFields = jsonSchemaToFields(jsonContent)
          setFields(parsedFields)
          setJsonError(null)
        } catch (e) {
          message.warning(t('dataModel.schema.parseError'))
          return // Don't switch tabs if parse fails
        }
      }
    }
    setEditMode(key as 'visual' | 'json')
  }

  const handleFieldsChange = useCallback((newFields: DataTypeField[]) => {
    setFields(newFields)
    const newJson = fieldsToJsonSchema(newFields)
    setJsonContent(newJson)
    onChange(
      { type: 'json_schema', fields: newFields, definition: newJson },
      newJson
    )
  }, [onChange])

  const handleJsonChange = useCallback((value: string | undefined) => {
    const newJson = value || ''
    setJsonContent(newJson)
    try {
      if (newJson.trim()) {
        JSON.parse(newJson)
        setJsonError(null)
        const parsedFields = jsonSchemaToFields(newJson)
        onChange(
          { type: 'json_schema', fields: parsedFields, definition: newJson },
          newJson
        )
      } else {
        setJsonError(null)
        onChange({ type: 'json_schema', fields: [], definition: '' }, '')
      }
    } catch (e) {
      setJsonError((e as Error).message)
    }
  }, [onChange])

  const tabItems = [
    {
      key: 'visual',
      label: t('dataModel.schema.visual'),
      children: (
        <VisualFieldBuilder
          fields={fields}
          onChange={handleFieldsChange}
          idFields={idFields}
        />
      ),
    },
    {
      key: 'json',
      label: t('dataModel.schema.jsonSchema'),
      children: (
        <div>
          {jsonError && (
            <Alert
              message={t('dataModel.schema.invalidJson')}
              description={jsonError}
              type="error"
              showIcon
              style={{ marginBottom: 16 }}
            />
          )}
          <Editor
            height="400px"
            language="json"
            theme="vs-light"
            value={jsonContent}
            onChange={handleJsonChange}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              lineNumbers: 'on',
              tabSize: 2,
              scrollBeyondLastLine: false,
              automaticLayout: true,
              formatOnPaste: true,
              formatOnType: true,
            }}
          />
        </div>
      ),
    },
  ]

  return (
    <Tabs
      activeKey={editMode}
      onChange={handleTabChange}
      items={tabItems}
    />
  )
}
