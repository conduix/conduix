/**
 * 워크플로우 세트 — 함께 돌아야 데이터가 완성되는 묶음.
 *
 * 왜 tags 인가: 워크플로우 간 의존을 표현하는 스키마가 없고, 새로 추가하면 마이그레이션과
 * 배포가 필요하다. tags 는 이미 있고 자유 문자열이라 규칙만 정하면 바로 쓸 수 있다.
 *
 * 목록(ProjectDetail)과 상세(WorkflowDetail) 양쪽에서 같은 판정을 써야 하므로 여기 모은다 —
 * 각자 구현하면 한쪽만 고쳐져 표시가 어긋난다.
 */

export const SET_TAG_PREFIX = 'set:'

/** 워크플로우가 속한 세트 이름. 없으면 null. */
export function setNameOf(tags?: string): string | null {
  if (!tags) return null
  for (const raw of tags.split(',')) {
    const t = raw.trim()
    if (t.startsWith(SET_TAG_PREFIX)) {
      const name = t.slice(SET_TAG_PREFIX.length).trim()
      if (name) return name
    }
  }
  return null
}

/**
 * 세트 이름 → 구성원 목록.
 *
 * 혼자만 태그를 가진 경우는 세트가 아니다 — 짝이 없는데 "세트"라고 표시하면
 * 사용자가 없는 짝을 찾게 된다.
 */
export function buildSetIndex<T extends { id: string; tags?: string }>(
  workflows: T[],
): Map<string, T[]> {
  const byName = new Map<string, T[]>()
  for (const w of workflows) {
    const name = setNameOf(w.tags)
    if (!name) continue
    byName.set(name, [...(byName.get(name) ?? []), w])
  }
  for (const [name, members] of byName) {
    if (members.length < 2) byName.delete(name)
  }
  return byName
}

/**
 * 기존 tags 에서 set: 항목만 교체한다.
 *
 * 다른 태그(용도·팀 구분 등)를 지우면 사용자가 설정한 정보가 사라진다.
 * 세트 이름이 비면 set: 항목을 제거한다 = 세트에서 빼기.
 */
export function applySetTag(currentTags: string | undefined, setName: string): string[] {
  const kept = (currentTags ?? '')
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t && !t.startsWith(SET_TAG_PREFIX))

  const name = setName.trim()
  return name ? [...kept, `${SET_TAG_PREFIX}${name}`] : kept
}
