import { api } from './api'
import type {
  Plugin,
  StageListResponse,
  StageSchemaResponse,
  PluginCreateRequest,
  ClusterAgent,
  TestNativePluginRequest,
  TestNativePluginResponse,
} from '../types/plugin'

// Plugin API

export async function getPlugins(): Promise<Plugin[]> {
  const resp = await api.get<{ success: boolean; data: Plugin[] }>('/plugins')
  return resp.data?.data || []
}

export async function getPlugin(name: string): Promise<Plugin> {
  const resp = await api.get<{ success: boolean; data: Plugin }>(`/plugins/${name}`)
  return resp.data.data
}

export async function createPlugin(req: PluginCreateRequest): Promise<Plugin> {
  const resp = await api.post<{ success: boolean; data: Plugin }>('/plugins', req)
  return resp.data.data
}

export async function updatePlugin(name: string, req: Partial<PluginCreateRequest>): Promise<Plugin> {
  const resp = await api.put<{ success: boolean; data: Plugin }>(`/plugins/${name}`, req)
  return resp.data.data
}

export async function deletePlugin(name: string): Promise<void> {
  await api.delete(`/plugins/${name}`)
}

// Script Test API

export interface TestScriptRequest {
  code: string
  timeout?: string
  sample_data: Record<string, unknown>
  plugin_name?: string
}

export interface TestScriptResponse {
  success: boolean
  output?: Record<string, unknown>
  dropped: boolean
  error?: string
  elapsed: string
}

export async function testScript(req: TestScriptRequest): Promise<TestScriptResponse> {
  const resp = await api.post<{ success: boolean; data: TestScriptResponse }>('/plugins/test-script', req)
  return resp.data.data
}

// Native Plugin Test API

export async function testNativePlugin(req: TestNativePluginRequest): Promise<TestNativePluginResponse> {
  const resp = await api.post<{ success: boolean; data: TestNativePluginResponse }>('/plugins/test-native', req)
  return resp.data.data
}

// Stage API

export async function getStages(): Promise<StageListResponse> {
  const resp = await api.get<{ success: boolean; data: StageListResponse }>('/stages')
  return resp.data.data
}

export async function getStageSchema(stageType: string): Promise<StageSchemaResponse> {
  const resp = await api.get<{ success: boolean; data: StageSchemaResponse }>(`/stages/${stageType}/schema`)
  return resp.data.data
}

// Runner API

export interface RunnerPluginStatus {
  id: string
  name: string
  source_hash: string
  deployed_hash: string
  needs_build: boolean
}

export interface RunnerVersion {
  id: string
  status: 'pending' | 'building' | 'ready' | 'failed'
  source_hash: string
  image_tag?: string
  build_log?: string
  error?: string
  duration_ms?: number
  created_by?: string
  started_at?: string
  finished_at?: string
}

export interface RunnerStatusResponse {
  needs_build: boolean
  plugins: RunnerPluginStatus[]
  latest_ready_version?: RunnerVersion
}

export async function getRunnerStatus(): Promise<RunnerStatusResponse> {
  const resp = await api.get<{ success: boolean } & RunnerStatusResponse>('/runner/status')
  return resp.data
}

export async function getRunnerVersions(): Promise<RunnerVersion[]> {
  const resp = await api.get<{ success: boolean; data: RunnerVersion[] }>('/runner/versions')
  return resp.data?.data || []
}

export async function startRunnerBuild(): Promise<void> {
  await api.post('/runner/build')
}

// Cluster Agent API

export async function getClusterAgents(clusterId: string): Promise<ClusterAgent[]> {
  const resp = await api.get<{ success: boolean; data: ClusterAgent[] }>(`/clusters/${clusterId}/agents`)
  return resp.data?.data || []
}
