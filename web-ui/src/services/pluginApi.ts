import { api } from './api'
import type {
  Plugin,
  StageListResponse,
  StageSchemaResponse,
  PluginCreateRequest,
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
