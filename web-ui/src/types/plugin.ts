// Plugin 플러그인 타입 정의

export interface Plugin {
  id: string
  name: string
  version: string
  image: string
  description?: string
  source_repo?: string
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
  pluginImage: string
}

export interface StageSchemaResponse {
  type: string
  displayName: string
  pluginImage?: string
  configSchema: Record<string, unknown>
  uiSchema?: Record<string, unknown>
}

export interface PluginCreateRequest {
  name: string
  version: string
  image: string
  description?: string
  source_repo?: string
  stages: PluginStageCreate[]
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
  runner_jobs: number
  runner_deployments: number
  runner_pods: number
  collected_at: string
}

// V3 Plugin Build types
export interface PluginBuild {
  id: string
  plugin_id: string
  status: 'pending' | 'building' | 'success' | 'failed'
  source_code: string
  go_mod?: string
  build_log?: string
  error?: string
  duration_ms?: number
  version?: string
  platform: string
  created_by?: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface PluginBinary {
  id: string
  plugin_id: string
  version: string
  platform: string
  checksum: string
  size_bytes: number
  build_id?: string
  created_at: string
}

export interface BuildPluginRequest {
  name: string
  version: string
  source_code: string
  go_mod?: string
  platform?: string
}

export interface ValidateSourceResponse {
  valid: boolean
  imports: string[]
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
