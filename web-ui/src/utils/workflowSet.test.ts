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

// 서버는 tags 를 JSON 배열 문자열로 저장한다(API 요청의 []string 을 직렬화).
// 콤마 분리만 처리하면 UI 로 설정한 세트가 화면에 안 뜬다(실측).
describe('저장 형식 호환', () => {
  it('JSON 배열 형식을 파싱한다 — 서버 API 저장 형식', () => {
    expect(setNameOf('["set:child-meal"]')).toBe('child-meal')
    expect(setNameOf('["team-a","set:welfare","urgent"]')).toBe('welfare')
  })

  it('콤마 형식도 계속 파싱한다 — DB 직접 편집분', () => {
    expect(setNameOf('set:restroom-pipeline')).toBe('restroom-pipeline')
  })

  it('빈 JSON 배열은 세트 없음', () => {
    expect(setNameOf('[]')).toBeNull()
  })

  it('깨진 JSON 은 콤마 분리로 폴백한다 — 화면이 비는 것보다 낫다', () => {
    expect(setNameOf('[set:broken')).toBe('broken')
  })

  it('applySetTag 가 JSON 형식의 기존 태그를 보존한다', () => {
    // 여기서 따로 split 하면 team-a 가 유실된다.
    expect(applySetTag('["team-a","set:old"]', 'new')).toEqual(['team-a', 'set:new'])
  })

  it('JSON 배열에서 세트만 해제한다', () => {
    expect(applySetTag('["team-a","set:old"]', '')).toEqual(['team-a'])
  })

  it('buildSetIndex 가 두 형식이 섞여도 같은 세트로 묶는다', () => {
    const idx = buildSetIndex([
      { id: 'a', tags: '["set:mixed"]' },
      { id: 'b', tags: 'set:mixed' },
    ])
    expect(idx.get('mixed')?.map((w) => w.id)).toEqual(['a', 'b'])
  })
})
