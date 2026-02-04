import { Typography, Box } from '@mui/material'

export default function HistoryPage() {
  return (
    <Box>
      <Typography variant="h5" gutterBottom>
        실행 히스토리
      </Typography>
      <Typography color="text.secondary">
        전체 파이프라인 실행 히스토리가 표시될 예정입니다.
      </Typography>
    </Box>
  )
}
