import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import {
  Card,
  CardHeader,
  CardContent,
  Badge,
  Tooltip,
  LinearProgress,
  Box,
  Typography,
} from '@mui/material'
import BoltIcon from '@mui/icons-material/Bolt'
import FilterAltIcon from '@mui/icons-material/FilterAlt'
import OutputIcon from '@mui/icons-material/Output'
import HubIcon from '@mui/icons-material/Hub'
import AccountTreeIcon from '@mui/icons-material/AccountTree'
import WarningIcon from '@mui/icons-material/Warning'
import type { GraphNodeData, ActorType, ActorState } from '../../../types/graph'

const typeIcons: Record<ActorType, React.ReactNode> = {
  source: <BoltIcon fontSize="small" />,
  transform: <FilterAltIcon fontSize="small" />,
  sink: <OutputIcon fontSize="small" />,
  router: <AccountTreeIcon fontSize="small" />,
  supervisor: <HubIcon fontSize="small" />,
}

const stateColors: Record<ActorState, 'default' | 'primary' | 'success' | 'warning' | 'error'> = {
  created: 'default',
  starting: 'primary',
  running: 'success',
  stopping: 'warning',
  stopped: 'default',
  restarting: 'warning',
  failed: 'error',
}

const typeColors: Record<ActorType, string> = {
  source: '#52c41a',
  transform: '#1890ff',
  sink: '#722ed1',
  router: '#fa8c16',
  supervisor: '#13c2c2',
}

interface ActorNodeProps {
  data: GraphNodeData
  selected?: boolean
}

export const ActorNode = memo(function ActorNode({ data, selected }: ActorNodeProps) {
  const { actor_name, actor_type, metrics, parallelism } = data
  const type = actor_type as ActorType

  const errorRate = metrics && metrics.input_count > 0
    ? (metrics.error_count / metrics.input_count) * 100
    : 0

  const hasErrors = errorRate > 0
  const isCritical = errorRate > 10

  const getBorderColor = () => {
    if (selected) return '#1890ff'
    if (isCritical) return '#ff4d4f'
    if (errorRate > 5) return '#fa8c16'
    if (hasErrors) return '#faad14'
    return typeColors[type] || '#d9d9d9'
  }

  return (
    <>
      {type !== 'source' && (
        <Handle
          type="target"
          position={Position.Left}
          style={{
            background: typeColors[type],
            width: 10,
            height: 10,
          }}
        />
      )}

      <Card
        variant="outlined"
        sx={{
          minWidth: 160,
          borderColor: getBorderColor(),
          borderWidth: selected ? 2 : 1,
          boxShadow: selected ? '0 0 10px rgba(24, 144, 255, 0.3)' : undefined,
        }}
      >
        <CardHeader
          sx={{
            py: 0.75,
            px: 1.5,
            minHeight: 36,
            '& .MuiCardHeader-content': {
              overflow: 'hidden',
            },
            '& .MuiCardHeader-action': {
              alignSelf: 'center',
              m: 0,
            },
          }}
          title={
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
              <Box sx={{ color: typeColors[type], display: 'flex', alignItems: 'center' }}>
                {typeIcons[type]}
              </Box>
              <Typography
                variant="body2"
                sx={{ fontWeight: 500, fontSize: 13 }}
              >
                {actor_name}
              </Typography>
              {hasErrors && (
                <Tooltip title={`Error rate: ${errorRate.toFixed(1)}%`}>
                  <WarningIcon
                    sx={{
                      color: isCritical ? '#ff4d4f' : '#faad14',
                      fontSize: 14,
                    }}
                  />
                </Tooltip>
              )}
            </Box>
          }
          action={
            metrics && (
              <Badge
                color={stateColors[metrics.state]}
                variant="dot"
                sx={{
                  '& .MuiBadge-badge': {
                    right: 4,
                    top: 4,
                  },
                }}
              >
                <Typography variant="caption" sx={{ fontSize: 11, pr: 1 }}>
                  {metrics.state}
                </Typography>
              </Badge>
            )
          }
        />
        <CardContent sx={{ py: 1, px: 1.5, '&:last-child': { pb: 1 } }}>
          {metrics ? (
            <Box sx={{ fontSize: 12 }}>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.5 }}>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  In:
                </Typography>
                <Typography variant="caption" sx={{ fontWeight: 600 }}>
                  {metrics.input_count.toLocaleString()}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.5 }}>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  Out:
                </Typography>
                <Typography variant="caption" sx={{ fontWeight: 600 }}>
                  {metrics.output_count.toLocaleString()}
                </Typography>
              </Box>
              {metrics.error_count > 0 && (
                <Box sx={{ display: 'flex', justifyContent: 'space-between', color: '#ff4d4f' }}>
                  <Typography variant="caption">Err:</Typography>
                  <Typography variant="caption" sx={{ fontWeight: 600 }}>
                    {metrics.error_count.toLocaleString()}
                  </Typography>
                </Box>
              )}
              {metrics.throughput_per_sec > 0 && (
                <Box sx={{ mt: 0.75 }}>
                  <Tooltip title={`${metrics.throughput_per_sec.toFixed(1)} records/sec`}>
                    <LinearProgress
                      variant="determinate"
                      value={Math.min(100, metrics.throughput_per_sec / 10)}
                      sx={{
                        height: 4,
                        borderRadius: 2,
                        '& .MuiLinearProgress-bar': {
                          backgroundColor: typeColors[type],
                        },
                      }}
                    />
                  </Tooltip>
                </Box>
              )}
            </Box>
          ) : (
            <Typography
              variant="caption"
              sx={{ color: 'text.disabled', textAlign: 'center', display: 'block' }}
            >
              No metrics
            </Typography>
          )}

          {parallelism && parallelism > 1 && (
            <Typography
              variant="caption"
              sx={{ color: 'text.secondary', mt: 0.5, textAlign: 'right', display: 'block' }}
            >
              x{parallelism}
            </Typography>
          )}
        </CardContent>
      </Card>

      {type !== 'sink' && (
        <Handle
          type="source"
          position={Position.Right}
          style={{
            background: typeColors[type],
            width: 10,
            height: 10,
          }}
        />
      )}
    </>
  )
})

export default ActorNode
