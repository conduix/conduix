import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Box,
  Card,
  CardContent,
  Chip,
  Button,
  Typography,
  Tabs,
  Tab,
  Breadcrumbs,
  Link,
  TextField,
  FormControl,
  Select,
  MenuItem,
  InputLabel,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableRow,
} from '@mui/material'
import {
  ArrowBack as ArrowBackIcon,
  Save as SaveIcon,
  Storage as DatabaseIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { SchemaEditor, DataTypeSchema, DataTypeField } from '../components/SchemaEditor'
import { useSnackbar } from '../hooks/useSnackbar'

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

interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel(props: TabPanelProps) {
  const { children, value, index, ...other } = props
  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      id={`tabpanel-${index}`}
      aria-labelledby={`tab-${index}`}
      {...other}
    >
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  )
}

export default function DataModelDetailPage() {
  const { t } = useTranslation()
  const { showSuccess, showError } = useSnackbar()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [dataModel, setDataModel] = useState<DataType | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [tabValue, setTabValue] = useState(0)

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    display_name: '',
    description: '',
    category: '',
  })
  const [formErrors, setFormErrors] = useState<Record<string, string>>({})

  // Schema state
  const [schema, setSchema] = useState<DataTypeSchema>({})
  const [jsonSchema, setJsonSchema] = useState<string>('')
  const [hasChanges, setHasChanges] = useState(false)

  const fetchDataModel = useCallback(async () => {
    try {
      setLoading(true)
      const response = await api.getDataType(id!)
      if (response.success) {
        const data = response.data as DataType
        setDataModel(data)

        // Initialize form
        setFormData({
          name: data.name || '',
          display_name: data.display_name || '',
          description: data.description || '',
          category: data.category || '',
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
              // Ignore parse errors
            }
          }
        }
      } else {
        showError(t('dataModel.loadError'))
      }
    } catch (error) {
      showError(t('dataModel.loadError'))
    } finally {
      setLoading(false)
    }
  }, [id, showError, t])

  useEffect(() => {
    if (id) {
      fetchDataModel()
    }
  }, [id, fetchDataModel])

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
        showSuccess(t('dataModel.schemaAutoSaved'))
        setHasChanges(false)
      } else {
        showError(response.error || t('dataModel.updateError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('dataModel.updateError'))
    }
  }, [id, t, showSuccess, showError])

  const validateForm = () => {
    const errors: Record<string, string> = {}
    if (!formData.display_name) {
      errors.display_name = t('dataModel.nameRequired')
    }
    if (!formData.name) {
      errors.name = t('dataModel.slugRequired')
    } else if (!/^[a-z0-9_-]+$/.test(formData.name)) {
      errors.name = t('dataModel.slugPattern')
    }
    setFormErrors(errors)
    return Object.keys(errors).length === 0
  }

  const handleSave = async () => {
    if (!validateForm()) return

    try {
      setSaving(true)

      const updateData = {
        ...formData,
        schema: schema,
        json_schema: jsonSchema,
      }

      const response = await api.updateDataType(id!, updateData)
      if (response.success) {
        showSuccess(t('dataModel.updateSuccess'))
        setHasChanges(false)
        fetchDataModel() // Refresh data
      } else {
        showError(response.error || t('dataModel.updateError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('dataModel.updateError'))
    } finally {
      setSaving(false)
    }
  }

  const handleFormChange = (field: string, value: string) => {
    setFormData(prev => ({ ...prev, [field]: value }))
    setHasChanges(true)
    // Clear error when user starts typing
    if (formErrors[field]) {
      setFormErrors(prev => ({ ...prev, [field]: '' }))
    }
  }

  const getCategoryConfig = (category: string | undefined) => {
    const configs: Record<string, { color: 'primary' | 'success' | 'warning' | 'secondary' | 'info' | 'default'; text: string }> = {
      master: { color: 'primary', text: t('dataModel.categories.master') },
      transaction: { color: 'success', text: t('dataModel.categories.transaction') },
      log: { color: 'warning', text: t('dataModel.categories.log') },
      metric: { color: 'secondary', text: t('dataModel.categories.metric') },
      reference: { color: 'info', text: t('dataModel.categories.reference') },
    }
    return configs[category || ''] || { color: 'default' as const, text: category || '-' }
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
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress />
      </Box>
    )
  }

  if (!dataModel) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <Typography sx={{
          color: "text.secondary"
        }}>{t('dataModel.notFound')}</Typography>
      </Box>
    );
  }

  const categoryConfig = getCategoryConfig(dataModel.category)

  return (
    <Box>
      <Breadcrumbs sx={{ mb: 2 }}>
        <Link
          component="button"
          underline="hover"
          color="inherit"
          onClick={() => navigate('/data-models')}
        >
          {t('dataModel.list')}
        </Link>
        <Typography sx={{
          color: "text.primary"
        }}>{dataModel.display_name}</Typography>
      </Breadcrumbs>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Button
            startIcon={<ArrowBackIcon />}
            onClick={() => navigate('/data-models')}
          >
            {t('common.back')}
          </Button>
          <Typography variant="h5" sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <DatabaseIcon />
            {dataModel.display_name}
          </Typography>
          <Chip label={categoryConfig.text} color={categoryConfig.color} size="small" />
        </Box>
        <Button
          variant="contained"
          startIcon={<SaveIcon />}
          onClick={handleSave}
          disabled={!hasChanges || saving}
        >
          {saving ? <CircularProgress size={20} /> : t('common.save')}
        </Button>
      </Box>
      <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label={t('dataModel.overview')} />
          <Tab label={t('dataModel.schemaTab')} />
        </Tabs>
      </Box>
      <TabPanel value={tabValue} index={0}>
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <TextField
                label={t('dataModel.name')}
                value={formData.display_name}
                onChange={(e) => handleFormChange('display_name', e.target.value)}
                error={!!formErrors.display_name}
                helperText={formErrors.display_name}
                required
                fullWidth
                placeholder={t('dataModel.namePlaceholder')}
              />

              <TextField
                label={t('dataModel.slug')}
                value={formData.name}
                onChange={(e) => handleFormChange('name', e.target.value)}
                error={!!formErrors.name}
                helperText={formErrors.name || t('dataModel.slugHelp')}
                required
                fullWidth
                placeholder={t('dataModel.slugPlaceholder')}
              />

              <FormControl fullWidth>
                <InputLabel>{t('dataModel.category')}</InputLabel>
                <Select
                  value={formData.category}
                  onChange={(e) => handleFormChange('category', e.target.value)}
                  label={t('dataModel.category')}
                >
                  <MenuItem value="">{t('dataModel.selectCategory')}</MenuItem>
                  <MenuItem value="master">{t('dataModel.categories.master')}</MenuItem>
                  <MenuItem value="transaction">{t('dataModel.categories.transaction')}</MenuItem>
                  <MenuItem value="log">{t('dataModel.categories.log')}</MenuItem>
                  <MenuItem value="metric">{t('dataModel.categories.metric')}</MenuItem>
                  <MenuItem value="reference">{t('dataModel.categories.reference')}</MenuItem>
                </Select>
              </FormControl>

              <TextField
                label={t('common.description')}
                value={formData.description}
                onChange={(e) => handleFormChange('description', e.target.value)}
                multiline
                rows={3}
                fullWidth
              />
            </Box>

            <Box sx={{ mt: 3 }}>
              <Typography
                variant="subtitle2"
                sx={{
                  color: "text.secondary",
                  mb: 1
                }}>
                {t('dataModel.additionalInfo')}
              </Typography>
              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell component="th" sx={{ fontWeight: 'medium', width: 200 }}>
                      {t('dataModel.project')}
                    </TableCell>
                    <TableCell>{dataModel.project?.name || '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ fontWeight: 'medium' }}>
                      {t('dataModel.parent')}
                    </TableCell>
                    <TableCell>
                      {dataModel.parent?.display_name || t('dataModel.parentPlaceholder')}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ fontWeight: 'medium' }}>
                      {t('dataModel.idFields')}
                    </TableCell>
                    <TableCell>
                      {getIdFields().length > 0 ? (
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                          {getIdFields().map(field => (
                            <Chip key={field} label={field} size="small" />
                          ))}
                        </Box>
                      ) : '-'}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ fontWeight: 'medium' }}>
                      {t('common.createdAt')}
                    </TableCell>
                    <TableCell>
                      {new Date(dataModel.created_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ fontWeight: 'medium' }}>
                      {t('common.updatedAt')}
                    </TableCell>
                    <TableCell>
                      {new Date(dataModel.updated_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </Box>
          </CardContent>
        </Card>
      </TabPanel>
      <TabPanel value={tabValue} index={1}>
        <Card>
          <CardContent>
            <SchemaEditor
              schema={schema}
              jsonSchema={jsonSchema}
              onChange={handleSchemaChange}
              onGenerateComplete={handleSchemaGenerateComplete}
              idFields={getIdFields()}
            />
          </CardContent>
        </Card>
      </TabPanel>
    </Box>
  );
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
