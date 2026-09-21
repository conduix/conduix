import { describe, expect, it } from 'vitest'

import editorSource from './NativeStageEditor/NativeStageEditor.tsx?raw'
import registrySource from './ModuleRegistry/ModuleRegistryDialog.tsx?raw'
import pluginsSource from '../pages/Plugins.tsx?raw'
import moduleApiSource from '../services/moduleApi.ts?raw'
import en from '../i18n/locales/en.json'
import ko from '../i18n/locales/ko.json'

// 서버는 "기본 버전으로 컴파일되지 않습니다" + 컴파일러 원문을 돌려준다. 그걸 삼키면
// 사용자는 버튼을 눌러도 아무 일도 안 일어난 것처럼 보이고, 왜 안 되는지 알 길이 없다.
describe('의존성 버전 올리기 실패 사유를 화면에 전달한다', () => {
  it('stage 편집기가 올리기 실패를 상태에 담아 표시한다', () => {
    expect(editorSource).toMatch(/handleUpgradeModule[\s\S]{0,600}catch[\s\S]{0,200}setUpgradeError\(/)
    expect(editorSource).toMatch(/upgradeError &&[\s\S]{0,300}Alert/)
  })

  it('실패 원문이 여러 줄이라 줄바꿈을 보존한다', () => {
    expect(editorSource).toMatch(/upgradeError &&[\s\S]{0,300}whiteSpace: 'pre-wrap'/)
  })

  it('모듈 레지스트리도 실패 사유를 표시한다(폐기 409 등)', () => {
    expect(registrySource).toMatch(/catch[\s\S]{0,200}setError\(/)
    expect(registrySource).toMatch(/error &&[\s\S]{0,200}Alert/)
  })

  it('일괄 올리기는 stage 별 성공·실패를 모두 보여준다 — 일부 실패가 묻히면 안 된다', () => {
    expect(registrySource).toMatch(/upgradeResults[\s\S]{0,800}r\.success \? '✓' : '✗'/)
    expect(registrySource).toMatch(/upgradeResults[\s\S]{0,900}r\.error/)
  })
})

describe('기본 버전과 stage 고정 버전을 나란히 보여준다', () => {
  it('편집기가 기본 버전과 이 stage 의 고정 버전을 둘 다 렌더링한다', () => {
    expect(editorSource).toMatch(/plugins\.deps\.defaultVersion/)
    expect(editorSource).toMatch(/plugins\.deps\.thisStage/)
  })

  it('두 값이 같으면 올리기 버튼을 내지 않는다', () => {
    expect(editorSource).toMatch(/const behind = pinned !== undefined && pinned !== m\.version/)
    expect(editorSource).toMatch(/behind && \(/)
  })

  it('목록에 pinned_behind 배지가 있다', () => {
    expect(pluginsSource).toMatch(/plugin\.pinned_behind/)
    expect(pluginsSource).toMatch(/plugin\.pinnedBehind/)
  })
})

describe('레지스트리 API 계약', () => {
  it('기본 버전 변경이 기존 stage 를 바꾸지 않는다는 점을 주석으로 못박는다', () => {
    expect(moduleApiSource).toMatch(/기존 stage 의 고정 버전은 바뀌지 않는다/)
  })

  it('폐기는 쿼리스트링으로 보낸다 — api.delete 는 config 인자를 받지 않는다', () => {
    expect(moduleApiSource).toMatch(/URLSearchParams\(\{ module_path: modulePath, version \}\)/)
  })
})

// fallback 문자열에만 기대면 영어 환경에서 한국어가 그대로 나온다.
describe('의존성 버전 UI 의 i18n 키가 en/ko 양쪽에 등록돼 있다', () => {
  const keys: Array<[string, string]> = [
    ['plugins', 'deps'],
    ['modules', 'registryTitle'],
    ['modules', 'singleOnlyHelp'],
    ['modules', 'upgradeAllHelp'],
    ['plugin', 'depsStatus'],
    ['plugin', 'pinnedBehind'],
    ['common', 'close'],
  ]

  for (const [ns, key] of keys) {
    it(`${ns}.${key}`, () => {
      const enNs = (en as Record<string, Record<string, unknown>>)[ns]
      const koNs = (ko as Record<string, Record<string, unknown>>)[ns]
      expect(enNs?.[key], `en.${ns}.${key}`).toBeDefined()
      expect(koNs?.[key], `ko.${ns}.${key}`).toBeDefined()
    })
  }

  it('영어 번역에 한글이 섞여 있지 않다', () => {
    const enDeps = JSON.stringify((en as Record<string, unknown>).plugins) +
      JSON.stringify((en as Record<string, unknown>).modules)
    expect(enDeps).not.toMatch(/[가-힣]/)
  })
})
