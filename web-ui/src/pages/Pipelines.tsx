import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Button,
  Typography,
  Chip,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
} from '@mui/material'
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid'
import {
  Add as AddIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
} from '@mui/icons-material'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import { ConfirmDialog } from '../components/common/ConfirmDialog'

interface Pipeline {
  id: string
  name: string
  description: string
  config_yaml: string
  created_at: string
  updated_at: string
  status?: string
}

const defaultConfigYaml = `version: "1.0"
name: "new-pipeline"
type: flat

sources:
  demo_source:
    type: demo
    interval: 1s

transforms:
  parse:
    type: remap
    inputs:
      - demo_source

sinks:
  console:
    type: console
    inputs:
      - parse

checkpoint:
  enabled: true
  storage: redis
  interval: 10s
`

export default function PipelinesPage() {
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPipeline, setEditingPipeline] = useState<Pipeline | null>(null)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    description: '',
    config_yaml: defaultConfigYaml,
  })
  const navigate = useNavigate()
  const { showSuccess, showError } = useSnackbar()

  const fetchPipelines = useCallback(async () => {
    try {
      setLoading(true)
      const response = await api.getPipelines()
      if (response.success) {
        setPipelines(response.data.items || [])
      }
    } catch (error) {
      showError('파이프라인 목록을 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }, [showError])

  useEffect(() => {
    fetchPipelines()
  }, [fetchPipelines])

  const handleCreate = () => {
    setEditingPipeline(null)
    setFormData({ name: '', description: '', config_yaml: defaultConfigYaml })
    setModalOpen(true)
  }

  const handleEdit = (pipeline: Pipeline) => {
    setEditingPipeline(pipeline)
    setFormData({
      name: pipeline.name,
      description: pipeline.description,
      config_yaml: pipeline.config_yaml,
    })
    setModalOpen(true)
  }

  const handleDeleteClick = (id: string) => {
    setDeletingId(id)
    setDeleteConfirmOpen(true)
  }

  const handleDeleteConfirm = async () => {
    if (!deletingId) return
    try {
      await api.deletePipeline(deletingId)
      showSuccess('파이프라인이 삭제되었습니다')
      fetchPipelines()
    } catch (error) {
      showError('파이프라인 삭제에 실패했습니다')
    } finally {
      setDeleteConfirmOpen(false)
      setDeletingId(null)
    }
  }

  const handleSubmit = async () => {
    try {
      if (editingPipeline) {
        await api.updatePipeline(editingPipeline.id, formData)
        showSuccess('파이프라인이 수정되었습니다')
      } else {
        await api.createPipeline(formData)
        showSuccess('파이프라인이 생성되었습니다')
      }
      setModalOpen(false)
      fetchPipelines()
    } catch (error) {
      showError('저장에 실패했습니다')
    }
  }

  const columns: GridColDef[] = [
    {
      field: 'name',
      headerName: '이름',
      flex: 1,
      renderCell: (params: GridRenderCellParams<Pipeline>) => (
        <Typography
          component="a"
          sx={{ cursor: 'pointer', color: 'primary.main', textDecoration: 'none' }}
          onClick={() => navigate(`/pipelines/${params.row.id}`)}
        >
          {params.value}
        </Typography>
      ),
    },
    {
      field: 'description',
      headerName: '설명',
      flex: 1,
    },
    {
      field: 'status',
      headerName: '상태',
      width: 120,
      renderCell: (params) => {
        const statusConfig: Record<string, { color: 'success' | 'warning' | 'error' | 'info' | 'default'; text: string }> = {
          running: { color: 'success', text: '실행 중' },
          paused: { color: 'warning', text: '일시중지' },
          stopped: { color: 'default', text: '중지됨' },
          failed: { color: 'error', text: '실패' },
          pending: { color: 'info', text: '대기 중' },
        }
        const config = statusConfig[params.value || 'stopped'] || statusConfig.stopped
        return <Chip label={config.text} color={config.color} size="small" />
      },
    },
    {
      field: 'created_at',
      headerName: '생성일',
      width: 120,
      valueFormatter: ({ value }) => new Date(value).toLocaleDateString(),
    },
    {
      field: 'actions',
      headerName: '액션',
      width: 120,
      sortable: false,
      renderCell: (params: GridRenderCellParams<Pipeline>) => (
        <Box>
          <IconButton size="small" onClick={() => handleEdit(params.row)} title="수정">
            <EditIcon fontSize="small" />
          </IconButton>
          <IconButton
            size="small"
            color="error"
            onClick={() => handleDeleteClick(params.row.id)}
            title="삭제"
          >
            <DeleteIcon fontSize="small" />
          </IconButton>
        </Box>
      ),
    },
  ]

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5">파이프라인</Typography>
        <Button variant="contained" startIcon={<AddIcon />} onClick={handleCreate}>
          새 파이프라인
        </Button>
      </Box>

      <DataGrid
        rows={pipelines}
        columns={columns}
        loading={loading}
        autoHeight
        pageSizeOptions={[10, 25, 50]}
        initialState={{
          pagination: { paginationModel: { pageSize: 10 } },
        }}
        disableRowSelectionOnClick
      />

      <Dialog open={modalOpen} onClose={() => setModalOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{editingPipeline ? '파이프라인 수정' : '새 파이프라인'}</DialogTitle>
        <DialogContent>
          <TextField
            label="이름"
            fullWidth
            required
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            sx={{ mt: 2, mb: 2 }}
          />
          <TextField
            label="설명"
            fullWidth
            multiline
            rows={2}
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            sx={{ mb: 2 }}
          />
          <TextField
            label="설정 (YAML)"
            fullWidth
            required
            multiline
            rows={15}
            value={formData.config_yaml}
            onChange={(e) => setFormData({ ...formData, config_yaml: e.target.value })}
            placeholder="파이프라인 설정을 YAML 형식으로 입력하세요"
            sx={{
              '& .MuiInputBase-input': {
                fontFamily: 'monospace',
              },
            }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setModalOpen(false)}>취소</Button>
          <Button variant="contained" onClick={handleSubmit}>
            저장
          </Button>
        </DialogActions>
      </Dialog>

      <ConfirmDialog
        open={deleteConfirmOpen}
        title="삭제 확인"
        message="정말 삭제하시겠습니까?"
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteConfirmOpen(false)}
      />
    </Box>
  )
}
