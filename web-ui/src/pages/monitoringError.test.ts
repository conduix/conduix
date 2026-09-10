import { describe, expect, it } from 'vitest'

import workflowDetailSource from './WorkflowDetail.tsx?raw'

// 실측: realtime 파이프라인이 DDL 방어로 정지했을 때 화면에 Live Monitoring 도 에러도
// 안 나왔다. 정지한 파이프라인은 백엔드 statsCollectors 에서 제거되어 pipelines 배열에
// 나타나지 않으므로, 사유는 그룹 레벨 error_message 로만 전달된다.
describe('모니터링 실패 표시', () => {
  const src = workflowDetailSource

  it('모니터링 응답의 error_message 를 모델에 갖는다', () => {
    // 인터페이스에 없으면 값이 와도 UI 가 쓸 수 없다.
    expect(src).toMatch(/interface MonitoringResponse \{[\s\S]{0,400}error_message\?: string/)
  })

  it('파이프라인이 비었을 때 사유를 보여준다 — "데이터 없음" 으로 끝내지 않는다', () => {
    expect(src).toContain('monitoringData?.error_message ?')
    expect(src).toContain("t('workflow.monitoringFailed')")
    expect(src).toContain('{monitoringData.error_message}')
  })

  it('사유를 error severity 로 보여준다 — 실패가 정보처럼 보이면 안 된다', () => {
    expect(src).toMatch(/monitoringData\?\.error_message \?[\s\S]{0,400}severity="error"/)
  })

  it('사유가 없을 때는 기존 "데이터 없음" 문구를 유지한다', () => {
    // 정상 기동 직후에도 pipelines 가 비는 순간이 있어, 그때 에러로 보이면 오해를 준다.
    expect(src).toContain("t('workflow.noMonitoringData')")
  })

  it('긴 사유가 레이아웃을 깨지 않게 줄바꿈한다', () => {
    expect(src).toMatch(/error_message[\s\S]{0,300}wordBreak: 'break-word'/)
  })
})

describe('파이프라인 상태 색상', () => {
  const src = workflowDetailSource

  it('실패 상태를 회색으로 뭉개지 않는다', () => {
    // 이전 코드: color={pipeline.status === 'running' ? 'success' : 'default'}
    expect(src).not.toContain("pipeline.status === 'running' ? 'success' : 'default'")
    expect(src).toContain('monitoringPipelineColor(pipeline.status)')
  })

  it('색상 판정을 한 곳에 둔다', () => {
    expect(src).toMatch(/function monitoringPipelineColor\(status: string\)/)
  })

  it('error/failed 는 error 색, paused/stopped 는 warning 색', () => {
    expect(src).toMatch(/case 'error':[\s\S]{0,60}case 'failed':[\s\S]{0,40}return 'error'/)
    expect(src).toMatch(/case 'paused':[\s\S]{0,60}case 'stopped':[\s\S]{0,40}return 'warning'/)
  })
})
