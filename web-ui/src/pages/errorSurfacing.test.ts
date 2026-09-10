import { describe, expect, it } from 'vitest'

import workflowsSource from './Workflows.tsx?raw'
import workflowDetailSource from './WorkflowDetail.tsx?raw'

// 서버가 사유를 담아 응답하는데도 UI 가 console.error 만 하면, 사용자는 버튼을 눌러도
// 아무 반응이 없다고 느낀다 — 실패 사실 자체가 화면에 없다(실측).
describe('실패 사유를 화면에 전달한다 — 목록', () => {
  const src = workflowsSource

  it('실행 실패를 조용히 삼키지 않는다', () => {
    expect(src).not.toMatch(/catch \(error\) \{\s*console\.error\('Failed to start workflow/)
  })

  it('실행 실패 시 서버 사유를 보여준다', () => {
    expect(src).toMatch(/handleStart[\s\S]{0,900}showError\(/)
    expect(src).toMatch(/handleStart[\s\S]{0,900}response\?\.data\?\.error\?\.message/)
  })

  it('중지 실패도 사유를 보여준다', () => {
    expect(src).toMatch(/handleStop[\s\S]{0,700}showError\(/)
  })

  it('목록 조회 실패를 알린다 — 빈 목록과 구분되어야 한다', () => {
    expect(src).toMatch(/workflow\.loadError/)
  })

  it('남은 console.error 가 실제 핸들러에 없다', () => {
    // 주석 안의 언급은 허용한다(왜 고쳤는지 설명).
    const codeLines = src
      .split('\n')
      .filter((l) => !l.trim().startsWith('//'))
      .join('\n')
    expect(codeLines).not.toContain('console.error(')
  })
})

// 서버가 빌드를 걸고 실행을 예약하면 status=building 으로 응답한다.
// 이걸 "실행됨" 으로 표시하면 사용자는 왜 진행이 없는지 알 수 없다.
describe('자동 빌드 응답 처리', () => {
  it('목록 화면이 building 을 구분한다', () => {
    expect(workflowsSource).toContain("res.data?.status === 'building'")
    expect(workflowsSource).toContain("t('workflow.buildStarted')")
  })

  it('상세 화면도 building 을 구분한다', () => {
    expect(workflowDetailSource).toContain("res.data?.status === 'building'")
    expect(workflowDetailSource).toContain("t('workflow.buildStarted')")
  })

  it('서버가 준 안내 문구를 우선 사용한다 — 사유별 메시지가 다르다', () => {
    // 서버는 plugin_changed / core_changed 등 이유별로 다른 문구를 만든다.
    expect(workflowsSource).toMatch(/res\.message \|\| t\('workflow\.buildStarted'\)/)
    expect(workflowDetailSource).toMatch(/res\.message \|\| t\('workflow\.buildStarted'\)/)
  })
})
