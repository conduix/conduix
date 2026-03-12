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
  stages?: PluginStage[]
}

export interface PluginStage {
  id: string
  plugin_id: string
  stage_type: string
  category?: string
  display_name?: string
  description?: string
  config_schema: Record<string, unknown> // JSON Schema
  ui_schema?: Record<string, unknown>
  icon?: string
  color?: string
  created_at: string
  updated_at: string
  plugin?: Plugin
}

export interface StageListResponse {
  builtin: StageInfo[]
  plugins: PluginStageInfo[]
}

export interface StageInfo {
  type: string
  displayName: string
  category: string
  description?: string
}

export interface PluginStageInfo extends StageInfo {
  pluginName: string
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
  stages: PluginStageCreate[]
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

export interface PluginStageCreate {
  type: string
  displayName: string
  category: string
  description?: string
  configSchema: Record<string, unknown>
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
