import axios, { AxiosInstance } from 'axios'
import { useAuthStore } from '../store/auth'

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api/v1'

class ApiService {
  private client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // 요청 인터셉터
    this.client.interceptors.request.use(
      (config) => {
        const token = useAuthStore.getState().token
        if (token) {
          config.headers.Authorization = `Bearer ${token}`
        }
        // 언어 설정 전달 (i18n)
        const lang = localStorage.getItem('i18nextLng') || 'ko'
        config.headers['Accept-Language'] = lang
        return config
      },
      (error) => Promise.reject(error)
    )

    // 응답 인터셉터
    this.client.interceptors.response.use(
      (response) => response,
      (error) => {
        if (error.response?.status === 401) {
          useAuthStore.getState().logout()
          window.location.href = '/login'
        }
        return Promise.reject(error)
      }
    )
  }

  // 범용 HTTP 메서드
  async get<T = unknown>(url: string, config?: { headers?: Record<string, string>; params?: Record<string, unknown> }) {
    const response = await this.client.get<T>(url, config)
    return response
  }

  async post<T = unknown>(url: string, data?: unknown) {
    const response = await this.client.post<T>(url, data)
    return response
  }

  async put<T = unknown>(url: string, data?: unknown) {
    const response = await this.client.put<T>(url, data)
    return response
  }

  async delete<T = unknown>(url: string) {
    const response = await this.client.delete<T>(url)
    return response
  }

  // 인증
  async getProviders() {
    const response = await this.client.get('/auth/providers')
    return response.data
  }

  async login(provider: string) {
    const response = await this.client.post('/auth/login', { provider })
    return response.data
  }

  async getCurrentUser() {
    const response = await this.client.get('/auth/me')
    return response.data
  }

  async logout() {
    const response = await this.client.post('/auth/logout')
    return response.data
  }

  // 파이프라인
  async getPipelines(page = 1, pageSize = 20) {
    const response = await this.client.get('/pipelines', {
      params: { page, page_size: pageSize },
    })
    return response.data
  }

  async getPipeline(id: string) {
    const response = await this.client.get(`/pipelines/${id}`)
    return response.data
  }

  async createPipeline(data: { name: string; description?: string; config_yaml: string }) {
    const response = await this.client.post('/pipelines', data)
    return response.data
  }

  async updatePipeline(id: string, data: { name?: string; description?: string; config_yaml?: string }) {
    const response = await this.client.put(`/pipelines/${id}`, data)
    return response.data
  }

  async deletePipeline(id: string) {
    const response = await this.client.delete(`/pipelines/${id}`)
    return response.data
  }

  // NOTE: 개별 파이프라인 실행 제어(start/stop/pause)는 지원하지 않음
  // 파이프라인 실행 제어는 Workflow 단위로만 가능

  async getPipelineStatus(id: string) {
    const response = await this.client.get(`/pipelines/${id}/status`)
    return response.data
  }

  async getPipelineHistory(id: string) {
    const response = await this.client.get(`/pipelines/${id}/history`)
    return response.data
  }

  async getPipelineMetrics(id: string) {
    const response = await this.client.get(`/pipelines/${id}/metrics`)
    return response.data
  }

  // 파이프라인 그래프
  async getPipelineGraph(id: string) {
    const response = await this.client.get(`/pipelines/${id}/graph`)
    return response.data
  }

  async updatePipelineGraph(id: string, data: {
    add_edges?: Array<{ id: string; source: string; target: string }>;
    remove_edges?: string[];
    update_nodes?: Array<{ id: string; position: { x: number; y: number } }>;
  }) {
    const response = await this.client.put(`/pipelines/${id}/graph`, data)
    return response.data
  }

  async getPipelineActorMetrics(id: string) {
    const response = await this.client.get(`/pipelines/${id}/actor-metrics`)
    return response.data
  }

  // 스케줄
  async getSchedules() {
    const response = await this.client.get('/schedules')
    return response.data
  }

  async createSchedule(data: { pipeline_id: string; cron_expression: string; enabled: boolean }) {
    const response = await this.client.post('/schedules', data)
    return response.data
  }

  // 에이전트
  async getAgents() {
    const response = await this.client.get('/agents')
    return response.data
  }

  async getAgentStatus(id: string) {
    const response = await this.client.get(`/agents/${id}/status`)
    return response.data
  }

  // 데이터 유형
  async getDataTypes(params?: { project_id?: string; category?: string }) {
    const response = await this.client.get('/data-types', { params })
    return response.data
  }

  async getProjectDataTypes(projectId: string, category?: string) {
    const params = category ? { category } : undefined
    const response = await this.client.get(`/projects/${projectId}/data-types`, { params })
    return response.data
  }

  async getDataType(id: string) {
    const response = await this.client.get(`/data-types/${id}`)
    return response.data
  }

  async createDataType(data: {
    project_id: string
    parent_id?: string | null
    name: string
    display_name: string
    description?: string
    category?: string
    delete_strategy?: unknown
    id_fields?: string[]
    schema?: unknown
    storage?: unknown
    preworks?: Array<{
      name: string
      description?: string
      type: string
      phase: string
      order?: number
      config: Record<string, unknown>
    }>
  }) {
    const response = await this.client.post('/data-types', data)
    return response.data
  }

  async updateDataType(id: string, data: {
    parent_id?: string | null
    name?: string
    display_name?: string
    description?: string
    category?: string
    delete_strategy?: unknown
    id_fields?: string[]
    schema?: unknown
    storage?: unknown
  }) {
    const response = await this.client.put(`/data-types/${id}`, data)
    return response.data
  }

  async deleteDataType(id: string) {
    const response = await this.client.delete(`/data-types/${id}`)
    return response.data
  }

  async getDataTypeCategories() {
    const response = await this.client.get('/data-types/categories')
    return response.data
  }

  // 데이터 유형 사전작업
  async addPrework(dataTypeId: string, data: {
    name: string
    description?: string
    type: string
    phase: string
    order?: number
    config: Record<string, unknown>
  }) {
    const response = await this.client.post(`/data-types/${dataTypeId}/preworks`, data)
    return response.data
  }

  async deletePrework(dataTypeId: string, preworkId: string) {
    const response = await this.client.delete(`/data-types/${dataTypeId}/preworks/${preworkId}`)
    return response.data
  }

  async executePrework(dataTypeId: string, preworkId: string) {
    const response = await this.client.post(`/data-types/${dataTypeId}/preworks/${preworkId}/execute`)
    return response.data
  }

  // 삭제 전략 프리셋
  async getDeleteStrategyPresets() {
    const response = await this.client.get('/delete-strategy-presets')
    return response.data
  }

  // 프로젝트
  async getProjects(params?: { page?: number; page_size?: number; search?: string; status?: string }) {
    const response = await this.client.get('/projects', { params })
    return response.data
  }

  async getProject(id: string) {
    const response = await this.client.get(`/projects/${id}`)
    return response.data
  }

  async createProject(data: {
    name: string
    alias: string
    description?: string
    owner_id?: string
    owner_ids?: string[]
    metadata?: string
    tags?: string
  }) {
    const response = await this.client.post('/projects', data)
    return response.data
  }

  async updateProject(id: string, data: {
    name?: string
    alias?: string
    description?: string
    status?: string
    owner_id?: string
    owner_ids?: string[]
    metadata?: string
    tags?: string
  }) {
    const response = await this.client.put(`/projects/${id}`, data)
    return response.data
  }

  async deleteProject(id: string) {
    const response = await this.client.delete(`/projects/${id}`)
    return response.data
  }

  async getProjectWorkflows(id: string) {
    const response = await this.client.get(`/projects/${id}/workflows`)
    return response.data
  }

  async getProjectHierarchy(id: string) {
    const response = await this.client.get(`/projects/${id}/hierarchy`)
    return response.data
  }

  // 워크플로우
  async getWorkflows(params?: { page?: number; page_size?: number; project_id?: string; type?: string }) {
    const response = await this.client.get('/workflows', { params })
    return response.data
  }

  async getWorkflow(id: string) {
    const response = await this.client.get(`/workflows/${id}`)
    return response.data
  }

  async createWorkflow(data: {
    project_id: string
    name: string
    slug?: string
    description?: string
    type: 'batch' | 'realtime'
    schedule_type?: string
    schedule_cron?: string
    schedule_enabled?: boolean
  }) {
    const response = await this.client.post('/workflows', data)
    return response.data
  }

  async updateWorkflow(id: string, data: {
    name?: string
    slug?: string
    description?: string
    type?: 'batch' | 'realtime'
    schedule_type?: string
    schedule_cron?: string
    schedule_enabled?: boolean
    pipelines?: Array<{
      id: string
      name: string
      description?: string
      priority: number
      depends_on?: string[]
      source: { type: string; name: string; config: Record<string, unknown> }
      transforms?: Array<{ name: string; type: string; config: Record<string, unknown> }>
      sinks: Array<{ type: string; name: string; config: Record<string, unknown>; condition?: string }>
      weight?: number
      parent_pipeline_id?: string | null
      target_data_type_id?: string | null
      expansion_mode?: string
      parameter_bindings?: Array<{ parent_field: string; child_param: string }>
    }>
  }) {
    const response = await this.client.put(`/workflows/${id}`, data)
    return response.data
  }

  async deleteWorkflow(id: string) {
    const response = await this.client.delete(`/workflows/${id}`)
    return response.data
  }

  async startWorkflow(id: string) {
    const response = await this.client.post(`/workflows/${id}/start`)
    return response.data
  }

  async stopWorkflow(id: string) {
    const response = await this.client.post(`/workflows/${id}/stop`)
    return response.data
  }

  // 사용자 관리
  async getUsers(params?: { page?: number; page_size?: number; search?: string; role?: string }) {
    const response = await this.client.get('/users', { params })
    return response.data
  }

  async getUser(id: string) {
    const response = await this.client.get(`/users/${id}`)
    return response.data
  }

  async updateUserRole(id: string, role: string) {
    const response = await this.client.put(`/users/${id}/role`, { role })
    return response.data
  }

  async getRoles() {
    const response = await this.client.get('/roles')
    return response.data
  }

  // 사용자 검색 (자동완성용)
  async searchUsers(query: string, limit = 10) {
    const response = await this.client.get('/users/search', {
      params: { q: query, limit },
    })
    return response.data
  }

  // 권한 관리
  async getPermissions(params?: { resource_type?: string; user_id?: string }) {
    const response = await this.client.get('/permissions', { params })
    return response.data
  }

  async createPermission(data: {
    resource_type: string
    resource_id: string
    user_id?: string
    role_id?: string
    actions: string[]
  }) {
    const response = await this.client.post('/permissions', data)
    return response.data
  }

  async deletePermission(id: string) {
    const response = await this.client.delete(`/permissions/${id}`)
    return response.data
  }
}

export const api = new ApiService()
