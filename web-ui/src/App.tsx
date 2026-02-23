import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { ThemeProvider, createTheme, CssBaseline } from '@mui/material'
import { LocalizationProvider } from '@mui/x-date-pickers'
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs'
import { useTranslation } from 'react-i18next'
import { useMemo } from 'react'
import { useAuthStore } from './store/auth'
import { useThemeStore } from './store/theme'
import MainLayout from './components/Layout/MainLayout'
import LoginPage from './pages/Login'
import DashboardPage from './pages/Dashboard'
import PipelinesPage from './pages/Pipelines'
import PipelineDetailPage from './pages/PipelineDetail'
import WorkflowsPage from './pages/Workflows'
import SchedulesPage from './pages/Schedules'
import AgentsPage from './pages/Agents'
import ClustersPage from './pages/Clusters'
import HistoryPage from './pages/History'
import ProfilePage from './pages/Profile'
import UsersPage from './pages/Users'
import ProjectsPage from './pages/Projects'
import ProjectDetailPage from './pages/ProjectDetail'
import WorkflowDetailPage from './pages/WorkflowDetail'
import StageEditorPage from './pages/StageEditor'
import SourceEditorPage from './pages/SourceEditor'
import DataModelsPage from './pages/DataModels'
import DataModelDetailPage from './pages/DataModelDetail'
import 'dayjs/locale/ko'

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

function App() {
  const { i18n } = useTranslation()
  const mode = useThemeStore((state) => state.mode)

  const theme = useMemo(
    () =>
      createTheme({
        palette: {
          mode,
        },
        typography: {
          fontFamily: [
            '-apple-system',
            'BlinkMacSystemFont',
            '"Segoe UI"',
            'Roboto',
            '"Helvetica Neue"',
            'Arial',
            'sans-serif',
          ].join(','),
        },
        components: {
          MuiButton: {
            styleOverrides: {
              root: {
                textTransform: 'none',
              },
            },
          },
        },
      }),
    [mode]
  )

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={i18n.language === 'ko' ? 'ko' : 'en'}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />

            <Route
              path="/"
              element={
                <PrivateRoute>
                  <MainLayout />
                </PrivateRoute>
              }
            >
              <Route index element={<Navigate to="/dashboard" replace />} />
              <Route path="dashboard" element={<DashboardPage />} />
              <Route path="workflows" element={<WorkflowsPage />} />
              <Route path="pipelines" element={<PipelinesPage />} />
              <Route path="pipelines/:id" element={<PipelineDetailPage />} />
              <Route path="schedules" element={<SchedulesPage />} />
              <Route path="agents" element={<AgentsPage />} />
              <Route path="clusters" element={<ClustersPage />} />
              <Route path="history" element={<HistoryPage />} />
              <Route path="profile" element={<ProfilePage />} />
              <Route path="users" element={<UsersPage />} />
              <Route path="projects" element={<ProjectsPage />} />
              <Route path="projects/:id" element={<ProjectDetailPage />} />
              <Route path="workflows/:id" element={<WorkflowDetailPage />} />
              <Route path="projects/:projectAlias/workflows/:workflowId" element={<WorkflowDetailPage />} />
              <Route path="projects/:projectAlias/workflows/:workflowId/pipelines/:pipelineId/stages" element={<StageEditorPage />} />
              <Route path="projects/:projectAlias/workflows/:workflowId/pipelines/:pipelineId/source" element={<SourceEditorPage />} />
              <Route path="data-models" element={<DataModelsPage />} />
              <Route path="data-models/:id" element={<DataModelDetailPage />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </LocalizationProvider>
    </ThemeProvider>
  )
}

export default App
