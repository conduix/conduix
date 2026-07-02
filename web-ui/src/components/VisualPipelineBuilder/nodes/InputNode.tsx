import { memo } from 'react'
import { Handle, Position, type NodeProps, type Node } from '@xyflow/react'
import { Box, Typography, Chip } from '@mui/material'
import {
  Input as InputIcon,
  Cloud as KafkaIcon,
  Storage as SqlIcon,
  Http as HttpIcon,
  Description as FileIcon,
} from '@mui/icons-material'

const inputTypeIcons: Record<string, React.ReactNode> = {
  kafka: <KafkaIcon fontSize="small" />,
  sql: <SqlIcon fontSize="small" />,
  http: <HttpIcon fontSize="small" />,
  rest_api: <HttpIcon fontSize="small" />,
  file: <FileIcon fontSize="small" />,
}

type InputNodeData = {
  label: string
  inputType: string
  config: Record<string, unknown>
}

type InputNodeType = Node<InputNodeData, 'input'>

export const InputNode = memo(({ data, selected }: NodeProps<InputNodeType>) => {
  const icon = inputTypeIcons[data.inputType] || <InputIcon fontSize="small" />

  return (
    <Box
      sx={{
        px: 2,
        py: 1.5,
        minWidth: 150,
        bgcolor: selected ? 'success.light' : 'success.main',
        color: 'white',
        borderRadius: 2,
        border: selected ? '3px solid' : '1px solid',
        borderColor: selected ? 'success.dark' : 'success.dark',
        boxShadow: selected ? 4 : 2,
        transition: 'all 0.2s',
        '&:hover': {
          boxShadow: 4,
        },
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
        {icon}
        <Typography variant="subtitle2" sx={{
          fontWeight: "bold"
        }}>
          {data.label}
        </Typography>
      </Box>
      <Chip
        label={data.inputType}
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
          border: '2px solid #52c41a',
        }}
      />
    </Box>
  );
})

InputNode.displayName = 'InputNode'
