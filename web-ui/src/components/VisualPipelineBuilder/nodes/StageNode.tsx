import { memo } from 'react'
import { Handle, Position, type NodeProps, type Node } from '@xyflow/react'
import { Box, Typography, Chip } from '@mui/material'
import {
  FilterAlt as FilterIcon,
  Transform as TransformIcon,
  Merge as MergeIcon,
  CallSplit as RouterIcon,
  Verified as ValidateIcon,
  Speed as ThrottleIcon,
  ContentCopy as DedupeIcon,
  AccountTree as SubPipelineIcon,
  Layers as AggregateIcon,
  Search as LookupIcon,
  Autorenew as AsyncIcon,
} from '@mui/icons-material'

const stageTypeIcons: Record<string, React.ReactNode> = {
  filter: <FilterIcon fontSize="small" />,
  remap: <TransformIcon fontSize="small" />,
  enrich: <MergeIcon fontSize="small" />,
  router: <RouterIcon fontSize="small" />,
  validate: <ValidateIcon fontSize="small" />,
  throttle: <ThrottleIcon fontSize="small" />,
  dedupe: <DedupeIcon fontSize="small" />,
  sub_pipeline: <SubPipelineIcon fontSize="small" />,
  aggregate: <AggregateIcon fontSize="small" />,
  windowed_aggregate: <AggregateIcon fontSize="small" />,
  batch_lookup: <LookupIcon fontSize="small" />,
  async_enrich: <AsyncIcon fontSize="small" />,
  es_lookup: <LookupIcon fontSize="small" />,
  lookup_enrich: <LookupIcon fontSize="small" />,
}

type StageNodeData = {
  label: string
  stageType: string
  config: Record<string, unknown>
  index: number
}

type StageNodeType = Node<StageNodeData, 'stage'>

export const StageNode = memo(({ data, selected }: NodeProps<StageNodeType>) => {
  const icon = stageTypeIcons[data.stageType] || <TransformIcon fontSize="small" />

  return (
    <Box
      sx={{
        px: 2,
        py: 1.5,
        minWidth: 150,
        bgcolor: selected ? 'primary.light' : 'primary.main',
        color: 'white',
        borderRadius: 2,
        border: selected ? '3px solid' : '1px solid',
        borderColor: selected ? 'primary.dark' : 'primary.dark',
        boxShadow: selected ? 4 : 2,
        transition: 'all 0.2s',
        '&:hover': {
          boxShadow: 4,
        },
      }}
    >
      {/* Input Handle */}
      <Handle
        type="target"
        position={Position.Left}
        style={{
          width: 12,
          height: 12,
          background: '#fff',
          border: '2px solid #1890ff',
        }}
      />

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
        {icon}
        <Typography variant="subtitle2" fontWeight="bold">
          {data.label}
        </Typography>
      </Box>
      <Chip
        label={data.stageType}
        size="small"
        sx={{
          bgcolor: 'rgba(255,255,255,0.2)',
          color: 'white',
          fontSize: '0.7rem',
          height: 20,
        }}
      />

      {/* Output Handle */}
      <Handle
        type="source"
        position={Position.Right}
        style={{
          width: 12,
          height: 12,
          background: '#fff',
          border: '2px solid #1890ff',
        }}
      />
    </Box>
  )
})

StageNode.displayName = 'StageNode'
