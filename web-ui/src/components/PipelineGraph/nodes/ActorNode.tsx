import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Card, Badge, Tooltip, Progress } from 'antd'
import {
  ThunderboltOutlined,
  FilterOutlined,
  ExportOutlined,
  ClusterOutlined,
  BranchesOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import type { GraphNodeData, ActorType, ActorState } from '../../../types/graph'

const typeIcons: Record<ActorType, React.ReactNode> = {
  source: <ThunderboltOutlined />,
  transform: <FilterOutlined />,
  sink: <ExportOutlined />,
  router: <BranchesOutlined />,
  supervisor: <ClusterOutlined />,
}

const stateColors: Record<ActorState, 'default' | 'processing' | 'success' | 'warning' | 'error'> = {
  created: 'default',
  starting: 'processing',
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
        size="small"
        style={{
          minWidth: 160,
          borderColor: getBorderColor(),
          borderWidth: selected ? 2 : 1,
          boxShadow: selected ? '0 0 10px rgba(24, 144, 255, 0.3)' : undefined,
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
            <span style={{ color: typeColors[type] }}>{typeIcons[type]}</span>
            <span style={{ fontWeight: 500 }}>{actor_name}</span>
            {hasErrors && (
              <Tooltip title={`Error rate: ${errorRate.toFixed(1)}%`}>
                <WarningOutlined style={{ color: isCritical ? '#ff4d4f' : '#faad14', fontSize: 12 }} />
              </Tooltip>
            )}
          </div>
        }
        extra={
          metrics && (
            <Badge
              status={stateColors[metrics.state]}
              text={<span style={{ fontSize: 11 }}>{metrics.state}</span>}
            />
          )
        }
        styles={{
          header: { padding: '6px 12px', minHeight: 36 },
          body: { padding: '8px 12px' },
        }}
      >
        {metrics ? (
          <div style={{ fontSize: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
              <span style={{ color: '#666' }}>In:</span>
              <strong>{metrics.input_count.toLocaleString()}</strong>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
              <span style={{ color: '#666' }}>Out:</span>
              <strong>{metrics.output_count.toLocaleString()}</strong>
            </div>
            {metrics.error_count > 0 && (
              <div style={{ display: 'flex', justifyContent: 'space-between', color: '#ff4d4f' }}>
                <span>Err:</span>
                <strong>{metrics.error_count.toLocaleString()}</strong>
              </div>
            )}
            {metrics.throughput_per_sec > 0 && (
              <div style={{ marginTop: 6 }}>
                <Tooltip title={`${metrics.throughput_per_sec.toFixed(1)} records/sec`}>
                  <Progress
                    percent={Math.min(100, metrics.throughput_per_sec / 10)}
                    size="small"
                    showInfo={false}
                    strokeColor={typeColors[type]}
                  />
                </Tooltip>
              </div>
            )}
          </div>
        ) : (
          <div style={{ fontSize: 12, color: '#999', textAlign: 'center' }}>
            No metrics
          </div>
        )}

        {parallelism && parallelism > 1 && (
          <div style={{ fontSize: 11, color: '#666', marginTop: 4, textAlign: 'right' }}>
            x{parallelism}
          </div>
        )}
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
