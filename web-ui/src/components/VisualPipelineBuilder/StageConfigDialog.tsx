import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Box,
  Typography,
  Tabs,
  Tab,
  IconButton,
} from '@mui/material'
import { Close as CloseIcon } from '@mui/icons-material'
import Editor from '@monaco-editor/react'
import yaml from 'js-yaml'

import type { Stage, Output, WorkflowInput } from '../../types/pipeline'

interface StageConfigDialogProps {
  open: boolean
  onClose: () => void
  nodeType: string
  config: Stage | Output | WorkflowInput
  onSave: (config: Stage | Output | WorkflowInput) => void
}

export function StageConfigDialog({
  open,
  onClose,
  nodeType,
  config,
  onSave,
}: StageConfigDialogProps) {
  const [editMode, setEditMode] = useState<'form' | 'yaml'>('form')
  const [localConfig, setLocalConfig] = useState<Stage | Output | WorkflowInput>(config)
  const [yamlContent, setYamlContent] = useState('')
  const [yamlError, setYamlError] = useState<string | null>(null)

  useEffect(() => {
    setLocalConfig(config)
    setYamlContent(yaml.dump(config, { indent: 2 }))
    setYamlError(null)
  }, [config, open])

  const handleYamlChange = (value: string | undefined) => {
    const newYaml = value || ''
    setYamlContent(newYaml)

    try {
      const parsed = yaml.load(newYaml) as Stage | Output | WorkflowInput
      setLocalConfig(parsed)
      setYamlError(null)
    } catch (e) {
      setYamlError((e as Error).message)
    }
  }

  const handleFieldChange = (field: string, value: unknown) => {
    setLocalConfig((prev) => ({
      ...prev,
      [field]: value,
    }))
    setYamlContent(yaml.dump({ ...localConfig, [field]: value }, { indent: 2 }))
  }

  const handleSave = () => {
    if (yamlError) return
    onSave(localConfig)
  }

  const renderFormFields = () => {
    if (nodeType === 'stage') {
      const stageConfig = localConfig as Stage
      return (
        <>
          <TextField
            fullWidth
            label="Name"
            value={stageConfig.name || ''}
            onChange={(e) => handleFieldChange('name', e.target.value)}
            margin="normal"
          />
          <FormControl fullWidth margin="normal">
            <InputLabel>Type</InputLabel>
            <Select
              value={stageConfig.type || ''}
              label="Type"
              onChange={(e) => handleFieldChange('type', e.target.value)}
            >
              {['filter', 'remap', 'enrich', 'validate', 'aggregate', 'router', 'dedupe', 'throttle', 'sub_pipeline', 'batch_lookup', 'async_enrich', 'es_lookup'].map((type) => (
                <MenuItem key={type} value={type}>
                  {type}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </>
      )
    }

    if (nodeType === 'output') {
      const outputConfig = localConfig as Output
      return (
        <>
          <TextField
            fullWidth
            label="Name"
            value={outputConfig.name || ''}
            onChange={(e) => handleFieldChange('name', e.target.value)}
            margin="normal"
          />
          <FormControl fullWidth margin="normal">
            <InputLabel>Type</InputLabel>
            <Select
              value={outputConfig.type || ''}
              label="Type"
              onChange={(e) => handleFieldChange('type', e.target.value)}
            >
              {['elasticsearch', 's3', 'kafka', 'sql', 'mongodb', 'rest_api', 'file'].map((type) => (
                <MenuItem key={type} value={type}>
                  {type}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </>
      )
    }

    if (nodeType === 'input') {
      const inputConfig = localConfig as WorkflowInput
      return (
        <>
          <TextField
            fullWidth
            label="Name"
            value={inputConfig.name || ''}
            onChange={(e) => handleFieldChange('name', e.target.value)}
            margin="normal"
          />
          <FormControl fullWidth margin="normal">
            <InputLabel>Type</InputLabel>
            <Select
              value={inputConfig.type || ''}
              label="Type"
              onChange={(e) => handleFieldChange('type', e.target.value)}
            >
              {['kafka', 'rest_api', 'sql', 'cdc', 'file', 'k8s_logs', 'mongodb_cdc', 'redis_stream'].map((type) => (
                <MenuItem key={type} value={type}>
                  {type}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </>
      )
    }

    return null
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      slotProps={{ paper: {
        sx: { height: '80vh' },
      } }}
    >
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="h6">
          Configure {nodeType === 'stage' ? 'Stage' : nodeType === 'output' ? 'Output' : 'Input'}
        </Typography>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <Box sx={{ borderBottom: 1, borderColor: 'divider', px: 3 }}>
        <Tabs value={editMode} onChange={(_, v) => setEditMode(v)}>
          <Tab label="Form" value="form" />
          <Tab label="YAML" value="yaml" />
        </Tabs>
      </Box>

      <DialogContent sx={{ display: 'flex', flexDirection: 'column', p: 3 }}>
        {editMode === 'form' ? (
          <Box sx={{ flex: 1, overflow: 'auto' }}>
            {renderFormFields()}
            <Typography variant="subtitle2" sx={{ mt: 3, mb: 1 }}>
              Configuration (YAML)
            </Typography>
            <Box sx={{ flex: 1, minHeight: 300, border: 1, borderColor: 'divider', borderRadius: 1 }}>
              <Editor
                height="300px"
                language="yaml"
                value={yaml.dump((localConfig as Stage | Output | WorkflowInput).config || {}, { indent: 2 })}
                onChange={(value) => {
                  try {
                    const parsed = yaml.load(value || '') as Record<string, unknown>
                    handleFieldChange('config', parsed)
                  } catch {
                    // Ignore parse errors during typing
                  }
                }}
                options={{
                  minimap: { enabled: false },
                  lineNumbers: 'on',
                  scrollBeyondLastLine: false,
                  fontSize: 13,
                }}
              />
            </Box>
          </Box>
        ) : (
          <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
            {yamlError && (
              <Typography color="error" variant="caption" sx={{ mb: 1 }}>
                {yamlError}
              </Typography>
            )}
            <Box sx={{ flex: 1, border: 1, borderColor: yamlError ? 'error.main' : 'divider', borderRadius: 1 }}>
              <Editor
                height="100%"
                language="yaml"
                value={yamlContent}
                onChange={handleYamlChange}
                options={{
                  minimap: { enabled: false },
                  lineNumbers: 'on',
                  scrollBeyondLastLine: false,
                  fontSize: 13,
                }}
              />
            </Box>
          </Box>
        )}
      </DialogContent>

      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          onClick={handleSave}
          disabled={!!yamlError}
        >
          Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}

export default StageConfigDialog
