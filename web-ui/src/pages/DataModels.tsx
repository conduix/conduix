import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Typography,
  Chip,
  Card,
  CardContent,
  IconButton,
  TextField,
  InputAdornment,
  FormControl,
  Select,
  MenuItem,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  TablePagination,
  CircularProgress,
  Grid,
} from '@mui/material'
import {
  Delete as DeleteIcon,
  Visibility as VisibilityIcon,
  Storage as DatabaseIcon,
  Apps as AppstoreIcon,
  CheckCircle as CheckCircleIcon,
  Search as SearchIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { useSnackbar } from '../hooks/useSnackbar'
import { ConfirmDialog } from '../components/common/ConfirmDialog'

interface DataType {
  id: string
  project_id: string
  parent_id?: string | null
  name: string
  display_name: string
  description?: string
  category?: string
  id_fields?: string
  json_schema?: string
  schema?: {
    type?: string
    definition?: string
    fields?: Array<{ name: string; type: string; required?: boolean; description?: string }>
  }
  created_at: string
  updated_at: string
  project?: {
    id: string
    name: string
    alias: string
  }
  parent?: DataType
}

interface Project {
  id: string
  name: string
  alias: string
}

export default function DataModelsPage() {
  const { t } = useTranslation()
  const { showSuccess, showError } = useSnackbar()
  const [dataModels, setDataModels] = useState<DataType[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [pagination, setPagination] = useState({ page: 0, rowsPerPage: 20, total: 0 })
  const [filters, setFilters] = useState<{
    project_id?: string
    category?: string
    search?: string
  }>({})
  const [searchInput, setSearchInput] = useState('')
  const [deleteDialog, setDeleteDialog] = useState<{ open: boolean; id: string | null }>({
    open: false,
    id: null,
  })
  const navigate = useNavigate()

  const fetchDataModels = useCallback(async () => {
    try {
      setLoading(true)
      const response = await api.getDataTypes({
        project_id: filters.project_id,
        category: filters.category,
      })
      if (response.success) {
        let items = response.data || []
        // 검색어 필터링 (클라이언트 사이드)
        if (filters.search) {
          const searchLower = filters.search.toLowerCase()
          items = items.filter((item: DataType) =>
            item.name.toLowerCase().includes(searchLower) ||
            item.display_name.toLowerCase().includes(searchLower) ||
            (item.description || '').toLowerCase().includes(searchLower)
          )
        }
        setDataModels(items)
        setPagination(prev => ({ ...prev, total: items.length }))
      }
    } catch (error) {
      showError(t('dataModel.loadError'))
    } finally {
      setLoading(false)
    }
  }, [filters.project_id, filters.category, filters.search, showError, t])

  useEffect(() => {
    fetchDataModels()
    fetchProjects()
  }, [pagination.page, pagination.rowsPerPage, fetchDataModels])

  const fetchProjects = async () => {
    try {
      const response = await api.getProjects({ page: 1, page_size: 100 })
      if (response.success) {
        setProjects(response.data?.projects || [])
      }
    } catch (error) {
      console.error('Failed to load projects:', error)
    }
  }

  const handleDelete = async () => {
    if (!deleteDialog.id) return
    try {
      const response = await api.deleteDataType(deleteDialog.id)
      if (response.success) {
        showSuccess(t('dataModel.deleteSuccess'))
        fetchDataModels()
      } else {
        showError(response.error || t('dataModel.deleteError'))
      }
    } catch (error: unknown) {
      const errMsg = (error as { response?: { data?: { error?: string } } })?.response?.data?.error
      showError(errMsg || t('dataModel.deleteError'))
    } finally {
      setDeleteDialog({ open: false, id: null })
    }
  }

  const handleSearch = () => {
    setFilters(prev => ({ ...prev, search: searchInput }))
    setPagination(prev => ({ ...prev, page: 0 }))
  }

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSearch()
    }
  }

  const handleProjectChange = (value: string) => {
    setFilters(prev => ({ ...prev, project_id: value || undefined }))
    setPagination(prev => ({ ...prev, page: 0 }))
  }

  const handleCategoryChange = (value: string) => {
    setFilters(prev => ({ ...prev, category: value || undefined }))
    setPagination(prev => ({ ...prev, page: 0 }))
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

  const hasSchema = (dataModel: DataType): boolean => {
    return !!(dataModel.json_schema || (dataModel.schema?.fields && dataModel.schema.fields.length > 0))
  }

  // Calculate statistics
  const totalCount = dataModels.length
  const schemaDefinedCount = dataModels.filter(dm => hasSchema(dm)).length
  const categoryStats = dataModels.reduce((acc, dm) => {
    const cat = dm.category || 'unknown'
    acc[cat] = (acc[cat] || 0) + 1
    return acc
  }, {} as Record<string, number>)

  // Paginated data
  const paginatedData = dataModels.slice(
    pagination.page * pagination.rowsPerPage,
    pagination.page * pagination.rowsPerPage + pagination.rowsPerPage
  )

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5">{t('dataModel.list')}</Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <FormControl size="small" sx={{ minWidth: 180 }}>
            <Select
              displayEmpty
              value={filters.project_id || ''}
              onChange={(e) => handleProjectChange(e.target.value)}
            >
              <MenuItem value="">{t('dataModel.allProjects')}</MenuItem>
              {projects.map(p => (
                <MenuItem key={p.id} value={p.id}>{p.name}</MenuItem>
              ))}
            </Select>
          </FormControl>
          <FormControl size="small" sx={{ minWidth: 150 }}>
            <Select
              displayEmpty
              value={filters.category || ''}
              onChange={(e) => handleCategoryChange(e.target.value)}
            >
              <MenuItem value="">{t('dataModel.category')}</MenuItem>
              <MenuItem value="master">{t('dataModel.categories.master')}</MenuItem>
              <MenuItem value="transaction">{t('dataModel.categories.transaction')}</MenuItem>
              <MenuItem value="log">{t('dataModel.categories.log')}</MenuItem>
              <MenuItem value="metric">{t('dataModel.categories.metric')}</MenuItem>
              <MenuItem value="reference">{t('dataModel.categories.reference')}</MenuItem>
            </Select>
          </FormControl>
          <TextField
            size="small"
            placeholder={t('dataModel.searchPlaceholder')}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={handleSearchKeyDown}
            sx={{ width: 200 }}
            slotProps={{ input: {
              endAdornment: (
                <InputAdornment position="end">
                  <IconButton size="small" onClick={handleSearch}>
                    <SearchIcon />
                  </IconButton>
                </InputAdornment>
              ),
            } }}
          />
        </Box>
      </Box>
      <Grid container spacing={2} sx={{ mb: 2 }}>
        <Grid
          size={{
            xs: 12,
            md: 4
          }}>
          <Card>
            <CardContent sx={{ py: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <DatabaseIcon color="primary" />
                <Box>
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>
                    {t('dataModel.totalDataModels')}
                  </Typography>
                  <Typography variant="h5">{totalCount}</Typography>
                </Box>
              </Box>
            </CardContent>
          </Card>
        </Grid>
        <Grid
          size={{
            xs: 12,
            md: 4
          }}>
          <Card>
            <CardContent sx={{ py: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <CheckCircleIcon sx={{ color: 'success.main' }} />
                <Box>
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>
                    {t('dataModel.schemaDefined')}
                  </Typography>
                  <Typography variant="h5" sx={{ color: 'success.main' }}>
                    {schemaDefinedCount}
                    <Typography component="span" variant="body2" sx={{
                      color: "text.secondary"
                    }}>
                      {' '}/ {totalCount}
                    </Typography>
                  </Typography>
                </Box>
              </Box>
            </CardContent>
          </Card>
        </Grid>
        <Grid
          size={{
            xs: 12,
            md: 4
          }}>
          <Card>
            <CardContent sx={{ py: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <AppstoreIcon color="primary" />
                <Box>
                  <Typography variant="body2" sx={{
                    color: "text.secondary"
                  }}>
                    {t('dataModel.byCategory')}
                  </Typography>
                  <Typography variant="h5">{Object.keys(categoryStats).length}</Typography>
                </Box>
              </Box>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
      <TableContainer component={Paper}>
        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
            <CircularProgress />
          </Box>
        ) : (
          <>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>{t('dataModel.name')}</TableCell>
                  <TableCell sx={{ width: 130 }}>{t('dataModel.category')}</TableCell>
                  <TableCell sx={{ width: 150 }}>{t('dataModel.project')}</TableCell>
                  <TableCell sx={{ width: 120 }} align="center">{t('dataModel.schemaStatus')}</TableCell>
                  <TableCell sx={{ width: 120 }}>{t('common.updatedAt')}</TableCell>
                  <TableCell sx={{ width: 100 }}>{t('common.actions')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {paginatedData.map((record) => (
                  <TableRow key={record.id} hover>
                    <TableCell>
                      <Box
                        component="a"
                        onClick={() => navigate(`/data-models/${record.id}`)}
                        sx={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 1,
                          cursor: 'pointer',
                          color: 'primary.main',
                          textDecoration: 'none',
                          '&:hover': { textDecoration: 'underline' },
                        }}
                      >
                        <DatabaseIcon fontSize="small" />
                        <span>{record.display_name}</span>
                        <Typography
                          component="code"
                          sx={{ fontSize: 12, color: 'text.secondary' }}
                        >
                          {record.name}
                        </Typography>
                      </Box>
                    </TableCell>
                    <TableCell>
                      {(() => {
                        const config = getCategoryConfig(record.category)
                        return <Chip label={config.text} color={config.color} size="small" />
                      })()}
                    </TableCell>
                    <TableCell>{record.project?.name || '-'}</TableCell>
                    <TableCell align="center">
                      {hasSchema(record) ? (
                        <Chip
                          label={t('dataModel.schemaDefined')}
                          color="success"
                          size="small"
                          variant="outlined"
                        />
                      ) : (
                        <Chip
                          label={t('dataModel.schemaUndefined')}
                          size="small"
                          variant="outlined"
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      {new Date(record.updated_at).toLocaleDateString()}
                    </TableCell>
                    <TableCell>
                      <Box sx={{ display: 'flex', gap: 0.5 }}>
                        <IconButton
                          size="small"
                          onClick={() => navigate(`/data-models/${record.id}`)}
                          title={t('dataModel.viewDetail')}
                        >
                          <VisibilityIcon fontSize="small" />
                        </IconButton>
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() => setDeleteDialog({ open: true, id: record.id })}
                          title={t('common.delete')}
                        >
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
                {paginatedData.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} align="center" sx={{ py: 4 }}>
                      <Typography sx={{
                        color: "text.secondary"
                      }}>
                        {t('common.noData')}
                      </Typography>
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
            <TablePagination
              component="div"
              count={pagination.total}
              page={pagination.page}
              onPageChange={(_, newPage) => setPagination(prev => ({ ...prev, page: newPage }))}
              rowsPerPage={pagination.rowsPerPage}
              onRowsPerPageChange={(e) => setPagination(prev => ({
                ...prev,
                rowsPerPage: parseInt(e.target.value, 10),
                page: 0,
              }))}
              labelRowsPerPage={t('common.rowsPerPage')}
              labelDisplayedRows={({ from, to, count }) =>
                t('common.displayedRows', { from, to, count })
              }
            />
          </>
        )}
      </TableContainer>
      <ConfirmDialog
        open={deleteDialog.open}
        title={t('dataModel.deleteConfirm')}
        message={t('dataModel.deleteWarning')}
        confirmText={t('common.delete')}
        onConfirm={handleDelete}
        onCancel={() => setDeleteDialog({ open: false, id: null })}
      />
    </Box>
  );
}
