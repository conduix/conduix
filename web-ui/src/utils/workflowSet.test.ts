import { describe, expect, it } from 'vitest'

import { applySetTag, buildSetIndex, setNameOf } from './workflowSet'

describe('setNameOf', () => {
  it('set: 접두사에서 이름을 뽑는다', () => {
    expect(setNameOf('set:restroom-pipeline')).toBe('restroom-pipeline')
  })

  it('다른 태그와 섞여 있어도 찾는다', () => {
    expect(setNameOf('team-a, set:welfare, urgent')).toBe('welfare')
  })

  it('공백을 다듬는다', () => {
    expect(setNameOf('  set: welfare  ')).toBe('welfare')
  })

  it('세트 태그가 없으면 null', () => {
    expect(setNameOf('team-a, urgent')).toBeNull()
    expect(setNameOf('')).toBeNull()
    expect(setNameOf(undefined)).toBeNull()
  })

  it('이름이 빈 set: 은 세트가 아니다', () => {
    expect(setNameOf('set:')).toBeNull()
    expect(setNameOf('set:   ')).toBeNull()
  })
})

describe('buildSetIndex', () => {
  it('같은 이름을 가진 것들을 묶는다', () => {
    const idx = buildSetIndex([
      { id: 'a', tags: 'set:x' },
      { id: 'b', tags: 'set:x' },
      { id: 'c', tags: 'set:y' },
      { id: 'd', tags: 'set:y' },
    ])
    expect(idx.get('x')?.map((w) => w.id)).toEqual(['a', 'b'])
    expect(idx.get('y')?.map((w) => w.id)).toEqual(['c', 'd'])
  })

  it('혼자만 태그를 가진 경우는 세트가 아니다', () => {
    // 짝이 없는데 "세트"라고 표시하면 사용자가 없는 짝을 찾게 된다.
    const idx = buildSetIndex([
      { id: 'a', tags: 'set:lonely' },
      { id: 'b', tags: 'set:pair' },
      { id: 'c', tags: 'set:pair' },
    ])
    expect(idx.has('lonely')).toBe(false)
    expect(idx.has('pair')).toBe(true)
  })

  it('태그 없는 워크플로우는 무시한다', () => {
    const idx = buildSetIndex([{ id: 'a' }, { id: 'b', tags: '' }])
    expect(idx.size).toBe(0)
  })
})

describe('applySetTag', () => {
  it('세트 태그를 추가한다', () => {
    expect(applySetTag(undefined, 'welfare')).toEqual(['set:welfare'])
  })

  it('기존 set: 만 교체하고 다른 태그는 남긴다', () => {
    // 다른 태그를 지우면 사용자가 설정한 정보가 사라진다.
    expect(applySetTag('team-a, set:old, urgent', 'new')).toEqual(['team-a', 'urgent', 'set:new'])
  })

  it('빈 이름이면 세트에서 뺀다', () => {
    expect(applySetTag('team-a, set:old', '')).toEqual(['team-a'])
    expect(applySetTag('set:old', '   ')).toEqual([])
  })

  it('공백만 있는 항목은 버린다', () => {
    expect(applySetTag('team-a, , set:old', 'x')).toEqual(['team-a', 'set:x'])
  })
})
