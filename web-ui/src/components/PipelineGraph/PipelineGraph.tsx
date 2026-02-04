import { useCallback, useEffect, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  ConnectionMode,
  type Connection,
  type Edge,
  type Node,
  type NodeTypes,
  ReactFlowProvider,
  BackgroundVariant,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { CircularProgress, Alert, Box, Typography } from '@mui/material'

import { api } from '../../services/api'
import { ActorNode } from './nodes/ActorNode'
import { usePipelineMetrics } from '../../hooks/usePipelineMetrics'
import { useSnackbar } from '../../hooks/useSnackbar'
import type { PipelineGraph as PipelineGraphType, GraphNode, GraphEdge, ActorType, GraphNodeData } from '../../types/graph'

// React Flow node/edge types
type FlowNode = Node<GraphNodeData>
type FlowEdge = Edge

const nodeTypes: NodeTypes = {
  source: ActorNode,
  transform: ActorNode,
  sink: ActorNode,
  router: ActorNode,
  supervisor: ActorNode,
}

// 노드 색상 맵
const typeColors: Record<ActorType, string> = {
  source: '#52c41a',
  transform: '#1890ff',
  sink: '#722ed1',
  router: '#fa8c16',
  supervisor: '#13c2c2',
}

interface PipelineGraphProps {
  pipelineId: string
  readonly?: boolean
  pollingInterval?: number
}

function PipelineGraphInner({
  pipelineId,
  readonly = false,
  pollingInterval = 5000,
}: PipelineGraphProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const { showWarning, showSuccess, showError } = useSnackbar()

  // 메트릭 폴링
  const { metrics } = usePipelineMetrics(pipelineId, pollingInterval)

  // 초기 그래프 로드
  useEffect(() => {
    async function fetchGraph() {
      if (!pipelineId) return

      try {
        setLoading(true)
        setError(null)
        const response = await api.getPipelineGraph(pipelineId)

        if (response.success && response.data) {
          const graph: PipelineGraphType = response.data

          // React Flow 형식으로 변환
          const rfNodes = graph.nodes.map((node: GraphNode) => ({
            id: node.id,
            type: node.type,
            position: { x: node.position.x, y: node.position.y },
            data: node.data,
          }))

          const rfEdges = graph.edges.map((edge: GraphEdge) => ({
            id: edge.id,
            source: edge.source,
            target: edge.target,
            animated: edge.animated ?? true,
            style: { stroke: '#999' },
          }))

          setNodes(rfNodes)
          setEdges(rfEdges)
        } else {
          setError(response.error || 'Failed to load graph')
        }
      } catch (err) {
        console.error('Failed to load pipeline graph:', err)
        setError('Failed to load pipeline graph')
      } finally {
        setLoading(false)
      }
    }

    fetchGraph()
  }, [pipelineId, setNodes, setEdges])

  // 메트릭 업데이트 시 노드 갱신
  useEffect(() => {
    if (metrics && Object.keys(metrics).length > 0) {
      setNodes((nds) =>
        nds.map((node) => {
          const actorMetrics = metrics[node.id]
          if (actorMetrics) {
            return {
              ...node,
              data: {
                ...node.data,
                metrics: actorMetrics,
              },
            }
          }
          return node
        })
      )
    }
  }, [metrics, setNodes])

  // 연결 추가 핸들러
  const onConnect = useCallback(
    async (connection: Connection) => {
      if (readonly) {
        showWarning('Read-only mode')
        return
      }

      if (!connection.source || !connection.target) return

      try {
        const response = await api.updatePipelineGraph(pipelineId, {
          add_edges: [{
            id: `${connection.source}-${connection.target}`,
            source: connection.source,
            target: connection.target,
          }],
        })

        if (response.success) {
          setEdges((eds) => addEdge({
            ...connection,
            animated: true,
            style: { stroke: '#999' },
          }, eds))
          showSuccess('Connection added')
        } else {
          showError(response.error || 'Failed to add connection')
        }
      } catch (err) {
        console.error('Failed to add connection:', err)
        showError('Failed to add connection')
      }
    },
    [pipelineId, readonly, setEdges, showWarning, showSuccess, showError]
  )

  // 엣지 삭제 핸들러
  const onEdgesDelete = useCallback(
    async (deletedEdges: Edge[]) => {
      if (readonly) {
        showWarning('Read-only mode')
        return
      }

      try {
        const response = await api.updatePipelineGraph(pipelineId, {
          remove_edges: deletedEdges.map((e) => e.id),
        })

        if (!response.success) {
          showError(response.error || 'Failed to remove connection')
          // 실패 시 엣지 복구
          setEdges((eds) => [...eds, ...deletedEdges])
        } else {
          showSuccess('Connection removed')
        }
      } catch (err) {
        console.error('Failed to remove connection:', err)
        showError('Failed to remove connection')
        // 실패 시 엣지 복구
        setEdges((eds) => [...eds, ...deletedEdges])
      }
    },
    [pipelineId, readonly, setEdges, showWarning, showSuccess, showError]
  )

  // MiniMap 노드 색상
  const nodeColor = useCallback((node: { type?: string }) => {
    return typeColors[node.type as ActorType] || '#999'
  }, [])

  if (loading) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          alignItems: 'center',
          height: 500,
          bgcolor: 'grey.50',
          borderRadius: 2,
        }}
      >
        <CircularProgress size={40} />
        <Typography sx={{ mt: 2, color: 'text.secondary' }}>
          Loading pipeline graph...
        </Typography>
      </Box>
    )
  }

  if (error) {
    return (
      <Alert
        severity="error"
        sx={{ m: 2 }}
      >
        <Typography variant="subtitle2">Failed to load graph</Typography>
        <Typography variant="body2">{error}</Typography>
      </Alert>
    )
  }

  if (nodes.length === 0) {
    return (
      <Alert
        severity="info"
        sx={{ m: 2 }}
      >
        <Typography variant="subtitle2">No graph data</Typography>
        <Typography variant="body2">
          This pipeline has no actors configured. Please update the pipeline YAML configuration.
        </Typography>
      </Alert>
    )
  }

  return (
    <Box
      sx={{
        height: 600,
        border: 1,
        borderColor: 'divider',
        borderRadius: 2,
        bgcolor: 'background.paper',
      }}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onEdgesDelete={onEdgesDelete}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        connectionMode={ConnectionMode.Loose}
        snapToGrid
        snapGrid={[15, 15]}
        deleteKeyCode={readonly ? null : ['Backspace', 'Delete']}
        nodesConnectable={!readonly}
        nodesDraggable={!readonly}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
        <Controls />
        <MiniMap
          nodeColor={nodeColor}
          nodeStrokeWidth={3}
          zoomable
          pannable
        />
      </ReactFlow>
    </Box>
  )
}

// Provider로 감싸서 export
export function PipelineGraph(props: PipelineGraphProps) {
  return (
    <ReactFlowProvider>
      <PipelineGraphInner {...props} />
    </ReactFlowProvider>
  )
}

export default PipelineGraph
