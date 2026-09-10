import { describe, expect, it } from 'vitest'

import projectDetailSource from './ProjectDetail.tsx?raw'
import workflowDetailSource from './WorkflowDetail.tsx?raw'

// 세트 로직 자체는 utils/workflowSet.test.ts 가 단위 테스트한다.
// 여기서는 화면들이 그 공용 모듈을 쓰고 필요한 UI 를 갖췄는지만 본다.

describe('세트 배선 — 목록', () => {
  it('공용 모듈을 쓴다 — 각자 구현하면 표시가 어긋난다', () => {
    expect(projectDetailSource).toContain("from '../utils/workflowSet'")
    expect(projectDetailSource).not.toContain("const SET_TAG_PREFIX = 'set:'")
  })

  it('배지에 세트 이름을 보여준다 — 숫자만으로는 무슨 세트인지 알 수 없다', () => {
    expect(projectDetailSource).toContain("t('workflow.setBadge'")
    expect(projectDetailSource).toMatch(/name: setName/)
  })

  it('세트 인덱스를 useMemo 로 만든다 — 행마다 전체를 훑으면 O(n²)', () => {
    expect(projectDetailSource).toMatch(/useMemo\(\(\) => buildSetIndex\(workflows\), \[workflows\]\)/)
  })
})

describe('세트 배선 — 설정/해제', () => {
  const src = projectDetailSource

  it('편집 폼에 세트 이름 입력이 있다', () => {
    expect(src).toContain("t('workflow.setNameField'")
    expect(src).toContain('workflowForm.set_name')
  })

  it('기존 세트를 datalist 로 제시해 오타를 줄인다', () => {
    expect(src).toContain('workflow-set-options')
  })

  it('편집 진입 시 현재 세트 이름을 채운다', () => {
    expect(src).toMatch(/set_name: setNameOf\(workflow\.tags\) \|\| ''/)
  })

  it('저장 시 set_name 을 tags 로 변환해 보낸다', () => {
    expect(src).toContain('applySetTag(')
  })
})

describe('세트 배선 — 상세 화면', () => {
  const src = workflowDetailSource

  it('상세에도 세트를 보여준다 — 목록에만 있으면 단건 판단이 안 된다', () => {
    expect(src).toContain("from '../utils/workflowSet'")
    expect(src).toContain('setNameOf(workflow.tags)')
  })

  it('같은 세트의 다른 워크플로우를 링크로 제시한다', () => {
    expect(src).toContain('setSiblings')
    expect(src).toContain("t('workflow.setRunsWith'")
  })

  it('세트가 아니면 행을 만들지 않는다 — 빈 행은 정보가 아니다', () => {
    expect(src).toMatch(/\{setNameOf\(workflow\.tags\) && \(/)
  })
})

describe('목록 Description 처리', () => {
  const src = projectDetailSource

  it('워크플로우 목록에서 description 을 렌더하지 않는다', () => {
    expect(src).not.toContain('{workflow.description || ')
  })

  it('프로젝트 상세에서는 description 을 보여준다', () => {
    expect(src).toContain('{project.description || ')
  })

  it('데이터모델 목록 description 은 nowrap 으로 말줄임한다', () => {
    expect(src).toMatch(/whiteSpace: 'nowrap'[\s\S]{0,200}dataType\.description/)
  })
})
