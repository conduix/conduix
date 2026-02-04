import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  Button,
  Chip,
  Tabs,
  Tab,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
} from '@mui/material'
import {
  ArrowBack as ArrowBackIcon,
  AccountTree as AccountTreeIcon,
} from '@mui/icons-material'
import { api } from '../services/api'
import { PipelineGraph } from '../components/PipelineGraph'
import { useSnackbar } from '../hooks/useSnackbar'

interface Pipeline {
  id: string
  name: string
  description?: string
  config?: Record<string, unknown>
  config_yaml?: string
  status?: string
  created_at: string
  updated_at: string
}

interface PipelineHistory {
  id: string
  pipeline_id: string
  status: string
  started_at: string
  finished_at?: string
  error?: string
  processed_count?: number
  error_count?: number
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
      id={`pipeline-tabpanel-${index}`}
      aria-labelledby={`pipeline-tab-${index}`}
      {...other}
    >
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  )
}

export default function PipelineDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { showError } = useSnackbar()
  const [pipeline, setPipeline] = useState<Pipeline | null>(null)
  const [history, setHistory] = useState<PipelineHistory[]>([])
  const [loading, setLoading] = useState(true)
  const [tabValue, setTabValue] = useState(0)

  const fetchPipeline = useCallback(async () => {
    try {
      const response = await api.getPipeline(id!)
      if (response.success) {
        setPipeline(response.data)
      }
    } catch (error) {
      showError('파이프라인 정보를 불러오는데 실패했습니다')
    } finally {
      setLoading(false)
    }
  }, [id, showError])

  const fetchHistory = useCallback(async () => {
    try {
      const response = await api.getPipelineHistory(id!)
      if (response.success) {
        setHistory(response.data || [])
      }
    } catch (error) {
      // 히스토리가 없을 수 있음
    }
  }, [id])

  useEffect(() => {
    if (id) {
      fetchPipeline()
      fetchHistory()
    }
  }, [id, fetchPipeline, fetchHistory])

  // NOTE: 개별 파이프라인 실행 제어(start/stop/pause)는 지원하지 않음
  // 파이프라인 실행 제어는 PipelineGroup 단위로만 가능

  const getStatusColor = (status: string): 'success' | 'primary' | 'error' | 'warning' | 'default' => {
    const colors: Record<string, 'success' | 'primary' | 'error' | 'warning' | 'default'> = {
      running: 'success',
      completed: 'primary',
      failed: 'error',
      paused: 'warning',
    }
    return colors[status] || 'default'
  }

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue)
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress />
      </Box>
    )
  }

  if (!pipeline) {
    return (
      <Box sx={{ textAlign: 'center', py: 6 }}>
        <Typography>파이프라인을 찾을 수 없습니다.</Typography>
      </Box>
    )
  }

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button
            startIcon={<ArrowBackIcon />}
            onClick={() => navigate('/pipelines')}
          >
            목록
          </Button>
          <Typography variant="h5">{pipeline.name}</Typography>
        </Box>
      </Box>

      <Card>
        <CardContent>
          <Tabs value={tabValue} onChange={handleTabChange}>
            <Tab label="정보" />
            <Tab label="설정" />
            <Tab label="실행 히스토리" />
            <Tab
              icon={<AccountTreeIcon sx={{ mr: 0.5 }} />}
              iconPosition="start"
              label="Pipeline Graph"
            />
          </Tabs>

          <TabPanel value={tabValue} index={0}>
            <TableContainer component={Paper} variant="outlined">
              <Table>
                <TableBody>
                  <TableRow>
                    <TableCell component="th" sx={{ width: 150, bgcolor: 'grey.50' }}>ID</TableCell>
                    <TableCell>{pipeline.id}</TableCell>
                    <TableCell component="th" sx={{ width: 150, bgcolor: 'grey.50' }}>이름</TableCell>
                    <TableCell>{pipeline.name}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>설명</TableCell>
                    <TableCell colSpan={3}>{pipeline.description || '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>생성일</TableCell>
                    <TableCell>{new Date(pipeline.created_at).toLocaleString()}</TableCell>
                    <TableCell component="th" sx={{ bgcolor: 'grey.50' }}>수정일</TableCell>
                    <TableCell>{new Date(pipeline.updated_at).toLocaleString()}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </TableContainer>
          </TabPanel>

          <TabPanel value={tabValue} index={1}>
            <Paper
              variant="outlined"
              sx={{
                p: 2,
                bgcolor: 'grey.50',
                borderRadius: 2,
                overflow: 'auto',
                maxHeight: 500,
              }}
            >
              <pre style={{ margin: 0, fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
                {pipeline.config_yaml}
              </pre>
            </Paper>
          </TabPanel>

          <TabPanel value={tabValue} index={2}>
            <TableContainer component={Paper} variant="outlined">
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>실행 ID</TableCell>
                    <TableCell>상태</TableCell>
                    <TableCell align="right">처리량</TableCell>
                    <TableCell align="right">에러</TableCell>
                    <TableCell>시작 시간</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {history.length > 0 ? (
                    history.map((row) => (
                      <TableRow key={row.id}>
                        <TableCell sx={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {row.id}
                        </TableCell>
                        <TableCell>
                          <Chip
                            label={row.status}
                            color={getStatusColor(row.status)}
                            size="small"
                          />
                        </TableCell>
                        <TableCell align="right">{row.processed_count?.toLocaleString() || 0}</TableCell>
                        <TableCell align="right">{row.error_count?.toLocaleString() || 0}</TableCell>
                        <TableCell>
                          {row.started_at ? new Date(row.started_at).toLocaleString() : '-'}
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={5} align="center" sx={{ py: 4 }}>
                        <Typography color="text.secondary">실행 히스토리가 없습니다.</Typography>
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </TableContainer>
          </TabPanel>

          <TabPanel value={tabValue} index={3}>
            <PipelineGraph
              pipelineId={id!}
              readonly={false}
              pollingInterval={5000}
            />
          </TabPanel>
        </CardContent>
      </Card>
    </Box>
  )
}
