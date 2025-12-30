import type { Node, Edge } from '@xyflow/react'

export type ActorType = 'supervisor' | 'source' | 'transform' | 'sink' | 'router'
export type ActorState = 'created' | 'starting' | 'running' | 'stopping' | 'stopped' | 'restarting' | 'failed'

export interface ActorMetrics {
  input_count: number
  output_count: number
  error_count: number
  throughput_per_sec: number
  state: ActorState
  last_updated: string
}

export interface GraphNodeData {
  actor_name: string
  actor_type: ActorType
  actor_config?: Record<string, unknown>
  parallelism?: number
  metrics?: ActorMetrics
  [key: string]: unknown  // Index signature for React Flow compatibility
}

export interface GraphPosition {
  x: number
  y: number
}

export interface GraphNode {
  id: string
  type: string
  label: string
  position: GraphPosition
  data: GraphNodeData
}

export interface GraphEdge {
  id: string
  source: string
  target: string
  label?: string
  animated?: boolean
}

export interface PipelineGraph {
  pipeline_id: string
  nodes: GraphNode[]
  edges: GraphEdge[]
  layout?: string
}

export interface GraphUpdateRequest {
  add_edges?: GraphEdge[]
  remove_edges?: string[]
  update_nodes?: Array<{
    id: string
    position: GraphPosition
  }>
}

// React Flow 호환 타입
export type ActorNode = Node<GraphNodeData>
export type PipelineEdge = Edge

// API 응답 타입
export interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

export type ActorMetricsMap = Record<string, ActorMetrics>
