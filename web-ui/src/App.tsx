import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import enUS from 'antd/locale/en_US'
import koKR from 'antd/locale/ko_KR'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from './store/auth'
import MainLayout from './components/Layout/MainLayout'
import LoginPage from './pages/Login'
import DashboardPage from './pages/Dashboard'
import PipelinesPage from './pages/Pipelines'
import PipelineDetailPage from './pages/PipelineDetail'
import SchedulesPage from './pages/Schedules'
import AgentsPage from './pages/Agents'
import HistoryPage from './pages/History'
import ProfilePage from './pages/Profile'
import UsersPage from './pages/Users'
import ProjectsPage from './pages/Projects'
import ProjectDetailPage from './pages/ProjectDetail'
import WorkflowDetailPage from './pages/WorkflowDetail'

const antdLocales: Record<string, typeof enUS> = {
  en: enUS,
  ko: koKR,
}

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

function App() {
  const { i18n } = useTranslation()
  const currentLocale = antdLocales[i18n.language] || enUS

  return (
    <ConfigProvider locale={currentLocale}>
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
            <Route path="pipelines" element={<PipelinesPage />} />
            <Route path="pipelines/:id" element={<PipelineDetailPage />} />
            <Route path="schedules" element={<SchedulesPage />} />
            <Route path="agents" element={<AgentsPage />} />
            <Route path="history" element={<HistoryPage />} />
            <Route path="profile" element={<ProfilePage />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="projects" element={<ProjectsPage />} />
            <Route path="projects/:id" element={<ProjectDetailPage />} />
            <Route path="workflows/:id" element={<WorkflowDetailPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  )
}

export default App
