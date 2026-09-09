import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// 메뉴 라벨(Stage)과 URL(/plugins)이 어긋나 있던 것을 고쳤다.
// 라우트·메뉴·리다이렉트는 서로 맞물려야 하고, 한쪽만 바뀌면 죽은 링크가 된다.
// 컴포넌트를 렌더하지 않고 소스를 읽어 검증한다(jsdom 설정 불필요).
const src = (p: string) => readFileSync(join(__dirname, p), 'utf8')

describe('stage 라우트 일관성', () => {
  const app = src('App.tsx')
  const layout = src('components/Layout/MainLayout.tsx')

  it('/stages 가 Stage 페이지를 렌더한다', () => {
    expect(app).toMatch(/<Route\s+path="stages"\s+element={<PluginsPage\s*\/>}/)
  })

  it('/plugins 는 /stages 로 리다이렉트한다 — 기존 북마크가 깨지지 않아야 한다', () => {
    expect(app).toMatch(/<Route\s+path="plugins"\s+element={<Navigate\s+to="\/stages"\s+replace\s*\/>}/)
  })

  it('사이드바 메뉴가 /stages 를 가리킨다', () => {
    expect(layout).toContain("key: '/stages'")
    expect(layout).not.toContain("key: '/plugins'")
  })
})

describe('runner 버전 관측 배선', () => {
  const detail = src('pages/WorkflowDetail.tsx')

  it('실행 타입에 runner_version_id 가 있다', () => {
    expect(detail).toContain('runner_version_id?: string')
  })

  it('최신 ready 버전을 기준으로 비교한다 — 기준이 없으면 경고하지 않아야 한다', () => {
    expect(detail).toContain('latestReadyRunnerVersion')
    // 기준 없이 warning 을 칠하면 오탐이 된다.
    expect(detail).toMatch(/latestReadyRunnerVersion\s*&&\s*exec\.runner_version_id\s*!==\s*latestReadyRunnerVersion/)
  })
})

describe('빌드 모니터링 배선', () => {
  const plugins = src('pages/Plugins.tsx')
  const api = src('services/pluginApi.ts')

  it('폴링이 building 중에도 돈다 — needs_build 만 보면 빌드 시작과 함께 멈춘다', () => {
    expect(plugins).toMatch(/runnerStatus\?\.needs_build\s*\|\|\s*isBuilding/)
  })

  it('폴링이 빌드 히스토리도 갱신한다 — 로그 다이얼로그를 열어둔 채 진행이 보여야 한다', () => {
    expect(plugins).toContain('loadBuildVersions()')
  })

  it('상태 응답 타입에 building/failed 버전이 있다', () => {
    expect(api).toContain('building_version?: RunnerVersion | null')
    expect(api).toContain('last_failed_version?: RunnerVersion | null')
  })

  it('RunnerVersion 에 build_number 가 있다 — id(rv-해시)만으로는 최신 판별이 안 된다', () => {
    expect(api).toContain('build_number: number')
  })
})
