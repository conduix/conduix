import { api } from './api'
import type {
  Plugin,
  PluginBuild,
  StageListResponse,
  StageSchemaResponse,
  PluginCreateRequest,
  BuildPluginRequest,
  ValidateSourceResponse,
  ClusterAgent,
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

// Plugin Build API (V3)

export async function buildPlugin(req: BuildPluginRequest): Promise<PluginBuild> {
  const resp = await api.post<{ success: boolean; data: PluginBuild }>('/plugins/build', req)
  return resp.data.data
}

export async function getBuild(buildId: string): Promise<PluginBuild> {
  const resp = await api.get<{ success: boolean; data: PluginBuild }>(`/plugins/builds/${buildId}`)
  return resp.data.data
}

export async function getPluginBuilds(name: string): Promise<PluginBuild[]> {
  const resp = await api.get<{ success: boolean; data: PluginBuild[] }>(`/plugins/${name}/builds`)
  return resp.data?.data || []
}

export async function validateSource(sourceCode: string): Promise<ValidateSourceResponse> {
  const resp = await api.post<{ success: boolean; data: ValidateSourceResponse }>('/plugins/validate', { source_code: sourceCode })
  return resp.data.data
}

export function getPluginBinaryUrl(name: string, version?: string): string {
  const base = `/api/v1/plugins/${name}/binary`
  return version ? `${base}?version=${version}` : base
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

// Cluster Agent API

export async function getClusterAgents(clusterId: string): Promise<ClusterAgent[]> {
  const resp = await api.get<{ success: boolean; data: ClusterAgent[] }>(`/clusters/${clusterId}/agents`)
  return resp.data?.data || []
}
