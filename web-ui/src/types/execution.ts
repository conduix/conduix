// 실행·스케줄 공용 타입.
// 기존엔 WorkflowDetail.tsx 등에 인라인 정의돼 있어 History/Schedules 화면이 재사용할 수 없었다.
// 백엔드 models.WorkflowExecution / handlers.ScheduleResponse 와 대응한다.

export interface WorkflowRef {
  id: string
  name: string
  type?: string
  project_id?: string
}

export interface WorkflowExecution {
  id: string
  workflow_id: string
  cluster_id?: string
  agent_id?: string
  status: string
  started_at: string
  completed_at?: string
  duration_ms?: number
  total_records: number
  failed_records: number
  error_message?: string
  triggered_by?: string
  triggered_by_id?: string
  created_at: string
  parent_execution_id?: string
  total_sub_executions?: number
  completed_sub_executions?: number
  // GET /executions 는 Workflow 를 preload 하므로 이름 표시에 쓸 수 있다.
  workflow?: WorkflowRef
}

export interface ExecutionListData {
  executions: WorkflowExecution[]
  total: number
  limit: number
  offset: number
}

// GET /schedules 응답 1건. 백엔드가 배열을 data 에 직접 담는다(페이징 없음).
export interface WorkflowSchedule {
  workflow_id: string
  workflow_name: string
  type: string // cron | interval | manual (미설정이면 빈 문자열)
  cron?: string
  interval?: string
  timezone: string
  enabled: boolean
  last_run_at?: string
  next_run_at?: string
  status: string
}
