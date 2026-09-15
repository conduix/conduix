import { describe, it, expect } from 'vitest'

import { parseExecutionList } from './Executions'

// 목록 API 응답을 화면이 읽을 수 있는 배열로 바꾸는 변환.
//
// 이 화면은 한때 data.items 를 읽었는데 API 는 data.executions 를 준다.
// 필드명이 어긋나도 예외가 나지 않고 빈 배열이 되어, 실행이 돌고 있는데도
// 화면에는 "실행 중인 워크플로우가 없습니다" 만 떴다(실측: running 1건인데 빈 화면).
// 조용히 비는 실패라 눈으로 보기 전에는 드러나지 않으므로 변환만 따로 고정한다.
describe('parseExecutionList', () => {
  it('API 응답의 executions 를 읽는다', () => {
    const res = {
      executions: [
        { id: 'e1', workflow_id: 'w1', status: 'running', delegated_to: 'conduix-rt' },
        { id: 'e2', workflow_id: 'w2', status: 'running', delegated_to: 'conduix-rt' },
      ],
      total: 2,
      limit: 50,
      offset: 0,
    }
    const rows = parseExecutionList(res)
    expect(rows).toHaveLength(2)
    expect(rows[0].id).toBe('e1')
    expect(rows[0].delegated_to).toBe('conduix-rt')
  })

  it('items 로 오는 응답은 빈 배열이다 — 이 키는 API 가 쓰지 않는다', () => {
    expect(parseExecutionList({ items: [{ id: 'e1' }] })).toEqual([])
  })

  it('배열이 그대로 오면 그대로 쓴다', () => {
    expect(parseExecutionList([{ id: 'e1' }])).toHaveLength(1)
  })

  it('빈 목록·null·형식 오류에도 터지지 않고 빈 배열을 준다', () => {
    expect(parseExecutionList({ executions: [], total: 0 })).toEqual([])
    expect(parseExecutionList(null)).toEqual([])
    expect(parseExecutionList(undefined)).toEqual([])
    expect(parseExecutionList({ executions: 'not-an-array' })).toEqual([])
  })
})
