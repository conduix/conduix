import { useCallback, useMemo, useState, useRef, DragEvent } from 'react'
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
  Panel,
  useReactFlow,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {
  Box,
  Paper,
  Typography,
  IconButton,
  Tooltip,
} from '@mui/material'
import {
  Add as AddIcon,
  Download as DownloadIcon,
  Upload as UploadIcon,
} from '@mui/icons-material'
import dagre from 'dagre'
import yaml from 'js-yaml'

import { InputNode } from './nodes/InputNode'
import { StageNode } from './nodes/StageNode'
import { OutputNode } from './nodes/OutputNode'
import { StagePanel } from './StagePanel'
import { StageConfigDialog } from './StageConfigDialog'
import type { WorkflowPipeline, WorkflowInput, Stage, Output } from '../../types/pipeline'

const nodeTypes: NodeTypes = {
  input: InputNode,
  stage: StageNode,
  output: OutputNode,
}

// Node colors by type
const nodeColors: Record<string, string> = {
  input: '#52c41a',
  stage: '#1890ff',
  output: '#722ed1',
}

// Stage type icons/labels
const stageTypes = [
  { type: 'filter', label: 'Filter', description: 'Filter records by condition' },
  { type: 'remap', label: 'Remap', description: 'Transform/rename fields' },
  { type: 'enrich', label: 'Enrich', description: 'Add external data' },
  { type: 'aggregate', label: 'Aggregate', description: 'Aggregate records' },
  { type: 'router', label: 'Router', description: 'Route to outputs' },
  { type: 'validate', label: 'Validate', description: 'Validate against schema' },
  { type: 'dedupe', label: 'Dedupe', description: 'Remove duplicates' },
  { type: 'throttle', label: 'Throttle', description: 'Rate limiting' },
  { type: 'sub_pipeline', label: 'Sub-Pipeline', description: 'Nested pipeline' },
  { type: 'batch_lookup', label: 'Batch Lookup', description: 'Batch lookup enrichment' },
  { type: 'async_enrich', label: 'Async Enrich', description: 'Async enrichment' },
  { type: 'es_lookup', label: 'ES Lookup', description: 'Elasticsearch lookup' },
]

interface VisualPipelineBuilderProps {
  pipeline: WorkflowPipeline
  onChange: (pipeline: WorkflowPipeline) => void
  readonly?: boolean
}

// Convert pipeline to React Flow nodes/edges
function pipelineToFlow(pipeline: WorkflowPipeline): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []
  const yOffset = 0
  const xSpacing = 300
  const ySpacing = 120

  // Input node
  if (pipeline.input) {
    nodes.push({
      id: 'input',
      type: 'input',
      position: { x: 0, y: yOffset },
      data: {
        label: pipeline.input.name || 'Input',
        inputType: pipeline.input.type,
        config: pipeline.input,
      },
    })
  }

  // Stage nodes
  if (pipeline.stages && pipeline.stages.length > 0) {
    pipeline.stages.forEach((stage, idx) => {
      const nodeId = `stage-${stage.id || idx}`
      nodes.push({
        id: nodeId,
        type: 'stage',
        position: { x: xSpacing, y: yOffset + idx * ySpacing },
        data: {
          label: stage.name || `Stage ${idx + 1}`,
          stageType: stage.type,
          config: stage,
          index: idx,
        },
      })

      // Edge from input to first stage
      if (idx === 0 && pipeline.input) {
        edges.push({
          id: `input-${nodeId}`,
          source: 'input',
          target: nodeId,
          animated: true,
        })
      }

      // Edge from previous stage
      if (idx > 0) {
        const prevNodeId = `stage-${pipeline.stages![idx - 1].id || idx - 1}`
        edges.push({
          id: `${prevNodeId}-${nodeId}`,
          source: prevNodeId,
          target: nodeId,
          animated: true,
        })
      }
    })
  }

  // Output nodes
  if (pipeline.outputs && pipeline.outputs.length > 0) {
    const lastStageIdx = (pipeline.stages?.length || 0) - 1
    const lastStageId = lastStageIdx >= 0 ? `stage-${pipeline.stages![lastStageIdx].id || lastStageIdx}` : 'input'

    pipeline.outputs.forEach((output, idx) => {
      const nodeId = `output-${output.id || idx}`
      nodes.push({
        id: nodeId,
        type: 'output',
        position: { x: xSpacing * 2, y: yOffset + idx * ySpacing },
        data: {
          label: output.name || `Output ${idx + 1}`,
          outputType: output.type,
          config: output,
          index: idx,
        },
      })

      // Edge from last stage to output
      edges.push({
        id: `${lastStageId}-${nodeId}`,
        source: lastStageId,
        target: nodeId,
        animated: true,
      })
    })
  }

  // Auto-layout with dagre
  const dagreGraph = new dagre.graphlib.Graph()
  dagreGraph.setDefaultEdgeLabel(() => ({}))
  dagreGraph.setGraph({ rankdir: 'LR', nodesep: 80, ranksep: 150 })

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: 180, height: 60 })
  })
  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target)
  })

  dagre.layout(dagreGraph)

  // Apply dagre positions
  nodes.forEach((node) => {
    const nodeWithPosition = dagreGraph.node(node.id)
    node.position = {
      x: nodeWithPosition.x - 90,
      y: nodeWithPosition.y - 30,
    }
  })

  return { nodes, edges }
}

