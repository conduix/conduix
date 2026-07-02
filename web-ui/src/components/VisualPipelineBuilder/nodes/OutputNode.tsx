import { memo } from 'react'
import { Handle, Position, type NodeProps, type Node } from '@xyflow/react'
import { Box, Typography, Chip } from '@mui/material'
import {
  Output as OutputIcon,
  Search as ElasticsearchIcon,
  Cloud as S3Icon,
  Storage as SqlIcon,
  CloudQueue as KafkaIcon,
  Http as HttpIcon,
  Description as FileIcon,
} from '@mui/icons-material'

const outputTypeIcons: Record<string, React.ReactNode> = {
  elasticsearch: <ElasticsearchIcon fontSize="small" />,
  s3: <S3Icon fontSize="small" />,
  sql: <SqlIcon fontSize="small" />,
  kafka: <KafkaIcon fontSize="small" />,
  mongodb: <SqlIcon fontSize="small" />,
  rest_api: <HttpIcon fontSize="small" />,
  file: <FileIcon fontSize="small" />,
}

type OutputNodeData = {
  label: string
  outputType: string
  config: Record<string, unknown>
  index: number
}

type OutputNodeType = Node<OutputNodeData, 'output'>

export const OutputNode = memo(({ data, selected }: NodeProps<OutputNodeType>) => {
  const icon = outputTypeIcons[data.outputType] || <OutputIcon fontSize="small" />

  return (
    <Box
      sx={{
        px: 2,
        py: 1.5,
        minWidth: 150,
        bgcolor: selected ? 'secondary.light' : 'secondary.main',
        color: 'white',
        borderRadius: 2,
        border: selected ? '3px solid' : '1px solid',
        borderColor: selected ? 'secondary.dark' : 'secondary.dark',
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
          border: '2px solid #722ed1',
        }}
      />
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
        {icon}
        <Typography variant="subtitle2" sx={{
          fontWeight: "bold"
        }}>
          {data.label}
        </Typography>
      </Box>
      <Chip
        label={data.outputType}
        size="small"
        sx={{
          bgcolor: 'rgba(255,255,255,0.2)',
          color: 'white',
          fontSize: '0.7rem',
          height: 20,
        }}
      />
    </Box>
  );
})

OutputNode.displayName = 'OutputNode'
