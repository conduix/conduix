import { DragEvent } from 'react'
import {
  Paper,
  Typography,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Divider,
  Tooltip,
} from '@mui/material'
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
  filter: <FilterIcon />,
  remap: <TransformIcon />,
  enrich: <MergeIcon />,
  router: <RouterIcon />,
  validate: <ValidateIcon />,
  throttle: <ThrottleIcon />,
  dedupe: <DedupeIcon />,
  sub_pipeline: <SubPipelineIcon />,
  aggregate: <AggregateIcon />,
  batch_lookup: <LookupIcon />,
  async_enrich: <AsyncIcon />,
  es_lookup: <LookupIcon />,
}

interface StageType {
  type: string
  label: string
  description: string
}

interface StagePanelProps {
  stageTypes: StageType[]
}

export function StagePanel({ stageTypes }: StagePanelProps) {
  const onDragStart = (event: DragEvent, stageType: string) => {
    event.dataTransfer.setData('application/reactflow', stageType)
    event.dataTransfer.effectAllowed = 'move'
  }

  // Group stage types
  const basicStages = stageTypes.filter((s) =>
    ['filter', 'remap', 'enrich', 'validate'].includes(s.type)
  )
  const advancedStages = stageTypes.filter((s) =>
    ['aggregate', 'router', 'dedupe', 'throttle', 'sub_pipeline'].includes(s.type)
  )
  const lookupStages = stageTypes.filter((s) =>
    ['batch_lookup', 'async_enrich', 'es_lookup'].includes(s.type)
  )

  const renderStageItem = (stage: StageType) => (
    <Tooltip key={stage.type} title={stage.description} placement="right">
      <ListItem
        draggable
        onDragStart={(e) => onDragStart(e, stage.type)}
        sx={{
          cursor: 'grab',
          borderRadius: 1,
          mb: 0.5,
          bgcolor: 'background.paper',
          border: '1px solid',
          borderColor: 'divider',
          '&:hover': {
            bgcolor: 'action.hover',
            borderColor: 'primary.main',
          },
          '&:active': {
            cursor: 'grabbing',
          },
        }}
      >
        <ListItemIcon sx={{ minWidth: 36 }}>
          {stageTypeIcons[stage.type] || <TransformIcon />}
        </ListItemIcon>
        <ListItemText
          primary={stage.label}
          slotProps={{ primary: {
            variant: 'body2',
            sx: { fontWeight: 500 },
          } }}
        />
      </ListItem>
    </Tooltip>
  )

  return (
    <Paper
      sx={{
        width: 220,
        height: '100%',
        overflow: 'auto',
        borderRight: 1,
        borderColor: 'divider',
        p: 1.5,
      }}
      elevation={0}
    >
      <Typography
        variant="subtitle2"
        sx={{
          color: "text.secondary",
          mb: 1,
          textTransform: 'uppercase',
          fontSize: '0.7rem',
          fontWeight: 600
        }}>
        Stages
      </Typography>
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          display: 'block',
          mb: 2
        }}>
        Drag and drop to add
      </Typography>
      {/* Basic Stages */}
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          display: 'block',
          mb: 1,
          fontWeight: 600
        }}>
        Basic
      </Typography>
      <List dense disablePadding sx={{ mb: 2 }}>
        {basicStages.map(renderStageItem)}
      </List>
      <Divider sx={{ my: 1.5 }} />
      {/* Advanced Stages */}
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          display: 'block',
          mb: 1,
          fontWeight: 600
        }}>
        Advanced
      </Typography>
      <List dense disablePadding sx={{ mb: 2 }}>
        {advancedStages.map(renderStageItem)}
      </List>
      <Divider sx={{ my: 1.5 }} />
      {/* Lookup Stages */}
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          display: 'block',
          mb: 1,
          fontWeight: 600
        }}>
        Lookup
      </Typography>
      <List dense disablePadding>
        {lookupStages.map(renderStageItem)}
      </List>
    </Paper>
  );
}

export default StagePanel
