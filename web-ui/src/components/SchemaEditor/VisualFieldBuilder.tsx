import { useState } from 'react'
import {
  Card,
  CardContent,
  Button,
  Stack,
  TextField,
  Select,
  MenuItem,
  FormControlLabel,
  Checkbox,
  IconButton,
  Menu,
  Box,
  Typography,
  InputAdornment,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  FormControl,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteIcon from '@mui/icons-material/Delete'
import DragIndicatorIcon from '@mui/icons-material/DragIndicator'
import VpnKeyIcon from '@mui/icons-material/VpnKey'
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown'
import { useTranslation } from 'react-i18next'
import { useSnackbar } from '../../hooks/useSnackbar'

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
  const { showWarning } = useSnackbar()
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)
  const [deleteConfirmIndex, setDeleteConfirmIndex] = useState<number | null>(null)
  const [quickAddAnchor, setQuickAddAnchor] = useState<null | HTMLElement>(null)

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
      showWarning(t('dataModel.schema.duplicateFieldName'))
      return
    }
    onChange([newField, ...fields])
    setQuickAddAnchor(null)
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
      showWarning(t('dataModel.schema.duplicateFieldName'))
      return
    }
    onChange([...fields, ...newFields])
    setQuickAddAnchor(null)
  }

  const handleFieldChange = (index: number, field: Partial<DataTypeField>) => {
    const newFields = [...fields]
    newFields[index] = { ...newFields[index], ...field }
    onChange(newFields)
  }

  const handleDeleteField = (index: number) => {
    const newFields = fields.filter((_, i) => i !== index)
    onChange(newFields)
    setDeleteConfirmIndex(null)
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

  const isKeyField = (fieldName: string) => idFields.includes(fieldName)

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Stack direction="row" spacing={1}>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={handleAddField}
          >
            {t('dataModel.schema.addField')}
          </Button>
          <Button
            variant="outlined"
            endIcon={<KeyboardArrowDownIcon />}
            onClick={(e) => setQuickAddAnchor(e.currentTarget)}
          >
            {t('dataModel.quickAdd.title')}
          </Button>
          <Menu
            anchorEl={quickAddAnchor}
            open={Boolean(quickAddAnchor)}
            onClose={() => setQuickAddAnchor(null)}
          >
            <MenuItem onClick={handleAddIdField}>
              {t('dataModel.quickAdd.idField')}
            </MenuItem>
            <MenuItem onClick={handleAddTimestampFields}>
              {t('dataModel.quickAdd.timestamps')}
            </MenuItem>
          </Menu>
        </Stack>
      </Box>
      {fields.length === 0 ? (
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            py: 6,
            color: 'text.secondary',
          }}
        >
          <Typography variant="body2">
            {t('dataModel.schema.noFields')}
          </Typography>
        </Box>
      ) : (
        <Stack spacing={1}>
          {fields.map((field, index) => (
            <Card
              key={index}
              variant="outlined"
              draggable
              onDragStart={(e) => handleDragStart(e, index)}
              onDragOver={(e) => handleDragOver(e, index)}
              onDragLeave={handleDragLeave}
              onDrop={(e) => handleDrop(e, index)}
              onDragEnd={handleDragEnd}
              sx={{
                cursor: 'grab',
                opacity: draggedIndex === index ? 0.5 : 1,
                borderColor: dragOverIndex === index ? 'primary.main' : undefined,
                borderStyle: dragOverIndex === index ? 'dashed' : undefined,
              }}
            >
              <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
                <Stack direction="row" spacing={1.5} sx={{
                  alignItems: "center"
                }}>
                  <DragIndicatorIcon sx={{ color: 'text.disabled', cursor: 'grab' }} />

                  <TextField
                    size="small"
                    placeholder={t('dataModel.schema.fieldNamePlaceholder')}
                    value={field.name}
                    onChange={(e) => handleFieldChange(index, { name: e.target.value })}
                    sx={{ width: 150 }}
                    error={!field.name}
                    slotProps={{ input: {
                      endAdornment: isKeyField(field.name) ? (
                        <InputAdornment position="end">
                          <VpnKeyIcon sx={{ color: 'warning.main', fontSize: 18 }} />
                        </InputAdornment>
                      ) : null,
                    } }}
                  />

                  <FormControl size="small" sx={{ width: 130 }}>
                    <Select
                      value={field.type}
                      onChange={(e) => handleFieldChange(index, { type: e.target.value })}
                    >
                      {FIELD_TYPES.map(ft => (
                        <MenuItem key={ft.value} value={ft.value}>
                          {t(`dataModel.schema.fieldTypes.${ft.value}`, ft.label)}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>

                  <FormControlLabel
                    control={
                      <Checkbox
                        size="small"
                        checked={field.required || false}
                        onChange={(e) => handleFieldChange(index, { required: e.target.checked })}
                      />
                    }
                    label={t('dataModel.schema.fieldRequired')}
                    sx={{ minWidth: 100 }}
                  />

                  <TextField
                    size="small"
                    placeholder={t('dataModel.schema.fieldDescriptionPlaceholder')}
                    value={field.description || ''}
                    onChange={(e) => handleFieldChange(index, { description: e.target.value })}
                    sx={{ flex: 1, minWidth: 150 }}
                  />

                  <IconButton
                    color="error"
                    size="small"
                    onClick={() => setDeleteConfirmIndex(index)}
                  >
                    <DeleteIcon />
                  </IconButton>
                </Stack>
              </CardContent>
            </Card>
          ))}
        </Stack>
      )}
      <Dialog
        open={deleteConfirmIndex !== null}
        onClose={() => setDeleteConfirmIndex(null)}
      >
        <DialogTitle>{t('dataModel.schema.deleteFieldConfirm')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {deleteConfirmIndex !== null && fields[deleteConfirmIndex]?.name
              ? t('dataModel.schema.deleteFieldConfirmMessage', { name: fields[deleteConfirmIndex].name })
              : t('dataModel.schema.deleteFieldConfirm')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteConfirmIndex(null)}>
            {t('common.cancel')}
          </Button>
          <Button
            color="error"
            variant="contained"
            onClick={() => deleteConfirmIndex !== null && handleDeleteField(deleteConfirmIndex)}
          >
            {t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
