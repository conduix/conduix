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

/**
 * tags 를 태그 배열로 만든다.
 *
 * 저장 형식이 두 가지다:
 * - JSON 배열 문자열 `["set:x","team-a"]` — 서버가 API 요청의 []string 을 직렬화한 것
 * - 콤마 구분 문자열 `set:x, team-a` — DB 에 직접 넣거나 손으로 편집한 경우
 *
 * 한쪽만 처리하면 UI 로 설정한 세트가 화면에 안 뜬다(실측: API 저장 후 배지 미표시).
 */
function parseTags(tags?: string): string[] {
  if (!tags) return []
  const trimmed = tags.trim()
  if (!trimmed) return []

  if (trimmed.startsWith('[')) {
    try {
      const parsed: unknown = JSON.parse(trimmed)
      if (Array.isArray(parsed)) {
        return parsed.filter((t): t is string => typeof t === 'string').map((t) => t.trim())
      }
    } catch {
      // JSON 이 깨졌으면 아래 콤마 분리로 폴백한다 — 화면이 비는 것보다 낫다.
    }
  }
  // 폴백 시 JSON 잔여 문자(대괄호·따옴표)를 떼어낸다. 안 떼면 '[set:x' 처럼 남아
  // set: 접두사 매칭이 실패한다.
  return trimmed
    .split(',')
    .map((t) => t.trim().replace(/^[[\]"']+|[[\]"']+$/g, '').trim())
    .filter((t) => t)
}

/** 워크플로우가 속한 세트 이름. 없으면 null. */
export function setNameOf(tags?: string): string | null {
  for (const t of parseTags(tags)) {
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
  // parseTags 를 공유한다 — 여기서 따로 split 하면 JSON 형식의 기존 태그가 유실된다.
  const kept = parseTags(currentTags).filter((t) => t && !t.startsWith(SET_TAG_PREFIX))

  const name = setName.trim()
  return name ? [...kept, `${SET_TAG_PREFIX}${name}`] : kept
}
