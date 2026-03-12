// Plugin 플러그인 타입 정의

export interface Plugin {
  id: string
  name: string
  type?: 'script' | 'native'
  version: string
  description?: string
  source_repo?: string
  source_code?: string
  go_mod?: string
  source_hash?: string
  deployed_hash?: string
  last_test_passed?: boolean
  last_test_at?: string
  last_test_error?: string
  status: 'active' | 'inactive' | 'deprecated'
  created_by?: string
  created_at: string
  updated_at: string
}

export interface StageListResponse {
  builtin: StageInfo[]
}

export interface StageInfo {
  type: string
  displayName: string
  category: string
  description?: string
}

export interface StageSchemaResponse {
  type: string
  displayName: string
  configSchema: Record<string, unknown>
  uiSchema?: Record<string, unknown>
}

export interface PluginCreateRequest {
  name: string
  description?: string
  source_repo?: string
  source_code?: string
  go_mod?: string
}

// Native Plugin Test
export interface TestNativePluginRequest {
  source_code: string
  go_mod?: string
  config?: Record<string, unknown>
  sample_data: Record<string, unknown>[]
  plugin_name?: string
}

export interface SecurityCheckResult {
  passed: boolean
  errors?: string[]
  warnings?: string[]
}

export interface TestNativePluginResponse {
  success: boolean
  security_check?: SecurityCheckResult
  build_output?: string
  build_error?: string
  build_elapsed?: string
  exec_output?: Record<string, unknown>[]
  exec_error?: string
  exec_elapsed?: string
}

// ClusterMetrics 클러스터 메트릭
export interface ClusterMetrics {
  node_count: number
  cpu_capacity_millicores: number
  memory_capacity_bytes: number
  cpu_allocatable_millicores: number
  mem_allocatable_bytes: number
  total_pods: number
  running_pods: number
  pending_pods: number
  failed_pods: number
  collected_at: string
}

export interface ClusterAgent {
  id: string
  hostname: string
  status: string
  is_leader: boolean
  last_heartbeat?: string
  metrics?: string // JSON
  cluster_id: string
}
