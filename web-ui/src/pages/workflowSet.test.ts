import { describe, expect, it } from 'vitest'

import projectDetailSource from './ProjectDetail.tsx?raw'

// 세트 판별 로직은 ProjectDetail 안의 순수 함수다. 컴포넌트를 렌더하지 않고
// 소스에 규칙이 살아있는지 검증한다(jsdom 설정 불필요 — 다른 테스트와 같은 방식).
describe('워크플로우 세트 표시', () => {
  const src = projectDetailSource

  it('set: 접두사로 세트를 식별한다', () => {
    expect(src).toContain("const SET_TAG_PREFIX = 'set:'")
    expect(src).toContain('function setNameOf(')
  })

  it('혼자만 태그를 가진 경우는 세트로 보지 않는다', () => {
    // 짝이 없는데 "세트"라고 표시하면 사용자가 없는 짝을 찾게 된다.
    expect(src).toMatch(/if \(ids\.length < 2\) byName\.delete\(name\)/)
  })

  it('세트 인덱스를 useMemo 로 만든다 — 행마다 전체를 훑으면 O(n²)', () => {
    expect(src).toMatch(/useMemo\(\(\) => buildSetIndex\(workflows\), \[workflows\]\)/)
  })

  it('세트 배지는 구성 개수를 보여준다', () => {
    expect(src).toContain("t('workflow.setBadge'")
    expect(src).toContain('setMembers.length')
  })
})

describe('목록 Description 처리', () => {
  const src = projectDetailSource

  it('워크플로우 목록에서 description 을 렌더하지 않는다', () => {
    // 여러 줄 설명이 들어오면 행 높이가 늘어나 표를 읽을 수 없다(실측).
    expect(src).not.toContain('{workflow.description || ')
  })

  it('프로젝트 상세에서는 description 을 보여준다', () => {
    expect(src).toContain('{project.description || ')
  })

  it('데이터모델 목록 description 은 nowrap 으로 말줄임한다', () => {
    // textOverflow 만으로는 줄바꿈이 그대로 펼쳐져 말줄임이 동작하지 않는다.
    expect(src).toMatch(/whiteSpace: 'nowrap'[\s\S]{0,200}dataType\.description/)
  })
})