function VisualPipelineBuilderInner({
  pipeline,
  onChange,
  readonly = false,
}: VisualPipelineBuilderProps) {
  const reactFlowInstance = useReactFlow()
  const reactFlowWrapper = useRef<HTMLDivElement>(null)

  const initialFlow = useMemo(() => pipelineToFlow(pipeline), [pipeline])
  const [nodes, setNodes, onNodesChange] = useNodesState(initialFlow.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialFlow.edges)

  const [selectedNode, setSelectedNode] = useState<Node | null>(null)
  const [configDialogOpen, setConfigDialogOpen] = useState(false)
  const [, setYamlDialogOpen] = useState(false)

  // Handle connection
  const onConnect = useCallback(
    (connection: Connection) => {
      if (readonly) return
      setEdges((eds) => addEdge({ ...connection, animated: true }, eds))
    },
    [readonly, setEdges]
  )

  // Handle node selection
  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    setSelectedNode(node)
    if (!readonly) {
      setConfigDialogOpen(true)
    }
  }, [readonly])

  // Handle node deletion
  const onNodesDelete = useCallback(
    (deletedNodes: Node[]) => {
      if (readonly) return

      // Update pipeline based on deleted nodes
      const newPipeline = { ...pipeline }

      deletedNodes.forEach((node) => {
        if (node.type === 'stage' && newPipeline.stages) {
          const index = node.data.index as number
          newPipeline.stages = newPipeline.stages.filter((_, i) => i !== index)
        } else if (node.type === 'output' && newPipeline.outputs) {
          const index = node.data.index as number
          newPipeline.outputs = newPipeline.outputs.filter((_, i) => i !== index)
        }
      })

      onChange(newPipeline)
    },
    [pipeline, onChange, readonly]
  )

  // Handle drag over
  const onDragOver = useCallback((event: DragEvent) => {
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
  }, [])

  // Handle drop (add new stage)
  const onDrop = useCallback(
    (event: DragEvent) => {
      event.preventDefault()
      if (readonly) return

      const type = event.dataTransfer.getData('application/reactflow')
      if (!type) return

      const position = reactFlowInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      })

      // Create new stage
      const newStage: Stage = {
        id: `stage-${Date.now()}`,
        name: `New ${type}`,
        type: type as Stage['type'],
        config: {},
      }

      // Add to pipeline
      const newPipeline = {
        ...pipeline,
        stages: [...(pipeline.stages || []), newStage],
      }
      onChange(newPipeline)

      // Add node to flow
      const newNode: Node = {
        id: newStage.id!,
        type: 'stage',
        position,
        data: {
          label: newStage.name,
          stageType: newStage.type,
          config: newStage,
          index: (pipeline.stages?.length || 0),
        },
      }
      setNodes((nds) => [...nds, newNode])
    },
    [reactFlowInstance, pipeline, onChange, readonly, setNodes]
  )

  // Handle config save
  const handleConfigSave = useCallback(
    (config: Stage | Output | WorkflowInput) => {
      if (!selectedNode) return

      const newPipeline = { ...pipeline }

      if (selectedNode.type === 'stage' && newPipeline.stages) {
        const index = selectedNode.data.index as number
        newPipeline.stages = [...newPipeline.stages]
        newPipeline.stages[index] = config as Stage
      } else if (selectedNode.type === 'output' && newPipeline.outputs) {
        const index = selectedNode.data.index as number
        newPipeline.outputs = [...newPipeline.outputs]
        newPipeline.outputs[index] = config as Output
      } else if (selectedNode.type === 'input') {
        newPipeline.input = config as WorkflowInput
      }

      onChange(newPipeline)
      setConfigDialogOpen(false)
      setSelectedNode(null)
    },
    [selectedNode, pipeline, onChange]
  )

  // Add new output
  const handleAddOutput = useCallback(() => {
    if (readonly) return

    const newOutput: Output = {
      id: `output-${Date.now()}`,
      name: `New Output`,
      type: 'elasticsearch',
      config: {},
    }

    const newPipeline = {
      ...pipeline,
      outputs: [...(pipeline.outputs || []), newOutput],
    }
    onChange(newPipeline)
  }, [pipeline, onChange, readonly])

  // Export to YAML
  const handleExportYaml = useCallback(() => {
    const yamlContent = yaml.dump(pipeline, { indent: 2 })
    const blob = new Blob([yamlContent], { type: 'text/yaml' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${pipeline.name || 'pipeline'}.yaml`
    a.click()
    URL.revokeObjectURL(url)
  }, [pipeline])

  // MiniMap node color
  const nodeColor = useCallback((node: { type?: string }) => {
    return nodeColors[node.type || 'stage'] || '#999'
  }, [])

  return (
    <Box sx={{ display: 'flex', height: '100%' }}>
      {/* Stage Panel (Drag source) */}
      {!readonly && (
        <StagePanel stageTypes={stageTypes} />
      )}
      {/* Flow Canvas */}
      <Box
        ref={reactFlowWrapper}
        sx={{ flex: 1, height: '100%' }}
        onDragOver={onDragOver}
        onDrop={onDrop}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onNodeClick={onNodeClick}
          onNodesDelete={onNodesDelete}
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

          {/* Toolbar Panel */}
          <Panel position="top-right">
            <Paper sx={{ p: 1, display: 'flex', gap: 1 }}>
              {!readonly && (
                <Tooltip title="Add Output">
                  <IconButton size="small" onClick={handleAddOutput}>
                    <AddIcon />
                  </IconButton>
                </Tooltip>
              )}
              <Tooltip title="Export YAML">
                <IconButton size="small" onClick={handleExportYaml}>
                  <DownloadIcon />
                </IconButton>
              </Tooltip>
              <Tooltip title="Import YAML">
                <IconButton size="small" onClick={() => setYamlDialogOpen(true)}>
                  <UploadIcon />
                </IconButton>
              </Tooltip>
            </Paper>
          </Panel>

          {/* Info Panel */}
          <Panel position="bottom-left">
            <Paper sx={{ p: 1 }}>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>
                {nodes.length} nodes, {edges.length} connections
              </Typography>
            </Paper>
          </Panel>
        </ReactFlow>
      </Box>
      {/* Config Dialog */}
      {selectedNode && (
        <StageConfigDialog
          open={configDialogOpen}
          onClose={() => {
            setConfigDialogOpen(false)
            setSelectedNode(null)
          }}
          nodeType={selectedNode.type || 'stage'}
          config={selectedNode.data.config as Stage | Output | WorkflowInput}
          onSave={handleConfigSave}
        />
      )}
    </Box>
  );
}

export function VisualPipelineBuilder(props: VisualPipelineBuilderProps) {
  return (
    <ReactFlowProvider>
      <VisualPipelineBuilderInner {...props} />
    </ReactFlowProvider>
  )
}

export default VisualPipelineBuilder
