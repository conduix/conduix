/**
 * import 문 ↔ 의존성 레지스트리 연계의 순수 로직.
 *
 * 사용자는 소스에 import 를 쓰고, 플랫폼은 레지스트리(허용 목록 + 버전)를 관리한다.
 * 같은 정보를 두 번 입력하지 않게, 여기서 import 를 파싱해 "등록됨/미등록" 을 판정하고
 * 서버 에러·gopls 진단을 사용자가 행동할 수 있는 형태로 바꾼다. 컴포넌트에서 분리한 이유는
 * 단위 테스트 — Monaco·React 없이 규칙만 검증한다.
 */

/** conduix 내부 모듈 — 레지스트리 등록 없이 import 가능(서버 dependency.InternalModulePrefixes 와 동일). */
export const INTERNAL_MODULE_PREFIXES = [
  'github.com/conduix/conduix/plugin-sdk',
  'github.com/conduix/conduix/pipeline-core',
  'github.com/conduix/conduix/shared',
]

/** Go 소스의 import 경로 목록. 블록 import 와 단일 import 모두, 주석 줄은 제외. */
export function parseGoImports(source: string): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  const push = (p: string) => {
    if (!seen.has(p)) {
      seen.add(p)
      out.push(p)
    }
  }
  // import ( ... ) 블록
  const blockRe = /^\s*import\s*\(([\s\S]*?)^\s*\)/gm
  let m: RegExpExecArray | null
  while ((m = blockRe.exec(source)) !== null) {
    for (const rawLine of m[1].split('\n')) {
      const line = rawLine.replace(/\/\/.*$/, '').trim()
      const pm = /(?:^|\s)"([^"]+)"\s*$/.exec(line)
      if (pm) push(pm[1])
    }
  }
  // import "x" / import alias "x" (단일)
  const singleRe = /^\s*import\s+(?:[\w.]+\s+)?"([^"]+)"/gm
  while ((m = singleRe.exec(source)) !== null) push(m[1])
  return out
}

/** 표준 라이브러리: 첫 세그먼트에 점이 없다(서버 dependency.IsStdlib 와 동일 규칙). */
export function isStdlibImport(importPath: string): boolean {
  const first = importPath.split('/')[0]
  return !first.includes('.')
}

export function isInternalImport(importPath: string): boolean {
  return INTERNAL_MODULE_PREFIXES.some((p) => importPath === p || importPath.startsWith(p + '/'))
}

/** import 경로를 소유한 등록 모듈(가장 긴 접두사). 없으면 undefined. */
export function owningModule(importPath: string, modulePaths: string[]): string | undefined {
  let best: string | undefined
  for (const m of modulePaths) {
    if ((importPath === m || importPath.startsWith(m + '/')) && (!best || m.length > best.length)) best = m
  }
  return best
}

export interface ImportUsage {
  importPath: string
  /** 등록 모듈 경로. 미등록이면 undefined. */
  modulePath?: string
}

/** 외부 import 만 골라 등록 여부를 붙인다. 표준 라이브러리·내부 모듈은 제외. */
export function classifyImports(imports: string[], modulePaths: string[]): ImportUsage[] {
  return imports
    .filter((p) => !isStdlibImport(p) && !isInternalImport(p))
    .map((p) => ({ importPath: p, modulePath: owningModule(p, modulePaths) }))
}

/** 서버 응답 코드(shared/types ErrCodeMissingModules). */
export const MISSING_MODULES_CODE = 'BUSINESS_MISSING_MODULES'

/**
 * axios 에러에서 미등록 모듈 상세({import 경로: 제안 모듈 경로})를 꺼낸다.
 * 해당 코드가 아니면 null — 호출부는 일반 에러 처리로 떨어진다.
 */
export function missingModulesFromError(err: unknown): Record<string, string> | null {
  const data = (err as { response?: { data?: { error?: { code?: string; details?: Record<string, string> } } } })
    ?.response?.data
  const apiErr = data?.error
  if (!apiErr || apiErr.code !== MISSING_MODULES_CODE) return null
  return apiErr.details && Object.keys(apiErr.details).length > 0 ? apiErr.details : {}
}

/** 제안 모듈 경로들의 중복 제거 목록(등록 호출 단위). */
export function uniqueSuggestedModules(details: Record<string, string>): string[] {
  return Array.from(new Set(Object.values(details).filter((v) => v && v.trim() !== '')))
}

/** 우리가 바꿔 쓴 진단 메시지의 접두 — code action 이 이 마커를 알아보는 표식. */
export const UNREGISTERED_IMPORT_PREFIX = '레지스트리 미등록 모듈'

/**
 * gopls 의 "모듈 없음" 진단을 사용자가 행동할 수 있는 문장으로 바꾼다.
 * 원문 예: `could not import github.com/x/y (no required module provides package "github.com/x/y")`
 * 다른 진단은 null(그대로 표시).
 */
export function friendlyGoplsMessage(message: string): { message: string; importPath: string } | null {
  const m =
    /no required module provides package\s+"?([^\s")]+)"?/.exec(message) ??
    /could not import\s+"?([^\s")]+)"?/.exec(message)
  if (!m) return null
  const importPath = m[1]
  return {
    importPath,
    message: `${UNREGISTERED_IMPORT_PREFIX}: ${importPath} — 저장 시 등록이 필요합니다. 의존성 탭 또는 빠른 수정으로 추가하세요.`,
  }
}

/** 모듈·버전 "추가" 권한(서버 라우트와 동일: operator, admin). */
export function canManageModules(role?: string | null): boolean {
  return role === 'admin' || role === 'operator'
}
